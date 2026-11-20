package dhtcrawler

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/drops"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
)

// errNoMetadata reports that none of the peers supplied metadata.
var errNoMetadata = errors.New("no peer returned metadata")

// errBannedContent reports metadata whose contents or name match the blocking
// rules. The infohash is recorded in the blocklist and is never indexed.
var errBannedContent = errors.New("banned content")

// errDeceptiveContent reports metadata that claims to be media but ships
// executables. The infohash is recorded in the blocklist and is never indexed.
var errDeceptiveContent = errors.New("deceptive content")

// startFetch starts the peer lookup and metadata fetch stages.
func (c *Crawler) startFetch(ctx context.Context, wg *sync.WaitGroup) {
	startPool(ctx, wg, c.cfg.Workers.GetPeers, c.forPeers, &c.inFlight.getPeers, c.getPeers)
	startPool(ctx, wg, c.cfg.Workers.Metadata, c.forFetch, &c.inFlight.metadata, c.metadata)
}

// getPeers looks up the peers for an unseen infohash. The peers are remembered
// in the routing table so this instance can serve them, and passed to the
// metadata stage.
func (c *Crawler) getPeers(ctx context.Context, item hashAndNode) {
	res, err := c.client.GetPeers(ctx, item.node, item.infoHash)
	if err != nil {
		c.stats.peerLookupsFailed.Add(1)

		c.dropUnansweredAddr(ctx, item.node, fmt.Errorf("get_peers: %w", err))

		if ctx.Err() == nil {
			c.logger.Debug("failed to look up peers", "info_hash", item.infoHash.String(), "error", err)
		}

		return
	}

	c.rememberNode(ctx, res.ID, item.node)
	c.addDiscovered(ctx, res.Nodes)

	if len(res.Values) == 0 {
		c.stats.peerLookupsEmpty.Add(1)

		return
	}

	c.stats.peerLookups.Add(1)

	hashPeers := make([]ktable.HashPeer, 0, len(res.Values))
	for _, peer := range res.Values {
		hashPeers = append(hashPeers, ktable.HashPeer{Addr: peer})
	}

	c.kTable.BatchCommand(ktable.PutHash{ID: item.infoHash, Peers: hashPeers})

	peers := c.usablePeers(res.Values, c.cfg.PeersPerInfoHash)
	if len(peers) == 0 {
		return
	}

	send(ctx, c.forFetch, infoHashWithPeers{infoHash: item.infoHash, node: item.node, peers: peers})
}

// rememberNode records a node that has just answered a query.
func (c *Crawler) rememberNode(ctx context.Context, id protocol.ID, addr netip.AddrPort) {
	c.kTable.BatchCommand(ktable.PutNode{
		ID:      id,
		Addr:    addr,
		Options: []ktable.NodeOption{ktable.NodeResponded()},
	})
}

// metadata fetches the torrent metadata from the peers found by getPeers and
// queues the resulting row for persistence.
func (c *Crawler) metadata(ctx context.Context, job infoHashWithPeers) {
	// One budget covers every peer attempt and the scrape, so a hash with a
	// long list of dead peers cannot hold a worker indefinitely.
	fetchCtx, cancel := context.WithTimeout(ctx, c.cfg.MetadataTimeout)
	defer cancel()

	info, err := c.fetchMetainfo(fetchCtx, job)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Debug("failed to fetch metadata", "info_hash", job.infoHash.String(), "error", err)
		}

		return
	}

	seeders, leechers := c.scrapePeers(fetchCtx, job.node, job.infoHash)
	files := filesFromInfo(info)
	class := c.classifier.Classify(info.BestName(), files)

	switch class.Drop {
	case classify.DropBanned:
		c.stats.infohashesBlocked.Add(1)
		c.recordRejection(job.infoHash, info.BestName(), info.TotalLength(), class.Drop)
		c.blockInfohash(ctx, job.infoHash, errBannedContent)

		return
	case classify.DropDeceptive:
		c.stats.infohashesDeceptive.Add(1)
		c.recordRejection(job.infoHash, info.BestName(), info.TotalLength(), class.Drop)
		c.blockInfohash(ctx, job.infoHash, errDeceptiveContent)

		return
	case classify.DropAdult:
		c.stats.torrentsDropped.Add(1)
		c.recordRejection(job.infoHash, info.BestName(), info.TotalLength(), class.Drop)
		c.logger.Debug("dropped adult torrent", "info_hash", job.infoHash.String())

		return
	case classify.DropScript:
		c.stats.torrentsDroppedScript.Add(1)
		c.recordRejection(job.infoHash, info.BestName(), info.TotalLength(), class.Drop)
		c.logger.Debug("dropped foreign-script torrent", "info_hash", job.infoHash.String())

		return
	}

	row := rowFromInfo(job.infoHash, info, files, class, seeders, leechers, time.Now())

	if c.tmdb != nil && tmdbCandidate(class) {
		send(ctx, c.forTMDB, tmdbJob{row: row, name: info.BestName()})

		return
	}

	send(ctx, c.forPersist, row)
}

