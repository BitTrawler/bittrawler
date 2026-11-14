package crawlstate

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/protocol"
)

func testID(n int) protocol.ID {
	var id protocol.ID

	id[0] = 0xcc
	id[1] = byte(n >> 8)
	id[2] = byte(n)
	id[19] = 0xff

	return id
}

func testAddr(n int) netip.AddrPort {
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, byte(n >> 16), byte(n >> 8), byte(n)}), 6881)
}

func newStore(t *testing.T) *Store {
	t.Helper()

	store, err := Open(t.TempDir())
	require.NoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	return store
}

func loadIDs(t *testing.T, store *Store, limit int) []protocol.ID {
	t.Helper()

	nodes, err := store.Load(context.Background(), limit)
	require.NoError(t, err)

	ids := make([]protocol.ID, len(nodes))
	for i, node := range nodes {
		ids[i] = node.ID
	}

	return ids
}

// assertNode compares every field. Timestamps are compared as instants,
// because Load returns them in UTC.
func assertNode(t *testing.T, want, got Node) {
	t.Helper()

	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.Addr, got.Addr)
	assert.Equal(t, want.Support, got.Support)
	assert.True(t, want.LastRespondedAt.Equal(got.LastRespondedAt),
		"last responded at = %s, want %s", got.LastRespondedAt, want.LastRespondedAt)
	assert.True(t, want.NextSampleAt.Equal(got.NextSampleAt),
		"next sample at = %s, want %s", got.NextSampleAt, want.NextSampleAt)
	assert.Equal(t, want.Sampled, got.Sampled)
	assert.Equal(t, want.Total, got.Total)
	assert.True(t, want.UpdatedAt.Equal(got.UpdatedAt),
		"updated at = %s, want %s", got.UpdatedAt, want.UpdatedAt)
}

func TestOpenRequiresDir(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("open with an empty directory returned no error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	base := time.Date(2026, 11, 14, 12, 0, 0, 123456789, time.UTC)

	nodes := []Node{
		{
			ID:              testID(1),
			Addr:            netip.MustParseAddrPort("192.0.2.10:6881"),
			Support:         SupportYes,
			LastRespondedAt: base,
			NextSampleAt:    base.Add(30 * time.Minute),
			Sampled:         120,
			Total:           2400,
			UpdatedAt:       base.Add(time.Minute),
		},
		{
			ID:              testID(2),
			Addr:            netip.MustParseAddrPort("[2001:db8::2]:49000"),
			Support:         SupportNo,
			LastRespondedAt: base.Add(-time.Hour),
			NextSampleAt:    base.Add(6 * time.Hour),
			Sampled:         80,
			Total:           900,
			UpdatedAt:       base.Add(2 * time.Minute),
		},
		{
			// Never responded and never sampled: zero times must stay zero.
			ID:        testID(3),
			Addr:      netip.MustParseAddrPort("198.51.100.3:1"),
			Support:   SupportUnknown,
			Sampled:   7,
			Total:     7,
			UpdatedAt: base.Add(3 * time.Minute),
		},
	}

	store, err := Open(dir)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, nodes))
	require.NoError(t, store.Close())

	reopened, err := Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })

	loaded, err := reopened.Load(ctx, 0)
	require.NoError(t, err)
	require.Len(t, loaded, len(nodes))

	for i, want := range nodes {
		assertNode(t, want, loaded[i])
	}
}

func TestSaveReplacesExistingNode(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)

	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	first := Node{
		ID:              testID(1),
		Addr:            netip.MustParseAddrPort("192.0.2.1:6881"),
		Support:         SupportUnknown,
		LastRespondedAt: base,
		NextSampleAt:    base,
		Sampled:         1,
		Total:           1,
		UpdatedAt:       base,
	}
	second := Node{
		ID:              testID(1),
		Addr:            netip.MustParseAddrPort("192.0.2.1:6882"),
		Support:         SupportYes,
		LastRespondedAt: base.Add(time.Hour),
		NextSampleAt:    base.Add(2 * time.Hour),
		Sampled:         9,
		Total:           40,
		UpdatedAt:       base.Add(3 * time.Hour),
	}

	require.NoError(t, store.Save(ctx, []Node{first}))
	require.NoError(t, store.Save(ctx, []Node{second}))

	loaded, err := store.Load(ctx, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assertNode(t, second, loaded[0])
}

