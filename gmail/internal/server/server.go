// Package server serves an inherited listener and drains active requests.
package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DrainError reports requests still executing at the drain deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve serves ln until cancellation or failure, draining requests and stop
// concurrently under one deadline.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var mu sync.Mutex
	active := 0
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
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
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		if stop != nil {
			stop(drainCtx)
		}
		close(stopped)
	}()
	shutdown := make(chan error, 1)
	go func() { shutdown <- srv.Shutdown(drainCtx) }()
	for shutdown != nil || stopped != nil {
		select {
		case err := <-shutdown:
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			shutdown = nil
		case <-stopped:
			stopped = nil
		case <-drainCtx.Done():
			mu.Lock()
			unfinished := active
			mu.Unlock()
			_ = srv.Close()
			if unfinished != 0 {
				return &DrainError{Unfinished: unfinished}
			}
			return nil
		}
	}
	return nil
}