// fetchMetainfo asks the peers in turn for the metadata. A torrent that fails
// the banning checks is recorded in the blocklist and reported as an error so
// no part of it is ever stored.
func (c *Crawler) fetchMetainfo(ctx context.Context, job infoHashWithPeers) (metainfo.Info, error) {
	for _, peer := range job.peers {
		res, err := c.requester.Request(ctx, job.infoHash, peer)
		if err != nil {
			c.peerFailed(peer)

			c.logger.Debug("metadata request failed",
				"info_hash", job.infoHash.String(), "peer", peer.String(), "error", err)

			continue
		}

		if banErr := c.banning.Check(res.Info); banErr != nil {
			c.stats.infohashesBlocked.Add(1)
			c.blockInfohash(ctx, job.infoHash, banErr)

			return metainfo.Info{}, banErr
		}

		c.stats.metadataFetched.Add(1)

		return res.Info, nil
	}

	c.stats.metadataFailed.Add(1)

	return metainfo.Info{}, errNoMetadata
}

// recordRejection keeps a rejected torrent for the overview's feed. The record
// is in memory only, so it costs this process a few hundred kilobytes at most
// and leaves no trace on disk.
func (c *Crawler) recordRejection(hash protocol.ID, name string, size int64, reason classify.Drop) {
	if c.cfg.Rejected == nil {
		return
	}

	c.cfg.Rejected.Add(drops.Event{
		InfoHash: hash,
		Name:     name,
		Size:     size,
		Reason:   reason,
		At:       time.Now(),
	})
}

// blockInfohash records a hash that must not be indexed. Blocking is rare, so
// it is written through immediately; the short budget keeps a slow database
// from holding a metadata worker, and detaching from the crawl context means a
// shutdown does not lose a decision that has already been made.
func (c *Crawler) blockInfohash(ctx context.Context, hash protocol.ID, reason error) {
	blockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if _, err := c.blocklist.Block(blockCtx, []protocol.ID{hash}); err != nil {
		c.logger.Error("failed to block infohash", "info_hash", hash.String(), "error", err)
	}

	c.logger.Debug("blocked infohash", "info_hash", hash.String(), "reason", reason)
}

// scrapePeers records the approximate seeders and leechers for a torrent. The
// scrape is best effort: a failure leaves both at zero and the torrent is still
// indexed.
func (c *Crawler) scrapePeers(ctx context.Context, node netip.AddrPort, hash protocol.ID) (int, int) {
	res, err := c.client.GetPeersScrape(ctx, node, hash)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Debug("failed to scrape peers", "info_hash", hash.String(), "error", err)
		}

		return 0, 0
	}

	c.rememberNode(ctx, res.ID, node)

	return int(res.BfSeeders.ApproximatedSize()), int(res.BfPeers.ApproximatedSize())
}

// usablePeers removes duplicate, invalid and recently dead addresses and keeps
// at most limit of them. The DHT frequently returns the same address several
// times, and the same unreachable peers turn up for many torrents: dialling
// them again costs a timeout on a metadata worker every time.
func (c *Crawler) usablePeers(addrs []netip.AddrPort, limit int) []netip.AddrPort {
	usable := make([]netip.AddrPort, 0, len(addrs))

	for _, addr := range addrs {
		if _, failed := c.failedPeers.Get(addr); !failed {
			usable = append(usable, addr)
		}
	}

	return uniquePeers(usable, limit)
}

// peerFailed remembers a peer that did not supply metadata, so it is skipped
// for a while. A peer that is merely restarting gets another chance when the
// entry expires.
func (c *Crawler) peerFailed(peer netip.AddrPort) {
	c.failedPeers.Add(peer, struct{}{})
}

// uniquePeers removes duplicate and invalid addresses and keeps at most limit
// of them.
func uniquePeers(addrs []netip.AddrPort, limit int) []netip.AddrPort {
	seen := make(map[netip.AddrPort]struct{}, len(addrs))
	peers := make([]netip.AddrPort, 0, min(len(addrs), limit))

	for _, addr := range addrs {
		if !addr.IsValid() {
			continue
		}

		if _, ok := seen[addr]; ok {
			continue
		}

		seen[addr] = struct{}{}
		peers = append(peers, addr)

		if len(peers) >= limit {
			break
		}
	}

	return peers
}
