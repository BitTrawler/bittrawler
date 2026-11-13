//go:build !unix

package app

import "time"

// processUsage is not available on this platform.
func processUsage(time.Duration) (uint64, float64) {
	return 0, 0
}
