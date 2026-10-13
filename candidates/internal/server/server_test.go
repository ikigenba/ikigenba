package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/server"
)

// R-2CTD-6N0K R-3NQ3-794E R-3OXZ-L0V3
func TestServeAndDrain(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "deadline"}[timeout], func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			var stopped atomic.Int64
			stopEntered := make(chan context.Context, 1)
			result := make(chan error, 1)
			go func() {
				result <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					close(entered)
					<-release
					w.Header().Set("X-Test", "chosen")
					w.WriteHeader(201)
					_, _ = io.WriteString(w, "test-response")
				}), 100*time.Millisecond, func(dc context.Context) { stopped.Add(1); stopEntered <- dc })
			}()
			request := make(chan error, 1)
			go func() {
				resp, e := http.Get("http://" + ln.Addr().String())
				if e == nil {
					defer func() { _ = resp.Body.Close() }()
					b, re := io.ReadAll(resp.Body)
					if re != nil {
						e = re
					} else if !timeout && (resp.StatusCode != 201 || resp.Header.Get("X-Test") != "chosen" || string(b) != "test-response") {
						e = errors.New("handler response differs")
					}
				}
				request <- e
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not start")
			}
			select {
			case e := <-result:
				t.Fatal("serve ended early", e)
			default:
			}
			cancel()
			dc := <-stopEntered
			if !timeout {
				release <- struct{}{}
			}
			select {
			case e := <-result:
				if timeout {
					var de *server.DrainError
					if !errors.As(e, &de) || de.Unfinished != 1 {
						t.Fatal(e)
					}
					if dc.Err() == nil {
						t.Fatal("stop context not expired")
					}
				} else if e != nil {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("drain stuck")
			}
			if stopped.Load() != 1 {
				t.Fatal(stopped.Load())
			}
			if !timeout {
				if e := <-request; e != nil {
					t.Fatal(e)
				}
			}
		})
	}
	for n, want := range map[int]string{1: "stopped with 1 request unfinished", 0: "stopped with 0 requests unfinished", 2: "stopped with 2 requests unfinished"} {
		if (&server.DrainError{Unfinished: n}).Error() != want {
			t.Fatal(n)
		}
	}
}

type closeObservedListener struct {
	net.Listener
	closed chan struct{}
}

func (l closeObservedListener) Close() error {
	err := l.Listener.Close()
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return err
}

// R-3NQ3-794E
func TestServeClosesListenerAndCountsUnfinished(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := closeObservedListener{Listener: raw, closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	stopped := make(chan error, 1)
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { entered <- struct{}{}; <-release }), 100*time.Millisecond, func(dc context.Context) { <-dc.Done(); stopped <- dc.Err() })
	}()
	for range 2 {
		go func() {
			response, e := http.Get("http://" + ln.Addr().String())
			if e == nil {
				_ = response.Body.Close()
			}
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("request did not enter")
		}
	}
	cancel()
	select {
	case <-ln.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("listener not closed")
	}
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener accepted after cancellation")
	}
	select {
	case err = <-result:
		var de *server.DrainError
		if !errors.As(err, &de) || de.Unfinished != 2 {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not return")
	}
	select {
	case err = <-stopped:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop deadline did not expire")
	}
}

// R-3NQ3-794E
func TestServeWaitsForStopWithinDrain(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, func(context.Context) { close(entered); <-release })
	}()
	cancel()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("stop not called")
	}
	select {
	case err = <-result:
		t.Fatal("returned before stop completed", err)
	default:
	}
	close(release)
	select {
	case err = <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not complete")
	}
}
