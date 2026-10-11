package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/fetch"
	"github.com/ikigenba/ikigenba/artifacts/internal/files"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/tools"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

type random struct{ n byte }

func (r *random) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

var owner = identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "req"}
var other = identity.Caller{UserID: "other", Email: "other@example.test", RequestID: "other-req"}
var now = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

const base = "https://artifacts.test"

type fixture struct {
	cfg    tools.Config
	client *mcp.Client
	db     *db.DB
	blobs  string
	tc     *telemetry.Capture
	ec     *events.Capture
}

func setup(t *testing.T, fc fetch.Config, limit int64, clocks ...func() time.Time) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	clock := func() time.Time { return now }
	toolClock := clock
	if len(clocks) > 0 {
		toolClock = clocks[0]
		if toolClock != nil {
			clock = toolClock
		}
	}
	dir := t.TempDir()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "db"), Migrations: artifacts.Migrations(), Now: clock})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	blobs := filepath.Join(dir, "blobs")
	if e = os.Mkdir(blobs, 0700); e != nil {
		t.Fatal(e)
	}
	tc := &telemetry.Capture{}
	ec := &events.Capture{}
	w := telemetry.New(telemetry.Config{Service: "artifacts", Sink: tc, Now: clock, Rand: &random{}, Stderr: io.Discard})
	em := events.New(events.Config{Service: "artifacts", Sink: ec, Emits: trail.Emits(), Now: clock, Rand: &random{}, Stderr: io.Discard})
	t.Cleanup(func() { em.Shutdown(context.Background()); w.Shutdown(context.Background(), "test") })
	fc.Telemetry = w
	cfg := tools.Config{Store: store.New(d, blobs, store.Config{Now: clock, Rand: &random{}}), Telemetry: w, Events: em, Max: limit, Now: toolClock, Fetch: fc}
	srv := mcp.NewServer(mcp.ServerConfig{Name: "artifacts", Telemetry: w})
	tools.Register(srv, cfg)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(events.NewContext(urls.NewContext(r.Context(), base), events.Cause{ID: "evt_0123456789abcdef", Depth: 2})))
	})
	s := httptest.NewServer(identity.Require(h))
	t.Cleanup(s.Close)
	return &fixture{cfg: cfg, client: mcp.NewClient(mcp.ClientConfig{Endpoint: s.URL}), db: d, blobs: blobs, tc: tc, ec: ec}
}
func (f *fixture) call(t *testing.T, c identity.Caller, name string, args any) mcp.Result {
	t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.client.CallTool(context.Background(), c, name, b)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func structured(t *testing.T, r mcp.Result, out any) {
	t.Helper()
	if r.IsError() {
		b, _ := json.Marshal(r)
		t.Fatalf("error result %s", b)
	}
	b, _ := json.Marshal(r)
	var v struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	if e := json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal(v.Structured, out); e != nil {
		t.Fatal(e)
	}
	actual := map[string]any{}
	expected := map[string]any{}
	encoded, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(v.Structured, &actual); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(encoded, &expected); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("structured content has unexpected members: %s expected %s", v.Structured, encoded)
	}
}

