package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"eventplane/correlation"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sites/internal/sites"
	"sort"
	"strings"
	"testing"
)

type correlationRecordingTransport struct {
	next http.RoundTripper
	ids  []string
}

func (r *correlationRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.ids = append(r.ids, correlation.FromContext(req.Context()))
	return r.next.RoundTrip(req)
}

// fakeMirror is an in-memory sites.MirrorClient: a subtree of mirror paths →
// bytes. List returns every entry whose path sits under the requested prefix;
// Fetch returns one entry's bytes. It carries no network/HTTP — the sync verb's
// logic is exercised against this fake (the HTTP client itself is unit-tested in
// the sites package, Phase 6).
type fakeMirror struct {
	files      map[string][]byte // full mirror path → bytes
	entries    []sites.MirrorFile
	listErr    error
	fetchErr   error
	listCalls  []string
	fetchCalls []string
}

func (f *fakeMirror) List(_ context.Context, prefix string) ([]sites.MirrorFile, error) {
	f.listCalls = append(f.listCalls, prefix)
	if f.listErr != nil {
		return nil, f.listErr
	}
	p := strings.TrimRight(prefix, "/")
	var out []sites.MirrorFile
	if f.entries != nil {
		for _, entry := range f.entries {
			if p == "" || entry.Path == p || strings.HasPrefix(entry.Path, p+"/") {
				out = append(out, entry)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
		return out, nil
	}
	for path, data := range f.files {
		if p == "" || path == p || strings.HasPrefix(path, p+"/") {
			out = append(out, sites.MirrorFile{Path: path, Kind: "file", Size: int64(len(data))})
		}
	}
	// Deterministic order so assertions are stable.
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func TestSyncImportsOnlyFileEntries(t *testing.T) {
	mirror := &fakeMirror{
		files: map[string][]byte{
			"/source/index.html":    []byte("home"),
			"/source/assets/app.js": []byte("app"),
		},
		entries: []sites.MirrorFile{
			{Path: "/source", Kind: "dir"},
			{Path: "/source/index.html", Kind: "file"},
			{Path: "/source/assets/app.js", Kind: "file"},
		},
	}
	h, root := newTestHandler(t, mirror)
	callOK(t, h, "create", map[string]any{"name": "demo", "slug": "demo", "visibility": "private"})

	out := callOK(t, h, "sync", map[string]any{"source_path": "/source", "slug": "demo"})
	gotTree := readTreeFiles(t, filepath.Join(root, "private", "demo"))
	sort.Strings(mirror.fetchCalls)
	// R-G118-GMFK
	if out["written"] != float64(2) || out["deleted"] != float64(0) || !reflect.DeepEqual(mirror.fetchCalls, []string{"/source/assets/app.js", "/source/index.html"}) || !reflect.DeepEqual(gotTree, map[string]string{"assets/app.js": "app", "index.html": "home"}) {
		t.Fatalf("sync result=%#v fetches=%v tree=%v, want two files and no directory fetch", out, mirror.fetchCalls, gotTree)
	}
}

func (f *fakeMirror) Fetch(_ context.Context, path string) ([]byte, error) {
	f.fetchCalls = append(f.fetchCalls, path)
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	data, ok := f.files[path]
	if !ok {
		return nil, sites.ErrNotFound
	}
	return data, nil
}

func newSyncCommitHandler(t *testing.T, mirror sites.MirrorClient, transport *commitRecordingTransport) (*testHandler, string) {
	t.Helper()
	version := sites.NewVersionClient("http://repos.test", &http.Client{Transport: transport})
	h, root := newTestHandlerWithVersionAndToken(t, version, nil, mirror)
	createTestSite(t, h, "demo")
	transport.reset()
	return h, root
}

func decodedCommitChanges(t *testing.T, call recordedCommitRequest) map[string]sites.FileChange {
	t.Helper()
	raw, ok := call.body["changes"].([]any)
	if !ok {
		t.Fatalf("commit changes = %#v, want array", call.body["changes"])
	}
	got := make(map[string]sites.FileChange, len(raw))
	for _, item := range raw {
		change, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("commit change = %#v, want object", item)
		}
		path, _ := change["path"].(string)
		op, _ := change["op"].(string)
		decoded := sites.FileChange{Path: path, Delete: op == "delete"}
		if !decoded.Delete {
			encoded, _ := change["content_b64"].(string)
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("decode %q content: %v", path, err)
			}
			decoded.Data = data
		}
		got[path] = decoded
	}
	return got
}

func readTreeFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("read tree %s: %v", root, err)
	}
	return got
}

