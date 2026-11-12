package dhtcrawler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/client"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
)

// soughtNodeIDRotation is how often the random target for find_node and
// sample_infohashes changes. A fixed target would keep asking the same slice of
// the keyspace.
const soughtNodeIDRotation = 10 * time.Second

const (
	// nodeMaintenanceSchedule is how often the table is scanned for nodes that
	// are due a find_node or a sample_infohashes request.
	nodeMaintenanceSchedule = time.Second
	// findNodeCutoff is how long a node can go without a response before it is
	// asked for its closest nodes. It is deliberately longer than the five
	// second window the k-table uses to decide a node is ready for a BEP 51
	// sample: with the same cutoff the two schedulers would race for every node
	// and find_node would keep resetting the response time before a sample could
	// be taken.
	findNodeCutoff = 30 * time.Second
	// findNodeBatch bounds how many nodes one scan queues.
	findNodeBatch = 10
	// sampleInfoHashesBatch bounds how many nodes one scan queues for BEP 51.
	sampleInfoHashesBatch = 60
)

// startDiscovery starts the node side of the crawl: bootstrap, node intake and
// the per-node maintenance queries.
func (c *Crawler) startDiscovery(ctx context.Context, wg *sync.WaitGroup) {
	c.soughtNodeID.Set(protocol.RandomNodeID())

	startWorker(ctx, wg, c.runSoughtNodeIDRotation)
	startWorker(ctx, wg, c.runBootstrap)
	startWorker(ctx, wg, c.runInboundNodes)
	startWorker(ctx, wg, c.runDiscoveredNodes)
	startWorker(ctx, wg, c.runPingScheduler)
	startPool(ctx, wg, c.cfg.Workers.Ping, c.forPing, c.pingNode)
	startWorker(ctx, wg, c.runFindNodeScheduler)
	startPool(ctx, wg, c.cfg.Workers.FindNode, c.forFind, c.findNode)
	startWorker(ctx, wg, c.runSampleInfoHashesScheduler)
	startPool(ctx, wg, c.cfg.Workers.SampleInfoHashes, c.forSample, c.sampleInfoHashes)
}

func (c *Crawler) runSoughtNodeIDRotation(ctx context.Context) {
	ticker := time.NewTicker(soughtNodeIDRotation)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.soughtNodeID.Set(protocol.RandomNodeID())
		}
	}
}

// runBootstrap queues the bootstrap nodes for a ping, which is how their node
// IDs are learnt. It repeats on the configured interval because a bootstrap
// address can change its ID or be replaced while the process runs.
func (c *Crawler) runBootstrap(ctx context.Context) {
	wait := time.Duration(0)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		c.resolveBootstrapNodes(ctx)

		wait = c.cfg.BootstrapReseedInterval
	}
}

func (c *Crawler) resolveBootstrapNodes(ctx context.Context) {
	for _, hostPort := range c.cfg.BootstrapNodes {
		host, portString, splitErr := net.SplitHostPort(hostPort)
		if splitErr != nil {
			c.logger.Warn("invalid bootstrap node address", "address", hostPort, "error", splitErr)
			continue
		}

		port, portErr := strconv.ParseUint(portString, 10, 16)
		if portErr != nil {
			c.logger.Warn("invalid bootstrap node port", "address", hostPort, "error", portErr)
			continue
		}

		addrs, lookupErr := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if lookupErr != nil {
			if ctx.Err() == nil {
				c.logger.Warn("failed to resolve bootstrap node", "address", hostPort, "error", lookupErr)
			}

			continue
		}

		for _, addr := range addrs {
			node := ktable.NewNode(ktable.ID{}, netip.AddrPortFrom(addr, uint16(port)))
			if !send(ctx, c.forPing, node) {
				return
			}
		}
	}
}

