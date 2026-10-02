// Package server serves a supplied listener and drains requests on cancellation.
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

// Serve serves HTTP until cancellation or an accept failure. stop runs before
// unfinished connections are cut off, with the remaining drain budget.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(ctx context.Context)) error {
	var mu sync.Mutex
	connections := make(map[net.Conn]http.ConnState)
	active := 0
	draining := false
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			active++
			mu.Unlock()
			defer func() {
				mu.Lock()
				active--
				mu.Unlock()
			}()
			h.ServeHTTP(w, r)
		}),
		ConnState: func(conn net.Conn, state http.ConnState) {
			mu.Lock()
			if state == http.StateClosed || state == http.StateHijacked {
				delete(connections, conn)
			} else {
				connections[conn] = state
			}
			closeIdle := draining && (state == http.StateNew || state == http.StateIdle)
			mu.Unlock()
			if closeIdle {
				_ = conn.Close()
			}
		},
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		if ctx.Err() == nil {
			return err
		}
	case <-ctx.Done():
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), drain)
	// A stop callback may retain this context. Even a fast drain must leave
	// its budget available until the actual deadline.
	context.AfterFunc(drainCtx, cancel)
	mu.Lock()
	draining = true
	var idle []net.Conn
	for conn, state := range connections {
		if state == http.StateNew || state == http.StateIdle {
			idle = append(idle, conn)
		}
	}
	mu.Unlock()
	for _, conn := range idle {
		_ = conn.Close()
	}
	err := srv.Shutdown(drainCtx)
	mu.Lock()
	unfinished := active
	mu.Unlock()
	if stop != nil {
		stop(drainCtx)
	}
	if err != nil {
		_ = srv.Close()
		return &DrainError{Unfinished: unfinished}
	}
	return nil
}
