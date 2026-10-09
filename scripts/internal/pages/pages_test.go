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
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/tools"
)

var fixedTime = time.Date(2026, 10, 5, 9, 31, 40, 0, time.FixedZone("test", 3600))
var fixedBanner = page.Banner{Service: pages.ServiceName, Version: "test", Icon: template.HTML("fixture-icon"), Home: "https://fixture-home.test", Tools: true, Trail: []page.Level{{Name: "stale", URL: "/stale"}}, Services: []page.Service{{Name: "fixture-sibling", URL: "https://fixture-sibling.test", Enabled: true}}}

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

// R-VKHH-HQ5X R-OIC3-1CB3 R-VO56-N1E0 R-VPD3-0T4P R-VQKZ-EKVE R-VRSV-SCM3 R-VT0S-64CS R-70GP-VANL R-VXWD-P7BK R-VZ4A-2Z29 R-W0C6-GQSY R-W6FO-DLIF R-W7NK-RD94 R-WBB9-WOH7
// R-RWIX-JJ9E R-RYYQ-B2QS R-S06M-OUHH R-S1EJ-2M86
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
	row := pages.RunRow{ID: "run", URL: "/alpha/runs/run/", Status: store.StatusExited, Commit: "abcdef0", Started: "start", StartedAt: "stamp", Duration: &pages.Duration{Seconds: 12}, ExitCode: 0}
	card := pages.RunCard{ID: "run", URL: row.URL, Status: row.Status, ExitCode: row.ExitCode, Running: false, Commit: "abcdef012345", Ref: "main", Started: "start", StartedAt: "stamp", Finished: "finish", FinishedAt: "endstamp", Duration: &pages.Duration{Seconds: 12}, Trigger: "manual", User: "user", Request: "request", StdoutSize: pages.Size{Number: "1", Unit: "byte"}, StderrSize: pages.Size{Number: "0", Unit: "byte"}, Truncated: true, Failure: &pages.Failure{Reason: store.ReasonTooLarge, Repo: repo, Ref: "fixture-ref", TreeMaxBytes: 123, OperationSeconds: 7}, FilesGone: false}
	file := &pages.FileText{Size: pages.Size{Number: "1", Unit: "byte"}, Text: "<text>&\n", URL: "/file"}
	cases := []struct {
		name string
		data any
	}{
		{"landing", pages.LandingData{Banner: fixedBanner, Scripts: []pages.ScriptRow{{Name: "alpha", URL: "/alpha/", Repo: repo, Ref: "main", LastRun: &row}}}},
		{"script", pages.ScriptData{Banner: fixedBanner, Script: pages.ScriptCard{ID: "script", Name: "alpha", Repo: repo, Ref: "main", Created: "created", CreatedAt: "stamp", RunsKept: 1, KeepNewest: 10, KeepDays: 30}, Runs: []pages.RunRow{row}}},
		{"run", pages.RunData{Banner: fixedBanner, Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: card, Input: file, Stdout: file, Stderr: file, Files: []pages.FileRow{{Path: "file", Size: pages.Size{Number: "1", Unit: "byte"}, URL: "/file"}}}},
		{"about", pages.AboutData{Banner: fixedBanner, Description: pages.Description}},
		{"tools", pages.ToolsData{fixedBanner, []pages.Tool{{"fixture-tool", "fixture-description"}}}},
		{"notfound", pages.NoticeData{Banner: fixedBanner}},
		{"unavailable", pages.NoticeData{Banner: fixedBanner}},
	}
	for _, c := range cases {
		body := rendered(t, c.name, c.data)
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for code := 200; code <= 599; code++ {
				w := httptest.NewRecorder()
				w.Header().Add("X-Keep", "one")
				w.Header().Add("X-Keep", "two")
				w.Header().Add("Content-Type", "old")
				set.Write(w, httptest.NewRequest(method, "/", nil), code, c.name, c.data)
				requireEqual(t, w.Code, code)
				requireEqual(t, w.Header(), http.Header{"Content-Type": {"text/html; charset=utf-8"}, "X-Keep": {"one", "two"}})
				want := ""
				if method != "HEAD" {
					want = body
				}
				requireEqual(t, w.Body.String(), want)
			}
		}
	}
}

