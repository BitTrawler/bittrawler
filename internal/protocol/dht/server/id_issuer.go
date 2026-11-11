package server

import "crypto/rand"

// newTransactionID returns a random transaction ID. Upstream used a counter,
// which made an outstanding query's ID predictable to anyone who could observe
// one query, and a response only has to carry it to be matched.
func newTransactionID() string {
	var b [4]byte

	_, _ = rand.Read(b[:])

	return string(b[:])
}
