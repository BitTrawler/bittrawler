//go:build !windows

package server

import (
	"errors"
	"fmt"
	"net/netip"

	"golang.org/x/sys/unix"
)

func newSocket(localAddr netip.AddrPort) (Socket, error) {
	family := unix.AF_INET
	if localAddr.Addr().Is6() {
		family = unix.AF_INET6
	}

	fd, err := unix.Socket(family, unix.SOCK_DGRAM, 0)
	if err != nil {
		return nil, fmt.Errorf("create socket: %w", err)
	}

	unix.CloseOnExec(fd)

	return &socket{fd: fd}, nil
}

type socket struct {
	fd int
}

func (s *socket) Open(localAddr netip.AddrPort) error {
	sa, err := addrPortToSockaddr(localAddr)
	if err != nil {
		return err
	}

	return unix.Bind(s.fd, sa)
}

func (s *socket) Close() error {
	return unix.Close(s.fd)
}

func (s *socket) Send(remoteAddr netip.AddrPort, data []byte) error {
	sa, err := addrPortToSockaddr(remoteAddr)
	if err != nil {
		return err
	}

	return unix.Sendto(s.fd, data, 0, sa)
}

func (s *socket) Receive(data []byte) (int, netip.AddrPort, error) {
	n, sa, err := unix.Recvfrom(s.fd, data, 0)
	if err != nil {
		return n, netip.AddrPort{}, err
	}

	addr, err := sockaddrToAddrPort(sa)
	if err != nil {
		return n, netip.AddrPort{}, err
	}

	return n, addr, nil
}

func (s *socket) LocalAddr() (netip.AddrPort, error) {
	sa, err := unix.Getsockname(s.fd)
	if err != nil {
		return netip.AddrPort{}, err
	}

	return sockaddrToAddrPort(sa)
}

func addrPortToSockaddr(addr netip.AddrPort) (unix.Sockaddr, error) {
	if addr.Addr().Is4() {
		return &unix.SockaddrInet4{Addr: addr.Addr().As4(), Port: int(addr.Port())}, nil
	}

	if addr.Addr().Is6() {
		return &unix.SockaddrInet6{Addr: addr.Addr().As16(), Port: int(addr.Port())}, nil
	}

	return nil, errors.New("invalid address")
}

func sockaddrToAddrPort(addr unix.Sockaddr) (netip.AddrPort, error) {
	switch sa := addr.(type) {
	case *unix.SockaddrInet4:
		return netip.AddrPortFrom(netip.AddrFrom4(sa.Addr), uint16(sa.Port)), nil
	case *unix.SockaddrInet6:
		return netip.AddrPortFrom(netip.AddrFrom16(sa.Addr), uint16(sa.Port)), nil
	default:
		return netip.AddrPort{}, fmt.Errorf("unsupported sockaddr type: %T", addr)
	}
}