func TestSyncCommitsBatchThenReconcilesAndMatchingResyncIsNoOp(t *testing.T) {
	mirror := &fakeMirror{files: map[string][]byte{
		"/source/index.html":    []byte("new home"),
		"/source/css/app.css":   []byte("body{}"),
		"/source/images/logo.x": {0x00, 0x01, 0xff},
	}}
	transport := &commitRecordingTransport{}
	h, root := newSyncCommitHandler(t, mirror, transport)
	dir := filepath.Join(root, "public", "demo")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("old home"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.txt"), []byte("remove me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// R-EYKO-KH2V
	out := callOK(t, h, "sync", map[string]any{"source_path": "/source", "slug": "demo"})
	if out["written"] != float64(3) || out["deleted"] != float64(1) {
		t.Fatalf("sync counts = %#v, want written 3 deleted 1", out)
	}
	calls := transport.snapshot()
	if len(calls) != 1 {
		t.Fatalf("repos calls = %d, want exactly one", len(calls))
	}
	if calls[0].clientID != testClientID || calls[0].body["actor"] != testClientID {
		t.Fatalf("commit actor/header = %v/%q, want %q", calls[0].body["actor"], calls[0].clientID, testClientID)
	}
	if calls[0].body["message"] != "sync /source" {
		t.Fatalf("commit message = %v, want sync /source", calls[0].body["message"])
	}
	changes := decodedCommitChanges(t, calls[0])
	if len(changes) != 4 {
		t.Fatalf("commit changes = %#v, want three writes and one delete", changes)
	}
	for rel, want := range map[string][]byte{"index.html": []byte("new home"), "css/app.css": []byte("body{}"), "images/logo.x": {0x00, 0x01, 0xff}} {
		change, ok := changes[rel]
		if !ok || change.Delete || string(change.Data) != string(want) {
			t.Errorf("commit change %q = %#v, want write %v", rel, change, want)
		}
	}
	if stale := changes["stale.txt"]; !stale.Delete {
		t.Fatalf("stale change = %#v, want delete", stale)
	}
	if len(mirror.listCalls) != 1 || len(mirror.fetchCalls) != 3 {
		t.Fatalf("mirror calls = list %#v fetch %#v, want one list and three fetches", mirror.listCalls, mirror.fetchCalls)
	}

	// R-EZSK-Y8TK
	wantTree := map[string]string{"index.html": "new home", "css/app.css": "body{}", "images/logo.x": string([]byte{0x00, 0x01, 0xff})}
	if got := readTreeFiles(t, dir); !mapsEqual(got, wantTree) {
		t.Fatalf("served tree = %#v, want %#v", got, wantTree)
	}
	site, err := h.store.Get(context.Background(), "demo")
	if err != nil || site.RepoSha != "write-sha" {
		t.Fatalf("repo sha = %q, %v, want write-sha", site.RepoSha, err)
	}
	transport.reset()
	out = callOK(t, h, "sync", map[string]any{"source_path": "/source", "slug": "demo"})
	if out["written"] != float64(0) || out["deleted"] != float64(0) {
		t.Fatalf("matching resync counts = %#v, want zeros", out)
	}
	if calls := transport.snapshot(); len(calls) != 0 {
		t.Fatalf("matching resync repos calls = %d, want zero", len(calls))
	}
}

func TestSyncPrefixesPublishRootWritesAndDeletesOnce(t *testing.T) {
	// R-4180-50XG
	mirror := &fakeMirror{files: map[string][]byte{"/source/index.html": []byte("new")}}
	transport := &commitRecordingTransport{}
	h, root := newSyncCommitHandler(t, mirror, transport)
	if err := h.store.SetPath(context.Background(), "demo", "public"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "public", "demo")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.html"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	callOK(t, h, "sync", map[string]any{"source_path": "/source", "slug": "demo"})
	calls := transport.snapshot()
	if len(calls) != 1 {
		t.Fatalf("sync repos calls = %d, want one", len(calls))
	}
	changes := decodedCommitChanges(t, calls[0])
	if write := changes["public/index.html"]; write.Delete || string(write.Data) != "new" {
		t.Fatalf("prefixed write = %#v", write)
	}
	if stale := changes["public/stale.html"]; !stale.Delete {
		t.Fatalf("prefixed delete = %#v", stale)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v, want exactly write and delete", changes)
	}
	if got := readTreeFiles(t, dir); !reflect.DeepEqual(got, map[string]string{"index.html": "new"}) {
		t.Fatalf("served tree = %#v", got)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func TestSyncFailedBatchCommitLeavesTreeAndShaUnchanged(t *testing.T) {
	mirror := &fakeMirror{files: map[string][]byte{
		"/source/index.html":  []byte("after"),
		"/source/assets/a.js": []byte("new"),
		"/source/third.txt":   []byte("third"),
	}}
	transport := &commitRecordingTransport{}
	h, root := newSyncCommitHandler(t, mirror, transport)
	dir := filepath.Join(root, "public", "demo")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetRepoSha(context.Background(), "demo", "before-sha"); err != nil {
		t.Fatal(err)
	}
	before := readTreeFiles(t, dir)
	transport.status = http.StatusBadRequest

	// R-F28D-PSAY
	result := call(t, h, "sync", map[string]any{"source_path": "/source", "slug": "demo"})
	if !result.IsError || result.StructuredContent["code"] != "internal" {
		t.Fatalf("sync result = %#v, want internal error envelope", result)
	}
	if calls := transport.snapshot(); len(calls) != 1 {
		t.Fatalf("failed sync repos calls = %d, want one", len(calls))
	}
	if after := readTreeFiles(t, dir); !mapsEqual(after, before) {
		t.Fatalf("tree after failed commit = %#v, want unchanged %#v", after, before)
	}
	site, err := h.store.Get(context.Background(), "demo")
	if err != nil || site.RepoSha != "before-sha" {
		t.Fatalf("repo sha after failed commit = %q, %v, want before-sha", site.RepoSha, err)
	}
}

// readWorking reads a file relative to a private site's directory, failing the
// test if it is absent.
func readWorking(t *testing.T, h *testHandler, slug, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.layout.SiteDir(sites.Private, slug), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read private site %s/%s: %v", slug, rel, err)
	}
	return string(b)
}

func TestSyncAbsentSlugReturnsNotFound(t *testing.T) {
	h, _ := newTestHandler(t, &fakeMirror{files: map[string][]byte{
		"/sites/marketing/index.html":   []byte("<h1>home</h1>"),
		"/sites/marketing/css/app.css":  []byte("body{}"),
		"/sites/marketing/img/logo.png": {0x89, 0x50, 0x4e, 0x47}, // binary-safe
	}})

	// R-56CN-HE21
	env := callErr(t, h, tool("sync"), map[string]any{"source_path": "/sites/marketing"})
	if env["code"] != "not_found" {
		t.Fatalf("error code = %v, want not_found", env["code"])
	}
	if _, err := h.store.Get(context.Background(), "marketing"); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("get marketing after failed sync err = %v, want ErrNotFound", err)
	}
	listed := callOK(t, h, tool("list"), map[string]any{})
	if arr, _ := listed["sites"].([]any); len(arr) != 0 {
		t.Fatalf("list after failed sync = %+v, want empty", listed)
	}
	if _, err := os.Stat(h.layout.SiteDir(sites.Public, "marketing")); !os.IsNotExist(err) {
		t.Fatalf("public dir should not exist after failed sync: %v", err)
	}
	if _, err := os.Stat(h.layout.SiteDir(sites.Private, "marketing")); !os.IsNotExist(err) {
		t.Fatalf("private dir should not exist after failed sync: %v", err)
	}
}

// TestSyncExistingReconciles: a second sync over an existing site writes new
// upstream files and deletes working files that vanished upstream.
func TestSyncExistingReconciles(t *testing.T) {
	mirror := &fakeMirror{files: map[string][]byte{
		"/site/a.html": []byte("a"),
		"/site/b.html": []byte("b"),
	}}
	h, _ := newTestHandler(t, mirror)

	callOK(t, h, tool("create"), map[string]any{"name": "blog", "slug": "blog", "visibility": "private"})
	if out := callOK(t, h, tool("sync"), map[string]any{"source_path": "/site", "slug": "blog"}); out["written"] != float64(2) {
		t.Fatalf("first sync written = %v, want 2", out["written"])
	}

	// Upstream changes: a.html updated, b.html removed, c.html added.
	mirror.files = map[string][]byte{
		"/site/a.html": []byte("a2"),
		"/site/c.html": []byte("c"),
	}
	out := callOK(t, h, tool("sync"), map[string]any{"source_path": "/site", "slug": "blog"})
	if out["written"] != float64(2) {
		t.Fatalf("second sync written = %v, want 2", out["written"])
	}
	if out["deleted"] != float64(1) {
		t.Fatalf("second sync deleted = %v, want 1", out["deleted"])
	}
	if readWorking(t, h, "blog", "a.html") != "a2" {
		t.Fatal("a.html not overwritten")
	}
	if readWorking(t, h, "blog", "c.html") != "c" {
		t.Fatal("c.html not written")
	}
	if _, err := os.Stat(filepath.Join(h.layout.SiteDir(sites.Private, "blog"), "b.html")); !os.IsNotExist(err) {
		t.Fatalf("b.html should be deleted, stat err = %v", err)
	}
}

func TestSyncPreservesRequestContextThroughMirrorCalls(t *testing.T) {
	var serverRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverRequests++
		switch r.URL.Path {
		case "/list":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("cursor") == "" {
				_, _ = w.Write([]byte(`{"files":[],"next_cursor":"page-2"}`))
				return
			}
			_, _ = w.Write([]byte(`{"files":[{"path":"/source/index.html","kind":"file","size":5}]}`))
		case "/content":
			_, _ = w.Write([]byte("hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	recorder := &correlationRecordingTransport{next: srv.Client().Transport}
	mirror := sites.NewMirrorClient(srv.URL, &http.Client{Transport: recorder})
	h, _ := newTestHandler(t, mirror)
	callOK(t, h, tool("create"), map[string]any{"name": "demo", "slug": "demo", "visibility": "private"})

	correlationID := "01JZZZZZZZZZZZZZZZZZZZZZZZ"
	withCorrelation := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := correlation.WithContext(r.Context(), correlationID)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
	out := callOK(t, withCorrelation, tool("sync"), map[string]any{"source_path": "/source", "slug": "demo"})
	if out["written"] != float64(1) {
		t.Fatalf("sync written = %v, want 1", out["written"])
	}

	// R-BPLY-KD8S — the MCP request context must remain live across both cursor
	// pages and the content fetch for the chassis transport to read it.
	if len(recorder.ids) != serverRequests {
		t.Fatalf("recorded contexts = %d, server requests = %d", len(recorder.ids), serverRequests)
	}
	if len(recorder.ids) != 3 {
		t.Fatalf("recorded contexts = %d, want 3", len(recorder.ids))
	}
	for i, got := range recorder.ids {
		if got != correlationID {
			t.Errorf("outgoing request %d correlation id = %q, want %q", i, got, correlationID)
		}
	}
}

// TestSyncSlugDerivation: a valid basename auto-derives the slug; an invalid
// basename with no explicit slug is a validation error.
func TestSyncSlugDerivation(t *testing.T) {
	h, _ := newTestHandler(t, &fakeMirror{files: map[string][]byte{
		"/projects/good-slug/index.html": []byte("ok"),
		"/x/Marketing Site/index.html":   []byte("x"),
	}})

	// Valid basename derives.
	callOK(t, h, tool("create"), map[string]any{"name": "good-slug", "slug": "good-slug", "visibility": "private"})
	out := callOK(t, h, tool("sync"), map[string]any{"source_path": "/projects/good-slug"})
	if out["slug"] != "good-slug" {
		t.Fatalf("derived slug = %v, want good-slug", out["slug"])
	}

	// Invalid basename ("Marketing Site" has a space + uppercase), no slug given.
	env := callErr(t, h, tool("sync"), map[string]any{"source_path": "/x/Marketing Site"})
	if env["code"] != "validation" {
		t.Fatalf("error code = %v, want validation", env["code"])
	}
	if msg, _ := env["message"].(string); !strings.Contains(msg, "slug") {
		t.Fatalf("validation message should mention slug, got %q", msg)
	}
}

// TestSyncMissingSourcePath: source_path is required.
func TestSyncMissingSourcePath(t *testing.T) {
	h, _ := newTestHandler(t, &fakeMirror{files: map[string][]byte{}})
	env := callErr(t, h, tool("sync"), map[string]any{})
	if env["code"] != "validation" {
		t.Fatalf("error code = %v, want validation", env["code"])
	}
}

func TestSyncMirrorFailuresAreSourceUnavailable(t *testing.T) {
	cases := []struct {
		name   string
		mirror sites.MirrorClient
	}{
		{"unconfigured", nil},
		{"list", &fakeMirror{listErr: errors.New("mirror list down")}},
		{"fetch", &fakeMirror{files: map[string][]byte{"/source/a": []byte("a")}, fetchErr: errors.New("mirror fetch down")}},
	}

	// R-D28W-PWQ4
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h *testHandler
			if tc.mirror == nil {
				h, _ = newTestHandler(t)
			} else {
				h, _ = newTestHandler(t, tc.mirror)
			}
			callOK(t, h, "create", map[string]any{"name": "demo", "slug": "demo", "visibility": "private"})
			env := callErr(t, h, "sync", map[string]any{"source_path": "/source", "slug": "demo"})
			if env["code"] != "source_unavailable" {
				t.Fatalf("sync %s code = %v, want source_unavailable", tc.name, env["code"])
			}
		})
	}
}

func TestSyncPublicSiteUsesPublicDirectory(t *testing.T) {
	mirror := &fakeMirror{files: map[string][]byte{
		"/feed/index.html": []byte("v1"),
	}}
	h, _ := newTestHandler(t, mirror)

	callOK(t, h, tool("create"), map[string]any{"name": "live", "slug": "live", "visibility": "public"})

	// R-56CN-HE21
	out := callOK(t, h, tool("sync"), map[string]any{"source_path": "/feed", "slug": "live"})
	if out["written"] != float64(1) {
		t.Fatalf("sync written = %v, want 1", out["written"])
	}

	publicPath := filepath.Join(h.layout.SiteDir(sites.Public, "live"), "index.html")
	b, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatalf("read public file: %v", err)
	}
	if string(b) != "v1" {
		t.Fatalf("public content = %q, want v1", b)
	}
	after, err := h.store.Get(context.Background(), "live")
	if err != nil {
		t.Fatalf("get live: %v", err)
	}
	if after.Visibility != sites.Public {
		t.Fatal("site should remain public after sync")
	}
}
