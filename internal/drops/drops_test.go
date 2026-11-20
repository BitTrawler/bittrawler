package drops

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
)

func event(n int, reason classify.Drop) Event {
	return Event{
		Name:   fmt.Sprintf("Torrent %d", n),
		Size:   int64(n),
		Reason: reason,
		At:     time.Unix(int64(n), 0),
	}
}

func TestRecentReturnsNewestFirst(t *testing.T) {
	t.Parallel()

	recorder := NewRecorder(10)

	for i := 1; i <= 3; i++ {
		recorder.Add(event(i, classify.DropBanned))
	}

	events := recorder.Recent(10, nil)

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	if events[0].Name != "Torrent 3" || events[2].Name != "Torrent 1" {
		t.Fatalf("events are not newest first: %v", events)
	}
}

func TestRecentKeepsOnlyTheRequestedReasons(t *testing.T) {
	t.Parallel()

	recorder := NewRecorder(10)

	recorder.Add(event(1, classify.DropBanned))
	recorder.Add(event(2, classify.DropAdult))
	recorder.Add(event(3, classify.DropScript))
	recorder.Add(event(4, classify.DropDeceptive))
	recorder.Add(event(5, classify.DropAdult))

	events := recorder.Recent(10, []classify.Drop{classify.DropAdult})

	if len(events) != 2 {
		t.Fatalf("got %d adult events, want 2", len(events))
	}

	if events[0].Name != "Torrent 5" {
		t.Fatalf("newest adult event = %q, want %q", events[0].Name, "Torrent 5")
	}

	// The limit counts matches, not events read: a filter must not be cut short
	// by the events it skipped.
	events = recorder.Recent(1, []classify.Drop{classify.DropAdult})

	if len(events) != 1 || events[0].Name != "Torrent 5" {
		t.Fatalf("limited filter = %v, want Torrent 5", events)
	}
}

func TestRecentWrapsAtCapacity(t *testing.T) {
	t.Parallel()

	recorder := NewRecorder(3)

	for i := 1; i <= 5; i++ {
		recorder.Add(event(i, classify.DropBanned))
	}

	events := recorder.Recent(10, nil)

	if len(events) != 3 {
		t.Fatalf("got %d events, want the 3 the recorder holds", len(events))
	}

	for i, want := range []string{"Torrent 5", "Torrent 4", "Torrent 3"} {
		if events[i].Name != want {
			t.Fatalf("event %d = %q, want %q", i, events[i].Name, want)
		}
	}
}

func TestRecentOnEmptyRecorder(t *testing.T) {
	t.Parallel()

	if events := NewRecorder(5).Recent(5, nil); len(events) != 0 {
		t.Fatalf("got %d events from an empty recorder", len(events))
	}
}

func TestLongNamesAreShortened(t *testing.T) {
	t.Parallel()

	recorder := NewRecorder(1)

	long := strings.Repeat("a", 500) + "蝙蝠侠"
	recorder.Add(Event{Name: long})

	events := recorder.Recent(1, nil)
	if got := len([]rune(events[0].Name)); got != 301 {
		t.Fatalf("name is %d runes, want 301", got)
	}

	if !strings.HasSuffix(events[0].Name, "…") {
		t.Fatalf("shortened name %q has no ellipsis", events[0].Name)
	}
}

func TestNilRecorderIsUsable(t *testing.T) {
	t.Parallel()

	var recorder *Recorder

	recorder.Add(event(1, classify.DropBanned))

	if events := recorder.Recent(5, nil); len(events) != 0 {
		t.Fatalf("got %d events from a nil recorder", len(events))
	}
}

// TestRecorderIsSafeForConcurrentUse exercises the lock: the race detector
// fails the build if the ring is read without it.
func TestRecorderIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	recorder := NewRecorder(20)

	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(2)

		go func(n int) {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				recorder.Add(event(n*100+j, classify.DropScript))
			}
		}(i)

		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				_ = recorder.Recent(5, []classify.Drop{classify.DropScript})
			}
		}()
	}

	wg.Wait()
}
