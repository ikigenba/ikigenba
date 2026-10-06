package serving_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/serving"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
	"github.com/ikigenba/ikigenba/sites/internal/visitor"
)

const repository = "rep_0123456789abcdef"
const visitorID = "vis_1a2b3c4d5e6f7081"

var instant = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

type fixture struct {
	db                                   *db.DB
	t                                    *testing.T
	base, binary, work, repos, root, sha string
	env                                  []string
	g                                    *git.Git
	l                                    *limits.Limits
	c                                    *cache.Cache
	s                                    *store.Store
	site                                 store.Site
	cfg                                  serving.Config
	capture                              *telemetry.Capture
	operations                           atomic.Int64
	random                               *bytes.Reader
	after                                func(time.Duration) <-chan time.Time
	joined                               func(string, string)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func newFixture(t *testing.T, files map[string]string) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	binary, err := exec.LookPath("git")
	must(t, err)
	f := &fixture{t: t, base: t.TempDir(), binary: binary}
	f.work = filepath.Join(f.base, "work")
	f.repos = filepath.Join(f.base, "repos")
	f.root = filepath.Join(f.base, "cache")
	f.env = []string{"HOME=" + f.base, "XDG_CONFIG_HOME=" + f.base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	f.g, err = git.Find(filepath.Dir(binary), func() []string { return f.env })
	must(t, err)
	f.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	f.l = limits.New(settings.Defaults(), limits.Clock{After: func(d time.Duration) <-chan time.Time { f.operations.Add(1); return f.after(d) }})
	f.open()
	seed := make([]byte, 512)
	for i := range seed {
		seed[i] = byte(i)
	}
	f.db, err = db.Open(context.Background(), db.Config{Path: filepath.Join(f.base, "sites.db"), Migrations: sites.Migrations(), Now: func() time.Time { return instant }})
	must(t, err)
	t.Cleanup(func() { must(t, f.db.Close()) })
	f.s = store.New(f.db, store.Config{Now: func() time.Time { return instant }, Rand: bytes.NewReader(seed)})
	f.site, err = f.s.Create(context.Background(), store.Draft{Owner: "u_fixture", Name: "blog", Repo: repository, Ref: "main", Visibility: store.Public, Listed: true})
	must(t, err)
	if files != nil {
		f.run("init", "--initial-branch=main", f.work)
		for name, body := range files {
			f.write(filepath.Join(f.work, name), body)
		}
		f.run("-C", f.work, "add", ".")
		f.run("-C", f.work, "commit", "-m", "fixture")
		f.sha = f.run("-C", f.work, "rev-parse", "HEAD")
		f.run("clone", "--bare", f.work, f.c.RepoDir(repository))
		for key, value := range map[string]string{"id": repository, "name": "fixture", "owner": "u_fixture", "created": "2000-01-01T00:00:00Z"} {
			f.run("config", "--file", filepath.Join(f.c.RepoDir(repository), "config"), "ikigenba."+key, value)
		}
		must(t, f.c.Unpack(context.Background(), f.site.ID, repository, f.sha))
		f.site, err = f.s.Publish(context.Background(), f.site.ID, f.sha)
		must(t, err)
	}
	ps, err := pages.Load()
	must(t, err)
	f.capture = &telemetry.Capture{}
	writer := telemetry.New(telemetry.Config{Service: "sites", Sink: f.capture, Stderr: io.Discard, Now: func() time.Time { return instant }, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 128))})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		writer.Shutdown(ctx, "SIGTERM")
	})
	f.random = bytes.NewReader(bytes.Repeat([]byte{0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f, 0x70, 0x81}, 256))
	f.cfg = serving.Config{Banner: func(page.User) page.Banner { return page.Banner{} }, Pages: ps, Store: f.s, Cache: f.c, Telemetry: writer, Rand: f.random}
	return f
}
func (f *fixture) open() {
	var err error
	f.c, err = cache.Open(cache.Config{Root: f.root, Repos: f.repos, Git: f.g, Limits: f.l, Joined: func(s, h string) {
		if f.joined != nil {
			f.joined(s, h)
		}
	}})
	must(f.t, err)
}
func (f *fixture) run(args ...string) string {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.binary, args...)
	cmd.Env = f.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}
func (f *fixture) write(p, body string) {
	f.t.Helper()
	must(f.t, os.MkdirAll(filepath.Dir(p), 0700))
	must(f.t, os.WriteFile(p, []byte(body), 0600))
}
func (f *fixture) req(method, target, user string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "sites.example.test"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Request-Id", "request-fixture")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	return r
}
func (f *fixture) handler(apex bool) http.Handler {
	if apex {
		return identity.Optional(serving.Apex(f.cfg))
	}
	return identity.Optional(serving.Sites(f.cfg))
}
func (f *fixture) answer(r *http.Request, apex bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.handler(apex).ServeHTTP(w, r)
	return w
}
func (f *fixture) events() []telemetry.Event {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	must(f.t, f.cfg.Telemetry.Flush(ctx))
	return f.capture.Events()
}
func assertHeader(t *testing.T, w *httptest.ResponseRecorder, key, value string) {
	t.Helper()
	got := w.Header().Values(key)
	if value == "" {
		if len(got) != 0 {
			t.Fatalf("%s=%v", key, got)
		}
		return
	}
	if !reflect.DeepEqual(got, []string{value}) {
		t.Fatalf("%s=%v want %q", key, got, value)
	}
}
func assertStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d want %d body %s", w.Code, status, w.Body.String())
	}
	assertHeader(t, w, "Last-Modified", "")
}
func (f *fixture) notice(r *http.Request, status int, name string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.cfg.Pages.Write(w, r, status, name, pages.NoticeData{Banner: f.cfg.Banner(page.User{})})
	return w
}
func assertNotice(t *testing.T, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != want.Code || got.Body.String() != want.Body.String() || got.Header().Get("Content-Type") != want.Header().Get("Content-Type") {
		t.Fatalf("page differs: %d %s", got.Code, got.Body.String())
	}
}