// R-PYGZ-0J9L
func refused(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Content    []struct{ Type, Text string }
		Structured json.RawMessage `json:"structuredContent"`
	}
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if !r.IsError() || len(v.Content) != 1 || v.Content[0].Type != "text" || v.Content[0].Text != want || len(v.Structured) != 0 {
		t.Fatalf("want %q got %s", want, b)
	}
}
func (f *fixture) put(t *testing.T, path string, c identity.Caller) store.Artifact {
	t.Helper()
	a, _, e := f.cfg.Store.Put(context.Background(), store.Draft{Path: path, OwnerID: c.UserID, OwnerEmail: c.Email, Max: 100, Expires: now.Add(24 * time.Hour)}, strings.NewReader("seed bytes"))
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func (f *fixture) events(t *testing.T) []events.Event {
	t.Helper()
	if e := f.cfg.Events.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := f.cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	es := f.ec.Events()
	if len(es) == 0 {
		for _, event := range f.tc.Events() {
			if strings.HasPrefix(event.Name, "artifact.") {
				t.Fatal(event)
			}
		}
	}
	return es
}
func (f *fixture) snapshot(t *testing.T) string {
	t.Helper()
	all, e := f.cfg.Store.List(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	data := map[string]any{"records": all}
	bs, e := os.ReadDir(f.blobs)
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range bs {
		v, e := os.ReadFile(filepath.Join(f.blobs, b.Name()))
		if e != nil {
			t.Fatal(e)
		}
		data[b.Name()] = string(v)
	}
	v, _ := json.Marshal(data)
	return string(v)
}

// R-M0A9-5KIF R-Z876-HU2X
func TestCatalogDescriptions(t *testing.T) {
	const guide string = tools.BytesGuide
	if guide == "" || strings.Contains(guide, "\n") {
		t.Fatal("byte guide must be a non-empty paragraph without newlines")
	}
	catalog := tools.Catalog()
	names := []string{"list", "show", "delete", "import"}
	if len(catalog) != len(names) {
		t.Fatalf("catalog has %d entries, want %d", len(catalog), len(names))
	}
	firstLines := make(map[string]bool)
	for i, entry := range catalog {
		first, _, _ := strings.Cut(entry.Description, "\n")
		if entry.Name != names[i] || first == "" || firstLines[first] || entry.Description != first+"\n\n"+guide || tools.FirstLine(entry.Description) != first {
			t.Fatalf("invalid catalog entry %d: %#v", i, entry)
		}
		firstLines[first] = true
	}
}

// R-Z876-HU2X
func TestFirstLine(t *testing.T) {
	for _, tc := range []struct{ description, want string }{
		{"", ""},
		{"one", "one"},
		{"one\ntwo", "one"},
		{"\nrest", ""},
		{"one\n\ntwo\nthree", "one"},
		{"one\n", "one"},
	} {
		if got := tools.FirstLine(tc.description); got != tc.want {
			t.Fatalf("FirstLine(%q) = %q, want %q", tc.description, got, tc.want)
		}
	}
}

// R-ND3R-Z7MO R-NEBO-CZDD R-NFJK-QR42 R-NGRH-4IUR R-NJ79-W2C5 R-TY44-J0F8
func TestPublicContracts(t *testing.T) {
	f := setup(t, fetch.Config{}, 100)
	catalog := tools.Catalog()
	infos, e := f.client.ListTools(context.Background(), owner)
	if e != nil {
		t.Fatal(e)
	}
	if len(infos) != 4 || len(catalog) != 4 {
		t.Fatal(infos)
	}
	for i, name := range []string{"list", "show", "delete", "import"} {
		if infos[i].Name != name || catalog[i].Name != name || catalog[i].Description == "" || infos[i].Description != catalog[i].Description {
			t.Fatal(infos)
		}
		effect := mcp.Read
		if i >= 2 {
			effect = mcp.Destructive
		}
		if infos[i].Effect() != effect {
			t.Fatal(infos[i])
		}
	}
	for _, fn := range []func(string) string{tools.NoArtifact, tools.InvalidPath, tools.NotOwner, tools.InvalidExpiry, tools.PastExpiry, tools.NotOnSpace, tools.FetchUnanswered, tools.FetchTimedOut, tools.FetchCutOff} {
		if !strings.Contains(fn("fixture"), "fixture") {
			t.Fatal("missing copy argument")
		}
	}
	if !strings.Contains(tools.FetchFailed(409), strconv.Itoa(409)) || !strings.Contains(tools.TooLarge(123), "123") {
		t.Fatal("missing numeric argument")
	}
	for i, info := range infos {
		var schema struct {
			Type       string
			Required   []string
			Properties map[string]struct{ Type string }
		}
		if e = json.Unmarshal(info.InputSchema, &schema); e != nil {
			t.Fatal(e)
		}
		wantProperties := map[string]struct{ Type string }{}
		required := map[string]bool{}
		if i > 0 {
			wantProperties["path"] = struct{ Type string }{"string"}
			required["path"] = true
		}
		if i == 3 {
			wantProperties["url"] = struct{ Type string }{"string"}
			wantProperties["expires"] = struct{ Type string }{"string"}
			required["url"] = true
		}
		gotRequired := map[string]bool{}
		for _, name := range schema.Required {
			gotRequired[name] = true
		}
		if schema.Type != "object" || len(schema.Properties) != len(wantProperties) || !reflect.DeepEqual(gotRequired, required) {
			t.Fatal(info.Name, schema)
		}
		for name, want := range wantProperties {
			if schema.Properties[name] != want {
				t.Fatal(info.Name, schema)
			}
		}
	}
	b, _ := json.Marshal(tools.ImportArgs{URL: "url", Path: "path"})
	var args map[string]any
	_ = json.Unmarshal(b, &args)
	if args["url"] != "url" || args["path"] != "path" {
		t.Fatal(args)
	}
	b, _ = json.Marshal(tools.Listed{ID: "i", Path: "p", Bytes: 2, ContentType: "ct", SHA256: "hash", Owner: "mail", Modified: "date", URL: "url"})
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	if len(fields) != 8 || fields["bytes"] != float64(2) || fields["created"] != nil {
		t.Fatal(fields)
	}
}

// R-NKF6-9U2U R-NLN2-NLTJ
func TestListShow(t *testing.T) {
	f := setup(t, fetch.Config{}, 100)
	var list tools.ArtifactList
	structured(t, f.call(t, owner, "list", tools.ListArgs{}), &list)
	if list.Artifacts == nil || len(list.Artifacts) != 0 {
		t.Fatal(list)
	}
	a := f.put(t, "z", owner)
	b := f.put(t, "a", other)
	structured(t, f.call(t, other, "list", tools.ListArgs{}), &list)
	want := make([]tools.Listed, 0)
	for _, v := range []store.Artifact{b, a} {
		m := files.Describe(v, base)
		want = append(want, tools.Listed{ID: m.ID, Path: m.Path, Bytes: m.Bytes, ContentType: m.ContentType, SHA256: m.SHA256, Owner: m.Owner, Modified: m.Modified, Expires: m.Expires, URL: m.URL})
	}
	if !reflect.DeepEqual(list.Artifacts, want) {
		t.Fatal(list, want)
	}
	var got files.Metadata
	structured(t, f.call(t, other, "show", tools.PathArgs{Path: a.Path}), &got)
	if !reflect.DeepEqual(got, files.Describe(a, base)) {
		t.Fatal(got)
	}
}

// R-NMUZ-1DK8 R-NO2V-F5AX R-NPAR-SX1M R-NQIO-6OSB
func TestDeleteAndRefusals(t *testing.T) {
	f := setup(t, fetch.Config{}, 100)
	a := f.put(t, "keep", owner)
	snap := f.snapshot(t)
	for _, name := range []string{"show", "delete"} {
		for _, p := range []string{"a//b", "absent"} {
			want := tools.NoArtifact(p)
			if p == "a//b" {
				want = tools.InvalidPath(p)
			}
			refused(t, f.call(t, owner, name, tools.PathArgs{Path: p}), want)
		}
	}
	refused(t, f.call(t, other, "delete", tools.PathArgs{Path: a.Path}), tools.NotOwner(a.Path))
	if f.snapshot(t) != snap || len(f.events(t)) != 0 {
		t.Fatal("refusal changed state")
	}
	f.db.SetFailing(true)
	for _, name := range []string{"list", "show", "delete"} {
		args := any(tools.PathArgs{Path: a.Path})
		if name == "list" {
			args = tools.ListArgs{}
		}
		refused(t, f.call(t, owner, name, args), store.Unreachable)
	}
	f.db.SetFailing(false)
	if f.snapshot(t) != snap || len(f.events(t)) != 0 {
		t.Fatal("database refusal changed state")
	}
	var got tools.Deleted
	structured(t, f.call(t, owner, "delete", tools.PathArgs{Path: a.Path}), &got)
	if got != (tools.Deleted{Deleted: true, ID: a.ID}) {
		t.Fatal(got)
	}
	refused(t, f.call(t, owner, "show", tools.PathArgs{Path: a.Path}), tools.NoArtifact(a.Path))
	es := f.events(t)
	if len(es) != 1 || es[0].Name != trail.Deleted || es[0].User != owner.UserID || es[0].RequestID != owner.RequestID || es[0].Attrs["artifact"] != a.ID || es[0].Cause != "evt_0123456789abcdef" || es[0].Depth != 3 || !reflect.DeepEqual(es[0].Attrs, events.Attrs{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}) {
		t.Fatal(es)
	}
	n := 0
	for _, e := range f.tc.Events() {
		if e.Name == trail.Deleted {
			n++
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
	assertLifecycle(t, f, trail.Deleted, a, 0)
}
func sibling(t *testing.T, h http.HandlerFunc) fetch.Config {
	t.Helper()
	dir, e := os.MkdirTemp("", "import-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	l, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close() })
	p := filepath.Join(t.TempDir(), "services.json")
	b, _ := json.Marshal(map[string]any{"services": []map[string]any{{"name": "scripts", "url": "http://scripts.test", "socket": socket, "description": "fixture", "enabled": true, "mcp": true}}})
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	return fetch.Config{ServicesPath: p, Limit: time.Minute, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
}

const source = "http://scripts.test/output"

// R-O7L9-JH61 R-O8T5-X8WQ
func TestImportCreateReplace(t *testing.T) {
	var phase atomic.Int64
	var seconds atomic.Int64
	payloads := []string{"imported body", "replacement contents", "final"}
	types := []string{"custom/import", "custom/replaced", "custom/final"}
	fc := sibling(t, func(w http.ResponseWriter, _ *http.Request) {
		i := phase.Load()
		w.Header().Set("Content-Type", types[i])
		_, _ = io.WriteString(w, payloads[i])
	})
	f := setup(t, fc, 100, func() time.Time { return now.Add(time.Duration(seconds.Load()) * time.Second) })
	expiry := now.Add(time.Hour).Format(time.RFC3339)
	newExpiry := now.Add(2 * time.Hour).Format(time.RFC3339)
	var first files.Metadata
	var previous store.Artifact
	for i, exp := range []*string{&expiry, &newExpiry, nil} {
		phase.Store(int64(i))
		seconds.Store(int64(i) * 60)
		var got files.Metadata
		structured(t, f.call(t, owner, "import", tools.ImportArgs{URL: source, Path: "new", Expires: exp}), &got)
		a, e := f.cfg.Store.Get(context.Background(), "new")
		if e != nil {
			t.Fatal(e)
		}
		if a.OwnerID != owner.UserID || a.OwnerEmail != owner.Email || a.ContentType != types[i] || a.Bytes != int64(len(payloads[i])) || !reflect.DeepEqual(got, files.Describe(a, base)) || !a.Modified.Equal(now.Add(time.Duration(i)*time.Minute)) {
			t.Fatal(a, got)
		}
		if exp != nil {
			want, _ := time.Parse(time.RFC3339, *exp)
			if !a.Expires.Equal(want) {
				t.Fatal(a)
			}
		} else if !a.Expires.IsZero() {
			t.Fatal(a)
		}
		if i == 0 {
			first = got
		} else if got.ID != first.ID || got.Created != first.Created || a.SHA256 == previous.SHA256 {
			t.Fatal(a, previous)
		}
		_, blob, e := f.cfg.Store.Open(context.Background(), a.Path)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(blob)
		_ = blob.Close()
		if e != nil || string(b) != payloads[i] {
			t.Fatal(string(b), e)
		}
		es := f.events(t)
		name := trail.Replaced
		if i == 0 {
			name = trail.Created
		}
		if len(es) != i+1 {
			t.Fatal(es)
		}
		assertLifecycle(t, f, name, a, i)
		previous = a
	}
}
func assertLifecycle(t *testing.T, f *fixture, name string, a store.Artifact, index int) {
	t.Helper()
	es := f.events(t)
	event := es[index]
	attrs := events.Attrs{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}
	if event.Name != name || event.User != owner.UserID || event.RequestID != owner.RequestID || event.Cause != "evt_0123456789abcdef" || event.Depth != 3 || !reflect.DeepEqual(event.Attrs, attrs) {
		t.Fatal(event)
	}
	var recorded []telemetry.Event
	for _, e := range f.tc.Events() {
		if strings.HasPrefix(e.Name, "artifact.") {
			recorded = append(recorded, e)
		}
	}
	if len(recorded) != index+1 {
		t.Fatal(recorded)
	}
	e := recorded[index]
	if e.Name != name || e.User != owner.UserID || e.RequestID != owner.RequestID || !reflect.DeepEqual(e.Attrs, telemetry.Attrs(attrs)) {
		t.Fatal(e)
	}
}

// R-P1XO-NJT5 R-OCGV-2K4T
func TestImportChecksPrecedence(t *testing.T) {
	requests := make(chan struct{}, 10)
	fc := sibling(t, func(w http.ResponseWriter, _ *http.Request) { requests <- struct{}{}; _, _ = io.WriteString(w, "body") })
	f := setup(t, fc, 100)
	f.put(t, "owned", other)
	snap := f.snapshot(t)
	bad := "bad"
	past := now.Add(900 * time.Millisecond).Format(time.RFC3339Nano)
	for _, tc := range []struct {
		args    tools.ImportArgs
		want    string
		failing bool
	}{{tools.ImportArgs{URL: "outside", Path: "a//b", Expires: &bad}, tools.InvalidPath("a//b"), true}, {tools.ImportArgs{URL: "outside", Path: "owned", Expires: &bad}, tools.InvalidExpiry(bad), true}, {tools.ImportArgs{URL: "outside", Path: "owned", Expires: &past}, tools.PastExpiry(past), true}, {tools.ImportArgs{URL: "outside", Path: "owned"}, tools.NotOnSpace("outside"), true}, {tools.ImportArgs{URL: source, Path: "owned"}, store.Unreachable, true}, {tools.ImportArgs{URL: source, Path: "owned"}, tools.NotOwner("owned"), false}} {
		f.db.SetFailing(tc.failing)
		refused(t, f.call(t, owner, "import", tc.args), tc.want)
		f.db.SetFailing(false)
		if f.snapshot(t) != snap || len(f.events(t)) != 0 {
			t.Fatal("changed on refusal")
		}
	}
	select {
	case <-requests:
		t.Fatal("precheck made request")
	default:
	}
}

// R-OB8Y-OSE4 R-OCGV-2K4T R-P1XO-NJT5
func TestImportFetchRefusals(t *testing.T) {
	for _, kind := range []string{"status", "cutoff", "large", "timeout", "unanswered", "db-put", "race-owner"} {
		t.Run(kind, func(t *testing.T) {
			timer := make(chan time.Time, 1)
			var f *fixture
			fc := sibling(t, func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "status":
					w.WriteHeader(302)
				case "cutoff":
					c, _, e := w.(http.Hijacker).Hijack()
					if e != nil {
						t.Error(e)
						return
					}
					_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 9\r\n\r\nshort")
					_ = c.Close()
				case "unanswered":
					c, _, e := w.(http.Hijacker).Hijack()
					if e != nil {
						t.Error(e)
						return
					}
					_ = c.Close()
				case "timeout":
					w.Header().Set("Content-Length", "9")
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					timer <- time.Time{}
					<-r.Context().Done()
				case "db-put":
					f.db.SetFailing(true)
					_, _ = io.WriteString(w, "body")
				case "race-owner":
					w.Header().Set("Content-Length", "4")
					_, _ = io.WriteString(w, "bo")
					w.(http.Flusher).Flush()
					ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
					defer cancel()
					for {
						entries, e := os.ReadDir(f.blobs)
						if e != nil {
							t.Error(e)
							return
						}
						streaming := false
						for _, entry := range entries {
							info, e := entry.Info()
							if e == nil && info.Size() == 2 {
								streaming = true
							}
						}
						if streaming {
							break
						}
						select {
						case <-ctx.Done():
							t.Error("import did not stream prefix")
							return
						default:
							runtime.Gosched()
						}
					}
					f.put(t, "race", other)
					_, _ = io.WriteString(w, "dy")
				default:
					_, _ = io.WriteString(w, "oversized")
				}
			})
			fc.After = func(time.Duration) <-chan time.Time { return timer }
			limit := int64(100)
			if kind == "large" {
				limit = 4
			}
			f = setup(t, fc, limit)
			a := f.put(t, "keep", owner)
			original := a
			snap := f.snapshot(t)
			path := a.Path
			if kind == "race-owner" {
				path = "race"
			}
			want := tools.TooLarge(4)
			switch kind {
			case "status":
				want = tools.FetchFailed(302)
			case "cutoff":
				want = tools.FetchCutOff(source)

			case "timeout":
				want = tools.FetchTimedOut(source)
			case "unanswered":
				want = tools.FetchUnanswered(source)
			case "db-put":
				want = store.Unreachable
			case "race-owner":
				want = tools.NotOwner(path)
			}
			refused(t, f.call(t, owner, "import", tools.ImportArgs{URL: source, Path: path}), want)
			f.db.SetFailing(false)
			if kind != "race-owner" && f.snapshot(t) != snap {
				t.Fatal("refusal changed state")
			}
			if kind == "race-owner" {
				a, e := f.cfg.Store.Get(context.Background(), "race")
				if e != nil || a.OwnerID != other.UserID || a.ContentType != store.DefaultContentType || !a.Expires.Equal(now.Add(24*time.Hour)) {
					t.Fatal(a, e)
				}
				_, blob, e := f.cfg.Store.Open(context.Background(), "race")
				if e != nil {
					t.Fatal(e)
				}
				b, e := io.ReadAll(blob)
				_ = blob.Close()
				if e != nil || string(b) != "seed bytes" {
					t.Fatal(string(b), e)
				}
				entries, e := os.ReadDir(f.blobs)
				if e != nil || len(entries) != 2 {
					t.Fatal(entries, e)
				}
				keep, e := f.cfg.Store.Get(context.Background(), "keep")
				if e != nil || !reflect.DeepEqual(keep, original) {
					t.Fatal(keep, e)
				}
				_, keepBlob, e := f.cfg.Store.Open(context.Background(), "keep")
				if e != nil {
					t.Fatal(e)
				}
				keepBytes, e := io.ReadAll(keepBlob)
				_ = keepBlob.Close()
				if e != nil || string(keepBytes) != "seed bytes" {
					t.Fatal(string(keepBytes), e)
				}
			}
			if len(f.events(t)) != 0 {
				t.Fatal("refusal emitted event")
			}
			for _, e := range f.tc.Events() {
				if strings.HasPrefix(e.Name, "artifact.") {
					t.Fatal(e)
				}
			}
		})
	}
}

// R-O7L9-JH61 R-OCGV-2K4T
func TestImportDefaultContentTypeAndExactCap(t *testing.T) {
	fc := sibling(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = io.WriteString(w, "body")
	})
	f := setup(t, fc, 4)
	var got files.Metadata
	structured(t, f.call(t, owner, "import", tools.ImportArgs{URL: source, Path: "exact"}), &got)
	a, e := f.cfg.Store.Get(context.Background(), "exact")
	if e != nil || a.ContentType != store.DefaultContentType || a.Bytes != 4 || !a.Expires.IsZero() || !reflect.DeepEqual(got, files.Describe(a, base)) {
		t.Fatal(a, e, got)
	}
}

// R-ND3R-Z7MO
func TestDefaultImportClock(t *testing.T) {
	f := setup(t, fetch.Config{}, 100, nil)
	past := time.Time{}.Format(time.RFC3339)
	refused(t, f.call(t, owner, "import", tools.ImportArgs{URL: "outside", Path: "valid", Expires: &past}), tools.PastExpiry(past))
}
