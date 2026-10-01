// Package server serves HTTP on an inherited listener and drains requests on stop.
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

// DrainError reports handlers still running at the drain deadline.
type DrainError struct {
	Unfinished int
}

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve owns ln, serving h until ctx is done or accepting connections fails.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error {
	state := &connections{states: make(map[net.Conn]http.ConnState), active: make(map[net.Conn]bool), changed: make(chan struct{}, 1)}
	srv := &http.Server{
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connectionKey{}, c)
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, _ := r.Context().Value(connectionKey{}).(net.Conn)
			state.mu.Lock()
			state.calls++
			state.active[c] = true
			state.mu.Unlock()
			defer state.finish(c)
			h.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
		ConnState:         state.update,
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		if ctx.Err() == nil {
			_ = srv.Close()
			state.closeAll()
			return err
		}
	case <-ctx.Done():
	}
	deadline := time.NewTimer(drain)
	defer deadline.Stop()
	_ = ln.Close()
	srv.SetKeepAlivesEnabled(false)
	state.stop()
	for {
		state.mu.Lock()
		remaining := len(state.states)
		calls := state.calls
		state.mu.Unlock()
		if remaining == 0 && calls == 0 {
			_ = srv.Close()
			state.closeAll()
			return nil
		}
		select {
		case <-state.changed:
		case <-deadline.C:
			state.mu.Lock()
			n := state.calls
			state.mu.Unlock()
			_ = srv.Close()
			state.closeAll()
			if n == 0 {
				return nil
			}
			return &DrainError{Unfinished: n}
		}
	}
}

type connections struct {
	mu       sync.Mutex
	states   map[net.Conn]http.ConnState
	active   map[net.Conn]bool
	calls    int
	stopping bool
	changed  chan struct{}
}

type connectionKey struct{}

func (s *connections) finish(c net.Conn) {
	s.mu.Lock()
	s.calls--
	delete(s.active, c)
	closeConn := s.stopping && s.states[c] == http.StateHijacked
	if closeConn {
		delete(s.states, c)
	}
	s.mu.Unlock()
	if closeConn {
		_ = c.Close()
	}
	s.notify()
}

func (s *connections) update(c net.Conn, state http.ConnState) {
	s.mu.Lock()
	if state == http.StateClosed {
		delete(s.states, c)
	} else {
		s.states[c] = state
	}
	closeConn := s.stopping && (state == http.StateNew || state == http.StateIdle)
	s.mu.Unlock()
	if closeConn {
		_ = c.Close()
	}
	s.notify()
}

func (s *connections) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *connections) stop() {
	s.mu.Lock()
	s.stopping = true
	var idle []net.Conn
	for c, state := range s.states {
		if state == http.StateNew || state == http.StateIdle || (state == http.StateHijacked && !s.active[c]) {
			idle = append(idle, c)
			if state == http.StateHijacked {
				delete(s.states, c)
			}
		}
	}
	s.mu.Unlock()
	for _, c := range idle {
		_ = c.Close()
	}
}

func (s *connections) closeAll() {
	s.mu.Lock()
	all := make([]net.Conn, 0, len(s.states))
	for c := range s.states {
		all = append(all, c)
	}
	s.mu.Unlock()
	for _, c := range all {
		_ = c.Close()
	}
}