// R-EDHD-NT2A R-EEPA-1KSZ R-EFX6-FCJO R-EH52-T4AD R-EICZ-6W12 R-EJKV-KNRR R-EPOD-HIH8
func TestDeclaredServingSeams(t *testing.T) {
	f := newFixture(t, nil)
	// Unkeyed construction makes the exact exported Config shape a consumer contract.
	cfg := serving.Config{f.cfg.Banner, f.cfg.Pages, "", f.s, f.c, f.cfg.Telemetry, f.random}
	var sites, apex func(serving.Config) http.Handler
	sites, apex = serving.Sites, serving.Apex
	for _, h := range []http.Handler{sites(cfg), apex(cfg)} {
		r := f.req("GET", "/missing/", "")
		w := httptest.NewRecorder()
		identity.Optional(h).ServeHTTP(w, r)
		assertNotice(t, w, f.notice(r, 404, "notfound"))
	}
	visibility := store.Private
	_, _, err := f.s.Update(context.Background(), f.site.ID, store.Change{Visibility: &visibility})
	must(t, err)
	for _, user := range []string{"", "u_other"} {
		r := f.req("GET", "/blog/", user)
		w := f.answer(r, false)
		if user == "" {
			assertStatus(t, w, 302)
			assertHeader(t, w, "Location", urls.SignIn(r))
		} else {
			assertNotice(t, w, f.notice(r, 404, "notfound"))
			assertHeader(t, w, "Cache-Control", "private, no-cache")
		}
	}
}

// R-X4HQ-635N R-ETC2-MTPB R-Y2FP-NURO R-Y3NM-1MID R-4YFR-E1U4
func TestSiteAnswerOrder(t *testing.T) {
	f := newFixture(t, nil)
	for _, method := range []string{"POST", "DELETE", "OPTIONS", "PUT"} {
		for _, target := range []string{"/blog", "/blog/", "/nosuch"} {
			w := f.answer(f.req(method, target, ""), false)
			assertStatus(t, w, 405)
			assertHeader(t, w, "Allow", "GET, HEAD")
			if w.Body.Len() != 0 {
				t.Fatal("method body")
			}
		}
	}
	for _, target := range []string{"/nosuch", "/nosuch/", "/Blog", "/Blog/x"} {
		r := f.req("GET", target, "")
		w := f.answer(r, false)
		assertNotice(t, w, f.notice(r, 404, "notfound"))
		assertHeader(t, w, "Cache-Control", "no-cache")
		assertHeader(t, w, "Location", "")
		assertHeader(t, w, "ETag", "")
	}
	before := f.operations.Load()
	for _, target := range []string{"/blog", "/blog?ref=card"} {
		r := f.req("GET", target, "")
		w := f.answer(r, false)
		assertStatus(t, w, 301)
		want := "/blog/"
		if r.URL.RawQuery != "" {
			want += "?" + r.URL.RawQuery
		}
		assertHeader(t, w, "Location", want)
		assertHeader(t, w, "ETag", "")
	}
	r := f.req("GET", "/blog/", "")
	w := f.answer(r, false)
	assertNotice(t, w, f.notice(r, 404, "notfound"))
	assertHeader(t, w, "ETag", "")
	if f.operations.Load() != before {
		t.Fatal("git before tree")
	}
	entries, err := os.ReadDir(f.root)
	must(t, err)
	if len(entries) != 0 {
		t.Fatal("early answer made cache")
	}
	_, err = f.s.Delete(context.Background(), f.site.ID)
	must(t, err)
	r = f.req("GET", "/blog", "")
	assertNotice(t, f.answer(r, false), f.notice(r, 404, "notfound"))
	f.db.SetFailing(true)
	for _, method := range []string{"POST", "DELETE", "OPTIONS", "PUT", "PATCH", "CUSTOM"} {
		for _, target := range []string{"/blog", "/blog/", "/nosuch", "/"} {
			for _, user := range []string{"", "u_fixture"} {
				w := f.answer(f.req(method, target, user), false)
				assertStatus(t, w, 405)
				assertHeader(t, w, "Allow", "GET, HEAD")
				if w.Body.Len() != 0 {
					t.Fatal("failing catalog method body")
				}
			}
		}
	}
}

