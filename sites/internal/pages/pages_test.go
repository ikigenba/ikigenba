package pages_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

func templateSet(t *testing.T) *template.Template {
	t.Helper()
	s, err := page.Templates().ParseFS(sites.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func execute(t *testing.T, s *template.Template, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := s.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func load(t *testing.T) *pages.Set {
	t.Helper()
	s, err := pages.Load()
	if err != nil || s == nil {
		t.Fatalf("Load: %v, %v", s, err)
	}
	return s
}

func catalog(t *testing.T) *store.Store {
	t.Helper()
	s, _ := catalogHandle(t)
	return s
}

func catalogHandle(t *testing.T) (*store.Store, *db.DB) {
	t.Helper()
	random := make([]byte, (2*paging.DefaultLimit+10)*16)
	for i := 0; i < len(random); i += 8 {
		binary.LittleEndian.PutUint64(random[i:], uint64(i/8))
	}
	handle, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), Migrations: sites.Migrations(), Now: func() time.Time { return time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	return store.New(handle, store.Config{Now: func() time.Time { return time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC) }, Rand: bytes.NewReader(random)}), handle
}

func fixtureSites(t *testing.T, s *store.Store) []store.Site {
	t.Helper()
	var xs []store.Site
	for i, d := range []store.Draft{
		{Owner: "alice", Name: "z-own-secret", Repo: "repository-own-secret", Ref: "main", Visibility: store.Private, Listed: false},
		{Owner: "bob", Name: "c-other-secret", Repo: "repository-other-secret", Ref: "main", Visibility: store.Public, Listed: false},
		{Owner: "alice", Name: "b-private", Repo: "repository-private", Ref: "main", Visibility: store.Private, Listed: true},
		{Owner: "bob", Name: "a-public", Repo: "repository-public", Ref: "main", Visibility: store.Public, Listed: true},
	} {
		x, err := s.Create(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		if i >= 2 {
			x, err = s.Publish(context.Background(), x.ID, strings.Repeat("a", 40))
			if err != nil {
				t.Fatal(err)
			}
		}
		xs = append(xs, x)
	}
	return xs
}

func request(method, target, user, email string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader("request payload"))
	r.Host = "sites.example.test:7400"
	r.Header.Set("X-Forwarded-Proto", "Http")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	r.Header.Set("X-User-Email", email)
	return r
}

func answer(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func banner(u page.User) page.Banner {
	return page.Banner{Service: "sites", Release: "test-build+local", Commit: "test-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true, Trail: []page.Level{{Name: "source-level", URL: "/source-level"}}}
}

func config(t *testing.T, s *store.Store) pages.Config {
	t.Helper()
	return pages.Config{Banner: banner, Pages: load(t), Store: s}
}

// R-CP7E-TIAG R-CQFB-7A15 R-OURC-DHRL R-CU30-CL98 R-CVAW-QCZX R-CWIT-44QM
// R-D06I-9FYP R-X3R9-INHK R-X4Z5-WF89 R-X7EY-NYPN R-X8MV-1QGC
// R-X9UR-FI71 R-XB2N-T9XQ R-XCAK-71OF
func TestTemplateSetAndWrite(t *testing.T) {
	t.Chdir(t.TempDir())
	s := load(t)
	expected := templateSet(t)
	b := banner(page.User{Email: "a<\"&@example.test", ProfileURL: "https://example.test/?a=<\"&", LogoutURL: "https://example.test/logout?a=<\"&"})
	// Unkeyed construction proves the entire exported field sequence.
	row := pages.SiteRow{"odd<slug", "name<\"&", "https://example.test/<\"&", "private<\"&", false, true, true}
	values := []struct {
		name string
		data any
	}{
		{"landing", pages.LandingData{b, "https://example.test/<\"&", []pages.SiteRow{row}, "current<&", "prev<&", "next<&"}},
		{"landing", pages.LandingData{b, "https://example.test/", nil, "", "", ""}},
		{"about", pages.AboutData{b, "description<\"&"}},
		{"tools", pages.ToolsData{b, []pages.Tool{{"tool<\"&", "description<\"&"}, {"second-tool", "second description"}}}},
		{"tools", pages.ToolsData{b, nil}},
		{"notfound", pages.NoticeData{b}},
		{"unavailable", pages.NoticeData{b}},
	}
	for _, v := range values {
		if expected.Lookup(v.name) == nil {
			t.Fatalf("missing template %s", v.name)
		}
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, status := range []int{200, 299, 302, 404, 503, 599} {
				w := httptest.NewRecorder()
				w.Header()["Content-Type"] = []string{"old", "duplicate"}
				w.Header()["Cache-Control"] = []string{"private", "no-store"}
				w.Header().Set("Retry-After", "60")
				w.Header().Add("Set-Cookie", "visitor=one")
				w.Header().Add("Set-Cookie", "other=two")
				w.Header().Set("ETag", "caller-tag")
				before := w.Header().Clone()
				before["Content-Type"] = []string{"text/html; charset=utf-8"}
				s.Write(w, request(method, "/", "alice", ""), status, v.name, v.data)
				if w.Code != status || !reflect.DeepEqual(w.Header(), before) {
					t.Fatalf("%s %s %d: status %d headers %v", v.name, method, status, w.Code, w.Header())
				}
				want := execute(t, expected, v.name, v.data)
				if method == "HEAD" {
					want = ""
				}
				if w.Body.String() != want {
					t.Fatalf("%s %s: body differs from embedded template", v.name, method)
				}
			}
		}
		w := httptest.NewRecorder()
		s.Write(w, request("GET", "/", "alice", ""), 200, v.name, v.data)
		if !reflect.DeepEqual(w.Header(), http.Header{"Content-Type": {"text/html; charset=utf-8"}}) {
			t.Fatalf("extra headers: %v", w.Header())
		}
	}
}

// R-X2JD-4VQV R-XWC7-R027 R-D7HW-K2EV R-F142-115B R-XEQC-YL5T R-XFY9-CCWI
// R-F2BY-ESW0 R-DCDI-35DN R-P6YC-776J
func TestHandlerTemplateBytesAndBannerUser(t *testing.T) {
	s := catalog(t)
	fixtureSites(t, s)
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	files := []string{
		`{"services":[{"name":"auth","url":"https://account.test/a?x=1&y=2","description":"","socket":"/unused","enabled":true,"mcp":false},{"name":"sites","url":"https://published.test/path/","description":"","socket":"/unused","enabled":true,"mcp":true}]}`,
		`{"services":[]}`,
		`invalid`,
	}
	for _, file := range files {
		if err := os.WriteFile(servicesPath, []byte(file), 0600); err != nil {
			t.Fatal(err)
		}
		var users []page.User
		cfg := pages.Config{func(u page.User) page.Banner { users = append(users, u); return banner(u) }, load(t), servicesPath, s, []pages.Tool{{Name: "supplied-second", Description: "second supplied description"}, {Name: "supplied-first", Description: "first supplied description"}}}
		h := identity.Optional(pages.Handler(cfg))
		for _, path := range []string{"/?query=a%26b", "/about?query=a%26b", "/tools?query=a%26b"} {
			for _, email := range []string{" alice<&\"@example.test ", ""} {
				for _, user := range []string{"alice", "bob"} {
					r := request("GET", path, user, email)
					r.Header.Add("X-User-Email", "ignored@example.test")
					users = nil
					w := answer(h, r)
					u := page.User{Email: email, ProfileURL: urls.AuthProfile(r, servicesPath), LogoutURL: urls.AuthLogout(r, servicesPath)}
					if !reflect.DeepEqual(users, []page.User{u}) {
						t.Fatalf("banner users: %#v, want %#v", users, u)
					}
					b := banner(u)
					b.Trail = []page.Level{{Name: "about", URL: "/about"}}
					name := "about"
					var data any = pages.AboutData{Banner: b, Description: pages.Description}
					if r.URL.Path == "/" {
						b.Trail = nil
						name = "landing"
						base := urls.SitesURL(r, servicesPath)
						xs, err := s.Visible(r.Context(), user, "", paging.DefaultLimit+1)
						if err != nil {
							t.Fatal(err)
						}
						rows := make([]pages.SiteRow, 0, len(xs))
						for _, x := range xs {
							rows = append(rows, pages.SiteRow{Slug: x.Slug, Name: x.Name, URL: urls.SiteURL(base, x.Slug), Visibility: x.Visibility, Listed: x.Listed, Published: x.Commit != "", Mine: x.Owner == user})
						}
						data = pages.LandingData{Banner: b, SitesURL: base, Sites: rows}
					}
					if r.URL.Path == "/tools" {
						name = "tools"
						b.Trail = []page.Level{{Name: "tools", URL: "/tools"}}
						data = pages.ToolsData{Banner: b, Tools: cfg.Tools}
						for _, tool := range cfg.Tools {
							if !strings.Contains(w.Body.String(), tool.Name) || !strings.Contains(w.Body.String(), tool.Description) {
								t.Fatal("supplied tool missing from page")
							}
						}
					}
					if r.URL.Path != "/" {
						level := b.Trail[0]
						if !strings.Contains(w.Body.String(), level.Name) || !strings.Contains(w.Body.String(), level.URL) {
							t.Fatal("expected level missing from page")
						}
					}
					if w.Code != 200 || !reflect.DeepEqual(w.Header()["Content-Type"], []string{"text/html; charset=utf-8"}) || w.Body.String() != execute(t, templateSet(t), name, data) {
						t.Fatalf("%s %s: wrong template answer %d", path, user, w.Code)
					}
				}
			}
		}
	}
}

// R-EXGC-VPX8 R-EYO9-9HNX R-EZW5-N9EM R-F142-115B R-F2BY-ESW0 R-F3JU-SKMP R-F8FG-BNLH
func TestLandingWindows(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit - 1, paging.DefaultLimit, paging.DefaultLimit + 1, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s := catalog(t)
			ctx := context.Background()
			for _, name := range []string{"a-hidden-before", "z-hidden-after"} {
				if _, err := s.Create(ctx, store.Draft{Owner: "bob", Name: name, Repo: "hidden-repository", Ref: "main", Visibility: store.Public}); err != nil {
					t.Fatal(err)
				}
			}
			var visible []store.Site
			for i := count - 1; i >= 0; i-- {
				d := store.Draft{Owner: "alice", Name: fmt.Sprintf("site-%06d", i), Repo: fmt.Sprintf("repository-%06d", i), Ref: "main", Visibility: store.Private, Listed: i%3 != 0}
				if i%3 == 1 {
					d.Owner = "bob"
					d.Visibility = store.Public
				}
				x, err := s.Create(ctx, d)
				if err != nil {
					t.Fatal(err)
				}
				if i%2 == 0 {
					x, err = s.Publish(ctx, x.ID, strings.Repeat("b", 40))
					if err != nil {
						t.Fatal(err)
					}
				}
				visible = append(visible, x)
			}
			slices.Reverse(visible)
			cfg := config(t, s)
			h := identity.Optional(pages.Handler(cfg))
			token := paging.Token{Tool: "sites"}
			positions := []string{"last", (paging.Token{Tool: "sites-before"}).Encode("site-000000-extra"), (paging.Token{Tool: "sites-before"}).Encode("zz-after-all"), "", token.Encode(""), token.Encode("site-000000"), token.Encode("site-000000-extra"), token.Encode("zz-after-all")}
			if count > paging.DefaultLimit {
				positions = append(positions, token.Encode(visible[paging.DefaultLimit-1].Name), (paging.Token{Tool: "sites-before"}).Encode(visible[paging.DefaultLimit].Name), (paging.Token{Tool: "sites-before"}).Encode(visible[paging.DefaultLimit].Name+"-gap"))
			}
			for _, current := range positions {
				for _, suffix := range []string{"", "&limit=1&unrelated=ignored&page=bogus"} {
					r := request("GET", "/?page="+current+suffix, "alice", "alice@example.test")
					var key string
					reverse := current == "last"
					if current != "" && !reverse {
						if err := token.Decode(current, &key); err != nil {
							if err := (paging.Token{Tool: "sites-before"}).Decode(current, &key); err != nil {
								t.Fatal(err)
							}
							reverse = true
						}
					}
					remaining := slices.Clone(visible)
					if reverse {
						if current != "last" {
							remaining = slices.DeleteFunc(remaining, func(x store.Site) bool { return x.Name >= key })
						}
					} else if current != "" {
						remaining = slices.DeleteFunc(remaining, func(x store.Site) bool { return x.Name <= key })
					}
					if reverse && len(remaining) <= paging.DefaultLimit {
						for _, method := range []string{"GET", "HEAD"} {
							w := answer(h, request(method, r.URL.String(), "alice", "alice@example.test"))
							if w.Code != 303 || !reflect.DeepEqual(w.Header()["Location"], []string{"/"}) {
								t.Fatalf("underflow: %d %v", w.Code, w.Header())
							}
						}
						continue
					}
					window := remaining[:min(len(remaining), paging.DefaultLimit)]
					if reverse {
						window = remaining[max(0, len(remaining)-paging.DefaultLimit):]
					}
					next, prev := "", ""
					if len(window) > 0 {
						if reverse && current != "last" || !reverse && len(remaining) > paging.DefaultLimit {
							next = token.Encode(window[len(window)-1].Name)
						}
						if current != "" {
							prev = (paging.Token{Tool: "sites-before"}).Encode(window[0].Name)
						}
					}
					base := urls.SitesURL(r, cfg.ServicesPath)
					rows := make([]pages.SiteRow, 0, len(window))
					for _, x := range window {
						rows = append(rows, pages.SiteRow{Slug: x.Slug, Name: x.Name, URL: urls.SiteURL(base, x.Slug), Visibility: x.Visibility, Listed: x.Listed, Published: x.Commit != "", Mine: x.Owner == "alice"})
					}
					b := banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, cfg.ServicesPath), LogoutURL: urls.AuthLogout(r, cfg.ServicesPath)})
					b.Trail = nil
					data := pages.LandingData{Banner: b, SitesURL: base, Sites: rows, Page: current, Prev: prev, Next: next}
					w := answer(h, r)
					if w.Code != http.StatusOK || !reflect.DeepEqual(w.Header()["Content-Type"], []string{"text/html; charset=utf-8"}) || w.Body.String() != execute(t, templateSet(t), "landing", data) {
						t.Fatalf("landing with %d visible sites at %s differs from window template", count, r.URL)
					}
					head := answer(h, request("HEAD", r.URL.String(), "alice", "alice@example.test"))
					if head.Code != w.Code || !reflect.DeepEqual(head.Header(), w.Header()) || head.Body.Len() != 0 {
						t.Fatalf("landing HEAD differs at %s", r.URL)
					}
				}
			}
		})
	}
}

