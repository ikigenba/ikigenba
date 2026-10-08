package pages_test

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
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
func requireContains(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, v := range values {
		if !strings.Contains(body, v) {
			t.Fatalf("missing %q in %s", v, body)
		}
	}
}
func requireAbsent(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, v := range values {
		if strings.Contains(body, v) {
			t.Fatalf("unexpected %q", v)
		}
	}
}

// R-VKHH-HQ5X R-VMXA-99NB R-VO56-N1E0 R-VPD3-0T4P R-VQKZ-EKVE R-VRSV-SCM3 R-VT0S-64CS R-VU8O-JW3H R-70GP-VANL R-VXWD-P7BK R-VZ4A-2Z29 R-W0C6-GQSY R-7TOP-X110 R-W2RZ-8AAC R-W3ZV-M211 R-W57R-ZTRQ R-W6FO-DLIF R-W7NK-RD94 R-WBB9-WOH7 R-WCJ6-AG7W R-WDR2-O7YL R-WEYZ-1ZPA R-WHER-TJ6O
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
	requireEqual(t, description, "Python scripts run from the suite's repositories")
	t.Chdir(t.TempDir())
	set, e := pages.Load()
	if e != nil || set == nil {
		t.Fatalf("load: %v", e)
	}
	repo := pages.Repo{ID: "repo", Name: "Repository <name>"}
	row := pages.RunRow{ID: "run", URL: "/alpha/runs/run/", Status: "exited 0", Kind: "ok", Commit: "abcdef0", Started: "start", StartedAt: "stamp", Duration: "12s", Exit: "0"}
	card := pages.RunCard{ID: "run", URL: row.URL, Status: row.Status, Kind: row.Kind, Running: false, Commit: "abcdef012345", Ref: "main", Started: "start", StartedAt: "stamp", Finished: "finish", FinishedAt: "endstamp", Duration: "12s", Trigger: "manual", User: "user", Request: "request", StdoutSize: "1 B", StderrSize: "0 B", Truncated: true, Failure: &pages.Failure{Title: "Fail", Reason: "Reason <text>"}, FilesGone: false}
	file := &pages.FileText{Size: "1 B", Text: "<text>&\n", URL: "/file"}
	cases := []struct {
		name string
		data any
	}{
		{"landing", pages.LandingData{Banner: fixedBanner, Scripts: []pages.ScriptRow{{Name: "alpha", URL: "/alpha/", Repo: repo, Ref: "main", LastRun: &row}}}},
		{"script", pages.ScriptData{Banner: fixedBanner, Script: pages.ScriptCard{ID: "script", Name: "alpha", Repo: repo, Ref: "main", Created: "created", CreatedAt: "stamp", RunsKept: 1, KeepNewest: 10, KeepDays: 30}, Runs: []pages.RunRow{row}}},
		{"run", pages.RunData{Banner: fixedBanner, Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: card, Input: file, Stdout: file, Stderr: file, Files: []pages.FileRow{{Path: "file", Size: "1 B", URL: "/file"}}}},
		{"about", pages.AboutData{Banner: fixedBanner, Description: pages.Description}},
		{"notfound", pages.NoticeData{Banner: fixedBanner}},
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
	row := pages.RunRow{ID: u.ID, URL: "/alpha/runs/" + u.ID + "/", Status: "exited 0", Kind: "ok", Commit: "aaaaaaa", Started: u.Started.UTC().Format("2006-01-02 15:04"), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: "12s", Exit: "0"}
	b := fixedBanner
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	expectedLanding := pages.LandingData{Banner: b, Scripts: []pages.ScriptRow{{Name: "alpha", URL: "/alpha/", Repo: pages.Repo{ID: f.sc.Repo, Name: "Repository"}, Ref: "main", LastRun: &row}, {Name: "beta", URL: "/beta/", Repo: pages.Repo{ID: second.Repo, Name: "Repository"}, Ref: "main", LastRun: &pages.RunRow{ID: sibling.ID, URL: "/beta/runs/" + sibling.ID + "/", Status: "running", Kind: "info", Commit: "aaaaaaa", Started: sibling.Started.UTC().Format("2006-01-02 15:04"), StartedAt: sibling.Started.UTC().Format(time.RFC3339)}}}}
	w := f.request(context.Background(), "GET", "/?query=yes", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "landing", expectedLanding))
	expectedScript := pages.ScriptData{Banner: b, Script: pages.ScriptCard{ID: f.sc.ID, Name: "alpha", Repo: pages.Repo{ID: f.sc.Repo, Name: "Repository"}, Ref: "main", Created: f.sc.Created.UTC().Format("2006-01-02 15:04 UTC"), CreatedAt: f.sc.Created.UTC().Format(time.RFC3339), RunsKept: 1, KeepNewest: 10, KeepDays: 30}, Runs: []pages.RunRow{row}}
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

// R-SSNY-GDTR R-SXJJ-ZGSJ R-WOQ6-45MU R-STVU-U5KG R-7UWM-ASRP R-WUTO-10CB R-X3CY-PEJ6
func TestRunPageData(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	cases := []struct {
		status, reason, word, kind, title, sentence string
		seconds                                     int64
		exit                                        int
	}{
		{store.StatusRunning, "", "running", "info", "", "", 0, 0},
		{store.StatusExited, "", "exited 0", "ok", "", "", 12, 0},
		{store.StatusExited, "", "exited 7", "warn", "", "", 221, 7},
		{store.StatusTimedOut, "", "timed out", "warn", "", "", 600, 0},
		{store.StatusKilled, "", "killed", "warn", "", "", 1, 0},
		{store.StatusFailed, store.ReasonRepositoryMissing, "failed", "err", "The repository is unavailable", "The repository " + sc.Repo + " is not there. The script was not started.", 0, 0},
		{store.StatusFailed, store.ReasonCommitMissing, "failed", "err", "The ref did not resolve", "'main' names no commit in the repository Repository. The script was not started.", 0, 0},
		{store.StatusFailed, store.ReasonTooLarge, "failed", "err", "The tree is too large", "The commit's files add up to more than 1229 bytes. The script was not started.", 0, 0},
		{store.StatusFailed, store.ReasonGitFailed, "failed", "err", "git failed", "git could not read the repository Repository. The script was not started.", 0, 0},
		{store.StatusFailed, store.ReasonTimedOut, "failed", "err", "git took too long", "git took longer than 12 seconds. The script was not started.", 0, 0},
		{store.StatusFailed, store.ReasonQueueAbandoned, "failed", "err", "The run never left the queue", "The run was waiting for a slot when scripts stopped. The script was not started.", 0, 0},
		{store.StatusFailed, store.ReasonStartFailed, "failed", "err", "The script could not start", "The script's process could not be launched. The script was not started.", 0, 0},
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
		var filePaths []string
		for _, row := range occurrenceElements(byID(t, w.Body.String(), "file-list").body, "tr", "data-file") {
			filePaths = append(filePaths, values(row, "data-file")...)
		}
		requireEqual(t, filePaths, []string{".hidden", "a b/é?#.txt", "a.txt", "a/z", "z"})
		b := fixedBanner
		b.Email = "person@example.com"
		b.ProfileURL = "https://auth.sbx.example/"
		b.LogoutURL = "https://auth.sbx.example/logout"
		dur := ""
		if c.status != store.StatusQueued && c.status != store.StatusRunning && c.status != store.StatusFailed {
			dur = fmt.Sprintf("%ds", c.seconds)
			if c.seconds >= 60 {
				dur = fmt.Sprintf("%dm %ds", c.seconds/60, c.seconds%60)
			}
		}
		outSize, errSize := "1.2 kB", "69 B"
		if c.status == store.StatusQueued || c.status == store.StatusRunning {
			errSize = "0 B"
		}
		if c.status == store.StatusFailed {
			outSize = "0 B"
			errSize = "0 B"
		}
		card := pages.RunCard{ID: u.ID, URL: path, Status: c.word, Kind: c.kind, Running: c.status == store.StatusRunning || c.status == store.StatusQueued, Commit: u.SHA, Ref: "main", Started: u.Started.UTC().Format("2006-01-02 15:04:05 UTC"), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: dur, Trigger: "manual", User: "owner", Request: u.RequestID, StdoutSize: outSize, StderrSize: errSize, Truncated: u.Truncated}
		if card.Running {
			card.Notice = "Running"
			if c.status == store.StatusQueued {
				card.Notice = "Queued"
			}
		}
		if !card.Running {
			card.Finished = u.Finished.UTC().Format("2006-01-02 15:04:05 UTC")
			card.FinishedAt = u.Finished.UTC().Format(time.RFC3339)
		}
		if c.title != "" {
			card.Failure = &pages.Failure{Title: c.title, Reason: c.sentence}
		}
		data := pages.RunData{Banner: b, Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: card, Input: &pages.FileText{Size: "17 B", Text: "{\"text\":\"<&>\"}\n", URL: path + runs.InputFile}, Stdout: &pages.FileText{Size: "1.2 kB", Text: strings.Repeat("x", 1229), URL: path + runs.StdoutFile}, Stderr: &pages.FileText{Size: "0 B", URL: path + runs.StderrFile}, Files: []pages.FileRow{{Path: ".hidden", Size: "1 B", URL: path + "out/.hidden"}, {Path: "a b/é?#.txt", Size: "4 B", URL: path + "out/a%20b/%C3%A9%3F%23.txt"}, {Path: "a.txt", Size: "7 B", URL: path + "out/a.txt"}, {Path: "a/z", Size: "6 B", URL: path + "out/a/z"}, {Path: "z", Size: "1.0 MB", URL: path + "out/z"}}}
		data.Input.Size = fmt.Sprintf("%d B", len(data.Input.Text))
		requireEqual(t, w.Body.String(), rendered(t, "run", data))
		verifyRunHooks(t, w.Body.String(), data)
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
			data.Run.StdoutSize = "0 B"
			data.Run.StderrSize = "0 B"
		}
		w = f.request(context.Background(), "GET", path, "owner")
		requireEqual(t, w.Body.String(), rendered(t, "run", data))
		verifyRunHooks(t, w.Body.String(), data)
	}
	// A lost or unnamed repository is represented by its id, including failure sentences.
	repoDir := f.cfg.Source.RepoDir(sc.Repo)
	if e := os.RemoveAll(repoDir); e != nil {
		t.Fatal(e)
	}
	w := f.request(context.Background(), "GET", "/alpha/", "owner")
	requireContains(t, w.Body.String(), `title="the repository is gone"`, sc.Repo)
	for i, reason := range []string{store.ReasonCommitMissing, store.ReasonGitFailed} {
		u := f.add(t, sc, 50+i, store.StatusFailed, reason, 0, 0)
		w = f.request(context.Background(), "GET", "/alpha/runs/"+u.ID+"/", "owner")
		requireContains(t, normalise(w.Body.String()), "repository "+sc.Repo+". The script was not started.")
	}
}

