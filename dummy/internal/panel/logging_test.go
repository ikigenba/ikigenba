package panel_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy/internal/cli"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func panelTestStore() *widget.Store {
	data := make([]byte, 16*4096)
	for i := range data {
		data[i] = byte(i / 16)
		data[i] ^= byte(i / 4096)
	}
	return widget.NewStore(bytes.NewReader(data))
}

func panelTestTelemetry(t *testing.T, stderr io.Writer) (*telemetry.Writer, *telemetry.Capture, *bytes.Buffer) {
	t.Helper()
	capture := &telemetry.Capture{}
	diagnostics := &bytes.Buffer{}
	writer := telemetry.New(telemetry.Config{Service: panel.ServiceName, Version: cli.Version, Sink: capture, Stderr: io.MultiWriter(diagnostics, stderr), Now: func() time.Time { return time.Unix(1700000000, 0) }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 65536))})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "cleanup") })
	return writer, capture, diagnostics
}

// R-CMTA-3ATT R-KSBT-MVET R-KTJQ-0N5I
// R-CO16-H2KI R-L5QP-UCKG R-L6YM-84B5
func TestPanelRequestTrail(t *testing.T) {
	cases := []struct {
		method, path, body, media, user string
		status                          int
		created                         bool
	}{
		{"GET", "/widgets?secret=query", "", "", "reader", 200, false},
		{"HEAD", "/widgets/table", "", "", "reader", 200, false},
		{"GET", "/missing", "", "", "reader", 404, false},
		{"PUT", "/widgets", "", "", "reader", 405, false},
		{"POST", "/widgets", "name=new&count=4&status=active", "application/x-www-form-urlencoded", "reader", 303, true},
		{"POST", "/widgets", "name=alpha&count=4&status=active", "application/x-www-form-urlencoded", "reader", 422, false},
		{"POST", "/widgets", "name=new&count=4&status=active", "text/plain", "reader", 415, false},
		{"GET", "/_appkit/theme.css", "", "", "reader", 200, false},
		{"POST", "/mcp", "broken", "application/json", "reader", 400, false},
		{"GET", "/widgets", "", "", "", 500, false},
		{"POST", "/mcp", "", "", "", 500, false},
		{"GET", "/_appkit/theme.css", "", "", "", 500, false},
	}
	for _, tc := range cases {
		for _, requestID := range []string{"trace", ""} {
			t.Run(tc.method+tc.path+tc.user+requestID, func(t *testing.T) {
				t.Setenv(services.Variable, "")
				writer, capture, diagnostics := panelTestTelemetry(t, io.Discard)
				store := panelTestStore()
				h := panel.Handler(store, pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
				r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				r.Header["X-User-Id"] = []string{tc.user, "ignored"}
				r.Header["X-Request-Id"] = []string{requestID, "ignored"}
				r.Header.Set("X-User-Email", "private@example.test")
				r.Header.Set("Content-Type", tc.media)
				out := httptest.NewRecorder()
				h.ServeHTTP(out, r)
				if out.Code != tc.status {
					t.Fatalf("status %d want %d", out.Code, tc.status)
				}
				if err := writer.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				events := capture.Events()
				count := 2
				if tc.created {
					count++
				}
				if len(events) != count {
					t.Fatalf("events %#v", events)
				}
				id := requestID
				if id == "" {
					id = hex.EncodeToString(bytes.Repeat([]byte{7}, 16))
				}
				for _, e := range events {
					if e.RequestID != id || e.User != tc.user {
						t.Fatalf("envelope %#v", e)
					}
				}
				if events[0].Name != "request.started" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": tc.method, "path": r.URL.Path}) {
					t.Fatalf("start %#v", events[0])
				}
				last := events[len(events)-1]
				if last.Name != "request.finished" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"status": int64(tc.status), "duration_us": int64(0)}) {
					t.Fatalf("finish %#v", last)
				}
				if tc.created {
					all := store.All()
					e := events[1]
					if e.Name != "widget.created" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"widget": all[len(all)-1].ID}) {
						t.Fatalf("creation %#v", e)
					}
				}
				if diagnostics.Len() != 0 {
					t.Fatalf("stderr %q", diagnostics.String())
				}
			})
		}
	}
}

// R-CMTA-3ATT
func TestPanelPanicTrail(t *testing.T) {
	t.Setenv(services.Variable, "")
	writer, capture, _ := panelTestTelemetry(t, io.Discard)
	h := panel.Handler(panelTestStore(), func(page.User) page.Banner { panic("banner failed") }, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
	r := pageTestRequest("GET", "/widgets")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("no panic")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), r)
	}()
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != 2 || events[0].Name != "request.started" || events[1].Name != "request.finished" || events[1].Attrs["status"] != int64(500) {
		t.Fatalf("events %#v", events)
	}
}

