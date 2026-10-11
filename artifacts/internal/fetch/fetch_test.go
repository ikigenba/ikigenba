package fetch_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts/internal/fetch"
)

func serviceFile(t *testing.T, socket string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "services.json")
	data := map[string]any{"services": []map[string]any{{"name": "scripts", "url": "http://scripts.test:7401", "socket": socket, "description": "fixture", "enabled": true, "mcp": true}, {"name": "second", "url": "http://scripts.test:7401", "socket": socket, "description": "fixture", "enabled": true, "mcp": true}, {"name": "artifacts", "url": "https://artifacts.test", "socket": socket, "description": "fixture", "enabled": true, "mcp": true}}}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func sibling(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fetch-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close() })
	return serviceFile(t, socket)
}
func config(t *testing.T, path string) (fetch.Config, *telemetry.Capture) {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "artifacts", Sink: capture, Now: func() time.Time { return time.Unix(123, 0) }, Rand: repeat{}, Stderr: io.Discard})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	return fetch.Config{ServicesPath: path, Telemetry: w, Limit: time.Minute, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}, capture
}

type repeat struct{}

func (repeat) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(i)
	}
	return len(p), nil
}

// R-NSYG-Y89P R-NVE9-PRR3 R-NWM6-3JHS R-OZHV-W0BR
func TestTarget(t *testing.T) {
	p := serviceFile(t, "/fixture/socket")
	for _, u := range []string{"http://scripts.test:7401/x?d=1", "https://artifacts.test/files/a"} {
		e, err := fetch.Target(p, u)
		if err != nil || e.Socket != "/fixture/socket" {
			t.Fatalf("%+v %v", e, err)
		}
		if u[0:4] == "http" && u[4] != 's' && e.Name != "scripts" {
			t.Fatal(e)
		}
	}
	for _, u := range []string{"%", "/x", "http://user@scripts.test:7401/x", "https://scripts.test:7401/x", "http://scripts.test:74011/x", "http://scripts.test:7401.evil.test/x", "http://scripts.test:7401@evil.test/x", "http://other.test:7401/x"} {
		if _, err := fetch.Target(p, u); !errors.Is(err, fetch.ErrNotOnSpace) {
			t.Fatal(u, err)
		}
	}
	for _, p := range []string{"", filepath.Join(t.TempDir(), "absent"), serviceFile(t, "")} {
		if _, err := fetch.Target(p, "http://scripts.test:7401/x"); !errors.Is(err, fetch.ErrNotOnSpace) {
			t.Fatal(err)
		}
	}
	errs := []error{fetch.ErrNotOnSpace, fetch.ErrNoAnswer, fetch.ErrTimedOut}
	for i, e := range errs {
		if e == nil {
			t.Fatal("nil sentinel")
		}
		for j, o := range errs {
			if i != j && errors.Is(e, o) {
				t.Fatal("overlapping errors")
			}
		}
	}
	var status error = &fetch.StatusError{Code: 404}
	_ = status.Error()
}

