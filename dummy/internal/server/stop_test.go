package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// R-KCH4-NURS R-KA1B-WBAE
func TestServeCallsStopOnceAfterDrainAndWaitsForIt(t *testing.T) {
	ln := listenLoopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	reached := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	result := make(chan error, 1)
	go func() {
		result <- Serve(ctx, ln, http.NotFoundHandler(), time.Second, func(stopCtx context.Context) {
			calls.Add(1)
			if ctx.Err() == nil || stopCtx.Err() != nil {
				t.Error("stop called before cancellation or with prematurely done context")
			}
			deadline, ok := stopCtx.Deadline()
			if !ok || time.Until(deadline) <= 0 {
				t.Error("stop context lacks future deadline")
			}
			close(reached)
			<-release
		})
	}()
	cancel()
	<-reached
	select {
	case err := <-result:
		t.Fatalf("returned before stop: %v", err)
	default:
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("stop calls=%d", calls.Load())
	}
	calls.Store(0)
	if err := Serve(context.Background(), &errorListener{err: errors.New("accept failed")}, http.NotFoundHandler(), time.Second, func(context.Context) { calls.Add(1) }); err == nil {
		t.Error("failure returned nil")
	}
	if calls.Load() != 0 {
		t.Error("serving failure called stop")
	}
}

type stopOrderListener struct {
	net.Listener
	stopReturned *atomic.Bool
	t            *testing.T
}

func (l *stopOrderListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &stopOrderConn{Conn: c, listener: l}, nil
}

type stopOrderConn struct {
	net.Conn
	listener *stopOrderListener
}

func (c *stopOrderConn) Close() error {
	if !c.listener.stopReturned.Load() {
		c.listener.t.Error("active connection closed before stop returned")
	}
	return c.Conn.Close()
}

// R-KB98-A313 R-KCH4-NURS
func TestServeDeadlineCallsStopBeforeClosingUnfinishedConnection(t *testing.T) {
	base := listenLoopback(t)
	var stopped atomic.Bool
	ln := &stopOrderListener{Listener: base, stopReturned: &stopped, t: t}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	result := make(chan error, 1)
	go func() {
		result <- Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "prefix")
			w.(http.Flusher).Flush()
			close(started)
			<-release
		}), 20*time.Millisecond, func(stopCtx context.Context) {
			calls.Add(1)
			if stopCtx.Err() == nil {
				t.Error("deadline stop context is not done")
			}
			stopped.Store(true)
		})
	}()
	conn := dialListener(t, base)
	defer func() { _ = conn.Close() }()
	_, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: dummy\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	select {
	case err := <-result:
		var drain *DrainError
		if !errors.As(err, &drain) || drain.Unfinished != 1 {
			t.Errorf("drain=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve waited for handler")
	}
	if calls.Load() != 1 {
		t.Errorf("stop calls=%d", calls.Load())
	}
}
