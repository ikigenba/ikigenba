package pages_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/pages"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/tools"
)

func fixedBanner(u page.User) page.Banner {
	return page.Banner{Service: pages.ServiceName, Release: "sample-release", Commit: "abcdefg", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Trail: []page.Level{{Name: "discard", URL: "/discard"}}}
}
func templates(t *testing.T) *template.Template {
	t.Helper()
	ts, e := page.Templates().ParseFS(gdrive.Assets(), "*.html")
	if e != nil {
		t.Fatal(e)
	}
	return ts
}
func render(t *testing.T, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if e := templates(t).ExecuteTemplate(&b, name, data); e != nil {
		t.Fatal(e)
	}
	return b.String()
}
func req(h http.Handler, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://gdrive.example.test"+path, nil)
	r.Header.Set("X-User-Id", "alice")
	r.Header.Set("X-User-Email", "alice@example.test")
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func pageStore(t *testing.T) (*store.Store, *db.DB) {
	t.Helper()
	now := time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC)
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "gdrive.db"), Migrations: gdrive.Migrations(), Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	return store.New(d, store.Config{Rand: &sequence{n: 150}, Now: func() time.Time { return now }}), d
}
func TestTemplateSurface(t *testing.T) {
	// R-BBSQ-YUAM R-BD0N-CM1B R-L6YB-IMUK R-BGOC-HX9E
	if pages.ServiceName != "gdrive" || pages.Description != "Google Drive files, pulled into and pushed from artifacts" || pages.MinuteLayout != "2006-01-02 15:04" || pages.SecondLayout != "2006-01-02 15:04:05 UTC" || pages.ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || pages.AboutLevel != (page.Level{Name: "about", URL: "/about"}) {
		t.Fatal("constants")
	}
	t.Chdir(t.TempDir())
	s, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name string
		data any
	}{{"landing", pages.LandingData{Transfers: []pages.TransferRow{}}}, {"tools", pages.ToolsData{Tools: []pages.ToolRow{{Name: "fixture-tool", Description: "fixture-description"}}}}, {"about", pages.AboutData{Description: pages.Description}}, {"notfound", pages.NotFoundData{}}}
	statuses := []string{store.StatusQueued, store.StatusRunning, store.StatusDone, store.StatusFailed}
	reasons := []string{store.ReasonNotFound, store.ReasonForbidden, store.ReasonTooLarge, store.ReasonReconnect, store.ReasonProvider, store.ReasonArtifacts, store.ReasonInterrupted}
	for _, status := range statuses {
		cases = append(cases, struct {
			name string
			data any
		}{"landing", pages.LandingData{Transfers: []pages.TransferRow{{ID: "fixture-id", Status: status, Direction: store.DirectionPull}}}})
		for _, direction := range []string{store.DirectionPull, store.DirectionPush} {
			for _, set := range []bool{false, true} {
				v := pages.TransferView{ID: "fixture-id", Direction: direction, Status: status}
				if set {
					v.Format = "fixture-format"
					v.Finished = "fixture-finished"
					v.URL = "https://artifact.example.test"
					v.Link = "https://drive.example.test"
					v.ArtifactPage = "https://artifact.example.test/page"
				}
				cases = append(cases, struct {
					name string
					data any
				}{"transfer", pages.TransferData{Transfer: v}})
			}
		}
	}
	for _, reason := range reasons {
		cases = append(cases, struct {
			name string
			data any
		}{"transfer", pages.TransferData{Transfer: pages.TransferView{Status: store.StatusFailed, Reason: reason}}})
	}
	for _, c := range cases {
		for _, method := range []string{"GET", "HEAD"} {
			w := httptest.NewRecorder()
			s.Write(w, httptest.NewRequest(method, "/", nil), 207, c.name, c.data)
			want := render(t, c.name, c.data)
			if method == "HEAD" {
				want = ""
			}
			if w.Code != 207 || w.Body.String() != want || len(w.Header().Values("Content-Type")) != 1 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("%s %s response mismatch", method, c.name)
			}
		}
	}
}
func TestPageDataFields(t *testing.T) {
	// R-2VL3-KGCC
	b := fixedBanner(page.User{Email: "fixture-email"})
	row := pages.TransferRow{"id", "direction", "status", "reason", "name", "link", "path", "artifact", "url", 17, "started", "started-text", "finished", "finished-text"}
	view := pages.TransferView{"id", "direction", "status", "reason", "file", "name", "kind", "path", "artifact", "content-type", "format", 19, "started", "started-text", "finished", "finished-text", "url", "link"}
	cases := []struct{ got, want any }{
		{row, pages.TransferRow{ID: "id", Direction: "direction", Status: "status", Reason: "reason", Name: "name", Link: "link", Path: "path", ArtifactPage: "artifact", URL: "url", Bytes: 17, Started: "started", StartedText: "started-text", Finished: "finished", FinishedText: "finished-text"}},
		{view, pages.TransferView{ID: "id", Direction: "direction", Status: "status", Reason: "reason", File: "file", Name: "name", Kind: "kind", Path: "path", ArtifactPage: "artifact", ContentType: "content-type", Format: "format", Bytes: 19, Started: "started", StartedText: "started-text", Finished: "finished", FinishedText: "finished-text", URL: "url", Link: "link"}},
		{pages.LandingData{b, []pages.TransferRow{row}, "current-page", "next-page", "prev-page"}, pages.LandingData{Banner: b, Transfers: []pages.TransferRow{row}, Page: "current-page", Next: "next-page", Prev: "prev-page"}},
		{pages.TransferData{b, view}, pages.TransferData{Banner: b, Transfer: view}},
		{pages.ToolRow{"name", "description"}, pages.ToolRow{Name: "name", Description: "description"}},
		{pages.ToolsData{b, []pages.ToolRow{{Name: "name", Description: "description"}}}, pages.ToolsData{Banner: b, Tools: []pages.ToolRow{{Name: "name", Description: "description"}}}},
		{pages.AboutData{b, "description"}, pages.AboutData{Banner: b, Description: "description"}},
		{pages.NotFoundData{b}, pages.NotFoundData{Banner: b}},
	}
	for _, c := range cases {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Fatalf("data fields: got %#v want %#v", c.got, c.want)
		}
	}
}