// R-NZ1Y-V2Z6 R-P0PS-9S2G R-O2PO-0E79
func TestFetchRequestAndBody(t *testing.T) {
	requests := make(chan *http.Request, 3)
	p := sibling(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r
		w.Header().Set("Content-Type", "custom/type; x=y")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write([]byte("raw encoded bytes"))
	})
	cfg, capture := config(t, p)
	for _, caller := range []identity.Caller{{UserID: "user", Email: "u@example.test", RequestID: "request"}, {}} {
		ctx := identity.NewContext(context.Background(), caller)
		res, err := fetch.Get(ctx, cfg, "http://scripts.test:7401/a%2Fb?x=y")
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(res.Body)
		if err != nil || string(b) != "raw encoded bytes" || res.ContentType != "custom/type; x=y" {
			t.Fatalf("%q %v %+v", b, err, res)
		}
		_ = res.Body.Close()
		r := <-requests
		if r.Method != "GET" || r.Host != "scripts" || r.URL.EscapedPath() != "/a%2Fb" || r.URL.RawQuery != "x=y" || r.ContentLength != 0 {
			t.Fatal(r)
		}
		if r.Header.Get("X-User-Id") != caller.UserID || r.Header.Get("X-User-Email") != caller.Email || r.Header.Get("X-Request-Id") != caller.RequestID {
			t.Fatal(r.Header)
		}
		for key, value := range map[string]string{"X-User-Id": caller.UserID, "X-User-Email": caller.Email, "X-Request-Id": caller.RequestID} {
			_, present := r.Header[key]
			if present != (value != "") {
				t.Fatal(key, r.Header)
			}
		}
		if r.Body != nil {
			body, e := io.ReadAll(r.Body)
			if e != nil || len(body) != 0 {
				t.Fatal(body, e)
			}
		}
	}
	if err := cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	es := capture.Events()
	if len(es) != 2 {
		t.Fatal(es)
	}
	for _, e := range es {
		if e.Name != "sibling.called" || !reflect.DeepEqual(e.Attrs["target"], "scripts") || e.Attrs["method"] != "GET" || e.Attrs["path"] != "/a/b" {
			t.Fatal(e)
		}
	}
	if _, err := fetch.Get(context.Background(), cfg, "https://outside.test/x"); !errors.Is(err, fetch.ErrNotOnSpace) {
		t.Fatal(err)
	}
	select {
	case r := <-requests:
		t.Fatal(r)
	default:
	}
}

// R-O3XK-E5XY
func TestStatuses(t *testing.T) {
	for _, status := range []int{201, 302, 404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			count := make(chan struct{}, 2)
			p := sibling(t, func(w http.ResponseWriter, _ *http.Request) {
				count <- struct{}{}
				w.Header().Set("Location", "/next")
				w.WriteHeader(status)
			})
			cfg, _ := config(t, p)
			r, err := fetch.Get(context.Background(), cfg, "http://scripts.test:7401/x")
			var e *fetch.StatusError
			if r != nil || !errors.As(err, &e) || e.Code != status {
				t.Fatal(r, err)
			}
			<-count
			select {
			case <-count:
				t.Fatal("followed redirect")
			default:
			}
		})
	}
}

// R-O1HR-MMGK
func TestBound(t *testing.T) {
	for _, phase := range []string{"headers", "body", "eof", "closed"} {
		t.Run(phase, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			p := sibling(t, func(w http.ResponseWriter, r *http.Request) {
				if phase != "headers" {
					w.Header().Set("Content-Length", "4")
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				close(entered)
				if phase == "eof" {
					_, _ = w.Write([]byte("done"))
					return
				}
				select {
				case <-release:
				case <-r.Context().Done():
				}
			})
			cfg, _ := config(t, p)
			timer := make(chan time.Time, 1)
			calls := make(chan time.Duration, 2)
			cfg.After = func(d time.Duration) <-chan time.Time { calls <- d; return timer }
			type answer struct {
				r *fetch.Response
				e error
			}
			result := make(chan answer, 1)
			go func() {
				r, e := fetch.Get(context.Background(), cfg, "http://scripts.test:7401/x")
				result <- answer{r, e}
			}()
			<-entered
			if d := <-calls; d != cfg.Limit {
				t.Fatal(d)
			}
			if phase == "headers" {
				timer <- time.Time{}
				a := <-result
				if a.r != nil || !errors.Is(a.e, fetch.ErrTimedOut) {
					t.Fatal(a)
				}
				select {
				case <-calls:
					t.Fatal("multiple waits")
				default:
				}
				return
			}
			a := <-result
			if a.e != nil {
				t.Fatal(a.e)
			}
			var closedErr error
			if phase == "closed" {
				_ = a.r.Body.Close()
				_, closedErr = a.r.Body.Read(make([]byte, 8))
			}
			if phase == "eof" {
				if _, e := io.ReadAll(a.r.Body); e != nil {
					t.Fatal(e)
				}
			}
			timer <- time.Time{}
			if phase == "closed" {
				_, e := a.r.Body.Read(make([]byte, 8))
				if errors.Is(e, fetch.ErrTimedOut) || (closedErr == nil) != (e == nil) || (e != nil && closedErr != nil && e.Error() != closedErr.Error()) {
					t.Fatal(closedErr, e)
				}
			}
			if phase == "eof" {
				if _, e := a.r.Body.Read(make([]byte, 8)); !errors.Is(e, io.EOF) {
					t.Fatal(e)
				}
			}
			if phase == "body" {
				for range 2 {
					if _, e := a.r.Body.Read(make([]byte, 8)); !errors.Is(e, fetch.ErrTimedOut) {
						t.Fatal(e)
					}
				}
			}
			_ = a.r.Body.Close()
			close(release)
			select {
			case <-calls:
				t.Fatal("multiple waits")
			default:
			}
		})
	}
}

