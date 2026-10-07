package web_test

import (
	"bytes"
	"context"
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
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/web"
)

type fileFixture struct {
	t       *testing.T
	pages   *pages.Set
	banner  page.Banner
	dir     string
	st      *store.Store
	db      *db.DB
	core    *runs.Core
	handler http.Handler
	run     store.Run
	folder  string
	mu      sync.Mutex
	banners int
}

func fileSetup(t *testing.T) *fileFixture {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "catalog"), Migrations: scripts.Migrations(), Now: func() time.Time { return time.Unix(100, 0) }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: func() time.Time { return time.Unix(100, 0) }, Rand: fileRandom()})
	sc, e := st.Create(context.Background(), store.Draft{Owner: "alice", Name: "report", Repo: "rep_1111111111111111", Ref: "main"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := st.AddRun(context.Background(), store.Run{ID: "run_1111111111111111", Script: sc.ID, Ref: "main", User: "alice", Trigger: store.TriggerManual, Status: store.StatusFailed, Reason: store.ReasonCommitMissing, Started: time.Unix(100, 0), Finished: time.Unix(100, 0)})
	if e != nil {
		t.Fatal(e)
	}
	set, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	core := runs.New(runs.Config{MaxActive: 100, MaxQueued: 100, Runs: dir})
	f := &fileFixture{t: t, pages: set, banner: page.Banner{Service: "distinct-files-banner", Version: "fixture", Email: "banner@example.test", ProfileURL: "/fixture-profile", LogoutURL: "/fixture-logout"}, dir: dir, st: st, db: d, core: core, run: r, folder: core.Folder(r)}
	// R-ZJ5Q-8VX6 R-ZKDM-MNNV R-ZMTF-E759
	f.handler = identity.Require(web.Files(web.FilesConfig{func(u page.User) page.Banner {
		if !reflect.DeepEqual(u, page.User{}) {
			t.Errorf("nonzero user: %#v", u)
		}
		f.mu.Lock()
		f.banners++
		f.mu.Unlock()
		return f.banner
	}, set, st, core}))
	fileWrite(t, filepath.Join(f.folder, "input.json"), []byte("{\"x\":1}"))
	fileWrite(t, filepath.Join(f.folder, "stdout"), []byte("hello\n"))
	fileWrite(t, filepath.Join(f.folder, "stderr"), nil)
	fileWrite(t, filepath.Join(f.folder, "out", "charts", "sales.svg"), []byte{0, 255, 13, 10})
	fileWrite(t, filepath.Join(f.folder, "tree", "main.py"), []byte("private"))
	return f
}
func fileWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func (f *fileFixture) request(ctx context.Context, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("X-User-Id", user)
	r.Header.Set("Host", "scripts.test")
	r.Header.Set("X-Forwarded-Proto", "https")
	if ctx != nil {
		r = r.WithContext(ctx)
	}
	f.mu.Lock()
	before := f.banners
	f.mu.Unlock()
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	f.mu.Lock()
	calls := f.banners - before
	f.mu.Unlock()
	wantCalls := 0
	if w.Code == 404 {
		wantCalls = 1
	}
	if calls != wantCalls {
		f.t.Errorf("banner calls %d for status %d", calls, w.Code)
	}
	if len(w.Header().Values("Set-Cookie")) != 0 {
		f.t.Error("file response sent cookie")
	}
	if w.Code == 404 {
		expected := httptest.NewRecorder()
		f.pages.Write(expected, r, 404, "notfound", pages.NoticeData{Banner: f.banner})
		if w.Body.String() != expected.Body.String() || !reflect.DeepEqual(w.Header(), http.Header{"Content-Type": {"text/html; charset=utf-8"}}) {
			f.t.Errorf("wrong not-found body/headers: %v %q", w.Header(), w.Body.String())
		}
	}
	if w.Code == 503 {
		want := store.Unreachable + "\n"
		if method == "HEAD" {
			want = ""
		}
		if w.Body.String() != want || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) {
			f.t.Errorf("wrong catalog refusal %v %q", w.Header(), w.Body.String())
		}
	}
	if w.Code == 405 && (!reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0) {
		f.t.Error("wrong method refusal", w.Header(), w.Body.String())
	}
	return w
}
func fileURL(f *fileFixture, suffix string) string { return "/report/runs/" + f.run.ID + "/" + suffix }
func TestFilesWholeAndHead(t *testing.T) {
	// R-ZLLJ-0FEK R-ZO1B-RYVY R-ZVCQ-2LC4 R-Y359-25N8 R-ZXSI-U4TI R-ZZ0F-7WK7
	f := fileSetup(t)
	for _, name := range []string{".env", "index.html", "charts/index.html", "quote\"\\.txt", "東京.txt", "low\x1f.txt", "del\x7f.txt", " \"\\~"} {
		fileWrite(t, filepath.Join(f.folder, "out", name), []byte(name))
	}
	for _, suffix := range []string{"input.json", "stdout", "stderr", "out/charts/sales.svg", "out/.env", "out/index.html", "out/charts/index.html", "out/quote%22%5C.txt", "out/" + url.PathEscape("東京.txt"), "out/low%1F.txt", "out/del%7F.txt", "out/" + url.PathEscape(" \"\\~")} {
		t.Run(suffix, func(t *testing.T) {
			w := f.request(context.Background(), "GET", fileURL(f, suffix), "alice")
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			p, _ := url.PathUnescape(suffix)
			root, e := os.OpenRoot(f.folder)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = root.Close() }()
			b, e := root.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(w.Body.Bytes(), b) || !reflect.DeepEqual(w.Header().Values("Content-Length"), []string{strconv.Itoa(len(b))}) || !reflect.DeepEqual(w.Header().Values("X-Content-Type-Options"), []string{"nosniff"}) {
				t.Fatalf("headers=%v body=%q", w.Header(), w.Body.Bytes())
			}
			ct := "text/plain; charset=utf-8"
			if strings.HasPrefix(suffix, "out/") {
				ct = "application/octet-stream"
				disp := "attachment; filename=\"" + strings.ReplaceAll(strings.ReplaceAll(filepath.Base(p), "\\", "\\\\"), "\"", "\\\"") + "\""
				if strings.Contains(p, "東京") || strings.ContainsAny(p, "\x1f\x7f") {
					disp = "attachment"
				}
				if !reflect.DeepEqual(w.Header().Values("Content-Disposition"), []string{disp}) {
					t.Fatalf("%v", w.Header())
				}
			} else if len(w.Header().Values("Content-Disposition")) != 0 {
				t.Fatal(w.Header())
			}
			if !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{ct}) {
				t.Fatal(w.Header())
			}
			head := f.request(context.Background(), "HEAD", fileURL(f, suffix), "alice")
			if head.Code != w.Code || !reflect.DeepEqual(head.Header(), w.Header()) || head.Body.Len() != 0 {
				t.Fatal("HEAD", head.Code, head.Header(), head.Body)
			}
		})
	}
	if f.banners != 0 {
		t.Fatal("banner for served files")
	}
}
func TestFilesMissingAndIsolation(t *testing.T) {
	// R-XZHJ-WUF5 R-O032-4QHT R-02O4-D7SA
	f := fileSetup(t)
	outside := filepath.Join(t.TempDir(), "outside")
	fileWrite(t, outside, []byte("outside"))
	if e := os.Symlink(outside, filepath.Join(f.folder, "out", "link")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(filepath.Join(f.folder, "out", "charts"), filepath.Join(f.folder, "out", "graphs")); e != nil {
		t.Fatal(e)
	}
	missing := f.request(context.Background(), "GET", fileURL(f, "out/missing"), "alice")
	for _, suffix := range []string{"out", "out/", "out/charts", "out/charts/", "out/missing.txt", "tree/main.py", "tree/", "stdout/", "out//report.csv", "Stdout", "out/charts/../report.csv", "out/../tree/main.py", "out/./report.csv", "out/%2e%2e/input.json", "out/charts%2Fsales.svg", "out/charts%252Fsales.svg", "out/sales%00.svg", "./stdout", "out/link", "out/graphs/sales.svg"} {
		w := f.request(context.Background(), "GET", fileURL(f, suffix), "alice")
		if w.Code != 404 || w.Body.String() != missing.Body.String() || !reflect.DeepEqual(w.Header(), http.Header{"Content-Type": {"text/html; charset=utf-8"}}) {
			t.Fatalf("%s: %d %v", suffix, w.Code, w.Header())
		}
		head := f.request(context.Background(), "HEAD", fileURL(f, suffix), "alice")
		if head.Code != 404 || !reflect.DeepEqual(head.Header(), w.Header()) || head.Body.Len() != 0 {
			t.Fatal("head missing")
		}
	}
	for _, p := range []string{fileURL(f, "stdout"), "/missing/runs/" + f.run.ID + "/stdout", "/report/runs/run_2222222222222222/stdout"} {
		w := f.request(context.Background(), "GET", p, "bob")
		if w.Code != 404 || w.Body.String() != missing.Body.String() {
			t.Fatal(p, w.Code)
		}
	}
	root, e := os.OpenRoot(filepath.Dir(outside))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = root.Close() }()
	b, e := root.ReadFile(filepath.Base(outside))
	if e != nil || string(b) != "outside" {
		t.Fatal("link target changed")
	}
	if e := os.RemoveAll(f.folder); e != nil {
		t.Fatal(e)
	}
	if w := f.request(context.Background(), "GET", fileURL(f, "stdout"), "alice"); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestFilesMethodCatalogAndConcurrency(t *testing.T) {
	// R-0UFV-YDCK R-0WVO-PWTY R-0ZBH-HGBC R-03W0-QZIZ
	f := fileSetup(t)
	before, e := os.ReadFile(filepath.Join(f.folder, "stdout"))
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			w := f.request(context.Background(), "GET", fileURL(f, "stdout"), "alice")
			if w.Code != 200 || w.Header().Get("Set-Cookie") != "" {
				t.Errorf("concurrent status %d", w.Code)
			}
		})
	}
	wg.Wait()
	after, e := os.ReadFile(filepath.Join(f.folder, "stdout"))
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("file changed")
	}
	rr, e := f.st.RunByID(context.Background(), f.run.ID)
	if e != nil || !reflect.DeepEqual(rr, f.run) {
		t.Fatal("catalog changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, suffix := range []string{"input.json", "out/missing.txt", "tree/main.py"} {
		for _, m := range []string{"GET", "HEAD"} {
			w := f.request(ctx, m, fileURL(f, suffix), "alice")
			want := store.Unreachable + "\n"
			if m == "HEAD" {
				want = ""
			}
			if w.Code != 503 || w.Body.String() != want || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatal(w.Code, w.Body)
			}
		}
	}
	f.db.SetFailing(true)
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		for _, user := range []string{"alice", "bob"} {
			for _, suffix := range []string{"input.json", "out/missing.txt", "out/../input.json"} {
				w := f.request(context.Background(), m, fileURL(f, suffix), user)
				if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0 {
					t.Fatal(w.Code, w.Header())
				}
			}
		}
	}
	for _, m := range []string{"GET", "HEAD"} {
		for _, suffix := range []string{"tree/main.py", "out/../input.json", "Stdout", "input.json"} {
			w := f.request(context.Background(), m, fileURL(f, suffix), "alice")
			if w.Code != 503 {
				t.Fatal(suffix, w.Code)
			}
		}
	}
	if f.banners != 0 {
		t.Fatal("banner used for non404")
	}
}
func TestFilesRunningGrowth(t *testing.T) {
	// R-Y4D5-FXDX
	f := fileSetup(t)
	sc, e := f.st.Find(context.Background(), "alice", "report")
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.st.AddRun(context.Background(), store.Run{ID: "run_3333333333333333", Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: "alice", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: time.Unix(100, 0)})
	if e != nil {
		t.Fatal(e)
	}
	f.run = r
	f.folder = f.core.Folder(r)
	for _, suffix := range []string{"stdout", "out/live.txt"} {
		for _, text := range []string{"first", "first second"} {
			fileWrite(t, filepath.Join(f.folder, suffix), []byte(text))
			w := f.request(context.Background(), "GET", fileURL(f, suffix), "alice")
			if w.Code != 200 || w.Body.String() != text {
				t.Fatal(w.Code, w.Body)
			}
			want := http.Header{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Length": {strconv.Itoa(len(text))}, "X-Content-Type-Options": {"nosniff"}}
			if strings.HasPrefix(suffix, "out/") {
				want.Set("Content-Type", "application/octet-stream")
				want.Set("Content-Disposition", "attachment; filename=\"live.txt\"")
			}
			for name, values := range want {
				if !reflect.DeepEqual(w.Header().Values(name), values) {
					t.Fatalf("running %s %q header %s: %v", suffix, text, name, w.Header().Values(name))
				}
			}
			if suffix == "stdout" && len(w.Header().Values("Content-Disposition")) != 0 {
				t.Fatal("running stdout disposition", w.Header())
			}
		}
	}
}

func fileRandom() *bytes.Reader {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i/8 + 1)
	}
	return bytes.NewReader(b)
}