// R-XKFK-26WW R-XLNG-FYNL R-XMVC-TQEA R-XO39-7I4Z R-XPB5-L9VO
// These readers implement D08's written-markup vocabulary using the standard library.
type attribute struct {
	name, value string
	hasValue    bool
}

type element struct {
	tag                              string
	attrs                            map[string]string // Compatibility view; use attributes for occurrence checks.
	attributes                       []attribute
	body                             string
	start, end, contentEnd, closeEnd int
	contentPresent                   bool
	startTag                         string
}

var attributePattern = regexp.MustCompile(`^[\t\n\f\r ]+([^\t\n\f\r "'<>/=]+)(?:="([^"]*)")?`)

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
func asciiAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
func asciiSpace(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// tagSpans scans every '<', including overlapping spans, as the definition does.
func tagSpans(s, name string, closing bool) [][2]int {
	var spans [][2]int
	prefix := "<"
	if closing {
		prefix += "/"
	}
	prefix += asciiLower(name)
	for i := 0; i < len(s); i++ {
		if s[i] != '<' || len(s)-i <= len(prefix) || asciiLower(s[i:i+len(prefix)]) != prefix || asciiAlnum(s[i+len(prefix)]) {
			continue
		}
		if j := strings.IndexByte(s[i:], '>'); j >= 0 {
			spans = append(spans, [2]int{i, i + j + 1})
		}
	}
	return spans
}
func tagNamed(e element, name string) bool {
	spans := tagSpans(e.startTag, name, false)
	return len(spans) > 0 && spans[0][0] == 0
}
func readElement(body string, span [2]int, name string) element {
	e := element{tag: asciiLower(name), attrs: map[string]string{}, start: span[0], end: span[1], contentEnd: -1, closeEnd: -1, startTag: body[span[0]:span[1]]}
	i := 1
	for i < len(e.startTag) && (asciiAlnum(e.startTag[i]) || e.startTag[i] == '-') {
		i++
	}
	rest := e.startTag[i : len(e.startTag)-1]
	for {
		a := attributePattern.FindStringSubmatchIndex(rest)
		if a == nil {
			break
		}
		x := attribute{name: asciiLower(rest[a[2]:a[3]]), hasValue: a[4] >= 0}
		if x.hasValue {
			x.value = html.UnescapeString(rest[a[4]:a[5]])
		}
		e.attributes = append(e.attributes, x)
		e.attrs[x.name] = x.value
		rest = rest[a[1]:]
	}
	if closing := tagSpans(body[e.end:], name, true); len(closing) > 0 {
		e.contentPresent = true
		e.contentEnd = e.end + closing[0][0]
		e.closeEnd = e.end + closing[0][1]
		e.body = body[e.end:e.contentEnd]
	}
	return e
}
func elements(body, tag string) []element {
	var result []element
	if tag != "" {
		for _, span := range tagSpans(body, tag, false) {
			result = append(result, readElement(body, span, tag))
		}
		return result
	}
	for i := 0; i < len(body); i++ {
		if body[i] != '<' {
			continue
		}
		j := i + 1
		for j < len(body) && (asciiAlnum(body[j]) || body[j] == '-') {
			j++
		}
		if j == i+1 || j == len(body) {
			continue
		}
		name := body[i+1 : j]
		// All element names D08 names are alphanumeric; a following hyphen
		// is their delimiter even though it remains part of the attribute-reader name.
		if k := strings.IndexByte(name, '-'); k > 0 {
			name = name[:k]
		}
		if end := strings.IndexByte(body[i:], '>'); end >= 0 {
			result = append(result, readElement(body, [2]int{i, i + end + 1}, name))
		}
	}
	return result
}
func values(e element, name string) []string {
	var found []string
	for _, a := range e.attributes {
		if a.hasValue && a.name == asciiLower(name) {
			found = append(found, a.value)
		}
	}
	return found
}
func bearing(e element, name string) bool {
	for _, a := range e.attributes {
		if a.name == asciiLower(name) {
			return true
		}
	}
	return false
}
func hasAttr(e element, name, value string) bool {
	for _, v := range values(e, name) {
		if v == value {
			return true
		}
	}
	return false
}
func identified(body, tag, id string) []element {
	var found []element
	for _, e := range elements(body, tag) {
		if hasAttr(e, "id", id) {
			found = append(found, e)
		}
	}
	return found
}
func content(t *testing.T, e element) string {
	t.Helper()
	if !e.contentPresent {
		t.Fatalf("%s at %d has absent element content", e.tag, e.start)
	}
	return e.body
}
func within(inner, outer element) bool {
	return outer.contentPresent && inner.start >= outer.end && inner.end <= outer.contentEnd
}
func visibleText(s string) string {
	bodies := elements(s, "body")
	if len(bodies) == 0 || !bodies[0].contentPresent {
		return ""
	}
	return normalise(bodies[0].body)
}
func normalise(s string) string {
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + s[i+j+1:]
	}
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}
func byID(t *testing.T, body, id string) element {
	t.Helper()
	var got []element
	for _, e := range elements(body, "") {
		if hasAttr(e, "id", id) {
			got = append(got, e)
		}
	}
	if len(got) != 1 {
		t.Fatalf("id %s: count %d", id, len(got))
	}
	return got[0]
}
func countID(body, id string) int {
	n := 0
	for _, e := range elements(body, "") {
		if hasAttr(e, "id", id) {
			n++
		}
	}
	return n
}
func class(e element, c string) bool {
	for _, value := range values(e, "class") {
		for _, token := range strings.FieldsFunc(value, asciiSpace) {
			if token == c {
				return true
			}
		}
	}
	return false
}
func ofClass(body, tag, c string) []element {
	var found []element
	for _, e := range elements(body, tag) {
		if class(e, c) {
			found = append(found, e)
		}
	}
	return found
}
func contentTexts(t *testing.T, body, tag string) []string {
	t.Helper()
	var v []string
	for _, e := range elements(body, tag) {
		v = append(v, normalise(content(t, e)))
	}
	return v
}
func attr(t *testing.T, e element, key, want string) {
	t.Helper()
	if !hasAttr(e, key, want) {
		t.Fatalf("%s has %s occurrences %#v; want an occurrence %q", e.tag, key, values(e, key), want)
	}
}
func one(t *testing.T, body, tag string) element {
	t.Helper()
	es := elements(body, tag)
	if len(es) != 1 {
		t.Fatalf("%s count %d", tag, len(es))
	}
	return es[0]
}
func bearingElements(body, tag, name string) []element {
	var found []element
	for _, e := range elements(body, tag) {
		if bearing(e, name) {
			found = append(found, e)
		}
	}
	return found
}
func occurrenceElements(body, tag, name string) []element {
	var found []element
	for _, e := range elements(body, tag) {
		if len(values(e, name)) > 0 {
			found = append(found, e)
		}
	}
	return found
}

// Run facts require one dd after each dt; leading dd tags are permitted.
func runFactDDs(t *testing.T, body string) []element {
	t.Helper()
	dts, dds := elements(body, "dt"), elements(body, "dd")
	var paired []element
	for i, dt := range dts {
		end := len(body)
		if i+1 < len(dts) {
			end = dts[i+1].start
		}
		var following []element
		for _, dd := range dds {
			if dd.start >= dt.end && dd.start < end {
				following = append(following, dd)
			}
		}
		requireEqual(t, len(following), 1)
		paired = append(paired, following[0])
	}
	return paired
}

