package app

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/reclassify"
	"github.com/BitTrawler/bittrawler/internal/web"
)

// reclassifyState is the maintenance pass's shared state.
type reclassifyState struct {
	mu       sync.Mutex
	running  bool
	apply    bool
	progress reclassify.Progress
	started  time.Time
	finished time.Time
	failure  string
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
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", "The form could not be read."), http.StatusBadRequest)

		return
	}

	if !a.startReclassify(r.PostFormValue("action") == "apply") {
		http.Redirect(w, r, "/settings?reclassify=busy", http.StatusSeeOther)

		return
	}

	http.Redirect(w, r, "/settings?reclassify=started", http.StatusSeeOther)
}

// startReclassify launches a pass, or reports that one is already running. The
// pass runs on the application's context, so stopping the process ends it
// before the store closes.
func (a *App) startReclassify(apply bool) bool {
	a.reclassify.mu.Lock()

	if a.reclassify.running {
		a.reclassify.mu.Unlock()

		return false
	}

	a.reclassify.running = true
	a.reclassify.apply = apply
	a.reclassify.progress = reclassify.Progress{}
	a.reclassify.started = time.Now()
	a.reclassify.finished = time.Time{}
	a.reclassify.failure = ""
	a.reclassify.mu.Unlock()

	a.wg.Add(1)

	go func() {
		defer a.wg.Done()

		progress, err := reclassify.Run(a.ctx, a.torrentStore, classify.New(classify.Config{}),
			reclassify.Options{Apply: apply}, func(p reclassify.Progress) {
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
		Running: a.reclassify.running,
		Apply:   a.reclassify.apply,
		Scanned: progress.Scanned,
		Matched: progress.Matched,
		Changed: progress.Changed,
		Written: progress.Written,
		Failure: a.reclassify.failure,
	}

	if !a.reclassify.finished.IsZero() {
		status.Finished = humanAge(time.Now(), a.reclassify.finished)
	}

	status.Transitions = reclassifyTransitions(progress.ByTransition)
	status.Samples = reclassifySamples(progress.Samples)

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

	for _, key := range keys[:min(len(keys), 8)] {
		out = append(out, fmt.Sprintf("%s: %d", key, byChange[key]))
	}

	return out
}

// reclassifySamples formats a few changed rows for the page.
func reclassifySamples(samples []reclassify.Sample) []string {
	out := make([]string, 0, min(len(samples), 5))

	for _, sample := range samples[:min(len(samples), 5)] {
		out = append(out, fmt.Sprintf("%s -> %s  %s", classify.CategoryName(sample.From), classify.CategoryName(sample.To), sample.Name))
	}

	return out
}