// R-CO9K-I92V R-RWIX-JJ9E
func TestPartials(t *testing.T) {
	for _, status := range []string{store.StatusQueued, store.StatusRunning, store.StatusExited, store.StatusTimedOut, store.StatusKilled, store.StatusFailed} {
		row := pages.RunRow{Status: status}
		card := pages.RunCard{Status: status}
		requireEqual(t, rendered(t, "run-status", row), rendered(t, "run-status", card))
	}
	for _, exit := range []int{0, 1} {
		requireEqual(t, rendered(t, "run-status", pages.RunRow{Status: store.StatusExited, ExitCode: exit}), rendered(t, "run-status", pages.RunCard{Status: store.StatusExited, ExitCode: exit}))
	}
	for _, minutes := range []int64{0, 1} {
		_ = rendered(t, "run-duration", pages.Duration{Minutes: minutes, Seconds: 41})
	}
	for _, unit := range []string{"byte", "kilobyte", "megabyte"} {
		_ = rendered(t, "file-size", pages.Size{Number: "17", Unit: unit})
	}
}

type fixture struct {
	t           *testing.T
	cfg         pages.Config
	db          *db.DB
	handler     http.Handler
	writer      *telemetry.Writer
	sc          store.Script
	root, trace string
	mu          sync.Mutex
	users       []page.User
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
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
	f.writer = w
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
	}, Pages: set, Store: s, Source: src, Runs: core, MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: fixedBanner.Version, Telemetry: w}), KeepDays: 30, KeepCount: 10, TreeMaxBytes: 1229, OperationSeconds: 12}
	tools.Register(f.cfg.MCP, tools.Config{Store: s, Source: src, Runs: core, Telemetry: w})
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