// R-W6XT-ITGV
func TestPrivateGuestRevealsNoTree(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "secret", "style.css": "private"})
	v := store.Private
	_, _, err := f.s.Update(context.Background(), f.site.ID, store.Change{Visibility: &v})
	must(t, err)
	before := f.operations.Load()
	for _, method := range []string{"GET", "HEAD"} {
		for _, target := range []string{"/blog", "/blog/", "/blog/style.css", "/blog/missing?x=1"} {
			r := f.req(method, target, "")
			r.Header.Set("If-None-Match", "*")
			r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
			first := f.answer(r, false)
			assertStatus(t, first, 302)
			assertHeader(t, first, "Location", urls.SignIn(r))
			assertHeader(t, first, "ETag", "")
			assertHeader(t, first, "Set-Cookie", "")
			must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha)))
			second := f.answer(r, false)
			if first.Body.String() != second.Body.String() {
				t.Fatal("tree leaked")
			}
		}
	}
	if f.operations.Load() != before {
		t.Fatal("guest started git")
	}
	if _, err = os.Stat(f.c.Dir(f.site.ID, f.sha)); !os.IsNotExist(err) {
		t.Fatalf("guest rebuilt tree %v", err)
	}
	if len(f.events()) != 0 {
		t.Fatal("guest recorded event")
	}
}

// R-EKSR-YFIG R-EY7O-5WO3 R-EZFK-JOES R-F0NG-XG5H R-EOGH-3QQJ R-F1VD-B7W6 R-F339-OZMV R-F4B6-2RDK
func TestTreePathsAndConditionalFiles(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "root\r\nno-final-newline", "404.html": "own missing", "style.css": "private bytes", "about/index.html": "about", "space dir/index.html": "spaced", ".env": "secret", ".gitignore": "secret", ".hidden/config": "secret"})
	root := f.c.Dir(f.site.ID, f.sha)
	must(t, os.Symlink("style.css", filepath.Join(root, "link.html")))
	must(t, os.Symlink("about", filepath.Join(root, "linked")))
	must(t, os.Symlink(f.base, filepath.Join(root, "outside")))
	must(t, os.Mkdir(filepath.Join(root, "drafts"), 0700))
	other, err := f.s.Create(context.Background(), store.Draft{Owner: "u_other", Name: "recipes", Repo: repository, Ref: "main", Visibility: store.Public, Listed: true})
	must(t, err)
	must(t, f.c.Unpack(context.Background(), other.ID, repository, f.sha))
	_, err = f.s.Publish(context.Background(), other.ID, f.sha)
	must(t, err)
	f.write(filepath.Join(f.c.Dir(other.ID, f.sha), "index.html"), "another site")
	r := f.req("GET", "/blog/", "")
	w := f.answer(r, false)
	assertStatus(t, w, 200)
	if w.Body.String() != "root\r\nno-final-newline" {
		t.Fatal("file changed")
	}
	assertHeader(t, w, "Content-Type", "text/html; charset=utf-8")
	assertHeader(t, w, "Content-Length", strconv.Itoa(len("root\r\nno-final-newline")))
	tag := "\"" + f.sha + "\""
	assertHeader(t, w, "ETag", tag)
	for _, headers := range [][]string{{tag}, {"W/" + tag}, {" \tW/" + tag + "\t "}, {"\"other\", ", " , " + tag}, {"\"other\", *"}, {",,", "*"}} {
		for _, method := range []string{"GET", "HEAD"} {
			r = f.req(method, "/blog/style.css", "")
			for _, h := range headers {
				r.Header.Add("If-None-Match", h)
			}
			w = f.answer(r, false)
			assertStatus(t, w, 304)
			assertHeader(t, w, "ETag", tag)
			assertHeader(t, w, "Content-Type", "")
			assertHeader(t, w, "Content-Length", "")
			if w.Body.Len() != 0 {
				t.Fatal("304 body")
			}
		}
	}
	for _, h := range []string{"", ", ,", "w/" + tag, "W/W/" + tag, "\"else\""} {
		r = f.req("GET", "/blog/style.css", "")
		r.Header.Set("If-None-Match", h)
		w = f.answer(r, false)
		assertStatus(t, w, 200)
	}
	for _, target := range []string{"/blog/about?x=1", "/blog/space%20dir?x=%2f"} {
		r = f.req("GET", target, "")
		r.Header.Set("If-None-Match", "*")
		w = f.answer(r, false)
		assertStatus(t, w, 301)
		assertHeader(t, w, "Location", r.URL.EscapedPath()+"/?"+r.URL.RawQuery)
		assertHeader(t, w, "ETag", "")
	}
	for _, target := range []string{"/blog/.env", "/blog/.hidden/config", "/blog/.git/config", "/blog/about/../style.css", "/blog/%2e%2e/recipes/", "/blog/./style.css", "/blog/link.html", "/blog/linked/index.html", "/blog/outside/sites.db", "/blog/drafts/", "/blog//style.css", "/blog/about//", "/blog/style.css/extra", "/blog/missing"} {
		r = f.req("GET", target, "")
		r.Header.Set("If-None-Match", "*")
		w = f.answer(r, false)
		assertStatus(t, w, 404)
		if w.Body.String() != "own missing" {
			t.Fatalf("%s read wrong bytes %q", target, w.Body.String())
		}
		assertHeader(t, w, "Content-Type", "text/html; charset=utf-8")
		assertHeader(t, w, "Content-Length", strconv.Itoa(len("own missing")))
		assertHeader(t, w, "ETag", "")
	}
	for _, kind := range []string{"absent", "symlink", "directory"} {
		must(t, os.RemoveAll(filepath.Join(root, "404.html")))
		switch kind {
		case "symlink":
			must(t, os.Symlink("index.html", filepath.Join(root, "404.html")))
		case "directory":
			must(t, os.Mkdir(filepath.Join(root, "404.html"), 0700))
		}
		r = f.req("GET", "/blog/missing", "")
		w = f.answer(r, false)
		assertNotice(t, w, f.notice(r, 404, "notfound"))
		assertHeader(t, w, "ETag", "")
	}
}

