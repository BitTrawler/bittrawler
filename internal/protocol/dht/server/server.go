package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/concurrency"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/responder"
	"github.com/anacrolix/torrent/bencode"
)

// Server is a DHT node's UDP endpoint: it answers incoming queries through a
// responder and sends outgoing queries on the caller's behalf.
type Server interface {
	// Query sends a KRPC query and waits for the matching response.
	Query(ctx context.Context, addr netip.AddrPort, q string, args dht.MsgArgs) (dht.RecvMsg, error)
	// LocalAddr is the bound UDP address, useful when port 0 was requested.
	LocalAddr() netip.AddrPort
	// Done is closed when the read loop stops, whether from Close or a socket
	// error.
	Done() <-chan struct{}
	Close() error
}

type queryKey struct {
	addr netip.AddrPort
	id   string
}

type server struct {
	cfg       Config
	logger    *slog.Logger
	responder responder.Responder
	conn      *net.UDPConn
	localAddr netip.AddrPort

	mutex     sync.Mutex
	queries   map[queryKey]chan dht.RecvMsg
	queryRate concurrency.KeyedLimiter

	stopped   chan struct{}
	stopOnce  sync.Once
	closeOnce sync.Once
	closeErr  error
	wg        sync.WaitGroup
}

func (s *server) LocalAddr() netip.AddrPort {
	return s.localAddr
}

func (s *server) Done() <-chan struct{} {
	return s.stopped
}

// Close stops the read loop and closes the socket. It is safe to call more
// than once.
func (s *server) Close() error {
	s.stopOnce.Do(func() {
		close(s.stopped)
	})

	s.closeOnce.Do(func() {
		// net.UDPConn reads go through the netpoller, so Close unblocks the
		// read loop on every platform. A raw file descriptor blocked in
		// recvfrom is not woken by close on Linux.
		s.closeErr = s.conn.Close()
		s.wg.Wait()
	})

	return s.closeErr
}

func (s *server) read() {
	defer s.wg.Done()

	buffer := make([]byte, 65507)

	for {
		n, from, err := s.conn.ReadFromUDPAddrPort(buffer)
		if err != nil {
			if s.stoppedClosed() {
				return
			}

			// A UDP read error is not attacker-controlled content, but it must
			// not panic the process either. Stop serving and let the owner
			// restart or report it.
			s.logger.Error("dht socket read failed", "error", err)
			s.stopOnce.Do(func() { close(s.stopped) })

			return
		}

		if n == 0 {
			continue
		}

		var msg dht.Msg

		if err := bencode.Unmarshal(buffer[:n], &msg); err != nil {
			s.logger.Debug("could not unmarshal dht packet", "error", err)
			continue
		}

		recv := dht.RecvMsg{Msg: msg, From: unmapAddrPort(from)}

		switch msg.Y {
		case dht.YQuery:
			// Serialised on the read loop: a packet flood must not turn into a
			// goroutine flood, and answering is cheap.
			s.handleQuery(recv)
		case dht.YResponse, dht.YError:
			s.handleResponse(recv)
		}
	}
}

// unmapAddrPort strips the IPv4-mapped IPv6 form from an address. A dual-stack
// socket reports an IPv4 peer as ::ffff:a.b.c.d, which would never match the
// plain IPv4 address the outgoing query was sent to, so every response from an
// IPv4 node would be dropped.
func unmapAddrPort(addr netip.AddrPort) netip.AddrPort {
	if addr.Addr().Is4In6() {
		return netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())
	}

	return addr
}

func (s *server) stoppedClosed() bool {
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
}

func (s *server) handleQuery(msg dht.RecvMsg) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ResponderTimeout)
	defer cancel()

	res := dht.Msg{
		T: msg.Msg.T,
		Y: dht.YResponse,
	}

	ret, retErr := s.responder.Respond(ctx, msg)
	if retErr != nil {
		var dhtErr dht.Error
		if errors.As(retErr, &dhtErr) {
			res.E = &dhtErr
		} else {
			res.E = &dht.Error{Code: dht.ErrorCodeServerError, Msg: "server error"}
			s.logger.Error("dht responder failed", "query", msg.Msg.Q, "from", msg.From, "error", retErr)
		}
	} else {
		res.R = &ret
	}

	if err := s.send(msg.From, res); err != nil {
		s.logger.Debug("could not send dht response", "query", msg.Msg.Q, "from", msg.From, "error", err)
	}
}

// handleResponse matches a response to an outstanding query by transaction ID
// and source address. Matching on the transaction ID alone let any host answer
// a query it had merely observed.
func (s *server) handleResponse(msg dht.RecvMsg) {
	key := queryKey{addr: msg.From, id: msg.Msg.T}

	s.mutex.Lock()
	ch, ok := s.queries[key]
	s.mutex.Unlock()

	if !ok {
		return
	}

	// A second response to the same query is dropped rather than blocking this
	// goroutine forever, which is what `ch <- msg` did upstream.
	select {
	case ch <- msg:
	default:
	}
}

func (s *server) Query(
	ctx context.Context,
	addr netip.AddrPort,
	q string,
	args dht.MsgArgs,
) (dht.RecvMsg, error) {
	if err := s.queryRate.Wait(ctx, addr.Addr().String()); err != nil {
		return dht.RecvMsg{}, err
	}

	id := newTransactionID()
	ch := make(chan dht.RecvMsg, 1)
	key := queryKey{addr: addr, id: id}

	s.mutex.Lock()
	s.queries[key] = ch
	s.mutex.Unlock()

	defer func() {
		s.mutex.Lock()
		delete(s.queries, key)
		s.mutex.Unlock()
	}()

	if err := s.send(addr, dht.Msg{Q: q, T: id, A: &args, Y: dht.YQuery}); err != nil {
		return dht.RecvMsg{}, fmt.Errorf("send query: %w", err)
	}

	timer := time.NewTimer(s.cfg.QueryTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return dht.RecvMsg{}, ctx.Err()
	case <-s.stopped:
		return dht.RecvMsg{}, errors.New("dht server stopped")
	case <-timer.C:
		return dht.RecvMsg{}, fmt.Errorf("dht query %q to %s timed out", q, addr)
	case res := <-ch:
		return res, responseErr(res)
	}
}

func responseErr(res dht.RecvMsg) error {
	if res.Msg.Y == dht.YError {
		if res.Msg.E != nil {
			return res.Msg.E
		}

		return errors.New("dht error response without error data")
	}

	if res.Msg.R == nil {
		return errors.New("dht response without return data")
	}

	return nil
}

func (s *server) send(addr netip.AddrPort, msg dht.Msg) error {
	data, err := bencode.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = s.conn.WriteToUDPAddrPort(data, addr)

	return err
}
