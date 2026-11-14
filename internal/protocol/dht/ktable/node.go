package ktable

import (
	"net/netip"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
)

type nodeKeyspace struct {
	keyspace[netip.AddrPort, NodeOption, Node, *node]
}

func (k *nodeKeyspace) getLastRespondedBefore(t time.Time) []Node {
	var peers []Node

	for _, it := range k.items {
		if it.lastRespondedAt.Before(t) {
			peers = append(peers, it.snapshot())
		}
	}

	return peers
}

func (k *nodeKeyspace) getCandidatesForSampleInfoHashes(n int) []*node {
	//nolint:prealloc
	var candidates []*node

	for _, it := range k.items {
		if !it.IsSampleInfoHashesCandidate() {
			continue
		}

		candidates = append(candidates, it)
		if len(candidates) == n {
			break
		}
	}

	return candidates
}

type Node interface {
	keyspaceItem
	Addr() netip.AddrPort
	Time() time.Time
	Dropped() bool
	IsSampleInfoHashesCandidate() bool
}

type nodeBase struct {
	id   ID
	addr netip.AddrPort
}

func NewNode(id ID, addr netip.AddrPort) Node {
	return nodeBase{
		id:   id,
		addr: addr,
	}
}

func (p nodeBase) ID() protocol.ID {
	return p.id
}

func (p nodeBase) Addr() netip.AddrPort {
	return p.addr
}

func (nodeBase) Time() time.Time {
	return time.Time{}
}

func (nodeBase) Dropped() bool {
	return false
}

func (nodeBase) IsSampleInfoHashesCandidate() bool {
	return true
}

type node struct {
	nodeBase
	discoveredAt    time.Time
	lastRespondedAt time.Time
	dropReason      error

	bep51Support             protocolSupport
	sampledNum               int
	lastDiscoveredNum        int
	totalNum                 int
	nextSampleInfoHashesTime time.Time
	sampleFailures           int

	reverseMap *reverseMap
}

// NodeSupport records whether a node has been seen answering BEP 51
// sample_infohashes queries.
type NodeSupport int

const (
	NodeSupportUnknown NodeSupport = iota
	NodeSupportYes
	NodeSupportNo
)

// NodeState is a routing table node as restored from or saved for an earlier
// run.
type NodeState struct {
	ID              ID
	Addr            netip.AddrPort
	Support         NodeSupport
	LastRespondedAt time.Time
	NextSampleAt    time.Time
	Sampled         int
	Total           int
}

type NodeOption interface {
	apply(*node)
}

type nodeOption struct {
	fn func(*node)
}

func (p nodeOption) apply(peer *node) {
	p.fn(peer)
}

type protocolSupport int

const (
	protocolSupportUnknown protocolSupport = iota
	protocolSupportYes
	protocolSupportNo
)

var _ keyspaceItemPrivate[netip.AddrPort, NodeOption, Node] = (*node)(nil)

func (n *node) update(addr netip.AddrPort) {
	if n.addr != addr {
		n.reverseMap.dropPeerIDForAddr(n.addr.Addr())
	}

	n.addr = addr
	n.reverseMap.putAddrPeerID(addr.Addr(), n.id)
}

func (n *node) apply(option NodeOption) {
	option.apply(n)
}

func (n *node) drop(reason error) {
	n.dropReason = reason
	n.reverseMap.dropPeerIDForAddr(n.addr.Addr())
}

func (n *node) public() Node {
	return n.snapshot()
}

// nodeSnapshot is an immutable view of a node taken while the table lock is
// held. Queries return snapshots rather than the live node so a caller reading a
// node cannot race with another goroutine updating it.
type nodeSnapshot struct {
	nodeBase
	lastRespondedAt          time.Time
	dropped                  bool
	bep51Support             protocolSupport
	nextSampleInfoHashesTime time.Time
}

func (n nodeSnapshot) Time() time.Time {
	return n.lastRespondedAt
}

func (n nodeSnapshot) Dropped() bool {
	return n.dropped
}