// runInboundNodes forwards nodes learnt from inbound DHT queries. Inbound
// traffic is dropped by the responder when its sink is full, so a slow crawl
// cannot stall the DHT server.
func (c *Crawler) runInboundNodes(ctx context.Context) {
	if c.inbound == nil {
		return
	}

	for {
		node, ok := recv(ctx, c.inbound)
		if !ok {
			return
		}

		if !send(ctx, c.discovered, node) {
			return
		}
	}
}

// runDiscoveredNodes sends every discovered node to exactly one of the
// maintenance pools, chosen among the pools that can accept it. Spreading the
// work stops every discovered node from taking the same route.
func (c *Crawler) runDiscoveredNodes(ctx context.Context) {
	for {
		node, ok := recv(ctx, c.discovered)
		if !ok {
			return
		}

		c.queueNode(ctx, node)
	}
}

func (c *Crawler) queueNode(ctx context.Context, node ktable.Node) {
	addr := node.Addr()
	if !addr.IsValid() {
		return
	}

	if len(c.kTable.FilterKnownAddrs([]netip.Addr{addr.Addr()})) == 0 {
		return
	}

	select {
	case c.forPing <- node:
	case c.forFind <- node:
	case c.forSample <- node:
	case <-ctx.Done():
	}
}

// runPingScheduler queues nodes that have not answered for oldNodeAge.
func (c *Crawler) runPingScheduler(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.OldNodeCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		for _, node := range c.kTable.GetOldestNodes(time.Now().Add(-c.cfg.OldNodeAge), 0) {
			if !send(ctx, c.forPing, node) {
				return
			}
		}
	}
}

// pingNode checks that a node is still alive, and drops it from the table when
// it is not.
func (c *Crawler) pingNode(ctx context.Context, node ktable.Node) {
	if node.Dropped() {
		return
	}

	res, err := c.client.Ping(ctx, node.Addr())
	nodeID := node.ID()

	if err == nil {
		if !nodeID.IsZero() && nodeID != res.ID {
			err = errors.New("node responded with a mismatching ID")
		} else {
			nodeID = res.ID
		}
	}

	if err != nil {
		c.dropUnansweredNode(ctx, nodeID, fmt.Errorf("ping: %w", err))

		return
	}

	c.kTable.BatchCommand(ktable.PutNode{
		ID:      nodeID,
		Addr:    node.Addr(),
		Options: []ktable.NodeOption{ktable.NodeResponded()},
	})
}

// runFindNodeScheduler queues nodes that have not been asked for their closest
// nodes recently. This is the main way the routing table grows beyond what
// inbound traffic and get_peers replies provide.
func (c *Crawler) runFindNodeScheduler(ctx context.Context) {
	ticker := time.NewTicker(nodeMaintenanceSchedule)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		for _, node := range c.kTable.GetOldestNodes(time.Now().Add(-findNodeCutoff), findNodeBatch) {
			if !send(ctx, c.forFind, node) {
				return
			}
		}
	}
}

func (c *Crawler) findNode(ctx context.Context, node ktable.Node) {
	if node.Dropped() {
		return
	}

	res, err := c.client.FindNode(ctx, node.Addr(), c.soughtNodeID.Get())
	if err != nil {
		c.dropUnansweredNode(ctx, node.ID(), fmt.Errorf("find_node: %w", err))

		return
	}

	c.kTable.BatchCommand(ktable.PutNode{
		ID:      node.ID(),
		Addr:    node.Addr(),
		Options: []ktable.NodeOption{ktable.NodeResponded()},
	})
	c.addDiscovered(ctx, res.Nodes)
}

// runSampleInfoHashesScheduler queues nodes that are due for a BEP 51 sample.
func (c *Crawler) runSampleInfoHashesScheduler(ctx context.Context) {
	ticker := time.NewTicker(nodeMaintenanceSchedule)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		for _, node := range c.kTable.GetNodesForSampleInfoHashes(sampleInfoHashesBatch) {
			if !send(ctx, c.forSample, node) {
				return
			}
		}
	}
}