func TestPagesRoutesAndData(t *testing.T) {
	// R-BHW8-VP03 R-78WB-LOZ9 R-7A47-ZGPY R-30GP-3JB4 R-BNZQ-SJPK R-7DRX-4RY1 R-BQFJ-K36Y R-BRNF-XUXN R-BVB5-365Q R-BWJ1-GXWF R-BU38-PEF1 R-BXQX-UPN4 R-BYYU-8HDT R-344E-8UJ7 R-BJ45-9GQS R-L867-WEL9 R-L9E4-A6BY
	st, d := pageStore(t)
	s, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "services.json")
	writeServices := func(base string) {
		t.Helper()
		if e := os.WriteFile(p, []byte(`{"services":[{"name":"artifacts","url":"`+base+`","description":"","socket":"","enabled":true,"mcp":true},{"name":"auth","url":"https://login.example.test/","description":"","socket":"","enabled":true,"mcp":true}]}`), 0600); e != nil {
			t.Fatal(e)
		}
	}
	writeServices("https://artifact.example.test/")
	cfg := pages.Config{Banner: fixedBanner, Pages: s, ServicesPath: p, Store: st}
	h := identity.Require(pages.Handler(cfg))
	ctx := context.Background()
	b := fixedBanner(page.User{Email: "alice@example.test", ProfileURL: "https://login.example.test/", LogoutURL: "https://login.example.test/logout"})
	b.Trail = nil
	if got := req(h, "GET", "/"); got.Code != 200 || got.Body.String() != render(t, "landing", pages.LandingData{Banner: b, Transfers: []pages.TransferRow{}}) {
		t.Fatal("empty landing")
	}
	original, e := st.Add(ctx, store.Transfer{UserID: "alice", Direction: store.DirectionPull, Status: store.StatusRunning, File: "file-fixture", Name: "fixture-name", Kind: "file", Path: "fixture/path", ContentType: "text/plain"})
	if e != nil {
		t.Fatal(e)
	}
	done, e := st.Finish(ctx, original.ID, store.Ending{Status: store.StatusDone, Bytes: 41, File: "file-fixture", Name: "fixture-name", URL: "https://artifact.example.test/files/fixture/path", Link: "https://drive.example.test/file", ArtifactID: "art_1234567890abcdef"})
	if e != nil {
		t.Fatal(e)
	}
	other, e := st.Add(ctx, store.Transfer{UserID: "bob", Direction: store.DirectionPush, Status: store.StatusQueued, Path: "private/path", Name: "private-fixture"})
	if e != nil {
		t.Fatal(e)
	}
	wantRow := pages.TransferRow{ID: done.ID, Direction: done.Direction, Status: done.Status, Reason: done.Reason, Name: done.Name, Link: done.Link, Path: done.Path, ArtifactPage: "https://artifact.example.test/" + done.ArtifactID, URL: done.URL, Bytes: done.Bytes, Started: done.Started.UTC().Format(time.RFC3339), StartedText: done.Started.UTC().Format(pages.MinuteLayout), Finished: done.Finished.UTC().Format(time.RFC3339), FinishedText: done.Finished.UTC().Format(pages.MinuteLayout)}
	got := req(h, "GET", "/")
	if got.Body.String() != render(t, "landing", pages.LandingData{Banner: b, Transfers: []pages.TransferRow{wantRow}}) {
		t.Fatal("landing row data")
	}
	tb := b
	tb.Trail = []page.Level{{Name: done.ID, URL: "/" + done.ID}}
	wantView := pages.TransferView{ID: done.ID, Direction: done.Direction, Status: done.Status, Reason: done.Reason, File: done.File, Name: done.Name, Kind: done.Kind, Path: done.Path, ArtifactPage: wantRow.ArtifactPage, ContentType: done.ContentType, Format: done.Format, Bytes: done.Bytes, Started: wantRow.Started, StartedText: done.Started.UTC().Format(pages.SecondLayout), Finished: wantRow.Finished, FinishedText: done.Finished.UTC().Format(pages.SecondLayout), URL: done.URL, Link: done.Link}
	if got := req(h, "GET", "/"+done.ID); got.Code != 200 || got.Body.String() != render(t, "transfer", pages.TransferData{Banner: tb, Transfer: wantView}) {
		t.Fatal("transfer view")
	}
	for _, base := range []string{"https://changed.example.test/", ""} {
		writeServices(base)
		wantView.ArtifactPage = ""
		if base != "" {
			wantView.ArtifactPage = base + done.ArtifactID
		}
		if req(h, "GET", "/"+done.ID).Body.String() != render(t, "transfer", pages.TransferData{Banner: tb, Transfer: wantView}) {
			t.Fatal("live services")
		}
	}
	toolRows := []pages.ToolRow{}
	for _, entry := range tools.Catalog() {
		toolRows = append(toolRows, pages.ToolRow{Name: entry.Name, Description: entry.Description})
	}
	toolsBanner := b
	toolsBanner.Trail = []page.Level{pages.ToolsLevel}
	aboutBanner := b
	aboutBanner.Trail = []page.Level{pages.AboutLevel}
	for _, c := range []struct {
		path, name string
		data       any
	}{{"/tools", "tools", pages.ToolsData{Banner: toolsBanner, Tools: toolRows}}, {"/about", "about", pages.AboutData{Banner: aboutBanner, Description: pages.Description}}} {
		if got := req(h, "GET", c.path); got.Code != 200 || got.Body.String() != render(t, c.name, c.data) {
			t.Fatal(c.path)
		}
	}
	nfBanner := fixedBanner(page.User{Email: "alice@example.test", ProfileURL: "https://login.example.test/", LogoutURL: "https://login.example.test/logout"})
	nf := render(t, "notfound", pages.NotFoundData{Banner: nfBanner})
	paths := []string{"/nope", "/xfr_0000000000000000", "/" + other.ID, "/" + done.ID[:4] + strings.ToUpper(done.ID[4:]), "/" + done.ID[:len(done.ID)-1], "/" + done.ID + "a", "/" + done.ID + "/", "/%78" + done.ID[1:]}
	for _, path := range paths {
		if got := req(h, "GET", path); got.Code != 404 || got.Body.String() != nf {
			t.Fatalf("notfound %s", path)
		}
	}
	_, e = st.Finish(ctx, other.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
	if e != nil {
		t.Fatal(e)
	}
	newer, err := st.Add(ctx, store.Transfer{UserID: "alice", Direction: store.DirectionPull, Status: store.StatusRunning, Path: "newer/path"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.Finish(ctx, newer.ID, store.Ending{Status: store.StatusDone}); err != nil {
		t.Fatal(err)
	}
	if _, e = st.Prune(ctx, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 1, 1); e != nil {
		t.Fatal(e)
	}
	if got := req(h, "GET", "/"+done.ID); got.Code != 404 || got.Body.String() != nf {
		t.Fatal("pruned")
	}
	d.SetFailing(true)
	for _, path := range []string{"/", "/tools", "/about", "/" + done.ID} {
		if got := req(h, "POST", path); got.Code != 405 || got.Header().Get("Allow") != "GET, HEAD" || got.Body.Len() != 0 {
			t.Fatal("method", path)
		}
	}
	for _, path := range []string{"/", "/" + done.ID} {
		for _, method := range []string{"GET", "HEAD"} {
			got := req(h, method, path)
			want := store.Unreachable + "\n"
			if method == "HEAD" {
				want = ""
			}
			if got.Code != 503 || got.Body.String() != want || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" || len(got.Header().Values("Content-Type")) != 1 {
				t.Fatal("unavailable", path)
			}
		}
	}
	for _, path := range []string{"/tools", "/about", "/nope", "/%78" + done.ID[1:], "/" + done.ID + "/"} {
		get := req(h, "GET", path)
		head := req(h, "HEAD", path)
		if head.Code != get.Code || head.Header().Get("Content-Type") != get.Header().Get("Content-Type") || head.Body.Len() != 0 || get.Header().Get("Set-Cookie") != "" {
			t.Fatal("head", path)
		}
	}
	if got := req(h, "DELETE", "/nope"); got.Code != 404 || got.Body.String() != nf {
		t.Fatal("unknown method")
	}
	d.SetFailing(false)
	if ts, e := st.List(ctx, "alice"); e != nil || len(ts) != 1 {
		t.Fatal("pages changed transfers")
	}
	cfg.ServicesPath = ""
	var seen page.User
	cfg.Banner = func(u page.User) page.Banner { seen = u; return fixedBanner(u) }
	r := httptest.NewRequest("GET", "https://gdrive.sbx.ikigenba.dev:443/about", nil)
	r.Header.Set("X-User-Id", "alice")
	r.Header.Set("X-User-Email", "fixture-email")
	r.Header.Set("X-Forwarded-Proto", "https")
	identity.Require(pages.Handler(cfg)).ServeHTTP(httptest.NewRecorder(), r)
	if seen != (page.User{Email: "fixture-email", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"}) {
		t.Fatal(seen)
	}
}

type sequence struct{ n byte }

func (s *sequence) Read(p []byte) (int, error) {
	for i := range p {
		s.n++
		p[i] = s.n
	}
	return len(p), nil
}

func TestTransferStateProjectionAndReadOnly(t *testing.T) {
	// R-7A47-ZGPY R-30GP-3JB4 R-7DRX-4RY1 R-L9E4-A6BY R-L867-WEL9
	_, d := pageStore(t)
	now := time.Date(2026, 10, 5, 10, 0, 7, 0, time.FixedZone("fixture", 3600))
	st := store.New(d, store.Config{Now: func() time.Time { return now }, Rand: &sequence{n: 150}})
	ctx := context.Background()
	queued, e := st.Add(ctx, store.Transfer{UserID: "alice", Direction: store.DirectionPush, Status: store.StatusQueued, File: "queued-file", Name: "queued-name", Kind: "file", Path: "queued/path", ContentType: "text/plain"})
	if e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Hour)
	failed, e := st.Add(ctx, store.Transfer{UserID: "alice", Direction: store.DirectionPull, Status: store.StatusRunning, File: "failed-file", Name: "failed-name", Kind: "document", Path: "failed/path", ContentType: "application/pdf", Format: "application/pdf"})
	if e != nil {
		t.Fatal(e)
	}
	failed, e = st.Finish(ctx, failed.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonReconnect, Bytes: 19})
	if e != nil {
		t.Fatal(e)
	}
	done, e := st.Add(ctx, store.Transfer{UserID: "alice", Direction: store.DirectionPull, Status: store.StatusRunning, File: "done-file", Name: "done-name", Kind: "drawing", Path: "done/path", ContentType: "image/png", Format: "image/png"})
	if e != nil {
		t.Fatal(e)
	}
	done, e = st.Finish(ctx, done.ID, store.Ending{Status: store.StatusDone, Bytes: 27, URL: "https://artifact.example.test/files/done/path", Link: "https://drive.example.test/done"})
	if e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Hour)
	running, e := st.Add(ctx, store.Transfer{UserID: "alice", Direction: store.DirectionPull, Status: store.StatusRunning, File: "running-file", Name: "running-name", Kind: "file", Path: "running/path", ContentType: "text/plain"})
	if e != nil {
		t.Fatal(e)
	}
	if done.ID <= failed.ID {
		t.Fatal("fixture tie order")
	}
	before, e := st.List(ctx, "alice")
	if e != nil {
		t.Fatal(e)
	}
	s, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	h := identity.Require(pages.Handler(pages.Config{Banner: fixedBanner, Pages: s, Store: st}))
	b := fixedBanner(page.User{Email: "alice@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"})
	b.Trail = nil
	rows := []pages.TransferRow{}
	for _, tf := range []store.Transfer{running, done, failed, queued} {
		row := pages.TransferRow{ID: tf.ID, Direction: tf.Direction, Status: tf.Status, Reason: tf.Reason, Name: tf.Name, Link: tf.Link, Path: tf.Path, URL: tf.URL, Bytes: tf.Bytes, Started: tf.Started.UTC().Format(time.RFC3339), StartedText: tf.Started.UTC().Format(pages.MinuteLayout)}
		view := pages.TransferView{ID: tf.ID, Direction: tf.Direction, Status: tf.Status, Reason: tf.Reason, File: tf.File, Name: tf.Name, Kind: tf.Kind, Path: tf.Path, ContentType: tf.ContentType, Format: tf.Format, Bytes: tf.Bytes, Started: tf.Started.UTC().Format(time.RFC3339), StartedText: tf.Started.UTC().Format(pages.SecondLayout), URL: tf.URL, Link: tf.Link}
		if !tf.Finished.IsZero() {
			row.Finished = tf.Finished.UTC().Format(time.RFC3339)
			row.FinishedText = tf.Finished.UTC().Format(pages.MinuteLayout)
			view.Finished = tf.Finished.UTC().Format(time.RFC3339)
			view.FinishedText = tf.Finished.UTC().Format(pages.SecondLayout)
		}
		rows = append(rows, row)
		tb := b
		tb.Trail = []page.Level{{Name: tf.ID, URL: "/" + tf.ID}}
		get := req(h, "GET", "/"+tf.ID)
		if get.Code != 200 || get.Body.String() != render(t, "transfer", pages.TransferData{Banner: tb, Transfer: view}) {
			t.Fatal("state projection", tf.Status)
		}
		head := req(h, "HEAD", "/"+tf.ID)
		if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 || get.Header().Get("Set-Cookie") != "" {
			t.Fatal("transfer HEAD", tf.Status)
		}
	}
	get := req(h, "GET", "/")
	if get.Code != 200 || get.Body.String() != render(t, "landing", pages.LandingData{Banner: b, Transfers: rows}) {
		t.Fatal("multiple rows in store order")
	}
	head := req(h, "HEAD", "/")
	if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
		t.Fatal("landing HEAD")
	}
	for _, path := range []string{"/", "/about", "/tools", "/" + done.ID, "/unknown"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			if req(h, method, path).Header().Get("Set-Cookie") != "" {
				t.Fatal("cookie", method, path)
			}
		}
	}
	after, e := st.List(ctx, "alice")
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("pages changed records", before, after, e)
	}
}