// R-RU34-RZS0 R-RVB1-5RIP R-NY3P-CSH2 R-S9XT-R0F1 R-WL2G-YUER R-WPY2-HXDJ R-72WI-MU4Z R-WW1K-ES30 R-WX9G-SJTP R-S2MF-GDYV R-RSV8-E81B R-S528-7XG9 R-NXN9-D70F R-X5SR-GY0K R-X70N-UPR9 R-XMU3-C5LL R-0S03-6TV6 R-XBW9-DSQ1 R-XD45-RKGQ
// R-CUD2-F3SC R-CZ8N-Y6R4 R-D0GK-BYHT R-D1OG-PQ8I
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
	row := pages.RunRow{ID: u.ID, URL: "/alpha/runs/" + u.ID + "/", Status: store.StatusExited, Commit: "aaaaaaa", Started: u.Started.UTC().Format(pages.MinuteLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: &pages.Duration{Seconds: 12}, ExitCode: 0}
	b := withTrail(fixedBanner)
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	expectedLanding := pages.LandingData{Banner: b, Scripts: []pages.ScriptRow{{Name: "alpha", URL: "/alpha/", Repo: pages.Repo{ID: f.sc.Repo, Name: "Repository"}, Ref: "main", LastRun: &row}, {Name: "beta", URL: "/beta/", Repo: pages.Repo{ID: second.Repo, Name: "Repository"}, Ref: "main", LastRun: &pages.RunRow{ID: sibling.ID, URL: "/beta/runs/" + sibling.ID + "/", Status: store.StatusRunning, Commit: "aaaaaaa", Started: sibling.Started.UTC().Format(pages.MinuteLayout), StartedAt: sibling.Started.UTC().Format(time.RFC3339)}}}}
	w := f.request(context.Background(), "GET", "/?query=yes", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "landing", expectedLanding))
	expectedScript := pages.ScriptData{Banner: withTrail(b, page.Level{Name: "alpha", URL: "/alpha/"}), Script: pages.ScriptCard{ID: f.sc.ID, Name: "alpha", Repo: pages.Repo{ID: f.sc.Repo, Name: "Repository"}, Ref: "main", Created: f.sc.Created.UTC().Format(pages.CardLayout), CreatedAt: f.sc.Created.UTC().Format(time.RFC3339), RunsKept: 1, KeepNewest: 10, KeepDays: 30}, Runs: []pages.RunRow{row}}
	w = f.request(context.Background(), "GET", "/alpha/", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "script", expectedScript))
	w = f.request(context.Background(), "GET", "/about?x=1", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "about", pages.AboutData{Banner: withTrail(b, page.Level{Name: "about", URL: "/about"}), Description: pages.Description}))
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
	for _, path := range []string{"/nope/", "/nope", "/nope/runs/" + foreign.ID + "/", "/Alpha/", "/private/", "/private", "/about/", "/tools/", "/mcp/tools", "/alpha/runs/latest", "/alpha/runs/latest/", "/alpha/runs/" + foreign.ID + "/", "/alpha/runs/" + foreign.ID, "/alpha/runs/" + sibling.ID + "/", "/alpha/runs/" + sibling.ID, "/alpha/runs/" + strings.ToUpper(u.ID) + "/", "/alpha/runs/" + strings.ToUpper(u.ID), "/alpha/runs", "/alpha/runs/", "/alpha/other", "/alpha/other/", "/alpha/index.html", "/alpha/./", "/alpha/%2e/", "/alpha/../", "/alpha//", "/alpha/runs/" + u.ID + "//", "//", "/%2falpha/"} {
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
	for _, path := range []string{"/", "/about", "/tools", "/alpha/", "/alpha", "/alpha/runs/" + u.ID + "/", "/nope/"} {
		get := f.request(context.Background(), "GET", path, "owner")
		head := f.request(context.Background(), "HEAD", path, "owner")
		requireEqual(t, head.Code, get.Code)
		requireEqual(t, head.Header(), get.Header())
		requireEqual(t, head.Body.Len(), 0)
		requireEqual(t, get.Header().Values("Set-Cookie"), []string(nil))
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		for _, path := range []string{"/", "/about", "/tools", "/alpha/", "/private/", "/nope/"} {
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
			requireEqual(t, w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"})
			want := ""
			if method == "GET" {
				want = rendered(t, "unavailable", pages.NoticeData{Banner: fixedBanner})
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
			requireEqual(t, w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"})
			requireEqual(t, w.Header().Values("Location"), []string(nil))
			want := ""
			if method == "GET" {
				want = rendered(t, "unavailable", pages.NoticeData{Banner: fixedBanner})
			}
			requireEqual(t, w.Body.String(), want)
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		for _, path := range []string{"/", "/about", "/tools", "/alpha/", "/private/", "/nope/"} {
			w = f.request(context.Background(), method, path, "owner")
			requireEqual(t, w.Code, 405)
			requireEqual(t, w.Header(), http.Header{"Allow": {"GET, HEAD"}})
			requireEqual(t, w.Body.Len(), 0)
		}
	}
	f.mu.Lock()
	requireEqual(t, len(f.users), 20)
	for _, user := range f.users {
		requireEqual(t, user, page.User{})
	}
	f.mu.Unlock()
	w = f.request(context.Background(), "GET", "/about", "owner")
	requireEqual(t, w.Code, 200)
	requireEqual(t, w.Body.String(), rendered(t, "about", pages.AboutData{Banner: withTrail(b, page.Level{Name: "about", URL: "/about"}), Description: pages.Description}))
}

// R-S6A4-LP6Y
// R-CVKY-SVJ1 R-CWSV-6N9Q R-CY0R-KF0F R-CUD2-F3SC R-CT56-1C1N
func TestRunPageData(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	cases := []struct {
		status, reason string
		seconds        int64
		exit           int
	}{
		{store.StatusQueued, "", 0, 0},
		{store.StatusRunning, "", 0, 0},
		{store.StatusExited, "", 12, 0},
		{store.StatusExited, "", 221, 7},
		{store.StatusTimedOut, "", 600, 0},
		{store.StatusKilled, "", 1, 0},
		{store.StatusFailed, store.ReasonRepositoryMissing, 0, 0},
		{store.StatusFailed, store.ReasonCommitMissing, 0, 0},
		{store.StatusFailed, store.ReasonTooLarge, 0, 0},
		{store.StatusFailed, store.ReasonGitFailed, 0, 0},
		{store.StatusFailed, store.ReasonTimedOut, 0, 0},
		{store.StatusFailed, store.ReasonQueueAbandoned, 0, 0},
		{store.StatusFailed, store.ReasonStartFailed, 0, 0},
	}
	var rows []pages.RunRow
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
		b := withTrail(fixedBanner)
		b.Email = "person@example.com"
		b.ProfileURL = "https://auth.sbx.example/"
		b.LogoutURL = "https://auth.sbx.example/logout"
		var dur *pages.Duration
		if c.status != store.StatusQueued && c.status != store.StatusRunning && c.status != store.StatusFailed {
			dur = &pages.Duration{Minutes: c.seconds / 60, Seconds: c.seconds % 60}
		}
		outSize, errSize := pages.Size{Number: "1.2", Unit: "kilobyte"}, pages.Size{Number: "69", Unit: "byte"}
		if c.status == store.StatusQueued || c.status == store.StatusRunning {
			errSize = pages.Size{Number: "0", Unit: "byte"}
		}
		if c.status == store.StatusQueued || c.status == store.StatusFailed {
			outSize = pages.Size{Number: "0", Unit: "byte"}
			errSize = pages.Size{Number: "0", Unit: "byte"}
		}
		card := pages.RunCard{ID: u.ID, URL: path, Status: c.status, ExitCode: c.exit, Running: c.status == store.StatusRunning || c.status == store.StatusQueued, Commit: u.SHA, Ref: "main", Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: dur, Trigger: "manual", User: "owner", Request: u.RequestID, StdoutSize: outSize, StderrSize: errSize, Truncated: u.Truncated}
		if !card.Running {
			card.Finished = u.Finished.UTC().Format(pages.SecondLayout)
			card.FinishedAt = u.Finished.UTC().Format(time.RFC3339)
		}
		if c.status == store.StatusFailed {
			card.Failure = &pages.Failure{Reason: c.reason, Repo: pages.Repo{ID: sc.Repo, Name: "Repository"}, Ref: u.Ref, TreeMaxBytes: f.cfg.TreeMaxBytes, OperationSeconds: f.cfg.OperationSeconds}
		}
		data := pages.RunData{Banner: withTrail(b, page.Level{Name: "alpha", URL: "/alpha/"}, page.Level{Name: u.ID, URL: "/alpha/runs/" + u.ID + "/"}), Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: card, Input: &pages.FileText{Size: pages.Size{Number: "17", Unit: "byte"}, Text: "{\"text\":\"<&>\"}\n", URL: path + runs.InputFile}, Stdout: &pages.FileText{Size: pages.Size{Number: "1.2", Unit: "kilobyte"}, Text: strings.Repeat("x", 1229), URL: path + runs.StdoutFile}, Stderr: &pages.FileText{Size: pages.Size{Number: "0", Unit: "byte"}, URL: path + runs.StderrFile}, Files: []pages.FileRow{{Path: ".hidden", Size: pages.Size{Number: "1", Unit: "byte"}, URL: path + "out/.hidden"}, {Path: "a b/é?#.txt", Size: pages.Size{Number: "4", Unit: "byte"}, URL: path + "out/a%20b/%C3%A9%3F%23.txt"}, {Path: "a.txt", Size: pages.Size{Number: "7", Unit: "byte"}, URL: path + "out/a.txt"}, {Path: "a/z", Size: pages.Size{Number: "6", Unit: "byte"}, URL: path + "out/a/z"}, {Path: "z", Size: pages.Size{Number: "1.0", Unit: "megabyte"}, URL: path + "out/z"}}}
		row := pages.RunRow{ID: u.ID, URL: path, Status: u.Status, ExitCode: u.ExitCode, Commit: "aaaaaaa", Started: u.Started.UTC().Format(pages.MinuteLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: dur}
		if u.SHA == "" {
			row.Commit = ""
		}
		rows = append([]pages.RunRow{row}, rows...)
		requireEqual(t, f.request(context.Background(), "GET", "/", "owner").Body.String(), rendered(t, "landing", pages.LandingData{Banner: b, Scripts: []pages.ScriptRow{{Name: sc.Name, URL: "/alpha/", Repo: pages.Repo{ID: sc.Repo, Name: "Repository"}, Ref: sc.Ref, LastRun: &row}}}))
		requireEqual(t, f.request(context.Background(), "GET", "/alpha/", "owner").Body.String(), rendered(t, "script", pages.ScriptData{Banner: withTrail(b, page.Level{Name: "alpha", URL: "/alpha/"}), Script: pages.ScriptCard{ID: sc.ID, Name: sc.Name, Repo: pages.Repo{ID: sc.Repo, Name: "Repository"}, Ref: sc.Ref, Created: sc.Created.UTC().Format(pages.CardLayout), CreatedAt: sc.Created.UTC().Format(time.RFC3339), RunsKept: len(rows), KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}, Runs: rows}))
		data.Input.Size = pages.Size{Number: fmt.Sprint(len(data.Input.Text)), Unit: "byte"}
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
			data.Run.StdoutSize = pages.Size{Number: "0", Unit: "byte"}
			data.Run.StderrSize = pages.Size{Number: "0", Unit: "byte"}
		}
		w = f.request(context.Background(), "GET", path, "owner")
		requireEqual(t, w.Body.String(), rendered(t, "run", data))
	}
}

// R-XBW9-DSQ1 R-S6A4-LP6Y
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
	b := withTrail(fixedBanner)
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	path := "/alpha/runs/" + u.ID + "/"
	d := pages.RunData{Banner: withTrail(b, page.Level{Name: "alpha", URL: "/alpha/"}, page.Level{Name: u.ID, URL: "/alpha/runs/" + u.ID + "/"}), Script: pages.ScriptLink{Name: "alpha", URL: "/alpha/"}, Run: pages.RunCard{ID: u.ID, URL: path, Status: store.StatusRunning, Running: true, Commit: u.SHA, Ref: "main", Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Trigger: "manual", User: "owner", Request: u.RequestID, StdoutSize: pages.Size{Number: "0", Unit: "byte"}, StderrSize: pages.Size{Number: "0", Unit: "byte"}}}
	w = f.request(context.Background(), "GET", path, "owner")
	requireEqual(t, w.Body.String(), rendered(t, "run", d))
	for _, name := range []string{runs.InputFile, runs.StdoutFile, runs.StderrFile} {
		writeFile(t, filepath.Join(folder, name), "")
	}
	d.Input = &pages.FileText{Size: pages.Size{Number: "0", Unit: "byte"}, URL: path + runs.InputFile}
	d.Stdout = &pages.FileText{Size: pages.Size{Number: "0", Unit: "byte"}, URL: path + runs.StdoutFile}
	d.Stderr = &pages.FileText{Size: pages.Size{Number: "0", Unit: "byte"}, URL: path + runs.StderrFile}
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

// R-NZBL-QK7R R-XJ7N-OF67
// R-D449-H9PW
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
		{"/alpha/runs/" + otherFailure.ID + "/", [][]string{argv(sc.Repo)}},
		{"/about", nil}, {"/tools", nil}, {"/alpha", nil}, {"/alpha/runs/" + u.ID, nil},
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
		for _, path := range []string{"/", "/alpha/", "/alpha", "/never/", "/alpha/runs/" + failed.ID + "/", "/missing/", "/about", "/tools"} {
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
// R-VQKZ-EKVE R-VRSV-SCM3 R-VT0S-64CS R-70GP-VANL R-VXWD-P7BK R-VZ4A-2Z29 R-W0C6-GQSY R-W6FO-DLIF R-W7NK-RD94 R-RU34-RZS0
// R-CFQ9-TUW0 R-CDAH-2BEM R-CEID-G35B R-CGY6-7MMP R-CI62-LEDE R-CJDY-Z643 R-CLTR-QPLH
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
	_ = struct {
		ID, URL, Status            string
		ExitCode                   int
		Commit, Started, StartedAt string
		Duration                   *pages.Duration
	}(pages.RunRow{})
	_ = struct{ Minutes, Seconds int64 }(pages.Duration{})
	_ = struct{ Number, Unit string }(pages.Size{})
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
		ID, URL, Status                                       string
		ExitCode                                              int
		Running                                               bool
		Commit, Ref, Started, StartedAt, Finished, FinishedAt string
		Duration                                              *pages.Duration
		Trigger, Event, User, Request                         string
		StdoutSize, StderrSize                                pages.Size
		Truncated                                             bool
		Failure                                               *pages.Failure
		FilesGone                                             bool
	}(pages.RunCard{})
	_ = struct {
		Reason                         string
		Repo                           pages.Repo
		Ref                            string
		TreeMaxBytes, OperationSeconds int64
	}(pages.Failure{})
	_ = struct {
		Size      pages.Size
		Text, URL string
	}(pages.FileText{})
	_ = struct {
		Path string
		Size pages.Size
		URL  string
	}(pages.FileRow{})
	_ = struct {
		Banner      page.Banner
		Description string
	}(pages.AboutData{})
	// R-S7I0-ZGXN R-S8PX-D8OC
	_ = struct {
		Banner page.Banner
		Tools  []pages.Tool
	}(pages.ToolsData{})
	_ = struct{ Name, Description string }(pages.Tool{})
	_ = struct{ Banner page.Banner }(pages.NoticeData{})
	_ = struct {
		Banner                                              func(page.User) page.Banner
		Pages                                               *pages.Set
		ServicesPath                                        string
		Store                                               *store.Store
		Source                                              *source.Source
		Runs                                                *runs.Core
		MCP                                                 *mcp.Server
		KeepDays, KeepCount, TreeMaxBytes, OperationSeconds int64
	}(pages.Config{})
	requireEqual(t, pages.ServiceName, "scripts")
}

// R-D5C5-V1GL
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
	for _, failing := range []bool{false, true} {
		f.db.SetFailing(failing)
		requestContext := ctx
		if failing {
			requestContext = context.Background()
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, path := range []string{"/", "/alpha/", "/alpha", "/none/", "/alpha/runs/" + u.ID + "/"} {
				f.mu.Lock()
				f.users = nil
				f.mu.Unlock()
				w := f.request(requestContext, method, path, "owner")
				requireEqual(t, w.Code, 503)
				f.mu.Lock()
				requireEqual(t, f.users, []page.User{{}})
				f.mu.Unlock()
			}
		}
	}
}

// R-CC2K-OJNX
func TestTimeLayouts(t *testing.T) {
	const layouts = pages.MinuteLayout + pages.CardLayout + pages.SecondLayout
	_ = layouts
	for _, layout := range []string{pages.MinuteLayout, pages.CardLayout, pages.SecondLayout} {
		if layout == "" || fixedTime.Format(layout) == "" {
			t.Fatal("empty time layout")
		}
	}
}

// R-CT56-1C1N R-CWSV-6N9Q R-S6A4-LP6Y
func TestSizeBoundaryAndDurationFormatting(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	b := withTrail(fixedBanner)
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	for i, c := range []struct {
		bytes        int64
		number, unit string
		seconds      int64
	}{
		{0, "0", "byte", 0},
		{69, "69", "byte", 12},
		{999, "999", "byte", 59},
		{1000, "1.0", "kilobyte", 60},
		{1229, "1.2", "kilobyte", 221},
		{12034, "12.0", "kilobyte", 600},
		{999999, "999.9", "kilobyte", 1},
		{1000000, "1.0", "megabyte", 2},
		{1048576, "1.0", "megabyte", 3},
	} {
		u := f.add(t, sc, i+1, store.StatusRunning, "", 0, 0)
		var err error
		u, err = f.cfg.Store.FinishRun(context.Background(), u.ID, store.Ending{Status: store.StatusExited, Finished: u.Started.Add(time.Duration(c.seconds) * time.Second), StdoutBytes: c.bytes, StderrBytes: c.bytes})
		if err != nil {
			t.Fatal(err)
		}
		path := "/alpha/runs/" + u.ID + "/"
		want := pages.RunData{Banner: withTrail(b, page.Level{Name: "alpha", URL: "/alpha/"}, page.Level{Name: u.ID, URL: "/alpha/runs/" + u.ID + "/"}), Script: pages.ScriptLink{Name: sc.Name, URL: "/alpha/"}, Run: pages.RunCard{
			ID: u.ID, URL: path, Status: store.StatusExited, Commit: u.SHA, Ref: u.Ref,
			Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339),
			Finished: u.Finished.UTC().Format(pages.SecondLayout), FinishedAt: u.Finished.UTC().Format(time.RFC3339), Duration: &pages.Duration{Minutes: c.seconds / 60, Seconds: c.seconds % 60},
			Trigger: u.Trigger, User: u.User, Request: u.RequestID, StdoutSize: pages.Size{Number: c.number, Unit: c.unit}, StderrSize: pages.Size{Number: c.number, Unit: c.unit}, FilesGone: true,
		}}
		requireEqual(t, f.request(context.Background(), "GET", path, "owner").Body.String(), rendered(t, "run", want))
	}
}

