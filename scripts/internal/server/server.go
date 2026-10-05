// Package server serves inherited listeners and drains accepted requests.
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
	"time"
)

// DrainError counts requests cut off at the drain deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve owns ln, serves HTTP and finishes accepted requests before stopping.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var mu sync.Mutex
	changed := make(chan struct{})
	connections := make(map[net.Conn]http.ConnState)
	requests := make(map[*http.Request]context.CancelFunc)
	draining := false
	signal := func() { close(changed); changed = make(chan struct{}) }
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithCancel(r.Context())
		mu.Lock()
		requests[r] = cancel
		signal()
		mu.Unlock()
		defer func() { cancel(); mu.Lock(); delete(requests, r); signal(); mu.Unlock() }()
		h.ServeHTTP(w, r.WithContext(c))
	})
	srv.ConnState = func(c net.Conn, state http.ConnState) {
		mu.Lock()
		if state == http.StateClosed || state == http.StateHijacked {
			delete(connections, c)
		} else {
			connections[c] = state
		}
		closeIdle := draining && (state == http.StateIdle || state == http.StateNew)
		signal()
		mu.Unlock()
		if closeIdle {
			_ = c.Close()
		}
	}
	serving := make(chan error, 1)
	go func() { serving <- srv.Serve(ln) }()
	select {
	case err := <-serving:
		if ctx.Err() == nil {
			_ = srv.Close()
			if err == nil {
				return errors.New("listener stopped")
			}
			return err
		}
	case <-ctx.Done():
	}
	deadline, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	mu.Lock()
	draining = true
	for c, state := range connections {
		if state == http.StateNew || state == http.StateIdle {
			_ = c.Close()
		}
	}
	mu.Unlock()
	_ = ln.Close()
	srv.SetKeepAlivesEnabled(false)
	for {
		mu.Lock()
		n := len(requests)
		empty := len(connections) == 0
		wake := changed
		mu.Unlock()
		if empty || deadline.Err() != nil {
			if stop != nil {
				stop(deadline)
			}
			if deadline.Err() != nil && n > 0 {
				mu.Lock()
				for _, finish := range requests {
					finish()
				}
				mu.Unlock()
				_ = srv.Close()
				return &DrainError{Unfinished: n}
			}
			_ = srv.Close()
			return nil
		}
		select {
		case <-wake:
		case <-deadline.Done():
		}
	}
}
