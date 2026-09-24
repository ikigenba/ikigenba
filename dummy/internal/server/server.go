package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type connectionKey struct{}

type gatedConn struct {
	net.Conn
	writeMu sync.Mutex
	gateMu  *sync.Mutex
	cutoff  atomic.Bool
}

func (c *gatedConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.gateMu.Lock()
	closed := c.cutoff.Load()
	c.gateMu.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	return c.Conn.Write(p)
}

type gatedListener struct {
	net.Listener
	gateMu *sync.Mutex
}

func (l gatedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &gatedConn{Conn: conn, gateMu: l.gateMu}, nil
}

// DrainError reports the number of requests still being handled when a drain ends.
type DrainError struct {
	Unfinished int
}

// Error describes the unfinished request count.
func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve serves h on ln until ctx is cancelled or the server fails.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error {
	if ctx.Err() != nil {
		_ = ln.Close()
		return nil
	}

	var connectionsMu sync.Mutex
	connections := make(map[net.Conn]http.ConnState)
	activeConnections := make(map[*gatedConn]int)
	active := 0
	handlerDone := make(chan struct{}, 1)
	stateChanged := make(chan struct{}, 1)
	var draining bool
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn := r.Context().Value(connectionKey{}).(*gatedConn)
			connectionsMu.Lock()
			active++
			activeConnections[conn]++
			connectionsMu.Unlock()
			defer func() {
				connectionsMu.Lock()
				active--
				activeConnections[conn]--
				if activeConnections[conn] == 0 {
					delete(activeConnections, conn)
				}
				if active == 0 {
					select {
					case handlerDone <- struct{}{}:
					default:
					}
				}
				connectionsMu.Unlock()
			}()
			h.ServeHTTP(w, r)
		}),
		ErrorLog:          log.New(io.Discard, "", 0),
		ReadHeaderTimeout: 5 * time.Second,
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			return context.WithValue(ctx, connectionKey{}, conn)
		},
		ConnState: func(conn net.Conn, state http.ConnState) {
			connectionsMu.Lock()
			closeNew := state == http.StateNew && draining
			if state == http.StateClosed || state == http.StateHijacked {
				delete(connections, conn)
			} else {
				connections[conn] = state
			}
			connectionsMu.Unlock()
			select {
			case stateChanged <- struct{}{}:
			default:
			}
			if closeNew {
				_ = conn.Close()
			}
		},
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- httpServer.Serve(gatedListener{Listener: ln, gateMu: &connectionsMu})
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		type cutoffState struct {
			unfinished int
			pending    []net.Conn
		}
		cutoff := make(chan cutoffState, 1)
		waitForDelivered := func(pending []net.Conn) {
			for {
				connectionsMu.Lock()
				waiting := false
				for _, conn := range pending {
					if connections[conn] == http.StateActive {
						waiting = true
						break
					}
				}
				connectionsMu.Unlock()
				if !waiting {
					return
				}
				<-stateChanged
			}
		}
		timer := time.AfterFunc(drain, func() {
			connectionsMu.Lock()
			state := cutoffState{unfinished: active}
			toClose := make([]*gatedConn, 0, len(activeConnections))
			if state.unfinished > 0 {
				for conn := range activeConnections {
					conn.cutoff.Store(true)
					toClose = append(toClose, conn)
				}
				for conn, status := range connections {
					if status == http.StateActive && activeConnections[conn.(*gatedConn)] == 0 {
						state.pending = append(state.pending, conn)
					}
				}
			}
			connectionsMu.Unlock()
			if state.unfinished > 0 {
				cancel()
				for _, conn := range toClose {
					_ = conn.SetWriteDeadline(time.Now())
					_ = conn.Close()
				}
			}
			cutoff <- state
		})
		_ = ln.Close()
		connectionsMu.Lock()
		draining = true
		var newConnections []net.Conn
		for conn, state := range connections {
			if state == http.StateNew {
				newConnections = append(newConnections, conn)
			}
		}
		connectionsMu.Unlock()
		for _, conn := range newConnections {
			_ = conn.Close()
		}
		shutdownResult := make(chan error, 1)
		go func() { shutdownResult <- httpServer.Shutdown(shutdownCtx) }()
		shutdownFinished := false
		var shutdownErr error
		for {
			connectionsMu.Lock()
			allHandlersDone := active == 0
			connectionsMu.Unlock()
			if shutdownFinished && allHandlersDone {
				if timer.Stop() {
					<-serveResult
					return shutdownErr
				}
				cut := <-cutoff
				<-serveResult
				if cut.unfinished > 0 {
					waitForDelivered(cut.pending)
					return &DrainError{Unfinished: cut.unfinished}
				}
				return shutdownErr
			}
			select {
			case cut := <-cutoff:
				if !shutdownFinished {
					shutdownErr = <-shutdownResult
				}
				<-serveResult
				if cut.unfinished > 0 {
					waitForDelivered(cut.pending)
					return &DrainError{Unfinished: cut.unfinished}
				}
				return shutdownErr
			case shutdownErr = <-shutdownResult:
				shutdownFinished = true
			case <-handlerDone:
			}
		}
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
			return nil
		}
		return err
	}
}
