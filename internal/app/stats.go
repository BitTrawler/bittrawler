package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/dhtcrawler"
	"github.com/BitTrawler/bittrawler/internal/store"
)

const (
	// statsInterval is how often the live counters are refreshed.
	statsInterval = 5 * time.Second
	// statsFullInterval is how often the stored row count, shard table and
	// categories are recounted. Counting rows means scanning the shards, so it
	// is slower than reading the crawler's atomics.
	statsFullInterval = 60 * time.Second
	// rateWindow is how far back the displayed ingest rate is measured.
	rateWindow = time.Minute
	// historyWindow is how much sample history the sparkline shows.
	historyWindow = 15 * time.Minute
	// sparklineBars is the number of bars the activity chart draws.
	sparklineBars = 60
)

// statsSnapshot is what the interface reports about the running instance.
type statsSnapshot struct {
	Updated       time.Time
	Counters      dhtcrawler.Stats
	RatePerMinute float64
	History       []rateSample
	Nodes         int
	Hashes        int
	StoreRows     int
	StoreBytes    int64
	BytesPerRow   float64
	Shards        []store.ShardStats
	Categories    map[int]int
	Months        []string
	HeapAlloc     uint64
	Goroutines    int
	RSSBytes      uint64
	CPUPercent    float64
}

// rateSample is one reading of the counters, kept for the rates and the
// activity chart.
type rateSample struct {
	at        time.Time
	persisted uint64
	triaged   uint64
	metadata  uint64
}

// statsCache holds the latest snapshot and the history behind the rates.
type statsCache struct {
	mu            sync.RWMutex
	snapshot      statsSnapshot
	history       []rateSample
	lastPersisted uint64
	lastFull      time.Time
	haveFull      bool
}

// runStats refreshes the counters until the context is cancelled.
func (a *App) runStats(ctx context.Context) {
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()

	a.refreshStats(ctx, true)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refreshStats(ctx, false)
		}
	}
}

// refreshStats takes a new snapshot. The crawler counters and the process
// readings are cheap; the stored row count is a shard scan, so it runs on a
// slower interval.
func (a *App) refreshStats(ctx context.Context, full bool) {
	now := time.Now()
	snapshot := statsSnapshot{
		Updated:    now,
		Nodes:      a.table.NodeCount(),
		Hashes:     a.table.HashCount(),
		Goroutines: runtime.NumGoroutine(),
	}

	if crawler := a.crawler.Load(); crawler != nil {
		snapshot.Counters = crawler.Stats()
	}

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	snapshot.HeapAlloc = memory.HeapAlloc
	snapshot.RSSBytes, snapshot.CPUPercent = processUsage(now.Sub(a.started))

	a.stats.mu.Lock()
	persisted := snapshot.Counters.TorrentsPersisted

	if persisted < a.stats.lastPersisted {
		a.stats.history = nil
	}

	a.stats.lastPersisted = persisted
	a.stats.history = append(a.stats.history, rateSample{
		at:        now,
		persisted: persisted,
		triaged:   snapshot.Counters.HashesTriaged,
		metadata:  snapshot.Counters.MetadataFetched,
	})

	cutoff := now.Add(-historyWindow)
	for len(a.stats.history) > 2 && a.stats.history[0].at.Before(cutoff) {
		a.stats.history = a.stats.history[1:]
	}

	// The displayed rate uses the last minute; the sparkline uses the whole
	// retained history.
	base := a.stats.history[0]
	for _, sample := range a.stats.history {
		if !sample.at.Before(now.Add(-rateWindow)) {
			base = sample

			break
		}
	}

	if elapsed := now.Sub(base.at).Minutes(); elapsed > 0 {
		snapshot.RatePerMinute = (float64(persisted) - float64(base.persisted)) / elapsed
	}

	snapshot.History = append([]rateSample{}, a.stats.history...)
	snapshot.StoreRows = a.stats.snapshot.StoreRows
	snapshot.StoreBytes = a.stats.snapshot.StoreBytes
	snapshot.BytesPerRow = a.stats.snapshot.BytesPerRow
	snapshot.Shards = a.stats.snapshot.Shards
	snapshot.Categories = a.stats.snapshot.Categories

	// While the store looks empty a recount is cheap, and it catches rows that
	// arrived since the last full pass without waiting for the slow interval.
	doFull := full || !a.stats.haveFull || a.stats.snapshot.StoreRows == 0 ||
		now.Sub(a.stats.lastFull) >= statsFullInterval
	if doFull {
		a.stats.lastFull = now
		a.stats.haveFull = true
	}

	a.stats.mu.Unlock()

	months := a.torrentStore.ShardMonths()
	snapshot.Months = months
	snapshot.StoreBytes = shardBytes(a.dataDir, months)

	if doFull {
		stats, err := a.torrentStore.Stats(ctx)
		if err != nil {
			a.logger.Warn("count the store", "error", err)
		} else {
			snapshot.StoreRows = stats.Rows
			snapshot.StoreBytes = stats.DBBytes + stats.WALBytes
			snapshot.Shards = append([]store.ShardStats{}, stats.Shards...)
			snapshot.Categories = make(map[int]int, len(stats.Categories))

			for category, rows := range stats.Categories {
				snapshot.Categories[category] = rows
			}
		}
	}

	if snapshot.StoreRows > 0 {
		snapshot.BytesPerRow = float64(snapshot.StoreBytes) / float64(snapshot.StoreRows)
	} else if persisted > 0 {
		snapshot.BytesPerRow = float64(snapshot.StoreBytes) / float64(persisted)
	}

	a.stats.mu.Lock()
	a.stats.snapshot = snapshot
	a.stats.mu.Unlock()
}

// snapshot returns a copy of the latest snapshot.
func (a *App) snapshot() statsSnapshot {
	a.stats.mu.RLock()
	defer a.stats.mu.RUnlock()

	snapshot := a.stats.snapshot
	snapshot.Months = append([]string{}, a.stats.snapshot.Months...)
	snapshot.Shards = append([]store.ShardStats{}, a.stats.snapshot.Shards...)
	snapshot.History = append([]rateSample{}, a.stats.snapshot.History...)

	if a.stats.snapshot.Categories != nil {
		categories := make(map[int]int, len(a.stats.snapshot.Categories))
		for category, rows := range a.stats.snapshot.Categories {
			categories[category] = rows
		}

		snapshot.Categories = categories
	}

	return snapshot
}

// shardBytes returns the size of the shard files and their write-ahead logs.
func shardBytes(dir string, months []string) int64 {
	var total int64

	for _, month := range months {
		for _, suffix := range []string{"", "-wal"} {
			info, err := os.Stat(filepath.Join(dir, "torrents-"+month+".db"+suffix))
			if err == nil {
				total += info.Size()
			}
		}
	}

	return total
}