// R-EXGC-VPX8 R-F5ZN-K443
func TestLandingUndecodablePage(t *testing.T) {
	s, handle := catalogHandle(t)
	cfg := config(t, s)
	var users []page.User
	cfg.Banner = func(u page.User) page.Banner { users = append(users, u); return banner(u) }
	h := identity.Optional(pages.Handler(cfg))
	foreign := (paging.Token{Tool: "list"}).Encode("site-position")
	var decoded string
	if err := (paging.Token{Tool: "sites"}).Decode(foreign, &decoded); err == nil {
		t.Fatal("fixture must be rejected under the landing token binding")
	}
	wrongShape := (paging.Token{Tool: "sites"}).Encode(int64(123))
	for _, failing := range []bool{false, true} {
		handle.SetFailing(failing)
		for _, current := range []string{"bogus", "Last", foreign, wrongShape, (paging.Token{Tool: "sites-before"}).Encode(int64(123)), (paging.Token{Tool: "sites-before"}).Encode("")} {
			for _, method := range []string{"GET", "HEAD"} {
				r := request(method, "/?ignored=x&page="+current+"&page=", "alice", "ignored@example.test")
				users = nil
				got := answer(h, r)
				want := httptest.NewRecorder()
				want.Header().Set("Cache-Control", "no-cache")
				cfg.Pages.Write(want, r, http.StatusNotFound, "notfound", pages.NoticeData{Banner: banner(page.User{})})
				if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
					t.Fatalf("invalid page %q failing=%v method=%s: status %d headers %v", current, failing, method, got.Code, got.Header())
				}
				if !reflect.DeepEqual(users, []page.User{{}}) {
					t.Fatalf("notice banner users: %#v", users)
				}
			}
		}
	}
}

