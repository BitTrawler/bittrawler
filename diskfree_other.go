//go:build !unix

package main

import "errors"

// freeBytes is not available on this platform.
func freeBytes(string) (uint64, error) {
	return 0, errors.New("free space is not available on this platform")
}
