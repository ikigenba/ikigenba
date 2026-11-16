package serving_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/serving"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/visitor"
)

func (f *fixture) domain(name string) {
	f.t.Helper()
	var err error
	f.site, _, err = f.s.Update(context.Background(), f.site.ID, store.Change{Domain: &name})
	must(f.t, err)
}
func (f *fixture) domainAnswer(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	identity.Optional(serving.Domains(f.cfg)).ServeHTTP(w, r)
	return w
}

// R-EDQI-81KF R-EEYE-LTB4 R-E86N-M3WC R-E9EJ-ZVN1 R-EOPL-NZ8O R-EPXI-1QZD
func TestCanonicalLinks(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "index", "a b.html": "spaced", "a.html": "escaped", "style.css": "style", "about/index.html": "about"})
	f.domain("example.com")
	for _, tc := range []struct {
		target, link string
		conditional  bool
		status       int
	}{
		{"/blog/a%20b.html?x=1", "/a%20b.html", false, 200},
		{"/blog/", "/", false, 200},
		{"/blog/%61.html", "/a.html", false, 200},
		{"/blog/style.css", "/style.css", true, 304},
		{"/blog/about", "", false, 301},
		{"/blog/missing", "", false, 404},
		{"/missing/", "", false, 404},
	} {
		for _, method := range []string{"GET", "HEAD"} {
			r := f.req(method, tc.target, "")
			r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
			if tc.conditional {
				r.Header.Set("If-None-Match", "*")
			}
			w := f.answer(r, false)
			assertStatus(t, w, tc.status)
			want := ""
			if tc.link != "" {
				want = "<https://example.com" + tc.link + ">; rel=\"canonical\""
			}
			assertHeader(t, w, "Link", want)
			if method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
		}
	}
	f.domain("")
	assertHeader(t, f.answer(f.req("GET", "/blog/", ""), false), "Link", "")
	f.domain("example.com")
	must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha, "")))
	must(t, os.RemoveAll(f.c.RepoDir(repository)))
	assertHeader(t, f.answer(f.req("GET", "/blog/", ""), false), "Link", "")
	for _, set := range []bool{false, true} {
		if set {
			_, err := f.s.SetApex(context.Background(), f.site.ID)
			must(t, err)
		}
		assertHeader(t, f.answer(f.req("GET", "/", ""), true), "Link", "")
	}
}

// R-EAMG-DNDQ R-E9EJ-ZVN1 R-EOPL-NZ8O R-EPXI-1QZD R-AG16-877K
func TestDomainAnswersMatchSlugTree(t *testing.T) {
	for _, state := range []string{"published", "unpublished", "unavailable", "stopping", "halted"} {
		t.Run(state, func(t *testing.T) {
			files := map[string]string{"index.html": "index", "404.html": "custom missing", "about/index.html": "about", "a b.html": "spaced", "mcp": "site mcp", "tools": "site tools", "_appkit/x": "site static", "other/index.html": "site other"}
			if state == "unpublished" {
				files = nil
			}
			f := newFixture(t, files)
			f.domain("example.org")
			if state == "unavailable" || state == "stopping" || state == "halted" {
				must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha, "")))
				if state == "unavailable" {
					must(t, os.RemoveAll(f.c.RepoDir(repository)))
				}
			}
			if state == "stopping" {
				f.l.Drain()
			}
			if state == "halted" {
				f.l.Halt()
			}
			operations := f.operations.Load()
			beforeTree := treeSnapshot(t, f.root)
			for _, target := range []string{"/", "/about?x=1", "/about/", "/a%20b.html?secret=hidden", "/%61%20b.html", "/missing", "/mcp", "/tools", "/_appkit/x", "/.env", "/other/"} {
				for _, conditional := range []bool{false, true} {
					for _, method := range []string{"GET", "HEAD"} {
						r := f.req(method, target, "")
						r.Host = "EXAMPLE.ORG:8443"
						r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
						if conditional {
							r.Header.Set("If-None-Match", "*")
						}
						slug := r.Clone(r.Context())
						u := *r.URL
						slug.URL = &u
						slug.URL.Path = "/blog" + r.URL.Path
						if r.URL.RawPath != "" {
							slug.URL.RawPath = "/blog" + r.URL.RawPath
						}
						slug.Host = "sites.example.test"
						got, want := f.domainAnswer(r), f.answer(slug, false)
						expected := want.Header().Clone()
						expected.Del("Link")
						if want.Code == 301 {
							expected.Set("Location", r.URL.EscapedPath()+"/")
							if r.URL.RawQuery != "" {
								expected.Set("Location", expected.Get("Location")+"?"+r.URL.RawQuery)
							}
						}
						if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), expected) {
							t.Fatalf("%s %s conditional=%t differs: %d %v %q vs %d %v %q", method, target, conditional, got.Code, got.Header(), got.Body.String(), want.Code, expected, want.Body.String())
						}
						assertHeader(t, got, "Last-Modified", "")
						assertHeader(t, got, "Link", "")
						if got.Code != 200 && got.Code != 304 {
							assertHeader(t, got, "ETag", "")
						}
						if method == "HEAD" && got.Body.Len() != 0 {
							t.Fatal("HEAD body")
						}
					}
				}
			}
			if state == "unpublished" && (operations != f.operations.Load() || !reflect.DeepEqual(beforeTree, treeSnapshot(t, f.root))) {
				t.Fatal("unpublished domain touched trees or started git")
			}
			// An empty URL path resolves the root, too.
			r := f.req("GET", "/", "")
			r.Host = "example.org"
			r.URL.Path = ""
			got := f.domainAnswer(r)
			if state == "published" && (got.Code != 200 || got.Body.String() != "index") {
				t.Fatal("empty path not root")
			}
			if state == "halted" && len(got.Header()) != 0 {
				t.Fatalf("halted answer headers %v body %q", got.Header(), got.Body.String())
			}
		})
	}
}

