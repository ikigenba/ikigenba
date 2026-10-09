package pages_test

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

var fixedTime = time.Date(2026, 10, 5, 9, 31, 40, 0, time.FixedZone("test", 3600))
var fixedBanner = page.Banner{Service: pages.ServiceName, Version: "test"}

func templates(t *testing.T) *template.Template {
	t.Helper()
	v, e := page.Templates().ParseFS(scripts.Assets(), "*.html")
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func rendered(t *testing.T, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if e := templates(t).ExecuteTemplate(&b, name, data); e != nil {
		t.Fatal(e)
	}
	return b.String()
}
func requireEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}

// R-VKHH-HQ5X R-OIC3-1CB3 R-VO56-N1E0 R-VPD3-0T4P R-VQKZ-EKVE R-VRSV-SCM3 R-VT0S-64CS R-VU8O-JW3H R-70GP-VANL R-VXWD-P7BK R-VZ4A-2Z29 R-W0C6-GQSY R-7TOP-X110 R-W2RZ-8AAC R-W3ZV-M211 R-W57R-ZTRQ R-W6FO-DLIF R-W7NK-RD94 R-WBB9-WOH7 R-WCJ6-AG7W R-WDR2-O7YL R-WEYZ-1ZPA R-WHER-TJ6O
func TestTemplateSetAndData(t *testing.T) {
	const serviceName = pages.ServiceName
	const description = pages.Description
	var load func() (*pages.Set, error)
	var write func(*pages.Set, http.ResponseWriter, *http.Request, int, string, any)
	load = pages.Load
	write = (*pages.Set).Write
	_ = load
	_ = write
	requireEqual(t, serviceName, "scripts")
	if description == "" || strings.ContainsAny(description, "\n\r\"\\") {
		t.Fatal("invalid description")
	}
	t.Chdir(t.TempDir())
	set, e := pages.Load()
	if e != nil || set == nil {
		t.Fatalf("load: %v", e)
	}
	repo := pages.Repo{ID: "repo", Name: "Repository <name>"}
	row := pages.RunRow{ID: "run", URL: "/alpha/runs/run/", Status: pages.WordExited + "0", Kind: pages.KindOK, Commit: "abcdef0", Started: "start", StartedAt: "stamp", Duration: "12" + pages.UnitSecond, Exit: "0"}
	card := pages.RunCard{ID: "run", URL: row.URL, Status: row.Status, Kind: row.Kind, Running: false, Commit: "abcdef012345", Ref: "main", Started: "start", StartedAt: "stamp", Finished: "finish", FinishedAt: "endstamp", Duration: "12" + pages.UnitSecond, Trigger: "manual", User: "user", Request: "request", StdoutSize: "1" + pages.UnitByte, StderrSize: "0" + pages.UnitByte, Truncated: true, Failure: &pages.Failure{Title: "Fail", Reason: "Reason <text>"}, FilesGone: false}
	file := &pages.FileText{Size: "1" + pages.UnitByte, Text: "<text>&\n", URL: "/file"}
	cases := []struct {
		name string
		data any
	}{
		{"landing", pages.LandingData{Banner: fixedBanner, Scripts: []pages.ScriptRow{{Name: "alpha", URL: "/alpha/", Repo: repo, Ref: "main", LastRun: &row}}}},
		{"script", pages.ScriptData{Banner: fixedBanner, Script: pages.ScriptCard{ID: "script", Name: "alpha", Repo: repo, Ref: "main", Created: "created", CreatedAt: "stamp", RunsKept: 1, KeepNewest: 10, KeepDays: 30}, Runs: []pages.RunRow{row}}},
		{"run", pages.RunData{Banner: fixedBanner, Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: card, Input: file, Stdout: file, Stderr: file, Files: []pages.FileRow{{Path: "file", Size: "1" + pages.UnitByte, URL: "/file"}}}},
		{"about", pages.AboutData{Banner: fixedBanner, Description: pages.Description}},
		{"notfound", pages.NoticeData{Banner: fixedBanner}},
		{"unavailable", pages.NoticeData{Banner: fixedBanner}},
	}
	for _, c := range cases {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, code := range []int{200, 404, 503, 599} {
				w := httptest.NewRecorder()
				w.Header().Add("X-Keep", "one")
				w.Header().Add("X-Keep", "two")
				w.Header().Add("Content-Type", "old")
				set.Write(w, httptest.NewRequest(method, "/", nil), code, c.name, c.data)
				requireEqual(t, w.Code, code)
				requireEqual(t, w.Header(), http.Header{"Content-Type": {"text/html; charset=utf-8"}, "X-Keep": {"one", "two"}})
				want := ""
				if method != "HEAD" {
					want = rendered(t, c.name, c.data)
				}
				requireEqual(t, w.Body.String(), want)
			}
		}
	}
}

