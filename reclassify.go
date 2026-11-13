package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gofrs/flock"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/reclassify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// runReclassify re-runs the classifier over the stored rows and reports or
// applies the changed category, seasons, episodes and air date. It can only use
// what the store kept: the name, the file count and the primary file. A dry run
// is the default, and the pass needs the running instance stopped.
func runReclassify(args []string) int {
	fs := flag.NewFlagSet("reclassify", flag.ContinueOnError)
	flags := addCommonFlags(fs)
	apply := fs.Bool("apply", false, "write the changes; without it this is a dry run")
	onlyCurrent := fs.Int("cat", 0, "only rows currently in this category; 0 means every row")
	limit := fs.Int("limit", 0, "scan at most this many rows; 0 means all of them")

	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	dataDir := config.DataDirectory(flags.overrides())

	if _, err := os.Stat(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "bittrawler: %s is not a data directory: %v\n", dataDir, err)

		return 1
	}

	lock := flock.New(filepath.Join(dataDir, ".lock"))

	locked, err := lock.TryLock()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bittrawler:", err)

		return 1
	}

	if !locked {
		fmt.Fprintln(os.Stderr, "bittrawler: the data directory is in use; stop the running instance first")

		return 1
	}

	defer func() { _ = lock.Unlock() }()

	torrents, err := store.Open(store.Config{Dir: dataDir})
	if err != nil {
		fmt.Fprintln(os.Stderr, "bittrawler:", err)

		return 1
	}

	defer torrents.Close()

	printed := 0

	progress, err := reclassify.Run(context.Background(), torrents, classify.New(classify.Config{}),
		reclassify.Options{OnlyCategory: *onlyCurrent, Limit: *limit, Apply: *apply},
		func(p reclassify.Progress) {
			for ; printed < len(p.Samples); printed++ {
				sample := p.Samples[printed]
				fmt.Printf("  %x  %s\n      %s -> %s\n", sample.InfoHash, sample.Name,
					classify.CategoryName(sample.From), classify.CategoryName(sample.To))
			}

			if !p.Done && p.Scanned%10000 == 0 {
				fmt.Fprintf(os.Stderr, "scanned %d rows, %d changed\n", p.Scanned, p.Changed)
			}
		})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bittrawler: wrote %d of %d changes: %v\n", progress.Written, progress.Changed, err)

		return 1
	}

	fmt.Printf("Scanned %d rows, %d in the selected category, %d changed.\n", progress.Scanned, progress.Matched, progress.Changed)

	if !*apply {
		fmt.Println("Dry run: nothing was written. Run again with -apply to write the changes.")

		return 0
	}

	fmt.Printf("Wrote %d rows.\n", progress.Written)

	printChanges(progress.ByTransition)

	return 0
}

// printChanges lists category transitions, biggest first.
func printChanges(byChange map[string]int) {
	if len(byChange) == 0 {
		return
	}

	keys := make([]string, 0, len(byChange))
	for key := range byChange {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		if byChange[keys[i]] != byChange[keys[j]] {
			return byChange[keys[i]] > byChange[keys[j]]
		}

		return keys[i] < keys[j]
	})

	fmt.Println("Changes by transition:")

	for _, key := range keys {
		fmt.Printf("  %5d  %s\n", byChange[key], key)
	}
}