// R-EG6A-ZL1T R-EHE7-DCSI R-AG16-877K
func TestDomainRefusalsDoNotReadTreesOrRecordViews(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "private bytes"})
	f.domain("example.org")
	must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha, "")))
	must(t, os.RemoveAll(f.c.RepoDir(repository)))
	h := identity.Optional(serving.Domains(f.cfg))
	for _, failing := range []bool{false, true} {
		f.db.SetFailing(failing)
		for _, host := range []string{"example.org", "unknown.org", "EXAMPLE.ORG:8443"} {
			for _, method := range []string{"POST", "DELETE", "OPTIONS", "PATCH"} {
				r := f.req(method, "/mcp?x=1", "u_other")
				r.Host = host
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				assertStatus(t, w, 405)
				assertHeader(t, w, "Allow", "GET, HEAD")
				assertHeader(t, w, "ETag", "")
				assertHeader(t, w, "Link", "")
				if w.Body.Len() != 0 {
					t.Fatal("method refusal", w.Header())
				}
			}
		}
	}
	f.db.SetFailing(false)
	for _, private := range []bool{false, true} {
		host := "unknown.org"
		if private {
			// A persisted mixed-release record can carry a domain while private.
			must(t, f.db.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec("UPDATE sites SET payload=json_set(payload,'$.Visibility',?) WHERE id=?", store.Private, f.site.ID)
				return err
			}))
			host = "EXAMPLE.ORG:443"
		}
		before := treeSnapshot(t, f.root)
		operations := f.operations.Load()
		for _, method := range []string{"GET", "HEAD"} {
			for _, user := range []string{"", "u_other"} {
				for _, target := range []string{"/", "/mcp", "/tools", "/blog/", "/.env"} {
					r := f.req(method, target, user)
					r.Host = host
					r.Header.Set("If-None-Match", "*")
					r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					assertNotice(t, w, f.notice(r, 404, "notfound"))
					assertHeader(t, w, "Cache-Control", "no-cache")
					for _, key := range []string{"ETag", "Location", "Set-Cookie", "Link", "Last-Modified"} {
						assertHeader(t, w, key, "")
					}
				}
			}
		}
		if f.operations.Load() != operations || !reflect.DeepEqual(before, treeSnapshot(t, f.root)) {
			t.Fatal("refusal touched trees")
		}
	}
	if len(f.events()) != 0 {
		t.Fatal("refusal recorded view")
	}
	f.db.SetFailing(true)
	r := f.req("GET", "/", "")
	r.Host = "example.org"
	w := f.domainAnswer(r)
	assertStatus(t, w, 503)
	if w.Body.String() != store.Unreachable+"\n" {
		t.Fatal("catalog error became missing host")
	}
}

// R-EL1W-IO0L
func TestDomainLookupFollowsEveryCatalogChange(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "first", "docs/index.html": "next"})
	h := identity.Optional(serving.Domains(f.cfg))
	answer := func(host string) *httptest.ResponseRecorder {
		r := f.req("GET", "/", "")
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	assertStatus(t, answer("example.org"), 404)
	f.domain("example.org")
	assertStatus(t, answer("example.org"), 200)
	f.domain("other.org")
	assertStatus(t, answer("example.org"), 404)
	assertStatus(t, answer("other.org"), 200)
	f.domain("")
	assertStatus(t, answer("other.org"), 404)
	f.domain("example.org")
	f.write(f.work+"/index.html", "new commit")
	f.run("-C", f.work, "add", ".")
	f.run("-C", f.work, "commit", "-m", "next")
	sha := f.run("-C", f.work, "rev-parse", "HEAD")
	f.run("-C", f.c.RepoDir(repository), "fetch", f.work, "main")
	_, err := f.s.Publish(context.Background(), f.site.ID, sha, "")
	must(t, err)
	w := answer("example.org")
	assertStatus(t, w, 200)
	assertHeader(t, w, "ETag", publishedTag(sha, ""))
	if w.Body.String() != "new commit" {
		t.Fatal("old commit served")
	}
	_, err = f.s.Delete(context.Background(), f.site.ID)
	must(t, err)
	assertStatus(t, answer("example.org"), 404)
}