// R-XH65-Q4N7 R-XIE2-3WDW R-XJLY-HO4L R-XKTU-VFVA R-P6YC-776J
func TestMethodsGuestsAndFailingCatalog(t *testing.T) {
	s, handle := catalogHandle(t)
	var calls int
	cfg := config(t, s)
	cfg.Banner = func(u page.User) page.Banner { calls++; return banner(u) }
	h := identity.Optional(pages.Handler(cfg))
	for _, target := range []string{"/?from=launcher", "/?page=bogus", "/about?from=launcher", "/tools?from=launcher"} {
		for _, user := range []string{"", "alice"} {
			get := answer(h, request("GET", target, user, "present@example.test"))
			head := answer(h, request("HEAD", target, user, "present@example.test"))
			if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
				t.Fatalf("HEAD differs: %s %q", target, user)
			}
		}
	}
	var before []*httptest.ResponseRecorder
	var rs []*http.Request
	for _, target := range []string{"/?from=launcher", "/?page=bogus", "/about?from=launcher", "/tools?from=launcher"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CUSTOM"} {
			for _, user := range []string{"", "alice"} {
				if target != "/tools?from=launcher" && user != "" && (method == "GET" || method == "HEAD") {
					continue
				}
				r := request(method, target, user, "present@example.test")
				if user == "" {
					r.Header["X-User-Id"] = []string{"", "alice"}
				}
				calls = 0
				w := answer(h, r)
				wantCalls := 0
				if user != "" && (method == "GET" || method == "HEAD") {
					wantCalls = 1
				}
				if calls != wantCalls {
					t.Fatal("wrong banner call count")
				}
				switch {
				case user != "" && (method == "GET" || method == "HEAD"):
					if w.Code != 200 {
						t.Fatal("signed-in tools page failed")
					}
				case method == "GET" || method == "HEAD":
					if w.Code != 302 || !reflect.DeepEqual(w.Header()["Location"], []string{urls.SignIn(r)}) {
						t.Fatalf("guest: %d %v", w.Code, w.Header())
					}
				case w.Code != 405 || !reflect.DeepEqual(w.Header()["Allow"], []string{"GET, HEAD"}) || w.Body.Len() != 0:
					t.Fatalf("refusal: %d %v %q", w.Code, w.Header(), w.Body.String())
				}
				before = append(before, w)
				rs = append(rs, r)
			}
		}
	}
	handle.SetFailing(true)
	for i, r := range rs {
		w := answer(h, r)
		if w.Code != before[i].Code || !reflect.DeepEqual(w.Header(), before[i].Header()) || w.Body.String() != before[i].Body.String() {
			t.Fatal("failing catalog changed refusal/redirect")
		}
	}
	for _, method := range []string{"GET", "HEAD"} {
		calls = 0
		w := answer(h, request(method, "/", "alice", ""))
		if w.Code != 503 || calls != 0 {
			t.Fatalf("failing landing called banner: %d calls %d", w.Code, calls)
		}
		w = answer(h, request(method, "/about", "alice", ""))
		if w.Code != 200 || calls != 1 {
			t.Fatalf("failing about: %d calls %d", w.Code, calls)
		}
	}
}

