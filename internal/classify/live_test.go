package classify

import (
	"sync"
	"testing"
)

func TestLiveAppliesANewFilter(t *testing.T) {
	t.Parallel()

	live := NewLive(Config{})

	if got := live.Classify("寄生兽 完全版", nil).Drop; got != DropNone {
		t.Fatalf("before the filter: drop = %v, want %v", got, DropNone)
	}

	live.Set(Config{DropScripts: []string{"cjk"}})

	if got := live.Classify("寄生兽 完全版", nil).Drop; got != DropScript {
		t.Fatalf("after the filter: drop = %v, want %v", got, DropScript)
	}

	// Clearing the filter puts the torrent back, which is the half of the
	// setting a restart used to be needed for.
	live.Set(Config{})

	if got := live.Classify("寄生兽 完全版", nil).Drop; got != DropNone {
		t.Fatalf("after clearing the filter: drop = %v, want %v", got, DropNone)
	}
}

func TestLiveDropAdultTracksTheSetting(t *testing.T) {
	t.Parallel()

	live := NewLive(Config{})

	if live.DropAdult() {
		t.Fatal("drop adult is on with the default configuration")
	}

	live.Set(Config{DropAdult: true})

	if !live.DropAdult() {
		t.Fatal("drop adult did not follow the new configuration")
	}
}

// TestLiveIsSafeToSwapWhileClassifying exercises the atomic swap: the race
// detector fails the build if the configuration is read without one.
func TestLiveIsSafeToSwapWhileClassifying(t *testing.T) {
	t.Parallel()

	live := NewLive(Config{})

	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				_ = live.Classify("寄生兽 完全版", nil)
				_ = live.DropAdult()
			}
		}()

		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				live.Set(Config{DropScripts: []string{"cjk"}, DropAdult: j%2 == 0})
			}
		}()
	}

	wg.Wait()
}