func (n nodeSnapshot) IsSampleInfoHashesCandidate() bool {
	now := time.Now()
	threshold := now.Add(-(5 * time.Second))

	return n.bep51Support != protocolSupportNo &&
		n.nextSampleInfoHashesTime.Before(now) &&
		n.lastRespondedAt.Before(threshold)
}

func (n *node) snapshot() nodeSnapshot {
	return nodeSnapshot{
		nodeBase:                 n.nodeBase,
		lastRespondedAt:          n.lastRespondedAt,
		dropped:                  n.dropReason != nil,
		bep51Support:             n.bep51Support,
		nextSampleInfoHashesTime: n.nextSampleInfoHashesTime,
	}
}

func (n *node) Time() time.Time {
	return n.lastRespondedAt
}

func (n *node) Dropped() bool {
	return n.dropReason != nil
}

func (n *node) IsSampleInfoHashesCandidate() bool {
	now := time.Now()
	threshold := now.Add(-(5 * time.Second))

	return n.bep51Support != protocolSupportNo &&
		n.nextSampleInfoHashesTime.Before(now) &&
		n.lastRespondedAt.Before(threshold)
}

func NodeResponded() NodeOption {
	return nodeOption{
		fn: func(n *node) {
			n.lastRespondedAt = time.Now()
		},
	}
}

func NodeBep51Support(supported bool) NodeOption {
	return nodeOption{
		fn: func(n *node) {
			s := protocolSupportNo
			if supported {
				s = protocolSupportYes
			}
			n.bep51Support = s
		},
	}
}

func NodeSampleInfoHashesRes(discoveredNum int, totalNum int, nextSampleTime time.Time) NodeOption {
	return nodeOption{
		fn: func(n *node) {
			n.sampledNum += discoveredNum
			n.lastDiscoveredNum = discoveredNum
			n.totalNum += totalNum
			n.sampleFailures = 0
			// a crude way of deprioritizing nodes that gave us no new samples:
			if discoveredNum == 0 {
				now := time.Now()
				if nextSampleTime.Before(now) {
					nextSampleTime = now
				}
				nextSampleTime = nextSampleTime.Add(5 * time.Minute)
			}
			n.nextSampleInfoHashesTime = nextSampleTime
		},
	}
}

// NodeSampleInfoHashesFailed records a failed sample attempt and schedules the
// node for another try. A failure is often a lost packet rather than a node
// that cannot answer BEP 51, so it must not disqualify the node for good.
func NodeSampleInfoHashesFailed() NodeOption {
	return nodeOption{
		fn: func(n *node) {
			n.sampleFailures++
			n.nextSampleInfoHashesTime = time.Now().Add(sampleFailureDelay(n.sampleFailures))
		},
	}
}

// sampleFailureDelay backs off over the first few failures and then holds at six
// hours, so a node that silently ignores sample_infohashes costs little.
func sampleFailureDelay(failures int) time.Duration {
	delay := time.Minute

	for range min(failures-1, 6) {
		delay *= 2
	}

	return min(delay, 6*time.Hour)
}

// NodeRestored applies state saved from an earlier run.
func NodeRestored(state NodeState) NodeOption {
	return nodeOption{
		fn: func(n *node) {
			n.bep51Support = state.Support.protocolSupport()
			n.lastRespondedAt = state.LastRespondedAt
			n.nextSampleInfoHashesTime = state.NextSampleAt
			n.sampledNum = state.Sampled
			n.totalNum = state.Total
		},
	}
}

func (s NodeSupport) protocolSupport() protocolSupport {
	switch s {
	case NodeSupportYes:
		return protocolSupportYes
	case NodeSupportNo:
		return protocolSupportNo
	default:
		return protocolSupportUnknown
	}
}

func nodeSupport(p protocolSupport) NodeSupport {
	switch p {
	case protocolSupportYes:
		return NodeSupportYes
	case protocolSupportNo:
		return NodeSupportNo
	default:
		return NodeSupportUnknown
	}
}