func some(t *testing.T, body, tag string, matches func(element) bool) {
	t.Helper()
	for _, e := range elements(body, tag) {
		if matches(e) {
			return
		}
	}
	t.Fatalf("no qualifying %s element", tag)
}
func contained(t *testing.T, inner, outer element) {
	t.Helper()
	if !within(inner, outer) {
		t.Fatal("element outside required container")
	}
}
func stripCodeTags(s string) string {
	spans := append(tagSpans(s, "code", false), tagSpans(s, "code", true)...)
	marked := make([]bool, len(s))
	for _, span := range spans {
		for i := span[0]; i < span[1]; i++ {
			marked[i] = true
		}
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if !marked[i] {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func appkitMarkup(t *testing.T, name string, b page.Banner) string {
	t.Helper()
	var out bytes.Buffer
	if err := page.Templates().ExecuteTemplate(&out, name, b); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// R-XRQY-CTD2: inspect the shared banner in the page's actual response.
func bannerHooks(t *testing.T, body string, b page.Banner) {
	t.Helper()
	headers := elements(body, "header")
	if len(headers) == 0 {
		t.Fatal("absent banner header")
	}
	header := content(t, headers[0])
	mark := one(t, header, "strong")
	if !class(mark, "mark") {
		t.Fatal("banner mark hook")
	}
	attr(t, mark, "data-service", b.Service)
	favicon := one(t, mark.body, "img")
	attr(t, favicon, "src", "/_appkit/favicon.svg")
	attr(t, favicon, "alt", "")
	service := one(t, mark.body, "span")
	if !class(service, "service") {
		t.Fatal("banner service hook")
	}
	requireEqual(t, strings.TrimSpace(mark.body[:favicon.start]), "")
	requireEqual(t, normalise(mark.body[favicon.end:service.start]), "Ikigenba")
	serviceContent := strings.TrimSpace(service.body)
	if !strings.HasPrefix(serviceContent, string(b.Icon)) {
		t.Fatal("banner service icon differs from returned icon")
	}
	requireEqual(t, strings.TrimSpace(serviceContent[len(b.Icon):]), template.HTMLEscapeString(b.Service))
	requireEqual(t, strings.TrimSpace(mark.body[service.closeEnd:]), "")
	requireEqual(t, strings.TrimSpace(header[:mark.start]), "")
	next := mark.closeEnd
	launchers := ofClass(header, "button", "launcher")
	if len(b.Services) > 0 {
		requireEqual(t, len(launchers), 1)
		requireEqual(t, strings.TrimSpace(header[next:launchers[0].start]), "")
		next = launchers[0].closeEnd
	} else {
		requireEqual(t, len(launchers), 0)
	}
	profile := one(t, header, "a")
	if !class(profile, "profile") {
		t.Fatal("banner profile hook")
	}
	attr(t, profile, "title", b.Email)
	requireEqual(t, strings.TrimSpace(header[next:profile.start]), "")
	form := one(t, header, "form")
	requireEqual(t, strings.TrimSpace(header[profile.closeEnd:form.start]), "")
	signout := one(t, form.body, "button")
	if !class(signout, "signout") {
		t.Fatal("banner sign-out hook")
	}
	for name, value := range map[string]string{"type": "submit", "aria-label": "Sign out", "title": "Sign out"} {
		attr(t, signout, name, value)
	}
	requireEqual(t, normalise(signout.body), "")
	one(t, signout.body, "svg")
}

func written(t *testing.T, body string, b page.Banner) string {
	t.Helper()
	banner := appkitMarkup(t, "banner", b)
	footer := appkitMarkup(t, "footer", b)
	starts := tagSpans(body, "body", false)
	ends := tagSpans(body, "body", true)
	if len(starts) == 0 || len(ends) == 0 {
		t.Fatal("absent body boundary")
	}
	bodyStart, bodyEnd := starts[0][1], ends[len(ends)-1][0]
	bannerStart := bodyStart
	for bannerStart < len(body) && asciiSpace(rune(body[bannerStart])) {
		bannerStart++
	}
	if !strings.HasPrefix(body[bannerStart:], banner) {
		t.Fatal("banner position")
	}
	footerEnd := bodyEnd
	for footerEnd > 0 && asciiSpace(rune(body[footerEnd-1])) {
		footerEnd--
	}
	footerStart := footerEnd - len(footer)
	if footerStart < bannerStart+len(banner) || body[footerStart:footerEnd] != footer {
		t.Fatal("footer position")
	}
	return body[:bannerStart] + body[bannerStart+len(banner):footerStart] + body[footerEnd:]
}
func titleHooks(t *testing.T, body, title, heading string) {
	t.Helper()
	bodies := elements(body, "body")
	if len(bodies) == 0 {
		t.Fatal("absent body start")
	}
	titleTag := one(t, body, "title")
	ends := tagSpans(body, "title", true)
	requireEqual(t, len(ends), 1)
	if titleTag.end > ends[0][0] || ends[0][1] > bodies[0].start {
		t.Fatal("title boundaries not ordered before body")
	}
	requireEqual(t, normalise(body[titleTag.end:ends[0][0]]), title)
	requireEqual(t, normalise(content(t, one(t, body, "h1"))), heading)
}
func resourceHooks(t *testing.T, body string) {
	t.Helper()
	bodies := elements(body, "body")
	if len(bodies) == 0 {
		t.Fatal("absent body start")
	}
	n := 0
	for _, link := range elements(body, "link") {
		if hasAttr(link, "rel", "stylesheet") {
			n++
			attr(t, link, "href", "/_appkit/theme.css")
			if link.end > bodies[0].start {
				t.Fatal("stylesheet after body")
			}
		}
	}
	requireEqual(t, n, 1)
	n = 0
	for _, e := range elements(body, "meta") {
		if hasAttr(e, "name", "viewport") {
			n++
			attr(t, e, "content", "width=device-width, initial-scale=1")
			if e.end > bodies[0].start {
				t.Fatal("viewport after body")
			}
		}
	}
	requireEqual(t, n, 1)
	for _, e := range elements(body, "") {
		if !tagNamed(e, "a") {
			for _, a := range e.attributes {
				switch a.name {
				case "href", "src", "poster", "data", "background", "manifest":
					if a.hasValue && a.value != "/" && (len(a.value) < 2 || a.value[0] != '/' || a.value[1] == '/' || a.value[1] == '\\') {
						t.Fatalf("non-local resource %s=%q", a.name, a.value)
					}
				}
			}
		}
	}
}
func feedbackHead(t *testing.T, body string) {
	t.Helper()
	var feedback []element
	for _, e := range elements(body, "script") {
		if hasAttr(e, "src", "/_appkit/feedback.js") {
			feedback = append(feedback, e)
		}
	}
	requireEqual(t, len(feedback), 1)
	bodies := elements(body, "body")
	if len(bodies) == 0 || feedback[0].end > bodies[0].start {
		t.Fatal("feedback script not before first body")
	}
	// Check bare defer lexically, including value syntax outside D08's reader.
	start := feedback[0].startTag
	i := 1
	for i < len(start) && (asciiAlnum(start[i]) || start[i] == '-') {
		i++
	}
	rest := start[i:]
	attribute := regexp.MustCompile(`^[\t\n\f\r ]+([^\t\n\f\r "'<>/=]+)([\t\n\f\r ]*=[\t\n\f\r ]*(?:"[^"]*"|'[^']*'|[^\t\n\f\r >]*))?`)
	bareDefer := false
	for {
		match := attribute.FindStringSubmatchIndex(rest)
		if match == nil {
			break
		}
		if asciiLower(rest[match[2]:match[3]]) == "defer" {
			if match[4] >= 0 {
				t.Fatal("feedback script defer attribute has a value")
			}
			bareDefer = true
		}
		rest = rest[match[1]:]
	}
	if !bareDefer {
		t.Fatal("feedback script lacks a bare defer attribute")
	}
}

func faviconHead(t *testing.T, body string) {
	t.Helper()
	var icons []element
	for _, e := range elements(body, "link") {
		if hasAttr(e, "rel", "icon") {
			icons = append(icons, e)
		}
	}
	requireEqual(t, len(icons), 1)
	bodies := elements(body, "body")
	if len(bodies) == 0 || icons[0].end > bodies[0].start {
		t.Fatal("favicon link not before first body")
	}
	attr(t, icons[0], "href", "/_appkit/favicon.svg")
	attr(t, icons[0], "type", "image/svg+xml")
}

func preloadHead(t *testing.T, body string) {
	t.Helper()
	var preloads []element
	for _, e := range elements(body, "link") {
		if hasAttr(e, "rel", "preload") {
			preloads = append(preloads, e)
		}
	}
	requireEqual(t, len(preloads), 1)
	bodies := elements(body, "body")
	if len(bodies) == 0 || preloads[0].start >= bodies[0].start {
		t.Fatal("font preload not before first body")
	}
	attr(t, preloads[0], "as", "font")
	attr(t, preloads[0], "type", "font/woff2")
	attr(t, preloads[0], "href", page.PreloadURL())
	found := false
	for _, a := range preloads[0].attributes {
		if a.name == "crossorigin" {
			found = true
			if a.hasValue && a.value != "" {
				t.Fatal("font preload crossorigin is not empty")
			}
		}
	}
	if !found {
		t.Fatal("font preload lacks crossorigin")
	}
}

// R-C6I9-ET70
func TestPlainPageFontPreload(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "plain-script")
	u := f.add(t, sc, 501, store.StatusRunning, "", 0, 0)
	for _, b := range []page.Banner{fixedBanner, {Service: "runner-Blue.7", Version: "build+candidate.8"}} {
		f.cfg.Banner = func(page.User) page.Banner { return b }
		f.handler = identity.Require(pages.Handler(f.cfg))
		for _, path := range []string{"/", "/about", "/plain-script/", "/plain-script/runs/" + u.ID + "/"} {
			t.Run(b.Service+path, func(t *testing.T) {
				w := f.request(context.Background(), "GET", path, "owner")
				requireEqual(t, w.Code, http.StatusOK)
				preloadHead(t, written(t, w.Body.String(), b))
			})
		}
	}
}

// R-C7Q5-SKXP
func TestNoticeFontPreload(t *testing.T) {
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []page.Banner{fixedBanner, {Service: "runner-Blue.7", Version: "build+candidate.8"}, {Service: "alternate", Version: "snapshot-42"}} {
		for _, status := range []int{200, 404, 503, 599} {
			t.Run(b.Service+"/"+fmt.Sprint(status), func(t *testing.T) {
				w := httptest.NewRecorder()
				set.Write(w, httptest.NewRequest("GET", "/unrelated", nil), status, "notfound", pages.NoticeData{Banner: b})
				preloadHead(t, w.Body.String())
			})
		}
	}
}

// R-A7LC-2TU3
func TestPlainPageFavicon(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "plain-script")
	u := f.add(t, sc, 501, store.StatusRunning, "", 0, 0)
	for _, b := range []page.Banner{fixedBanner, {Service: "runner-Blue.7", Version: "build+candidate.8"}} {
		f.cfg.Banner = func(page.User) page.Banner { return b }
		f.handler = identity.Require(pages.Handler(f.cfg))
		for _, path := range []string{"/", "/about", "/plain-script/", "/plain-script/runs/" + u.ID + "/"} {
			t.Run(b.Service+path, func(t *testing.T) {
				w := f.request(context.Background(), "GET", path, "owner")
				requireEqual(t, w.Code, http.StatusOK)
				faviconHead(t, written(t, w.Body.String(), b))
			})
		}
	}
}

// R-AA14-UDBH
func TestNoticeFavicon(t *testing.T) {
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []page.Banner{
		fixedBanner,
		{Service: "runner-Blue.7", Version: "build+candidate.8"},
		{Service: "alternate", Version: "snapshot-42"},
	} {
		for _, status := range []int{200, 404, 503, 599} {
			t.Run(b.Service+"/"+fmt.Sprint(status), func(t *testing.T) {
				w := httptest.NewRecorder()
				set.Write(w, httptest.NewRequest("GET", "/unrelated", nil), status, "notfound", pages.NoticeData{Banner: b})
				faviconHead(t, w.Body.String())
			})
		}
	}
}

func launcherScripts(t *testing.T, body string) {
	t.Helper()
	launchers := 0
	for _, e := range elements(body, "script") {
		if hasAttr(e, "src", "/_appkit/launcher.js") {
			launchers++
		} else {
			attr(t, e, "src", "/_appkit/feedback.js")
		}
	}
	requireEqual(t, launchers, 1)
}
func onlyFeedbackScripts(t *testing.T, body string) {
	t.Helper()
	for _, e := range elements(body, "script") {
		attr(t, e, "src", "/_appkit/feedback.js")
		srcCount := 0
		for _, a := range e.attributes {
			if a.name == "src" {
				srcCount++
			}
		}
		requireEqual(t, srcCount, 1)
		if bearing(e, "href") || bearing(e, "xlink:href") {
			t.Fatal("forbidden script link attribute")
		}
	}
}
func commonHooks(t *testing.T, body, title, heading string) {
	t.Helper()
	titleHooks(t, body, title, heading)
	resourceHooks(t, body)
	feedbackHead(t, body)
	onlyFeedbackScripts(t, body)
	requireEqual(t, len(elements(body, "style")), 0)
	for _, e := range elements(body, "") {
		for _, a := range []string{"style", "srcset", "imagesrcset"} {
			if len(values(e, a)) > 0 {
				t.Fatal(a)
			}
		}
		if tagNamed(e, "meta") && len(values(e, "http-equiv")) > 0 {
			t.Fatal("meta http-equiv")
		}
	}
}
func typedID(t *testing.T, body, tag, id string) element {
	t.Helper()
	found := identified(body, tag, id)
	requireEqual(t, len(found), 1)
	return found[0]
}
func noAttribute(t *testing.T, body, name string) {
	t.Helper()
	for _, e := range elements(body, "") {
		if len(values(e, name)) > 0 {
			t.Fatalf("unexpected %s attribute", name)
		}
	}
}
func breadcrumbs(t *testing.T, body string, wants []pages.ScriptLink) {
	t.Helper()
	nav := one(t, body, "nav")
	if !class(nav, "crumbs") {
		t.Fatal("breadcrumb class")
	}
	attr(t, nav, "aria-label", "Breadcrumb")
	_ = one(t, content(t, nav), "ol")
	lis := elements(nav.body, "li")
	requireEqual(t, len(lis), len(wants))
	requireEqual(t, len(elements(nav.body, "a")), len(wants)-1)
	for i, w := range wants {
		li := lis[i]
		if i == len(wants)-1 {
			attr(t, li, "aria-current", "page")
			requireEqual(t, normalise(content(t, li)), w.Name)
			requireEqual(t, len(elements(li.body, "a")), 0)
		} else {
			a := one(t, content(t, li), "a")
			attr(t, a, "href", w.URL)
			requireEqual(t, normalise(content(t, a)), w.Name)
		}
	}
	for _, e := range elements(body, "") {
		if len(values(e, "aria-current")) > 0 && e.start != nav.end+lis[len(lis)-1].start {
			t.Fatal("aria-current outside final breadcrumb")
		}
	}
}

// R-XQJ1-Z1MD R-XRQY-CTD2 R-XSYU-QL3R R-XU6R-4CUG R-XVEN-I4L5 R-XQ0X-B994 R-XR8T-P0ZT R-XXUG-9O2J R-XSGQ-2SQI R-Y0A9-17JX R-Y2Q1-SR1B R-Y3XY-6IS0 R-Y55U-KAIP R-Y6DQ-Y29E R-Y7LN-BU03 R-Y8TJ-PLQS R-YA1G-3DHH R-YB9C-H586 R-GS90-Y3QG R-YDP5-8OPK R-YEX1-MGG9 R-YG4Y-086Y R-YHCU-DZXN R-YJSN-5JF1 R-YL0J-JB5Q R-YM8F-X2WF R-YOO8-OMDT
func TestCatalogScriptAboutHooks(t *testing.T) {
	f := setup(t)
	b := fixedBanner
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	w := f.request(context.Background(), "GET", "/", "owner")
	body := written(t, w.Body.String(), b)
	commonHooks(t, body, "scripts", "scripts")
	requireEqual(t, len(elements(body, "nav")), 0)
	noAttribute(t, body, "aria-current")
	requireAbsent(t, visibleText(w.Body.String()), strings.Join(strings.Fields(b.Email), " "))
	summary := typedID(t, body, "p", "summary")
	requireEqual(t, summary.tag, "p")
	requireEqual(t, normalise(summary.body), "Python scripts run from repositories that repos holds. An agent creates a script from one of your repositories and runs it; every run keeps its folder, output and outcome here.")
	requireEqual(t, contentTexts(t, body, "h2"), []string{"Your scripts", "MCP tools"})
	section := byID(t, body, "scripts")
	requireEqual(t, section.tag, "section")
	holding(t, normalise(section.body), "Your scripts", "Each one is a repository and a ref. A run checks the ref out and runs main.py.")
	introHeading := elements(body, "h2")[0]
	contained(t, introHeading, section)
	intro := "Each one is a repository and a ref. A run checks the ref out and runs main.py."
	intervalEnd := -1
	for i := introHeading.closeEnd; i <= section.contentEnd; i++ {
		if strings.Contains(normalise(body[introHeading.closeEnd:i]), intro) {
			intervalEnd = i
			break
		}
	}
	if intervalEnd < 0 {
		t.Fatal("missing catalog introduction")
	}
	mainCodes := elements(body[introHeading.closeEnd:intervalEnd], "code")
	requireEqual(t, len(mainCodes), 1)
	requireEqual(t, normalise(content(t, mainCodes[0])), "main.py")
	empty := byID(t, body, "no-scripts")
	requireEqual(t, empty.tag, "div")
	contained(t, empty, section)
	requireEqual(t, normalise(content(t, one(t, content(t, empty), "h3"))), "No scripts yet")
	holding(t, normalise(empty.body), "No scripts yet", "A script you create with the create tool, from one of your repositories, shows up here.")
	some(t, empty.body, "code", func(e element) bool { return e.contentPresent && normalise(e.body) == "create" })
	requireEqual(t, countID(body, "script-list"), 0)
	requireEqual(t, len(occurrenceElements(body, "", "data-script")), 0)
	tools := typedID(t, body, "dl", "tools")
	requireEqual(t, tools.tag, "dl")
	dts := elements(tools.body, "dt")
	dds := elements(tools.body, "dd")
	names := []string{"list", "show", "create", "update", "delete", "subscribe", "unsubscribe", "run", "runs", "result", "cancel"}
	desc := []string{"The scripts you own, by name.", "One of your scripts, with its repository, its ref and its last run.", "Create a script from one of your repositories and a ref.", "Change the ref one of your scripts runs from.", "Delete one of your scripts and every run it has.", "Run one of your scripts each time an event matching a pattern is delivered.", "Stop running one of your scripts on an event it is subscribed to.", "Start a run of one of your scripts and return its id, status and commit.", "The runs of one of your scripts, newest first.", "One run whole: its details, its output so far, and the files it wrote.", "End one of your runs that is still queued or running."}
	requireEqual(t, len(dts), 11)
	requireEqual(t, len(dds), 11)
	for i, n := range names {
		attr(t, dts[i], "data-tool", n)
		requireEqual(t, normalise(one(t, dts[i].body, "code").body), n)
		requireEqual(t, normalise(dts[i].body), n)
		requireEqual(t, normalise(content(t, dds[i])), desc[i])
		if dds[i].start < dts[i].end || (i < 10 && dds[i].start > dts[i+1].start) {
			t.Fatal("tool ordering")
		}
	}
	assertAlternatingPairs(t, tools.body)
	holding(t, visibleText(body), "MCP tools", "Agents manage and run scripts with these tools, through the MCP gateway's call and mutate.", "list")
	about := typedID(t, body, "a", "about-link")
	requireEqual(t, about.tag, "a")
	attr(t, about, "href", "/about")
	requireEqual(t, normalise(about.body), "About scripts")
	sc := f.create(t, "owner", "alpha")
	beta := f.create(t, "owner", "beta")
	if _, _, e := f.cfg.Store.SetRef(context.Background(), beta.ID, "feature/&x"); e != nil {
		t.Fatal(e)
	}
	// Both present and unavailable repositories, with and without a last run.
	alphaRun := f.add(t, sc, 1, store.StatusExited, "", 12, 0)
	w = f.request(context.Background(), "GET", "/", "owner")
	body = written(t, w.Body.String(), b)
	table := byID(t, body, "script-list")
	contained(t, table, byID(t, body, "scripts"))
	requireEqual(t, table.tag, "table")
	requireEqual(t, contentTexts(t, content(t, table), "th"), []string{"Script", "Repository", "Ref", "Last run", "When"})
	rows := occurrenceElements(body, "", "data-script")
	requireEqual(t, len(rows), 2)
	requireEqual(t, countID(body, "no-scripts"), 0)
	for i, row := range rows {
		requireEqual(t, row.tag, "tr")
		contained(t, row, table)
		name := []string{"alpha", "beta"}[i]
		attr(t, row, "data-script", name)
		a := one(t, row.body, "a")
		if !class(a, "script-link") {
			t.Fatal("script-link")
		}
		attr(t, a, "href", "/"+name+"/")
		requireEqual(t, normalise(a.body), name)
		repo := ofClass(row.body, "td", "script-repo")
		requireEqual(t, len(repo), 1)
		span := one(t, repo[0].body, "span")
		attr(t, span, "title", sc.Repo)
		requireEqual(t, normalise(span.body), "Repository")
		if class(span, "muted") {
			t.Fatal("named repo muted")
		}
		ref := ofClass(row.body, "td", "script-ref")
		requireEqual(t, len(ref), 1)
		wantRef := "main"
		if i == 1 {
			wantRef = "feature/&x"
		}
		requireEqual(t, normalise(ref[0].body), wantRef)
		lastCells := ofClass(row.body, "td", "script-last")
		whenCells := ofClass(row.body, "td", "script-when")
		requireEqual(t, len(lastCells), 1)
		requireEqual(t, len(whenCells), 1)
		last, when := lastCells[0], whenCells[0]
		if i == 0 {
			status := one(t, last.body, "span")
			if !class(status, "status") {
				t.Fatal("catalog last-run status class")
			}
			attr(t, status, "data-kind", "ok")
			requireEqual(t, normalise(status.body), "exited 0")
			tt := one(t, when.body, "time")
			attr(t, tt, "datetime", alphaRun.Started.UTC().Format(time.RFC3339))
			requireEqual(t, normalise(tt.body), alphaRun.Started.UTC().Format("2006-01-02 15:04"))
		} else {
			if !class(last, "muted") {
				t.Fatal("never-run muted")
			}
			requireEqual(t, normalise(content(t, last)), "never run")
			requireEqual(t, len(elements(last.body, "span")), 0)
			requireEqual(t, normalise(content(t, when)), "")
			requireEqual(t, len(elements(when.body, "time")), 0)
		}
	}
	requireAbsent(t, w.Body.String(), sc.ID, beta.ID, alphaRun.ID, alphaRun.SHA)
	requireAbsent(t, normalise(w.Body.String()), sc.Repo)
	w = f.request(context.Background(), "GET", "/beta/", "owner")
	body = written(t, w.Body.String(), b)
	commonHooks(t, body, "beta · scripts", "beta")
	breadcrumbs(t, body, []pages.ScriptLink{{Name: "scripts", URL: "/"}, {Name: "beta"}})
	verifyScriptCard(t, body, beta, 0, "Repository", "feature/&x")
	noRuns := typedID(t, typedID(t, body, "section", "runs").body, "div", "no-runs")
	requireEqual(t, noRuns.tag, "div")
	requireEqual(t, normalise(one(t, noRuns.body, "h3").body), "No runs yet")
	holding(t, normalise(noRuns.body), "No runs yet", "Runs started with the run tool show up here, newest first.")
	requireEqual(t, countID(body, "run-list"), 0)
	requireEqual(t, len(occurrenceElements(body, "", "data-run")), 0)
	for i, st := range []string{store.StatusRunning, store.StatusExited, store.StatusTimedOut, store.StatusKilled, store.StatusFailed} {
		reason := ""
		if st == store.StatusFailed {
			reason = store.ReasonStartFailed
		}
		exit := 0
		if st == store.StatusExited {
			exit = 7
		}
		f.add(t, sc, i+2, st, reason, 221, exit)
	}
	w = f.request(context.Background(), "GET", "/alpha/", "owner")
	body = written(t, w.Body.String(), b)
	verifyScriptCard(t, body, sc, 6, "Repository", "main")
	runTable := typedID(t, body, "table", "run-list")
	contained(t, runTable, typedID(t, body, "section", "runs"))
	requireEqual(t, runTable.tag, "table")
	requireEqual(t, contentTexts(t, content(t, runTable), "th"), []string{"Run", "Status", "Commit", "Started", "Duration", "Exit"})
	rows = occurrenceElements(body, "", "data-run")
	records, e := f.cfg.Store.Runs(context.Background(), sc.ID)
	if e != nil {
		t.Fatal(e)
	}
	requireEqual(t, len(rows), len(records))
	requireEqual(t, countID(body, "no-runs"), 0)
	for i, row := range rows {
		u := records[i]
		requireEqual(t, row.tag, "tr")
		contained(t, row, runTable)
		attr(t, row, "data-run", u.ID)
		a := one(t, row.body, "a")
		if !class(a, "run-link") {
			t.Fatal("run link")
		}
		attr(t, a, "href", "/alpha/runs/"+u.ID+"/")
		requireEqual(t, normalise(a.body), u.ID)
		word, kind := "failed", "err"
		dur, exit := "", ""
		switch u.Status {
		case store.StatusRunning:
			word, kind = "running", "info"
		case store.StatusExited:
			word, kind = "exited "+fmt.Sprint(u.ExitCode), "warn"
			if u.ExitCode == 0 {
				kind = "ok"
			}
			exit = fmt.Sprint(u.ExitCode)
		case store.StatusTimedOut:
			word, kind = "timed out", "warn"
		case store.StatusKilled:
			word, kind = "killed", "warn"
		}
		if u.Status != store.StatusRunning && u.Status != store.StatusFailed {
			seconds := int64(u.Finished.Sub(u.Started) / time.Second)
			dur = fmt.Sprintf("%ds", seconds)
			if seconds >= 60 {
				dur = fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
			}
		}
		statuses := ofClass(row.body, "span", "status")
		requireEqual(t, len(statuses), 1)
		status := statuses[0]
		attr(t, status, "data-kind", kind)
		requireEqual(t, normalise(status.body), word)
		sha := ""
		if u.SHA != "" {
			sha = u.SHA[:7]
		}
		cells := ofClass(row.body, "td", "run-sha")
		requireEqual(t, len(cells), 1)
		requireEqual(t, normalise(content(t, cells[0])), sha)
		tt := one(t, row.body, "time")
		attr(t, tt, "datetime", u.Started.UTC().Format(time.RFC3339))
		requireEqual(t, normalise(tt.body), u.Started.UTC().Format("2006-01-02 15:04"))
		cells = ofClass(row.body, "td", "run-duration")
		requireEqual(t, len(cells), 1)
		requireEqual(t, normalise(content(t, cells[0])), dur)
		cells = ofClass(row.body, "td", "run-exit")
		requireEqual(t, len(cells), 1)
		requireEqual(t, normalise(content(t, cells[0])), exit)
	}
	// An absent ikigenba.name and an absent directory share the gone treatment.
	writeFile(t, filepath.Join(f.cfg.Source.RepoDir(sc.Repo), "config"), "[core]\n\tbare = true\n")
	for _, remove := range []bool{false, true} {
		if remove {
			if e := os.RemoveAll(f.cfg.Source.RepoDir(sc.Repo)); e != nil {
				t.Fatal(e)
			}
		}
		w = f.request(context.Background(), "GET", "/", "owner")
		row := occurrenceElements(byID(t, w.Body.String(), "script-list").body, "tr", "data-script")[0]
		span := one(t, ofClass(row.body, "td", "script-repo")[0].body, "span")
		if !class(span, "muted") {
			t.Fatal("gone muted")
		}
		attr(t, span, "title", "the repository is gone")
		requireEqual(t, normalise(span.body), sc.Repo)
		w = f.request(context.Background(), "GET", "/alpha/", "owner")
		verifyScriptCard(t, w.Body.String(), sc, 6, "", "main")
	}
	w = f.request(context.Background(), "GET", "/about", "owner")
	body = written(t, w.Body.String(), b)
	commonHooks(t, body, "About scripts", "About scripts")
	dl := typedID(t, body, "dl", "about")
	requireEqual(t, dl.tag, "dl")
	assertAlternatingPairs(t, dl.body)
	requireEqual(t, contentTexts(t, content(t, dl), "dt"), []string{"Name", "Version", "Description"})
	dds = elements(dl.body, "dd")
	requireEqual(t, len(dds), 3)
	for i, id := range []string{"about-name", "about-version", "about-description"} {
		attr(t, dds[i], "id", id)
		requireEqual(t, normalise(content(t, dds[i])), []string{b.Service, b.Version, pages.Description}[i])
	}
	home := typedID(t, body, "a", "home-link")
	requireEqual(t, home.tag, "a")
	attr(t, home, "href", "/")
	requireEqual(t, normalise(home.body), "Back to scripts")
	requireEqual(t, len(elements(body, "h2")), 0)
	requireEqual(t, len(elements(body, "nav")), 0)
	noAttribute(t, body, "aria-current")
	for _, id := range []string{"scripts", "script-list", "no-scripts", "tools"} {
		requireEqual(t, countID(body, id), 0)
	}
	// Services are banner input, and scripts adds no launcher itself.
	f.cfg.Banner = func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Version: fixedBanner.Version, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Services: []page.Service{{Name: "scripts", URL: "https://scripts.example", Enabled: true, Current: true}}}
	}
	h := identity.Require(pages.Handler(f.cfg))
	for _, path := range []string{"/", "/about", "/alpha/", "/alpha/runs/" + alphaRun.ID + "/"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-User-Id", "owner")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		requireEqual(t, len(ofClass(rec.Body.String(), "button", "launcher")), 1)
		launcherScripts(t, rec.Body.String())
	}
}

