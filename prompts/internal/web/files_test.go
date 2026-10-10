package web_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/web"
)

type fixture struct {
	cfg    web.FilesConfig
	d      *db.DB
	writer *telemetry.Writer
	root   string
	calls  []page.User
	mu     sync.Mutex
}

func setup(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	clock := func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(root, "catalog.db"), Migrations: prompts.Migrations(), Now: clock})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	var random []byte
	for i := 0; i < 128; i++ {
		random = append(random, 0, 0, 0, 0, 0, 0, 0, byte(i))
	}
	s := store.New(d, store.Config{Now: clock, Rand: bytes.NewReader(random)})
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: &telemetry.Capture{}, Stderr: io.Discard, Now: clock, Rand: bytes.NewReader(make([]byte, 4096))})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "done") })
	set, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	root = filepath.Join(root, "runs")
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	core := runs.New(runs.Config{Store: s, Writer: writer, Runs: root, Now: clock, Rand: bytes.NewReader(random), KeepDays: 1, KeepCount: 1, MaxActive: 1, MaxQueued: 1, PromptSeconds: 1, OutputMaxBytes: 100, MaxToolCalls: 1, RunMemoryMaxBytes: 1, RunPidsMax: 1, ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	f := &fixture{d: d, root: root, writer: writer}
	// R-YRHK-ANA1: positional construction proves every declared field and order.
	f.cfg = web.FilesConfig{func(u page.User) page.Banner {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, u)
		return page.Banner{Service: pages.ServiceName, Release: "fixture-release", Commit: "fixture-commit"}
	}, set, s, core}
	return f
}
func create(t *testing.T, f *fixture, name, owner string) store.Prompt {
	t.Helper()
	p, e := f.cfg.Store.Create(context.Background(), store.Draft{Name: name, Owner: owner, Model: "fixture-model", Prompt: "fixture-text"})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func add(t *testing.T, f *fixture, p store.Prompt, n int, status string) store.Run {
	t.Helper()
	r, e := f.cfg.Store.AddRun(context.Background(), store.Run{ID: fmt.Sprintf("prr_%016x", n), Prompt: p.ID, Model: p.Model, User: p.Owner, RequestID: "fixture-request", Trigger: store.TriggerManual, Status: status, Started: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func put(t *testing.T, path string, b []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func req(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("X-User-Id", "alice")
	r.Header.Set("X-Request-Id", "fixture-request")
	return r
}

// R-YSPG-OF0Q R-YV59-FYI4: use the exported handler with its identity gate.
func serve(f *fixture, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	identity.Require(web.Files(f.cfg)).ServeHTTP(w, r)
	return w
}
func base(p store.Prompt, r store.Run) string { return "/" + p.Name + "/runs/" + r.ID + "/" }
func equal(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v want %#v", a, b)
	}
}
func missingBody(t *testing.T) string {
	t.Helper()
	set, e := page.Templates().ParseFS(prompts.Assets(), "*.html")
	if e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	if e := set.ExecuteTemplate(&b, "notfound", pages.NoticeData{Banner: page.Banner{Service: pages.ServiceName, Release: "fixture-release", Commit: "fixture-commit"}}); e != nil {
		t.Fatal(e)
	}
	return b.String()
}
func missing(t *testing.T, f *fixture, path string) {
	t.Helper()
	for _, m := range []string{"GET", "HEAD"} {
		w := serve(f, req(m, path))
		equal(t, w.Code, 404)
		equal(t, w.Header(), http.Header{"Content-Type": []string{"text/html; charset=utf-8"}})
		b := missingBody(t)
		if m == "HEAD" {
			b = ""
		}
		equal(t, w.Body.String(), b)
	}
}
func pair(t *testing.T, f *fixture, path string) *httptest.ResponseRecorder {
	t.Helper()
	g := serve(f, req("GET", path))
	h := serve(f, req("HEAD", path))
	equal(t, h.Code, g.Code)
	equal(t, h.Header(), g.Header())
	equal(t, h.Body.Len(), 0)
	return g
}

// R-YWD5-TQ8T R-Z4WG-I4FO R-Z64C-VW6D R-Z7C9-9NX2 R-Z8K5-NFNR R-Z9S2-17EG
func TestFilesBytesAndHeaders(t *testing.T) {
	f := setup(t)
	p := create(t, f, "writer", "alice")
	r := add(t, f, p, 1, store.StatusRunning)
	folder := f.cfg.Runs.Folder(r)
	for _, name := range []string{runs.InputFile, runs.StdoutFile, runs.StderrFile, runs.TranscriptFile} {
		b := []byte{0, 255, 'x', '\n'}
		if name == runs.StderrFile {
			b = nil
		}
		put(t, filepath.Join(folder, name), b)
		w := pair(t, f, base(p, r)+name)
		equal(t, w.Code, 200)
		equal(t, w.Body.Bytes(), b)
		equal(t, w.Header(), http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}, "Content-Length": []string{strconv.Itoa(len(b))}, "X-Content-Type-Options": []string{"nosniff"}})
	}
	for _, name := range []string{".env", "report.html", "index.html", "charts/index.html", "transcript.jsonl", "quote\"slash\\.txt", "space name", "nonasciié", "control\x01"} {
		b := []byte("payload:" + name)
		put(t, filepath.Join(folder, runs.WorkDir, name), b)
		parts := strings.Split(name, "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		w := pair(t, f, base(p, r)+runs.WorkDir+"/"+strings.Join(parts, "/"))
		d := "attachment"
		if name != "nonasciié" && name != "control\x01" {
			bn := filepath.Base(name)
			bn = strings.ReplaceAll(bn, "\\", "\\\\")
			bn = strings.ReplaceAll(bn, "\"", "\\\"")
			d += "; filename=\"" + bn + "\""
		}
		equal(t, w.Code, 200)
		equal(t, w.Body.Bytes(), b)
		equal(t, w.Header(), http.Header{"Content-Type": []string{"application/octet-stream"}, "Content-Length": []string{strconv.Itoa(len(b))}, "Content-Disposition": []string{d}, "X-Content-Type-Options": []string{"nosniff"}})
	}
	for _, name := range []string{runs.StdoutFile, runs.TranscriptFile, runs.WorkDir + "/growing"} {
		for _, b := range []string{"first", "first plus later"} {
			put(t, filepath.Join(folder, name), []byte(b))
			equal(t, pair(t, f, base(p, r)+name).Body.String(), b)
		}
	}
}

// R-Z2GN-QKYA R-Z3OK-4COZ
func TestFilesRefuseAbsentAndUnsafe(t *testing.T) {
	f := setup(t)
	p := create(t, f, "writer", "alice")
	r := add(t, f, p, 1, store.StatusRunning)
	folder := f.cfg.Runs.Folder(r)
	for _, name := range []string{runs.InputFile, runs.StdoutFile, runs.StderrFile, runs.TranscriptFile, "notes.txt", "out/report.csv", "work/charts/sales.svg", "work/report.csv", "work/report.html"} {
		put(t, filepath.Join(folder, name), []byte("fixture"))
	}
	other := create(t, f, "other", "alice")
	foreign := create(t, f, "foreign", "bob")
	fr := add(t, f, foreign, 2, store.StatusRunning)
	for _, path := range []string{base(other, r) + runs.InputFile, base(foreign, fr) + runs.InputFile, "/unknown/runs/" + r.ID + "/stdout", base(p, store.Run{ID: "prr_ffffffffffffffff"}) + runs.InputFile} {
		missing(t, f, path)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	put(t, outside, []byte("external"))
	for link, target := range map[string]string{"work/link": "../stdout", "work/external": outside, "work/dirlink": "charts"} {
		if e := os.Symlink(target, filepath.Join(folder, link)); e != nil {
			t.Fatal(e)
		}
	}
	for _, tail := range []string{"work", "work/", "work/charts", "work/charts/", "work/missing.txt", "notes.txt", "out/report.csv", "stdout/", "transcript.jsonl/", "work//report.csv", "Stdout", "Transcript.jsonl", "work/charts/../report.csv", "work/../transcript.jsonl", "work/./report.csv", "work/%2e%2e/input.json", "work/charts%2Fsales.svg", "work/charts%252Fsales.svg", "work/sales%00.svg", "./stdout", "work/link", "work/external", "work/dirlink/sales.svg"} {
		missing(t, f, base(p, r)+tail)
	}
	if e := os.Remove(filepath.Join(folder, runs.TranscriptFile)); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(folder, runs.TranscriptFile)); e != nil {
		t.Fatal(e)
	}
	missing(t, f, base(p, r)+runs.TranscriptFile)
	if e := os.Rename(filepath.Join(folder, "work"), filepath.Join(folder, "saved")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("saved", filepath.Join(folder, "work")); e != nil {
		t.Fatal(e)
	}
	missing(t, f, base(p, r)+"work/report.csv")
	if e := os.RemoveAll(folder); e != nil {
		t.Fatal(e)
	}
	missing(t, f, base(p, r)+runs.InputFile)
}

// R-ZAZY-EZ55
func TestQueuedFiles(t *testing.T) {
	f := setup(t)
	p := create(t, f, "waiting", "alice")
	r := add(t, f, p, 1, store.StatusQueued)
	put(t, filepath.Join(f.cfg.Runs.Folder(r), runs.InputFile), []byte("queued-input"))
	equal(t, pair(t, f, base(p, r)+runs.InputFile).Body.String(), "queued-input")
	for _, n := range []string{runs.StdoutFile, runs.StderrFile, runs.TranscriptFile} {
		missing(t, f, base(p, r)+n)
	}
}

// R-YXL2-7HZI R-Z00U-Z1GW R-Z18R-CT7L
func TestFilesMethodsAndCatalogFailure(t *testing.T) {
	f := setup(t)
	p := create(t, f, "writer", "alice")
	r := add(t, f, p, 1, store.StatusRunning)
	put(t, filepath.Join(f.cfg.Runs.Folder(r), runs.InputFile), []byte("fixture"))
	tails := []string{runs.InputFile, runs.TranscriptFile, "work/../input.json", "Stdout", "work/missing.txt"}
	paths := []string{"/unknown/runs/missing/input.json", base(p, store.Run{ID: "prr_ffffffffffffffff"}) + runs.InputFile}
	for _, x := range tails {
		paths = append(paths, base(p, r)+x)
	}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, path := range paths {
			for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
				w := serve(f, req(m, path))
				equal(t, w.Code, 405)
				equal(t, w.Header(), http.Header{"Allow": []string{"GET, HEAD"}})
				equal(t, w.Body.Len(), 0)
			}
		}
	}
	for _, cancelled := range []bool{false, true} {
		f.d.SetFailing(!cancelled)
		for _, path := range paths {
			for _, m := range []string{"GET", "HEAD"} {
				r := req(m, path)
				if cancelled {
					ctx, cancel := context.WithCancel(r.Context())
					cancel()
					r = r.WithContext(ctx)
				}
				w := serve(f, r)
				equal(t, w.Code, 503)
				equal(t, w.Header(), http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}})
				b := store.Unreachable + "\n"
				if m == "HEAD" {
					b = ""
				}
				equal(t, w.Body.String(), b)
			}
		}
	}
}

