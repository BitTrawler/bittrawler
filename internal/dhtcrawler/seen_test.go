package dhtcrawler

import (
	"testing"

	"github.com/BitTrawler/bittrawler/internal/protocol"
)

// TestSeenHashesKeepsTwoGenerations covers the rotation: hashes are remembered
// for one window after the generation that holds them is retired, and forgotten
// once that generation is cleared two windows later.
func TestSeenHashesKeepsTwoGenerations(t *testing.T) {
	t.Parallel()

	// A capacity of four means two hashes per generation.
	seen := newSeenHashes(4, 0.001)

	if seen.perGen != 2 {
		t.Fatalf("perGen = %d, want 2", seen.perGen)
	}

	first := []protocol.ID{testID(1), testID(2)}

	for _, hash := range first {
		seen.add(hash)
	}

	// Adding the second hash filled the generation and rotated; the hashes are
	// still visible because the previous generation is consulted.
	for _, hash := range first {
		if !seen.seen(hash) {
			t.Fatalf("hash %x forgotten one rotation after it was seen", hash)
		}
	}

	second := []protocol.ID{testID(3), testID(4)}

	for _, hash := range second {
		seen.add(hash)
	}

	for _, hash := range first {
		if seen.seen(hash) {
			t.Fatalf("hash %x still seen two windows later", hash)
		}
	}

	for _, hash := range second {
		if !seen.seen(hash) {
			t.Fatalf("hash %x forgotten one rotation after it was seen", hash)
		}
	}
}

// TestSeenHashesTreatsOneAsAWindow checks the smallest configured size, which
// must still rotate and test without dividing by zero.
func TestSeenHashesTreatsOneAsAWindow(t *testing.T) {
	t.Parallel()

	seen := newSeenHashes(1, 0.001)

	if seen.perGen != 1 {
		t.Fatalf("perGen = %d, want 1", seen.perGen)
	}

	seen.add(testID(1))
	seen.add(testID(2))
	seen.add(testID(3))

	if seen.seen(testID(1)) {
		t.Fatalf("hash from the first window is still seen")
	}
}