// R-AH92-LYY9 R-AIGY-ZQOY R-R1U7-CVH5 R-AG16-877K R-APSD-AD54
func TestDomainCookiesAndExactViewTrail(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, user := range []string{"", "u_other"} {
			for _, scheme := range []string{"http", "HTTPS"} {
				f := newFixture(t, map[string]string{"index.html": "index", "about/index.html": "about"})
				f.domain("example.org")
				for i, target := range []string{"/", "/about?from=secret", "/about/", "/absent"} {
					r := f.req(method, target, user)
					r.Host = "EXAMPLE.ORG:8443"
					r.Header.Set("X-Forwarded-Proto", scheme)
					r.Header.Set("Referer", "https://news.example/post?secret=hidden")
					r.Header.Set("X-Request-Id", fmt.Sprintf("domain-view-%d", i))
					if i == 2 {
						r.Header.Set("If-None-Match", "*")
					}
					w := f.domainAnswer(r)
					assertHeader(t, w, "Set-Cookie", visitor.Cookie(visitorID, scheme == "HTTPS"))
					events := f.events()
					want := telemetry.Attrs{"site": f.site.ID, "visitor": visitorID, "host": "example.org", "path": r.URL.Path, "status": int64(w.Code), "referrer_host": "news.example", "commit": f.sha}
					if len(events) != i+1 || events[i].Name != "site.viewed" || events[i].RequestID != r.Header.Get("X-Request-Id") || events[i].User != user || !reflect.DeepEqual(events[i].Attrs, want) {
						t.Fatalf("trail %v want %v", events, want)
					}
					if strings.Contains(w.Header().Get("Set-Cookie"), "Domain=") {
						t.Fatal("cookie has domain attribute")
					}
				}
				r := f.req("GET", "/", "")
				r.Host = "example.org"
				r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
				before := f.random.Len()
				assertHeader(t, f.domainAnswer(r), "Set-Cookie", "")
				if before != f.random.Len() {
					t.Fatal("existing visitor read random")
				}
			}
		}
	}
}

// R-ER5E-FIQ2 R-ESDA-TAGR
func TestDomainConcurrentRequestsPreserveCatalogAndCache(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "index", "404.html": "missing", "about/index.html": "about"})
	f.domain("example.org")
	_, err := f.s.SetApex(context.Background(), f.site.ID)
	must(t, err)
	before, err := f.s.Visible(context.Background(), "u_fixture", "", 100)
	must(t, err)
	apex, set, err := f.s.Apex(context.Background())
	must(t, err)
	tree := treeSnapshot(t, f.root)
	operations := f.operations.Load()
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	domain := identity.Optional(serving.Domains(f.cfg))
	apexHandler := identity.Optional(serving.Apex(f.cfg))
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			r := f.req("GET", "/", "")
			r.Host = "example.org"
			r.Header.Set("X-Request-Id", fmt.Sprintf("domain-concurrent-%d", i))
			w := httptest.NewRecorder()
			domain.ServeHTTP(w, r)
			if w.Code != 200 || w.Body.String() != "index" {
				t.Errorf("concurrent domain answer %d %q", w.Code, w.Body.String())
			}
			w = httptest.NewRecorder()
			apexHandler.ServeHTTP(w, r)
			if w.Code != 302 {
				t.Errorf("concurrent apex answer %d", w.Code)
			}
		}(i)
	}
	group.Wait()
	for _, method := range []string{"GET", "HEAD", "POST"} {
		for _, target := range []string{"/about", "/about/", "/missing", "/.env"} {
			r := f.req(method, target, "")
			r.Host = "example.org"
			domain.ServeHTTP(httptest.NewRecorder(), r)
		}
	}
	r := f.req("GET", "/", "")
	r.Host = "example.org"
	r.Header.Set("If-None-Match", "*")
	w := httptest.NewRecorder()
	domain.ServeHTTP(w, r)
	assertStatus(t, w, 304)
	after, err := f.s.Visible(context.Background(), "u_fixture", "", 100)
	must(t, err)
	afterApex, afterSet, err := f.s.Apex(context.Background())
	must(t, err)
	if !reflect.DeepEqual(before, after) || apex != afterApex || set != afterSet {
		t.Fatal("domain serving changed catalog")
	}
	if operations != f.operations.Load() || !reflect.DeepEqual(tree, treeSnapshot(t, f.root)) {
		t.Fatal("domain serving changed cache or started git")
	}
	if logs.Len() != 0 {
		t.Fatalf("default logger: %s", logs.String())
	}
}