// R-DM4P-5BB7
func TestHandlerConcurrent(t *testing.T) {
	s := catalog(t)
	fixtureSites(t, s)
	h := identity.Optional(pages.Handler(config(t, s)))
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			path := "/"
			if i%2 == 0 {
				path = "/about"
			}
			w := answer(h, request("GET", path, "alice", "alice@example.test"))
			if w.Code != 200 || w.Body.Len() == 0 {
				t.Errorf("concurrent %s: %d", path, w.Code)
			}
		})
	}
	wg.Wait()
}

// R-F77J-XVUS
func TestLandingForwardBackwardWalk(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit + 1, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 7} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s := catalog(t)
			visible := make([]store.Site, 0, count)
			for i := range count {
				x, err := s.Create(context.Background(), store.Draft{Owner: "alice", Name: fmt.Sprintf("walk-%06d", i), Repo: "walk-repository", Ref: "main", Visibility: store.Private})
				if err != nil {
					t.Fatal(err)
				}
				visible = append(visible, x)
			}
			cfg := config(t, s)
			h := identity.Optional(pages.Handler(cfg))
			check := func(current string, window []store.Site, prev, next string) {
				t.Helper()
				r := request("GET", "/?page="+current, "alice", "walk@example.test")
				b := banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, cfg.ServicesPath), LogoutURL: urls.AuthLogout(r, cfg.ServicesPath)})
				b.Trail = nil
				base := urls.SitesURL(r, cfg.ServicesPath)
				rows := make([]pages.SiteRow, 0, len(window))
				for _, x := range window {
					rows = append(rows, pages.SiteRow{Slug: x.Slug, Name: x.Name, URL: urls.SiteURL(base, x.Slug), Visibility: x.Visibility, Listed: x.Listed, Published: x.Commit != "", Mine: x.Owner == "alice"})
				}
				want := pages.LandingData{Banner: b, SitesURL: base, Sites: rows, Page: current, Prev: prev, Next: next}
				w := answer(h, r)
				if w.Code != 200 || w.Body.String() != execute(t, templateSet(t), "landing", want) {
					t.Fatalf("walk page %q does not match expected window", current)
				}
			}
			after, before := paging.Token{Tool: "sites"}, paging.Token{Tool: "sites-before"}
			var windows [][]store.Site
			current := ""
			for start := 0; start < len(visible); start += paging.DefaultLimit {
				end := min(start+paging.DefaultLimit, len(visible))
				window := visible[start:end]
				prev, next := "", ""
				if start > 0 {
					prev = before.Encode(window[0].Name)
				}
				if end < len(visible) {
					next = after.Encode(window[len(window)-1].Name)
				}
				check(current, window, prev, next)
				windows = append(windows, window)
				if next != "" {
					current = next
				}
			}
			steps := 0
			for j := len(windows) - 1; j > 0; j-- {
				window := windows[j]
				prev := before.Encode(window[0].Name)
				next := ""
				if j != len(windows)-1 {
					next = after.Encode(window[len(window)-1].Name)
				}
				check(current, window, prev, next)
				current = prev
				steps++
			}
			w := answer(h, request("GET", "/?page="+current, "alice", "walk@example.test"))
			if steps != len(windows)-1 || w.Code != 303 || !reflect.DeepEqual(w.Header()["Location"], []string{"/"}) {
				t.Fatalf("walk did not end at first-page redirect: steps %d response %d %v", steps, w.Code, w.Header())
			}
		})
	}
}

// R-P6YC-776J
func TestLandingRedirectDoesNotCallBanner(t *testing.T) {
	s, handle := catalogHandle(t)
	cfg := config(t, s)
	calls := 0
	cfg.Banner = func(u page.User) page.Banner { calls++; return banner(u) }
	h := identity.Optional(pages.Handler(cfg))
	for _, failing := range []bool{false, true} {
		handle.SetFailing(failing)
		for _, value := range []string{"last", (paging.Token{Tool: "sites-before"}).Encode("before-any")} {
			for _, method := range []string{"GET", "HEAD"} {
				calls = 0
				w := answer(h, request(method, "/?page="+value, "alice", "banner@example.test"))
				want := 303
				if failing {
					want = 503
				}
				if w.Code != want || calls != 0 {
					t.Fatalf("status %d banner calls %d", w.Code, calls)
				}
			}
		}
	}
}