type entry struct {
	Mode  fs.FileMode
	Size  int64
	Bytes string
}

func snapshot(t *testing.T, root string) map[string]entry {
	t.Helper()
	out := map[string]entry{}
	e := filepath.WalkDir(root, func(path string, _ fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		value := ""
		if info.Mode().IsRegular() {
			b, e := fixtureBytes(path)
			if e != nil {
				return e
			}
			value = string(b)
		} else if info.Mode()&os.ModeSymlink != 0 {
			value, e = os.Readlink(path)
			if e != nil {
				return e
			}
			b, e := fixtureBytes(path)
			if e == nil {
				value += "\x00" + string(b)
			}
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		out[rel] = entry{info.Mode(), info.Size(), value}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}

// R-L6TU-UPYQ R-ZDFR-6IMJ R-ZENN-KAD8
func TestFilesReadOnlyBannerAndConcurrency(t *testing.T) {
	f := setup(t)
	p := create(t, f, "writer", "alice")
	r := add(t, f, p, 1, store.StatusRunning)
	never := create(t, f, "never", "alice")
	folder := f.cfg.Runs.Folder(r)
	put(t, filepath.Join(folder, runs.InputFile), []byte("data"))
	external := filepath.Join(t.TempDir(), "target")
	put(t, external, []byte("external"))
	if e := os.MkdirAll(filepath.Join(folder, "work"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(external, filepath.Join(folder, "work/link")); e != nil {
		t.Fatal(e)
	}
	before := snapshot(t, f.root)
	externalBefore := snapshot(t, filepath.Dir(external))
	catalog, e := f.cfg.Store.List(context.Background(), "alice")
	if e != nil {
		t.Fatal(e)
	}
	records, e := f.cfg.Store.Runs(context.Background(), p.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{base(p, r) + runs.InputFile, base(p, r) + "missing", base(p, r) + "work/link", "/" + never.Name + "/runs/missing/stdout"} {
		for _, m := range []string{"GET", "HEAD", "POST"} {
			f.mu.Lock()
			f.calls = nil
			f.mu.Unlock()
			w := serve(f, req(m, path))
			if w.Header().Get("Set-Cookie") != "" {
				t.Fatal("cookie")
			}
			f.mu.Lock()
			calls := append([]page.User(nil), f.calls...)
			f.mu.Unlock()
			if w.Code == 404 {
				equal(t, calls, []page.User{{}})
			} else {
				equal(t, len(calls), 0)
			}
		}
	}
	f.d.SetFailing(true)
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	equal(t, serve(f, req("GET", base(p, r)+runs.InputFile)).Code, 503)
	equal(t, len(f.calls), 0)
	f.d.SetFailing(false)
	handler := identity.Require(web.Files(f.cfg))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req("GET", base(p, r)+runs.InputFile))
			if w.Code != 200 || w.Body.String() != "data" {
				t.Error("concurrent response")
			}
		})
	}
	wg.Wait()
	equal(t, snapshot(t, f.root), before)
	equal(t, snapshot(t, filepath.Dir(external)), externalBefore)
	after, e := f.cfg.Store.List(context.Background(), "alice")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, after, catalog)
	rs, e := f.cfg.Store.Runs(context.Background(), p.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, rs, records)
	b, e := fixtureBytes(external)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, string(b), "external")
}

func fullHandler(f *fixture) http.Handler {
	return web.Handler(web.Config{Banner: f.cfg.Banner, MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: f.writer}), Store: f.cfg.Store, Runs: f.cfg.Runs, KeepDays: 1, KeepCount: 1, Telemetry: f.writer})
}

func fixtureBytes(path string) ([]byte, error) {
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.Base(path))
}
