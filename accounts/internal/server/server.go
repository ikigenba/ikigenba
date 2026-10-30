// Package server serves an inherited listener and drains its requests.
package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// DrainError reports handlers still running at the shutdown deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve runs h until cancellation, then drains requests and stops its services.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var active atomic.Int64
	requests, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return requests },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			active.Add(1)
			defer active.Add(-1)
			h.ServeHTTP(w, r)
		}),
	}
	defer func() { _ = srv.Close() }()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), drain)
	defer cancelDrain()
	deadline := make(chan int, 1)
	endRequests := context.AfterFunc(drainCtx, func() {
		unfinished := int(active.Load())
		cancelRequests()
		deadline <- unfinished
	})
	defer endRequests()
	err := srv.Shutdown(drainCtx)
	var unfinished int
	if err != nil {
		unfinished = <-deadline
	}
	stop(drainCtx)
	if err != nil && unfinished > 0 {
		return &DrainError{Unfinished: unfinished}
	}
	return nil
}
