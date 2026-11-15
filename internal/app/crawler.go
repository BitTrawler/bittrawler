package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/dhtcrawler"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/client"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/banning"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/metainforequester"
)

// SetCrawl starts or stops crawling. A stopped crawl keeps serving Torznab and
// the web interface over the data already on disk.
func (a *App) SetCrawl(ctx context.Context, enabled bool) error {
	a.crawlMu.Lock()
	running := a.crawlRunning
	a.crawlMu.Unlock()

	if enabled == running {
		return nil
	}

	if enabled {
		return a.startCrawl()
	}

	return a.stopCrawl(ctx)
}

// startCrawl starts a crawler and returns without waiting for it.
func (a *App) startCrawl() error {
	a.crawlMu.Lock()
	defer a.crawlMu.Unlock()

	if a.crawlRunning {
		return nil
	}

	if a.ctx == nil {
		return errors.New("app: start the application before crawling")
	}

	crawler, err := dhtcrawler.New(a.crawlerConfig())
	if err != nil {
		return fmt.Errorf("app: start crawler: %w", err)
	}

	a.crawler.Store(crawler)

	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})

	a.crawlRunning = true
	a.crawlCancel = cancel
	a.crawlDone = done
	a.crawlErr = nil

	go func() {
		err := crawler.Run(ctx)

		a.crawlMu.Lock()
		a.crawlRunning = false
		a.crawlCancel = nil
		a.crawlErr = err
		a.crawlMu.Unlock()

		close(done)

		if err != nil && !errors.Is(err, context.Canceled) {
			a.logger.Error("crawl stopped", "error", err)
		}
	}()

	a.logger.Info("crawl started")

	return nil
}

// stopCrawl cancels the crawl and waits for it to flush the batches it has in
// flight.
func (a *App) stopCrawl(ctx context.Context) error {
	a.crawlMu.Lock()

	if !a.crawlRunning {
		a.crawlMu.Unlock()

		return nil
	}

	cancel, done := a.crawlCancel, a.crawlDone

	a.crawlMu.Unlock()

	cancel()

	select {
	case <-done:
		a.logger.Info("crawl stopped")

		return nil
	case <-ctx.Done():
		return fmt.Errorf("app: crawler did not stop in time: %w", ctx.Err())
	}
}

// crawlerConfig builds the crawler's settings from the current configuration.
// The crawler is built fresh for every start, so a paused crawl picks up
// changed settings when it resumes.
func (a *App) crawlerConfig() dhtcrawler.Config {
	cfg := a.currentConfig()

	crawler := dhtcrawler.DefaultConfig()
	crawler.KTable = a.table
	crawler.DHT = client.New(a.nodeID, a.dht)
	crawler.Requester = metainforequester.New(
		metainforequester.NewDefaultConfig(),
		a.logger.With("component", "metainfo-requester"),
	)
	crawler.Banning = banning.New()
	crawler.Blocklist = a.blocklist
	crawler.Store = a.torrentStore
	crawler.State = a.crawlState
	crawler.Classification = classify.Config{
		DropAdult:   cfg.Classify.DropAdult,
		DropScripts: cfg.Classify.DropScripts,
	}
	if client := a.tmdbClient(); client != nil {
		crawler.TMDB = client
	}
	crawler.TMDBWorkers = cfg.TMDB.Workers
	crawler.TMDBTimeout = cfg.TMDB.Timeout
	crawler.DiscoveredNodes = a.discovered
	crawler.BootstrapNodes = cfg.Crawler.BootstrapNodes
	crawler.BootstrapReseedInterval = cfg.Crawler.BootstrapReseedInterval
	crawler.OldNodeAge = cfg.Crawler.OldNodeAge
	crawler.OldNodeCheckInterval = cfg.Crawler.OldNodeCheckInterval
	crawler.SampleInfoHashesOverride = cfg.Crawler.SampleInfoHashesOverride
	crawler.SeenHashesSize = uint(cfg.Crawler.SeenHashesSize)
	crawler.SeenHashesFalsePositiveRate = cfg.Crawler.SeenHashesFalsePositiveRate
	crawler.TriageBatchSize = cfg.Crawler.TriageBatchSize
	crawler.TriageBatchWait = cfg.Crawler.TriageBatchWait
	crawler.PersistBatchSize = cfg.Crawler.PersistBatchSize
	crawler.PersistBatchWait = cfg.Crawler.PersistBatchWait
	crawler.PeersPerInfoHash = cfg.Crawler.PeersPerInfoHash
	crawler.MetadataTimeout = cfg.Crawler.MetadataTimeout
	crawler.Workers = dhtcrawler.WorkerCounts{
		Ping:             cfg.Crawler.Workers.Ping,
		FindNode:         cfg.Crawler.Workers.FindNode,
		SampleInfoHashes: cfg.Crawler.Workers.SampleInfoHashes,
		GetPeers:         cfg.Crawler.Workers.GetPeers,
		Metadata:         cfg.Crawler.Workers.Metadata,
	}
	crawler.Logger = a.logger.With("component", "crawler")

	return crawler
}
