package panel_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func panelDatabaseStore(t *testing.T) (*widget.Store, *db.DB) {
	t.Helper()
	handle, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "widgets.db"), Migrations: dummy.Migrations(), Now: func() time.Time { return time.Unix(1000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	})
	data := make([]byte, 16*4096)
	for i := range data {
		data[i] = byte(i/16) ^ byte(i/4096)
	}
	return widget.NewStore(handle, bytes.NewReader(data)), handle
}
func panelEmptyStore(t *testing.T) *widget.Store { t.Helper(); s, _ := panelDatabaseStore(t); return s }
func panelTestStore(t *testing.T) *widget.Store {
	t.Helper()
	s := panelEmptyStore(t)
	for _, d := range []widget.Draft{{Name: "alpha", Count: 3, Status: widget.StatusActive}, {Name: "beta", Count: 0, Status: widget.StatusPaused}, {Name: "gamma", Count: 12, Status: widget.StatusRetired}} {
		panelStoreCreate(t, s, d)
	}
	return s
}
func panelStoreAll(t *testing.T, s *widget.Store) []widget.Widget {
	t.Helper()
	values, err := s.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return values
}
func panelStoreCreate(t *testing.T, s *widget.Store, d widget.Draft) (widget.Widget, widget.FieldErrors) {
	t.Helper()
	w, e, err := s.Create(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	return w, e
}
func panelStoreCheck(t *testing.T, s *widget.Store, d widget.Draft) widget.FieldErrors {
	t.Helper()
	e, err := s.Check(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func panelTestTelemetry(t *testing.T, stderr io.Writer) (*telemetry.Writer, *telemetry.Capture, *bytes.Buffer) {
	t.Helper()
	return panelTestTelemetryWithClock(t, stderr, func() time.Time { return time.Unix(1700000000, 0) })
}

func panelTestTelemetryWithClock(t *testing.T, stderr io.Writer, now func() time.Time) (*telemetry.Writer, *telemetry.Capture, *bytes.Buffer) {
	t.Helper()
	capture := &telemetry.Capture{}
	diagnostics := &bytes.Buffer{}
	writer := telemetry.New(telemetry.Config{Service: panel.ServiceName, Version: "test display", Sink: capture, Stderr: io.MultiWriter(diagnostics, stderr), Now: now, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 65536))})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "cleanup") })
	return writer, capture, diagnostics
}

