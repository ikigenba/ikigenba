package sim

import (
	"errors"
	"maps"
	"net"
	"os"
	"sync"
)

// hostListener merges a host's user sockets into the listener its daemon serves.
type hostListener struct {
	mu      sync.Mutex
	addr    net.UnixAddr
	closed  chan struct{}
	ready   chan *userSocket
	sockets map[string]*userSocket
	peak    int
}

type userSocket struct {
	mu       sync.Mutex
	listener *net.UnixListener
	info     os.FileInfo
	uid      int64
	closed   chan struct{}
	done     chan struct{}
	taken    chan struct{}
	pending  net.Conn
}

type boxConn struct {
	net.Conn
	uid int64
}

func newHostListener(dir string) *hostListener {
	return &hostListener{
		addr:   net.UnixAddr{Name: dir, Net: "unix"},
		closed: make(chan struct{}), ready: make(chan *userSocket),
	}
}

func boxUID(conn net.Conn) (int64, error) {
	if c, ok := conn.(*boxConn); ok {
		return c.uid, nil
	}
	return 0, errors.New("connection did not come from a simulated user socket")
}

func (h *hostListener) Addr() net.Addr { return &h.addr }

func (h *hostListener) Accept() (net.Conn, error) {
	for {
		select {
		case <-h.closed:
			return nil, net.ErrClosed
		case s := <-h.ready:
			// Share the close lock so a handoff cannot race with removal.
			h.mu.Lock()
			c := s.take()
			h.mu.Unlock()
			if c != nil {
				return &boxConn{Conn: c, uid: s.uid}, nil
			}
		}
	}
}

func (h *hostListener) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.closed:
		return net.ErrClosed
	default:
		close(h.closed)
	}
	for _, s := range h.sockets {
		s.close()
	}
	h.sockets = nil
	h.peak = 0
	return nil
}

func (h *hostListener) prune(want map[string]*boxUser) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for path, s := range h.sockets {
		u := want[path]
		if u == nil || u.box != h || u.uid != s.uid || s.stopped() || !s.sameSocket() {
			s.close()
			delete(h.sockets, path)
		}
	}
	switch n := len(h.sockets); {
	case n == 0:
		h.sockets = nil
		h.peak = 0
	case n <= h.peak/2:
		// Deletion alone keeps the map allocated at its former high-water mark.
		compact := make(map[string]*userSocket, n)
		maps.Copy(compact, h.sockets)
		h.sockets = compact
		h.peak = n
	}
}

func (h *hostListener) listening(path string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sockets[path] != nil
}

func (h *hostListener) listen(path string, uid int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.closed:
		return false
	default:
	}
	if h.sockets[path] != nil {
		return true
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		listener.SetUnlinkOnClose(false)
		_ = listener.Close()
		return false
	}
	s := &userSocket{
		listener: listener, info: info, uid: uid, closed: make(chan struct{}),
		done: make(chan struct{}), taken: make(chan struct{}, 1),
	}
	if h.sockets == nil {
		h.sockets = make(map[string]*userSocket)
	}
	h.sockets[path] = s
	h.peak = max(h.peak, len(h.sockets))
	go s.serve(h.ready)
	return true
}

func (s *userSocket) serve(ready chan<- *userSocket) {
	defer close(s.done)
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			return
		}
		s.mu.Lock()
		select {
		case <-s.closed:
			_ = conn.Close()
			s.mu.Unlock()
			return
		default:
			s.pending = conn
			s.mu.Unlock()
		}
		// One accepted connection per socket can wait for the host daemon.
		select {
		case ready <- s:
		case <-s.closed:
			return
		}
		select {
		case <-s.taken:
		case <-s.closed:
			return
		}
	}
}

func (s *userSocket) take() net.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.pending
	if c != nil {
		s.pending = nil
		s.taken <- struct{}{}
	}
	return c
}

func (s *userSocket) stopped() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *userSocket) sameSocket() bool {
	info, err := os.Lstat(s.listener.Addr().String())
	return err == nil && os.SameFile(s.info, info)
}

func (s *userSocket) close() {
	s.mu.Lock()
	close(s.closed)
	if s.pending != nil {
		_ = s.pending.Close()
		s.pending = nil
	}
	s.mu.Unlock()
	if !s.sameSocket() {
		s.listener.SetUnlinkOnClose(false)
	}
	_ = s.listener.Close()
	<-s.done
}