// R-EM0O-C795
func TestFixedContentTypes(t *testing.T) {
	table := map[string]string{"html": "text/html; charset=utf-8", "htm": "text/html; charset=utf-8", "css": "text/css; charset=utf-8", "js": "text/javascript; charset=utf-8", "mjs": "text/javascript; charset=utf-8", "txt": "text/plain; charset=utf-8", "md": "text/markdown; charset=utf-8", "csv": "text/csv; charset=utf-8", "json": "application/json", "map": "application/json", "webmanifest": "application/manifest+json", "xml": "application/xml", "rss": "application/rss+xml", "atom": "application/atom+xml", "svg": "image/svg+xml", "png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp", "avif": "image/avif", "ico": "image/x-icon", "woff": "font/woff", "woff2": "font/woff2", "ttf": "font/ttf", "otf": "font/otf", "pdf": "application/pdf", "wasm": "application/wasm", "mp4": "video/mp4", "webm": "video/webm", "mp3": "audio/mpeg", "ogg": "audio/ogg", "wav": "audio/wav", "zip": "application/zip", "weird": "application/octet-stream", "": "application/octet-stream"}
	files := map[string]string{}
	for ext := range table {
		name := "asset"
		if ext != "" {
			name += "." + strings.ToUpper(ext)
		}
		files[name] = "unchanged"
	}
	f := newFixture(t, files)
	for ext, want := range table {
		name := "asset"
		if ext != "" {
			name += "." + strings.ToUpper(ext)
		}
		w := f.answer(f.req("GET", "/blog/"+name, ""), false)
		assertStatus(t, w, 200)
		assertHeader(t, w, "Content-Type", want)
		if w.Body.String() != "unchanged" {
			t.Fatal("file changed")
		}
	}
}

// R-4ZNN-RTKT R-FAEN-ZM31 R-1DZ9-ITG2
func TestHeadParityAndViewCacheControl(t *testing.T) {
	for _, visibility := range []string{store.Public, store.Private} {
		f := newFixture(t, map[string]string{"index.html": "index", "404.html": "missing", "about/index.html": "about"})
		_, _, err := f.s.Update(context.Background(), f.site.ID, store.Change{Visibility: &visibility})
		must(t, err)
		for _, user := range []string{"", "u_other"} {
			for _, target := range []string{"/blog", "/blog/", "/blog/about", "/blog/missing", "/nosuch/"} {
				get := f.req("GET", target, user)
				get.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
				head := get.Clone(get.Context())
				head.Method = "HEAD"
				gw, hw := f.answer(get, false), f.answer(head, false)
				assertStatus(t, hw, gw.Code)
				if !reflect.DeepEqual(gw.Header(), hw.Header()) || hw.Body.Len() != 0 {
					t.Fatalf("HEAD differs %s %v %v", target, gw.Header(), hw.Header())
				}
				if strings.HasPrefix(target, "/blog") && (visibility != store.Private || user != "") {
					assertHeader(t, gw, "Cache-Control", visibility+", no-cache")
				}
			}
		}
		for _, apex := range []bool{false, true} {
			r := f.req("GET", "/missing", "")
			g := f.answer(r, apex)
			r.Method = "HEAD"
			h := f.answer(r, apex)
			if !reflect.DeepEqual(g.Header(), h.Header()) || h.Code != g.Code || h.Body.Len() != 0 {
				t.Fatal("notice HEAD differs")
			}
		}
	}
	f := newFixture(t, nil)
	for _, state := range []string{"unpublished", "unavailable", "stopping"} {
		if state != "unpublished" {
			var err error
			f.site, err = f.s.Publish(context.Background(), f.site.ID, strings.Repeat("a", 40))
			must(t, err)
		}
		if state == "stopping" {
			f.l.Drain()
		}
		r := f.req("GET", "/blog/", "")
		r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
		g := f.answer(r, false)
		r.Method = "HEAD"
		h := f.answer(r, false)
		assertHeader(t, g, "Cache-Control", "public, no-cache")
		if !reflect.DeepEqual(g.Header(), h.Header()) || h.Code != g.Code || h.Body.Len() != 0 {
			t.Fatalf("%s HEAD differs", state)
		}
	}
}

