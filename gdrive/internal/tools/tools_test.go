package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
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
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/trail"
	"github.com/ikigenba/ikigenba/gdrive/internal/transfers"
)

type sequence struct{ n byte }

func (s *sequence) Read(p []byte) (int, error) {
	for i := range p {
		s.n++
		p[i] = s.n
	}
	return len(p), nil
}

var caller = identity.Caller{UserID: "alice", Email: "alice@example.test", RequestID: "request-fixture"}
var clock = time.Date(2026, 10, 11, 12, 0, 7, 0, time.UTC)

type fixture struct {
	st                        *store.Store
	d                         *db.DB
	w                         *telemetry.Writer
	cap                       *telemetry.Capture
	em                        *events.Emitter
	bus                       *events.Capture
	dc                        *drive.Client
	cc                        *connections.Client
	ac                        *artifacts.Client
	mu                        sync.Mutex
	tokenCalls, artifactCalls int
	requests                  []*http.Request
	tokenStatus               int
	tokenBody                 string
	driveStatus               int
	fileMime                  string
	fileNext                  string
	client                    *mcp.Client
	engine                    *transfers.Engine
}

func socket(t *testing.T, name string, h http.Handler) services.Entry {
	t.Helper()
	dir, e := os.MkdirTemp("", "gdt-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := filepath.Join(dir, "s")
	l, e := net.Listen("unix", p)
	if e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return services.Entry{Name: name, Socket: p, URL: "https://" + name + ".example.test"}
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{tokenStatus: 200, tokenBody: `{"access_token":"secret-fixture","expires":"2030-01-01T00:00:00Z","account":"fixture-account","scopes":[]}`, driveStatus: 200, fileNext: "next-fixture", fileMime: "text/plain", cap: &telemetry.Capture{}, bus: &events.Capture{}}
	f.w = telemetry.New(telemetry.Config{Service: "gdrive", Sink: f.cap, Stderr: io.Discard, Now: func() time.Time { return clock }, Rand: &sequence{}})
	t.Cleanup(func() { f.w.Shutdown(context.Background(), "finished") })
	f.em = events.New(events.Config{Service: "gdrive", Sink: f.bus, Stderr: io.Discard, Telemetry: f.w, Now: func() time.Time { return clock }, Rand: &sequence{}, Emits: trail.Emits()})
	t.Cleanup(func() { f.em.Shutdown(context.Background()) })
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "gdrive.db"), Migrations: gdrive.Migrations(), Now: func() time.Time { return clock }})
	if e != nil {
		t.Fatal(e)
	}
	f.d = d
	t.Cleanup(func() { _ = d.Close() })
	f.st = store.New(d, store.Config{Now: func() time.Time { return clock }, Rand: &sequence{n: 150}})
	ce := socket(t, "connections", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tokenCalls++
		if r.URL.Path != "/token/google" || r.Header.Get("X-User-Id") != caller.UserID {
			t.Error("token identity/path")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.tokenStatus)
		_, _ = io.WriteString(w, f.tokenBody)
	}))
	f.cc = connections.NewClient(connections.ClientConfig{Service: ce, Telemetry: f.w})
	ae := socket(t, "artifacts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.artifactCalls++
		if r.Method != "HEAD" {
			t.Error("unexpected artifact request", r.Method)
		}
		w.Header().Set("Content-Length", "3")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"fixture-hash"`)
		w.Header().Set("Last-Modified", clock.Format(http.TimeFormat))
		w.WriteHeader(200)
	}))
	f.ac = artifacts.NewClient(artifacts.ClientConfig{Service: ae, Telemetry: f.w})
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Clone(context.Background()))
		if r.Header.Get("Authorization") != "Bearer secret-fixture" {
			t.Error("drive token")
		}
		w.Header().Set("Content-Type", "application/json")
		if f.driveStatus != 200 && r.URL.Path != "/drive/v3/files/root" {
			w.WriteHeader(f.driveStatus)
			_, _ = io.WriteString(w, `{"error":{}}`)
			return
		}
		switch r.URL.Path {
		case "/drive/v3/drives":
			_, _ = io.WriteString(w, `{"drives":[{"id":"drive-fixture","name":"shared-fixture"}]}`)
		case "/drive/v3/files/folder-fixture":
			_, _ = io.WriteString(w, `{"id":"folder-fixture","name":"folder-fixture","mimeType":"application/vnd.google-apps.folder"}`)
		case "/drive/v3/files/root":
			_, _ = io.WriteString(w, `{"id":"root-fixture"}`)
		case "/drive/v3/files":
			_, _ = io.WriteString(w, `{"files":[{"id":"file-fixture","name":"fixture-name","mimeType":"text/plain","size":"3","modifiedTime":"2026-10-11T12:00:07Z","webViewLink":"https://drive.example.test/file"}],"nextPageToken":"`+f.fileNext+`"}`)
		default:
			_, _ = io.WriteString(w, `{"id":"file-fixture","name":"fixture-name","mimeType":"`+f.fileMime+`","size":"3","modifiedTime":"2026-10-11T12:00:07Z","webViewLink":"https://drive.example.test/file"}`)
		}
	}))
	t.Cleanup(ds.Close)
	f.dc = drive.NewClient(drive.Config{HTTP: ds.Client(), BaseURL: ds.URL, Telemetry: f.w})
	f.engine = transfers.New(transfers.Config{Store: f.st, Drive: f.dc, Artifacts: f.ac, Connections: f.cc, Telemetry: f.w, Events: f.em, ConnectURL: "https://connections.example.test/google/connect", MaxBytes: 100, MaxActive: 0, MaxQueued: 10, Now: func() time.Time { return clock }})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "gdrive", Telemetry: f.w})
	Register(srv, Config{Transfers: f.engine, Drive: f.dc, Store: f.st})
	hs := httptest.NewServer(identity.Require(srv))
	t.Cleanup(hs.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: hs.URL + "/mcp", HTTPClient: hs.Client()})
	return f
}
func call(t *testing.T, f *fixture, name string, args any) mcp.Result {
	t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	before := f.tokenCalls
	f.mu.Unlock()
	readOnly := name == "drives" || name == "list" || name == "search" || name == "show" || name == "transfers" || name == "transfer"
	var snapshot sideEffects
	if readOnly {
		snapshot = observeSideEffects(t, f)
	}
	r, e := f.client.CallTool(context.Background(), caller, name, b)
	if readOnly {
		after := observeSideEffects(t, f)
		if !reflect.DeepEqual(snapshot, after) {
			t.Fatal("read call changed state", name, snapshot, after)
		}
	}
	if name == "drives" || name == "list" || name == "search" || name == "show" || name == "pull" || name == "push" {
		f.mu.Lock()
		after := f.tokenCalls
		f.mu.Unlock()
		if after != before+1 {
			t.Fatal("call token count", name, before, after)
		}
	}
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func resultWire(t *testing.T, r mcp.Result) ([]struct{ Type, Text string }, json.RawMessage) {
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
	return v.Content, v.Structured
}
func success(t *testing.T, r mcp.Result, want any) {
	t.Helper()
	content, sc := resultWire(t, r)
	if r.IsError() || len(content) != 1 || content[0].Type != "text" {
		t.Fatal("not successful", r)
	}
	var a, b any
	if e := json.Unmarshal(sc, &a); e != nil {
		t.Fatal(e)
	}
	expected, e := json.Marshal(want)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(expected, &b); e != nil {
		t.Fatal(e)
	}
	omitNull(b)
	if !reflect.DeepEqual(a, b) || content[0].Text != string(sc) {
		t.Fatalf("success mismatch %s != %s", sc, expected)
	}
}
func refused(t *testing.T, r mcp.Result, word, sentence, u string) {
	t.Helper()
	content, sc := resultWire(t, r)
	want := word + ": " + sentence
	if u != "" {
		want += " " + u
	}
	if !r.IsError() || len(content) != 1 || content[0].Type != "text" || content[0].Text != want || len(sc) != 0 {
		t.Fatalf("refusal %+v %s want %s", content, sc, want)
	}
}
func TestCatalogSchemasAndCopy(t *testing.T) {
	// R-0DCP-A1L5 R-YD1X-L7X4 R-7EZT-IJOQ R-0OBS-PZ9E R-0PJP-3R03
	f := newFixture(t)
	infos, e := f.client.ListTools(context.Background(), caller)
	if e != nil {
		t.Fatal(e)
	}
	entries := Catalog()
	names := []string{"drives", "list", "search", "show", "pull", "push", "transfers", "transfer"}
	if len(infos) != 8 || len(entries) != 8 {
		t.Fatal("catalog length")
	}
	seen := map[string]bool{}
	inputs := []any{NoArgs{}, ListArgs{}, SearchArgs{Query: ""}, ShowArgs{File: ""}, PullArgs{File: "", Path: ""}, PushArgs{Path: ""}, TransfersArgs{}, TransferArgs{Transfer: ""}}
	properties := [][]string{{}, {"folder", "page", "limit"}, {"query", "folder", "drive", "page", "limit"}, {"file"}, {"file", "path", "format", "expires"}, {"path", "folder", "name", "file"}, {"page", "limit"}, {"transfer"}}
	required := [][]string{{}, {}, {"query"}, {"file"}, {"file", "path"}, {"path"}, {}, {"transfer"}}
	for i, info := range infos {
		entry := entries[i]
		if info.Name != names[i] || entry.Name != names[i] || entry.Description == "" || strings.Contains(entry.Description, "\n") || seen[entry.Description] || info.Description != entry.Description+"\n\n"+TransferGuide {
			t.Fatal("metadata", info)
		}
		seen[entry.Description] = true
		effect := mcp.Read
		if i == 4 || i == 5 {
			effect = mcp.Destructive
		}
		if info.Effect() != effect {
			t.Fatal("effect")
		}
		var schema struct {
			Properties map[string]any
			Required   []string
		}
		if e = json.Unmarshal(info.InputSchema, &schema); e != nil {
			t.Fatal(e)
		}
		if len(schema.Properties) != len(properties[i]) {
			t.Fatal("properties")
		}
		for _, p := range properties[i] {
			if _, ok := schema.Properties[p]; !ok {
				t.Fatal("property", p)
			}
		}
		slices.Sort(schema.Required)
		slices.Sort(required[i])
		if strings.Join(schema.Required, ",") != strings.Join(required[i], ",") {
			t.Fatal("required", info.Name, schema.Required, required[i])
		}
		if _, e = json.Marshal(inputs[i]); e != nil {
			t.Fatal(e)
		}

	}
	seen = map[string]bool{}
	for _, s := range []string{RefuseNotConnected, RefuseReconnect, RefuseNotAFile, RefuseNotExportable, RefuseFormat, RefuseExpires, RefuseTooLarge, RefuseNotFound, RefuseProvider, RefusePath, RefuseUnavailable, RefuseStopping, RefuseQueueFull} {
		if s == "" || strings.Contains(s, "\n") || seen[s] {
			t.Fatal("copy")
		}
		seen[s] = true
	}
	if TransferGuide == "" || strings.Contains(TransferGuide, "\n") {
		t.Fatal("guide")
	}
}
func TestJSONRecords(t *testing.T) {
	// R-YE9T-YZNT R-YFHQ-CREI R-0I8A-T4JX
	for _, c := range []struct {
		v    any
		want string
	}{{DriveList{}, `{"drives":[]}`}, {DriveList{[]drive.Drive{}}, `{"drives":[]}`}, {FileList{}, `{"files":[]}`}, {FileList{Files: []drive.File{}}, `{"files":[]}`}, {FileList{Next: stringPointer("")}, `{"files":[],"next":""}`}, {FileList{Next: stringPointer("fixture-page")}, `{"files":[],"next":"fixture-page"}`}} {
		b, e := json.Marshal(c.v)
		if e != nil || string(b) != c.want {
			t.Fatal(string(b), e)
		}
	}
	zone := time.FixedZone("fixture", 3600)
	started := clock.In(zone)
	finished := started.Add(time.Minute)
	tfer := store.Transfer{ID: "fixture-id", Direction: store.DirectionPull, Status: store.StatusDone, File: "fixture-file", Name: "fixture-name", Kind: "file", Path: "fixture-path", ContentType: "text/plain", Bytes: 45, Started: started, Finished: finished, URL: "https://artifact.example.test/file", Link: "https://drive.example.test/file", UserID: "private-user", ArtifactID: "private-artifact", RequestID: "private-request"}
	r := RecordOf(tfer)
	if r.ID != tfer.ID || r.Direction != tfer.Direction || r.Status != tfer.Status || r.File != tfer.File || r.Name != tfer.Name || r.Kind != tfer.Kind || r.Path != tfer.Path || r.ContentType != tfer.ContentType || r.Bytes != 45 || r.Started != clock.Format(time.RFC3339) || r.Finished == nil || *r.Finished != finished.UTC().Format(time.RFC3339) || r.URL == nil || *r.URL != tfer.URL || r.Link == nil || *r.Link != tfer.Link || r.Reason != nil || r.Format != nil {
		t.Fatal(r)
	}
	tfer.Status = store.StatusRunning
	tfer.Finished = time.Time{}
	tfer.URL = ""
	tfer.Link = ""
	r = RecordOf(tfer)
	if r.Finished != nil || r.URL != nil || r.Link != nil || r.Reason != nil {
		t.Fatal("running record")
	}
	tfer.Reason = store.ReasonProvider
	tfer.Format = "format-fixture"
	r = RecordOf(tfer)
	if r.Reason == nil || *r.Reason != tfer.Reason || r.Format == nil || *r.Format != tfer.Format {
		t.Fatal("optional record")
	}
	f := newFixture(t)
	ts, e := f.st.Add(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Path: "fixture/path", UserID: caller.UserID})
	if e != nil {
		t.Fatal(e)
	}
	res := call(t, f, "transfer", TransferArgs{ts.ID})
	_, sc := resultWire(t, res)
	keys := []string{"id", "direction", "status", "file", "name", "kind", "path", "content_type", "bytes", "started"}
	last := -1
	for _, k := range keys {
		i := bytes.Index(sc, []byte(`"`+k+`":`))
		if i <= last {
			t.Fatal("order", string(sc))
		}
		last = i
	}
	success(t, res, RecordOf(ts))
	success(t, call(t, f, "transfers", NoArgs{}), TransferList{[]Record{RecordOf(ts)}, nil})
	exported, e := f.st.Add(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Path: "exported/path", UserID: caller.UserID, File: "doc-fixture", Name: "exported-fixture", Kind: "document", ContentType: "application/pdf", Format: "application/pdf"})
	if e != nil {
		t.Fatal(e)
	}
	completed, e := f.st.Finish(context.Background(), exported.ID, store.Ending{Status: store.StatusDone, Bytes: 13, URL: "https://artifact.example.test/files/exported/path", Link: "https://drive.example.test/doc"})
	if e != nil {
		t.Fatal(e)
	}
	completeResult := call(t, f, "transfer", TransferArgs{completed.ID})
	success(t, completeResult, RecordOf(completed))
	assertRecordOrder(t, completeResult, []string{"id", "direction", "status", "file", "name", "kind", "path", "content_type", "format", "bytes", "started", "finished", "url", "link"})
	failed, e := f.st.Add(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Path: "failed/path", UserID: caller.UserID, File: "doc-failed", Name: "failed-fixture", Kind: "document", ContentType: "application/pdf", Format: "application/pdf"})
	if e != nil {
		t.Fatal(e)
	}
	failed, e = f.st.Finish(context.Background(), failed.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonProvider, Bytes: 7})
	if e != nil {
		t.Fatal(e)
	}
	failedResult := call(t, f, "transfer", TransferArgs{failed.ID})
	success(t, failedResult, RecordOf(failed))
	assertRecordOrder(t, failedResult, []string{"id", "direction", "status", "reason", "file", "name", "kind", "path", "content_type", "format", "bytes", "started", "finished"})

}
func TestDriveReads(t *testing.T) {
	// R-0N3W-C7IP R-0UFA-MTYV R-M79P-28HF R-M8HL-G084 R-0Y2Z-S56Y R-KPVQ-5UGU R-KNFX-EAZG R-KR3M-JM7J
	f := newFixture(t)
	n := int64(3)
	file := drive.File{ID: "file-fixture", Name: "fixture-name", Kind: drive.KindFile, MimeType: "text/plain", Bytes: &n, Modified: clock, Link: "https://drive.example.test/file"}
	success(t, call(t, f, "drives", NoArgs{}), DriveList{[]drive.Drive{{ID: "drive-fixture", Name: "shared-fixture"}}})
	success(t, call(t, f, "show", ShowArgs{"file-fixture"}), file)
	folder, cursor, driveID, text := "folder-fixture", "page-fixture", "drive-fixture", "quote's \\ content"
	for _, a := range []ListArgs{{}, {Folder: &folder, Page: &cursor}, {Folder: stringPointer(""), Page: stringPointer("")}} {
		success(t, call(t, f, "list", a), FileList{[]drive.File{file}, stringPointer("next-fixture")})
	}
	for _, a := range []SearchArgs{{Query: ""}, {Query: text, Folder: &folder, Drive: &driveID, Page: &cursor}, {Query: "", Folder: stringPointer(""), Drive: stringPointer(""), Page: stringPointer("")}} {
		success(t, call(t, f, "search", a), FileList{[]drive.File{file}, stringPointer("next-fixture")})
	}
	f.mu.Lock()
	before := len(f.requests)
	f.mu.Unlock()
	refused(t, call(t, f, "show", ShowArgs{""}), "not_found", RefuseNotFound, "")
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != before || f.tokenCalls != 9 || f.artifactCalls != 0 {
		t.Fatal("read side effects", f.tokenCalls, f.artifactCalls)
	}
	sawList, sawSearch := false, false
	for _, r := range f.requests {
		q := r.URL.Query()
		if q.Get("pageToken") == cursor {
			if strings.Contains(q.Get("q"), folder) {
				sawList = true
			}
			if q.Get("driveId") == driveID && strings.Contains(q.Get("q"), "quote\\'s") && strings.Contains(q.Get("q"), "\\\\") {
				sawSearch = true
			}
		}
	}
	if !sawList || !sawSearch {
		t.Fatal("arguments not forwarded")
	}
	ts, e := f.st.List(context.Background(), caller.UserID)
	if e != nil || len(ts) != 0 {
		t.Fatal("read recorded")
	}
	if e = f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, e := range f.cap.Events() {
		if strings.HasPrefix(e.Name, "transfer.") {
			t.Fatal("read transfer telemetry")
		}
	}
	if len(f.bus.Events()) != 0 {
		t.Fatal("read transfer bus")
	}
}
func TestConnectionAndDriveRefusals(t *testing.T) {
	// R-7G7P-WBFF R-KM81-0J8R R-0T7E-9286
	for _, c := range []struct {
		status                  int
		body, word, sentence, u string
	}{{404, `{"error":"not_connected"}`, "not_connected", RefuseNotConnected, "https://connections.example.test/google/connect"}, {409, `{"error":"reconnect","url":"https://reconnect.example.test/"}`, "reconnect", RefuseReconnect, "https://reconnect.example.test/"}, {503, `{"error":"unavailable"}`, "unavailable", RefuseUnavailable, ""}} {
		t.Run(c.word, func(t *testing.T) {
			f := newFixture(t)
			f.tokenStatus = c.status
			f.tokenBody = c.body
			for _, a := range []struct {
				name string
				args any
			}{{"drives", NoArgs{}}, {"list", ListArgs{}}, {"search", SearchArgs{Query: "fixture"}}, {"show", ShowArgs{File: "fixture"}}} {
				refused(t, call(t, f, a.name, a.args), c.word, c.sentence, c.u)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.requests) != 0 || f.tokenCalls != 4 || f.artifactCalls != 0 {
				t.Fatal("refusal effects")
			}
		})
	}
	for _, status := range []int{404, 403, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFixture(t)
			f.driveStatus = status
			for _, a := range []struct {
				name string
				args any
			}{{"drives", NoArgs{}}, {"list", ListArgs{}}, {"search", SearchArgs{Query: "fixture"}}, {"show", ShowArgs{File: "fixture"}}} {
				word, sentence := "provider", RefuseProvider
				if status == 404 {
					word, sentence = "not_found", RefuseNotFound
				}
				refused(t, call(t, f, a.name, a.args), word, sentence, "")
			}
		})
	}
}
func TestTransferTools(t *testing.T) {
	// R-0ZAW-5WXN R-10IS-JOOC R-7HFM-A364 R-MDD6-Z36W R-MFSZ-QMOA R-146H-OZWF R-7JVF-1MNI R-KSBI-XDY8
	f := newFixture(t)
	ctx := context.Background()
	success(t, call(t, f, "transfers", NoArgs{}), TransferList{[]Record{}, nil})
	expiry := clock.Add(time.Hour).Format(time.RFC3339)
	r := call(t, f, "pull", PullArgs{File: "file-fixture", Path: "fixture/pull", Expires: &expiry})
	if r.IsError() {
		t.Fatal(r)
	}
	ts, e := f.st.List(ctx, caller.UserID)
	if e != nil || len(ts) != 1 {
		t.Fatal(ts, e)
	}
	success(t, r, RecordOf(ts[0]))
	if ts[0].File != "file-fixture" || ts[0].Path != "fixture/pull" || ts[0].Direction != store.DirectionPull {
		t.Fatal(ts)
	}
	name, folder := "push-fixture", "folder-fixture"
	r = call(t, f, "push", PushArgs{Path: "fixture/push", Name: &name, Folder: &folder, File: stringPointer("")})
	if r.IsError() {
		t.Fatal(r)
	}
	ts, e = f.st.List(ctx, caller.UserID)
	if e != nil || len(ts) != 2 {
		t.Fatal(ts, e)
	}
	success(t, r, RecordOf(ts[0]))
	if ts[0].Name != name || ts[0].Path != "fixture/push" || ts[0].Direction != store.DirectionPush {
		t.Fatal(ts)
	}
	f.mu.Lock()
	sawFolder := false
	for _, request := range f.requests {
		if request.URL.Path == "/drive/v3/files/"+folder {
			sawFolder = true
		}
	}
	f.mu.Unlock()
	if !sawFolder {
		t.Fatal("push folder not forwarded")
	}

	rs := []Record{}
	for _, tf := range ts {
		rs = append(rs, RecordOf(tf))
	}
	success(t, call(t, f, "transfers", NoArgs{}), TransferList{rs, nil})
	success(t, call(t, f, "transfer", TransferArgs{ts[1].ID}), RecordOf(ts[1]))
	other, e := f.st.Add(ctx, store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Path: "private/path", UserID: "bob"})
	if e != nil {
		t.Fatal(e)
	}
	success(t, call(t, f, "transfers", NoArgs{}), TransferList{rs, nil})
	for _, id := range []string{"", "wrong", "xfr_0000000000000000", other.ID} {
		refused(t, call(t, f, "transfer", TransferArgs{id}), "not_found", RefuseNotFound, "")
	}
	f.mu.Lock()
	before := len(f.requests)
	f.mu.Unlock()
	refused(t, call(t, f, "pull", PullArgs{File: "", Path: "fixture/path"}), "not_found", RefuseNotFound, "")
	f.mu.Lock()
	if len(f.requests) != before {
		t.Error("empty pull reached Drive")
	}
	f.mu.Unlock()
	f.d.SetFailing(true)
	refused(t, call(t, f, "transfers", NoArgs{}), "unavailable", RefuseUnavailable, "")
	refused(t, call(t, f, "transfer", TransferArgs{ts[0].ID}), "unavailable", RefuseUnavailable, "")
	refused(t, call(t, f, "transfer", TransferArgs{"wrong"}), "not_found", RefuseNotFound, "")
	refused(t, call(t, f, "pull", PullArgs{File: "file-fixture", Path: "fixture/path"}), "unavailable", RefuseUnavailable, "")
	refused(t, call(t, f, "push", PushArgs{Path: "fixture/path"}), "unavailable", RefuseUnavailable, "")
	f.d.SetFailing(false)
	for _, a := range []struct {
		name string
		args any
	}{{"pull", PullArgs{File: "file-fixture", Path: "bad%"}}, {"push", PushArgs{Path: "bad%"}}} {
		f.mu.Lock()
		n := f.tokenCalls
		f.mu.Unlock()
		refused(t, call(t, f, a.name, a.args), "path", RefusePath, "")
		f.mu.Lock()
		if f.tokenCalls != n+1 {
			t.Error("token count")
		}
		f.mu.Unlock()
	}
	f.engine.Drain(ctx)
	if _, e = f.st.Prune(ctx, clock.AddDate(1, 0, 0), 1, 1); e != nil {
		t.Fatal(e)
	}
	refused(t, call(t, f, "transfer", TransferArgs{ts[1].ID}), "not_found", RefuseNotFound, "")
}

func omitNull(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, a := range x {
			if a == nil {
				delete(x, k)
			} else {
				omitNull(a)
			}
		}
	case []any:
		for _, a := range x {
			omitNull(a)
		}
	}
}

func TestOptionalTransferArgumentsAndSchemaRefusal(t *testing.T) {
	// R-YD1X-L7X4 R-0ZAW-5WXN R-10IS-JOOC R-7G7P-WBFF
	f := newFixture(t)
	f.fileMime = "application/vnd.google-apps.document"
	format := drive.DefaultFormat(drive.KindDocument)
	expires := clock.Add(time.Hour).Format(time.RFC3339)
	r := call(t, f, "pull", PullArgs{File: "file-fixture", Path: "format/path", Format: &format, Expires: &expires})
	if r.IsError() {
		t.Fatal(r)
	}
	ts, e := f.st.List(context.Background(), caller.UserID)
	if e != nil || len(ts) != 1 || ts[0].Format != format || ts[0].ContentType != format {
		t.Fatal(ts, e)
	}
	success(t, r, RecordOf(ts[0]))
	refused(t, call(t, f, "pull", PullArgs{File: "file-fixture", Path: "format/path", Expires: stringPointer("")}), "expires", RefuseExpires, "")
	for _, value := range []string{clock.Add(-time.Hour).In(time.FixedZone("fixture", 3600)).Format(time.RFC3339), "malformed-fixture"} {
		refused(t, call(t, f, "pull", PullArgs{File: "file-fixture", Path: "format/path", Expires: &value}), "expires", RefuseExpires, "")
	}

	refused(t, call(t, f, "pull", PullArgs{File: "file-fixture", Path: "format/path", Format: ptr("invalid-format")}), "format", RefuseFormat, "")
	f.fileMime = "text/plain"
	r = call(t, f, "push", PushArgs{Path: "push/replace", File: ptr("file-fixture"), Folder: stringPointer(""), Name: stringPointer("")})
	if r.IsError() {
		t.Fatal(r)
	}
	ts, e = f.st.List(context.Background(), caller.UserID)
	if e != nil || len(ts) != 2 || ts[0].File != "file-fixture" || ts[0].Name != "fixture-name" {
		t.Fatal(ts, e)
	}
	success(t, r, RecordOf(ts[0]))
	for _, c := range []struct {
		name string
		args json.RawMessage
	}{{"search", json.RawMessage(`{}`)}, {"show", json.RawMessage(`{}`)}, {"pull", json.RawMessage(`{"file":"fixture"}`)}, {"push", json.RawMessage(`{}`)}, {"transfer", json.RawMessage(`{}`)}, {"list", json.RawMessage(`{"folder":17}`)}} {
		r, e := f.client.CallTool(context.Background(), caller, c.name, c.args)
		if e != nil || !r.IsError() {
			t.Fatal("schema accepted", c.name, r, e)
		}
	}
	f.engine.Drain(context.Background())
}

func stringPointer(s string) *string { return &s }

func assertRecordOrder(t *testing.T, r mcp.Result, keys []string) {
	t.Helper()
	_, sc := resultWire(t, r)
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(sc, &fields); e != nil {
		t.Fatal(e)
	}
	if len(fields) != len(keys) {
		t.Fatal("record fields", string(sc))
	}
	last := -1
	for _, key := range keys {
		pos := bytes.Index(sc, []byte(`"`+key+`":`))
		if pos <= last {
			t.Fatal("record member order", key, string(sc))
		}
		last = pos
	}
}

type sideEffects struct {
	Alice, Bob            []store.Transfer
	AliceError, BobError  bool
	Artifacts, Trail, Bus int
}

func observeSideEffects(t *testing.T, f *fixture) sideEffects {
	t.Helper()
	ctx := context.Background()
	if e := f.w.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	if e := f.em.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	alice, aerr := f.st.List(ctx, caller.UserID)
	bob, berr := f.st.List(ctx, "bob")
	f.mu.Lock()
	artifacts := f.artifactCalls
	f.mu.Unlock()
	count := 0
	for _, event := range f.cap.Events() {
		if strings.HasPrefix(event.Name, "transfer.") {
			count++
		}
	}
	return sideEffects{Alice: alice, Bob: bob, AliceError: aerr != nil, BobError: berr != nil, Artifacts: artifacts, Trail: count, Bus: len(f.bus.Events())}
}

func TestPagingSchemas(t *testing.T) {
	// R-YGPM-QJ57 R-YD1X-L7X4
	f := newFixture(t)
	infos, err := f.client.ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name != "list" && info.Name != "search" && info.Name != "transfers" {
			continue
		}
		var schema struct {
			Properties map[string]struct{ Type, Description string }
		}
		if err := json.Unmarshal(info.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		for _, field := range []struct{ name, typ, description string }{{"page", "string", paging.PageDescription}, {"limit", "integer", paging.LimitDescription}} {
			prop := schema.Properties[field.name]
			if prop.Type != field.typ || prop.Description != field.description {
				t.Fatalf("%s.%s: %+v", info.Name, field.name, prop)
			}
		}
		order := []string{"folder", "page", "limit"}
		if info.Name == "search" {
			order = []string{"query", "folder", "drive", "page", "limit"}
		}
		if info.Name == "transfers" {
			order = []string{"page", "limit"}
		}
		last := -1
		for _, name := range order {
			at := bytes.Index(info.InputSchema, []byte(`"`+name+`":`))
			if at <= last {
				t.Fatal("property order", info.Name, string(info.InputSchema))
			}
			last = at
		}
		if info.Name == "transfers" {
			var out struct {
				Properties map[string]struct{ Type, Description string }
				Required   []string
			}
			if err := json.Unmarshal(info.OutputSchema, &out); err != nil {
				t.Fatal(err)
			}
			if out.Properties["next"].Type != "string" || out.Properties["next"].Description != paging.NextDescription || slices.Contains(out.Required, "next") {
				t.Fatal("next schema", string(info.OutputSchema))
			}
		}
	}
}

func TestDrivePagingWindows(t *testing.T) {
	// R-M79P-28HF R-M8HL-G084 R-M9PH-TRYT
	for _, name := range []string{"list", "search"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			for _, limit := range []any{nil, paging.MaxLimit, 1, 7} {
				for _, next := range []string{"opaque +/= token", ""} {
					f.mu.Lock()
					f.fileNext = next
					f.mu.Unlock()
					args := map[string]any{"limit": limit}
					if name == "search" {
						args["query"] = ""
					}
					res := call(t, f, name, args)
					if res.IsError() {
						t.Fatal(res)
					}
					_, sc := resultWire(t, res)
					var got struct {
						Files []drive.File
						Next  *string
					}
					if err := json.Unmarshal(sc, &got); err != nil {
						t.Fatal(err)
					}
					if len(got.Files) != 1 || (next == "" && got.Next != nil) || (next != "" && (got.Next == nil || *got.Next != next)) {
						t.Fatal("page result", string(sc))
					}
					f.mu.Lock()
					request := f.requests[len(f.requests)-1]
					f.mu.Unlock()
					size := paging.DefaultLimit
					if limit != nil {
						size = limit.(int)
					}
					if request.URL.Query().Get("pageSize") != strconv.Itoa(size) {
						t.Fatal("size", request.URL)
					}
					success(t, res, FileList{got.Files, got.Next})
				}
			}
			// Absent limit follows the same default window as null.
			args := map[string]any{}
			if name == "search" {
				args["query"] = ""
			}
			res := call(t, f, name, args)
			if res.IsError() {
				t.Fatal(res)
			}
			f.mu.Lock()
			request := f.requests[len(f.requests)-1]
			f.mu.Unlock()
			if request.URL.Query().Get("pageSize") != strconv.Itoa(paging.DefaultLimit) {
				t.Fatal("default size")
			}
			for _, limit := range []int{-1, 0, paging.MaxLimit + 1} {
				f.mu.Lock()
				before := len(f.requests)
				f.mu.Unlock()
				_, err := paging.Window(&limit)
				args := map[string]any{"folder": "bad-folder", "page": "bad-page", "limit": limit}
				if name == "search" {
					args["query"] = ""
				}
				pagingRefused(t, call(t, f, name, args), err.Error())
				f.mu.Lock()
				after := len(f.requests)
				f.mu.Unlock()
				if before != after {
					t.Fatal("invalid limit reached Drive")
				}
			}
		})
	}
}
func pagingRefused(t *testing.T, r mcp.Result, text string) {
	t.Helper()
	blocks, sc := resultWire(t, r)
	if !r.IsError() || len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != text || len(sc) != 0 {
		t.Fatalf("paging refusal: %+v %s want %q", blocks, sc, text)
	}
}
func TestTransferPagingRefusals(t *testing.T) {
	// R-MAXE-7JPI R-MC5A-LBG7
	f := newFixture(t)
	token := paging.Token{Tool: "transfers"}
	stamp, id := clock.UTC().Format(time.RFC3339), "xfr_0123456789abcdef"
	valid := token.Encode(stamp, id)
	altered := "A" + valid[1:]
	if altered == valid {
		altered = "B" + valid[1:]
	}
	pages := []string{"invented", (paging.Token{Tool: "list"}).Encode(stamp, id), token.Encode(stamp), token.Encode("2026-10-05T09:00:00+02:00", id), token.Encode("2026-10-05T09:00:00.5Z", id), token.Encode("invalid-time", id), token.Encode(stamp, "run_0123456789abcdef"), altered}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, page := range append(pages, valid) {
			for _, limit := range []int{-1, 0, paging.MaxLimit + 1} {
				_, err := paging.Window(&limit)
				pagingRefused(t, call(t, f, "transfers", TransfersArgs{&page, &limit}), err.Error())
			}
		}
		for _, page := range pages {
			for _, limit := range []*int{nil, intPointer(1), intPointer(paging.MaxLimit)} {
				pagingRefused(t, call(t, f, "transfers", TransfersArgs{&page, limit}), paging.BadPage)
			}
		}
	}
	f.d.SetFailing(false)
	success(t, call(t, f, "transfers", map[string]any{"limit": nil}), TransferList{[]Record{}, nil})
}
func intPointer(n int) *int { return &n }
func TestTransferPagingTraversal(t *testing.T) {
	// R-MDD6-Z36W R-MEL3-CUXL R-MFSZ-QMOA R-YFHQ-CREI
	f := newFixture(t)
	ctx := context.Background()
	started := clock
	seeder := store.New(f.d, store.Config{Now: func() time.Time { return started }, Rand: &sequence{n: 150}})
	for i := 0; i < paging.DefaultLimit+2; i++ {
		started = clock.Add(time.Duration(i/9) * time.Second)
		user := caller.UserID
		if i == 3 {
			user = "bob"
		}
		_, err := seeder.Add(ctx, store.Transfer{UserID: user, Direction: store.DirectionPull, Status: store.StatusQueued, Path: strconv.Itoa(i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	all, err := f.st.List(ctx, caller.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{1, 7, paging.DefaultLimit, paging.MaxLimit} {
		var page *string
		offset := 0
		for {
			res := call(t, f, "transfers", TransfersArgs{page, &size})
			_, sc := resultWire(t, res)
			var got TransferList
			if err := json.Unmarshal(sc, &got); err != nil {
				t.Fatal(err)
			}
			end := min(offset+size, len(all))
			records := make([]Record, 0, end-offset)
			for _, tf := range all[offset:end] {
				records = append(records, RecordOf(tf))
			}
			var next *string
			if end < len(all) {
				last := all[end-1]
				next = stringPointer((paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID))
			}
			success(t, res, TransferList{records, next})
			if next != nil {
				if bytes.Index(sc, []byte(`"next":`)) < bytes.Index(sc, []byte(`"transfers":`)) {
					t.Fatal("transfer list order", string(sc))
				}
			}
			offset = end
			if got.Next == nil {
				break
			}
			page = got.Next
		}
		if offset != len(all) {
			t.Fatal("incomplete traversal")
		}
	}
	for _, args := range []any{TransfersArgs{}, map[string]any{"limit": nil}, TransfersArgs{Page: stringPointer("")}} {
		records := make([]Record, paging.DefaultLimit)
		for i, tf := range all[:paging.DefaultLimit] {
			records[i] = RecordOf(tf)
		}
		last := all[paging.DefaultLimit-1]
		next := stringPointer((paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID))
		success(t, call(t, f, "transfers", args), TransferList{records, next})
	}
	last := all[len(all)-1]
	page := (paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
	success(t, call(t, f, "transfers", TransfersArgs{Page: &page}), TransferList{[]Record{}, nil})
	f.d.SetFailing(true)
	for _, args := range []TransfersArgs{{}, {Page: &page}, {Limit: intPointer(paging.MaxLimit)}} {
		refused(t, call(t, f, "transfers", args), "unavailable", RefuseUnavailable, "")
	}
}
