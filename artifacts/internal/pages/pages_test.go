package pages_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	artifacts "github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/pages"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/tools"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

type countingRandom struct{ next uint64 }

func (r *countingRandom) Read(b []byte) (int, error) {
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], r.next)
	for i := range b {
		b[i] = encoded[i%len(encoded)]
	}
	r.next++
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
	// R-NQFY-7UB1 R-MSDH-H40V R-Z3BV-BR89 R-MW16-MF8Y R-MX93-06ZN
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
		{"landing", pages.LandingData{Banner: b, Artifacts: []pages.ArtifactRow{row}, TotalBytes: 74, Page: (paging.Token{Tool: "list"}).Encode("before-row"), Prev: (paging.Token{Tool: "list-before"}).Encode(row.Path), Next: (paging.Token{Tool: "list"}).Encode(row.Path)}},
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
	// R-Z4JR-PIYY
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

func TestLandingWindowsMatchList(t *testing.T) {
	// R-Z4JR-PIYY R-Z0W2-K7QV
	t.Setenv(services.Variable, "")
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := func() time.Time { return now }
	cfg, _ := fixtureWithClock(t, clock)
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: &telemetry.Capture{}, Now: clock, Rand: &countingRandom{}, Stderr: io.Discard})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	srv := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w})
	tools.Register(srv, tools.Config{Store: cfg.Store, Telemetry: w, Now: clock})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), "https://artifacts.example.test")))
	})
	server := httptest.NewServer(identity.Require(h))
	t.Cleanup(server.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL})
	caller := identity.Caller{UserID: "reader-id", Email: "reader@example.test"}
	r := request(http.MethodGet, "/")
	b := banner(r, cfg.ServicesPath)
	b.Trail = nil
	var all []store.Artifact
	check := func(value string) string {
		t.Helper()
		sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
		data := pages.LandingData{Banner: b, Page: value}
		r.URL.RawQuery = url.Values{"page": {value}}.Encode()
		for _, a := range all {
			data.TotalBytes += a.Bytes
		}
		first := all
		if value != "" {
			var position string
			if err := (paging.Token{Tool: "list"}).Decode(value, &position); err != nil {
				t.Fatal(err)
			}
			start := sort.Search(len(all), func(i int) bool { return all[i].Path > position })
			first = all[start:]
		}
		if len(first) > paging.DefaultLimit {
			first = first[:paging.DefaultLimit]
			data.Next = (paging.Token{Tool: "list"}).Encode(first[len(first)-1].Path)
		}
		if value != "" && len(first) > 0 {
			data.Prev = (paging.Token{Tool: "list-before"}).Encode(first[0].Path)
		}
		for _, a := range first {
			row := pages.ArtifactRow{ID: a.ID, Path: a.Path, Bytes: a.Bytes, ContentType: a.ContentType, Owner: a.OwnerEmail, Modified: a.Modified.UTC().Format(time.RFC3339), ModifiedText: a.Modified.UTC().Format("2006-01-02 15:04")}
			if !a.Expires.IsZero() {
				row.Expires = a.Expires.UTC().Format(time.RFC3339)
				row.ExpiresText = a.Expires.UTC().Format("2006-01-02 15:04")
			}
			data.Artifacts = append(data.Artifacts, row)
		}
		checkPage(t, cfg, r, http.StatusOK, "landing", data)
		in := tools.ListArgs{}
		if value != "" {
			in.Page = &value
		}
		args, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.CallTool(context.Background(), caller, "list", args)
		if err != nil || result.IsError() {
			t.Fatalf("list: %v, result %v", err, result)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Structured tools.ArtifactList `json:"structuredContent"`
		}
		if err := json.Unmarshal(encoded, &reply); err != nil {
			t.Fatal(err)
		}
		if len(reply.Structured.Artifacts) != len(data.Artifacts) {
			t.Fatalf("list has %d rows; page has %d", len(reply.Structured.Artifacts), len(data.Artifacts))
		}
		for i, row := range data.Artifacts {
			if reply.Structured.Artifacts[i].ID != row.ID {
				t.Fatalf("list row %d id differs from page", i)
			}
		}
		if data.Next == "" {
			if reply.Structured.Next != nil {
				t.Fatal("list has next for a complete first page")
			}
		} else if reply.Structured.Next == nil || *reply.Structured.Next != data.Next {
			t.Fatal("list next differs from page")
		}
		return data.Next
	}
	check("")
	paths := []string{"Zeta.bin", "alpha.bin"}
	for i := len(paths); i < paging.DefaultLimit; i++ {
		paths = append(paths, fmt.Sprintf("middle-%03d.bin", i))
	}
	for i := len(paths) - 1; i >= 0; i-- {
		expiry := time.Time{}
		if i%2 == 0 {
			expiry = now.Add(24 * time.Hour)
		}
		draft := store.Draft{Path: paths[i], OwnerID: fmt.Sprintf("owner-%d", i%2), OwnerEmail: fmt.Sprintf("owner-%d@example.test", i%2), ContentType: "application/test", Max: 100, Expires: expiry}
		a, _, err := cfg.Store.Put(context.Background(), draft, strings.NewReader(paths[i]))
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, a)
	}
	check("")
	all = append(all, put(t, cfg.Store, "zz-extra.bin", time.Time{}))
	check("")
	for _, position := range []string{"", "middle-010.bin", "middle-010.bina", "zz-extra.bin", "zzzz"} {
		check((paging.Token{Tool: "list"}).Encode(position))
	}
	for i := 0; i < paging.DefaultLimit; i++ {
		all = append(all, put(t, cfg.Store, fmt.Sprintf("later-%03d.bin", i), time.Time{}))
	}
	value := ""
	for {
		value = check(value)
		if value == "" {
			break
		}
	}
	// The token remains a valid position after its artifact is deleted.
	position := all[paging.DefaultLimit-1].Path
	if _, err := cfg.Store.Delete(context.Background(), position, all[paging.DefaultLimit-1].OwnerID); err != nil {
		t.Fatal(err)
	}
	all = append(all[:paging.DefaultLimit-1], all[paging.DefaultLimit:]...)
	check((paging.Token{Tool: "list"}).Encode(position))
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
	// R-Z9FD-8LXQ
	cfg, d := fixture(t)
	a := put(t, cfg.Store, "outage.bin", time.Time{})
	d.SetFailing(true)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/", "/?page=" + url.QueryEscape((paging.Token{Tool: "list"}).Encode("absent")), "/?page=last", "/?page=" + url.QueryEscape((paging.Token{Tool: "list-before"}).Encode("absent")), "/" + a.ID, "/art_0000000000000000"} {
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
	for _, path := range []string{"/tools", "/about", "/missing", "/?page=bogus"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := request(method, path)
			before := serve(cfg, r)
			d.SetFailing(false)
			after := serve(cfg, r)
			d.SetFailing(true)
			if before.Code != after.Code || !reflect.DeepEqual(before.Header(), after.Header()) || before.Body.String() != after.Body.String() {
				t.Fatalf("outage affects %s", path)
			}
		}
	}
}
func TestHeadAndReadOnly(t *testing.T) {
	// R-NANZ-7O5A
	cfg, _ := fixture(t)
	a := put(t, cfg.Store, "unchanged.bin", time.Time{})
	before, err := cfg.Store.List(context.Background(), nil, paging.DefaultLimit+1)
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
	after, err := cfg.Store.List(context.Background(), nil, paging.DefaultLimit+1)
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

func TestLandingPageValueAndInvalidTokens(t *testing.T) {
	// R-YZO6-6G06 R-Z5RO-3APN
	cfg, d := fixture(t)
	put(t, cfg.Store, "visible.bin", time.Time{})
	invalid := []string{"bogus", (paging.Token{Tool: "list_widgets"}).Encode("visible.bin"), (paging.Token{Tool: "list"}).Encode(int64(123)), (paging.Token{Tool: "list-before"}).Encode(int64(123)), "Last", "last "}
	valid := (paging.Token{Tool: "list"}).Encode("visible.bin")
	invalid = append(invalid, valid[:len(valid)-1])
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		for _, value := range invalid {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				r := request(method, "/?"+url.Values{"page": {value, valid}}.Encode())
				checkPage(t, cfg, r, 404, "notfound", pages.NoticeData{Banner: banner(r, cfg.ServicesPath)})
				// A nil store makes any attempted read observable as a panic.
				withoutStore := cfg
				withoutStore.Store = nil
				checkPage(t, withoutStore, r, 404, "notfound", pages.NoticeData{Banner: banner(r, cfg.ServicesPath)})
			}
		}
	}
	d.SetFailing(false)
	b := banner(request(http.MethodGet, "/"), cfg.ServicesPath)
	b.Trail = nil
	for _, query := range []string{"page=" + url.QueryEscape(valid), url.Values{"page": {valid, "bogus"}}.Encode()} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			checkPage(t, cfg, request(method, "/?"+query), 200, "landing", pages.LandingData{Banner: b, TotalBytes: int64(len("visible.bin")), Page: valid})
		}
	}
}

