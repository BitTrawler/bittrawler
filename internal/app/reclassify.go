package app

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/reclassify"
	"github.com/BitTrawler/bittrawler/internal/web"
)

// reclassifyState is the maintenance pass's shared state.
type reclassifyState struct {
	mu             sync.Mutex
	running        bool
	apply          bool
	deleteRejected bool
	progress       reclassify.Progress
	started        time.Time
	finished       time.Time
	failure        string
}

// handleReclassify starts a pass in the background. A dry run only reads; the
// apply pass writes the changed rows, and the search index follows through the
// update trigger.
func (a *App) handleReclassify(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	if err := r.ParseForm(); err != nil {
		a.renderSettings(w, a.buildSettingsData(r, a.currentConfig(), a.sourceSnapshot(), false, "", "The form could not be read."), http.StatusBadRequest)

		return
	}

	switch r.PostFormValue("action") {
	case "preview":
		if !a.startReclassify(false, false) {
			http.Redirect(w, r, "/settings?reclassify=busy#reclassify", http.StatusSeeOther)

			return
		}

		http.Redirect(w, r, "/settings?reclassify=started#reclassify", http.StatusSeeOther)
	case "apply":
		if !a.startReclassify(true, false) {
			http.Redirect(w, r, "/settings?reclassify=busy#reclassify", http.StatusSeeOther)

			return
		}

		http.Redirect(w, r, "/settings?reclassify=started#reclassify", http.StatusSeeOther)
	case "cleanup":
		if !a.startReclassify(false, true) {
			http.Redirect(w, r, "/settings?reclassify=busy#reclassify", http.StatusSeeOther)

			return
		}

		http.Redirect(w, r, "/settings?reclassify=cleanup#reclassify", http.StatusSeeOther)
	default:
		a.renderSettings(w, a.buildSettingsData(r, a.currentConfig(), a.sourceSnapshot(), false, "", "Unknown reclassification action."), http.StatusBadRequest)
	}
}

// startReclassify launches a pass, or reports that one is already running. The
// pass runs on the application's context, so stopping the process ends it
// before the store closes.
func (a *App) startReclassify(apply, deleteRejected bool) bool {
	a.reclassify.mu.Lock()

	if a.reclassify.running {
		a.reclassify.mu.Unlock()

		return false
	}

	total := 0
	if stats, err := a.torrentStore.Stats(context.Background()); err == nil {
		total = stats.Rows
	}

	a.reclassify.running = true
	a.reclassify.apply = apply
	a.reclassify.deleteRejected = deleteRejected
	a.reclassify.progress = reclassify.Progress{Total: total}
	a.reclassify.started = time.Now()
	a.reclassify.finished = time.Time{}
	a.reclassify.failure = ""
	a.reclassify.mu.Unlock()

	a.wg.Add(1)

	go func() {
		defer a.wg.Done()

		progress, err := reclassify.Run(a.ctx, a.torrentStore, classify.New(classify.Config{}),
			reclassify.Options{Apply: apply, DeleteRejected: deleteRejected}, func(p reclassify.Progress) {
				a.reclassify.mu.Lock()
				a.reclassify.progress = p
				a.reclassify.mu.Unlock()
			})

		a.reclassify.mu.Lock()
		defer a.reclassify.mu.Unlock()

		a.reclassify.running = false
		a.reclassify.progress = progress
		a.reclassify.finished = time.Now()

		if err != nil {
			a.reclassify.failure = err.Error()
		}
	}()

	return true
}

// settingsReclassify describes the pass to the settings page.
func (a *App) settingsReclassify() web.ReclassifyStatus {
	a.reclassify.mu.Lock()
	defer a.reclassify.mu.Unlock()

	progress := a.reclassify.progress

	status := web.ReclassifyStatus{
		Running:  a.reclassify.running,
		Apply:    a.reclassify.apply,
		Cleanup:  a.reclassify.deleteRejected,
		Scanned:  progress.Scanned,
		Total:    progress.Total,
		Percent:  reclassifyPercent(progress.Scanned, progress.Total),
		Matched:  progress.Matched,
		Changed:  progress.Changed,
		Written:  progress.Written,
		Deleted:  progress.Deleted,
		Rejected: reclassifyRejected(progress.Rejected),
		Failure:  a.reclassify.failure,
	}

	if !a.reclassify.finished.IsZero() {
		status.Finished = humanAge(time.Now(), a.reclassify.finished)
	}

	status.Transitions = reclassifyTransitions(progress.ByTransition)
	status.Samples, status.TokenUpdates = reclassifySamples(progress.Samples)

	return status
}

// reclassifyTransitions formats the category changes, biggest first.
func reclassifyTransitions(byChange map[string]int) []string {
	if len(byChange) == 0 {
		return nil
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

	out := make([]string, 0, min(len(keys), 8))

	for _, key := range keys {
		// A row can change its season or episode tokens without changing
		// category; "TV/HD -> TV/HD" says nothing, and those rows are listed
		// separately.
		from, to, ok := strings.Cut(key, " -> ")
		if ok && from == to {
			continue
		}

		out = append(out, fmt.Sprintf("%s: %d", key, byChange[key]))
	}

	if len(out) > 8 {
		out = out[:8]
	}

	return out
}

// reclassifySamples splits the changed rows into category changes and rows that
// kept their category but got new search tokens, which are different enough to
// list separately.
func reclassifySamples(samples []reclassify.Sample) ([]web.ReclassifyRow, []web.ReclassifyRow) {
	changes := make([]web.ReclassifyRow, 0, len(samples))
	tokens := make([]web.ReclassifyRow, 0)

	for _, sample := range samples {
		row := web.ReclassifyRow{
			Hash: fmt.Sprintf("%x", sample.InfoHash),
			Name: sample.Name,
			From: classify.CategoryName(sample.From),
			To:   classify.CategoryName(sample.To),
		}

		if sample.From == sample.To {
			tokens = append(tokens, row)

			continue
		}

		changes = append(changes, row)
	}

	return changes, tokens
}

// reclassifyRejected formats every rejected row for the page.
func reclassifyRejected(rejected []reclassify.Rejected) []web.ReclassifyRow {
	out := make([]web.ReclassifyRow, 0, len(rejected))

	for _, row := range rejected {
		out = append(out, web.ReclassifyRow{
			Hash:   fmt.Sprintf("%x", row.InfoHash),
			Name:   row.Name,
			From:   classify.CategoryName(row.From),
			Reason: row.Reason,
		})
	}

	return out
}

// reclassifyPercent is the completed share of the pass, for the progress bar.
func reclassifyPercent(scanned, total int) int {
	if total <= 0 {
		return 0
	}

	percent := scanned * 100 / total
	if percent > 100 {
		percent = 100
	}

	return percent
}