// R-XULY-DRGD
func verifyScriptCard(t *testing.T, body string, sc store.Script, n int, repo, ref string) {
	t.Helper()
	about := typedID(t, body, "p", "about-script")
	requireEqual(t, about.tag, "p")
	repoText := repo
	if repoText == "" {
		repoText = sc.Repo
	}
	requireEqual(t, normalise(about.body), "Runs main.py from the repository "+repoText+" at "+ref+".")
	requireEqual(t, contentTexts(t, content(t, about), "code"), []string{"main.py", ref})
	if repo != "" {
		requireEqual(t, normalise(one(t, about.body, "strong").body), repo)
		requireEqual(t, len(ofClass(about.body, "span", "muted")), 0)
	} else {
		requireEqual(t, len(elements(about.body, "strong")), 0)
		sp := ofClass(about.body, "span", "muted")
		requireEqual(t, len(sp), 1)
		attr(t, sp[0], "title", "the repository is gone")
		requireEqual(t, normalise(sp[0].body), sc.Repo)
	}
	card := typedID(t, body, "section", "script-card")
	requireEqual(t, card.tag, "section")
	holding(t, normalise(card.body), "Script", "Changed by an agent through the update tool.")
	dl := typedID(t, card.body, "dl", "script")
	requireEqual(t, dl.tag, "dl")
	assertAlternatingPairs(t, dl.body)
	requireEqual(t, contentTexts(t, content(t, dl), "dt"), []string{"Id", "Repository", "Ref", "Created", "Runs kept"})
	dds := elements(dl.body, "dd")
	requireEqual(t, len(dds), 5)
	requireEqual(t, normalise(content(t, dds[0])), sc.ID)
	some(t, dds[0].body, "code", func(e element) bool { return e.contentPresent && normalise(e.body) == sc.ID })
	if repo != "" {
		text := normalise(content(t, dds[1]))
		if !strings.HasPrefix(text, repo) {
			t.Fatal("repository name prefix")
		}
		holding(t, text, repo, sc.Repo)
	} else {
		requireEqual(t, normalise(content(t, dds[1])), sc.Repo)
		some(t, dds[1].body, "span", func(e element) bool { return class(e, "muted") && hasAttr(e, "title", "the repository is gone") })
	}
	some(t, dds[1].body, "code", func(e element) bool { return e.contentPresent && normalise(e.body) == sc.Repo })
	requireEqual(t, normalise(content(t, dds[2])), ref)
	some(t, dds[2].body, "code", func(e element) bool { return e.contentPresent && normalise(e.body) == ref })
	requireEqual(t, normalise(content(t, dds[3])), sc.Created.UTC().Format("2006-01-02 15:04 UTC"))
	some(t, dds[3].body, "time", func(e element) bool { return hasAttr(e, "datetime", sc.Created.UTC().Format(time.RFC3339)) })
	requireEqual(t, normalise(content(t, dds[4])), fmt.Sprintf("%d · the newest 10 are kept past 30 days", n))
	runsSection := typedID(t, body, "section", "runs")
	requireEqual(t, runsSection.tag, "section")
	holding(t, normalise(runsSection.body), "Runs", "Newest first. A run still running shows its progress when the page is reloaded.")
}

