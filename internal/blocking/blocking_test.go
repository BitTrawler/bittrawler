package blocking

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func newBlocklist(t *testing.T) *Blocklist {
	t.Helper()

	b, err := Open(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = b.Close() })

	return b
}

func assertIDs(t *testing.T, got, want []protocol.ID) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d hashes, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hash %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestOpenRequiresDir(t *testing.T) {
	if _, err := Open(Config{}); err == nil {
		t.Fatal("open with an empty directory returned no error")
	}
}

func TestOpenCreatesDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")

	b, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = b.Close() })

	if _, err := os.Stat(filepath.Join(dir, dbFileName)); err != nil {
		t.Fatalf("database file: %v", err)
	}
}

func TestFilterEmptyInput(t *testing.T) {
	b := newBlocklist(t)

	// Closing the database proves that an empty filter does not query it.
	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for _, input := range [][]protocol.ID{nil, {}} {
		got, err := b.Filter(context.Background(), input)
		if err != nil {
			t.Fatalf("filter: %v", err)
		}

		if len(got) != 0 {
			t.Fatalf("got %v, want no hashes", got)
		}
	}
}

func TestBlockThenFilter(t *testing.T) {
	b := newBlocklist(t)
	ctx := context.Background()

	blocked := testID(1)
	kept := testID(2)

	n, err := b.Block(ctx, []protocol.ID{blocked})
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	if n != 1 {
		t.Fatalf("blocked %d hashes, want 1", n)
	}

	got, err := b.Filter(ctx, []protocol.ID{blocked, kept})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}

	assertIDs(t, got, []protocol.ID{kept})
}

func TestFilterPreservesOrderAndRemovesDuplicates(t *testing.T) {
	b := newBlocklist(t)
	ctx := context.Background()

	a, blocked, c := testID(10), testID(11), testID(12)

	if n, err := b.Block(ctx, []protocol.ID{blocked}); err != nil || n != 1 {
		t.Fatalf("block: n=%d err=%v", n, err)
	}

	got, err := b.Filter(ctx, []protocol.ID{a, blocked, c, a, blocked})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}

	assertIDs(t, got, []protocol.ID{a, c})
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	b, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	id := testID(20)

	n, err := b.Block(ctx, []protocol.ID{id})
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	if n != 1 {
		t.Fatalf("blocked %d hashes, want 1", n)
	}

	// blocked_at is stored as Unix seconds.
	var blockedAt int64
	if err := b.db.QueryRow("SELECT blocked_at FROM blocked_infohash WHERE info_hash = ?", id[:]).Scan(&blockedAt); err != nil {
		t.Fatalf("blocked_at: %v", err)
	}

	if age := time.Since(time.Unix(blockedAt, 0)); age < -time.Minute || age > time.Minute {
		t.Fatalf("blocked_at = %d, which is %v from now", blockedAt, age)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	got, err := reopened.Filter(ctx, []protocol.ID{id})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("blocked hash survived the reopen: %v", got)
	}
}

func TestBlockIsIdempotent(t *testing.T) {
	b := newBlocklist(t)
	ctx := context.Background()

	hashes := []protocol.ID{testID(30), testID(31)}

	n, err := b.Block(ctx, hashes)
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	if n != len(hashes) {
		t.Fatalf("blocked %d hashes, want %d", n, len(hashes))
	}

	n, err = b.Block(ctx, hashes)
	if err != nil {
		t.Fatalf("block again: %v", err)
	}

	if n != 0 {
		t.Fatalf("second block recorded %d hashes, want 0", n)
	}
}

func TestChunking(t *testing.T) {
	b := newBlocklist(t)
	ctx := context.Background()

	// 1203 hashes span three parameter chunks at the 500-hash boundary.
	const count = 1203

	hashes := make([]protocol.ID, count)

	for i := range hashes {
		hashes[i] = testID(i)
	}

	evens := make([]protocol.ID, 0, count/2+1)

	for i := 0; i < count; i += 2 {
		evens = append(evens, hashes[i])
	}

	n, err := b.Block(ctx, evens)
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	if n != len(evens) {
		t.Fatalf("blocked %d hashes, want %d", n, len(evens))
	}

	// A second block of the same chunked input records nothing.
	n, err = b.Block(ctx, evens)
	if err != nil {
		t.Fatalf("block again: %v", err)
	}

	if n != 0 {
		t.Fatalf("second block recorded %d hashes, want 0", n)
	}

	// The filter runs over more than one chunk and keeps only the odd hashes,
	// in order, with the repeated input deduplicated.
	input := make([]protocol.ID, 0, count*2)
	input = append(input, hashes...)
	input = append(input, hashes...)

	got, err := b.Filter(ctx, input)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}

	want := make([]protocol.ID, 0, count-count/2)

	for i := 1; i < count; i += 2 {
		want = append(want, hashes[i])
	}

	assertIDs(t, got, want)
}

func TestConcurrentBlock(t *testing.T) {
	b := newBlocklist(t)
	ctx := context.Background()

	const goroutines = 8
	const perGoroutine = 100

	hashes := make([]protocol.ID, 0, goroutines*perGoroutine)

	for i := 0; i < goroutines*perGoroutine; i++ {
		hashes = append(hashes, testID(i))
	}

	var wg sync.WaitGroup

	errs := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if _, err := b.Block(ctx, hashes[g*perGoroutine:(g+1)*perGoroutine]); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent block: %v", err)
	}

	got, err := b.Filter(ctx, hashes)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("filter returned %d blocked hashes", len(got))
	}
}
