//go:build unix

package app

import (
	"math"
	"runtime"
	"syscall"
	"time"
)

// processUsage returns the peak resident set size in bytes and the CPU
// percentage since the process started. The peak is the number a long crawl
// should be judged on; Getrusage avoids a platform-specific process query.
func processUsage(uptime time.Duration) (uint64, float64) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, 0
	}

	rss := uint64(usage.Maxrss)
	if runtime.GOOS != "darwin" {
		// Darwin reports bytes; Linux reports kibibytes.
		rss *= 1024
	}

	cpuSeconds := float64(usage.Utime.Sec+usage.Stime.Sec) +
		float64(usage.Utime.Usec+usage.Stime.Usec)/1e6

	percent := 0.0
	if uptime > 0 {
		percent = math.Round(cpuSeconds/uptime.Seconds()*1000) / 10
	}

	return rss, percent
}
