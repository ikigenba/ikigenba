package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/drafts"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/pages"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
)

type setup struct {
	cfg     Config
	capture *telemetry.Capture
	bus     *events.Capture
	db      *db.DB
}

func fixture(t *testing.T) setup {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC) }
	capture := &telemetry.Capture{}
	bus := &events.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: capture, Stderr: io.Discard, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 4096)), Sleep: func(context.Context, time.Duration) {}})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: bus, Stderr: io.Discard, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x7b}, 4096)), Sleep: func(context.Context, time.Duration) {}, Emits: trail.Emits(), Telemetry: w})
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state.db"), Migrations: app.Migrations(), Now: now, Service: pages.ServiceName, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(d, store.Config{Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x6c}, 4096))})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test end")
		_ = d.Close()
	})
	b := func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true}
	}
	srv := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "test-version", Telemetry: w})
	return setup{Config{b, srv, "", st, nil, nil, nil, w, em}, capture, bus, d}
}
func raw(h http.Handler, method, path, user, id string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://gmail.example.test"+path, body)
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Email", "chosen@example.test")
	if id != "" {
		r.Header.Set("X-Request-Id", id)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func flush(t *testing.T, w *telemetry.Writer) {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

type unreadBody struct{ t *testing.T }

func (b unreadBody) Read([]byte) (int, error) {
	b.t.Fatal("unauthenticated request body read")
	return 0, io.EOF
}

// R-XDJJ-T6EW R-Y35F-UCZH R-Y811-DFY9 R-XZHQ-P1RE R-Y6T4-ZO7K
func TestMissingIdentityStopsEveryProtectedRoute(t *testing.T) {
	f := fixture(t)
	h := Handler(f.cfg)
	f.db.SetFailing(true)
	for _, p := range []string{"/", "/about", "/tools", "/drafts/drf_5555555555555555", "/transfers/xfr_5555555555555555", "/_appkit/theme.css", "/mcp", "/events/", "/declarations/", "/unknown"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			flush(t, f.cfg.Telemetry)
			before := len(f.capture.Events())
			r := httptest.NewRequest(method, "https://gmail.example.test"+p, unreadBody{t})
			r.Header["X-User-Id"] = []string{"", "later-user"}
			r.Header["X-Request-Id"] = []string{"first-request", "later-request"}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := identity.MissingBody
			if method == "HEAD" {
				want = ""
			}
			if w.Code != 500 || w.Body.String() != want || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || len(w.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("missing identity", method, p)
			}
			flush(t, f.cfg.Telemetry)
			es := f.capture.Events()[before:]
			if len(es) != 2 || es[0].Name != "request.started" || es[1].Name != "request.finished" || es[0].RequestID != "first-request" || es[1].RequestID != "first-request" || es[0].User != "" || es[1].User != "" || es[0].Attrs["method"] != method || es[0].Attrs["path"] != p || es[1].Attrs["status"] != int64(500) {
				t.Fatalf("request trail %+v", es)
			}
		}
	}
	if len(f.bus.Events()) != 0 {
		t.Fatal("unauthenticated event emitted")
	}
}

// R-Y0PN-2TI3 R-Y5L8-LWGV R-Y811-DFY9 R-Y6T4-ZO7K
func TestRoutingUsesDeclaredHandlers(t *testing.T) {
	f := fixture(t)
	f.cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(f.cfg.ServicesPath, []byte(`{"services":[{"name":"auth","url":"https://chosen-auth.example.test","description":"chosen auth","socket":"","enabled":true,"mcp":false},{"name":"artifacts","url":"https://chosen-artifacts.example.test","description":"chosen artifacts","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	draft, err := f.cfg.Store.AddDraft(ctx, store.Draft{UserID: "owner", GmailDraft: "chosen-provider-draft", Message: "chosen-message", Thread: "chosen-thread", Subject: "chosen subject", To: []string{"recipient@example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.cfg.Store.AttachToDraft(ctx, draft.ID, store.Attachment{Path: "chosen/attachment", Name: "chosen attachment", ContentType: "application/example", Bytes: 4}, draft.Message, draft.Thread); err != nil {
		t.Fatal(err)
	}
	transfer, err := f.cfg.Store.AddTransfer(ctx, store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, UserID: "owner", Message: "chosen-message", Path: "chosen/result", Name: "chosen result"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.cfg.Store.FinishTransfer(ctx, transfer.ID, store.Ending{Status: store.StatusDone, Bytes: 4, ArtifactID: "chosen-artifact", URL: "https://chosen-artifacts.example.test/files/chosen/result"}); err != nil {
		t.Fatal(err)
	}
	h := Handler(f.cfg)
	s, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := identity.Require(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: s, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store}))
	static := identity.Require(page.Static())
	cases := []struct {
		path    string
		handler http.Handler
		user    string
	}{{"/events", events.DeliveryHandler(nil), ""}, {"/declarations", events.DeclarationsHandler(f.cfg.Events, nil), ""}, {"/", ph, "owner"}, {"/about", ph, "owner"}, {"/tools", ph, "owner"}, {"/drafts/drf_5555555555555555", ph, "owner"}, {"/transfers/xfr_5555555555555555", ph, "owner"}, {"/xfr_5555555555555555", ph, "owner"}, {"/nope", ph, "owner"}, {"/mcp/", ph, "owner"}, {"/events/", ph, "owner"}, {"/declarations/", ph, "owner"}, {"/_appkit", ph, "owner"}, {"/_appkit/theme.css", static, "owner"}, {"/_appkit/nope", static, "owner"}}
	cases = append(cases, struct {
		path    string
		handler http.Handler
		user    string
	}{"/drafts/" + draft.ID, ph, "owner"}, struct {
		path    string
		handler http.Handler
		user    string
	}{"/transfers/" + transfer.ID, ph, "owner"})
	for _, c := range cases {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			id := method + c.path
			flush(t, f.cfg.Telemetry)
			before := len(f.capture.Events())
			got := raw(h, method, c.path, c.user, id, nil)
			want := raw(c.handler, method, c.path, c.user, id, nil)
			if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), want.Header()) || len(got.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("route differs", method, c.path, got.Code, want.Code)
			}
			if method == http.MethodGet && c.path == "/" {
				for _, url := range []string{"https://chosen-auth.example.test/", "https://chosen-artifacts.example.test/chosen-artifact"} {
					if !strings.Contains(got.Body.String(), url) || !strings.Contains(want.Body.String(), url) {
						t.Fatal("chosen services URL not rendered", url)
					}
				}
			}
			if method == http.MethodGet && c.path == "/drafts/"+draft.ID {
				url := "https://chosen-artifacts.example.test/files/chosen/attachment"
				if !strings.Contains(got.Body.String(), url) || !strings.Contains(want.Body.String(), url) {
					t.Fatal("chosen attachment URL not rendered", url)
				}
			}
			flush(t, f.cfg.Telemetry)
			es := f.capture.Events()[before:]
			if len(es) != 2 || es[0].Name != "request.started" || es[1].Name != "request.finished" {
				t.Fatalf("route events %+v", es)
			}
			for _, e := range es {
				if e.RequestID != id || e.User != c.user {
					t.Fatal("event identity", e)
				}
			}
			if es[0].Attrs["method"] != method || es[0].Attrs["path"] != c.path || es[1].Attrs["status"] != int64(got.Code) {
				t.Fatal("trail attrs")
			}
		}
	}
	for _, user := range []string{"", "owner"} {
		got := raw(h, "GET", "/declarations", user, "declarations-test", nil)
		var dec struct {
			Emits   []events.Emission `json:"emits"`
			Accepts []any             `json:"accepts"`
		}
		if err := json.Unmarshal(got.Body.Bytes(), &dec); err != nil {
			t.Fatal(err)
		}
		if got.Code != 200 || !reflect.DeepEqual(dec.Emits, sortedEmits()) || len(dec.Accepts) != 0 {
			t.Fatalf("declarations %+v", dec)
		}
	}
}

// R-Y4DC-84Q6 R-XZHQ-P1RE R-Y6T4-ZO7K R-Y811-DFY9
func TestMCPRegistrationCallerCauseAndRequestTrail(t *testing.T) {
	f := fixture(t)
	var observed identity.Caller
	var cause events.Cause
	mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct {
		Value string `json:"value"`
	}]{Name: "probe", Description: "Observe chosen caller.", Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, _ struct{}) (struct {
		Value string `json:"value"`
	}, error) {
		observed = c
		cause, _ = events.FromContext(ctx)
		f.cfg.Telemetry.Emit(ctx, "probe.called", telemetry.Attrs{"value": "chosen"})
		return struct {
			Value string `json:"value"`
		}{"chosen"}, nil
	}})
	h := Handler(f.cfg)
	srv := httptest.NewServer(h)
	defer srv.Close()
	hc := srv.Client()
	hc.Transport = causeTransport{base: hc.Transport, t: t}
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: hc, Name: "test", Version: "chosen-version"})
	ctx := events.NewContext(context.Background(), events.Cause{ID: "evt_5555555555555555", Depth: 3})
	caller := identity.Caller{UserID: "owner", Email: "chosen@example.test", RequestID: "first-request"}
	listed, e := client.ListTools(ctx, caller)
	if e != nil {
		t.Fatal(e)
	}
	catalog := tools.Catalog()
	if len(listed) != len(catalog)+1 {
		t.Fatal("tool registration count")
	}
	for i, x := range catalog {
		if listed[i+1].Name != x.Name || !strings.HasPrefix(listed[i+1].Description, x.Description) {
			t.Fatal("tool registration order")
		}
	}
	for _, name := range []string{"transfers", "drafts"} {
		r, e := client.CallTool(ctx, caller, name, json.RawMessage(`{}`))
		if e != nil || r.IsError() {
			t.Fatal("store tool wiring", name, e, r)
		}
	}
	flush(t, f.cfg.Telemetry)
	before := len(f.capture.Events())
	r, e := client.CallTool(ctx, caller, "probe", json.RawMessage(`{}`))
	if e != nil || r.IsError() {
		t.Fatal("probe", e, r)
	}
	if observed != caller || cause != (events.Cause{ID: "evt_5555555555555555", Depth: 3}) {
		t.Fatal("context wiring", observed, cause)
	}
	flush(t, f.cfg.Telemetry)
	es := f.capture.Events()[before:]
	started, finished, called := 0, 0, 0
	for _, event := range es {
		if event.User != caller.UserID || event.RequestID != caller.RequestID {
			t.Fatal("tool event identity", event)
		}
		switch event.Name {
		case "request.started":
			started++
			if event.Attrs["method"] != "POST" || event.Attrs["path"] != "/mcp" {
				t.Fatal("start attrs")
			}
		case "request.finished":
			finished++
			if event.Attrs["status"] != int64(200) {
				t.Fatal("finish status")
			}
		case "probe.called":
			called++
		}
	}
	if started != 1 || finished != 1 || called != 1 {
		t.Fatal("request trail", es)
	}
	caller.RequestID = ""
	if _, e := client.CallTool(ctx, caller, "probe", json.RawMessage(`{}`)); e != nil {
		t.Fatal(e)
	}
	if observed.UserID != caller.UserID || observed.Email != caller.Email || observed.RequestID != strings.Repeat("5a", 16) {
		t.Fatal("minted request id", observed)
	}
}

func sortedEmits() []events.Emission {
	e := trail.Emits()
	sort.Slice(e, func(i, j int) bool { return e[i].Event < e[j].Event })
	return e
}

func sibling(t *testing.T, name string, h http.Handler) services.Entry {
	t.Helper()
	dir, err := os.MkdirTemp("", "scratch.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, name)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return services.Entry{Name: name, Socket: path, URL: "https://" + name + ".example.test"}
}

// R-Y4DC-84Q6
func TestMCPUsesConfiguredGmailDraftsAndTransfers(t *testing.T) {
	f := fixture(t)
	caller := identity.Caller{UserID: "configured-owner", Email: "configured@example.test", RequestID: "configured-request"}
	now := func() time.Time { return time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC) }
	type call struct {
		service, method, path, format string
		header                        http.Header
		body                          []byte
	}
	var mu sync.Mutex
	var calls []call
	record := func(service string, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call{service, r.Method, r.URL.Path, r.URL.Query().Get("format"), r.Header.Clone(), b})
	}
	cc := connections.NewClient(connections.ClientConfig{Telemetry: f.cfg.Telemetry, Service: sibling(t, "connections", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("connections", r)
		if r.Method != http.MethodGet || r.URL.Path != "/token/google" {
			t.Error("unexpected connection request", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "configured-token", "expires": now().Add(time.Hour).Format(time.RFC3339), "account": "configured@example.test", "scopes": []string{}})
	}))})
	ac := artifacts.NewClient(artifacts.ClientConfig{Telemetry: f.cfg.Telemetry, Service: sibling(t, "artifacts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("artifacts", r)
		if r.URL.Path != "/files/chosen/result" {
			t.Error("unexpected artifact path", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(404)
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "configured-artifact", "path": "chosen/result", "bytes": 4, "content_type": "application/example", "sha256": "fixture-hash", "owner": caller.UserID, "created": now().Format(time.RFC3339), "modified": now().Format(time.RFC3339), "url": "https://artifacts.example.test/files/chosen/result"})
		default:
			t.Error("unexpected artifact method", r.Method)
			w.WriteHeader(500)
		}
	}))})
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("gmail", r)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/labels":
			_, _ = io.WriteString(w, `{"labels":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/drafts":
			_, _ = io.WriteString(w, `{"id":"configured-remote-draft","message":{"id":"configured-draft-message","threadId":"configured-thread"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages/configured-message/attachments/configured-part":
			_ = json.NewEncoder(w).Encode(map[string]any{"size": 4, "data": base64.RawURLEncoding.EncodeToString([]byte("data"))})
		case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages/configured-message":
			_, _ = io.WriteString(w, `{"id":"configured-message","threadId":"configured-thread","payload":{"headers":[{"name":"Subject","value":"configured subject"}],"parts":[{"partId":"part","filename":"configured.bin","mimeType":"application/example","body":{"size":4,"attachmentId":"configured-part"}}]}}`)
		default:
			t.Error("unexpected Gmail request", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(gs.Close)
	f.cfg.Gmail = gmail.NewClient(gmail.Config{HTTP: gs.Client(), BaseURL: gs.URL, Telemetry: f.cfg.Telemetry})
	f.cfg.Drafts = drafts.New(drafts.Config{Store: f.cfg.Store, Gmail: f.cfg.Gmail, Connections: cc, Telemetry: f.cfg.Telemetry, Events: f.cfg.Events})
	f.cfg.Transfers = transfers.New(transfers.Config{Store: f.cfg.Store, Gmail: f.cfg.Gmail, Artifacts: ac, Connections: cc, Drafts: f.cfg.Drafts, Telemetry: f.cfg.Telemetry, Events: f.cfg.Events, MaxBytes: 1024, MaxActive: 1, MaxQueued: 1, Now: now})
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f.cfg.Transfers.Drain(ctx)
	})
	srv := httptest.NewServer(Handler(f.cfg))
	t.Cleanup(srv.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	invoke := func(name string, args any, output any) {
		t.Helper()
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		r, err := client.CallTool(context.Background(), caller, name, b)
		if err != nil || r.IsError() {
			t.Fatal(name, err, r)
		}
		wire, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Structured json.RawMessage `json:"structuredContent"`
		}
		if err := json.Unmarshal(wire, &result); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(result.Structured, output); err != nil {
			t.Fatal(err)
		}
	}
	var shown tools.Message
	invoke("show", tools.ShowArgs{Message: "configured-message"}, &shown)
	if shown.ID != "configured-message" || shown.Subject != "configured subject" || len(shown.Attachments) != 1 || shown.Attachments[0].ID != "part" {
		t.Fatalf("configured Gmail result: %+v", shown)
	}
	subject := "chosen composition"
	var composed tools.Draft
	invoke("compose", tools.ComposeArgs{To: []string{"recipient@example.test"}, Cc: []string{}, Bcc: []string{}, Subject: &subject, Body: "chosen body"}, &composed)
	d, err := f.cfg.Store.GetDraft(context.Background(), caller.UserID, composed.ID)
	if err != nil || composed.Draft == nil || *composed.Draft != "configured-remote-draft" || d.GmailDraft != *composed.Draft || d.Subject != subject || d.Body != "chosen body" || !reflect.DeepEqual(d.To, []string{"recipient@example.test"}) {
		t.Fatal("configured draft service", composed, d, err)
	}
	var pulled tools.Record
	invoke("pull", tools.PullArgs{Message: shown.ID, Attachment: shown.Attachments[0].ID, Path: "chosen/result"}, &pulled)
	if pulled.Message != shown.ID || pulled.Path != "chosen/result" || pulled.Status != store.StatusRunning {
		t.Fatal("configured transfer result", pulled)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f.cfg.Transfers.Drain(ctx)
	x, err := f.cfg.Store.GetTransfer(context.Background(), caller.UserID, pulled.ID)
	if err != nil || x.Status != store.StatusDone || x.ArtifactID != "configured-artifact" || x.Bytes != 4 || x.UserID != caller.UserID || x.UserEmail != caller.Email || x.RequestID != caller.RequestID {
		t.Fatal("configured transfer service", x, err)
	}
	mu.Lock()
	snapshot := append([]call(nil), calls...)
	mu.Unlock()
	counts := map[string]int{}
	for _, req := range snapshot {
		counts[req.service+" "+req.method+" "+req.path]++
		if req.service == "gmail" {
			if req.header.Get("Authorization") != "Bearer configured-token" {
				t.Fatal("configured token not used", req)
			}
			if req.method == http.MethodPost {
				var payload struct {
					Message struct {
						Raw string `json:"raw"`
					} `json:"message"`
				}
				if err := json.Unmarshal(req.body, &payload); err != nil {
					t.Fatal(err)
				}
				mail, err := base64.RawURLEncoding.DecodeString(payload.Message.Raw)
				if err != nil || !bytes.Contains(mail, []byte(subject)) || !bytes.Contains(mail, []byte(base64.StdEncoding.EncodeToString([]byte("chosen body")))) || !bytes.Contains(mail, []byte("recipient@example.test")) {
					t.Fatal("composition did not reach chosen Gmail", err)
				}
			} else if req.path == "/gmail/v1/users/me/messages/configured-message" && req.format != "full" {
				t.Fatal("show request format", req.format)
			}
		} else {
			if req.header.Get("X-User-Id") != caller.UserID || req.header.Get("X-User-Email") != caller.Email || req.header.Get("X-Request-Id") != caller.RequestID {
				t.Fatal("configured caller not forwarded", req)
			}
			if req.method == http.MethodPut && !bytes.Equal(req.body, []byte("data")) {
				t.Fatal("attachment bytes did not reach chosen artifacts", req.body)
			}
		}
	}
	want := map[string]int{"connections GET /token/google": 4, "gmail GET /gmail/v1/users/me/labels": 2, "gmail GET /gmail/v1/users/me/messages/configured-message": 3, "gmail POST /gmail/v1/users/me/drafts": 1, "gmail GET /gmail/v1/users/me/messages/configured-message/attachments/configured-part": 1, "artifacts HEAD /files/chosen/result": 1, "artifacts PUT /files/chosen/result": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Fatal("configured dependencies were not used", counts)
	}
}

type causeTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (tr causeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	q := r.Clone(r.Context())
	events.Forward(r.Context(), q)
	response, err := tr.base.RoundTrip(q)
	if err == nil && len(response.Header.Values("Set-Cookie")) != 0 {
		tr.t.Error("MCP cookie")
	}
	return response, err
}
