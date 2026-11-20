// Package drops keeps a short, in-memory record of the torrents the crawler
// rejected, so the overview can show what is being blocked and dropped while
// the crawl runs. Nothing is written to disk: the record is the last few
// hundred decisions of this process and starts empty after a restart.
package drops

import (
	"sync"
	"time"
	"unicode/utf8"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/protocol"
)

const (
	// DefaultCapacity is how many rejections a recorder keeps. It only has to
	// cover the largest list the interface can ask for with enough left over
	// for a reason filter to find matches in.
	DefaultCapacity = 200
	// maxNameRunes bounds the name kept for one rejection. A torrent name is
	// meant to be short but nothing enforces it, and metadata can carry a name
	// of tens of kilobytes; this is a monitor, not a record.
	maxNameRunes = 300
)

// Event is one torrent the crawler refused.
type Event struct {
	InfoHash protocol.ID
	Name     string
	Size     int64
	Reason   classify.Drop
	At       time.Time
}

// Recorder holds the most recent rejections. Metadata workers write to it while
// the overview reads from it, so every method is safe for concurrent use.
type Recorder struct {
	mutex  sync.Mutex
	events []Event
	// next is the slot the next event goes into, and count is how many of the
	// slots hold an event. Once the ring is full, next is also the oldest slot.
	next  int
	count int
}

// NewRecorder returns a recorder that keeps capacity events. A capacity of zero
// or less means DefaultCapacity.
func NewRecorder(capacity int) *Recorder {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}

	return &Recorder{events: make([]Event, capacity)}
}

// Add records one rejection. A name longer than the record keeps is cut, which
// only affects the feed: the torrent itself is not stored either way.
func (r *Recorder) Add(event Event) {
	if r == nil {
		return
	}

	event.Name = shorten(event.Name)

	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.events[r.next] = event
	r.next = (r.next + 1) % len(r.events)

	if r.count < len(r.events) {
		r.count++
	}
}

// Recent returns up to limit events, newest first, keeping only the reasons in
// reasons. An empty reasons keeps every reason.
func (r *Recorder) Recent(limit int, reasons []classify.Drop) []Event {
	if r == nil || limit <= 0 {
		return nil
	}

	allowed := make(map[classify.Drop]struct{}, len(reasons))
	for _, reason := range reasons {
		allowed[reason] = struct{}{}
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	events := make([]Event, 0, min(limit, r.count))

	for i := 0; i < r.count && len(events) < limit; i++ {
		// Walk backwards from the slot before next, which is the newest event.
		// The offset is never below -len(events), so adding the length makes it
		// a valid index without a second modulo.
		event := r.events[(r.next-1-i+len(r.events))%len(r.events)]

		if len(allowed) > 0 {
			if _, ok := allowed[event.Reason]; !ok {
				continue
			}
		}

		events = append(events, event)
	}

	return events
}

// shorten cuts a name to the length a rejection keeps.
func shorten(name string) string {
	if utf8.RuneCountInString(name) <= maxNameRunes {
		return name
	}

	return string([]rune(name)[:maxNameRunes]) + "…"
}
