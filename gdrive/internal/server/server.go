// Package server owns the inherited listener and its bounded drain.
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

// DrainError reports handlers unfinished at the deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve serves the inherited listener and drains requests and stop together.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) error {
	var mu sync.Mutex
	active := 0
	changed := make(chan struct{})
	s := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		mu.Unlock()
		defer func() { mu.Lock(); active--; close(changed); changed = make(chan struct{}); mu.Unlock() }()
		h.ServeHTTP(w, r)
	})}
	served := make(chan error, 1)
	go func() { served <- s.Serve(ln) }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	dctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		if stop != nil {
			stop(dctx)
		}
		close(stopped)
	}()
	err := s.Shutdown(dctx)
	mu.Lock()
	n := active
	mu.Unlock()
	if err != nil {
		_ = s.Close()
		limit := time.NewTimer(time.Second)
		defer limit.Stop()
		for {
			mu.Lock()
			left, ch := active, changed
			mu.Unlock()
			if left == 0 {
				break
			}
			select {
			case <-ch:
			case <-limit.C:
				goto drained
			}
		}
	drained:
	}
	<-stopped
	if n > 0 {
		return &DrainError{n}
	}
	return nil
}
