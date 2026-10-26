package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/ledger/internal/server"
)

// R-83XY-WGSW R-855V-A8JL R-86DR-O0AA R-87LO-1S0Z
func TestServeDrains(t *testing.T) {
	for _, overdue := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "deadline"}[overdue], func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			release := make(chan struct{})
			handled := make(chan struct{})
			stops := make(chan context.Context, 1)
			result := make(chan error, 1)
			go func() {
				result <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					close(entered)
					<-release
					_, _ = io.WriteString(w, "chosen response")
					close(handled)
				}), 100*time.Millisecond, func(c context.Context) { stops <- c })
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
				t.Fatal("handler not reached")
			}
			select {
			case e := <-result:
				t.Fatalf("returned early: %v", e)
			default:
			}
			cancel()
			if !overdue {
				close(release)
			}
			select {
			case e := <-result:
				if overdue {
					var de *server.DrainError
					if !errors.As(e, &de) || de.Unfinished != 1 {
						t.Fatalf("drain: %v", e)
					}
				} else if e != nil {
					t.Fatal(e)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("server failed to drain")
			}
			c := <-stops
			if overdue {
				if c.Err() == nil {
					t.Fatal("deadline context is live")
				}
				close(release)
			}
			<-handled
			if got := <-response; got != "chosen response" {
				t.Fatal(got)
			}
			select {
			case <-stops:
				t.Fatal("stop called twice")
			default:
			}
		})
	}
	for n, want := range map[int]string{0: "stopped with 0 requests unfinished", 1: "stopped with 1 request unfinished", 2: "stopped with 2 requests unfinished"} {
		if got := (&server.DrainError{Unfinished: n}).Error(); got != want {
			t.Fatalf("%q != %q", got, want)
		}
	}
}
