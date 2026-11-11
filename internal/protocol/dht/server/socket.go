package server

import "net/netip"

type Socket interface {
	Open(localAddr netip.AddrPort) error
	Close() error
	Send(netip.AddrPort, []byte) error
	Receive([]byte) (int, netip.AddrPort, error)
	LocalAddr() (netip.AddrPort, error)
}

// NewSocket creates an unbound datagram socket for the address family of
// localAddr.
func NewSocket(localAddr netip.AddrPort) (Socket, error) {
	return newSocket(localAddr)
}
