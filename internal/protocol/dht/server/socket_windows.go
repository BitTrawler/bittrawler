//go:build windows

package server

import (
	"errors"
	"fmt"
	"net/netip"
	"syscall"

	"golang.org/x/sys/windows"
)

func newSocket(localAddr netip.AddrPort) (Socket, error) {
	family := windows.AF_INET
	if localAddr.Addr().Is6() {
		family = windows.AF_INET6
	}

	fd, err := windows.Socket(family, windows.SOCK_DGRAM, 0)
	if err != nil {
		return nil, fmt.Errorf("create socket: %w", err)
	}

	return &socket{fd: fd}, nil
}

type socket struct {
	fd windows.Handle
}

func (s *socket) Open(localAddr netip.AddrPort) error {
	sa, err := addrPortToSockaddr(localAddr)
	if err != nil {
		return err
	}

	return windows.Bind(s.fd, sa)
}

func (s *socket) Close() error {
	return windows.Close(s.fd)
}

func (s *socket) Send(remoteAddr netip.AddrPort, data []byte) error {
	sa, err := addrPortToSockaddr(remoteAddr)
	if err != nil {
		return err
	}

	return windows.Sendto(s.fd, data, 0, sa)
}

func (s *socket) Receive(data []byte) (int, netip.AddrPort, error) {
	n, sa, err := windows.Recvfrom(s.fd, data, 0)
	if err != nil {
		if ignoreReadFromError(err) {
			return 0, netip.AddrPort{}, nil
		}

		return n, netip.AddrPort{}, err
	}

	addr, err := sockaddrToAddrPort(sa)
	if err != nil {
		return n, netip.AddrPort{}, err
	}

	return n, addr, nil
}

func (s *socket) LocalAddr() (netip.AddrPort, error) {
	sa, err := windows.Getsockname(s.fd)
	if err != nil {
		return netip.AddrPort{}, err
	}

	return sockaddrToAddrPort(sa)
}

// see https://github.com/bitmagnet-io/bitmagnet/pull/203 and https://github.com/anacrolix/dht/issues/16
func ignoreReadFromError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case windows.WSAENETRESET, windows.WSAECONNRESET, windows.WSAECONNABORTED,
			windows.WSAECONNREFUSED, windows.WSAENETUNREACH, windows.WSAETIMEDOUT:
			return true
		}
	}

	return false
}

func addrPortToSockaddr(addr netip.AddrPort) (windows.Sockaddr, error) {
	if addr.Addr().Is4() {
		return &windows.SockaddrInet4{Addr: addr.Addr().As4(), Port: int(addr.Port())}, nil
	}

	if addr.Addr().Is6() {
		return &windows.SockaddrInet6{Addr: addr.Addr().As16(), Port: int(addr.Port())}, nil
	}

	return nil, errors.New("invalid address")
}

func sockaddrToAddrPort(addr windows.Sockaddr) (netip.AddrPort, error) {
	switch sa := addr.(type) {
	case *windows.SockaddrInet4:
		return netip.AddrPortFrom(netip.AddrFrom4(sa.Addr), uint16(sa.Port)), nil
	case *windows.SockaddrInet6:
		return netip.AddrPortFrom(netip.AddrFrom16(sa.Addr), uint16(sa.Port)), nil
	default:
		return netip.AddrPort{}, fmt.Errorf("unsupported sockaddr type: %T", addr)
	}
}
