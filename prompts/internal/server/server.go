// Package server serves an inherited listener and drains its active requests.
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

// DrainError reports requests still in progress at the drain deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// closeOnceListener lets shutdown and the serve loop release one shared socket.
type closeOnceListener struct {
	net.Listener
	once sync.Once
	err  error
}

func (l *closeOnceListener) Close() error {
	l.once.Do(func() { l.err = l.Listener.Close() })
	return l.err
}

// Serve answers requests until cancellation, then lets active responses drain.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	ln = &closeOnceListener{Listener: ln}
	var mu sync.Mutex
	active := make(map[*http.Request]context.CancelFunc)
	connections := make(map[net.Conn]http.ConnState)
	draining := false
	srv := &http.Server{ErrorLog: log.New(io.Discard, "", 0), ReadHeaderTimeout: 0}
	srv.ConnState = func(conn net.Conn, state http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		if state == http.StateClosed || state == http.StateHijacked {
			delete(connections, conn)
			return
		}
		connections[conn] = state
		if draining && (state == http.StateNew || state == http.StateIdle) {
			_ = conn.Close()
		}
	}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCtx, cancel := context.WithCancel(r.Context())
		mu.Lock()
		active[r] = cancel
		mu.Unlock()
		defer func() { mu.Lock(); delete(active, r); mu.Unlock(); cancel() }()
		h.ServeHTTP(w, r.WithContext(requestCtx))
	})
	result := make(chan error, 1)
	go func() { result <- srv.Serve(ln) }()
	select {
	case err := <-result:
		if ctx.Err() == nil {
			return err
		}
	case <-ctx.Done():
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	_ = ln.Close()
	mu.Lock()
	draining = true
	for conn, state := range connections {
		if state == http.StateNew || state == http.StateIdle {
			_ = conn.Close()
		}
	}
	mu.Unlock()
	err := srv.Shutdown(drainCtx)
	mu.Lock()
	n := len(active)
	mu.Unlock()
	if stop != nil {
		stop(drainCtx)
	}
	if err != nil {
		mu.Lock()
		for _, cancelRequest := range active {
			cancelRequest()
		}
		mu.Unlock()
		_ = srv.Close()
		if n > 0 {
			return &DrainError{Unfinished: n}
		}
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