// R-FBMK-DDTQ R-Y4VI-FE92 R-FFA9-IP1T
func TestFreshCatalogAndCachedTreeReadOnly(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "old"})
	before, _ := f.s.BySlug(context.Background(), "blog")
	must(t, func() error { _, err := f.s.SetApex(context.Background(), f.site.ID); return err }())
	root := f.c.Dir(f.site.ID, f.sha)
	st, err := os.Stat(filepath.Join(root, "index.html"))
	must(t, err)
	beforeOps := f.operations.Load()
	first := f.answer(f.req("GET", "/blog/", ""), false)
	moved := f.c.RepoDir(repository) + ".away"
	must(t, os.Rename(f.c.RepoDir(repository), moved))
	cached := f.answer(f.req("GET", "/blog/", ""), false)
	first.Header().Del("Set-Cookie")
	cached.Header().Del("Set-Cookie")
	if first.Code != cached.Code || first.Body.String() != cached.Body.String() || !reflect.DeepEqual(first.Header(), cached.Header()) {
		t.Fatal("repository affected cached answer")
	}
	// A disposable path to the real git disappears; the cached tree remains usable.
	privateBin := filepath.Join(f.base, "bin")
	must(t, os.Mkdir(privateBin, 0700))
	must(t, os.Symlink(f.binary, filepath.Join(privateBin, "git")))
	g, err := git.Find(privateBin, func() []string { return f.env })
	must(t, err)
	c, err := cache.Open(cache.Config{Root: f.root, Repos: f.repos, Git: g, Limits: f.l})
	must(t, err)
	f.cfg.Cache = c
	must(t, os.Remove(filepath.Join(privateBin, "git")))
	cached = f.answer(f.req("GET", "/blog/", ""), false)
	assertStatus(t, cached, 200)
	if cached.Body.String() != "old" || f.operations.Load() != beforeOps {
		t.Fatal("cached tree needed git")
	}
	after, err := f.s.BySlug(context.Background(), "blog")
	must(t, err)
	a, set, err := f.s.Apex(context.Background())
	must(t, err)
	if before != after || !set || a.ID != f.site.ID {
		t.Fatal("serving changed catalog")
	}
	afterSt, err := os.Stat(filepath.Join(root, "index.html"))
	must(t, err)
	if st.ModTime() != afterSt.ModTime() || st.Size() != afterSt.Size() {
		t.Fatal("serving changed tree")
	}
	f.cfg.Cache = f.c
	must(t, os.Rename(moved, f.c.RepoDir(repository)))
	blob := f.run("--git-dir="+f.c.RepoDir(repository), "hash-object", "-w", "--stdin")
	// A second real git commit holds an empty index and is unpacked normally.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.binary, "--git-dir="+f.c.RepoDir(repository), "mktree")
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader("100644 blob " + blob + "\tindex.html\n")
	out, err := cmd.Output()
	must(t, err)
	tree := strings.TrimSpace(string(out))
	sha := f.run("--git-dir="+f.c.RepoDir(repository), "commit-tree", tree, "-m", "second")
	must(t, f.c.Unpack(context.Background(), f.site.ID, repository, sha))
	_, err = f.s.Publish(context.Background(), f.site.ID, sha)
	must(t, err)
	r := f.req("GET", "/blog/", "")
	r.Header.Set("If-None-Match", "\""+f.sha+"\"")
	w := f.answer(r, false)
	assertStatus(t, w, 200)
	assertHeader(t, w, "ETag", "\""+sha+"\"")
	if w.Body.Len() != 0 {
		t.Fatal("old bytes served")
	}
	_, err = f.s.Delete(context.Background(), f.site.ID)
	must(t, err)
	r = f.req("GET", "/blog/", "")
	assertNotice(t, f.answer(r, false), f.notice(r, 404, "notfound"))
}

// R-F6QY-UAUY R-W4I0-R9ZH R-LPUZ-IU3X R-HENR-6828 R-LON3-52D8
func TestRebuildFailuresAndDrain(t *testing.T) {
	for _, reason := range []string{cache.ReasonRepositoryMissing, cache.ReasonCommitMissing, cache.ReasonTooLarge, cache.ReasonTimedOut, cache.ReasonGitFailed, "stopping"} {
		t.Run(reason, func(t *testing.T) {
			f := newFixture(t, map[string]string{"index.html": "published bytes"})
			must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha)))
			switch reason {
			case cache.ReasonRepositoryMissing:
				must(t, os.RemoveAll(f.c.RepoDir(repository)))
			case cache.ReasonCommitMissing:
				var err error
				f.site, err = f.s.Publish(context.Background(), f.site.ID, strings.Repeat("a", 40))
				must(t, err)
			case cache.ReasonTooLarge:
				s := settings.Defaults()
				s.SiteMaxBytes = 1
				f.l = limits.New(s, limits.Clock{After: f.after})
				f.open()
				f.cfg.Cache = f.c
			case cache.ReasonTimedOut:
				f.after = func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- instant; return ch }
			case cache.ReasonGitFailed:
				must(t, os.WriteFile(filepath.Join(f.c.RepoDir(repository), "config"), []byte("[broken"), 0600))
			case "stopping":
				f.l.Drain()
			}
			for i := 0; i < 2; i++ {
				r := f.req("GET", "/blog/", "")
				r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
				r.Header.Set("X-Request-Id", fmt.Sprintf("failure-%d", i))
				r.Header.Set("Referer", "https://news.example/post?hidden=detail")
				w := f.answer(r, false)
				assertStatus(t, w, 503)
				assertHeader(t, w, "ETag", "")
				assertHeader(t, w, "Cache-Control", "public, no-cache")
				if reason == "stopping" {
					assertHeader(t, w, "Retry-After", "30")
					assertHeader(t, w, "Content-Type", "text/plain; charset=utf-8")
					if w.Body.String() != "sites is stopping; try again later\n" {
						t.Fatal("drain body")
					}
				} else {
					assertHeader(t, w, "Retry-After", "60")
					assertNotice(t, w, f.notice(r, 503, "unavailable"))
				}
			}
			events := f.events()
			want := 4
			if reason == "stopping" {
				want = 2
			}
			if len(events) != want {
				t.Fatalf("events %v", events)
			}
			for i, e := range events {
				if reason == "stopping" || i%2 == 0 {
					want := telemetry.Attrs{"site": f.site.ID, "visitor": visitorID, "path": "/blog/", "status": int64(503), "referrer_host": "news.example", "commit": f.site.Commit}
					if e.Name != "site.viewed" || !reflect.DeepEqual(e.Attrs, want) {
						t.Fatalf("view %v want %v", e, want)
					}
				} else {
					want := telemetry.Attrs{"site": f.site.ID, "commit": f.site.Commit, "reason": reason}
					if e.Name != "site.unavailable" || !reflect.DeepEqual(e.Attrs, want) {
						t.Fatalf("unavailable %v want %v", e, want)
					}
				}
			}
		})
	}
}

