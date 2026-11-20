package classify

import "sync/atomic"

// Live is a classifier whose configuration can be replaced while it is in use.
// The crawler classifies torrents from many goroutines, and the drop rules can
// be changed in the interface at any time, so the current classifier is swapped
// atomically rather than rebuilt under a lock. A torrent is always classified
// by one configuration from start to finish.
type Live struct {
	current atomic.Pointer[Classifier]
}

// NewLive returns a live classifier with the given configuration.
func NewLive(cfg Config) *Live {
	live := &Live{}
	live.current.Store(New(cfg))

	return live
}

// Set replaces the configuration. Torrents already being classified finish
// under the old one.
func (l *Live) Set(cfg Config) {
	l.current.Store(New(cfg))
}

// Classify classifies one torrent with the configuration in force now.
func (l *Live) Classify(name string, files []File) Result {
	return l.current.Load().Classify(name, files)
}

// DropAdult reports whether adult torrents are dropped. The TMDB stage needs it
// after it has decided that a row is adult; it has no other use for the
// classifier's configuration.
func (l *Live) DropAdult() bool {
	return l.current.Load().cfg.DropAdult
}
