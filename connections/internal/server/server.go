// Package server manages an inherited listener and bounded request draining.
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

// DrainError reports handlers still running at the shared deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve serves until cancellation or listener failure, then drains once.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var active atomic.Int64
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { active.Add(1); defer active.Add(-1); h.ServeHTTP(w, r) })}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	var err error
	select {
	case <-ctx.Done():
	case err = <-done:
	}
	dctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	shutdownErr := srv.Shutdown(dctx)
	unfinished := int(active.Load())
	if stop != nil {
		stop(dctx)
	}
	if shutdownErr != nil {
		_ = srv.Close()
	}
	if unfinished > 0 {
		return &DrainError{Unfinished: unfinished}
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
