// Package server manages readiness, maintenance, and graceful HTTP draining.
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

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/declarations"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

// Delivery is the delivery loop's serving and draining lifecycle.
type Delivery interface {
	Run(ctx context.Context)
	Drain(ctx context.Context)
}

// Config supplies the shared components and scheduling hooks.
type Config struct {
	Listener     net.Listener
	Handler      http.Handler
	Store        *store.Store
	Declarations *declarations.Declarations
	Delivery     Delivery
	Telemetry    *telemetry.Writer
	NotifySocket string
	Retention    time.Duration
	Refresh      time.Duration
	Drain        time.Duration
	Now          func() time.Time
	SweepAfter   func(d time.Duration) <-chan time.Time
	RefreshAfter func(d time.Duration) <-chan time.Time
}

// DrainError counts handlers still running at the drain deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

type connections struct {
	mu       sync.Mutex
	states   map[net.Conn]http.ConnState
	stopping bool
	active   int
	changed  chan struct{}
}

func (c *connections) signal() { close(c.changed); c.changed = make(chan struct{}) }

func (c *connections) state(conn net.Conn, state http.ConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state == http.StateClosed || state == http.StateHijacked {
		delete(c.states, conn)
	} else {
		c.states[conn] = state
	}
	if c.stopping && state != http.StateActive {
		_ = conn.Close()
	}
	c.signal()
}

func (c *connections) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.active++
		c.mu.Unlock()
		defer func() { c.mu.Lock(); c.active--; c.signal(); c.mu.Unlock() }()
		next.ServeHTTP(w, r)
	})
}

func (c *connections) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopping = true
	for conn, state := range c.states {
		if state != http.StateActive {
			_ = conn.Close()
		}
	}
}

func (c *connections) wait(ctx context.Context) int {
	for {
		c.mu.Lock()
		busy := c.active != 0
		for _, state := range c.states {
			busy = busy || state == http.StateActive
		}
		changed := c.changed
		c.mu.Unlock()
		if !busy {
			return 0
		}
		select {
		case <-ctx.Done():
			c.mu.Lock()
			unfinished := c.active
			c.mu.Unlock()
			return unfinished
		case <-changed:
		}
	}
}

func notify(address string) error {
	if address == "" {
		return nil
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("READY=1"))
	return err
}

// Run serves until cancellation, then drains accepted work to one deadline.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SweepAfter == nil {
		cfg.SweepAfter = time.After
	}
	if cfg.RefreshAfter == nil {
		cfg.RefreshAfter = time.After
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = cfg.Listener.Close() }()
	_ = cfg.Store.Sweep(workCtx, cfg.Now().Add(-cfg.Retention))
	var refreshDone <-chan struct{}
	startRefresh := func() {
		done := make(chan struct{})
		refreshDone = done
		go func() { cfg.Declarations.Refresh(workCtx); close(done) }()
	}
	refreshTick := cfg.RefreshAfter(cfg.Refresh)
	startRefresh()
	// Startup participates in the same fixed-period schedule as later refreshes.
	starting := true
	for starting {
		select {
		case <-refreshDone:
			refreshDone = nil
			starting = false
		case <-refreshTick:
			if ctx.Err() == nil {
				refreshTick = cfg.RefreshAfter(cfg.Refresh)
			}
		case <-ctx.Done():
			<-refreshDone
			starting = false
		}
	}
	if err := notify(cfg.NotifySocket); err != nil {
		return err
	}
	cfg.Telemetry.Ready()
	loopDone := make(chan struct{})
	go func() { cfg.Delivery.Run(workCtx); close(loopDone) }()
	conns := &connections{states: make(map[net.Conn]http.ConnState), changed: make(chan struct{})}
	httpServer := &http.Server{Handler: conns.handler(cfg.Handler), ConnState: conns.state, ReadHeaderTimeout: 10 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(cfg.Listener) }()
	var sweepTick <-chan time.Time
	if ctx.Err() == nil {
		sweepTick = cfg.SweepAfter(time.Hour)
	}
	for ctx.Err() == nil {
		select {
		case err := <-served:
			if ctx.Err() == nil {
				cancel()
				<-loopDone
				_ = httpServer.Close()
				return err
			}
		case <-ctx.Done():
		case <-sweepTick:
			if ctx.Err() == nil {
				_ = cfg.Store.Sweep(workCtx, cfg.Now().Add(-cfg.Retention))
				if ctx.Err() == nil {
					sweepTick = cfg.SweepAfter(time.Hour)
				}
			}
		case <-refreshTick:
			if ctx.Err() == nil {
				refreshTick = cfg.RefreshAfter(cfg.Refresh)
				if refreshDone != nil {
					select {
					case <-refreshDone:
						refreshDone = nil
					default:
					}
				}
				if refreshDone == nil {
					startRefresh()
				}
			}
		case <-refreshDone:
			refreshDone = nil
		}
	}
	deadline, finish := context.WithTimeout(context.Background(), cfg.Drain)
	defer finish()
	_ = cfg.Listener.Close()
	httpServer.SetKeepAlivesEnabled(false)
	conns.stop()
	<-loopDone
	drained := make(chan struct{})
	go func() { cfg.Delivery.Drain(deadline); close(drained) }()
	unfinished := conns.wait(deadline)
	<-drained
	if refreshDone != nil {
		<-refreshDone
	}
	reason := context.Cause(ctx).Error()
	cfg.Telemetry.Shutdown(deadline, reason)
	_ = httpServer.Close()
	if unfinished > 0 {
		return &DrainError{Unfinished: unfinished}
	}
	return nil
}
