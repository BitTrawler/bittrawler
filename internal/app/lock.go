package app

import (
	"fmt"
	"path/filepath"

	"github.com/gofrs/flock"
)

// lockDataDirectory takes the single-writer lock on the data directory, so a
// second process fails with a clear message instead of writing to the same
// databases.
func lockDataDirectory(dir string) (*flock.Flock, error) {
	lock := flock.New(filepath.Join(dir, ".lock"))

	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("app: lock %s: %w", dir, err)
	}

	if !locked {
		return nil, fmt.Errorf("app: data directory %s is in use by another process", dir)
	}

	return lock, nil
}
