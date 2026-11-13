package app

import (
	"context"
	"time"
)

// retentionInterval is how often expired months are checked. A check with
// nothing due costs one glob and no database work.
const retentionInterval = time.Hour

// runRetention drops expired months at startup and then hourly.
func (a *App) runRetention(ctx context.Context) {
	a.dropExpired(ctx)

	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.dropExpired(ctx)
		}
	}
}

func (a *App) dropExpired(ctx context.Context) {
	dropped, err := a.torrentStore.DropExpired(ctx)
	if err != nil {
		a.logger.Error("retention check failed", "error", err)

		return
	}

	if len(dropped) > 0 {
		a.logger.Info("dropped expired months", "months", dropped)
	}
}