func TestLoadOrdersAndLimits(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)

	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	nodes := []Node{
		{ID: testID(1), Addr: testAddr(1), Sampled: 10, LastRespondedAt: base.Add(time.Hour)},
		{ID: testID(2), Addr: testAddr(2), Sampled: 20, LastRespondedAt: base},
		{ID: testID(3), Addr: testAddr(3), Sampled: 10, LastRespondedAt: base.Add(2 * time.Hour)},
		{ID: testID(4), Addr: testAddr(4), Sampled: 0},
		{ID: testID(5), Addr: testAddr(5), Sampled: 0, LastRespondedAt: base},
	}

	require.NoError(t, store.Save(ctx, nodes))

	assert.Equal(t, []protocol.ID{
		testID(2), // 20 samples
		testID(3), // 10 samples, responded later
		testID(1), // 10 samples, responded earlier
		testID(5), // never sampled, but responded
		testID(4), // never sampled and never responded
	}, loadIDs(t, store, 0))

	assert.Equal(t, []protocol.ID{testID(2), testID(3)}, loadIDs(t, store, 2))
	assert.Len(t, loadIDs(t, store, 100), len(nodes))
}

func TestSavePrunesAgedNodes(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)

	now := time.Now()

	nodes := []Node{
		{ID: testID(1), Addr: testAddr(1), Sampled: 5, UpdatedAt: now},
		{ID: testID(2), Addr: testAddr(2), Sampled: 4, UpdatedAt: now.Add(-8 * 24 * time.Hour)},
		// Just inside the seven days: a row saved six days ago is still useful.
		{ID: testID(3), Addr: testAddr(3), Sampled: 3, UpdatedAt: now.Add(-6 * 24 * time.Hour)},
	}

	require.NoError(t, store.Save(ctx, nodes))

	assert.Equal(t, []protocol.ID{testID(1), testID(3)}, loadIDs(t, store, 0))
}

func TestSavePrunesExcessNodes(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)

	nodes := make([]Node, maxNodes+1)
	now := time.Now()

	for i := range nodes {
		// Sampled rises with i, so the least productive node is the one Save
		// should push out of the cap.
		nodes[i] = Node{
			ID:        testID(i),
			Addr:      testAddr(i),
			Sampled:   i,
			Total:     i,
			UpdatedAt: now,
		}
	}

	require.NoError(t, store.Save(ctx, nodes))

	loaded, err := store.Load(ctx, 0)
	require.NoError(t, err)
	require.Len(t, loaded, maxNodes)

	// The sorted load puts the most productive first and the least productive
	// last, so these two rows prove both ends of the cap.
	assert.Equal(t, testID(maxNodes), loaded[0].ID)
	assert.Equal(t, testID(1), loaded[len(loaded)-1].ID)
}

func TestLoadEmptyStore(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)

	require.NoError(t, store.Save(ctx, nil))

	nodes, err := store.Load(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestLoadSkipsInvalidAddresses(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)

	valid := Node{
		ID:        testID(1),
		Addr:      testAddr(1),
		Support:   SupportYes,
		Sampled:   10,
		Total:     10,
		UpdatedAt: time.Now(),
	}
	zeroAddr := Node{ID: testID(2), Sampled: 99, Total: 99}
	zeroPort := Node{ID: testID(3), Addr: netip.MustParseAddrPort("192.0.2.3:0"), Sampled: 98, Total: 98}

	require.NoError(t, store.Save(ctx, []Node{valid, zeroAddr, zeroPort}))

	// A row written by an older version, or edited by hand, may hold text that
	// no longer parses. It ranks first by Sampled, so finding only the valid
	// node also shows that skipping does not stop the load.
	_, err := store.db.ExecContext(ctx,
		`INSERT INTO node (id, addr, support, last_responded_at, next_sample_at, sampled, total, updated_at)
		VALUES (?, ?, ?, NULL, NULL, ?, 0, ?)`,
		testID(4), "not-an-address", int64(SupportUnknown), 97, time.Now().UnixNano(),
	)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assertNode(t, valid, loaded[0])
}
