package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts"
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

func setup(t *testing.T, configuredBase string, clocks ...func() time.Time) *fixture {
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
	cfg := tools.Config{Base: configuredBase, Store: store.New(d, blobs, store.Config{Now: clock, Rand: &random{}}), Telemetry: w, Events: em, Now: toolClock}
	srv := mcp.NewServer(mcp.ServerConfig{Name: "artifacts", Telemetry: w})
	tools.Register(srv, cfg)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(events.NewContext(urls.NewContext(r.Context(), base), events.Cause{ID: "evt_0123456789abcdef", Depth: 2})))
	})
	s := httptest.NewServer(identity.Require(h))
	t.Cleanup(s.Close)
	return &fixture{cfg: cfg, client: mcp.NewClient(mcp.ClientConfig{Endpoint: s.URL}), db: d, blobs: blobs, tc: tc, ec: ec}
}

// R-DWQO-NDRN
func (f *fixture) call(t *testing.T, c identity.Caller, name string, args any) mcp.Result {
	t.Helper()
	recorded, emitted := 0, 0
	if name == "list" {
		if err := f.cfg.Telemetry.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := f.cfg.Events.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		recorded, emitted = len(f.tc.Events()), len(f.ec.Events())
	}
	b, e := json.Marshal(args)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.client.CallTool(context.Background(), c, name, b)
	if e != nil {
		t.Fatal(e)
	}
	if name == "list" {
		if err := f.cfg.Telemetry.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := f.cfg.Events.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		added := f.tc.Events()[recorded:]
		if len(added) != 1 || added[0].Name != "tool.called" || len(f.ec.Events()) != emitted {
			t.Fatal(added, f.ec.Events()[emitted:])
		}
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
	if _, ok := out.(*tools.ArtifactList); ok {
		if expected["next"] == nil {
			delete(expected, "next")
		}
		for _, value := range expected["artifacts"].([]any) {
			entry := value.(map[string]any)
			if entry["expires"] == nil {
				delete(entry, "expires")
			}
		}
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
	all, e := f.cfg.Store.List(context.Background(), nil, paging.MaxLimit)
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

// R-UMCE-YVKR R-UNKB-CNBG
func TestCatalogDescriptions(t *testing.T) {
	names := []string{"list", "show", "delete", "import"}
	guide := tools.BytesGuide
	if guide == "" || strings.Contains(guide, "\n") {
		t.Fatal(guide)
	}
	catalog := tools.Catalog()
	if len(catalog) != len(names) {
		t.Fatal(catalog)
	}
	firstLines := make(map[string]bool)
	for i, entry := range catalog {
		first, _, _ := strings.Cut(entry.Description, "\n")
		if entry.Name != names[i] || first == "" || firstLines[first] || entry.Description != first+"\n\n"+guide || tools.FirstLine(entry.Description) != first {
			t.Fatal(entry)
		}
		firstLines[first] = true
	}
}

// R-UNKB-CNBG
func TestFirstLine(t *testing.T) {
	for _, tc := range []struct{ description, want string }{{"", ""}, {"one", "one"}, {"one\ntwo", "one"}, {"\nrest", ""}, {"one\n\ntwo\nthree", "one"}, {"one\n", "one"}} {
		if got := tools.FirstLine(tc.description); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}

// R-UJWM-7C3D R-AA4X-KLBM R-UOS7-QF25 R-DVIS-9M0Y R-NGRH-4IUR
func TestPublicContracts(t *testing.T) {
	for _, configuredBase := range []string{"", base, "http://chosen.example:8123"} {
		f := setup(t, configuredBase)
		catalog := tools.Catalog()
		infos, e := f.client.ListTools(context.Background(), owner)
		if e != nil {
			t.Fatal(e)
		}
		if len(infos) != 4 {
			t.Fatal(infos)
		}
		for i, info := range infos {
			if info.Name != catalog[i].Name || info.Description != catalog[i].Description {
				t.Fatal(info)
			}
			effect := mcp.Read
			if i == 2 {
				effect = mcp.Destructive
			}
			if info.Effect() != effect {
				t.Fatal(info)
			}
			var schema struct {
				Type       string
				Required   []string
				Properties map[string]struct{ Type string }
			}
			if e = json.Unmarshal(info.InputSchema, &schema); e != nil {
				t.Fatal(e)
			}
			properties := map[string]struct{ Type string }{}
			required := map[string]bool{}
			if i == 0 {
				properties["page"] = struct{ Type string }{"string"}
				properties["limit"] = struct{ Type string }{"integer"}
			}
			if i > 0 {
				properties["path"] = struct{ Type string }{"string"}
				required["path"] = true
			}
			if i == 3 {
				properties["content_type"] = struct{ Type string }{"string"}
				properties["expires"] = struct{ Type string }{"string"}
			}
			gotRequired := map[string]bool{}
			for _, name := range schema.Required {
				gotRequired[name] = true
			}
			if schema.Type != "object" || len(schema.Properties) != len(properties) || !reflect.DeepEqual(gotRequired, required) {
				t.Fatal(info.Name, schema)
			}
			for name, want := range properties {
				if schema.Properties[name] != want {
					t.Fatal(info.Name, schema)
				}
			}
		}
	}
	for _, fn := range []func(string) string{tools.NoArtifact, tools.InvalidPath, tools.NotOwner} {
		if !strings.Contains(fn("fixture"), "fixture") {
			t.Fatal("missing copy argument")
		}
	}

	listed := tools.Listed{ID: "fixture-id", Path: "fixture-path", Bytes: 2, ContentType: "fixture-type", SHA256: "fixture-hash", Owner: "fixture-owner", Modified: "fixture-modified", URL: "fixture-url"}
	expected := map[string]any{"id": "fixture-id", "path": "fixture-path", "bytes": float64(2), "content_type": "fixture-type", "sha256": "fixture-hash", "owner": "fixture-owner", "modified": "fixture-modified", "url": "fixture-url", "expires": nil}
	assertJSONShape(t, listed, expected)
	assertJSONShape(t, tools.ArtifactList{Artifacts: []tools.Listed{listed}}, map[string]any{"artifacts": []any{expected}, "next": nil})
	expiry := "fixture-expiry"
	listed.Expires = &expiry
	expected["expires"] = expiry
	assertJSONShape(t, listed, expected)
	assertJSONShape(t, tools.ArtifactList{Artifacts: []tools.Listed{listed}}, map[string]any{"artifacts": []any{expected}, "next": nil})
	assertJSONShape(t, tools.ArtifactList{Artifacts: []tools.Listed{}}, map[string]any{"artifacts": []any{}, "next": nil})
	assertJSONShape(t, tools.Deleted{Deleted: true, ID: "fixture-deleted"}, map[string]any{"deleted": true, "id": "fixture-deleted"})

}

func assertJSONShape(t *testing.T, value any, expected map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("JSON shape: got %s, want %#v", encoded, expected)
	}
}

// R-AHGB-V7RS R-NLN2-NLTJ
func TestListShow(t *testing.T) {
	f := setup(t, base)
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
	f := setup(t, base)
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

// R-UR80-HYJJ R-TS19-97NI
func TestImportCopy(t *testing.T) {
	if tools.BearerPlaceholder == "" || strings.Contains(tools.BearerPlaceholder, "\n") || tools.ImportInstructions == "" {
		t.Fatal("empty or invalid copy")
	}
	for _, value := range []string{"", "fixture", "expires\ninput"} {
		seen := map[string]bool{store.Unreachable: true}
		for _, fn := range []func(string) string{tools.InvalidPath, tools.NotOwner, tools.InvalidExpiry, tools.PastExpiry} {
			text := fn(value)
			if text == "" || !strings.Contains(text, value) || seen[text] {
				t.Fatal(text)
			}
			seen[text] = true
		}
	}
}

func importUnchanged(t *testing.T, f *fixture, args tools.ImportArgs, failing bool) mcp.Result {
	t.Helper()
	before := f.snapshot(t)
	if e := f.cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := f.cfg.Events.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	recorded := len(f.tc.Events())
	emitted := len(f.ec.Events())
	f.db.SetFailing(failing)
	result := f.call(t, owner, "import", args)
	f.db.SetFailing(false)
	if f.snapshot(t) != before {
		t.Fatal("import changed store or blobs")
	}
	if e := f.cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := f.cfg.Events.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	es := f.tc.Events()[recorded:]
	if len(es) != 1 || es[0].Name != "tool.called" || len(f.ec.Events()) != emitted {
		t.Fatal(es, f.ec.Events()[emitted:])
	}
	return result
}

// R-TQTC-VFWT R-USFW-VQA8 R-UTNT-9I0X R-UXBI-ET90 R-AA4X-KLBM
func TestImportPlans(t *testing.T) {
	expiry := now.Add(time.Hour).In(time.FixedZone("fixture", -7*3600)).Add(123 * time.Millisecond).Format(time.RFC3339Nano)
	contentType := "application/fixture; spacing=  verbatim"
	empty := ""
	boundary := now.Add(1100 * time.Millisecond).Format(time.RFC3339Nano)
	for _, configuredBase := range []string{"", base} {
		f := setup(t, configuredBase, func() time.Time { return now.Add(900 * time.Millisecond) })
		a := f.put(t, "owned", owner)
		for _, path := range []string{"reports/q3.pdf", a.Path} {
			for _, input := range []tools.ImportArgs{{Path: path}, {Path: path, ContentType: &empty}, {Path: path, ContentType: &contentType, Expires: &expiry}, {Path: path, Expires: &boundary}} {
				result := importUnchanged(t, f, input, false)
				var got tools.Plan
				structured(t, result, &got)
				ct := store.DefaultContentType
				if input.ContentType != nil && *input.ContentType != "" {
					ct = *input.ContentType
				}
				want := tools.Plan{URL: configuredBase + "/files/" + path, Method: "PUT", Headers: tools.PlanHeaders{Authorization: tools.BearerPlaceholder, ContentType: ct, Expires: input.Expires}, Instructions: tools.ImportInstructions}
				if !reflect.DeepEqual(got, want) {
					t.Fatal(got, want)
				}
				encoded, e := json.Marshal(got)
				if e != nil {
					t.Fatal(e)
				}
				urlJSON, _ := json.Marshal(want.URL)
				authorizationJSON, _ := json.Marshal(tools.BearerPlaceholder)
				ctJSON, _ := json.Marshal(ct)
				instructionsJSON, _ := json.Marshal(tools.ImportInstructions)
				wantJSON := `{"url":` + string(urlJSON) + `,"method":"PUT","headers":{"Authorization":` + string(authorizationJSON) + `,"Content-Type":` + string(ctJSON)
				if input.Expires != nil {
					expiryJSON, _ := json.Marshal(*input.Expires)
					wantJSON += `,"X-Artifact-Expires":` + string(expiryJSON)
				}
				wantJSON += `},"instructions":` + string(instructionsJSON) + `}`
				if string(encoded) != wantJSON {
					t.Fatal(string(encoded), wantJSON)
				}
				wire, _ := json.Marshal(result)
				var envelope struct {
					Structured json.RawMessage `json:"structuredContent"`
				}
				if e = json.Unmarshal(wire, &envelope); e != nil || string(envelope.Structured) != wantJSON {
					t.Fatal(string(wire), e)
				}
				if path == a.Path {
					var shown files.Metadata
					structured(t, f.call(t, owner, "show", tools.PathArgs{Path: path}), &shown)
					if !reflect.DeepEqual(shown, files.Describe(a, base)) {
						t.Fatal(shown)
					}
				} else {
					refused(t, f.call(t, owner, "show", tools.PathArgs{Path: path}), tools.NoArtifact(path))
				}
			}
		}
	}
}

// R-UW3M-11IB R-UXBI-ET90
func TestImportRefusalsAndPrecedence(t *testing.T) {
	f := setup(t, base, func() time.Time { return now.Add(900 * time.Millisecond) })
	f.put(t, "owned", other)
	bad, empty := "bad", ""
	past := now.Add(-time.Second).Format(time.RFC3339)
	equalSecond := now.Add(999 * time.Millisecond).Format(time.RFC3339Nano)
	future := now.Add(time.Hour).Format(time.RFC3339)
	for _, tc := range []struct {
		args    tools.ImportArgs
		failing bool
		want    string
	}{
		{tools.ImportArgs{Path: "a//b", Expires: &bad}, true, tools.InvalidPath("a//b")},
		{tools.ImportArgs{Path: "owned", Expires: &bad}, true, tools.InvalidExpiry(bad)},
		{tools.ImportArgs{Path: "owned", Expires: &empty}, true, tools.InvalidExpiry(empty)},
		{tools.ImportArgs{Path: "owned", Expires: &past}, true, tools.PastExpiry(past)},
		{tools.ImportArgs{Path: "owned", Expires: &equalSecond}, true, tools.PastExpiry(equalSecond)},
		{tools.ImportArgs{Path: "owned", Expires: &future}, true, store.Unreachable},
		{tools.ImportArgs{Path: "absent"}, true, store.Unreachable},
		{tools.ImportArgs{Path: "owned", Expires: &future}, false, tools.NotOwner("owned")},
	} {
		refused(t, importUnchanged(t, f, tc.args, tc.failing), tc.want)
	}
}

// R-UJWM-7C3D
func TestDefaultImportClock(t *testing.T) {
	f := setup(t, base, nil)
	past := time.Time{}.Format(time.RFC3339)
	refused(t, importUnchanged(t, f, tools.ImportArgs{Path: "valid", Expires: &past}, false), tools.PastExpiry(past))
}

// R-ACKQ-C4T0 R-ADSM-PWJP
func TestListSchemas(t *testing.T) {
	f := setup(t, base)
	infos, err := f.client.ListTools(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	wantInput := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"page":  map[string]any{"type": "string", "description": paging.PageDescription},
			"limit": map[string]any{"type": "integer", "description": paging.LimitDescription},
		},
		"additionalProperties": false,
	}
	var input map[string]any
	if err := json.Unmarshal(infos[0].InputSchema, &input); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, wantInput) {
		t.Fatal(input, wantInput)
	}
	var output struct {
		Properties map[string]map[string]any
		Required   []string
	}
	if err := json.Unmarshal(infos[0].OutputSchema, &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output.Properties["next"], map[string]any{"type": "string", "description": paging.NextDescription}) {
		t.Fatal(output)
	}
	for _, name := range output.Required {
		if name == "next" {
			t.Fatal(output)
		}
	}
}

// R-AA4X-KLBM R-DVIS-9M0Y
func TestPagedPublicTypes(t *testing.T) {
	page, limit, expiry := "fixture-page", 2, "fixture-expiry"
	assertJSONShape(t, tools.ListArgs{Page: &page, Limit: &limit}, map[string]any{"page": page, "limit": float64(limit)})
	assertJSONShape(t, tools.PathArgs{Path: "fixture-path"}, map[string]any{"path": "fixture-path"})
	ct := "fixture-type"
	assertJSONShape(t, tools.ImportArgs{Path: "fixture-path", ContentType: &ct, Expires: &expiry}, map[string]any{"path": "fixture-path", "content_type": ct, "expires": expiry})
	f := setup(t, base)
	a, _, err := f.cfg.Store.Put(context.Background(), store.Draft{Path: "no-expiry", OwnerID: owner.UserID, OwnerEmail: owner.Email, Max: 100}, strings.NewReader("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	var got tools.ArtifactList
	structured(t, f.call(t, owner, "list", tools.ListArgs{}), &got)
	if len(got.Artifacts) != 1 || got.Artifacts[0].ID != a.ID || got.Artifacts[0].Expires != nil || got.Next != nil {
		t.Fatal(got)
	}
}

// R-AF0J-3OAE R-AG8F-HG13
func TestListRefusalPrecedence(t *testing.T) {
	f := setup(t, base)
	token := paging.Token{Tool: "list"}
	wrong := (paging.Token{Tool: "list_widgets"}).Encode("a")
	var decoded string
	if err := token.Decode(wrong, &decoded); err == nil {
		t.Fatal("fixture token has matching checksum")
	}
	pages := []string{"made-up", wrong, token.Encode(int64(1)), token.Encode("a", "b")}
	for _, failing := range []bool{false, true} {
		f.db.SetFailing(failing)
		for _, n := range []int{-1, 0, paging.MaxLimit + 1} {
			_, err := paging.Window(&n)
			if err == nil {
				t.Fatal("fixture limit accepted")
			}
			for _, page := range append(pages, token.Encode("a"), "") {
				refused(t, f.call(t, owner, "list", tools.ListArgs{Page: &page, Limit: &n}), err.Error())
			}
		}
		for _, page := range pages {
			for _, n := range []*int{nil, newInt(1), newInt(paging.MaxLimit)} {
				refused(t, f.call(t, owner, "list", tools.ListArgs{Page: &page, Limit: n}), paging.BadPage)
			}
		}
	}
	f.db.SetFailing(false)
}

func newInt(n int) *int { return &n }

func listed(a store.Artifact) tools.Listed {
	m := files.Describe(a, base)
	return tools.Listed{ID: m.ID, Path: m.Path, Bytes: m.Bytes, ContentType: m.ContentType, SHA256: m.SHA256, Owner: m.Owner, Modified: m.Modified, Expires: m.Expires, URL: m.URL}
}

func assertPage(t *testing.T, f *fixture, args any, remainder []store.Artifact, limit int) tools.ArtifactList {
	t.Helper()
	var got tools.ArtifactList
	structured(t, f.call(t, other, "list", args), &got)
	count := min(limit, len(remainder))
	want := make([]tools.Listed, count)
	for i := range want {
		want[i] = listed(remainder[i])
	}
	if !reflect.DeepEqual(got.Artifacts, want) {
		t.Fatal(got, want)
	}
	if len(remainder) > limit {
		next := (paging.Token{Tool: "list"}).Encode(remainder[limit-1].Path)
		if got.Next == nil || *got.Next != next {
			t.Fatal(got.Next, next)
		}
	} else if got.Next != nil {
		t.Fatal(got.Next)
	}
	return got
}

// R-AHGB-V7RS R-AIO8-8ZIH
func TestListPagination(t *testing.T) {
	f := setup(t, base)
	assertPage(t, f, tools.ListArgs{}, nil, paging.DefaultLimit)
	paths := []string{"B", "a", "a-b", "a.b", "a/b", "a0"}
	for i := 0; i < paging.DefaultLimit; i++ {
		paths = append(paths, fmt.Sprintf("z%03d", i))
	}
	all := make([]store.Artifact, len(paths))
	for i := len(paths) - 1; i >= 0; i-- {
		c := owner
		if i%2 != 0 {
			c = other
		}
		all[i] = f.put(t, paths[i], c)
	}
	empty := ""
	for _, args := range []any{
		map[string]any{}, map[string]any{"page": nil, "limit": nil},
		map[string]any{"page": ""}, tools.ListArgs{Page: &empty},
	} {
		assertPage(t, f, args, all, paging.DefaultLimit)
	}
	for _, n := range []int{1, 2, len(all), paging.MaxLimit} {
		assertPage(t, f, tools.ListArgs{Limit: &n}, all, n)
	}
	token := paging.Token{Tool: "list"}
	for _, cursor := range []string{"", "a", "a-", "zzzz"} {
		page := token.Encode(cursor)
		start := 0
		for start < len(all) && all[start].Path <= cursor {
			start++
		}
		assertPage(t, f, tools.ListArgs{Page: &page, Limit: newInt(2)}, all[start:], 2)
	}
	limit := 2
	var page *string
	var walked []tools.Listed
	for start := 0; ; start += limit {
		got := assertPage(t, f, tools.ListArgs{Page: page, Limit: &limit}, all[start:], limit)
		walked = append(walked, got.Artifacts...)
		if got.Next == nil {
			break
		}
		page = got.Next
	}
	want := make([]tools.Listed, len(all))
	for i := range want {
		want[i] = listed(all[i])
	}
	if !reflect.DeepEqual(walked, want) {
		t.Fatal(walked, want)
	}
	// The continuation position survives deletion of the artifact naming it.
	if _, err := f.cfg.Store.Delete(context.Background(), "a", other.UserID); err != nil {
		t.Fatal(err)
	}
	after := token.Encode("a")
	assertPage(t, f, tools.ListArgs{Page: &after, Limit: &limit}, all[2:], limit)
}