// R-IC9W-TOIW
func TestPanelKeepsWidgetIDsOutOfBodies(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{{"GET", "/widgets", ""}, {"GET", "/widgets/table", ""}, {"POST", "/widgets", "name=new&count=bad&status=active"}, {"GET", "/missing", ""}} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			store := panelTestStore()
			h := coreHandler(t, store, pageTestBanner, io.Discard)
			response := formRequest(h, tc.method, tc.path, "application/x-www-form-urlencoded", tc.body)
			for _, w := range store.All() {
				if strings.Contains(response.Body.String(), w.ID) {
					t.Fatalf("body shows widget id %q", w.ID)
				}
			}
		})
	}
}

type panelObservedBody struct {
	read func()
	body io.Reader
}

func (b panelObservedBody) Read(p []byte) (int, error) { b.read(); return b.body.Read(p) }
func (panelObservedBody) Close() error                 { return nil }

type panelObservedResponse struct {
	http.ResponseWriter
	before func()
}

func (w panelObservedResponse) WriteHeader(status int) {
	w.before()
	w.ResponseWriter.WriteHeader(status)
}
func (w panelObservedResponse) Write(p []byte) (int, error) {
	w.before()
	return w.ResponseWriter.Write(p)
}

// R-CMTA-3ATT
func TestPanelStartsTrailBeforeIO(t *testing.T) {
	for _, tc := range []struct{ method, path, media, body, user string }{
		{"POST", "/widgets", "application/x-www-form-urlencoded", "name=created&count=2&status=active", "caller"},
		{"GET", "/widgets", "", "", "caller"},
		{"GET", "/widgets/table", "", "", "caller"},
		{"GET", "/unknown", "", "", "caller"},
		{"GET", "/_appkit/theme.css", "", "", "caller"},
		{"POST", "/mcp", "application/json", "broken", "caller"},
		{"POST", "/widgets", "application/x-www-form-urlencoded", "name=unused", ""},
	} {
		t.Run(tc.method+tc.path+tc.user, func(t *testing.T) {
			t.Setenv(services.Variable, "")
			writer, capture, _ := panelTestTelemetry(t, io.Discard)
			h := panel.Handler(panelTestStore(), pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
			observed := 0
			check := func() {
				observed++
				if err := writer.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				events := capture.Events()
				if len(events) == 0 || events[0].Name != "request.started" {
					t.Fatalf("IO before start %#v", events)
				}
				for _, e := range events {
					if e.Name == "request.finished" {
						t.Fatal("finish before IO complete")
					}
				}
			}
			r := pageTestRequest(tc.method, tc.path)
			r.Header.Set("X-User-Id", tc.user)
			r.Header.Set("Content-Type", tc.media)
			r.Body = panelObservedBody{check, strings.NewReader(tc.body)}
			h.ServeHTTP(panelObservedResponse{httptest.NewRecorder(), check}, r)
			if observed == 0 {
				t.Fatal("no IO observed")
			}
		})
	}
}

// R-CMTA-3ATT R-KSBT-MVET
func TestPanelMCPDomainTrail(t *testing.T) {
	t.Setenv(services.Variable, "")
	writer, capture, diagnostics := panelTestTelemetry(t, io.Discard)
	store := panelTestStore()
	server := httptest.NewServer(panel.Handler(store, pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer))
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	_, err := client.CallTool(context.Background(), identity.Caller{UserID: "reader", RequestID: "tool-trace", Email: "private@example.test"}, "create_widget", json.RawMessage(`{"name":"new","count":1,"status":"active"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	names := []string{"request.started", "widget.created", "tool.called", "request.finished"}
	if len(events) != len(names) {
		t.Fatalf("events %#v", events)
	}
	for i, e := range events {
		if e.Name != names[i] || e.RequestID != "tool-trace" || e.User != "reader" {
			t.Fatalf("event %#v", e)
		}
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("stderr %q", diagnostics.String())
	}
}

type panelCanceledBody struct {
	ctx     context.Context
	reading chan struct{}
}

func (b panelCanceledBody) Read([]byte) (int, error) {
	close(b.reading)
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (panelCanceledBody) Close() error { return nil }

// R-CMTA-3ATT R-CO16-H2KI
func TestPanelTrailForInterruptedRequest(t *testing.T) {
	t.Setenv(services.Variable, "")
	writer, capture, diagnostics := panelTestTelemetry(t, io.Discard)
	h := panel.Handler(panelTestStore(), pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := pageTestRequest(http.MethodPost, "/widgets").WithContext(ctx)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reading := make(chan struct{})
	r.Body = panelCanceledBody{ctx: ctx, reading: reading}
	out := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); h.ServeHTTP(out, r) }()
	<-reading
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != 1 || events[0].Name != "request.started" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": r.Method, "path": r.URL.Path}) {
		t.Fatalf("in-progress request trail: %#v", events)
	}
	cancel()
	<-done
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events = capture.Events()
	if len(events) != 2 || events[1].Name != "request.finished" || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"status": int64(out.Code), "duration_us": int64(0)}) {
		t.Fatalf("interrupted request trail: %#v", events)
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("stderr %q", diagnostics.String())
	}
}
