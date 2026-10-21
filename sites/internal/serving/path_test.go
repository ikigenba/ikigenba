package serving_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/visitor"
)

func publishedTag(commit, path string) string {
	if path != "" {
		commit += "-" + hex.EncodeToString([]byte(path))
	}
	return "\"" + commit + "\""
}

func TestPublishedDirectoriesAndConfiguredChanges(t *testing.T) {
	// R-5HAK-LJ61 R-5IIG-ZAWQ R-5JQD-D2NF R-5KY9-QUE4 R-MC78-ZEQV R-MDF5-D6HK R-5M66-4M4T R-PX8N-N4Z0
	for _, source := range []string{"", "docs", "odd \" ,\\%é"} {
		t.Run(source, func(t *testing.T) {
			files := map[string]string{"index.html": "root index", "404.html": "root missing", "x": "root x", "site/only": "only new path", "site/index.html": "new index", "docs/index.html": "docs index", "docs/x": "docs x\r\nwithout newline", "docs/404.html": "docs missing"}
			if source != "" && source != "docs" {
				files[source+"/index.html"] = "unusual index"
				files[source+"/x"] = "unusual x"
				files[source+"/404.html"] = "unusual missing"
			}
			f := newFixture(t, files)
			f.site = publishPath(t, f, f.site, source)
			tag := publishedTag(f.sha, source)
			handler := f.handler(false)
			answered := 0
			answer := func(method, target, conditional string) *httptest.ResponseRecorder {
				r := f.req(method, target, "")
				answered++
				r.Header.Set("X-Request-Id", fmt.Sprintf("directory-view-%d", answered))
				r.Header.Set("Cookie", visitor.CookieName+"="+visitorID)
				if conditional != "" {
					r.Header.Set("If-None-Match", conditional)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				events := f.events()
				if len(events) != answered {
					t.Fatalf("got %d events for %d views", len(events), answered)
				}
				event := events[answered-1]
				if event.Name != "site.viewed" || event.RequestID != r.Header.Get("X-Request-Id") {
					t.Fatalf("wrong view event %v", event)
				}
				if tag := w.Header().Get("ETag"); tag != "" && event.Attrs["commit"] != tag[1:41] {
					t.Fatalf("event commit %v does not match response ETag %q", event.Attrs["commit"], tag)
				}
				return w
			}
			for _, rest := range []string{"/", "/x", "/absent", "/only"} {
				target := "/blog" + rest
				original := answer("GET", target, "")
				expected := files[strings.TrimPrefix(source+rest, "/")]
				status := 200
				if rest == "/" {
					expected = files[filepath.ToSlash(filepath.Join(source, "index.html"))]
				}
				if rest == "/absent" || rest == "/only" {
					status = 404
					expected = files[filepath.ToSlash(filepath.Join(source, "404.html"))]
				}
				assertStatus(t, original, status)
				if original.Body.String() != expected {
					t.Fatalf("%s: got %q want %q", target, original.Body.String(), expected)
				}
				if status == 200 {
					assertHeader(t, original, "ETag", tag)
					conditional := answer("GET", target, tag)
					assertStatus(t, conditional, 304)
					assertHeader(t, conditional, "ETag", tag)
					assertHeader(t, conditional, "Content-Type", "")
					assertHeader(t, conditional, "Content-Length", "")
					if conditional.Body.Len() != 0 {
						t.Fatal("conditional body")
					}
				}
				for _, configured := range []string{"docs", "", "site"} {
					_, fields, err := f.s.Update(context.Background(), f.site.ID, store.Change{Path: &configured})
					must(t, err)
					_ = fields
					next := answer("GET", target, "")
					if next.Code != original.Code || !reflect.DeepEqual(next.Header(), original.Header()) || next.Body.String() != original.Body.String() {
						t.Fatal("configured path changed serving")
					}
				}
			}
			before := answer("GET", "/blog/x", "")
			nextPath := "docs"
			if source == "docs" {
				nextPath = ""
			}
			f.site = publishPath(t, f, f.site, nextPath)
			after := answer("GET", "/blog/x", tag)
			assertStatus(t, after, 200)
			assertHeader(t, after, "ETag", publishedTag(f.sha, nextPath))
			if after.Body.String() == before.Body.String() {
				t.Fatal("publish path failed to change tree")
			}
			must(t, os.RemoveAll(f.c.RepoDir(repository)))
			operations := f.operations.Load()
			cached := answer("GET", "/blog/x", tag)
			if cached.Code != after.Code || !reflect.DeepEqual(cached.Header(), after.Header()) || cached.Body.String() != after.Body.String() || f.operations.Load() != operations {
				t.Fatal("cached directory depended on repository")
			}
		})
	}
}

func publishPath(t *testing.T, f *fixture, site store.Site, path string) store.Site {
	t.Helper()
	must(t, f.c.Unpack(context.Background(), site.ID, repository, f.sha, path))
	site, err := f.s.Publish(context.Background(), site.ID, f.sha, path)
	must(t, err)
	return site
}

func TestDistinctSitesShareCommitWithoutSharingPath(t *testing.T) {
	// R-DUYP-XPRM
	for _, reverse := range []bool{false, true} {
		for _, second := range []string{"two", ""} {
			t.Run(fmt.Sprint(reverse, second), func(t *testing.T) {
				f := newFixture(t, map[string]string{"x": "root", "one/x": "first", "two/x": "second"})
				a, err := f.s.Publish(context.Background(), f.site.ID, f.sha, "one")
				must(t, err)
				b, err := f.s.Create(context.Background(), store.Draft{Owner: a.Owner, Name: "other", Repo: repository, Ref: "main", Visibility: store.Public, Listed: true})
				must(t, err)
				b, err = f.s.Publish(context.Background(), b.ID, f.sha, second)
				must(t, err)
				must(t, os.RemoveAll(filepath.Join(f.root, a.ID)))
				order := []store.Site{a, b}
				if reverse {
					order = []store.Site{b, a}
				}
				for range 2 {
					for _, site := range order {
						w := f.answer(f.req("GET", "/"+site.Slug+"/x", ""), false)
						assertStatus(t, w, 200)
						want := "first"
						if site.ID == b.ID {
							want = "second"
							if second == "" {
								want = "root"
							}
						}
						if w.Body.String() != want {
							t.Fatalf("%s got %q want %q", site.Slug, w.Body.String(), want)
						}
						assertHeader(t, w, "ETag", publishedTag(f.sha, site.PublishedPath))
					}
				}
			})
		}
	}
}

func TestDirectoryGuestAndApexNeverRebuild(t *testing.T) {
	// R-5G2O-7RFC R-5NE2-IDVI
	f := newFixture(t, map[string]string{"docs/index.html": "private directory"})
	var err error
	f.site, err = f.s.Publish(context.Background(), f.site.ID, f.sha, "docs")
	must(t, err)
	_, err = f.s.SetApex(context.Background(), f.site.ID)
	must(t, err)
	operations := f.operations.Load()
	for _, method := range []string{"GET", "HEAD", "POST"} {
		f.answer(f.req(method, "/blog/", ""), true)
	}
	private := store.Private
	must(t, f.s.ClearApex(context.Background()))
	_, _, err = f.s.Update(context.Background(), f.site.ID, store.Change{Visibility: &private})
	must(t, err)
	for _, method := range []string{"GET", "HEAD"} {
		w := f.answer(f.req(method, "/blog/", ""), false)
		assertStatus(t, w, 302)
		assertHeader(t, w, "ETag", "")
	}
	if f.operations.Load() != operations {
		t.Fatal("guest or apex started git")
	}
	if _, err = os.Stat(f.c.Dir(f.site.ID, f.sha, "docs")); !os.IsNotExist(err) {
		t.Fatalf("unexpected tree %v", err)
	}
}