func TestPageQueryInvariance(t *testing.T) {
	// R-Z6ZK-H2GC
	cfg, d := fixture(t)
	a := put(t, cfg.Store, "query.bin", time.Time{})
	token := (paging.Token{Tool: "list"}).Encode("query.bin")
	before := (paging.Token{Tool: "list-before"}).Encode("query.bin")
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		for _, path := range []string{"/", "/tools", "/about", "/missing", "/" + a.ID, "/%61" + a.ID[1:]} {
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions, "CUSTOM"} {
				pairs := [][2]string{
					{"", "limit=1&other=chosen"},
					{"page=", "page=&page=bogus&limit=999"},
					{"page=" + url.QueryEscape(token), "page=" + url.QueryEscape(token) + "&page=bogus&limit=1&other=x"},
					{"page=bogus", "page=bogus&limit=2"},
					{"page=last", "page=last&page=bogus&limit=1"},
					{"page=" + url.QueryEscape(before), "page=" + url.QueryEscape(before) + "&page=last&limit=1"},
				}
				if path != "/" || (method != http.MethodGet && method != http.MethodHead) {
					pairs = append(pairs, [2]string{"page=bogus", "page=" + url.QueryEscape(token)})
				}
				for _, pair := range pairs {
					left := serve(cfg, request(method, path+"?"+pair[0]))
					right := serve(cfg, request(method, path+"?"+pair[1]))
					if left.Code != right.Code || !reflect.DeepEqual(left.Header(), right.Header()) || left.Body.String() != right.Body.String() {
						t.Fatalf("query affects %s %s (failing=%t): %q vs %q", method, path, failing, pair[0], pair[1])
					}
				}
			}
		}
	}
}

