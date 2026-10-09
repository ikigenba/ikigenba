// Package server serves an inherited listener and drains its requests on stop.
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

// DrainError reports requests whose handlers outlasted the drain window.
type DrainError struct {
	Unfinished int
}

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve owns ln and serves until cancellation or an accept failure. The stop
// callback shares the requests' drain deadline and precedes forced disconnection.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var mu sync.Mutex
	connections := make(map[net.Conn]http.ConnState)
	draining := false
	active := 0
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
		ConnState: func(c net.Conn, state http.ConnState) {
			mu.Lock()
			defer mu.Unlock()
			if state == http.StateClosed || state == http.StateHijacked {
				delete(connections, c)
				return
			}
			connections[c] = state
			if draining && (state == http.StateNew || state == http.StateIdle) {
				_ = c.Close()
			}
		},
	}
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
	deadline, cancel := context.WithTimeout(context.Background(), drain)
	// A caller may retain the callback's context. Even an early clean stop must
	// leave its deadline intact rather than cancel it when Serve returns.
	context.AfterFunc(deadline, cancel)
	mu.Lock()
	draining = true
	for c, state := range connections {
		if state == http.StateNew || state == http.StateIdle {
			_ = c.Close()
		}
	}
	mu.Unlock()
	err := srv.Shutdown(deadline)
	mu.Lock()
	unfinished := active
	mu.Unlock()
	if stop != nil {
		stop(deadline)
	}
	if err != nil {
		_ = srv.Close()
		return &DrainError{Unfinished: unfinished}
	}
	return nil
}
