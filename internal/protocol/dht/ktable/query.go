package ktable

import (
	"net/netip"
	"sort"
	"time"
)

type Query[T any] interface {
	execReturn(*table) T
}

var _ Query[[]Node] = GetClosestPeers{}

type GetClosestPeers struct {
	ID ID
}

func (c GetClosestPeers) execReturn(t *table) []Node {
	return t.nodes.getClosest(c.ID)
}

var _ Query[[]Node] = GetOldestPeers{}

type GetOldestPeers struct {
	Cutoff time.Time
	N      int
}

func (c GetOldestPeers) execReturn(t *table) []Node {
	peers := t.nodes.getLastRespondedBefore(c.Cutoff)
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].Time().Before(peers[j].Time())
	})

	if c.N > 0 && len(peers) > c.N {
		peers = peers[:c.N]
	}

	return peers
}

var _ Query[[]netip.Addr] = FilterKnownAddrs{}

type FilterKnownAddrs struct {
	Addrs []netip.Addr
}

func (c FilterKnownAddrs) execReturn(t *table) []netip.Addr {
	var unknown []netip.Addr

	for _, addr := range c.Addrs {
		if _, ok := t.addrs.addrs[addr.String()]; !ok {
			unknown = append(unknown, addr)
		}
	}

	return unknown
}

var _ Query[[]Node] = GetNodesForSampleInfoHashes{}

type GetNodesForSampleInfoHashes struct {
	N int
}

func (c GetNodesForSampleInfoHashes) execReturn(t *table) []Node {
	peers := make([]Node, 0, c.N)
	for _, p := range t.nodes.getCandidatesForSampleInfoHashes(c.N) {
		peers = append(peers, p.snapshot())
		if len(peers) >= c.N {
			break
		}
	}

	return peers
}

var _ Query[[]Node] = ClaimNodesForSampleInfoHashes{}

// ClaimNodesForSampleInfoHashes returns nodes that are due for a BEP 51 sample
// and marks each one as sampled until the lease expires. Without the claim the
// scheduler would queue every candidate again on its next tick, and a slow
// sample reply would let a queue of duplicates build that the workers never
// catch up with.
type ClaimNodesForSampleInfoHashes struct {
	N     int
	Lease time.Duration
}

func (c ClaimNodesForSampleInfoHashes) execReturn(t *table) []Node {
	now := time.Now()
	threshold := now.Add(-(5 * time.Second))

	candidates := make([]*node, 0, c.N)

	for _, it := range t.nodes.items {
		if it.bep51Support == protocolSupportNo ||
			!it.nextSampleInfoHashesTime.Before(now) ||
			!it.lastRespondedAt.Before(threshold) {
			continue
		}

		candidates = append(candidates, it)
	}

	// Nodes that supplied new hashes last time are asked again first, so a
	// crawl spends its sample budget where it has been productive.
	sort.SliceStable(candidates, func(i, j int) bool {
		productiveI := candidates[i].lastDiscoveredNum > 0
		productiveJ := candidates[j].lastDiscoveredNum > 0

		if productiveI != productiveJ {
			return productiveI
		}

		return candidates[i].sampledNum > candidates[j].sampledNum
	})

	if len(candidates) > c.N {
		candidates = candidates[:c.N]
	}

	claimed := make([]Node, len(candidates))

	for i, it := range candidates {
		it.nextSampleInfoHashesTime = now.Add(c.Lease)
		claimed[i] = it.snapshot()
	}

	return claimed
}

var _ Query[[]NodeState] = SnapshotNodes{}

// SnapshotNodes returns up to n nodes with their learning state, for saving
// across a restart.
type SnapshotNodes struct {
	N int
}

func (c SnapshotNodes) execReturn(t *table) []NodeState {
	states := make([]NodeState, 0, t.nodes.count())

	for _, it := range t.nodes.items {
		if it.dropReason != nil || it.id.IsZero() || !it.addr.IsValid() {
			continue
		}

		states = append(states, NodeState{
			ID:              it.id,
			Addr:            it.addr,
			Support:         nodeSupport(it.bep51Support),
			LastRespondedAt: it.lastRespondedAt,
			NextSampleAt:    it.nextSampleInfoHashesTime,
			Sampled:         it.sampledNum,
			Total:           it.totalNum,
		})
	}

	sort.Slice(states, func(i, j int) bool {
		if states[i].Sampled != states[j].Sampled {
			return states[i].Sampled > states[j].Sampled
		}

		return states[i].LastRespondedAt.After(states[j].LastRespondedAt)
	})

	if c.N > 0 && len(states) > c.N {
		states = states[:c.N]
	}

	return states
}

var _ Query[GetHashOrClosestNodesResult] = GetHashOrClosestNodes{}

type GetHashOrClosestNodes struct {
	ID ID
}

func (c GetHashOrClosestNodes) execReturn(t *table) GetHashOrClosestNodesResult {
	h, ok := t.hashes.get(c.ID)
	if ok {
		return GetHashOrClosestNodesResult{
			Hash:  h,
			Found: true,
		}
	}

	closestNodes := t.nodes.getClosest(c.ID)

	return GetHashOrClosestNodesResult{
		ClosestNodes: closestNodes,
	}
}

var _ Query[SampleHashesAndNodesResult] = SampleHashesAndNodes{}

type SampleHashesAndNodes struct{}

func (SampleHashesAndNodes) execReturn(t *table) SampleHashesAndNodesResult {
	nHashes := 20
	hashes := t.hashes.getRandom(nHashes)
	nNodes := 20 + (nHashes - len(hashes))
	nodes := t.nodes.getRandom(nNodes)
	totalHashes := t.hashes.count()

	return SampleHashesAndNodesResult{
		Hashes:      hashes,
		Nodes:       nodes,
		TotalHashes: totalHashes,
	}
}
