// Package server owns the inherited listener and bounded request drain.
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

// DrainError reports requests still active at the deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve serves the listener and drains requests and the stop callback together.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var active atomic.Int64
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { active.Add(1); defer active.Add(-1); h.ServeHTTP(w, r) }), ReadHeaderTimeout: 10 * time.Second}
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
	dc, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	stopped := make(chan struct{})
	go func() { stop(dc); close(stopped) }()
	err := srv.Shutdown(dc)
	unfinished := int(active.Load())
	if err != nil {
		_ = srv.Close()
	}
	select {
	case <-stopped:
	case <-dc.Done():
	}
	<-done
	if unfinished > 0 {
		return &DrainError{unfinished}
	}
	return nil
}