// R-Z23Y-XZHK R-ZAN9-MDOF R-ZBV6-05F4 R-IWES-N358 R-YZO6-6G06 R-Z4JR-PIYY
func TestBackwardWindowsAndLast(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1, 3 * paging.DefaultLimit, 3*paging.DefaultLimit + 7} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			cfg, _ := fixture(t)
			var all []store.Artifact
			for i := count - 1; i >= 0; i-- {
				path := fmt.Sprintf("path-%04d.bin", i)
				expiry := time.Time{}
				if i%2 == 0 {
					expiry = time.Date(2035, 2, 3, 4, 5, 6, 0, time.UTC)
				}
				a, _, err := cfg.Store.Put(context.Background(), store.Draft{Path: path, OwnerID: fmt.Sprintf("owner-%d", i%2), OwnerEmail: fmt.Sprintf("owner-%d@example.test", i%2), ContentType: "application/test", Expires: expiry, Max: 100}, strings.NewReader(path))
				if err != nil {
					t.Fatal(err)
				}
				all = append(all, a)
			}
			sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
			check := func(value string) (pages.LandingData, bool) {
				t.Helper()
				r := request(http.MethodGet, "/?"+url.Values{"page": {value}}.Encode())
				b := banner(r, cfg.ServicesPath)
				b.Trail = nil
				data := pages.LandingData{Banner: b, Page: value}
				for _, a := range all {
					data.TotalBytes += a.Bytes
				}
				selected := all
				backward := value == "last"
				if value != "" && value != "last" {
					var key string
					if err := (paging.Token{Tool: "list"}).Decode(value, &key); err == nil {
						selected = all[sort.Search(len(all), func(i int) bool { return all[i].Path > key }):]
					} else {
						if err := (paging.Token{Tool: "list-before"}).Decode(value, &key); err != nil {
							t.Fatal(err)
						}
						backward = true
						selected = all[:sort.Search(len(all), func(i int) bool { return all[i].Path >= key })]
					}
				}
				if backward && len(selected) <= paging.DefaultLimit {
					for _, method := range []string{http.MethodGet, http.MethodHead} {
						r.Method = method
						got := serve(cfg, r)
						if got.Code != http.StatusSeeOther || !reflect.DeepEqual(got.Header().Values("Location"), []string{"/"}) || got.Body.Len() != 0 {
							t.Fatalf("underflow %q: %d %v %q", value, got.Code, got.Header(), got.Body.String())
						}
					}
					return data, true
				}
				more := len(selected) > paging.DefaultLimit
				if more {
					if backward {
						selected = selected[len(selected)-paging.DefaultLimit:]
					} else {
						selected = selected[:paging.DefaultLimit]
					}
				}
				if len(selected) > 0 {
					if value != "" {
						data.Prev = (paging.Token{Tool: "list-before"}).Encode(selected[0].Path)
					}
					if backward && value != "last" || !backward && more {
						data.Next = (paging.Token{Tool: "list"}).Encode(selected[len(selected)-1].Path)
					}
				}
				for _, a := range selected {
					row := pages.ArtifactRow{ID: a.ID, Path: a.Path, Bytes: a.Bytes, ContentType: a.ContentType, Owner: a.OwnerEmail, Modified: a.Modified.UTC().Format(time.RFC3339), ModifiedText: a.Modified.UTC().Format("2006-01-02 15:04")}
					if !a.Expires.IsZero() {
						row.Expires = a.Expires.UTC().Format(time.RFC3339)
						row.ExpiresText = a.Expires.UTC().Format("2006-01-02 15:04")
					}
					data.Artifacts = append(data.Artifacts, row)
				}
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					r.Method = method
					checkPage(t, cfg, r, http.StatusOK, "landing", data)
				}
				return data, false
			}
			last, _ := check("last")
			for _, key := range []string{"", "path-0000.bin", fmt.Sprintf("path-%04d.bina", paging.DefaultLimit), "zz-missing"} {
				// The two tool names over the same key select opposite sides.
				check((paging.Token{Tool: "list"}).Encode(key))
				check((paging.Token{Tool: "list-before"}).Encode(key))
			}
			var forward []pages.LandingData
			value := ""
			for {
				data, underflow := check(value)
				if underflow {
					t.Fatal("forward window underflowed")
				}
				forward = append(forward, data)
				if data.Next == "" {
					break
				}
				value = data.Next
			}
			if count <= paging.DefaultLimit {
				return
			}
			if last.Page != "last" || last.Next != "" || last.Prev == "" || len(last.Artifacts) != paging.DefaultLimit {
				t.Fatal("last window navigation", last)
			}
			if count%paging.DefaultLimit == 0 && !reflect.DeepEqual(last.Artifacts, forward[len(forward)-1].Artifacts) {
				t.Fatal("last differs from final full forward window")
			}
			value = forward[len(forward)-1].Page
			for i := len(forward) - 1; i > 0; i-- {
				data, underflow := check(value)
				if underflow || !reflect.DeepEqual(data.Artifacts, forward[i].Artifacts) {
					t.Fatalf("backward window %d differs from forward window", i)
				}
				value = data.Prev
			}
			if _, underflow := check(value); !underflow {
				t.Fatal("backward walk did not finish at first-window redirect")
			}
		})
	}
}