type recordingWriter struct {
	header   http.Header
	statuses []int
	writes   int
	cancel   context.CancelFunc
	fail     bool
}

func (w *recordingWriter) Header() http.Header    { return w.header }
func (w *recordingWriter) WriteHeader(status int) { w.statuses = append(w.statuses, status) }
func (w *recordingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.cancel != nil {
		w.cancel()
	}
	if w.fail {
		return 0, errors.New("visitor write failed")
	}
	return len(p), nil
}
func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("wait timed out")
	}
}

// R-W5PX-51Q6
func TestCanceledAndHaltedRebuildWritesNothing(t *testing.T) {
	for _, halt := range []bool{false, true} {
		t.Run(strconv.FormatBool(halt), func(t *testing.T) {
			f := newFixture(t, map[string]string{"index.html": "index"})
			must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha)))
			r := f.req("GET", "/blog/", "")
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			if halt {
				f.l.Halt()
			} else {
				entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var once sync.Once
				f.after = func(time.Duration) <-chan time.Time {
					once.Do(func() { close(entered); <-release })
					return make(chan time.Time)
				}
				f.joined = func(string, string) { close(joined) }
				direct := make(chan struct{})
				go func() { defer close(direct); _, _ = f.c.Tree(context.Background(), f.site.ID, repository, f.sha) }()
				wait(t, entered)
				done := make(chan struct{})
				writer := &recordingWriter{header: make(http.Header)}
				go func() { defer close(done); f.handler(false).ServeHTTP(writer, r) }()
				wait(t, joined)
				cancel()
				wait(t, done)
				if writer.writes != 0 || len(writer.statuses) != 0 || len(writer.header) != 0 {
					t.Fatalf("canceled answer %v", writer)
				}
				if len(f.events()) != 0 {
					t.Fatal("canceled rebuild recorded")
				}
				close(release)
				wait(t, direct)
				return
			}
			writer := &recordingWriter{header: make(http.Header)}
			f.handler(false).ServeHTTP(writer, r)
			if writer.writes != 0 || len(writer.statuses) != 0 || len(writer.header) != 0 {
				t.Fatalf("halted answer %v", writer)
			}
			if len(f.events()) != 0 {
				t.Fatal("halted event")
			}
		})
	}
}

// R-LM7A-DIVU
func TestEventsFollowCompletedWriteContext(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			t.Run(fmt.Sprintf("unavailable=%t/canceled=%t", unavailable, canceled), func(t *testing.T) {
				f := newFixture(t, map[string]string{"index.html": strings.Repeat("x", 65536)})
				if unavailable {
					must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha)))
					must(t, os.RemoveAll(f.c.RepoDir(repository)))
				}
				r := f.req("GET", "/blog/", "")
				r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				r = r.WithContext(ctx)
				w := &recordingWriter{header: make(http.Header), fail: true}
				if canceled {
					w.cancel = cancel
				}
				f.handler(false).ServeHTTP(w, r)
				if w.writes == 0 {
					t.Fatal("no write attempt")
				}
				events := f.events()
				want := 1
				if unavailable {
					want = 2
				}
				if canceled {
					want = 0
				}
				if len(events) != want {
					t.Fatalf("events %v want %d", events, want)
				}
				if want > 0 && events[0].Name != "site.viewed" {
					t.Fatal("lost view on write error")
				}
				if want == 2 && events[1].Name != "site.unavailable" {
					t.Fatal("lost unavailable on write error")
				}
			})
		}
	}
}

// R-FIXY-O09W R-FK5V-1S0L R-W85P-WL7K
func TestApexAnswersWithoutTreeOrMethodGate(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "private"})
	before := f.operations.Load()
	for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
		for _, target := range []string{"/", "/about", "/mcp", "/hello?x=1"} {
			r := f.req(method, target, "")
			w := f.answer(r, true)
			assertNotice(t, w, f.notice(r, 404, "notfound"))
			assertHeader(t, w, "Cache-Control", "no-cache")
			assertHeader(t, w, "Location", "")
			assertHeader(t, w, "ETag", "")
		}
	}
	_, err := f.s.SetApex(context.Background(), f.site.ID)
	must(t, err)
	must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha)))
	must(t, os.RemoveAll(f.c.RepoDir(repository)))
	for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
		for _, user := range []string{"", "u_other"} {
			for _, target := range []string{"/", "/about", "/mcp", "/space%20dir?x=%2f", "/hello?x=1"} {
				r := f.req(method, target, user)
				r.Host = "example.test"
				r.Header.Set("If-None-Match", "*")
				w := f.answer(r, true)
				assertStatus(t, w, 302)
				want := urls.ApexBase(r, f.cfg.ServicesPath) + "/blog/" + strings.TrimPrefix(r.URL.EscapedPath(), "/")
				if r.URL.RawQuery != "" {
					want += "?" + r.URL.RawQuery
				}
				assertHeader(t, w, "Location", want)
				assertHeader(t, w, "ETag", "")
				assertHeader(t, w, "Set-Cookie", "")
			}
		}
	}
	if f.operations.Load() != before {
		t.Fatal("apex started git")
	}
	if _, err = os.Stat(f.c.Dir(f.site.ID, f.sha)); !os.IsNotExist(err) {
		t.Fatal("apex rebuilt tree")
	}
	if len(f.events()) != 0 {
		t.Fatal("apex event")
	}
	// An unpublished apex also redirects without probing its repository.
	other, err := f.s.Create(context.Background(), store.Draft{Owner: "u_other", Name: "empty", Repo: repository, Ref: "main", Visibility: store.Public, Listed: true})
	must(t, err)
	_, err = f.s.SetApex(context.Background(), other.ID)
	must(t, err)
	r := f.req("GET", "/", "")
	assertHeader(t, f.answer(r, true), "Location", urls.ApexBase(r, "")+"/empty/")
}