func TestLandingPaging(t *testing.T) {
	// R-30GP-3JB4 R-2Y0W-BZTQ R-32WH-V2SI R-2WSZ-Y831 R-344E-8UJ7 R-L867-WEL9
	st, d := pageStore(t)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < paging.DefaultLimit+2; i++ {
		user := "alice"
		if i == 3 {
			user = "bob"
		}
		_, err := st.Add(ctx, store.Transfer{UserID: user, Direction: store.DirectionPull, Status: store.StatusQueued, Name: "file-" + strconv.Itoa(i), Path: "path/" + strconv.Itoa(i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	all, err := st.List(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]pages.TransferRow, 0, paging.DefaultLimit)
	for _, tf := range all[:paging.DefaultLimit] {
		rows = append(rows, pages.TransferRow{ID: tf.ID, Direction: tf.Direction, Status: tf.Status, Name: tf.Name, Path: tf.Path, Started: tf.Started.UTC().Format(time.RFC3339), StartedText: tf.Started.UTC().Format(pages.MinuteLayout)})
	}
	last := all[paging.DefaultLimit-1]
	next := (paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
	b := fixedBanner(page.User{Email: "alice@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"})
	b.Trail = nil
	want := pages.LandingData{b, rows, "", next, ""}
	handler := identity.Require(pages.Handler(pages.Config{Banner: fixedBanner, Pages: set, Store: st}))
	got := req(handler, "GET", "/")
	if got.Code != 200 || got.Body.String() != render(t, "landing", want) {
		t.Fatal("first landing page")
	}
	writer := telemetry.New(telemetry.Config{Service: "gdrive", Sink: &telemetry.Capture{}, Stderr: io.Discard, Now: func() time.Time { return last.Started }, Rand: &sequence{}})
	t.Cleanup(func() { writer.Shutdown(ctx, "finished") })
	srv := mcp.NewServer(mcp.ServerConfig{Name: "gdrive", Telemetry: writer})
	tools.Register(srv, tools.Config{Store: st})
	hs := httptest.NewServer(identity.Require(srv))
	t.Cleanup(hs.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: hs.URL + "/mcp", HTTPClient: hs.Client()})
	call := func(value string) mcp.Result {
		t.Helper()
		args := map[string]string{}
		if value != "" {
			args["page"] = value
		}
		wire, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.CallTool(ctx, identity.Caller{UserID: "alice"}, "transfers", wire)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	checkWindow := func(path, value string, transfers []store.Transfer) {
		t.Helper()
		expectedRows := make([]pages.TransferRow, 0, len(transfers))
		expectedRecords := make([]tools.Record, 0, len(transfers))
		next := ""
		if len(transfers) > paging.DefaultLimit {
			transfers = transfers[:paging.DefaultLimit]
			last := transfers[len(transfers)-1]
			next = (paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
		}
		for _, tf := range transfers {
			expectedRows = append(expectedRows, pages.TransferRow{ID: tf.ID, Direction: tf.Direction, Status: tf.Status, Name: tf.Name, Path: tf.Path, Started: tf.Started.UTC().Format(time.RFC3339), StartedText: tf.Started.UTC().Format(pages.MinuteLayout)})
			expectedRecords = append(expectedRecords, tools.RecordOf(tf))
		}
		prev := ""
		if value != "" && len(transfers) != 0 {
			first := transfers[0]
			prev = (paging.Token{Tool: "transfers-before"}).Encode(first.Started.UTC().Format(time.RFC3339), first.ID)
		}
		data := pages.LandingData{b, expectedRows, value, next, prev}
		get := req(handler, "GET", path)
		if get.Code != 200 || get.Body.String() != render(t, "landing", data) {
			t.Fatal("landing window", path)
		}
		head := req(handler, "HEAD", path)
		if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
			t.Fatal("landing HEAD", path)
		}
		result := call(value)
		if result.IsError() {
			t.Fatal("transfers", result)
		}
		wire, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct{ StructuredContent tools.TransferList }
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		toolNext := ""
		if decoded.StructuredContent.Next != nil {
			toolNext = *decoded.StructuredContent.Next
		}
		if toolNext != next || !reflect.DeepEqual(decoded.StructuredContent.Transfers, expectedRecords) {
			t.Fatal("landing/tool window mismatch", path)
		}
	}
	for _, path := range []string{"/", "/?page=", "/?page=&page=bogus", "/?limit=1&user=bob&status=failed&other=bogus"} {
		checkWindow(path, "", all)
	}
	for _, suffix := range []string{"", "&page=bogus", "&limit=1&user=bob&status=failed"} {
		checkWindow("/?page="+url.QueryEscape(next)+suffix, next, all[paging.DefaultLimit:])
	}
	// Positioned tokens need not name an extant transfer: they name an ordering key.
	for _, key := range []struct {
		stamp string
		id    string
	}{
		{all[0].Started.Add(time.Hour).UTC().Format(time.RFC3339), all[0].ID},
		{all[0].Started.UTC().Format(time.RFC3339), "xfr_0000000000000000"},
		{all[0].Started.Add(-time.Hour).UTC().Format(time.RFC3339), all[0].ID},
		{all[0].Started.UTC().Format(time.RFC3339), all[1].ID},
	} {
		value := (paging.Token{Tool: "transfers"}).Encode(key.stamp, key.id)
		stamp, err := time.Parse(time.RFC3339, key.stamp)
		if err != nil {
			t.Fatal(err)
		}
		after := []store.Transfer{}
		for _, tf := range all {
			if tf.Started.Before(stamp) || tf.Started.Equal(stamp) && tf.ID < key.id {
				after = append(after, tf)
			}
		}
		checkWindow("/?page="+url.QueryEscape(value), value, after)
	}
	invalid := []string{
		"bogus", "LAST", "last/",
		next[:len(next)-1] + "!",
		(paging.Token{Tool: "list"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID),
		(paging.Token{Tool: "transfers"}).Encode("2026-10-05T09:00:00+02:00", last.ID),
		(paging.Token{Tool: "transfers"}).Encode("2026-10-05T09:00:07+00:00", last.ID),
		(paging.Token{Tool: "transfers"}).Encode("2026-10-05T09:00:07.5Z", last.ID),
		(paging.Token{Tool: "transfers"}).Encode("not-a-time", last.ID),
		(paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), "not-an-id"),
		(paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339)),
		(paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID, "extra"),
	}
	for _, stamp := range []string{"2026-10-05T09:00:00+02:00", "2026-10-05T09:00:07+00:00", "2026-10-05T09:00:07.5Z", "not-a-time"} {
		invalid = append(invalid, (paging.Token{Tool: "transfers-before"}).Encode(stamp, last.ID))
	}
	invalid = append(invalid, (paging.Token{Tool: "transfers-before"}).Encode(last.Started.UTC().Format(time.RFC3339), "not-an-id"), (paging.Token{Tool: "transfers-before"}).Encode(last.Started.UTC().Format(time.RFC3339)), (paging.Token{Tool: "transfers-before"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID, "extra"))
	withoutStore := map[string]*httptest.ResponseRecorder{}
	for _, path := range []string{"/tools", "/about", "/nowhere"} {
		withoutStore[path] = req(handler, "GET", path)
	}
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		for path, want := range withoutStore {
			got := req(handler, "GET", path)
			if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
				t.Fatal("store-independent page", failing, path)
			}
			head := req(handler, "HEAD", path)
			if head.Code != want.Code || !reflect.DeepEqual(head.Header(), want.Header()) || head.Body.Len() != 0 {
				t.Fatal("store-independent HEAD", failing, path)
			}
		}
		for _, value := range invalid {
			result := call(value)
			wire, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Content           []struct{ Type, Text string }
				StructuredContent json.RawMessage
			}
			if err := json.Unmarshal(wire, &decoded); err != nil {
				t.Fatal(err)
			}
			if !result.IsError() || len(decoded.Content) != 1 || decoded.Content[0].Type != "text" || decoded.Content[0].Text != paging.BadPage || len(decoded.StructuredContent) != 0 {
				t.Fatal("tool accepted undecodable page", value, result)
			}
			for _, method := range []string{"GET", "HEAD"} {
				want := req(handler, method, "/nowhere")
				for _, suffix := range []string{"", "&page=" + url.QueryEscape(next) + "&limit=1"} {
					got := req(handler, method, "/?page="+url.QueryEscape(value)+suffix)
					if got.Code != 404 || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
						t.Fatal("undecodable page", failing, method, value)
					}
				}
			}
		}
		if got := req(handler, "POST", "/?page=bogus"); got.Code != 405 || got.Header().Get("Allow") != "GET, HEAD" || got.Body.Len() != 0 {
			t.Fatal("method before page decoding")
		}
		if failing {
			for _, path := range []string{"/", "/?page=&page=bogus", "/?page=" + url.QueryEscape(next), "/?page=last", "/?page=" + url.QueryEscape((paging.Token{Tool: "transfers-before"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)), "/" + last.ID} {
				for _, method := range []string{"GET", "HEAD"} {
					got := req(handler, method, path)
					body := store.Unreachable + "\n"
					if method == "HEAD" {
						body = ""
					}
					if got.Code != 503 || len(got.Header().Values("Content-Type")) != 1 || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" || got.Body.String() != body {
						t.Fatal("decodable page unavailable", method, path)
					}
				}
			}
		}
	}
}

