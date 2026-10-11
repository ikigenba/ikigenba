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

	"github.com/ikigenba/ikigenba/connections/internal/server"
)

// R-1OFM-VKNA R-1PNJ-9CDZ R-1QVF-N44O
func TestServeDrain(t *testing.T) {
	for _, unfinished := range []bool{false, true} {
		t.Run(map[bool]string{false: "finish", true: "deadline"}[unfinished], func(t *testing.T) {
			ln, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			release := make(chan struct{})
			exited := make(chan struct{})
			done := make(chan error, 1)
			var stops atomic.Int32
			go func() {
				done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					close(entered)
					<-release
					_, _ = io.WriteString(w, "response")
					close(exited)
				}), 100*time.Millisecond, func(d context.Context) {
					stops.Add(1)
					if _, ok := d.Deadline(); !ok {
						t.Error("stop lacks deadline")
					}
					if unfinished && d.Err() == nil {
						t.Error("deadline not shared")
					}
				})
			}()
			response := make(chan string, 1)
			go func() {
				r, e := http.Get("http://" + ln.Addr().String())
				if e != nil {
					response <- e.Error()
					return
				}
				defer func() { _ = r.Body.Close() }()
				b, _ := io.ReadAll(r.Body)
				response <- string(b)
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("no request")
			}
			select {
			case e := <-done:
				t.Fatalf("premature: %v", e)
			default:
			}
			cancel()
			if !unfinished {
				close(release)
			}
			select {
			case e := <-done:
				if unfinished {
					var de *server.DrainError
					if !errors.As(e, &de) || de.Unfinished != 1 {
						t.Fatalf("%v", e)
					}
				} else if e != nil {
					t.Fatal(e)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("did not drain")
			}
			if stops.Load() != 1 {
				t.Fatal(stops.Load())
			}
			if unfinished {
				close(release)
			} else {
				select {
				case s := <-response:
					if s != "response" {
						t.Fatal(s)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("response did not finish")
				}
			}
			select {
			case <-exited:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not exit")
			}
			if unfinished {
				select {
				case <-response:
				case <-time.After(3 * time.Second):
					t.Fatal("response did not finish")
				}
			}
		})
	}
	for _, tc := range []struct {
		n int
		s string
	}{{0, "stopped with 0 requests unfinished"}, {1, "stopped with 1 request unfinished"}, {2, "stopped with 2 requests unfinished"}} {
		if s := (&server.DrainError{Unfinished: tc.n}).Error(); s != tc.s {
			t.Fatal(s)
		}
	}
}

// R-1PNJ-9CDZ
func TestDrainCountsTwoHandlers(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	exited := make(chan struct{}, 2)
	done := make(chan error, 1)
	responses := make(chan struct{}, 2)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			entered <- struct{}{}
			<-release
			_, _ = io.WriteString(w, "response")
			exited <- struct{}{}
		}), 100*time.Millisecond, func(d context.Context) {
			if d.Err() == nil {
				t.Error("stop preceded drain deadline")
			}
		})
	}()
	for range 2 {
		go func() {
			res, e := http.Get("http://" + ln.Addr().String())
			if e == nil {
				_ = res.Body.Close()
			}
			responses <- struct{}{}
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("missing active handler")
		}
	}
	started := time.Now()
	cancel()
	select {
	case e := <-done:
		var de *server.DrainError
		if !errors.As(e, &de) || de.Unfinished != 2 {
			t.Fatal(e)
		}
		if time.Since(started) > time.Second {
			t.Fatal("deadline not anchored to cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not drain")
	}
	if c, e := net.DialTimeout("tcp", ln.Addr().String(), 100*time.Millisecond); e == nil {
		_ = c.Close()
		t.Fatal("listener still accepting")
	}
	close(release)
	for range 2 {
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			t.Fatal("handler did not exit")
		}
		select {
		case <-responses:
		case <-time.After(3 * time.Second):
			t.Fatal("response did not finish")
		}
	}
}
