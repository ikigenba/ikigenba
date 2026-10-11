package pages_test

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	artifacts "github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/pages"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/tools"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

type countingRandom struct{ next byte }

func (r *countingRandom) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = r.next
		r.next++
	}
	return len(b), nil
}

func templates(t *testing.T) *template.Template {
	t.Helper()
	s, err := page.Templates().ParseFS(artifacts.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func rendered(t *testing.T, s *template.Template, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := s.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
func fixedBanner(u page.User) page.Banner {
	return page.Banner{Service: pages.ServiceName, Release: "test-release", Commit: "test-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Home: "https://home.example.test/", Tools: true, Trail: []page.Level{{Name: "prior", URL: "/prior"}}}
}
func request(method, path string) *http.Request {
	r := httptest.NewRequest(method, "https://artifacts.sbx.ikigenba.dev:443"+path, nil)
	r.Header.Set("X-User-Id", "reader-id")
	r.Header.Set("X-User-Email", "reader@example.test")
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}
func banner(r *http.Request, servicesPath string) page.Banner {
	a := urls.Auth(r, servicesPath)
	return fixedBanner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: a + "/", LogoutURL: a + "/logout"})
}
func serve(cfg pages.Config, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	identity.Require(pages.Handler(cfg)).ServeHTTP(w, r)
	return w
}
func checkPage(t *testing.T, cfg pages.Config, r *http.Request, status int, name string, data any) {
	t.Helper()
	w := serve(cfg, r)
	want := rendered(t, templates(t), name, data)
	if r.Method == http.MethodHead {
		want = ""
	}
	if w.Code != status || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Body.String() != want {
		t.Fatalf("%s %s: status %d, headers %v, body differs: %t", r.Method, r.URL.EscapedPath(), w.Code, w.Header(), w.Body.String() != want)
	}
}
func fixture(t *testing.T) (pages.Config, *db.DB) {
	t.Helper()
	return fixtureWithClock(t, func() time.Time { return time.Date(2030, 1, 2, 3, 4, 5, 987000000, time.FixedZone("test-zone", 7200)) })
}
func fixtureWithClock(t *testing.T, now func() time.Time) (pages.Config, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "records.db"), Migrations: artifacts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	blobs := filepath.Join(dir, "blobs")
	if err := os.Mkdir(blobs, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	return pages.Config{Banner: fixedBanner, Pages: s, ServicesPath: filepath.Join(dir, "absent.json"), Store: store.New(d, blobs, store.Config{Now: now, Rand: &countingRandom{next: 0xab}})}, d
}
func put(t *testing.T, s *store.Store, path string, exp time.Time) store.Artifact {
	t.Helper()
	a, _, err := s.Put(context.Background(), store.Draft{Path: path, OwnerID: "owner-id", OwnerEmail: "owner@example.test", ContentType: "application/test", Max: 100, Expires: exp}, strings.NewReader(path))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPublicDataAndTemplates(t *testing.T) {
	const _ string = pages.ServiceName
	const _ string = pages.Description
	// R-NQFY-7UB1 R-MSDH-H40V R-MUTA-8NI9 R-MW16-MF8Y R-MX93-06ZN
	if pages.ServiceName != "artifacts" || pages.Description != "Binary files agents and services share" || pages.ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || pages.AboutLevel != (page.Level{Name: "about", URL: "/about"}) {
		t.Fatal("service identity or levels")
	}
	s, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	row := pages.ArtifactRow{ID: "row-id", Path: "row-path", Bytes: 37, ContentType: "test/type", Owner: "row-owner", Modified: "modified", ModifiedText: "modified-text", Expires: "expiry", ExpiresText: "expiry-text"}
	view := pages.ArtifactView{ID: "view-id", Path: "view-path", URL: "https://example.test/files/view-path", Bytes: 42, ContentType: "view/type", SHA256: "hash", Owner: "view-owner", Created: "created", CreatedText: "created-text", Modified: "modified", ModifiedText: "modified-text", Expires: "expiry", ExpiresText: "expiry-text"}
	b := fixedBanner(page.User{Email: "chosen@example.test", ProfileURL: "https://profile.example/", LogoutURL: "https://logout.example/"})
	noExpiryRow := row
	noExpiryRow.Expires = ""
	noExpiryRow.ExpiresText = ""
	noExpiryView := view
	noExpiryView.Expires = ""
	noExpiryView.ExpiresText = ""
	cases := []struct {
		name string
		data any
	}{
		{"landing", pages.LandingData{Banner: b}},
		{"landing", pages.LandingData{Banner: b, Artifacts: []pages.ArtifactRow{row, noExpiryRow}, TotalBytes: 74}},
		{"artifact", pages.ArtifactData{Banner: b, Artifact: view}},
		{"artifact", pages.ArtifactData{Banner: b, Artifact: noExpiryView}},
		{"tools", pages.ToolsData{Banner: b, Tools: []pages.ToolRow{{Name: "test-tool", Description: "test-description"}}}},
		{"about", pages.AboutData{Banner: b, Description: pages.Description}},
		{"notfound", pages.NoticeData{Banner: b}},
	}
	for _, c := range cases {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := request(method, "/")
			w := httptest.NewRecorder()
			s.Write(w, r, 207, c.name, c.data)
			want := rendered(t, templates(t), c.name, c.data)
			if method == http.MethodHead {
				want = ""
			}
			if w.Code != 207 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Body.String() != want {
				t.Fatalf("%s %s render mismatch", method, c.name)
			}
		}
	}
}

func TestIdentityRequired(t *testing.T) {
	// R-OUMA-CXCZ
	cfg, _ := fixture(t)
	calls := 0
	cfg.Banner = func(u page.User) page.Banner { calls++; return fixedBanner(u) }
	r := request(http.MethodGet, "/")
	r.Header.Del("X-User-Id")
	w := serve(cfg, r)
	if w.Code != http.StatusInternalServerError || w.Body.String() != identity.MissingBody || calls != 0 {
		t.Fatalf("guest status %d banner calls %d", w.Code, calls)
	}
	r.Header.Set("X-User-Id", "reader-id")
	w = serve(cfg, r)
	if w.Code != 200 || calls != 1 {
		t.Fatal("authenticated page unreachable")
	}
}

func TestLandingData(t *testing.T) {
	// R-OS6H-LDVL
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.FixedZone("test-zone", 7200))
	cfg, _ := fixtureWithClock(t, func() time.Time { return now })
	r := request(http.MethodGet, "/")
	b := banner(r, cfg.ServicesPath)
	b.Trail = nil
	checkPage(t, cfg, r, 200, "landing", pages.LandingData{Banner: b})
	expiry := time.Date(2031, 7, 8, 9, 10, 11, 0, time.FixedZone("expiry-zone", -3600))
	z := put(t, cfg.Store, "zeta/report.bin", time.Time{})
	a := put(t, cfg.Store, "alpha.bin", expiry)
	now = now.Add(2*time.Hour + 3*time.Minute + 4*time.Second)
	z = put(t, cfg.Store, z.Path, time.Time{})
	a = put(t, cfg.Store, a.Path, expiry)
	if a.Created.Equal(a.Modified) || z.Created.Equal(z.Modified) {
		t.Fatal("fixture requires distinct creation and modification times")
	}
	rows := []pages.ArtifactRow{}
	for _, v := range []store.Artifact{a, z} {
		row := pages.ArtifactRow{ID: v.ID, Path: v.Path, Bytes: v.Bytes, ContentType: v.ContentType, Owner: v.OwnerEmail, Modified: v.Modified.UTC().Format(time.RFC3339), ModifiedText: v.Modified.UTC().Format("2006-01-02 15:04")}
		if !v.Expires.IsZero() {
			row.Expires = v.Expires.UTC().Format(time.RFC3339)
			row.ExpiresText = v.Expires.UTC().Format("2006-01-02 15:04")
		}
		rows = append(rows, row)
	}
	checkPage(t, cfg, r, 200, "landing", pages.LandingData{Banner: b, Artifacts: rows, TotalBytes: a.Bytes + z.Bytes})
}

func artifactData(r *http.Request, cfg pages.Config, a store.Artifact) pages.ArtifactData {
	b := banner(r, cfg.ServicesPath)
	b.Trail = []page.Level{{Name: a.Path, URL: "/" + a.ID}}
	v := pages.ArtifactView{ID: a.ID, Path: a.Path, URL: urls.File(urls.Base(r, cfg.ServicesPath), a.Path), Bytes: a.Bytes, ContentType: a.ContentType, SHA256: a.SHA256, Owner: a.OwnerEmail, Created: a.Created.UTC().Format(time.RFC3339), CreatedText: a.Created.UTC().Format("2006-01-02 15:04:05 UTC"), Modified: a.Modified.UTC().Format(time.RFC3339), ModifiedText: a.Modified.UTC().Format("2006-01-02 15:04:05 UTC")}
	if !a.Expires.IsZero() {
		v.Expires = a.Expires.UTC().Format(time.RFC3339)
		v.ExpiresText = a.Expires.UTC().Format("2006-01-02 15:04:05 UTC")
	}
	return pages.ArtifactData{Banner: b, Artifact: v}
}
func TestArtifactDataAndDeleted(t *testing.T) {
	// R-OTED-Z5MA R-OX23-4GUD
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.FixedZone("test-zone", 7200))
	cfg, _ := fixtureWithClock(t, func() time.Time { return now })
	for i, expiry := range []time.Time{{}, time.Date(2032, 2, 3, 4, 5, 6, 0, time.FixedZone("later", 3600))} {
		a := put(t, cfg.Store, []string{"no-expiry/report.bin", "with-expiry/report.bin"}[i], expiry)
		now = now.Add(3*time.Hour + 4*time.Minute + 5*time.Second)
		a = put(t, cfg.Store, a.Path, expiry)
		if a.Created.Equal(a.Modified) {
			t.Fatal("fixture requires distinct creation and modification times")
		}
		r := request(http.MethodGet, "/"+a.ID)
		checkPage(t, cfg, r, 200, "artifact", artifactData(r, cfg, a))
		if _, err := cfg.Store.Delete(context.Background(), a.Path, a.OwnerID); err != nil {
			t.Fatal(err)
		}
		checkPage(t, cfg, r, 404, "notfound", pages.NoticeData{Banner: banner(r, cfg.ServicesPath)})
	}
	r := request(http.MethodGet, "/art_0000000000000000")
	checkPage(t, cfg, r, 404, "notfound", pages.NoticeData{Banner: banner(r, cfg.ServicesPath)})
}
func TestCanonicalArtifactAddresses(t *testing.T) {
	// R-OY9Z-I8L2
	cfg, _ := fixture(t)
	a := put(t, cfg.Store, "canonical.bin", time.Time{})
	for _, path := range []string{"/" + a.ID[:4] + strings.ToUpper(a.ID[4:]), "/" + a.ID[:len(a.ID)-1], "/" + a.ID + "a", "/" + a.ID + "/", "/%61" + a.ID[1:]} {
		r := request(http.MethodGet, path)
		checkPage(t, cfg, r, 404, "notfound", pages.NoticeData{Banner: banner(r, cfg.ServicesPath)})
	}
}
func TestToolsAboutAndNotFoundWithoutStore(t *testing.T) {
	// R-UHGT-FSLZ R-N5SD-OL6I R-N4KH-ATFT
	cfg, _ := fixture(t)
	cfg.Store = nil
	r := request(http.MethodGet, "/tools")
	b := banner(r, cfg.ServicesPath)
	b.Trail = []page.Level{pages.ToolsLevel}
	d := pages.ToolsData{Banner: b}
	for _, e := range tools.Catalog() {
		d.Tools = append(d.Tools, pages.ToolRow{Name: e.Name, Description: tools.FirstLine(e.Description)})
	}
	checkPage(t, cfg, r, 200, "tools", d)
	r = request(http.MethodGet, "/about")
	b = banner(r, cfg.ServicesPath)
	b.Trail = []page.Level{pages.AboutLevel}
	checkPage(t, cfg, r, 200, "about", pages.AboutData{Banner: b, Description: pages.Description})
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodDelete, "OTHER"} {
		for _, path := range []string{"/missing", "/tools/", "/about/", "/a/b", "/" + strings.ToUpper("art_abcdefabcdefabcd")} {
			r = request(method, path)
			checkPage(t, cfg, r, 404, "notfound", pages.NoticeData{Banner: banner(r, cfg.ServicesPath)})
		}
	}
}
func TestBannerUser(t *testing.T) {
	// R-N70A-2CX7
	cfg, _ := fixture(t)
	for _, published := range []bool{false, true} {
		if published {
			if err := os.WriteFile(cfg.ServicesPath, []byte(`{"services":[{"name":"auth","url":"https://auth.example.test/","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var got page.User
		cfg.Banner = func(u page.User) page.Banner { got = u; return fixedBanner(u) }
		r := request(http.MethodGet, "/about")
		w := serve(cfg, r)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		profile := "https://auth.sbx.ikigenba.dev/"
		if published {
			profile = "https://auth.example.test/"
		}
		want := page.User{Email: "reader@example.test", ProfileURL: profile, LogoutURL: strings.TrimSuffix(profile, "/") + "/logout"}
		if got != want {
			t.Fatalf("user %#v, want %#v", got, want)
		}
	}
}
func TestMethodBeforeStore(t *testing.T) {
	// R-N886-G4NW
	cfg, _ := fixture(t)
	a := put(t, cfg.Store, "existing.bin", time.Time{})
	cfg.Store = nil
	for _, path := range []string{"/", "/tools", "/about", "/" + a.ID, "/art_0000000000000000"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions, "CUSTOM"} {
			w := serve(cfg, request(method, path))
			if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || w.Body.Len() != 0 {
				t.Fatalf("%s %s: %d %v", method, path, w.Code, w.Header())
			}
		}
	}
}
func TestStoreOutage(t *testing.T) {
	// R-N9G2-TWEL
	cfg, d := fixture(t)
	a := put(t, cfg.Store, "outage.bin", time.Time{})
	d.SetFailing(true)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/", "/" + a.ID, "/art_0000000000000000"} {
			w := serve(cfg, request(method, path))
			want := store.Unreachable + "\n"
			if method == http.MethodHead {
				want = ""
			}
			if w.Code != 503 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != want {
				t.Fatalf("%s %s: %d %v %q", method, path, w.Code, w.Header(), w.Body.String())
			}
		}
	}
	for _, path := range []string{"/tools", "/about", "/missing"} {
		r := request(http.MethodGet, path)
		before := serve(cfg, r)
		d.SetFailing(false)
		after := serve(cfg, r)
		d.SetFailing(true)
		if before.Code != after.Code || !reflect.DeepEqual(before.Header(), after.Header()) || before.Body.String() != after.Body.String() {
			t.Fatalf("outage affects %s", path)
		}
	}
}
func TestHeadAndReadOnly(t *testing.T) {
	// R-NANZ-7O5A
	cfg, _ := fixture(t)
	a := put(t, cfg.Store, "unchanged.bin", time.Time{})
	before, err := cfg.Store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/tools", "/about", "/" + a.ID, "/missing", "/art_0000000000000000"} {
		get := serve(cfg, request(http.MethodGet, path))
		head := serve(cfg, request(http.MethodHead, path))
		if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
			t.Fatalf("HEAD differs for %s", path)
		}
		for _, w := range []*httptest.ResponseRecorder{get, head, serve(cfg, request(http.MethodPost, path))} {
			if len(w.Header().Values("Set-Cookie")) != 0 {
				t.Fatalf("cookie on %s", path)
			}
		}
	}
	after, err := cfg.Store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("page changed records")
	}
	_, body, err := cfg.Store.Open(context.Background(), a.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	var got bytes.Buffer
	if _, err = got.ReadFrom(body); err != nil {
		t.Fatal(err)
	}
	if got.String() != a.Path {
		t.Fatal("page changed artifact bytes")
	}
}
