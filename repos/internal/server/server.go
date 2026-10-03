// Package server serves an inherited listener and drains its requests on stop.
package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// DrainError reports handler calls still in progress at the drain deadline.
type DrainError struct {
	Unfinished int
}

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return fmt.Sprintf("stopped with %d requests unfinished", e.Unfinished)
}

type requests struct {
	mu       sync.Mutex
	active   int
	draining bool
	conns    map[net.Conn]http.ConnState
}

func (r *requests) connection(conn net.Conn, state http.ConnState) {
	r.mu.Lock()
	if state == http.StateClosed || state == http.StateHijacked {
		delete(r.conns, conn)
	} else {
		r.conns[conn] = state
	}
	closeConn := r.draining && (state == http.StateNew || state == http.StateIdle)
	r.mu.Unlock()
	if closeConn {
		_ = conn.Close()
	}
}

func (r *requests) beginDrain() {
	r.mu.Lock()
	r.draining = true
	var idle []net.Conn
	for conn, state := range r.conns {
		if state == http.StateNew || state == http.StateIdle {
			idle = append(idle, conn)
		}
	}
	r.mu.Unlock()
	for _, conn := range idle {
		_ = conn.Close()
	}
}

func (r *requests) unfinished() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

func (r *requests) handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.active++
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			r.active--
			r.mu.Unlock()
		}()
		h.ServeHTTP(w, req)
	})
}

// Serve answers HTTP requests on ln until ctx ends or accepting fails.
// The stop callback runs after graceful drain or at its deadline, before
// unfinished requests are cancelled and their connections are closed.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	tracked := &requests{conns: make(map[net.Conn]http.ConnState)}
	srv := &http.Server{
		Handler:           tracked.handler(h),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
		BaseContext:       func(net.Listener) context.Context { return requestCtx },
		ConnState:         tracked.connection,
	}
	served := make(chan error, 1)
	if ctx.Err() == nil {
		go func() { served <- srv.Serve(ln) }()
		select {
		case err := <-served:
			if ctx.Err() == nil {
				return err
			}
		case <-ctx.Done():
		}
	} else {
		_ = ln.Close()
	}

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), drain)
	// A callback may retain its context: do not end it before its deadline
	// merely because the drain and Serve completed early.
	context.AfterFunc(drainCtx, cancelDrain)
	_ = ln.Close()
	tracked.beginDrain()
	err := srv.Shutdown(drainCtx)
	unfinished := tracked.unfinished()
	if stop != nil {
		stop(drainCtx)
	}
	if err != nil {
		cancelRequests()
		_ = srv.Close()
		if unfinished != 0 {
			return &DrainError{Unfinished: unfinished}
		}
	}
	return nil
}
