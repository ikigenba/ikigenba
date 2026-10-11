package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/artifacts/internal/server"
)

// R-D5RX-DHG9
func TestAPI(t *testing.T) {
	var e error = &server.DrainError{Unfinished: 2}
	if e.Error() == "" {
		t.Fatal(e)
	}
}

// R-D87Q-50XN
func TestError(t *testing.T) {
	for _, c := range []struct {
		n int
		s string
	}{{1, "stopped with 1 request unfinished"}, {0, "stopped with 0 requests unfinished"}, {3, "stopped with 3 requests unfinished"}} {
		if s := (&server.DrainError{Unfinished: c.n}).Error(); s != c.s {
			t.Fatal(s)
		}
	}
}

// R-D6ZT-R96Y
func TestServe(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "finish", true: "deadline"}[timeout], func(t *testing.T) {
			ln, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = ln.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			release := make(chan struct{})
			handled := make(chan struct{})
			stops := 0
			result := make(chan error, 1)
			go func() {
				result <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					close(entered)
					<-release
					_, _ = w.Write([]byte("chosen response"))
					close(handled)
				}), 100*time.Millisecond, func(c context.Context) {
					stops++
					if timeout && c.Err() == nil {
						t.Error("stop context not done")
					}
				})
			}()
			clientDone := make(chan struct{})
			go func() {
				defer close(clientDone)
				resp, e := http.Get("http://" + ln.Addr().String())
				if e != nil {
					if !timeout {
						t.Error(e)
					}
					return
				}
				defer func() { _ = resp.Body.Close() }()
				b, e := io.ReadAll(resp.Body)
				if !timeout && (e != nil || string(b) != "chosen response") {
					t.Error(string(b), e)
				}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("request absent")
			}
			select {
			case e := <-result:
				t.Fatalf("early return: %v", e)
			default:
			}
			cancel()
			if !timeout {
				close(release)
			}
			select {
			case e := <-result:
				if timeout {
					var d *server.DrainError
					if !errors.As(e, &d) || d.Unfinished != 1 {
						t.Fatal(e)
					}
				} else if e != nil {
					t.Fatal(e)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("drain absent")
			}
			if stops != 1 {
				t.Fatal(stops)
			}
			if timeout {
				close(release)
			}
			select {
			case <-handled:
			case <-time.After(3 * time.Second):
				t.Fatal("handler remains")
			}
			select {
			case <-clientDone:
			case <-time.After(3 * time.Second):
				t.Fatal("client remains")
			}
		})
	}
}

// R-D6ZT-R96Y
func TestSeveralUnfinished(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	done := make(chan struct{}, 3)
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { entered <- struct{}{}; <-release }), 100*time.Millisecond, func(c context.Context) {
			if c.Err() == nil {
				t.Error("drain context still active")
			}
		})
	}()
	for range 3 {
		go func() {
			resp, e := http.Get("http://" + ln.Addr().String())
			if e == nil {
				_ = resp.Body.Close()
			}
			done <- struct{}{}
		}()
	}
	for range 3 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("missing request")
		}
	}
	cancel()
	select {
	case err = <-result:
		var drain *server.DrainError
		if !errors.As(err, &drain) || drain.Unfinished != 3 {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing result")
	}
	close(release)
	for range 3 {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("client remains")
		}
	}
}