// R-UEU7-2XUB R-UIHW-892E R-LON3-52D8 R-ZZDT-YTVL R-RFVG-0T9A
func TestViewCookieAndExactAttrs(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, user := range []string{"", "u_fixture"} {
			for _, scheme := range []string{"http", "https"} {
				f := newFixture(t, map[string]string{"index.html": "index", "about/index.html": "about"})
				r := f.req(method, "/blog/about/?from=newsletter", user)
				r.Header.Set("X-Forwarded-Proto", scheme)
				r.Header.Set("Referer", "https://example.org/post?x=1")
				w := f.answer(r, false)
				assertStatus(t, w, 200)
				assertHeader(t, w, "Set-Cookie", visitor.Cookie(visitorID, scheme == "https"))
				events := f.events()
				want := telemetry.Attrs{"site": f.site.ID, "visitor": visitorID, "path": "/blog/about/", "status": int64(200), "referrer_host": "example.org", "commit": f.sha}
				if len(events) != 1 || events[0].Name != "site.viewed" || !reflect.DeepEqual(events[0].Attrs, want) {
					t.Fatalf("view %v want %v", events, want)
				}
				if "\""+events[0].Attrs["commit"].(string)+"\"" != w.Header().Get("ETag") {
					t.Fatal("event commit differs")
				}
			}
		}
	}
	for _, cookie := range []string{"", visitor.CookieName + "=bad", visitor.CookieName + "=vis_1A2B3C4D5E6F7081"} {
		f := newFixture(t, nil)
		r := f.req("GET", "/blog", "")
		r.Header.Set("Cookie", cookie)
		w := f.answer(r, false)
		assertHeader(t, w, "Set-Cookie", visitor.Cookie(visitorID, true))
	}
	f := newFixture(t, map[string]string{"index.html": "index"})
	r := f.req("GET", "/blog/", "")
	r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
	n := f.random.Len()
	w := f.answer(r, false)
	assertHeader(t, w, "Set-Cookie", "")
	if f.random.Len() != n {
		t.Fatal("valid cookie read random")
	}
	// Failed randomness may omit the viewed event; any event must stay bounded.
	f.cfg.Rand = bytes.NewReader(nil)
	r = f.req("GET", "/blog/", "")
	w = f.answer(r, false)
	assertStatus(t, w, 200)
	for _, event := range f.events() {
		if event.Name != "site.viewed" {
			continue
		}
		for key, value := range event.Attrs {
			switch key {
			case "site", "path", "status", "referrer_host", "commit":
			case "visitor":
				id, ok := value.(string)
				if !ok || (id != "" && !visitor.ValidID(id)) {
					t.Fatal("invalid visitor")
				}
			default:
				t.Fatalf("unexpected attr %s", key)
			}
		}
	}
}

// R-FGI5-WGSI
func TestConcurrentServingHasNoDefaultLogs(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "index"})
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	h := f.handler(false)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := f.req("GET", "/blog/", "")
			r.Header.Set("X-Request-Id", fmt.Sprintf("concurrent-%d", i))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 || w.Body.String() != "index" {
				t.Errorf("concurrent answer %d %q", w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
	if logs.Len() != 0 {
		t.Fatalf("default logs %s", logs.String())
	}
	if len(f.events()) != 32 {
		t.Fatal("lost concurrent event")
	}
}

func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	bounded, err := os.OpenRoot(root)
	must(t, err)
	defer func() { must(t, bounded.Close()) }()
	snapshot := map[string]string{}
	must(t, filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		st, err := e.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		value := st.Mode().String() + "/" + st.ModTime().String()
		if st.Mode().IsRegular() {
			body, err := bounded.ReadFile(relative)
			if err != nil {
				return err
			}
			value += "/" + string(body)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			value += "/" + target
		}
		snapshot[relative] = value
		return nil
	}))
	return snapshot
}

