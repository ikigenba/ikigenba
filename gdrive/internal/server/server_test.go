package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/gdrive/internal/server"
)

// R-ADNK-99J6 R-AG3D-0T0K
func TestDrainError(t *testing.T) {
	for n, want := range map[int]string{0: "stopped with 0 requests unfinished", 1: "stopped with 1 request unfinished", 2: "stopped with 2 requests unfinished"} {
		if got := (&server.DrainError{Unfinished: n}).Error(); got != want {
			t.Fatalf("%q", got)
		}
	}
}

type closeListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (l *closeListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return err
}

// R-AEVG-N19V
func TestServe(t *testing.T) {
	for _, cutoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "finish", true: "deadline"}[cutoff], func(t *testing.T) {
			raw, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ln := &closeListener{Listener: raw, closed: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			count := 1
			if cutoff {
				count = 2
			}
			entered := make(chan struct{}, count)
			release := make(chan struct{})
			stopping := make(chan context.Context, 1)
			var stopCalls atomic.Int64
			result := make(chan error, 1)
			go func() {
				result <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					entered <- struct{}{}
					<-release
					_, _ = io.WriteString(w, "chosen response")
				}), 100*time.Millisecond, func(c context.Context) {
					stopCalls.Add(1)
					stopping <- c
					if cutoff {
						<-c.Done()
					}
				})
			}()
			response := make(chan string, count)
			for i := 0; i < count; i++ {
				go func() {
					r, e := http.Get("http://" + ln.Addr().String())
					if e != nil {
						response <- ""
						return
					}
					defer func() { _ = r.Body.Close() }()
					b, _ := io.ReadAll(r.Body)
					response <- string(b)
				}()
			}
			for i := 0; i < count; i++ {
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("not entered")
				}
			}
			select {
			case <-result:
				t.Fatal("returned before cancellation")
			default:
			}
			cancel()
			var dctx context.Context
			select {
			case dctx = <-stopping:
			case <-time.After(3 * time.Second):
				t.Fatal("stop not called")
			}
			select {
			case <-ln.closed:
			case <-time.After(3 * time.Second):
				t.Fatal("listener kept accepting")
			}
			connection, e := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
			if e == nil {
				_ = connection.Close()
				t.Fatal("listener accepted after closure")
			}
			if !cutoff {
				close(release)
			}
			select {
			case e := <-result:
				if cutoff {
					var de *server.DrainError
					if !errors.As(e, &de) || de.Unfinished != count || dctx.Err() == nil {
						t.Fatalf("%v %v", e, dctx.Err())
					}
					close(release)
				} else if e != nil {
					t.Fatal(e)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("drain hung")
			}
			if stopCalls.Load() != 1 {
				t.Fatalf("stop called %d times", stopCalls.Load())
			}
			if !cutoff {
				select {
				case body := <-response:
					if body != "chosen response" {
						t.Fatal(body)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("response hung")
				}
			}
		})
	}
}
