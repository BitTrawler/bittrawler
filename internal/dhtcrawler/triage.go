package dhtcrawler

import (
	"context"
	"net/netip"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
)

// startTriage starts the goroutine that batches discovered hashes.
func (c *Crawler) startTriage(ctx context.Context, wg *sync.WaitGroup) {
	startWorker(ctx, wg, c.runTriage)
}

// runTriage collects discovered hashes into batches and checks each batch
// before any crawl work is spent on it. Batching is what keeps the blocklist
// and store checks to one query per shard per few hundred hashes.
func (c *Crawler) runTriage(ctx context.Context) {
	batch := make([]hashAndNode, 0, c.cfg.TriageBatchSize)
	ticker := time.NewTicker(c.cfg.TriageBatchWait)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case item := <-c.forTriage:
			batch = append(batch, item)

			if len(batch) >= c.cfg.TriageBatchSize {
				c.triageBatch(ctx, batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				c.triageBatch(ctx, batch)
				batch = batch[:0]
			}
		}
	}
}

// triageBatch drops hashes that are blocked or already stored and queues the
// rest for a peer lookup. Every other outcome is a deliberate no: the store is
// the record of what has been crawled, and it is not re-examined.
func (c *Crawler) triageBatch(ctx context.Context, batch []hashAndNode) {
	byHash := make(map[protocol.ID]netip.AddrPort, len(batch))
	hashes := make([]protocol.ID, 0, len(batch))

	for _, item := range batch {
		if _, ok := byHash[item.infoHash]; ok {
			continue
		}

		byHash[item.infoHash] = item.node
		hashes = append(hashes, item.infoHash)
	}

	c.stats.hashesTriaged.Add(uint64(len(hashes)))

	allowed, err := c.blocklist.Filter(ctx, hashes)
	if err != nil {
		// A cancelled crawl closes the database under an in-flight batch; that is
		// orderly shutdown, not a failure worth logging.
		if ctx.Err() == nil {
			c.logger.Error("failed to filter blocked hashes", "count", len(hashes), "error", err)
		}

		return
	}

	if len(allowed) == 0 {
		return
	}

	keys := make([][20]byte, len(allowed))
	for i, hash := range allowed {
		keys[i] = hash
	}

	stored, err := c.store.SeenBatch(ctx, keys)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Error("failed to check stored hashes", "count", len(keys), "error", err)
		}

		return
	}

	for i, hash := range allowed {
		if stored[i] {
			continue
		}

		if !send(ctx, c.forPeers, hashAndNode{infoHash: hash, node: byHash[hash]}) {
			return
		}
	}
}