// R-8AR9-SETA R-KSBT-MVET R-KTJQ-0N5I
// R-CO16-H2KI R-HVXC-2WUC R-L6YM-84B5
func TestPanelRequestTrail(t *testing.T) {
	cases := []struct {
		method, path, body, media, user string
		status                          int
		created, readsBody              bool
	}{
		{"GET", "/widgets?secret=query", "unread body", "", "reader", 200, false, false},
		{"GET", "/", "unread body", "", "reader", 303, false, false},
		{"HEAD", "/widgets", "", "", "reader", 200, false, false},
		{"HEAD", "/widgets/table", "", "", "reader", 200, false, false},
		{"GET", "/missing", "", "", "reader", 404, false, false},
		{"PUT", "/widgets", "", "", "reader", 405, false, false},
		{"POST", "/widgets", "name=new&count=4&status=active", "application/x-www-form-urlencoded", "reader", 303, true, true},
		{"POST", "/widgets", "name=alpha&count=4&status=active", "application/x-www-form-urlencoded", "reader", 422, false, true},
		{"POST", "/widgets", "name=new&count=4&status=active", "text/plain", "reader", 415, false, false},
		{"GET", "/_appkit/theme.css", "", "", "reader", 200, false, false},
		{"HEAD", "/_appkit/theme.css", "", "", "reader", 200, false, false},
		{"GET", "/_appkit/unknown", "", "", "reader", 404, false, false},
		{"POST", "/mcp", "broken", "application/json", "reader", 400, false, true},
		{"GET", "/widgets", "", "", "", 500, false, false},
		{"HEAD", "/widgets", "", "", "", 500, false, false},
		{"POST", "/widgets", "name=unread&count=4&status=active", "application/x-www-form-urlencoded", "", 500, false, false},
		{"POST", "/mcp", "unread body", "application/json", "", 500, false, false},
		{"GET", "/_appkit/theme.css", "", "", "", 500, false, false},
	}
	for _, tc := range cases {
		for _, headers := range []struct {
			name, requestID string
			present         bool
		}{{"provided", "trace", true}, {"empty", "", true}, {"absent", "", false}} {
			t.Run(tc.method+tc.path+tc.user+headers.name, func(t *testing.T) {
				t.Setenv(services.Variable, "")
				writer, capture, diagnostics := panelTestTelemetry(t, io.Discard)
				store := panelTestStore(t)
				h := panel.Handler(store, pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
				r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				if headers.present || tc.user != "" {
					r.Header["X-User-Id"] = []string{tc.user, "ignored"}
				}
				if headers.present {
					r.Header["X-Request-Id"] = []string{headers.requestID, "ignored"}
				}
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
				id := headers.requestID
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
				requestBytes := int64(0)
				if tc.readsBody {
					requestBytes = int64(len(tc.body))
				}
				last := events[len(events)-1]
				if last.Name != "request.finished" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"status": int64(tc.status), "duration_us": int64(0), "request_bytes": requestBytes, "response_bytes": int64(out.Body.Len())}) {
					t.Fatalf("finish %#v", last)
				}
				if tc.created {
					all := panelStoreAll(t, store)
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

// R-8AR9-SETA
func TestPanelPanicTrail(t *testing.T) {
	for _, tc := range []struct{ method, body string }{
		{"GET", ""},
		{"POST", "name=new&count=bad&status=active"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			t.Setenv(services.Variable, "")
			writer, capture, _ := panelTestTelemetry(t, io.Discard)
			h := panel.Handler(panelTestStore(t), func(page.User) page.Banner { panic("banner failed") }, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
			r := pageTestRequest(tc.method, "/widgets")
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Body = io.NopCloser(strings.NewReader(tc.body))
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
			if len(events) != 2 || events[0].Name != "request.started" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": tc.method, "path": r.URL.Path}) || events[1].Name != "request.finished" || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"status": int64(500), "duration_us": int64(0), "request_bytes": int64(len(tc.body)), "response_bytes": int64(0)}) {
				t.Fatalf("events %#v", events)
			}
		})
	}
}

// R-GWK8-ZKC7
func TestPanelKeepsWidgetIDsOutOfBodies(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{{"GET", "/widgets", ""}, {"GET", "/widgets/table", ""}, {"POST", "/widgets", "name=new&count=bad&status=active"}, {"GET", "/missing", ""}} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			store := panelTestStore(t)
			h := coreHandler(t, store, pageTestBanner, io.Discard)
			response := formRequest(h, tc.method, tc.path, "application/x-www-form-urlencoded", tc.body)
			for _, w := range panelStoreAll(t, store) {
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

// R-8AR9-SETA
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
			h := panel.Handler(panelTestStore(t), pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
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

// R-8AR9-SETA R-KSBT-MVET
func TestPanelMCPDomainTrail(t *testing.T) {
	t.Setenv(services.Variable, "")
	writer, capture, diagnostics := panelTestTelemetry(t, io.Discard)
	store := panelTestStore(t)
	h := panel.Handler(store, pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
	counts := make(chan telemetry.Attrs, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody bytes.Buffer
		original := r.Body
		defer func() {
			if err := original.Close(); err != nil {
				t.Errorf("close MCP request body: %v", err)
			}
		}()
		r.Body = io.NopCloser(io.TeeReader(original, &requestBody))
		var responseBytes int64
		h.ServeHTTP(panelCountingResponse{ResponseWriter: w, bytes: &responseBytes}, r)
		counts <- telemetry.Attrs{"status": int64(http.StatusOK), "duration_us": int64(0), "request_bytes": int64(requestBody.Len()), "response_bytes": responseBytes}
	}))
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	_, err := client.CallTool(context.Background(), identity.Caller{UserID: "reader", RequestID: "tool-trace", Email: "private@example.test"}, "create_widget", json.RawMessage(`{"name":"new","count":1,"status":"active"}`))
	if err != nil {
		t.Fatal(err)
	}
	measured := <-counts
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
	if !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": http.MethodPost, "path": "/mcp"}) || !reflect.DeepEqual(events[len(events)-1].Attrs, measured) {
		t.Fatalf("MCP request trail: %#v; measured %#v", events, measured)
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("stderr %q", diagnostics.String())
	}
}

type panelCountingResponse struct {
	http.ResponseWriter
	bytes *int64
}

func (w panelCountingResponse) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	*w.bytes += int64(n)
	return n, err
}

type panelCanceledBody struct {
	ctx     context.Context
	reading chan struct{}
	body    io.Reader
}

func (b panelCanceledBody) Read(p []byte) (int, error) {
	if n, err := b.body.Read(p); n > 0 || err != io.EOF {
		return n, err
	}
	close(b.reading)
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (panelCanceledBody) Close() error { return nil }

// R-8AR9-SETA R-CO16-H2KI
func TestPanelTrailForInterruptedRequest(t *testing.T) {
	t.Setenv(services.Variable, "")
	writer, capture, diagnostics := panelTestTelemetry(t, io.Discard)
	h := panel.Handler(panelTestStore(t), pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := pageTestRequest(http.MethodPost, "/widgets").WithContext(ctx)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reading := make(chan struct{})
	partialBody := "name=cut"
	r.Body = panelCanceledBody{ctx: ctx, reading: reading, body: strings.NewReader(partialBody)}
	r.ContentLength = int64(len(partialBody) + 100)
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
	if len(events) != 2 || events[1].Name != "request.finished" || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"status": int64(out.Code), "duration_us": int64(0), "request_bytes": int64(len(partialBody)), "response_bytes": int64(out.Body.Len())}) {
		t.Fatalf("interrupted request trail: %#v", events)
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("stderr %q", diagnostics.String())
	}
}

type panelShortResponse struct {
	*httptest.ResponseRecorder
}

func (w panelShortResponse) Write(p []byte) (int, error) {
	n, _ := w.ResponseRecorder.Write(p[:7])
	return n, io.ErrShortWrite
}

// R-8AR9-SETA
func TestPanelTrailCountsWrittenBytesAndDuration(t *testing.T) {
	t.Setenv(services.Variable, "")
	now := time.Unix(1700000000, 0)
	writer, capture, _ := panelTestTelemetryWithClock(t, io.Discard, func() time.Time { return now })
	banner := func(u page.User) page.Banner {
		now = now.Add(23 * time.Microsecond)
		return pageTestBanner(u)
	}
	h := panel.Handler(panelTestStore(t), banner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
	r := pageTestRequest(http.MethodGet, "/widgets")
	out := panelShortResponse{httptest.NewRecorder()}
	h.ServeHTTP(out, r)
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != 2 || events[0].Name != "request.started" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": r.Method, "path": r.URL.Path}) || events[1].Name != "request.finished" || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"status": int64(out.Code), "duration_us": int64(23), "request_bytes": int64(0), "response_bytes": int64(out.Body.Len())}) {
		t.Fatalf("short answer trail: %#v", events)
	}
	if out.Body.Len() != 7 {
		t.Fatalf("accepted body has %d bytes, want 7", out.Body.Len())
	}
}