type fixture struct {
	t           *testing.T
	cfg         pages.Config
	db          *db.DB
	handler     http.Handler
	sc          store.Script
	root, trace string
	mu          sync.Mutex
	users       []page.User
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, root: dir, trace: filepath.Join(dir, "trace")}
	gitPath, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	env := []string{"HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "PATH=" + filepath.Dir(gitPath), "GIT_TRACE=" + f.trace}
	g, e := git.Find(filepath.Dir(gitPath), func() []string { return append([]string{}, env...) })
	if e != nil {
		t.Fatal(e)
	}
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "catalog.db"), Migrations: scripts.Migrations(), Now: func() time.Time { return fixedTime }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	f.db = d
	s := store.New(d, store.Config{Now: func() time.Time { return fixedTime }, Rand: bytes.NewReader(bytes.Repeat([]byte{1, 2, 3, 4, 5, 6, 7, 8, 8, 7, 6, 5, 4, 3, 2, 1, 9, 9, 9, 9, 9, 9, 9, 9}, 20))})
	lim := limits.New(settings.Settings{OperationSeconds: 30, TreeMaxBytes: 10000}, limits.Clock{After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	src := source.New(source.Config{Repos: filepath.Join(dir, "repos"), Git: g, Limits: lim})
	repo := "rep_0102030405060708"
	repoDir := src.RepoDir(repo)
	if e := os.MkdirAll(filepath.Dir(repoDir), 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(t.Context(), gitPath, "init", "--bare", "--initial-branch=main", repoDir)
	cmd.Env = env
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init: %v %s", e, out)
	}
	cmd = exec.CommandContext(t.Context(), gitPath, "config", "--file", filepath.Join(repoDir, "config"), "ikigenba.name", "Repository")
	cmd.Env = env
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git config: %v %s", e, out)
	}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: fixedBanner.Version, Sink: &telemetry.Capture{}, Stderr: io.Discard, Now: func() time.Time { return fixedTime }, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 1024)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "done") })
	core := runs.New(runs.Config{MaxActive: 100, MaxQueued: 100, Store: s, Source: src, Writer: w, Runs: filepath.Join(dir, "runs"), ScriptSeconds: 1, OutputMaxBytes: 1, KeepDays: 30, KeepCount: 10, Now: func() time.Time { return fixedTime }, ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8})})
	set, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	f.cfg = pages.Config{Banner: func(u page.User) page.Banner {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.users = append(f.users, u)
		b := fixedBanner
		b.Email = u.Email
		b.ProfileURL = u.ProfileURL
		b.LogoutURL = u.LogoutURL
		return b
	}, Pages: set, Store: s, Source: src, Runs: core, KeepDays: 30, KeepCount: 10, TreeMaxBytes: 1229, OperationSeconds: 12}
	f.handler = identity.Require(pages.Handler(f.cfg))
	return f
}
func (f *fixture) create(t *testing.T, owner, name string) store.Script {
	t.Helper()
	s, e := f.cfg.Store.Create(context.Background(), store.Draft{Owner: owner, Name: name, Repo: "rep_0102030405060708", Ref: "main"})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func (f *fixture) request(ctx context.Context, method, path, owner string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "scripts.sbx.example:443"
	r.Header.Set("X-User-Id", owner)
	r.Header.Set("X-User-Email", "person@example.com")
	r.Header.Set("X-Forwarded-Proto", "https")
	if ctx != nil {
		r = r.WithContext(ctx)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if values, present := w.Header()["Set-Cookie"]; present {
		f.t.Errorf("%s %s carries Set-Cookie: %q", method, path, values)
	}
	if w.Code == http.StatusOK {
		requireEqual(f.t, w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"})
	}
	return w
}
func (f *fixture) add(t *testing.T, sc store.Script, i int, status, reason string, seconds int64, exit int) store.Run {
	t.Helper()
	u := store.Run{ID: fmt.Sprintf("run_%016x", i), Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: sc.Owner, RequestID: strings.Repeat("b", 32), Trigger: store.TriggerManual, Status: store.StatusRunning, Started: fixedTime.Add(time.Duration(i) * time.Second)}
	if status == store.StatusQueued {
		u.Status = status
	}
	if status == store.StatusFailed {
		u.Status = status
		u.Reason = reason
		u.SHA = ""
		u.Finished = u.Started
	}
	u, e := f.cfg.Store.AddRun(context.Background(), u)
	if e != nil {
		t.Fatal(e)
	}
	if status != store.StatusQueued && status != store.StatusRunning && status != store.StatusFailed {
		u, e = f.cfg.Store.FinishRun(context.Background(), u.ID, store.Ending{Status: status, ExitCode: exit, Finished: u.Started.Add(time.Duration(seconds) * time.Second), StdoutBytes: 1229, StderrBytes: 69, Truncated: true})
		if e != nil {
			t.Fatal(e)
		}
	}
	return u
}
func writeFile(t *testing.T, p, text string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}

// R-W8VH-54ZT R-WA3D-IWQI R-NY3P-CSH2 R-WJUK-L2O2 R-WL2G-YUER R-WPY2-HXDJ R-72WI-MU4Z R-WW1K-ES30 R-WX9G-SJTP R-WZP9-K3B3 R-0KOO-W7F0 R-X252-BMSH R-NXN9-D70F R-X5SR-GY0K R-X70N-UPR9 R-XMU3-C5LL R-0N4H-NQWE R-0PKA-FADS R-0S03-6TV6 R-XBW9-DSQ1 R-XD45-RKGQ R-XGRU-WVOT
func TestPageRoutesAndData(t *testing.T) {
	var handler func(pages.Config) http.Handler
	f := setup(t)
	handler = pages.Handler
	_ = handler
	f.sc = f.create(t, "owner", "alpha")
	other := f.create(t, "other", "private")
	second := f.create(t, "owner", "beta")
	u := f.add(t, f.sc, 1, store.StatusExited, "", 12, 0)
	foreign := f.add(t, other, 2, store.StatusRunning, "", 0, 0)
	sibling := f.add(t, second, 3, store.StatusRunning, "", 0, 0)
	row := pages.RunRow{ID: u.ID, URL: "/alpha/runs/" + u.ID + "/", Status: pages.WordExited + "0", Kind: pages.KindOK, Commit: "aaaaaaa", Started: u.Started.UTC().Format(pages.MinuteLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: "12" + pages.UnitSecond, Exit: "0"}
	b := fixedBanner
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	expectedLanding := pages.LandingData{Banner: b, Scripts: []pages.ScriptRow{{Name: "alpha", URL: "/alpha/", Repo: pages.Repo{ID: f.sc.Repo, Name: "Repository"}, Ref: "main", LastRun: &row}, {Name: "beta", URL: "/beta/", Repo: pages.Repo{ID: second.Repo, Name: "Repository"}, Ref: "main", LastRun: &pages.RunRow{ID: sibling.ID, URL: "/beta/runs/" + sibling.ID + "/", Status: pages.WordRunning, Kind: pages.KindInfo, Commit: "aaaaaaa", Started: sibling.Started.UTC().Format(pages.MinuteLayout), StartedAt: sibling.Started.UTC().Format(time.RFC3339)}}}}
	w := f.request(context.Background(), "GET", "/?query=yes", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "landing", expectedLanding))
	expectedScript := pages.ScriptData{Banner: b, Script: pages.ScriptCard{ID: f.sc.ID, Name: "alpha", Repo: pages.Repo{ID: f.sc.Repo, Name: "Repository"}, Ref: "main", Created: f.sc.Created.UTC().Format(pages.CardLayout), CreatedAt: f.sc.Created.UTC().Format(time.RFC3339), RunsKept: 1, KeepNewest: 10, KeepDays: 30}, Runs: []pages.RunRow{row}}
	w = f.request(context.Background(), "GET", "/alpha/", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "script", expectedScript))
	w = f.request(context.Background(), "GET", "/about?x=1", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "about", pages.AboutData{Banner: b, Description: pages.Description}))
	f.mu.Lock()
	for _, user := range f.users {
		requireEqual(t, user, page.User{Email: b.Email, ProfileURL: b.ProfileURL, LogoutURL: b.LogoutURL})
	}
	f.users = nil
	f.mu.Unlock()
	for _, base := range []string{"/alpha", "/alpha/runs/" + u.ID} {
		for _, query := range []string{"", "?a=%2F&b=2"} {
			get := f.request(context.Background(), "GET", base+query, "owner")
			head := f.request(context.Background(), "HEAD", base+query, "owner")
			requireEqual(t, get.Code, 301)
			requireEqual(t, get.Header().Values("Location"), []string{base + "/" + query})
			requireEqual(t, head.Code, get.Code)
			requireEqual(t, head.Header(), get.Header())
			requireEqual(t, head.Body.Len(), 0)
		}
	}
	for _, c := range []struct{ encoded, canonical string }{
		{"/%61lpha/", "/alpha/"},
		{"/alpha/%72uns/" + u.ID + "/", "/alpha/runs/" + u.ID + "/"},
		{"/alpha/runs/%72" + u.ID[1:] + "/", "/alpha/runs/" + u.ID + "/"},
	} {
		canonical := f.request(context.Background(), "GET", c.canonical, "owner")
		for _, method := range []string{"GET", "HEAD"} {
			encoded := f.request(context.Background(), method, c.encoded, "owner")
			requireEqual(t, encoded.Code, 200)
			requireEqual(t, encoded.Header(), canonical.Header())
			want := canonical.Body.String()
			if method == "HEAD" {
				want = ""
			}
			requireEqual(t, encoded.Body.String(), want)
		}
	}
	for _, base := range []string{"/%61lpha", "/alpha/%72uns/" + u.ID, "/alpha/runs/%72" + u.ID[1:]} {
		for _, query := range []string{"", "?from=%2F"} {
			get := f.request(context.Background(), "GET", base+query, "owner")
			head := f.request(context.Background(), "HEAD", base+query, "owner")
			want := "/alpha/"
			if strings.Contains(base, "/runs/") || strings.Contains(base, "/%72uns/") {
				want = "/alpha/runs/" + u.ID + "/"
			}
			requireEqual(t, get.Code, 301)
			requireEqual(t, get.Header().Values("Location"), []string{want + query})
			requireEqual(t, head.Code, get.Code)
			requireEqual(t, head.Header(), get.Header())
			requireEqual(t, head.Body.Len(), 0)
		}
	}
	f.mu.Lock()
	f.users = nil
	f.mu.Unlock()
	missing := rendered(t, "notfound", pages.NoticeData{Banner: fixedBanner})
	requireEqual(t, f.request(context.Background(), "GET", "/alpha/", "other").Body.String(), missing)
	for _, path := range []string{"/nope/", "/nope", "/nope/runs/" + foreign.ID + "/", "/Alpha/", "/private/", "/private", "/about/", "/mcp/tools", "/alpha/runs/latest", "/alpha/runs/latest/", "/alpha/runs/" + foreign.ID + "/", "/alpha/runs/" + foreign.ID, "/alpha/runs/" + sibling.ID + "/", "/alpha/runs/" + sibling.ID, "/alpha/runs/" + strings.ToUpper(u.ID) + "/", "/alpha/runs/" + strings.ToUpper(u.ID), "/alpha/runs", "/alpha/runs/", "/alpha/other", "/alpha/other/", "/alpha/index.html", "/alpha/./", "/alpha/%2e/", "/alpha/../", "/alpha//", "/alpha/runs/" + u.ID + "//", "//", "/%2falpha/"} {
		w = f.request(context.Background(), "GET", path, "owner")
		requireEqual(t, w.Code, 404)
		requireEqual(t, w.Body.String(), missing)
		_, locationPresent := w.Header()["Location"]
		requireEqual(t, locationPresent, false)
		head := f.request(context.Background(), "HEAD", path, "owner")
		requireEqual(t, head.Code, 404)
		requireEqual(t, head.Header(), w.Header())
		requireEqual(t, head.Body.Len(), 0)
	}
	f.mu.Lock()
	for _, user := range f.users {
		requireEqual(t, user, page.User{})
	}
	f.users = nil
	f.mu.Unlock()
	for _, path := range []string{"/", "/about", "/alpha/", "/alpha", "/alpha/runs/" + u.ID + "/", "/nope/"} {
		get := f.request(context.Background(), "GET", path, "owner")
		head := f.request(context.Background(), "HEAD", path, "owner")
		requireEqual(t, head.Code, get.Code)
		requireEqual(t, head.Header(), get.Header())
		requireEqual(t, head.Body.Len(), 0)
		requireEqual(t, get.Header().Values("Set-Cookie"), []string(nil))
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		for _, path := range []string{"/", "/about", "/alpha/", "/private/", "/nope/"} {
			w = f.request(context.Background(), method, path, "owner")
			requireEqual(t, w.Code, 405)
			requireEqual(t, w.Header(), http.Header{"Allow": {"GET, HEAD"}})
			requireEqual(t, w.Body.Len(), 0)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, path := range []string{"/", "/alpha/", "/alpha", "/nope/", "/alpha/runs/" + u.ID + "/"} {
		for _, method := range []string{"GET", "HEAD"} {
			w = f.request(ctx, method, path, "owner")
			requireEqual(t, w.Code, 503)
			requireEqual(t, w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"})
			want := ""
			if method == "GET" {
				want = store.Unreachable + "\n"
			}
			requireEqual(t, w.Body.String(), want)
		}
	}
	f.db.SetFailing(true)
	f.mu.Lock()
	f.users = nil
	f.mu.Unlock()
	for _, path := range []string{"/", "//", "/alpha/", "/alpha", "/nope/", "/about/", "/alpha/other", "/alpha/runs/", "/alpha/a/b/c/d", "/alpha/runs/" + u.ID + "/"} {
		for _, method := range []string{"GET", "HEAD"} {
			w = f.request(context.Background(), method, path, "owner")
			requireEqual(t, w.Code, 503)
			requireEqual(t, w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"})
			requireEqual(t, w.Header().Values("Location"), []string(nil))
			want := ""
			if method == "GET" {
				want = store.Unreachable + "\n"
			}
			requireEqual(t, w.Body.String(), want)
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		for _, path := range []string{"/", "/about", "/alpha/", "/private/", "/nope/"} {
			w = f.request(context.Background(), method, path, "owner")
			requireEqual(t, w.Code, 405)
			requireEqual(t, w.Header(), http.Header{"Allow": {"GET, HEAD"}})
			requireEqual(t, w.Body.Len(), 0)
		}
	}
	f.mu.Lock()
	requireEqual(t, len(f.users), 0)
	f.mu.Unlock()
	w = f.request(context.Background(), "GET", "/about", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "about", pages.AboutData{Banner: b, Description: pages.Description}))
}

// R-OQVD-PQHY R-OTB6-H9ZC R-WOQ6-45MU R-OUJ2-V1Q1 R-OVQZ-8TGQ R-WUTO-10CB R-X3CY-PEJ6
func TestRunPageData(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	cases := []struct {
		status, reason, word, kind, title, sentence string
		seconds                                     int64
		exit                                        int
	}{
		{store.StatusQueued, "", pages.WordQueued, pages.KindInfo, "", "", 0, 0},
		{store.StatusRunning, "", pages.WordRunning, pages.KindInfo, "", "", 0, 0},
		{store.StatusExited, "", pages.WordExited + "0", pages.KindOK, "", "", 12, 0},
		{store.StatusExited, "", pages.WordExited + "7", pages.KindWarn, "", "", 221, 7},
		{store.StatusTimedOut, "", pages.WordTimedOut, pages.KindWarn, "", "", 600, 0},
		{store.StatusKilled, "", pages.WordKilled, pages.KindWarn, "", "", 1, 0},
		{store.StatusFailed, store.ReasonRepositoryMissing, pages.WordFailed, pages.KindErr, pages.TitleRepositoryMissing, fmt.Sprintf(pages.TextRepositoryMissing, sc.Repo), 0, 0},
		{store.StatusFailed, store.ReasonCommitMissing, pages.WordFailed, pages.KindErr, pages.TitleCommitMissing, fmt.Sprintf(pages.TextCommitMissing, "main", "Repository"), 0, 0},
		{store.StatusFailed, store.ReasonTooLarge, pages.WordFailed, pages.KindErr, pages.TitleTooLarge, fmt.Sprintf(pages.TextTooLarge, f.cfg.TreeMaxBytes), 0, 0},
		{store.StatusFailed, store.ReasonGitFailed, pages.WordFailed, pages.KindErr, pages.TitleGitFailed, fmt.Sprintf(pages.TextGitFailed, "Repository"), 0, 0},
		{store.StatusFailed, store.ReasonTimedOut, pages.WordFailed, pages.KindErr, pages.TitleTimedOut, fmt.Sprintf(pages.TextTimedOut, f.cfg.OperationSeconds), 0, 0},
		{store.StatusFailed, store.ReasonQueueAbandoned, pages.WordFailed, pages.KindErr, pages.TitleQueueAbandoned, pages.TextQueueAbandoned, 0, 0},
		{store.StatusFailed, store.ReasonStartFailed, pages.WordFailed, pages.KindErr, pages.TitleStartFailed, pages.TextStartFailed, 0, 0},
	}
	for i, c := range cases {
		u := f.add(t, sc, i+1, c.status, c.reason, c.seconds, c.exit)
		folder := f.cfg.Runs.Folder(u)
		writeFile(t, filepath.Join(folder, runs.InputFile), "{\"text\":\"<&>\"}\n")
		writeFile(t, filepath.Join(folder, runs.StdoutFile), strings.Repeat("x", 1229))
		writeFile(t, filepath.Join(folder, runs.StderrFile), "")
		writeFile(t, filepath.Join(folder, runs.OutDir, ".hidden"), "h")
		writeFile(t, filepath.Join(folder, runs.OutDir, "a b", "é?#.txt"), "text")
		// Directory traversal visits a/z before a.txt; the page must globally sort paths.
		writeFile(t, filepath.Join(folder, runs.OutDir, "a", "z"), "nested")
		writeFile(t, filepath.Join(folder, runs.OutDir, "a.txt"), "sibling")
		writeFile(t, filepath.Join(folder, runs.OutDir, "z"), strings.Repeat("z", 1048576))
		if e := os.Symlink(filepath.Join(folder, runs.StdoutFile), filepath.Join(folder, runs.OutDir, "link")); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(filepath.Join(folder, runs.OutDir, "a b"), filepath.Join(folder, runs.OutDir, "linked-dir")); e != nil {
			t.Fatal(e)
		}
		path := "/alpha/runs/" + u.ID + "/"
		w := f.request(context.Background(), "GET", path, "owner")
		requireEqual(t, w.Code, 200)
		b := fixedBanner
		b.Email = "person@example.com"
		b.ProfileURL = "https://auth.sbx.example/"
		b.LogoutURL = "https://auth.sbx.example/logout"
		dur := ""
		if c.status != store.StatusQueued && c.status != store.StatusRunning && c.status != store.StatusFailed {
			dur = fmt.Sprintf("%d%s", c.seconds, pages.UnitSecond)
			if c.seconds >= 60 {
				dur = fmt.Sprintf("%d%s%d%s", c.seconds/60, pages.UnitMinute, c.seconds%60, pages.UnitSecond)
			}
		}
		outSize, errSize := "1.2"+pages.UnitKilobyte, "69"+pages.UnitByte
		if c.status == store.StatusQueued || c.status == store.StatusRunning {
			errSize = "0" + pages.UnitByte
		}
		if c.status == store.StatusQueued || c.status == store.StatusFailed {
			outSize = "0" + pages.UnitByte
			errSize = "0" + pages.UnitByte
		}
		card := pages.RunCard{ID: u.ID, URL: path, Status: c.word, Kind: c.kind, Running: c.status == store.StatusRunning || c.status == store.StatusQueued, Commit: u.SHA, Ref: "main", Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: dur, Trigger: "manual", User: "owner", Request: u.RequestID, StdoutSize: outSize, StderrSize: errSize, Truncated: u.Truncated}
		if card.Running {
			card.Notice = pages.NoticeRunning
			if c.status == store.StatusQueued {
				card.Notice = pages.NoticeQueued
			}
		}
		if !card.Running {
			card.Finished = u.Finished.UTC().Format(pages.SecondLayout)
			card.FinishedAt = u.Finished.UTC().Format(time.RFC3339)
		}
		if c.title != "" {
			card.Failure = &pages.Failure{Title: c.title, Reason: c.sentence}
		}
		data := pages.RunData{Banner: b, Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: card, Input: &pages.FileText{Size: "17" + pages.UnitByte, Text: "{\"text\":\"<&>\"}\n", URL: path + runs.InputFile}, Stdout: &pages.FileText{Size: "1.2" + pages.UnitKilobyte, Text: strings.Repeat("x", 1229), URL: path + runs.StdoutFile}, Stderr: &pages.FileText{Size: "0" + pages.UnitByte, URL: path + runs.StderrFile}, Files: []pages.FileRow{{Path: ".hidden", Size: "1" + pages.UnitByte, URL: path + "out/.hidden"}, {Path: "a b/é?#.txt", Size: "4" + pages.UnitByte, URL: path + "out/a%20b/%C3%A9%3F%23.txt"}, {Path: "a.txt", Size: "7" + pages.UnitByte, URL: path + "out/a.txt"}, {Path: "a/z", Size: "6" + pages.UnitByte, URL: path + "out/a/z"}, {Path: "z", Size: "1.0" + pages.UnitMegabyte, URL: path + "out/z"}}}
		data.Input.Size = fmt.Sprintf("%d%s", len(data.Input.Text), pages.UnitByte)
		requireEqual(t, w.Body.String(), rendered(t, "run", data))
		if e := os.Remove(filepath.Join(folder, runs.InputFile)); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(filepath.Join(folder, runs.StdoutFile), filepath.Join(folder, runs.InputFile)); e != nil {
			t.Fatal(e)
		}
		data.Input = nil
		requireEqual(t, f.request(context.Background(), "GET", path, "owner").Body.String(), rendered(t, "run", data))
		if e := os.RemoveAll(folder); e != nil {
			t.Fatal(e)
		}
		data.Run.FilesGone = true
		data.Input = nil
		data.Stdout = nil
		data.Stderr = nil
		data.Files = nil
		if card.Running {
			data.Run.StdoutSize = "0" + pages.UnitByte
			data.Run.StderrSize = "0" + pages.UnitByte
		}
		w = f.request(context.Background(), "GET", path, "owner")
		requireEqual(t, w.Body.String(), rendered(t, "run", data))
	}
}

// R-XBW9-DSQ1 R-WUTO-10CB R-X3CY-PEJ6
func TestNoticeAndEmptyRunFiles(t *testing.T) {
	f := setup(t)
	w := f.request(context.Background(), "GET", "/missing/", "owner")
	body := w.Body.String()
	requireEqual(t, body, rendered(t, "notfound", pages.NoticeData{Banner: fixedBanner}))
	sc := f.create(t, "owner", "alpha")
	u := f.add(t, sc, 1, store.StatusRunning, "", 0, 0)
	folder := f.cfg.Runs.Folder(u)
	if e := os.MkdirAll(filepath.Join(folder, runs.OutDir), 0700); e != nil {
		t.Fatal(e)
	}
	b := fixedBanner
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	path := "/alpha/runs/" + u.ID + "/"
	d := pages.RunData{Banner: b, Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: pages.RunCard{ID: u.ID, URL: path, Status: pages.WordRunning, Kind: pages.KindInfo, Running: true, Notice: pages.NoticeRunning, Commit: u.SHA, Ref: "main", Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Trigger: "manual", User: "owner", Request: u.RequestID, StdoutSize: "0" + pages.UnitByte, StderrSize: "0" + pages.UnitByte}}
	w = f.request(context.Background(), "GET", path, "owner")
	requireEqual(t, w.Body.String(), rendered(t, "run", d))
	for _, name := range []string{runs.InputFile, runs.StdoutFile, runs.StderrFile} {
		writeFile(t, filepath.Join(folder, name), "")
	}
	d.Input = &pages.FileText{Size: "0" + pages.UnitByte, URL: path + runs.InputFile}
	d.Stdout = &pages.FileText{Size: "0" + pages.UnitByte, URL: path + runs.StdoutFile}
	d.Stderr = &pages.FileText{Size: "0" + pages.UnitByte, URL: path + runs.StderrFile}
	w = f.request(context.Background(), "GET", path, "owner")
	requireEqual(t, w.Body.String(), rendered(t, "run", d))
}

type snapshotEntry struct {
	Mode  fs.FileMode
	Size  int64
	Bytes string
}

func snapshot(t *testing.T, root string) map[string]snapshotEntry {
	t.Helper()
	result := map[string]snapshotEntry{}
	dir, e := os.OpenRoot(root)
	if os.IsNotExist(e) {
		return result
	}
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = dir.Close() }()
	e = fs.WalkDir(dir.FS(), ".", func(p string, _ fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := dir.Lstat(p)
		if e != nil {
			return e
		}
		entry := snapshotEntry{Mode: info.Mode(), Size: info.Size()}
		if info.Mode().IsRegular() {
			b, e := dir.ReadFile(p)
			if e != nil {
				return e
			}
			entry.Bytes = string(b)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			entry.Bytes, e = dir.Readlink(p)
			if e != nil {
				return e
			}
		}
		result[p] = entry
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return result
}

// R-NZBL-QK7R R-XFJY-J3Y4 R-XJ7N-OF67
func TestReadOnlyGitAndConcurrentPages(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	never, e := f.cfg.Store.Create(context.Background(), store.Draft{Owner: "owner", Name: "never", Repo: "rep_1111111111111111", Ref: "main"})
	if e != nil {
		t.Fatal(e)
	}
	foreign := f.create(t, "other", "private")
	writeFile(t, filepath.Join(f.cfg.Source.RepoDir(never.Repo), "config"), "[ikigenba]\n\tname = Another repository\n")
	u := f.add(t, sc, 1, store.StatusRunning, "", 0, 0)
	failed := f.add(t, sc, 2, store.StatusFailed, store.ReasonCommitMissing, 0, 0)
	gitFailed := f.add(t, sc, 3, store.StatusFailed, store.ReasonGitFailed, 0, 0)
	otherFailure := f.add(t, sc, 4, store.StatusFailed, store.ReasonStartFailed, 0, 0)
	f.add(t, foreign, 5, store.StatusExited, "", 12, 0)
	folder := f.cfg.Runs.Folder(u)
	writeFile(t, filepath.Join(folder, runs.StdoutFile), "output")
	writeFile(t, filepath.Join(folder, runs.InputFile), "{}\n")
	writeFile(t, filepath.Join(folder, runs.OutDir, "nested", "file"), "bytes")
	writeFile(t, filepath.Join(folder, runs.TreeDir, "main.py"), "pass\n")
	if e := os.Symlink("stdout", filepath.Join(folder, "link")); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(filepath.Join(folder, runs.StdoutFile), 0400); e != nil {
		t.Fatal(e)
	}
	catalog := func(s *store.Store) map[string]any {
		result := map[string]any{}
		for _, owner := range []string{"owner", "other", "absent"} {
			scripts, err := s.List(context.Background(), owner)
			if err != nil {
				t.Fatal(err)
			}
			result["owner:"+owner] = scripts
			for _, script := range scripts {
				records, err := s.Runs(context.Background(), script.ID)
				if err != nil {
					t.Fatal(err)
				}
				result["runs:"+script.ID] = records
			}
		}
		return result
	}
	beforeCatalog := catalog(f.cfg.Store)
	runsRoot, repoRoot := filepath.Join(f.root, "runs"), filepath.Join(f.root, "repos")
	beforeRuns, beforeRepos := snapshot(t, runsRoot), snapshot(t, repoRoot)
	assertState := func(s *store.Store) {
		requireEqual(t, snapshot(t, runsRoot), beforeRuns)
		requireEqual(t, snapshot(t, repoRoot), beforeRepos)
		requireEqual(t, catalog(s), beforeCatalog)
	}
	argv := func(repo string) []string {
		return []string{"config", "--file", filepath.Join(f.cfg.Source.RepoDir(repo), "config"), "--get", "ikigenba.name"}
	}
	assertGit := func(want [][]string) {
		data, err := os.ReadFile(f.trace)
		if err != nil {
			t.Fatal(err)
		}
		got := [][]string{}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			_, args, ok := strings.Cut(line, "built-in: git ")
			if !ok {
				t.Fatalf("unexpected git process trace: %q", line)
			}
			got = append(got, strings.Fields(args))
		}
		if want == nil {
			want = [][]string{}
		}
		requireEqual(t, got, want)
	}
	cases := []struct {
		path    string
		gitArgs [][]string
	}{
		{"/", [][]string{argv(sc.Repo), argv(never.Repo)}},
		{"/alpha/", [][]string{argv(sc.Repo)}},
		{"/never/", [][]string{argv(never.Repo)}},
		{"/alpha/runs/" + u.ID + "/", nil},
		{"/alpha/runs/" + failed.ID + "/", [][]string{argv(sc.Repo)}},
		{"/alpha/runs/" + gitFailed.ID + "/", [][]string{argv(sc.Repo)}},
		{"/alpha/runs/" + otherFailure.ID + "/", nil},
		{"/about", nil}, {"/alpha", nil}, {"/alpha/runs/" + u.ID, nil},
		{"/missing/", nil}, {"/private/", nil}, {"/alpha/runs/nope/", nil},
	}
	for _, c := range cases {
		for _, method := range []string{"GET", "HEAD"} {
			writeFile(t, f.trace, "")
			f.request(context.Background(), method, c.path, "owner")
			assertGit(c.gitArgs)
			assertState(f.cfg.Store)
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
		for _, path := range []string{"/", "/alpha/", "/alpha/runs/" + failed.ID + "/", "/missing/"} {
			writeFile(t, f.trace, "")
			requireEqual(t, f.request(context.Background(), method, path, "owner").Code, 405)
			assertGit(nil)
			assertState(f.cfg.Store)
		}
	}
	for _, method := range []string{"GET", "HEAD", "POST"} {
		writeFile(t, f.trace, "")
		f.request(context.Background(), method, "/alpha/", "")
		assertGit(nil)
		assertState(f.cfg.Store)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, method := range []string{"GET", "HEAD"} {
		for _, path := range []string{"/", "/alpha/", "/never/", "/alpha/runs/" + failed.ID + "/", "/missing/"} {
			writeFile(t, f.trace, "")
			requireEqual(t, f.request(ctx, method, path, "owner").Code, 503)
			assertGit(nil)
			assertState(f.cfg.Store)
		}
	}
	_, e = os.Lstat(filepath.Join(runsRoot, never.ID))
	if !os.IsNotExist(e) {
		t.Fatalf("never-run folder created: %v", e)
	}
	want := f.request(context.Background(), "GET", "/alpha/runs/"+u.ID+"/", "owner").Body.String()
	var wg sync.WaitGroup
	errs := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			w := f.request(context.Background(), "GET", "/alpha/runs/"+u.ID+"/", "owner")
			if w.Code != 200 || w.Body.String() != want {
				errs <- "concurrent answer differs"
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	assertState(f.cfg.Store)
	f.db.SetFailing(true)
	for _, method := range []string{"GET", "HEAD", "POST"} {
		for _, path := range []string{"/", "/alpha/", "/alpha", "/never/", "/alpha/runs/" + failed.ID + "/", "/missing/", "/about"} {
			writeFile(t, f.trace, "")
			f.request(context.Background(), method, path, "owner")
			assertGit(nil)
			requireEqual(t, snapshot(t, runsRoot), beforeRuns)
			requireEqual(t, snapshot(t, repoRoot), beforeRepos)
			// Temporarily restore reads to observe records after the refusal.
			f.db.SetFailing(false)
			requireEqual(t, catalog(f.cfg.Store), beforeCatalog)
			f.db.SetFailing(true)
		}
	}
}

// The anonymous conversions prove the complete ordered data contracts by use;
// adding, removing, reordering or changing a field makes these fail to compile.
// R-VQKZ-EKVE R-VRSV-SCM3 R-VT0S-64CS R-VU8O-JW3H R-70GP-VANL R-VXWD-P7BK R-VZ4A-2Z29 R-W0C6-GQSY R-7TOP-X110 R-W2RZ-8AAC R-W3ZV-M211 R-W57R-ZTRQ R-W6FO-DLIF R-W7NK-RD94 R-W8VH-54ZT
func TestDataContracts(t *testing.T) {
	_ = struct {
		Banner  page.Banner
		Scripts []pages.ScriptRow
	}(pages.LandingData{})
	_ = struct {
		Name, URL string
		Repo      pages.Repo
		Ref       string
		LastRun   *pages.RunRow
	}(pages.ScriptRow{})
	_ = struct{ ID, Name string }(pages.Repo{})
	_ = struct{ ID, URL, Status, Kind, Commit, Started, StartedAt, Duration, Exit string }(pages.RunRow{})
	_ = struct {
		Banner        page.Banner
		Script        pages.ScriptCard
		Runs          []pages.RunRow
		Subscriptions []string
	}(pages.ScriptData{})
	_ = struct {
		ID, Name                string
		Repo                    pages.Repo
		Ref, Created, CreatedAt string
		RunsKept                int
		KeepNewest, KeepDays    int64
	}(pages.ScriptCard{})
	_ = struct {
		Banner                page.Banner
		Script                pages.ScriptLink
		Run                   pages.RunCard
		Input, Stdout, Stderr *pages.FileText
		Files                 []pages.FileRow
	}(pages.RunData{})
	_ = struct{ Name, URL string }(pages.ScriptLink{})
	_ = struct {
		ID, URL, Status, Kind                                                                                                          string
		Running                                                                                                                        bool
		Notice, Commit, Ref, Started, StartedAt, Finished, FinishedAt, Duration, Trigger, Event, User, Request, StdoutSize, StderrSize string
		Truncated                                                                                                                      bool
		Failure                                                                                                                        *pages.Failure
		FilesGone                                                                                                                      bool
	}(pages.RunCard{})
	_ = struct{ Title, Reason string }(pages.Failure{})
	_ = struct{ Size, Text, URL string }(pages.FileText{})
	_ = struct{ Path, Size, URL string }(pages.FileRow{})
	_ = struct {
		Banner      page.Banner
		Description string
	}(pages.AboutData{})
	_ = struct{ Banner page.Banner }(pages.NoticeData{})
	_ = struct {
		Banner                                              func(page.User) page.Banner
		Pages                                               *pages.Set
		ServicesPath                                        string
		Store                                               *store.Store
		Source                                              *source.Source
		Runs                                                *runs.Core
		KeepDays, KeepCount, TreeMaxBytes, OperationSeconds int64
	}(pages.Config{})
	requireEqual(t, pages.ServiceName, "scripts")
}

// R-XGRU-WVOT
func TestBannerCallCounts(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	u := f.add(t, sc, 1, store.StatusRunning, "", 0, 0)
	for _, c := range []struct {
		method, path string
		code, count  int
	}{{"GET", "/", 200, 1}, {"HEAD", "/", 200, 1}, {"GET", "/about", 200, 1}, {"GET", "/alpha/", 200, 1}, {"GET", "/alpha/runs/" + u.ID + "/", 200, 1}, {"GET", "/none/", 404, 1}, {"HEAD", "/none/", 404, 1}, {"GET", "/alpha", 301, 0}, {"HEAD", "/alpha/runs/" + u.ID, 301, 0}, {"POST", "/about", 405, 0}} {
		f.mu.Lock()
		f.users = nil
		f.mu.Unlock()
		w := f.request(context.Background(), c.method, c.path, "owner")
		requireEqual(t, w.Code, c.code)
		f.mu.Lock()
		calls := append([]page.User{}, f.users...)
		f.mu.Unlock()
		requireEqual(t, len(calls), c.count)
		if len(calls) > 0 {
			want := page.User{}
			if c.code == 200 {
				want = page.User{Email: "person@example.com", ProfileURL: "https://auth.sbx.example/", LogoutURL: "https://auth.sbx.example/logout"}
			}
			requireEqual(t, calls[0], want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.mu.Lock()
	f.users = nil
	f.mu.Unlock()
	w := f.request(ctx, "GET", "/alpha/", "owner")
	requireEqual(t, w.Code, 503)
	f.mu.Lock()
	requireEqual(t, len(f.users), 0)
	f.mu.Unlock()
}

// R-OJJZ-F41S R-OKRV-SVSH R-OLZS-6NJ6 R-ON7O-KF9V R-OOFK-Y70K R-OPNH-BYR9
func TestCopyConstants(t *testing.T) {
	const words = pages.WordQueued + pages.WordRunning + pages.WordExited + pages.WordTimedOut + pages.WordKilled + pages.WordFailed
	const kinds = pages.KindInfo + pages.KindOK + pages.KindWarn + pages.KindErr
	const notices = pages.NoticeQueued + pages.NoticeRunning
	const titles = pages.TitleRepositoryMissing + pages.TitleCommitMissing + pages.TitleTooLarge + pages.TitleGitFailed + pages.TitleTimedOut + pages.TitleStartFailed + pages.TitleQueueAbandoned
	const formats = pages.TextRepositoryMissing + pages.TextCommitMissing + pages.TextTooLarge + pages.TextGitFailed + pages.TextTimedOut + pages.TextStartFailed + pages.TextQueueAbandoned
	const display = pages.MinuteLayout + pages.CardLayout + pages.SecondLayout + pages.UnitSecond + pages.UnitMinute + pages.UnitByte + pages.UnitKilobyte + pages.UnitMegabyte
	_ = []string{words, kinds, notices, titles, formats, display}
	for _, v := range []string{pages.WordQueued, pages.WordRunning, pages.WordExited, pages.WordTimedOut, pages.WordKilled, pages.WordFailed, pages.KindInfo, pages.KindOK, pages.KindWarn, pages.KindErr, pages.NoticeQueued, pages.NoticeRunning, pages.TitleRepositoryMissing, pages.TitleCommitMissing, pages.TitleTooLarge, pages.TitleGitFailed, pages.TitleTimedOut, pages.TitleStartFailed, pages.TitleQueueAbandoned, pages.MinuteLayout, pages.CardLayout, pages.SecondLayout, pages.UnitSecond, pages.UnitMinute, pages.UnitByte, pages.UnitKilobyte, pages.UnitMegabyte} {
		if v == "" {
			t.Fatal("empty copy constant")
		}
	}
	for _, c := range []struct{ format, verbs string }{
		{pages.TextRepositoryMissing, "%s"}, {pages.TextCommitMissing, "%s%s"}, {pages.TextTooLarge, "%d"}, {pages.TextGitFailed, "%s"}, {pages.TextTimedOut, "%d"}, {pages.TextStartFailed, ""}, {pages.TextQueueAbandoned, ""},
	} {
		if c.format == "" {
			t.Fatal("empty format")
		}
		var verbs string
		for i := 0; i < len(c.format); i++ {
			if c.format[i] == '%' {
				if i+1 == len(c.format) {
					t.Fatal("unfinished verb")
				}
				verbs += c.format[i : i+2]
				i++
			}
		}
		requireEqual(t, verbs, c.verbs)
	}
	for _, layout := range []string{pages.MinuteLayout, pages.CardLayout, pages.SecondLayout} {
		if fixedTime.Format(layout) == "" {
			t.Fatal("empty formatted time")
		}
	}
}

// R-OTB6-H9ZC R-OVQZ-8TGQ R-WUTO-10CB R-X3CY-PEJ6
func TestSizeBoundaryAndDurationFormatting(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	b := fixedBanner
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	for i, c := range []struct {
		bytes        int64
		number, unit string
		seconds      int64
		duration     string
	}{
		{0, "0", pages.UnitByte, 0, "0" + pages.UnitSecond},
		{69, "69", pages.UnitByte, 12, "12" + pages.UnitSecond},
		{999, "999", pages.UnitByte, 59, "59" + pages.UnitSecond},
		{1000, "1.0", pages.UnitKilobyte, 60, "1" + pages.UnitMinute + "0" + pages.UnitSecond},
		{1229, "1.2", pages.UnitKilobyte, 221, "3" + pages.UnitMinute + "41" + pages.UnitSecond},
		{12034, "12.0", pages.UnitKilobyte, 600, "10" + pages.UnitMinute + "0" + pages.UnitSecond},
		{999999, "999.9", pages.UnitKilobyte, 1, "1" + pages.UnitSecond},
		{1000000, "1.0", pages.UnitMegabyte, 2, "2" + pages.UnitSecond},
		{1048576, "1.0", pages.UnitMegabyte, 3, "3" + pages.UnitSecond},
	} {
		u := f.add(t, sc, i+1, store.StatusRunning, "", 0, 0)
		var err error
		u, err = f.cfg.Store.FinishRun(context.Background(), u.ID, store.Ending{Status: store.StatusExited, Finished: u.Started.Add(time.Duration(c.seconds) * time.Second), StdoutBytes: c.bytes, StderrBytes: c.bytes})
		if err != nil {
			t.Fatal(err)
		}
		path := "/alpha/runs/" + u.ID + "/"
		want := pages.RunData{Banner: b, Script: pages.ScriptLink{Name: sc.Name, URL: "/alpha/"}, Run: pages.RunCard{
			ID: u.ID, URL: path, Status: pages.WordExited + "0", Kind: pages.KindOK, Commit: u.SHA, Ref: u.Ref,
			Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339),
			Finished: u.Finished.UTC().Format(pages.SecondLayout), FinishedAt: u.Finished.UTC().Format(time.RFC3339), Duration: c.duration,
			Trigger: u.Trigger, User: u.User, Request: u.RequestID, StdoutSize: c.number + c.unit, StderrSize: c.number + c.unit, FilesGone: true,
		}}
		requireEqual(t, f.request(context.Background(), "GET", path, "owner").Body.String(), rendered(t, "run", want))
	}
}

// R-70GP-VANL R-72WI-MU4Z
func TestScriptSubscriptions(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	b := fixedBanner
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	check := func(names []string) {
		t.Helper()
		want := pages.ScriptData{Banner: b, Script: pages.ScriptCard{ID: sc.ID, Name: sc.Name, Repo: pages.Repo{ID: sc.Repo, Name: "Repository"}, Ref: sc.Ref, Created: sc.Created.UTC().Format(pages.CardLayout), CreatedAt: sc.Created.UTC().Format(time.RFC3339), KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}, Subscriptions: names}
		requireEqual(t, f.request(context.Background(), "GET", "/alpha/", "owner").Body.String(), rendered(t, "script", want))
	}
	check(nil)
	for _, name := range []string{"repo.pushed", "auth.signed_in"} {
		if _, err := f.cfg.Store.Subscribe(context.Background(), sc.ID, name); err != nil {
			t.Fatal(err)
		}
	}
	check([]string{"auth.signed_in", "repo.pushed"})
	if _, err := f.cfg.Store.Unsubscribe(context.Background(), sc.ID, "auth.signed_in"); err != nil {
		t.Fatal(err)
	}
	check([]string{"repo.pushed"})
}

// R-WOQ6-45MU R-WPY2-HXDJ R-72WI-MU4Z R-WW1K-ES30 R-OQVD-PQHY R-OUJ2-V1Q1 R-OVQZ-8TGQ R-WUTO-10CB R-X3CY-PEJ6
func TestEmptyCatalogMissingRepositoryAndEventRun(t *testing.T) {
	f := setup(t)
	b := fixedBanner
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	requireEqual(t, f.request(context.Background(), "GET", "/", "owner").Body.String(), rendered(t, "landing", pages.LandingData{Banner: b}))
	sc := f.create(t, "owner", "alpha")
	for _, missing := range []bool{false, true} {
		repo := pages.Repo{ID: sc.Repo, Name: "Repository"}
		if missing {
			if err := os.RemoveAll(f.cfg.Source.RepoDir(sc.Repo)); err != nil {
				t.Fatal(err)
			}
			repo.Name = ""
		}
		requireEqual(t, f.request(context.Background(), "GET", "/", "owner").Body.String(), rendered(t, "landing", pages.LandingData{Banner: b, Scripts: []pages.ScriptRow{{Name: sc.Name, URL: "/alpha/", Repo: repo, Ref: sc.Ref}}}))
		requireEqual(t, f.request(context.Background(), "GET", "/alpha/", "owner").Body.String(), rendered(t, "script", pages.ScriptData{Banner: b, Script: pages.ScriptCard{ID: sc.ID, Name: sc.Name, Repo: repo, Ref: sc.Ref, Created: sc.Created.UTC().Format(pages.CardLayout), CreatedAt: sc.Created.UTC().Format(time.RFC3339), KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}}))
	}
	for i, c := range []struct{ status, reason, title, text string }{
		{store.StatusRunning, "", "", ""},
		{store.StatusFailed, store.ReasonCommitMissing, pages.TitleCommitMissing, fmt.Sprintf(pages.TextCommitMissing, sc.Ref, sc.Repo)},
		{store.StatusFailed, store.ReasonGitFailed, pages.TitleGitFailed, fmt.Sprintf(pages.TextGitFailed, sc.Repo)},
	} {
		u := store.Run{ID: fmt.Sprintf("run_%016x", i+1), Script: sc.ID, Ref: sc.Ref, Trigger: store.TriggerEvent, Event: fmt.Sprintf("fixture-event-%d", i), User: sc.Owner, RequestID: "fixture-request", Status: c.status, Reason: c.reason, Started: fixedTime}
		if c.status == store.StatusFailed {
			u.Finished = u.Started
		} else {
			u.SHA = strings.Repeat("a", 40)
		}
		var err error
		u, err = f.cfg.Store.AddRun(context.Background(), u)
		if err != nil {
			t.Fatal(err)
		}
		path := "/alpha/runs/" + u.ID + "/"
		card := pages.RunCard{ID: u.ID, URL: path, Status: pages.WordRunning, Kind: pages.KindInfo, Running: true, Notice: pages.NoticeRunning, Commit: u.SHA, Ref: u.Ref, Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Trigger: u.Trigger, Event: u.Event, User: u.User, Request: u.RequestID, StdoutSize: "0" + pages.UnitByte, StderrSize: "0" + pages.UnitByte, FilesGone: true}
		if c.status == store.StatusFailed {
			card.Status = pages.WordFailed
			card.Kind = pages.KindErr
			card.Running = false
			card.Notice = ""
			card.Finished = u.Finished.UTC().Format(pages.SecondLayout)
			card.FinishedAt = u.Finished.UTC().Format(time.RFC3339)
			card.Failure = &pages.Failure{Title: c.title, Reason: c.text}
		}
		requireEqual(t, f.request(context.Background(), "GET", path, "owner").Body.String(), rendered(t, "run", pages.RunData{Banner: b, Script: pages.ScriptLink{Name: sc.Name, URL: "/alpha/"}, Run: card}))
	}
}
