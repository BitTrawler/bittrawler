//go:build unix

package main

import "golang.org/x/sys/unix"

// freeBytes returns the bytes available to the current user on the filesystem
// holding path.
func freeBytes(path string) (uint64, error) {
	var stat unix.Statfs_t

	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}

	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