// R-FFA9-IP1T R-Y4VI-FE92
func TestCachedAnswersPreserveEntireTreeAndCatalog(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "index", "404.html": "missing", "about/index.html": "about"})
	_, err := f.s.SetApex(context.Background(), f.site.ID)
	must(t, err)
	beforeTree := treeSnapshot(t, f.root)
	before, err := f.s.Visible(context.Background(), "u_fixture")
	must(t, err)
	apex, set, err := f.s.Apex(context.Background())
	must(t, err)
	targets := []string{"/blog", "/blog/", "/blog/about", "/blog/missing", "/blog/.env", "/nosuch"}
	var reference []*httptest.ResponseRecorder
	for _, target := range targets {
		r := f.req("GET", target, "")
		r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
		reference = append(reference, f.answer(r, false))
	}
	missingBin := filepath.Join(f.base, "missingbin")
	must(t, os.Mkdir(missingBin, 0700))
	bin := filepath.Join(missingBin, "git")
	must(t, os.Symlink(f.binary, bin))
	g, err := git.Find(missingBin, func() []string { return f.env })
	must(t, err)
	c, err := cache.Open(cache.Config{Root: f.root, Repos: f.repos, Git: g, Limits: f.l})
	must(t, err)
	f.cfg.Cache = c
	must(t, os.Remove(bin))
	must(t, os.RemoveAll(c.RepoDir(repository)))
	operations := f.operations.Load()
	for i, target := range targets {
		r := f.req("GET", target, "")
		r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
		got := f.answer(r, false)
		want := reference[i]
		if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), want.Header()) {
			t.Fatalf("cached answer changed %s", target)
		}
		r.Method = "HEAD"
		head := f.answer(r, false)
		if head.Code != want.Code || head.Body.Len() != 0 || !reflect.DeepEqual(head.Header(), want.Header()) {
			t.Fatalf("cached HEAD changed %s", target)
		}
	}
	r := f.req("GET", "/blog/", "")
	r.Header.Set("If-None-Match", "*")
	assertStatus(t, f.answer(r, false), 304)
	for _, method := range []string{"POST", "GET", "HEAD"} {
		f.answer(f.req(method, "/hello", ""), true)
	}
	after, err := f.s.Visible(context.Background(), "u_fixture")
	must(t, err)
	afterApex, afterSet, err := f.s.Apex(context.Background())
	must(t, err)
	if !reflect.DeepEqual(before, after) || apex != afterApex || set != afterSet {
		t.Fatal("serving mutated catalog")
	}
	if !reflect.DeepEqual(beforeTree, treeSnapshot(t, f.root)) {
		t.Fatal("serving mutated cached trees")
	}
	if operations != f.operations.Load() {
		t.Fatal("cached answers started git")
	}
}

type publishWriter struct {
	*httptest.ResponseRecorder
	publish func()
}

func (w *publishWriter) Write(p []byte) (int, error) {
	if w.publish != nil {
		publish := w.publish
		w.publish = nil
		publish()
	}
	return w.ResponseRecorder.Write(p)
}

// R-ZZDT-YTVL R-LON3-52D8
func TestViewCommitRemainsTheAnsweredCommit(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "old bytes"})
	r := f.req("GET", "/blog/?private-query=hidden", "")
	r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
	newer := strings.Repeat("b", 40)
	writer := &publishWriter{ResponseRecorder: httptest.NewRecorder(), publish: func() { _, err := f.s.Publish(context.Background(), f.site.ID, newer); must(t, err) }}
	f.handler(false).ServeHTTP(writer, r)
	assertStatus(t, writer.ResponseRecorder, 200)
	if writer.Body.String() != "old bytes" {
		t.Fatal("changed body")
	}
	assertHeader(t, writer.ResponseRecorder, "ETag", "\""+f.sha+"\"")
	events := f.events()
	want := telemetry.Attrs{"site": f.site.ID, "visitor": visitorID, "path": "/blog/", "status": int64(200), "referrer_host": "", "commit": f.sha}
	if len(events) != 1 || !reflect.DeepEqual(events[0].Attrs, want) {
		t.Fatalf("wrong response snapshot event %v", events)
	}
}

// R-1DZ9-ITG2 R-LON3-52D8
func TestAllViewStatusesHaveExactAttributes(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "index", "404.html": "missing"})
	for i, tc := range []struct {
		path        string
		method      string
		conditional bool
		status      int
	}{{"/blog", "GET", false, 301}, {"/blog/", "GET", false, 200}, {"/blog/", "GET", true, 304}, {"/blog/missing", "GET", false, 404}, {"/blog/missing", "HEAD", false, 404}} {
		r := f.req(tc.method, tc.path, "")
		r.Header.Set("X-Request-Id", fmt.Sprintf("view-status-%d", i))
		r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
		if tc.conditional {
			r.Header.Set("If-None-Match", "*")
		}
		assertStatus(t, f.answer(r, false), tc.status)
		events := f.events()
		want := telemetry.Attrs{"site": f.site.ID, "visitor": visitorID, "path": tc.path, "status": int64(tc.status), "referrer_host": "", "commit": f.sha}
		if len(events) != i+1 || events[i].Name != "site.viewed" || !reflect.DeepEqual(events[i].Attrs, want) {
			t.Fatalf("view attrs %v", events)
		}
	}
}

// R-LON3-52D8
func TestUnpublishedViewsHaveExactAttributes(t *testing.T) {
	f := newFixture(t, nil)
	for i, method := range []string{"GET", "HEAD"} {
		r := f.req(method, "/blog/unfinished?hidden=query", "")
		r.Header.Set("X-Request-Id", fmt.Sprintf("unpublished-%d", i))
		r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
		r.Header.Set("Referer", "https://news.example:8443/post?hidden=detail")
		assertStatus(t, f.answer(r, false), 404)
		events := f.events()
		want := telemetry.Attrs{"site": f.site.ID, "visitor": visitorID, "path": "/blog/unfinished", "status": int64(404), "referrer_host": "news.example:8443", "commit": ""}
		if len(events) != i+1 || events[i].Name != "site.viewed" || !reflect.DeepEqual(events[i].Attrs, want) {
			t.Fatalf("unpublished view %v want %v", events, want)
		}
	}
}
