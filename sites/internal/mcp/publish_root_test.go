package mcp

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"eventplane/consumer"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sites/internal/sites"
	"sync"
	"testing"
)

type publishRootRepos struct {
	server *httptest.Server

	mu      sync.Mutex
	calls   []string
	entries []sites.TreeEntry
	sha     string
	status  int
}

func newPublishRootRepos(t *testing.T) *publishRootRepos {
	t.Helper()
	r := &publishRootRepos{sha: "export-sha"}
	r.server = httptest.NewServer(http.HandlerFunc(r.serveHTTP))
	t.Cleanup(r.server.Close)
	return r
}

func (r *publishRootRepos) serveHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.calls = append(r.calls, req.URL.Path)
	entries := append([]sites.TreeEntry(nil), r.entries...)
	sha, status := r.sha, r.status
	r.mu.Unlock()

	if status != 0 {
		http.Error(w, "rejected", status)
		return
	}
	switch req.URL.Path {
	case "/mcp":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"kind":"sites"}}}`))
	case "/archive":
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("X-Repos-Rev", sha)
		tw := tar.NewWriter(w)
		for _, entry := range entries {
			_ = tw.WriteHeader(&tar.Header{Name: entry.Path, Mode: 0o644, Size: int64(len(entry.Data))})
			_, _ = tw.Write(entry.Data)
		}
		_ = tw.Close()
	default:
		http.NotFound(w, req)
	}
}

func (r *publishRootRepos) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

func (r *publishRootRepos) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *publishRootRepos) setExport(entries []sites.TreeEntry, sha string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append([]sites.TreeEntry(nil), entries...)
	r.sha = sha
}

func newPublishRootHandler(t *testing.T, repos *publishRootRepos) (*testHandler, string) {
	t.Helper()
	return newTestHandlerWithVersion(t, sites.NewVersionClient(repos.server.URL, repos.server.Client()))
}

func TestCreateAcceptsNormalizedOptionalPublishRootBeforeSideEffects(t *testing.T) {
	// R-3V4I-867Z
	repos := newPublishRootRepos(t)
	h, root := newPublishRootHandler(t, repos)
	created := callOK(t, h, "create", map[string]any{"name": "Docs", "slug": "docs", "visibility": "public", "path": "/docs/site/"})
	stored, err := h.store.Get(context.Background(), "docs")
	if err != nil || stored.Path != "docs/site" || created["path"] != "docs/site" {
		t.Fatalf("normalized create path: stored=%q projection=%v err=%v", stored.Path, created["path"], err)
	}
	defaulted := callOK(t, h, "create", map[string]any{"name": "Root", "slug": "root", "visibility": "public"})
	stored, err = h.store.Get(context.Background(), "root")
	if err != nil || stored.Path != "" || defaulted["path"] != "" {
		t.Fatalf("default create path: stored=%q projection=%v err=%v", stored.Path, defaulted["path"], err)
	}

	repos.reset()
	bad := call(t, h, "create", map[string]any{"name": "Bad", "slug": "bad", "visibility": "public", "path": "a/../b"})
	if !bad.IsError || bad.StructuredContent["code"] != "validation" {
		t.Fatalf("invalid create = %+v, want validation", bad)
	}
	if _, err := h.store.Get(context.Background(), "bad"); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("invalid create row error = %v, want not found", err)
	}
	if _, err := os.Stat(filepath.Join(root, sites.PublicSeg, "bad")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid create directory stat = %v, want not exist", err)
	}
	if calls := repos.snapshot(); len(calls) != 0 {
		t.Fatalf("invalid create repos calls = %v, want none", calls)
	}
}

func TestSetPathExportsThenReconcilesStrippedSubtree(t *testing.T) {
	// R-3WCE-LXYO
	repos := newPublishRootRepos(t)
	repos.setExport([]sites.TreeEntry{
		{Path: "project.txt", Data: []byte("outside")},
		{Path: "public/index.html", Data: []byte("exported home")},
		{Path: "public/css/app.css", Data: []byte("body{}")},
	}, "S")
	h, _ := newPublishRootHandler(t, repos)
	createTestSite(t, h, "demo")
	dir := h.layout.SiteDir(sites.Public, "demo")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("old home"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	repos.reset()
	out := callOK(t, h, "set_path", map[string]any{"slug": "demo", "path": "public"})
	if calls := repos.snapshot(); !reflect.DeepEqual(calls, []string{"/archive"}) {
		t.Fatalf("set_path repos calls = %v, want one export", calls)
	}
	wantTree := map[string]string{"index.html": "exported home", "css/app.css": "body{}"}
	if got := readTreeFiles(t, dir); !reflect.DeepEqual(got, wantTree) {
		t.Fatalf("served tree = %#v, want %#v", got, wantTree)
	}
	stored, err := h.store.Get(context.Background(), "demo")
	if err != nil || stored.Path != "public" || stored.RepoSha != "S" || out["path"] != "public" {
		t.Fatalf("set_path state = path %q sha %q projection %v err %v", stored.Path, stored.RepoSha, out["path"], err)
	}
}

func TestSetPathReconcileReplacesDirectoryWithFile(t *testing.T) {
	repos := newPublishRootRepos(t)
	repos.setExport([]sites.TreeEntry{
		{Path: "public/index.html", Data: []byte("same home")},
		{Path: "public/qr", Data: []byte("qr file")},
	}, "type-change-sha")
	h, _ := newPublishRootHandler(t, repos)
	createTestSite(t, h, "demo")
	dir := h.layout.SiteDir(sites.Public, "demo")
	if err := os.MkdirAll(filepath.Join(dir, "qr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qr", "index.html"), []byte("old qr page"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("same home"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := call(t, h, "set_path", map[string]any{"slug": "demo", "path": "public"})
	got := readTreeFiles(t, dir)
	info, statErr := os.Stat(filepath.Join(dir, "qr"))
	// R-FJYN-3U1U
	if result.IsError || !reflect.DeepEqual(got, map[string]string{"index.html": "same home", "qr": "qr file"}) || statErr != nil || !info.Mode().IsRegular() {
		t.Fatalf("set_path directory-to-file result=%+v tree=%#v qr=%v statErr=%v", result, got, info, statErr)
	}
}

func TestSetPathExportFailuresLeaveRowShaAndFilesUntouched(t *testing.T) {
	// R-3XKA-ZPPD
	tests := []struct {
		name string
		new  func(*testing.T) sites.VersionClient
		code string
	}{
		{"connection refused", func(t *testing.T) sites.VersionClient {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			base := "http://" + listener.Addr().String()
			_ = listener.Close()
			return sites.NewVersionClient(base, &http.Client{})
		}, "source_unavailable"},
		{"rejected", func(t *testing.T) sites.VersionClient {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusBadRequest) }))
			t.Cleanup(srv.Close)
			return sites.NewVersionClient(srv.URL, srv.Client())
		}, "internal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTestHandlerWithVersion(t, tc.new(t))
			seedTestSite(t, h, "demo")
			dir := h.layout.SiteDir(sites.Public, "demo")
			if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("before"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := h.store.SetRepoSha(context.Background(), "demo", "before-sha"); err != nil {
				t.Fatal(err)
			}
			result := call(t, h, "set_path", map[string]any{"slug": "demo", "path": "public"})
			if !result.IsError || result.StructuredContent["code"] != tc.code {
				t.Fatalf("set_path = %+v, want %s", result, tc.code)
			}
			stored, err := h.store.Get(context.Background(), "demo")
			if err != nil || stored.Path != "" || stored.RepoSha != "before-sha" {
				t.Fatalf("failed set_path row = %+v err=%v", stored, err)
			}
			if got := readTreeFiles(t, dir); !reflect.DeepEqual(got, map[string]string{"index.html": "before"}) {
				t.Fatalf("failed set_path tree = %#v", got)
			}
		})
	}
}

func TestSetPathRejectsInvalidMissingAndUnknownBeforeExport(t *testing.T) {
	// R-3YS7-DHG2
	repos := newPublishRootRepos(t)
	h, _ := newPublishRootHandler(t, repos)
	createTestSite(t, h, "demo")
	repos.reset()
	for _, tc := range []struct {
		args map[string]any
		code string
	}{
		{map[string]any{"slug": "demo", "path": "a/../b"}, "validation"},
		{map[string]any{"slug": "missing", "path": "public"}, "not_found"},
		{map[string]any{"slug": "demo"}, "validation"},
	} {
		result := call(t, h, "set_path", tc.args)
		if !result.IsError || result.StructuredContent["code"] != tc.code {
			t.Errorf("set_path(%v) = %+v, want %s", tc.args, result, tc.code)
		}
	}
	if calls := repos.snapshot(); len(calls) != 0 {
		t.Fatalf("refused set_path repos calls = %v, want none", calls)
	}
	if stored, err := h.store.Get(context.Background(), "demo"); err != nil || stored.Path != "" {
		t.Fatalf("refused set_path changed row = %+v err=%v", stored, err)
	}
	callOK(t, h, "set_path", map[string]any{"slug": "demo", "path": ""})
}

func TestSetPathAbsentSubtreeEmptiesThenPushMaterializesIt(t *testing.T) {
	// R-43NS-WKEU
	repos := newPublishRootRepos(t)
	repos.setExport([]sites.TreeEntry{{Path: "README.md", Data: []byte("outside")}}, "empty-sha")
	h, _ := newPublishRootHandler(t, repos)
	createTestSite(t, h, "demo")
	dir := h.layout.SiteDir(sites.Public, "demo")
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	callOK(t, h, "set_path", map[string]any{"slug": "demo", "path": "public"})
	if got := readTreeFiles(t, dir); len(got) != 0 {
		t.Fatalf("absent subtree served tree = %#v, want empty", got)
	}
	stored, err := h.store.Get(context.Background(), "demo")
	if err != nil || stored.Path != "public" || stored.RepoSha != "empty-sha" {
		t.Fatalf("empty subtree row = %+v err=%v", stored, err)
	}

	repos.setExport([]sites.TreeEntry{{Path: "README.md", Data: []byte("outside")}, {Path: "public/index.html", Data: []byte("pushed")}}, "push-sha")
	payload, _ := json.Marshal(map[string]string{"branch": "main", "sha": "push-sha"})
	event := consumer.Event{Source: "repos", Kind: "push", Subject: "sites/demo", Payload: payload}
	if err := sites.NewPushHandler(h.store, h.layout, sites.NewVersionClient(repos.server.URL, repos.server.Client()))(context.Background(), event); err != nil {
		t.Fatalf("push materialization: %v", err)
	}
	if got := readTreeFiles(t, dir); !reflect.DeepEqual(got, map[string]string{"index.html": "pushed"}) {
		t.Fatalf("pushed subtree served tree = %#v", got)
	}
}
