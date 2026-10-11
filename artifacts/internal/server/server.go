// Package server serves an inherited listener and drains active handlers.
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

// DrainError counts handlers left at the drain deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve answers requests until cancellation, then drains within one deadline.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var active atomic.Int64
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { active.Add(1); defer active.Add(-1); h.ServeHTTP(w, r) })}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	deadline, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	// Shutdown closes listeners before waiting for active handlers.
	drained := make(chan error, 1)
	go func() { drained <- srv.Shutdown(deadline) }()
	var err error
	select {
	case err = <-drained:
	case <-deadline.Done():
		err = deadline.Err()
	}
	unfinished := int(active.Load())
	if stop != nil {
		stop(deadline)
	}
	if err != nil {
		_ = srv.Close()
		if unfinished > 0 {
			return &DrainError{unfinished}
		}
	}
	return nil
}