// counterRandom gives each transfer a distinct deterministic id.
type counterRandom struct{ n uint64 }

func (r *counterRandom) Read(p []byte) (int, error) {
	r.n++
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], r.n)
	copy(p, encoded[:])
	return len(p), nil
}

func TestLandingBackwardAndLast(t *testing.T) {
	// R-2Z8S-PRKF R-31OL-HB1T R-35CA-MM9W R-36K7-0E0L R-30GP-3JB4 R-2WSZ-Y831 R-L867-WEL9
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 3*paging.DefaultLimit + 7} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			_, d := pageStore(t)
			now := time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC)
			st := store.New(d, store.Config{Now: func() time.Time { return now }, Rand: &counterRandom{}})
			ctx := context.Background()
			for i := 0; i < count+4; i++ {
				user := "alice"
				if i >= count {
					user = "bob"
				}
				now = time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC).Add(time.Duration(i/3) * time.Second)
				if _, e := st.Add(ctx, store.Transfer{UserID: user, Direction: store.DirectionPull, Status: store.StatusQueued, Name: "file-" + strconv.Itoa(i), Path: "path/" + strconv.Itoa(i)}); e != nil {
					t.Fatal(e)
				}
			}
			all, e := st.List(ctx, "alice")
			if e != nil {
				t.Fatal(e)
			}
			set, e := pages.Load()
			if e != nil {
				t.Fatal(e)
			}
			h := identity.Require(pages.Handler(pages.Config{Banner: fixedBanner, Pages: set, Store: st}))
			b := fixedBanner(page.User{Email: "alice@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"})
			b.Trail = nil
			token := func(tool string, tf store.Transfer) string {
				return (paging.Token{Tool: tool}).Encode(tf.Started.UTC().Format(time.RFC3339), tf.ID)
			}
			check := func(value string, window []store.Transfer, next, prev string) {
				t.Helper()
				rows := make([]pages.TransferRow, 0, len(window))
				for _, tf := range window {
					rows = append(rows, pages.TransferRow{ID: tf.ID, Direction: tf.Direction, Status: tf.Status, Name: tf.Name, Path: tf.Path, Started: tf.Started.UTC().Format(time.RFC3339), StartedText: tf.Started.UTC().Format(pages.MinuteLayout)})
				}
				path := "/?page=" + url.QueryEscape(value) + "&page=bogus&limit=1&user=bob"
				get := req(h, "GET", path)
				if get.Code != 200 || get.Body.String() != render(t, "landing", pages.LandingData{b, rows, value, next, prev}) {
					t.Fatalf("window %q: status %d", value, get.Code)
				}
				head := req(h, "HEAD", path)
				if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
					t.Fatal("window HEAD", value)
				}
			}
			redirect := func(value string) {
				t.Helper()
				get := req(h, "GET", "/?page="+url.QueryEscape(value)+"&limit=1&user=bob")
				if get.Code != 303 || len(get.Header().Values("Location")) != 1 || get.Header().Get("Location") != "/" || get.Body.Len() != 0 {
					t.Fatal("underflow", value, get.Code, get.Header(), get.Body.String())
				}
			}
			if count == 0 {
				check("", all, "", "")
			}
			values := []string{}
			windows := [][]store.Transfer{}
			value := ""
			for start := 0; start < len(all); start += paging.DefaultLimit {
				end := min(start+paging.DefaultLimit, len(all))
				window := all[start:end]
				next, prev := "", ""
				if end < len(all) {
					next = token("transfers", window[len(window)-1])
				}
				if start != 0 {
					prev = token("transfers-before", window[0])
				}
				check(value, window, next, prev)
				values = append(values, value)
				windows = append(windows, window)
				value = next
			}
			if len(windows) > 1 {
				value = values[len(values)-1]
				for j := len(windows) - 1; j > 0; j-- {
					window := windows[j]
					next := ""
					if j != len(windows)-1 {
						next = token("transfers", window[len(window)-1])
					}
					prev := token("transfers-before", window[0])
					check(value, window, next, prev)
					value = prev
				}
				redirect(value)
			}
			if count <= paging.DefaultLimit {
				redirect("last")
			} else {
				window := all[count-paging.DefaultLimit:]
				prev := token("transfers-before", window[0])
				check("last", window, "", prev)
				for end := count - paging.DefaultLimit; end > paging.DefaultLimit; end -= paging.DefaultLimit {
					window = all[end-paging.DefaultLimit : end]
					value = prev
					prev = token("transfers-before", window[0])
					check(value, window, token("transfers", window[len(window)-1]), prev)
				}
				redirect(prev)
			}
			// A key that names no record still positions the backward window.
			key := store.Transfer{Started: now.Add(time.Hour), ID: "xfr_ffffffffffffffff"}
			redirect(token("transfers-before", key))
			if count > paging.DefaultLimit+1 {
				key = all[len(all)-1]
				key.ID = "xfr_ffffffffffffffff"
				before := []store.Transfer{}
				for _, tf := range all {
					if tf.Started.After(key.Started) || tf.Started.Equal(key.Started) && tf.ID > key.ID {
						before = append(before, tf)
					}
				}
				value = token("transfers-before", key)
				if len(before) <= paging.DefaultLimit {
					redirect(value)
				} else {
					window := before[len(before)-paging.DefaultLimit:]
					check(value, window, token("transfers", window[len(window)-1]), token("transfers-before", window[0]))
				}
			}
		})
	}
}
