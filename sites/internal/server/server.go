// Package server serves and drains an inherited HTTP listener.
package server

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DrainError reports the handler calls unfinished at the drain deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// observedConn separates a connection still waiting for its first bytes from
// one already carrying an incomplete request header.
type observedConn struct {
	net.Conn
	mu           sync.Mutex
	received     bool
	probe        time.Time
	readDeadline time.Time
}

func (c *observedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.mu.Lock()
		c.received = true
		if !c.probe.IsZero() {
			c.probe = time.Time{}
			_ = c.Conn.SetReadDeadline(c.readDeadline)
		}
		c.mu.Unlock()
	}
	return n, err
}

func (c *observedConn) SetReadDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = deadline
	if !c.probe.IsZero() && (deadline.IsZero() || c.probe.Before(deadline)) {
		deadline = c.probe
	}
	return c.Conn.SetReadDeadline(deadline)
}

func (c *observedConn) drainFirstRead() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.received && c.probe.IsZero() {
		// Let a read already queued by net/http collect bytes buffered before
		// cancellation; an empty connection then times out and is closed.
		c.probe = time.Now().Add(10 * time.Millisecond)
		_ = c.Conn.SetReadDeadline(c.probe)
	}
}

type observedListener struct{ net.Listener }

func (l observedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &observedConn{Conn: c}, nil
}

// Serve owns ln and serves h until failure or cancellation, then drains it.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var mu sync.Mutex
	requests := make(map[*http.Request]context.CancelFunc)
	connections := make(map[net.Conn]http.ConnState)
	draining := false
	changed := make(chan struct{}, 1)
	markChanged := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	closeRequestless := func() {
		mu.Lock()
		var idle []net.Conn
		var fresh []*observedConn
		if draining {
			for c, state := range connections {
				if state == http.StateNew {
					fresh = append(fresh, c.(*observedConn))
				}
			}
		}
		if draining && len(requests) == 0 {
			for c, state := range connections {
				if state == http.StateNew {
					idle = append(idle, c)
				}
			}
		}
		mu.Unlock()
		for _, c := range fresh {
			c.drainFirstRead()
		}
		for _, c := range idle {
			_ = c.Close()
		}
	}
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCtx, cancel := context.WithCancel(r.Context())
			mu.Lock()
			requests[r] = cancel
			mu.Unlock()
			defer func() { cancel(); mu.Lock(); delete(requests, r); mu.Unlock(); closeRequestless() }()
			h.ServeHTTP(w, r.WithContext(requestCtx))
		}),
		ConnState: func(c net.Conn, state http.ConnState) {
			mu.Lock()
			if state == http.StateClosed || state == http.StateHijacked {
				delete(connections, c)
			} else {
				connections[c] = state
			}
			closeIdle := draining && (state == http.StateIdle || (state == http.StateNew && len(requests) == 0))
			mu.Unlock()
			if closeIdle {
				_ = c.Close()
			}
			markChanged()
		},
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(observedListener{Listener: ln}) }()
	serveFinished := false
	select {
	case err := <-served:
		serveFinished = true
		if ctx.Err() == nil {
			_ = srv.Close()
			return err
		}
	case <-ctx.Done():
	}
	deadlineCtx, cancelDeadline := context.WithTimeout(context.Background(), drain)
	// The callback may retain its context after Serve returns. Its lifetime
	// therefore ends at the drain deadline, rather than at this return.
	context.AfterFunc(deadlineCtx, cancelDeadline)
	mu.Lock()
	draining = true
	idle := make([]net.Conn, 0, len(connections))
	for c, state := range connections {
		if state == http.StateIdle || (state == http.StateNew && len(requests) == 0) {
			idle = append(idle, c)
		}
	}
	mu.Unlock()
	for _, c := range idle {
		_ = c.Close()
	}
	// Closing only the listener permits an accepted partial header to finish
	// during the drain. http.Server.Shutdown would reject that request before
	// calling its handler.
	_ = ln.Close()
	if !serveFinished {
		<-served
	}
	var shutdownErr error
wait:
	for {
		closeRequestless()
		mu.Lock()
		empty := len(connections) == 0
		mu.Unlock()
		if empty {
			break
		}
		select {
		case <-changed:
		case <-deadlineCtx.Done():
			shutdownErr = deadlineCtx.Err()
			break wait
		}
	}
	mu.Lock()
	unfinished := len(requests)
	mu.Unlock()
	if stop != nil {
		stop(deadlineCtx)
	}
	if shutdownErr != nil {
		mu.Lock()
		for _, cancel := range requests {
			cancel()
		}
		mu.Unlock()
		_ = srv.Close()
		return &DrainError{Unfinished: unfinished}
	}
	return nil
}