// R-70GP-VANL R-72WI-MU4Z
func TestScriptSubscriptions(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	b := withTrail(fixedBanner)
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	check := func(names []string) {
		t.Helper()
		want := pages.ScriptData{Banner: withTrail(b, page.Level{Name: "alpha", URL: "/alpha/"}), Script: pages.ScriptCard{ID: sc.ID, Name: sc.Name, Repo: pages.Repo{ID: sc.Repo, Name: "Repository"}, Ref: sc.Ref, Created: sc.Created.UTC().Format(pages.CardLayout), CreatedAt: sc.Created.UTC().Format(time.RFC3339), KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}, Subscriptions: names}
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

// R-WPY2-HXDJ R-72WI-MU4Z R-WW1K-ES30 R-S6A4-LP6Y
func TestEmptyCatalogMissingRepositoryAndEventRun(t *testing.T) {
	f := setup(t)
	b := withTrail(fixedBanner)
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
		requireEqual(t, f.request(context.Background(), "GET", "/alpha/", "owner").Body.String(), rendered(t, "script", pages.ScriptData{Banner: withTrail(b, page.Level{Name: "alpha", URL: "/alpha/"}), Script: pages.ScriptCard{ID: sc.ID, Name: sc.Name, Repo: repo, Ref: sc.Ref, Created: sc.Created.UTC().Format(pages.CardLayout), CreatedAt: sc.Created.UTC().Format(time.RFC3339), KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}}))
	}
	for i, c := range []struct{ status, reason string }{
		{store.StatusRunning, ""},
		{store.StatusFailed, store.ReasonCommitMissing},
		{store.StatusFailed, store.ReasonGitFailed},
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
		card := pages.RunCard{ID: u.ID, URL: path, Status: store.StatusRunning, Running: true, Commit: u.SHA, Ref: u.Ref, Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Trigger: u.Trigger, Event: u.Event, User: u.User, Request: u.RequestID, StdoutSize: pages.Size{Number: "0", Unit: "byte"}, StderrSize: pages.Size{Number: "0", Unit: "byte"}, FilesGone: true}
		if c.status == store.StatusFailed {
			card.Status = store.StatusFailed
			card.Running = false
			card.Finished = u.Finished.UTC().Format(pages.SecondLayout)
			card.FinishedAt = u.Finished.UTC().Format(time.RFC3339)
			card.Failure = &pages.Failure{Reason: c.reason, Repo: pages.Repo{ID: sc.Repo}, Ref: u.Ref, TreeMaxBytes: f.cfg.TreeMaxBytes, OperationSeconds: f.cfg.OperationSeconds}
		}
		requireEqual(t, f.request(context.Background(), "GET", path, "owner").Body.String(), rendered(t, "run", pages.RunData{Banner: withTrail(b, page.Level{Name: "alpha", URL: "/alpha/"}, page.Level{Name: u.ID, URL: "/alpha/runs/" + u.ID + "/"}), Script: pages.ScriptLink{Name: sc.Name, URL: "/alpha/"}, Run: card}))
	}
}

func withTrail(b page.Banner, trail ...page.Level) page.Banner {
	b.Trail = trail
	return b
}

// R-SB5Q-4S5Q R-SCDM-IJWF R-S3UB-U5PK R-S9XT-R0F1
func TestToolsPageUsesCurrentServerAndOwnTrail(t *testing.T) {
	f := setup(t)
	original := page.Banner{Service: "fixture-service", Version: "fixture-version", Icon: template.HTML("fixture-icon"), Home: "https://fixture-home.test", Tools: true, Trail: []page.Level{{Name: "stale", URL: "/stale"}}, Services: []page.Service{{Name: "fixture-sibling", URL: "https://fixture-sibling.test", Enabled: true}}}
	f.cfg.Banner = func(user page.User) page.Banner {
		b := original
		b.Email = user.Email
		b.ProfileURL = user.ProfileURL
		b.LogoutURL = user.LogoutURL
		return b
	}
	f.cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: "fixture-server", Version: "fixture-version", Telemetry: f.writer})
	f.handler = identity.Require(pages.Handler(f.cfg))
	b := original
	b.Email = "person@example.com"
	b.ProfileURL = "https://auth.sbx.example/"
	b.LogoutURL = "https://auth.sbx.example/logout"
	check := func() {
		var entries []pages.Tool
		for _, tool := range f.cfg.MCP.Tools() {
			entries = append(entries, pages.Tool{tool.Name, tool.Description})
		}
		for _, failing := range []bool{false, true} {
			f.db.SetFailing(failing)
			for _, suffix := range []string{"", "?ignored=anything"} {
				get := f.request(context.Background(), "GET", "/tools"+suffix, "owner")
				requireEqual(t, get.Code, 200)
				requireEqual(t, get.Body.String(), rendered(t, "tools", pages.ToolsData{withTrail(b, page.Level{Name: "tools", URL: "/tools"}), entries}))
				head := f.request(context.Background(), "HEAD", "/tools"+suffix, "owner")
				requireEqual(t, head.Code, get.Code)
				requireEqual(t, head.Header(), get.Header())
				requireEqual(t, head.Body.Len(), 0)
				about := f.request(context.Background(), "GET", "/about"+suffix, "owner")
				requireEqual(t, about.Code, 200)
				requireEqual(t, about.Body.String(), rendered(t, "about", pages.AboutData{withTrail(b, page.Level{Name: "about", URL: "/about"}), pages.Description}))
			}
		}
	}
	check()
	for _, tool := range []struct{ name, description string }{{"z_fixture", "Fixture description one<&>.\n\nFixture details."}, {"a_fixture", "Fixture description two."}} {
		mcp.AddTool(f.cfg.MCP, mcp.Tool[tools.ListArgs, tools.ScriptList]{Name: tool.name, Description: tool.description, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, tools.ListArgs) (tools.ScriptList, error) {
			t.Error("tools page called a tool")
			return tools.ScriptList{}, nil
		}})
		check()
	}
	requireEqual(t, original.Trail, []page.Level{{Name: "stale", URL: "/stale"}})
}
