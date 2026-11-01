// Package server serves the inherited listener and bounds graceful shutdown.
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// DrainError reports handlers that did not finish before the drain deadline.
type DrainError struct {
	Unfinished int
}

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return fmt.Sprintf("stopped with %d requests unfinished", e.Unfinished)
}

// Serve handles requests until cancellation or a listener failure. Requests
// retain their context during the grace period and are cancelled at its end.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(ctx context.Context)) error {
	requests, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	var mu sync.Mutex
	active := 0
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return requests },
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
		if ctx.Err() == nil {
			cancelRequests()
			_ = srv.Close()
			deadline, cancel := context.WithTimeout(context.Background(), drain)
			defer cancel()
			stop(deadline)
			return err
		}
	case <-ctx.Done():
	}

	deadline, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	expired := make(chan struct{})
	unfinished := 0
	stopDeadline := context.AfterFunc(deadline, func() {
		mu.Lock()
		unfinished = active
		mu.Unlock()
		cancelRequests()
		_ = srv.Close()
		close(expired)
	})
	_ = srv.Shutdown(deadline)
	if !stopDeadline() {
		<-expired
	}
	stop(deadline)
	if unfinished != 0 {
		return &DrainError{Unfinished: unfinished}
	}
	return nil
}