// R-YPW5-2E4I R-7W4I-OKIE R-YSBX-TXLW R-YTJU-7PCL R-XVTU-RJ72 R-XX1R-5AXR R-O0JI-4BYG R-YYFF-QSBD
func verifyRunHooks(t *testing.T, body string, d pages.RunData) {
	t.Helper()
	body = written(t, body, d.Banner)
	c := d.Run
	commonHooks(t, body, c.ID+" · "+d.Script.Name+" · scripts", c.ID)
	breadcrumbs(t, body, []pages.ScriptLink{{Name: "scripts", URL: "/"}, d.Script, {Name: c.ID}})
	headline := typedID(t, body, "p", "headline")
	requireEqual(t, headline.tag, "p")
	st := ofClass(headline.body, "span", "status")
	requireEqual(t, len(st), 1)
	attr(t, st[0], "data-kind", c.Kind)
	requireEqual(t, normalise(content(t, st[0])), c.Status)
	muted := ofClass(headline.body, "span", "muted")
	requireEqual(t, len(muted), 1)
	tt := one(t, muted[0].body, "time")
	attr(t, tt, "datetime", c.StartedAt)
	want := c.Started
	if c.Failure == nil {
		want = "started " + want
		if c.Duration != "" {
			want += " · " + c.Duration
		}
	}
	requireEqual(t, normalise(content(t, muted[0])), want)
	if c.Running {
		running := typedID(t, body, "div", "running")
		requireEqual(t, running.tag, "div")
		attr(t, running, "data-kind", "info")
		requireEqual(t, normalise(typedID(t, running.body, "strong", "notice").body), d.Run.Notice)
		holding(t, normalise(running.body), d.Run.Notice, "The page does not stream. Reload it to see more output.")
		reload := typedID(t, body, "a", "reload")
		requireEqual(t, reload.tag, "a")
		attr(t, reload, "href", c.URL)
		requireEqual(t, normalise(reload.body), "Reload")
	} else {
		requireEqual(t, countID(body, "running"), 0)
		requireEqual(t, countID(body, "reload"), 0)
	}
	if c.Failure != nil {
		fail := typedID(t, body, "div", "failure")
		requireEqual(t, fail.tag, "div")
		attr(t, fail, "data-kind", "err")
		holding(t, normalise(fail.body), c.Failure.Title, c.Failure.Reason)
	} else {
		requireEqual(t, countID(body, "failure"), 0)
	}
	card := typedID(t, body, "section", "run-card")
	requireEqual(t, card.tag, "section")
	if c.Trigger == store.TriggerManual {
		holding(t, normalise(card.body), "Run", "Started by the run tool.")
	}
	dl := typedID(t, card.body, "dl", "run")
	requireEqual(t, dl.tag, "dl")
	wantDT := []string{"Status", "Script", "Commit", "Ref", "Started"}
	wantDD := []string{c.Status, d.Script.Name, c.Commit, c.Ref, c.Started}
	if c.Finished != "" {
		wantDT = append(wantDT, "Finished")
		wantDD = append(wantDD, c.Finished)
	}
	if c.Duration != "" {
		wantDT = append(wantDT, "Duration")
		wantDD = append(wantDD, c.Duration)
	}
	wantDT = append(wantDT, "Trigger", "User", "Request", "Output")
	wantDD = append(wantDD, c.Trigger, c.User, c.Request, "stdout "+c.StdoutSize+" · stderr "+c.StderrSize)
	requireEqual(t, contentTexts(t, content(t, dl), "dt"), wantDT)
	dds := runFactDDs(t, content(t, dl))
	for i := 2; i < len(wantDD); i++ {
		requireEqual(t, normalise(content(t, dds[i])), wantDD[i])
	}
	dts := elements(content(t, dl), "dt")
	for i, dt := range dts {
		if normalise(dt.body) == "Started" {
			some(t, dds[i].body, "time", func(e element) bool { return hasAttr(e, "datetime", c.StartedAt) })
		}
		if normalise(dt.body) == "Finished" {
			some(t, dds[i].body, "time", func(e element) bool { return hasAttr(e, "datetime", c.FinishedAt) })
		}
	}
	a := one(t, dds[1].body, "a")
	attr(t, a, "href", d.Script.URL)
	requireEqual(t, normalise(a.body), d.Script.Name)
	if c.Commit != "" {
		requireEqual(t, normalise(content(t, one(t, content(t, dds[2]), "code"))), c.Commit)
	} else {
		requireEqual(t, len(elements(dds[2].body, "code")), 0)
	}
	requireEqual(t, normalise(content(t, one(t, content(t, dds[3]), "code"))), c.Ref)
	some(t, dds[len(dds)-2].body, "code", func(e element) bool { return e.contentPresent && normalise(e.body) == c.Request })
	status := ofClass(dds[0].body, "span", "status")
	requireEqual(t, len(status), 1)
	attr(t, status[0], "data-kind", c.Kind)
	requireEqual(t, normalise(content(t, status[0])), c.Status)
	badges := ofClass(body, "span", "badge")
	if c.Truncated {
		requireEqual(t, len(badges), 1)
		attr(t, badges[0], "data-kind", "warn")
		requireEqual(t, normalise(badges[0].body), "output truncated")
		requireEqual(t, len(ofClass(dds[0].body, "span", "badge")), 1)
	} else {
		requireEqual(t, len(badges), 0)
	}
	requireAbsent(t, visibleText(body), "(truncated)")
	if c.FilesGone {
		gone := typedID(t, body, "div", "files-gone")
		requireEqual(t, gone.tag, "div")
		holding(t, normalise(gone.body), "Files no longer kept", "This run's folder is gone, so its input, output and files cannot be shown. The record stays.")
		for _, id := range []string{"input", "stdout", "stderr", "files"} {
			requireEqual(t, countID(body, id), 0)
		}
		for _, e := range elements(body, "") {
			if bearing(e, "download") {
				t.Fatal("download on gone folder")
			}
		}
		return
	}
	requireEqual(t, countID(body, "files-gone"), 0)
	for _, x := range []struct {
		id, heading, name, empty string
		file                     *pages.FileText
	}{{"input", "Input", "input.json", "No input.", d.Input}, {"stdout", "Standard output", "stdout", "No output.", d.Stdout}, {"stderr", "Standard error", "stderr", "No output.", d.Stderr}} {
		if x.file == nil {
			requireEqual(t, countID(body, x.id), 0)
			continue
		}
		section := typedID(t, body, "section", x.id)
		requireEqual(t, section.tag, "section")
		requireEqual(t, normalise(one(t, section.body, "h2").body), x.heading)
		ps := elements(section.body, "p")
		normal := []element{}
		muted := []element{}
		for _, p := range ps {
			if class(p, "muted") {
				muted = append(muted, p)
			} else {
				normal = append(normal, p)
			}
		}
		requireEqual(t, len(normal), 1)
		requireEqual(t, normalise(normal[0].body), x.name+", "+x.file.Size)
		downloads := bearingElements(section.body, "a", "download")
		requireEqual(t, len(downloads), 1)
		a := downloads[0]
		attr(t, a, "href", x.file.URL)
		if !bearing(a, "download") {
			t.Fatal("download attribute")
		}
		if !class(a, "button") {
			t.Fatal("download button")
		}
		requireEqual(t, normalise(a.body), "Download")
		if x.file.Text == "" {
			requireEqual(t, len(elements(section.body, "pre")), 0)
			requireEqual(t, len(muted), 1)
			requireEqual(t, normalise(content(t, muted[0])), x.empty)
		} else {
			requireEqual(t, len(muted), 0)
			pre := one(t, section.body, "pre")
			requireEqual(t, html.UnescapeString(stripCodeTags(content(t, pre))), x.file.Text)
		}
	}
	files := typedID(t, body, "section", "files")
	requireEqual(t, files.tag, "section")
	holding(t, normalise(files.body), "Files", "What the script wrote under out/. Each downloads as a file.")
	if len(d.Files) == 0 {
		some(t, files.body, "p", func(e element) bool {
			return hasAttr(e, "id", "no-files") && e.contentPresent && normalise(e.body) == "No files."
		})
		requireEqual(t, countID(body, "file-list"), 0)
		requireEqual(t, len(occurrenceElements(body, "", "data-file")), 0)
		return
	}
	requireEqual(t, countID(body, "no-files"), 0)
	table := typedID(t, files.body, "table", "file-list")
	requireEqual(t, table.tag, "table")
	rows := occurrenceElements(table.body, "tr", "data-file")
	requireEqual(t, len(rows), len(d.Files))
	for i, row := range rows {
		x := d.Files[i]
		attr(t, row, "data-file", x.Path)
		requireEqual(t, normalise(one(t, row.body, "code").body), x.Path)
		some(t, row.body, "td", func(e element) bool { return class(e, "file-size") && e.contentPresent && normalise(e.body) == x.Size })
		downloads := bearingElements(row.body, "a", "download")
		requireEqual(t, len(downloads), 1)
		a := downloads[0]
		attr(t, a, "href", x.URL)
		if !bearing(a, "download") {
			t.Fatal("file download")
		}
		if !class(a, "button") {
			t.Fatal("file button")
		}
		requireEqual(t, normalise(a.body), "Download")
	}
}