type fileSnapshotEntry struct {
	Mode  fs.FileMode
	Size  int64
	Bytes []byte
	Link  string
}
type fileSnapshot struct {
	Scripts []store.Script
	Runs    []store.Run
	Entries map[string]fileSnapshotEntry
	Targets map[string]fileSnapshotEntry
}

func takeFileSnapshot(t *testing.T, f *fileFixture) fileSnapshot {
	t.Helper()
	s := fileSnapshot{Scripts: []store.Script{}, Runs: []store.Run{}, Entries: map[string]fileSnapshotEntry{}, Targets: map[string]fileSnapshotEntry{}}
	for _, owner := range []string{"alice", "bob"} {
		ss, e := f.st.List(context.Background(), owner)
		if e != nil {
			t.Fatal(e)
		}
		s.Scripts = append(s.Scripts, ss...)
		for _, sc := range ss {
			rr, e := f.st.Runs(context.Background(), sc.ID)
			if e != nil {
				t.Fatal(e)
			}
			s.Runs = append(s.Runs, rr...)
		}
	}
	root, e := os.OpenRoot(f.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = root.Close() }()
	e = fs.WalkDir(root.FS(), ".", func(p string, _ fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		st, e := root.Lstat(p)
		if e != nil {
			return e
		}
		v := fileSnapshotEntry{Mode: st.Mode(), Size: st.Size()}
		if st.Mode().IsRegular() {
			v.Bytes, e = root.ReadFile(p)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			v.Link, e = root.Readlink(p)
			if e != nil {
				return e
			}
			target := v.Link
			if !filepath.IsAbs(target) {
				target = filepath.Join(f.dir, filepath.Dir(p), target)
			}
			targetRoot, err := os.OpenRoot(filepath.Dir(target))
			if err != nil {
				return err
			}
			targetInfo, err := targetRoot.Stat(filepath.Base(target))
			if err != nil {
				_ = targetRoot.Close()
				return err
			}
			tv := fileSnapshotEntry{Mode: targetInfo.Mode(), Size: targetInfo.Size()}
			if targetInfo.Mode().IsRegular() {
				tv.Bytes, err = targetRoot.ReadFile(filepath.Base(target))
			}
			_ = targetRoot.Close()
			if err != nil {
				return err
			}
			s.Targets[target] = tv
		}
		if e != nil {
			return e
		}
		s.Entries[p] = v
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func assertFileUnchanged(t *testing.T, f *fileFixture, before fileSnapshot) {
	t.Helper()
	after := takeFileSnapshot(t, f)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("file request changed state\nbefore %#v\nafter %#v", before, after)
	}
}
func TestFilesPreserveAllStateAndOwnerAssociation(t *testing.T) {
	// R-XZHJ-WUF5 R-ZMTF-E759 R-O032-4QHT R-02O4-D7SA R-ZO1B-RYVY R-0UFV-YDCK
	f := fileSetup(t)
	ctx := context.Background()
	other, e := f.st.Create(ctx, store.Draft{Owner: "alice", Name: "other", Repo: "rep_1111111111111111", Ref: "main"})
	if e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(t.TempDir(), "target")
	fileWrite(t, target, []byte("outside"))
	if e = os.Symlink(target, filepath.Join(f.folder, "out", "outside-link")); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink("charts/sales.svg", filepath.Join(f.folder, "out", "inside-link")); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(target, filepath.Join(f.folder, "input-link")); e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"GET", "HEAD"} {
		for _, tt := range []struct {
			path, user string
			status     int
		}{{fileURL(f, "input.json"), "alice", 200}, {fileURL(f, "out/charts/sales.svg"), "alice", 200}, {fileURL(f, "out/outside-link"), "alice", 404}, {fileURL(f, "out/inside-link"), "alice", 404}, {fileURL(f, "tree/main.py"), "alice", 404}, {fileURL(f, "stdout"), "bob", 404}, {"/other/runs/" + f.run.ID + "/stdout", "alice", 404}, {"/missing/runs/" + f.run.ID + "/stdout", "alice", 404}, {"/report/runs/run_2222222222222222/stdout", "alice", 404}} {
			before := takeFileSnapshot(t, f)
			w := f.request(ctx, method, tt.path, tt.user)
			if w.Code != tt.status {
				t.Fatal(tt.path, w.Code)
			}
			assertFileUnchanged(t, f, before)
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		for _, tt := range []struct{ path, user string }{{fileURL(f, "stdout"), "alice"}, {fileURL(f, "tree/main.py"), "alice"}, {fileURL(f, "stdout"), "bob"}} {
			before := takeFileSnapshot(t, f)
			if w := f.request(ctx, method, tt.path, tt.user); w.Code != 405 {
				t.Fatal(w.Code)
			}
			assertFileUnchanged(t, f, before)
		}
	}
	if _, e = os.Lstat(filepath.Join(f.dir, other.ID)); !os.IsNotExist(e) {
		t.Fatal("never-run script folder created", e)
	}
	// Record-file links, an out-directory link, and an absent stream all name nothing.
	root, e := os.OpenRoot(f.folder)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = root.Close() }()
	if e = root.Remove("stdout"); e != nil {
		t.Fatal(e)
	}
	if e = root.Symlink("out/charts/sales.svg", "stdout"); e != nil {
		t.Fatal(e)
	}
	if e = root.Remove("stderr"); e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"stdout", "stderr"} {
		before := takeFileSnapshot(t, f)
		if w := f.request(ctx, "GET", fileURL(f, suffix), "alice"); w.Code != 404 {
			t.Fatal(w.Code)
		}
		assertFileUnchanged(t, f, before)
	}
	if e = root.Rename("out", "kept-out"); e != nil {
		t.Fatal(e)
	}
	if e = root.Symlink("kept-out", "out"); e != nil {
		t.Fatal(e)
	}
	before := takeFileSnapshot(t, f)
	if w := f.request(ctx, "GET", fileURL(f, "out/charts/sales.svg"), "alice"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	assertFileUnchanged(t, f, before)
}
func TestFilesCatalogRefusalsUnknownRun(t *testing.T) {
	// R-0WVO-PWTY R-0ZBH-HGBC
	for _, failing := range []bool{false, true} {
		t.Run(strconv.FormatBool(failing), func(t *testing.T) {
			f := fileSetup(t)
			ctx := context.Background()
			if failing {
				f.db.SetFailing(true)
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			for _, method := range []string{"GET", "HEAD"} {
				for _, path := range []string{fileURL(f, "input.json"), fileURL(f, "out/missing.txt"), "/report/runs/run_2222222222222222/input.json"} {
					if w := f.request(ctx, method, path, "alice"); w.Code != 503 {
						t.Fatal(path, w.Code)
					}
				}
			}
		})
	}
}

func TestQueuedFiles(t *testing.T) {
	// R-SZZC-R09X
	f := fileSetup(t)
	sc, e := f.st.Find(context.Background(), "alice", "report")
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.st.AddRun(context.Background(), store.Run{ID: "run_4444444444444444", Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: "alice", Trigger: store.TriggerManual, Status: store.StatusQueued, Started: time.Unix(100, 0)})
	if e != nil {
		t.Fatal(e)
	}
	f.run = r
	f.folder = f.core.Folder(r)
	fileWrite(t, filepath.Join(f.folder, runs.InputFile), []byte(`{"value":1}`))
	for _, method := range []string{"GET", "HEAD"} {
		for _, stream := range []string{runs.StdoutFile, runs.StderrFile} {
			w := f.request(context.Background(), method, fileURL(f, stream), "alice")
			if w.Code != 404 || (method == "HEAD" && w.Body.Len() != 0) {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	w := f.request(context.Background(), "GET", fileURL(f, runs.InputFile), "alice")
	if w.Code != 200 || w.Body.String() != `{"value":1}` {
		t.Fatal(w.Code, w.Body.String())
	}
}