// R-O55G-RXON
func TestBrokenSibling(t *testing.T) {
	p := serviceFile(t, filepath.Join(t.TempDir(), "absent"))
	cfg, _ := config(t, p)
	if r, e := fetch.Get(context.Background(), cfg, "http://scripts.test:7401/x"); r != nil || !errors.Is(e, fetch.ErrNoAnswer) {
		t.Fatal(r, e)
	}
	for _, header := range []string{"", "HTTP/1.1 200 OK\r\nContent-Length: 8\r\n\r\nshort", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n8\r\nshort"} {
		p = sibling(t, func(w http.ResponseWriter, _ *http.Request) {
			c, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			_, _ = io.WriteString(c, header)
			_ = c.Close()
		})
		cfg, _ = config(t, p)
		r, e := fetch.Get(context.Background(), cfg, "http://scripts.test:7401/x")
		if header == "" {
			if r != nil || !errors.Is(e, fetch.ErrNoAnswer) {
				t.Fatal(r, e)
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		_, e = io.ReadAll(r.Body)
		_ = r.Body.Close()
		if e == nil || errors.Is(e, io.EOF) || errors.Is(e, fetch.ErrTimedOut) {
			t.Fatal(e)
		}
	}
}

// R-NWM6-3JHS R-NZ1Y-V2Z6 R-O2PO-0E79
func TestEmptyPathAndFreshServices(t *testing.T) {
	received := make(chan string, 2)
	p := sibling(t, func(w http.ResponseWriter, r *http.Request) {
		received <- r.URL.RequestURI()
		w.Header()["Content-Type"] = nil
		w.WriteHeader(200)
	})
	cfg, _ := config(t, p)
	res, e := fetch.Get(context.Background(), cfg, "http://scripts.test:7401")
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if e != nil || len(b) != 0 || res.ContentType != "" || <-received != "/" {
		t.Fatal(res, e)
	}
	if e = os.WriteFile(p, []byte(`{"services":[]}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = fetch.Get(context.Background(), cfg, "http://scripts.test:7401"); !errors.Is(e, fetch.ErrNotOnSpace) {
		t.Fatal(e)
	}
	select {
	case r := <-received:
		t.Fatal(r)
	default:
	}
}

// R-NSYG-Y89P
func TestDefaultTimer(t *testing.T) {
	p := sibling(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "complete") })
	cfg, _ := config(t, p)
	cfg.After = nil
	res, e := fetch.Get(context.Background(), cfg, "http://scripts.test:7401/x")
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if e != nil || string(body) != "complete" {
		t.Fatal(string(body), e)
	}
}

// R-NSYG-Y89P
func TestDefaultTimerExpires(t *testing.T) {
	p := sibling(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	cfg, _ := config(t, p)
	cfg.After = nil
	cfg.Limit = -time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := fetch.Get(ctx, cfg, "http://scripts.test:7401/x")
	if res != nil || !errors.Is(err, fetch.ErrTimedOut) {
		t.Fatal(res, err)
	}
}