func noticeHooks(t *testing.T, body string, b page.Banner) {
	t.Helper()
	footer := appkitMarkup(t, "footer", b)
	ends := tagSpans(body, "body", true)
	if len(ends) == 0 {
		t.Fatal("absent notice body end")
	}
	end := ends[len(ends)-1][0]
	for end > 0 && asciiSpace(rune(body[end-1])) {
		end--
	}
	start := end - len(footer)
	if start < 0 || body[start:end] != footer {
		t.Fatal("notice footer not followed by only ASCII whitespace")
	}
	for _, tag := range []string{"header", "form", "style"} {
		requireEqual(t, len(elements(body, tag)), 0)
	}
	requireEqual(t, len(ofClass(body, "strong", "mark")), 0)
	requireEqual(t, len(ofClass(body, "a", "profile")), 0)
	noAttribute(t, body, "style")
	feedbackHead(t, body)
	onlyFeedbackScripts(t, body)
	resourceHooks(t, body)
	titleHooks(t, body, "Not found", "Not found")
	notice := typedID(t, body, "p", "notfound")
	requireEqual(t, normalise(content(t, notice)), "There is nothing at this address.")
}

// R-YZNC-4K22 R-XTOM-GKH7 R-XUWI-UC7W R-Z234-W3JG R-Z3B1-9VA5
func TestDirectNoticeHooks(t *testing.T) {
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []page.Banner{
		fixedBanner,
		{Service: "runner-Blue.7", Version: "build+candidate.8"},
		{Service: "alternate", Version: "snapshot-42"},
	} {
		for _, status := range []int{200, 404, 503, 599} {
			t.Run(b.Service+"/"+b.Version+"/"+fmt.Sprint(status), func(t *testing.T) {
				w := httptest.NewRecorder()
				set.Write(w, httptest.NewRequest("GET", "/unrelated", nil), status, "notfound", pages.NoticeData{Banner: b})
				noticeHooks(t, w.Body.String(), b)
			})
		}
	}
}

