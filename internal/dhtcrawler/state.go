package dhtcrawler

import (
	"context"
	"time"

	"github.com/BitTrawler/bittrawler/internal/crawlstate"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
)

const (
	// stateRestoreLimit bounds how many nodes are loaded into the routing table
	// when a crawl starts. The most productive come first, so the rest are the
	// long tail that can be discovered again cheaply.
	stateRestoreLimit = 20_000
	// stateSaveInterval is how often the routing table is written to crawl.db.
	stateSaveInterval = 30 * time.Second
	// stateSaveTimeout bounds one save so a slow disk cannot hold the crawl.
	stateSaveTimeout = 10 * time.Second
)

// StateStore keeps the routing table between runs, so a restart does not begin
// again at the bootstrap nodes.
type StateStore interface {
	Load(ctx context.Context, limit int) ([]crawlstate.Node, error)
	Save(ctx context.Context, nodes []crawlstate.Node) error
}

// restoreState loads the saved nodes into the routing table. Their sample
// timers are restored with them, so a node that was due for a sample before
// the restart is due again now and one that asked for a long interval keeps it.
func (c *Crawler) restoreState(ctx context.Context) {
	if c.state == nil {
		return
	}

	nodes, err := c.state.Load(ctx, stateRestoreLimit)
	if err != nil {
		c.logger.Warn("could not restore the routing table", "error", err)

		return
	}

	if len(nodes) == 0 {
		return
	}

	commands := make([]ktable.Command, 0, len(nodes))

	for _, node := range nodes {
		commands = append(commands, ktable.PutNode{
			ID:   node.ID,
			Addr: node.Addr,
			Options: []ktable.NodeOption{ktable.NodeRestored(ktable.NodeState{
				ID:              node.ID,
				Addr:            node.Addr,
				Support:         ktable.NodeSupport(node.Support),
				LastRespondedAt: node.LastRespondedAt,
				NextSampleAt:    node.NextSampleAt,
				Sampled:         node.Sampled,
				Total:           node.Total,
			})},
		})
	}

	c.kTable.BatchCommand(commands...)

	c.logger.Info("restored the routing table", "nodes", len(nodes))
}

// runStateSaver writes the routing table periodically and once more on
// shutdown.
func (c *Crawler) runStateSaver(ctx context.Context) {
	if c.state == nil {
		return
	}

	ticker := time.NewTicker(stateSaveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.saveState()

			return
		case <-ticker.C:
			c.saveState()
		}
	}
}

// saveState writes the current routing table. It runs on the crawler's own
// context without its cancellation, because the final save happens during
// shutdown and losing it would undo the run's learning.
func (c *Crawler) saveState() {
	states := c.kTable.SnapshotNodes(0)
	nodes := make([]crawlstate.Node, 0, len(states))

	for _, state := range states {
		nodes = append(nodes, crawlstate.Node{
			ID:              state.ID,
			Addr:            state.Addr,
			Support:         crawlstate.Support(state.Support),
			LastRespondedAt: state.LastRespondedAt,
			NextSampleAt:    state.NextSampleAt,
			Sampled:         state.Sampled,
			Total:           state.Total,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), stateSaveTimeout)
	defer cancel()

	if err := c.state.Save(ctx, nodes); err != nil {
		c.logger.Warn("could not save the routing table", "error", err)
	}
}
