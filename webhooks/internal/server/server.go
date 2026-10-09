// Package server serves inherited listeners and bounds request draining.
package server

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// DrainError reports handlers still running at the drain deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve answers HTTP requests until cancellation or an accept failure.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var active atomic.Int64
	var connectionsMu sync.Mutex
	connections := make(map[net.Conn]http.ConnState)
	draining := false
	srv := &http.Server{ConnState: func(conn net.Conn, state http.ConnState) {
		connectionsMu.Lock()
		defer connectionsMu.Unlock()
		if state == http.StateClosed || state == http.StateHijacked {
			delete(connections, conn)
			return
		}
		connections[conn] = state
		if draining && (state == http.StateNew || state == http.StateIdle) {
			_ = conn.Close()
		}
	}, ReadHeaderTimeout: 10 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		h.ServeHTTP(w, r)
	})}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		if ctx.Err() == nil {
			_ = srv.Close()
			return err
		}
	case <-ctx.Done():
	}
	connectionsMu.Lock()
	draining = true
	for conn, state := range connections {
		if state == http.StateNew || state == http.StateIdle {
			_ = conn.Close()
		}
	}
	connectionsMu.Unlock()
	deadline, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	err := srv.Shutdown(deadline)
	unfinished := int(active.Load())
	if stop != nil {
		stop(deadline)
	}
	if err != nil {
		_ = srv.Close()
		if unfinished > 0 {
			return &DrainError{Unfinished: unfinished}
		}
	}
	return nil
}