// R-XRQY-CTD2 R-XU6R-4CUG R-XVEN-I4L5 R-XQ0X-B994 R-XR8T-P0ZT R-XXUG-9O2J R-XSGQ-2SQI R-Y0A9-17JX R-Y2Q1-SR1B R-YG4Y-086Y R-YHCU-DZXN
func TestAllPageCommonHooks(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "plain-script")
	u := f.add(t, sc, 501, store.StatusRunning, "", 0, 0)
	for _, base := range []page.Banner{fixedBanner, {Service: "runner-Blue.7", Version: "build+candidate.8"}, {Service: "runner <Blue>&", Version: "build candidate"}} {
		for _, services := range [][]page.Service{nil, {{Name: "scripts", URL: "https://scripts.example", Enabled: true, Current: true}, {Name: "other", URL: "https://other.example", Enabled: true, Icon: "plain-icon"}}, {{Name: "scripts", URL: "https://scripts.example", Enabled: true, Current: true, Icon: template.HTML(`<svg><path d="M1 1h2v2H1z"/></svg>`)}}} {
			if base.Service == "runner <Blue>&" && len(services) == 0 {
				continue
			}
			b := base
			b.Services = services
			if len(services) > 0 {
				b.Icon = services[0].Icon
			}
			f.cfg.Banner = func(user page.User) page.Banner {
				answer := b
				answer.Email, answer.ProfileURL, answer.LogoutURL = user.Email, user.ProfileURL, user.LogoutURL
				return answer
			}
			h := identity.Require(pages.Handler(f.cfg))
			for _, c := range []struct {
				path, title, heading string
				crumbs               []pages.ScriptLink
			}{
				{"/", "scripts", "scripts", nil},
				{"/about", "About scripts", "About scripts", nil},
				{"/plain-script/", "plain-script · scripts", "plain-script", []pages.ScriptLink{{Name: "scripts", URL: "/"}, {Name: sc.Name}}},
				{"/plain-script/runs/" + u.ID + "/", u.ID + " · plain-script · scripts", u.ID, []pages.ScriptLink{{Name: "scripts", URL: "/"}, {Name: sc.Name, URL: "/plain-script/"}, {Name: u.ID}}},
			} {
				t.Run(b.Service+"/"+fmt.Sprint(len(services))+c.path, func(t *testing.T) {
					request := httptest.NewRequest("GET", c.path, nil)
					request.Host = "scripts.sbx.example:443"
					request.Header.Set("X-User-Id", "owner")
					request.Header.Set("X-User-Email", "person@example.com")
					request.Header.Set("X-Forwarded-Proto", "https")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, request)
					requireEqual(t, w.Code, 200)
					whole := w.Body.String()
					banner := f.cfg.Banner(page.User{Email: "person@example.com", ProfileURL: "https://auth.sbx.example/", LogoutURL: "https://auth.sbx.example/logout"})
					bannerHooks(t, whole, banner)
					body := written(t, whole, banner)
					if len(services) > 0 {
						requireEqual(t, len(ofClass(whole, "button", "launcher")), 1)
						launcherScripts(t, whole)
						return
					}
					requireEqual(t, len(ofClass(whole, "button", "launcher")), 0)
					for _, e := range elements(whole, "script") {
						attr(t, e, "src", "/_appkit/feedback.js")
					}
					requireEqual(t, len(elements(whole, "input")), 0)
					requireAbsent(t, whole, "/_appkit/launcher.js")
					requireAbsent(t, visibleText(whole), strings.Join(strings.Fields(request.Header.Get("X-User-Email")), " "))
					commonHooks(t, body, c.title, c.heading)
					if len(c.crumbs) == 0 {
						requireEqual(t, len(elements(body, "nav")), 0)
						noAttribute(t, body, "aria-current")
					} else {
						breadcrumbs(t, body, c.crumbs)
					}
					if c.path == "/about" {
						dl := typedID(t, body, "dl", "about")
						assertAlternatingPairs(t, content(t, dl))
						requireEqual(t, contentTexts(t, content(t, dl), "dt"), []string{"Name", "Version", "Description"})
						dds := elements(dl.body, "dd")
						requireEqual(t, len(dds), 3)
						for i, id := range []string{"about-name", "about-version", "about-description"} {
							attr(t, dds[i], "id", id)
							requireEqual(t, normalise(content(t, dds[i])), []string{b.Service, b.Version, pages.Description}[i])
						}
						home := typedID(t, body, "a", "home-link")
						attr(t, home, "href", "/")
						requireEqual(t, normalise(content(t, home)), "Back to scripts")
						requireEqual(t, len(elements(body, "h2")), 0)
						for _, id := range []string{"scripts", "script-list", "no-scripts", "tools"} {
							requireEqual(t, countID(body, id), 0)
						}
					}
				})
			}
		}
	}
}