// R-ANCK-ITNQ R-AOKG-WLEF R-AH92-LYY9
func TestDomainEventsFollowCompletedWriteContext(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			f := newFixture(t, map[string]string{"index.html": strings.Repeat("x", 65536)})
			f.domain("example.org")
			if unavailable {
				must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha, "")))
				must(t, os.RemoveAll(f.c.RepoDir(repository)))
			}
			r := f.req("GET", "/", "")
			r.Host = "example.org"
			r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			writer := &recordingWriter{header: make(http.Header), fail: true}
			if canceled {
				writer.cancel = cancel
			}
			identity.Optional(serving.Domains(f.cfg)).ServeHTTP(writer, r)
			if writer.writes == 0 {
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
			if want > 0 {
				attrs := telemetry.Attrs{"site": f.site.ID, "visitor": visitorID, "host": "example.org", "path": "/", "status": int64(200), "referrer_host": "", "commit": f.sha}
				if unavailable {
					attrs["status"] = int64(503)
				}
				if events[0].Name != "site.viewed" || !reflect.DeepEqual(events[0].Attrs, attrs) || events[0].RequestID != r.Header.Get("X-Request-Id") {
					t.Fatalf("view event %v", events[0])
				}
			}
			if want == 2 {
				attrs := telemetry.Attrs{"site": f.site.ID, "commit": f.sha, "reason": "repository_missing"}
				if events[1].Name != "site.unavailable" || !reflect.DeepEqual(events[1].Attrs, attrs) || events[1].RequestID != r.Header.Get("X-Request-Id") {
					t.Fatalf("unavailable event %v", events[1])
				}
			}
		}
	}
}

// R-EAMG-DNDQ
func TestDomainCanceledRebuildHasNoAnswerOrTrail(t *testing.T) {
	f := newFixture(t, map[string]string{"index.html": "index"})
	f.domain("example.org")
	must(t, os.RemoveAll(f.c.Dir(f.site.ID, f.sha, "")))
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.after = func(time.Duration) <-chan time.Time {
		once.Do(func() { close(entered); <-release })
		return make(chan time.Time)
	}
	f.joined = func(string, string) { close(joined) }
	direct := make(chan struct{})
	go func() { defer close(direct); _, _ = f.c.Tree(context.Background(), f.site.ID, repository, f.sha, "") }()
	wait(t, entered)
	r := f.req("GET", "/", "")
	r.Host = "example.org"
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	r = r.WithContext(ctx)
	writer := &recordingWriter{header: make(http.Header)}
	done := make(chan struct{})
	go func() { defer close(done); identity.Optional(serving.Domains(f.cfg)).ServeHTTP(writer, r) }()
	wait(t, joined)
	cancel()
	wait(t, done)
	close(release)
	wait(t, direct)
	if writer.writes != 0 || len(writer.statuses) != 0 || len(writer.header) != 0 {
		t.Fatalf("canceled domain answered %v", writer)
	}
	if len(f.events()) != 0 {
		t.Fatal("canceled domain recorded trail")
	}
}

// R-R1U7-CVH5 R-AH92-LYY9
func TestSlugViewRecordsRequestHostDespiteCanonicalDomain(t *testing.T) {
	f := newFixture(t, map[string]string{"about/index.html": "about"})
	f.domain("example.org")
	for i, tc := range []struct{ host, want string }{
		{"Sites.Sbx.ikigenba.dev:8443", "sites.sbx.ikigenba.dev"},
		{"sites.sbx.ikigenba.dev", "sites.sbx.ikigenba.dev"},
		{"localhost:7400", "localhost"},
		{"Sites.ÉXAMPLE.test", "sites.Éxample.test"},
	} {
		r := f.req("GET", "/blog/about/?from=newsletter", "")
		r.Host = tc.host
		r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
		r.Header.Set("X-Request-Id", fmt.Sprintf("slug-host-%d", i))
		r.Header.Set("Referer", "https://example.org/post?x=1")
		w := f.answer(r, false)
		assertStatus(t, w, 200)
		events := f.events()
		want := telemetry.Attrs{"site": f.site.ID, "visitor": visitorID, "host": tc.want, "path": "/blog/about/", "status": int64(200), "referrer_host": "example.org", "commit": f.sha}
		if len(events) != i+1 || events[i].Name != "site.viewed" || events[i].RequestID != r.Header.Get("X-Request-Id") || !reflect.DeepEqual(events[i].Attrs, want) {
			t.Fatalf("slug view %v want %v", events, want)
		}
	}
}