// sampleInfoHashes asks one node for a sample of the infohashes it knows about
// and queues the ones this process has not seen before for triage.
func (c *Crawler) sampleInfoHashes(ctx context.Context, node ktable.Node) {
	if !node.IsSampleInfoHashesCandidate() {
		return
	}

	res, err := c.client.SampleInfoHashes(ctx, node.Addr(), c.soughtNodeID.Get())
	if err != nil {
		// A node that answers ping and find_node but not sample_infohashes is
		// alive and useful; the bootstrap routers are exactly that. Marking it
		// as unsupported stops the sample scheduler queuing it again without
		// losing a node the routing table needs.
		if ctx.Err() == nil {
			c.kTable.BatchCommand(ktable.PutNode{
				ID:      node.ID(),
				Addr:    node.Addr(),
				Options: []ktable.NodeOption{ktable.NodeBep51Support(false)},
			})
			c.logger.Debug("node does not answer sample_infohashes",
				"node_id", node.ID().String(), "address", node.Addr().String(), "error", err)
		}

		return
	}

	var discovered []protocol.ID

	for _, hash := range res.Samples {
		if !c.seenHashes.testAndAdd(hash) {
			discovered = append(discovered, hash)
		}
	}

	for _, hash := range discovered {
		if !send(ctx, c.forTriage, hashAndNode{infoHash: hash, node: node.Addr()}) {
			return
		}
	}

	c.kTable.BatchCommand(ktable.PutNode{
		ID:   node.ID(),
		Addr: node.Addr(),
		Options: []ktable.NodeOption{
			ktable.NodeResponded(),
			ktable.NodeBep51Support(true),
			ktable.NodeSampleInfoHashesRes(len(discovered), res.Num, time.Now().Add(c.sampleInterval(res.Interval, len(discovered) > 0))),
		},
	})
	c.addDiscovered(ctx, res.Nodes)
}

// sampleInterval works out when a node should be sampled again. The node's
// requested interval is honoured unless the reply contained new hashes and the
// operator has set an override; a productive node is worth asking more often. A
// node that asks for an interval shorter than the minimum is given the minimum
// so it is not hammered.
func (c *Crawler) sampleInterval(requestedSeconds int, productive bool) time.Duration {
	interval := time.Duration(requestedSeconds) * time.Second
	if interval < minSampleInfoHashesInterval {
		interval = minSampleInfoHashesInterval
	}

	if override := c.cfg.SampleInfoHashesOverride; override > 0 && productive && interval > override {
		interval = override
	}

	return interval
}

// addDiscovered queues nodes reported by a crawl response. It never blocks: the
// workers that report nodes are fed by the same pools that consume them, so
// blocking on a full queue would let the discovery pipeline deadlock against its
// own consumers. A full queue drops the nodes instead, and node discovery is
// cheap to repeat.
func (c *Crawler) addDiscovered(ctx context.Context, nodes []client.NodeInfo) {
	for _, node := range nodes {
		if !node.Addr.IsValid() {
			continue
		}

		select {
		case c.discovered <- ktable.NewNode(node.ID, node.Addr):
		case <-ctx.Done():
			return
		default:
			return
		}
	}
}

// dropUnansweredNode removes a node that did not answer. A cancellation of the
// crawl is not a node failure, so the node is left alone during shutdown.
func (c *Crawler) dropUnansweredNode(ctx context.Context, id protocol.ID, reason error) {
	if ctx.Err() != nil {
		return
	}

	c.logger.Debug("dropping unresponsive node", "node_id", id.String(), "error", reason)

	c.kTable.BatchCommand(ktable.DropNode{ID: id, Reason: reason})
}

// dropUnansweredAddr is dropUnansweredNode for queries whose failure leaves no
// node ID to drop by, such as get_peers.
func (c *Crawler) dropUnansweredAddr(ctx context.Context, addr netip.AddrPort, reason error) {
	if ctx.Err() != nil {
		return
	}

	c.logger.Debug("dropping unresponsive node", "address", addr.String(), "error", reason)

	c.kTable.BatchCommand(ktable.DropAddr{Addr: addr.Addr(), Reason: reason})
}