// R-YZNC-4K22 R-XTOM-GKH7 R-XUWI-UC7W R-Z234-W3JG R-Z3B1-9VA5
func TestNoticeAndEmptyRunFiles(t *testing.T) {
	f := setup(t)
	w := f.request(context.Background(), "GET", "/missing/", "owner")
	body := w.Body.String()
	noticeHooks(t, body, fixedBanner)
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
	d := pages.RunData{Banner: b, Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: pages.RunCard{ID: u.ID, URL: path, Status: "running", Kind: "info", Running: true, Notice: "Running", Commit: u.SHA, Ref: "main", Started: u.Started.UTC().Format("2006-01-02 15:04:05 UTC"), StartedAt: u.Started.UTC().Format(time.RFC3339), Trigger: "manual", User: "owner", Request: u.RequestID, StdoutSize: "0 B", StderrSize: "0 B"}}
	w = f.request(context.Background(), "GET", path, "owner")
	requireEqual(t, w.Body.String(), rendered(t, "run", d))
	verifyRunHooks(t, w.Body.String(), d)
	for _, name := range []string{runs.InputFile, runs.StdoutFile, runs.StderrFile} {
		writeFile(t, filepath.Join(folder, name), "")
	}
	d.Input = &pages.FileText{Size: "0 B", URL: path + runs.InputFile}
	d.Stdout = &pages.FileText{Size: "0 B", URL: path + runs.StdoutFile}
	d.Stderr = &pages.FileText{Size: "0 B", URL: path + runs.StderrFile}
	w = f.request(context.Background(), "GET", path, "owner")
	requireEqual(t, w.Body.String(), rendered(t, "run", d))
	verifyRunHooks(t, w.Body.String(), d)
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

func holding(t *testing.T, s string, values ...string) {
	t.Helper()
	for _, v := range values {
		i := strings.Index(s, v)
		if i < 0 {
			t.Fatalf("missing ordered %q in %q", v, s)
		}
		s = s[i+len(v):]
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

// R-SXJJ-ZGSJ R-SSNY-GDTR R-7UWM-ASRP
func TestSizeBoundaryAndDurationFormatting(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	for i, c := range []struct {
		bytes    int64
		text     string
		seconds  int64
		duration string
	}{{0, "0 B", 0, "0s"}, {69, "69 B", 12, "12s"}, {999, "999 B", 59, "59s"}, {1000, "1.0 kB", 60, "1m 0s"}, {1229, "1.2 kB", 221, "3m 41s"}, {12034, "12.0 kB", 600, "10m 0s"}, {999999, "999.9 kB", 1, "1s"}, {1000000, "1.0 MB", 2, "2s"}, {1048576, "1.0 MB", 3, "3s"}} {
		u := f.add(t, sc, i+1, store.StatusRunning, "", 0, 0)
		u, e := f.cfg.Store.FinishRun(context.Background(), u.ID, store.Ending{Status: store.StatusExited, ExitCode: 0, Finished: u.Started.Add(time.Duration(c.seconds) * time.Second), StdoutBytes: c.bytes, StderrBytes: c.bytes})
		if e != nil {
			t.Fatal(e)
		}
		w := f.request(context.Background(), "GET", "/alpha/runs/"+u.ID+"/", "owner")
		dl := byID(t, w.Body.String(), "run")
		dt := elements(dl.body, "dt")
		dd := runFactDDs(t, content(t, dl))
		for j, d := range dt {
			switch normalise(d.body) {
			case "Output":
				requireEqual(t, normalise(content(t, dd[j])), "stdout "+c.text+" · stderr "+c.text)
			case "Duration":
				requireEqual(t, normalise(content(t, dd[j])), c.duration)
			}
		}
	}
}

func assertAlternatingPairs(t *testing.T, body string) {
	t.Helper()
	var tags []string
	for _, e := range elements(body, "") {
		if e.tag == "dt" || e.tag == "dd" {
			tags = append(tags, e.tag)
		}
	}
	for i, tag := range tags {
		want := "dt"
		if i%2 == 1 {
			want = "dd"
		}
		requireEqual(t, tag, want)
	}
	requireEqual(t, len(tags)%2, 0)
}

// R-70GP-VANL R-72WI-MU4Z R-77S4-5X3R
func TestScriptSubscriptions(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	check := func(names []string) {
		t.Helper()
		w := f.request(context.Background(), "GET", "/alpha/", "owner")
		requireEqual(t, w.Code, 200)
		section := typedID(t, w.Body.String(), "section", "subscriptions")
		requireEqual(t, contentTexts(t, section.body, "h2"), []string{"Subscriptions"})
		if len(names) == 0 {
			empty := typedID(t, w.Body.String(), "div", "no-subscriptions")
			contained(t, empty, section)
			if !class(empty, "empty") {
				t.Fatal("missing empty class")
			}
			requireEqual(t, normalise(empty.body), "This script is subscribed to no events.")
			requireEqual(t, countID(w.Body.String(), "subscription-list"), 0)
			requireEqual(t, len(occurrenceElements(w.Body.String(), "", "data-event")), 0)
			return
		}
		list := typedID(t, w.Body.String(), "ul", "subscription-list")
		contained(t, list, section)
		requireEqual(t, countID(w.Body.String(), "no-subscriptions"), 0)
		items := occurrenceElements(w.Body.String(), "", "data-event")
		requireEqual(t, len(items), len(names))
		for i, item := range items {
			requireEqual(t, item.tag, "li")
			contained(t, item, list)
			attr(t, item, "data-event", names[i])
			requireEqual(t, normalise(one(t, item.body, "code").body), names[i])
		}
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

// R-7TOP-X110 R-7UWM-ASRP R-7900-JOUG
func TestRunEventOrigin(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	manual := f.add(t, sc, 1, store.StatusRunning, "", 0, 0)
	event, err := f.cfg.Store.AddRun(context.Background(), store.Run{ID: "run_0000000000000002", Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: sc.Owner, RequestID: "event-request", Trigger: store.TriggerEvent, Event: "evt_0123456789abcdef", Status: store.StatusRunning, Started: fixedTime})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []store.Run{manual, event} {
		w := f.request(context.Background(), "GET", "/alpha/runs/"+u.ID+"/", "owner")
		requireEqual(t, w.Code, 200)
		card := typedID(t, w.Body.String(), "section", "run-card")
		p := typedID(t, w.Body.String(), "p", "started-by")
		contained(t, p, card)
		code := one(t, p.body, "code")
		if u.Event == "" {
			requireEqual(t, normalise(p.body), "Started by the run tool.")
			requireEqual(t, normalise(code.body), "run")
		} else {
			requireEqual(t, normalise(p.body), "Started by the event "+u.Event+".")
			requireEqual(t, normalise(code.body), u.Event)
			requireAbsent(t, visibleText(w.Body.String()), "Started by the run tool.")
		}
	}
}

// R-SSNY-GDTR R-SXJJ-ZGSJ R-7UWM-ASRP R-7W4I-OKIE
func TestQueuedPage(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	u := f.add(t, sc, 90, store.StatusQueued, "", 0, 0)
	path := "/alpha/runs/" + u.ID + "/"
	writeFile(t, filepath.Join(f.cfg.Runs.Folder(u), runs.InputFile), "{}")
	w := f.request(context.Background(), "GET", path, "owner")
	requireEqual(t, w.Code, 200)
	body := w.Body.String()
	requireEqual(t, normalise(typedID(t, body, "strong", "notice").body), "Queued")
	running := typedID(t, body, "div", "running")
	attr(t, running, "data-kind", "info")
	holding(t, normalise(running.body), "Queued", "The page does not stream. Reload it to see more output.")
	attr(t, typedID(t, body, "a", "reload"), "href", path)
	requireEqual(t, countID(body, "stdout"), 0)
	requireEqual(t, countID(body, "stderr"), 0)
	requireEqual(t, normalise(typedID(t, body, "p", "no-files").body), "No files.")
	facts := typedID(t, body, "dl", "run")
	requireAbsent(t, normalise(facts.body), "Finished", "Duration")
	requireContains(t, normalise(facts.body), "stdout 0 B · stderr 0 B")
	requireEqual(t, normalise(typedID(t, body, "p", "headline").body), "queued started "+u.Started.UTC().Format("2006-01-02 15:04:05 UTC"))
	for _, path := range []string{"/", "/alpha/"} {
		body = f.request(context.Background(), "GET", path, "owner").Body.String()
		some(t, body, "span", func(e element) bool { return hasAttr(e, "data-kind", "info") && normalise(e.body) == "queued" })
	}
}

// R-IAR4-R2M2
func TestAboutShowsDisplayStringIncludingEmpty(t *testing.T) {
	f := setup(t)
	for _, display := range []string{"", "r142 (c604e32)", "Build+A.7-beta (commit42)"} {
		f.cfg.Banner = func(page.User) page.Banner { return page.Banner{Service: pages.ServiceName, Version: display} }
		request := httptest.NewRequest("GET", "/about", nil)
		request.Header.Set("X-User-Id", "owner")
		answer := httptest.NewRecorder()
		identity.Require(pages.Handler(f.cfg)).ServeHTTP(answer, request)
		requireEqual(t, answer.Code, 200)
		dl := typedID(t, answer.Body.String(), "dl", "about")
		dts := elements(dl.body, "dt")
		requireEqual(t, len(dts), 3)
		requireEqual(t, normalise(content(t, dts[1])), "Version")
		value := typedID(t, dl.body, "dd", "about-version")
		second, third, position := dts[1].start, dts[2].start, value.start
		if position <= second || position >= third {
			t.Fatal("version value is not between second and third labels")
		}
		requireEqual(t, normalise(content(t, value)), display)
	}
}
