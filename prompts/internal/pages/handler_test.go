package pages_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
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
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

var instant = time.Date(2025, 3, 4, 5, 6, 7, 0, time.FixedZone("fixture-offset", 3600))

type fixture struct {
	cfg   pages.Config
	db    *db.DB
	root  string
	calls []page.User
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	root := t.TempDir()
	clock := func() time.Time { return instant }
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(root, "catalog.db"), Migrations: prompts.Migrations(), Now: clock})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	var random []byte
	for i := 0; i < 128; i++ {
		random = append(random, 0, 0, 0, 0, 0, 0, 0, byte(i))
	}
	s := store.New(d, store.Config{Now: clock, Rand: bytes.NewReader(random)})
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: &telemetry.Capture{}, Stderr: io.Discard, Now: clock, Rand: bytes.NewReader(make([]byte, 256))})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "fixture done") })
	dir := filepath.Join(root, "runs")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	core := runs.New(runs.Config{Store: s, Writer: writer, Runs: dir, PromptSeconds: 1, OutputMaxBytes: 1, MaxToolCalls: 1, KeepDays: 1, KeepCount: 1, RunMemoryMaxBytes: 1, RunPidsMax: 1, MaxActive: 1, MaxQueued: 1, Now: clock, Rand: bytes.NewReader(random), ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	f := &fixture{db: d, root: dir}
	f.cfg = pages.Config{Pages: load(t), MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: writer}), Store: s, Runs: core, KeepDays: 7, KeepCount: 3, Banner: func(u page.User) page.Banner { f.calls = append(f.calls, u); return bannerValue(u) }}
	return f
}
func bannerValue(u page.User) page.Banner {
	return page.Banner{Service: pages.ServiceName, Version: "fixture-display", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Trail: []page.Level{{Name: "source-fixture", URL: "/source-fixture/"}}, Home: "https://home.fixture/", Tools: true}
}
func request(method, path, owner string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "prompts.example.test:443"
	r.Header.Set("X-User-Id", owner)
	r.Header.Set("X-User-Email", owner+"@example.test")
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}
func serve(cfg pages.Config, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	identity.Require(pages.Handler(cfg)).ServeHTTP(w, r)
	return w
}
func create(t *testing.T, f *fixture, name, owner string) store.Prompt {
	t.Helper()
	p, e := f.cfg.Store.Create(context.Background(), store.Draft{Name: name, Owner: owner, OwnerEmail: owner + "@private.test", Model: "fixture-model", Prompt: "fixture-text<&", System: "fixture-system<&", Schema: json.RawMessage(" { \"type\": \"object\" } "), Tools: []string{"suite", "files", "bash"}})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func add(t *testing.T, f *fixture, p store.Prompt, n int, status string, ending store.Ending) store.Run {
	t.Helper()
	r := store.Run{ID: fmt.Sprintf("prr_%016x", n), Prompt: p.ID, Model: "run-model<&", User: p.Owner, RequestID: "fixture-request<&", Trigger: store.TriggerManual, Status: status, Started: instant}
	if status == store.StatusFailed {
		r.Reason = ending.Reason
		r.Finished = instant.Add(41 * time.Second)
	}
	u, e := f.cfg.Store.AddRun(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if ending.Status != "" && status != store.StatusFailed {
		ending.Finished = instant.Add(221 * time.Second)
		u, e = f.cfg.Store.FinishRun(context.Background(), u.ID, ending)
		if e != nil {
			t.Fatal(e)
		}
	}
	return u
}
func wantBanner(owner string, trail ...page.Level) page.Banner {
	b := bannerValue(page.User{Email: owner + "@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"})
	b.Trail = trail
	return b
}
func expectedRow(u store.Run, name string) pages.RunRow {
	var duration *pages.Duration
	cost := ""
	if u.Status != store.StatusQueued && u.Status != store.StatusRunning {
		cost = expectedCost(u.Usage.CostNanos)
		if u.Status != store.StatusFailed {
			seconds := int64(u.Finished.Sub(u.Started) / time.Second)
			duration = &pages.Duration{Minutes: seconds / 60, Seconds: seconds % 60}
		}
	}
	return pages.RunRow{ID: u.ID, URL: "/" + name + "/runs/" + u.ID + "/", Status: u.Status, ExitCode: u.ExitCode, Model: u.Model, Cost: cost, Started: u.Started.UTC().Format(pages.MinuteLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: duration}
}
func expectedCost(n int64) string {
	micros := n / 1000
	if n%1000 >= 500 {
		micros++
	}
	return fmt.Sprintf("%d.%06d", micros/1000000, micros%1000000)
}
func expectedCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	for offset := len(s) - 3; offset > 0; offset -= 3 {
		s = s[:offset] + "," + s[offset:]
	}
	return s
}
func expectedUsage(u store.Run) *pages.Usage {
	if u.Status == store.StatusQueued || u.Status == store.StatusRunning {
		return nil
	}
	v := u.Usage
	return &pages.Usage{Calls: expectedCount(v.Calls), ToolCalls: expectedCount(v.ToolCalls), InputTokens: expectedCount(v.InputTokens), CachedTokens: expectedCount(v.CachedTokens), OutputTokens: expectedCount(v.OutputTokens), ReasoningTokens: expectedCount(v.ReasoningTokens), Cost: expectedCost(v.CostNanos)}
}
func assertPage(t *testing.T, w *httptest.ResponseRecorder, status int, name string, data any) {
	t.Helper()
	if w.Code != status || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) {
		t.Fatalf("response: %d %v", w.Code, w.Header())
	}
	if w.Body.String() != render(t, templates(t), name, data) {
		want := render(t, templates(t), name, data)
		got := w.Body.String()
		i := 0
		for i < len(want) && i < len(got) && want[i] == got[i] {
			i++
		}
		end := i + 100
		if end > len(want) {
			end = len(want)
		}
		endGot := i + 100
		if endGot > len(got) {
			endGot = len(got)
		}
		t.Fatalf("%s differ at %d: got %q want %q", name, i, got[i:endGot], want[i:end])
	}
}

// R-YUFC-PT2O R-YDCR-D0OY R-YEKN-QSFN R-YLW2-1EVT R-CBT1-D6V6 R-CGOM-W9TY R-YN3Y-F6MI R-CFGQ-II39
func TestLanding(t *testing.T) {
	f := setup(t)
	assertPage(t, serve(f.cfg, request("GET", "/?fixture=query", "alice")), 200, "landing", pages.LandingData{Banner: wantBanner("alice")})
	b := create(t, f, "bravo", "alice")
	a := create(t, f, "alpha", "alice")
	private := create(t, f, "private", "bob")
	assertPage(t, serve(f.cfg, request("GET", "/", "bob")), 200, "landing", pages.LandingData{Banner: wantBanner("bob"), Prompts: []pages.PromptRow{{Name: private.Name, URL: "/private/", Model: private.Model}}})
	last := add(t, f, b, 1, store.StatusRunning, store.Ending{Status: store.StatusExited, ExitCode: 1, Usage: store.Usage{CostNanos: 23714500}})
	row := expectedRow(last, b.Name)
	data := pages.LandingData{Banner: wantBanner("alice"), Prompts: []pages.PromptRow{{Name: a.Name, URL: "/alpha/", Model: a.Model}, {Name: b.Name, URL: "/bravo/", Model: b.Model, LastRun: &row}}}
	r := request("GET", "/?arbitrary=query", "alice")
	r.Header.Add("X-User-Id", "bob")
	assertPage(t, serve(f.cfg, r), 200, "landing", data)
}

// R-CHWJ-A1KN R-YPJR-6Q3W R-CD0X-QYLV R-CE8U-4QCK
func TestPrompt(t *testing.T) {
	f := setup(t)
	p := create(t, f, "fixture", "alice")
	for _, event := range []string{"zeta.*", "alpha.created"} {
		if _, e := f.cfg.Store.Subscribe(context.Background(), p.ID, event); e != nil {
			t.Fatal(e)
		}
	}
	for i, n := range []int64{0, 499, 500, 23714000, 23714499, 23714500, 1500000000} {
		add(t, f, p, i+1, store.StatusRunning, store.Ending{Status: store.StatusExited, Usage: store.Usage{CostNanos: n}})
	}
	add(t, f, p, 20, store.StatusQueued, store.Ending{})
	add(t, f, p, 21, store.StatusRunning, store.Ending{})
	add(t, f, p, 22, store.StatusFailed, store.Ending{Reason: store.ReasonQueueAbandoned})
	p, e := f.cfg.Store.Find(context.Background(), "alice", p.Name)
	if e != nil {
		t.Fatal(e)
	}
	rs, e := f.cfg.Store.Runs(context.Background(), p.ID)
	if e != nil {
		t.Fatal(e)
	}
	d := pages.PromptData{Banner: wantBanner("alice", page.Level{Name: p.Name, URL: "/" + p.Name + "/"}), Prompt: pages.PromptCard{ID: p.ID, Name: p.Name, Model: p.Model, Text: p.Prompt, System: p.System, Schema: string(p.Schema), Created: p.Created.UTC().Format(pages.CardLayout), CreatedAt: p.Created.UTC().Format(time.RFC3339), RunsKept: len(rs), KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}}
	for i, g := range p.Tools {
		d.Prompt.Tools = append(d.Prompt.Tools, pages.ToolGroup{Name: g, First: i == 0, Last: i == len(p.Tools)-1})
	}
	for _, u := range rs {
		d.Runs = append(d.Runs, expectedRow(u, p.Name))
	}
	for _, s := range p.Subscriptions {
		d.Subscriptions = append(d.Subscriptions, s.Event)
	}
	assertPage(t, serve(f.cfg, request("GET", "/fixture/?arbitrary=query", "alice")), 200, "prompt", d)
}

// R-CLK8-FCSQ R-YOBU-SYD7
func TestAbout(t *testing.T) {
	f := setup(t)
	f.db.SetFailing(true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertPage(t, serve(f.cfg, request("GET", "/about?query=yes", "alice").WithContext(ctx)), 200, "about", pages.AboutData{Banner: wantBanner("alice", page.Level{Name: "about", URL: "/about"}), Description: pages.Description})
}

// R-CMS4-T4JF R-CO01-6WA4 R-C9D8-LNDS R-CVBF-HIQA R-CWJB-VAGZ R-CXR8-927O R-CYZ4-MTYD
// R-D071-0LP2 R-D1EX-EDFR R-D2MT-S56G R-D52M-JONU R-D6AI-XGEJ R-D8QB-OZVX
func TestRouting(t *testing.T) {
	f := setup(t)
	p := create(t, f, "summarize", "alice")
	other := create(t, f, "other", "alice")
	private := create(t, f, "private", "bob")
	own := add(t, f, p, 10, store.StatusQueued, store.Ending{})
	another := add(t, f, other, 11, store.StatusQueued, store.Ending{})
	hidden := add(t, f, private, 12, store.StatusQueued, store.Ending{})
	paths := []struct {
		path     string
		status   int
		location string
	}{{"/", 200, ""}, {"/about", 200, ""}, {"/summarize/", 200, ""}, {"/summarize?from=landing", 301, "/summarize/?from=landing"}, {"/summarize/runs/" + own.ID, 301, "/summarize/runs/" + own.ID + "/"}, {"/summarize/runs/" + own.ID + "/", 200, ""}, {"/%73ummarize", 301, "/summarize/"}, {"/%73ummarize/", 200, ""}}
	for _, path := range []string{"//", "/nope/", "/nope", "/Summarize/", "/about/", "/mcp/tools", "/events/", "/declarations/cron.tick.fired", "/private", "/private/", "/nope/runs/" + own.ID + "/", "/summarize/runs", "/summarize/runs/", "/summarize/other", "/summarize/other/", "/summarize/index.html", "/summarize/./", "/summarize/%2e/", "/summarize//", "/summarize/../", "/summarize/a/b/c/d"} {
		paths = append(paths, struct {
			path     string
			status   int
			location string
		}{path, 404, ""})
	}
	for _, id := range []string{another.ID, hidden.ID, "latest", "malformed", "run_000000000000000a", strings.ToUpper(own.ID), "prr_ffffffffffffffff"} {
		for _, slash := range []string{"", "/"} {
			paths = append(paths, struct {
				path     string
				status   int
				location string
			}{"/summarize/runs/" + id + slash, 404, ""})
		}
	}
	for _, c := range paths {
		f.calls = nil
		get := serve(f.cfg, request("GET", c.path, "alice"))
		if get.Code != c.status {
			t.Fatalf("%s status %d", c.path, get.Code)
		}
		if c.status == 404 {
			assertPage(t, get, 404, "notfound", pages.NoticeData{Banner: bannerValue(page.User{})})
		}
		if c.status == 301 && (!reflect.DeepEqual(get.Header().Values("Location"), []string{c.location})) {
			t.Fatal("redirect", c.path, get.Header())
		}
		wantCalls := []page.User(nil)
		if c.status == 200 {
			wantCalls = []page.User{{Email: "alice@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"}}
		}
		if c.status == 404 {
			wantCalls = []page.User{{}}
		}
		if !reflect.DeepEqual(f.calls, wantCalls) {
			t.Fatal("banner arguments", c.path, f.calls)
		}
		f.calls = nil
		head := serve(f.cfg, request("HEAD", c.path, "alice"))
		if !reflect.DeepEqual(f.calls, wantCalls) {
			t.Fatal("HEAD banner arguments", c.path, f.calls)
		}
		if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
			t.Fatal("HEAD", c.path)
		}
		if _, exists := get.Header()["Set-Cookie"]; exists {
			t.Fatal("cookie")
		}
	}
	for _, failing := range []bool{false, true} {
		f.db.SetFailing(failing)
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for _, c := range paths {
				f.calls = nil
				r := request(method, c.path, "alice")
				r.Body = io.NopCloser(strings.NewReader("arbitrary body"))
				r.Header.Set("If-None-Match", "*")
				w := serve(f.cfg, r)
				if w.Code != 405 || w.Body.Len() != 0 || !reflect.DeepEqual(w.Header(), http.Header{"Allow": []string{"GET, HEAD"}}) || len(f.calls) != 0 {
					t.Fatalf("method refusal %s %s: %d %v", method, c.path, w.Code, w.Header())
				}
			}
		}
	}
	for _, mode := range []string{"failure", "cancelled"} {
		f.db.SetFailing(mode == "failure")
		for _, path := range []string{"/", "//", "/summarize/", "/summarize", "/nope/", "/about/", "/summarize/other", "/summarize/runs/", "/summarize/a/b/c/d", "/summarize/runs/" + own.ID + "/"} {
			for _, method := range []string{"GET", "HEAD"} {
				r := request(method, path, "alice")
				if mode == "cancelled" {
					ctx, cancel := context.WithCancel(r.Context())
					cancel()
					r = r.WithContext(ctx)
				}
				f.calls = nil
				w := serve(f.cfg, r)
				if method == "GET" {
					assertPage(t, w, 503, "unavailable", pages.NoticeData{Banner: bannerValue(page.User{})})
				} else if w.Code != 503 || w.Body.Len() != 0 {
					t.Fatal("unavailable HEAD")
				}
				if !reflect.DeepEqual(f.calls, []page.User{{}}) {
					t.Fatal("unavailable banner")
				}
				if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
					t.Fatal("unavailable headers")
				}
			}
		}
	}
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
}
func fileSize(n int64) pages.Size {
	switch {
	case n < 1000:
		return pages.Size{Number: strconv.FormatInt(n, 10), Unit: "byte"}
	case n < 1000000:
		return pages.Size{Number: fmt.Sprintf("%d.%d", n/1000, (n/100)%10), Unit: "kilobyte"}
	default:
		return pages.Size{Number: fmt.Sprintf("%d.%d", n/1000000, (n/100000)%10), Unit: "megabyte"}
	}
}

// R-93NZ-A08P R-CKCC-1L21 R-YQRN-KHUL R-CD0X-QYLV R-CE8U-4QCK R-CFGQ-II39
func TestRunPages(t *testing.T) {
	f := setup(t)
	p := create(t, f, "fixture", "alice")
	cases := []struct {
		status, reason string
		code           int
	}{{store.StatusQueued, "", 0}, {store.StatusRunning, "", 0}, {store.StatusExited, "", 0}, {store.StatusExited, "", 1}, {store.StatusKilled, "", 0}, {store.StatusTimedOut, "", 0}, {store.StatusFailed, store.ReasonStartFailed, 0}, {store.StatusFailed, store.ReasonQueueAbandoned, 0}}
	for i, c := range cases {
		for variant := 0; variant < 5; variant++ {
			t.Run(fmt.Sprintf("%s-%d-%d", c.status, i, variant), func(t *testing.T) {
				start := c.status
				ending := store.Ending{}
				if c.status == store.StatusFailed {
					ending.Reason = c.reason
				} else if c.status != store.StatusQueued && c.status != store.StatusRunning {
					start = store.StatusRunning
					ending = store.Ending{Status: c.status, ExitCode: c.code, StdoutBytes: 1229, StderrBytes: 69, StdoutTruncated: variant&1 != 0, StderrTruncated: variant&2 != 0, Usage: store.Usage{Calls: 0, ToolCalls: 7, InputTokens: 999, CachedTokens: 1000, OutputTokens: 18204, ReasoningTokens: 1234567, CostNanos: 23714500}}
				}
				u := add(t, f, p, 100+i*10+variant, start, ending)
				if variant == 1 { // Event identity is stored, not inferred from the current prompt.
					u.ID = fmt.Sprintf("prr_%016x", 1000+i)
					u.Event = fmt.Sprintf("fixture-event-%d", i)
					u.Trigger = store.TriggerEvent
					u.Status = start
					u.ExitCode = 0
					u.Usage = store.Usage{}
					u.StdoutBytes = 0
					u.StderrBytes = 0
					u.StdoutTruncated = false
					u.StderrTruncated = false
					if start != store.StatusFailed {
						u.Finished = time.Time{}
					}
					var e error
					u, e = f.cfg.Store.AddRun(context.Background(), u)
					if e != nil {
						t.Fatal(e)
					}
					if ending.Status != "" {
						ending.Finished = instant.Add(41 * time.Second)
						u, e = f.cfg.Store.FinishRun(context.Background(), u.ID, ending)
						if e != nil {
							t.Fatal(e)
						}
					}
				}
				folder := f.cfg.Runs.Folder(u)
				base := "/fixture/runs/" + u.ID + "/"
				running := u.Status == store.StatusQueued || u.Status == store.StatusRunning
				row := expectedRow(u, p.Name)
				d := pages.RunData{Banner: wantBanner("alice", page.Level{Name: p.Name, URL: "/" + p.Name + "/"}, page.Level{Name: u.ID, URL: base}), Prompt: pages.PromptLink{Name: p.Name, URL: "/fixture/"}, Run: pages.RunCard{ID: u.ID, URL: base, Status: u.Status, ExitCode: u.ExitCode, Running: running, Model: u.Model, Started: u.Started.UTC().Format(pages.SecondLayout), StartedAt: u.Started.UTC().Format(time.RFC3339), Duration: row.Duration, Trigger: u.Trigger, Event: u.Event, User: u.User, Request: u.RequestID, Usage: expectedUsage(u), StdoutSize: fileSize(0), StderrSize: fileSize(0), TranscriptSize: fileSize(0), Truncated: u.Truncated(), StdoutTruncated: u.StdoutTruncated, StderrTruncated: u.StderrTruncated, FilesGone: variant == 0}}
				if !running {
					d.Run.Finished = u.Finished.UTC().Format(pages.SecondLayout)
					d.Run.FinishedAt = u.Finished.UTC().Format(time.RFC3339)
				}
				if c.status == store.StatusFailed {
					d.Run.Failure = &pages.Failure{Reason: c.reason}
				}
				if variant != 0 {
					if e := os.MkdirAll(filepath.Join(folder, runs.WorkDir), 0700); e != nil {
						t.Fatal(e)
					}
					if variant != 4 {
						contents := map[string][]byte{runs.InputFile: []byte(`{"fixture":"<&"}`), runs.StdoutFile: bytes.Repeat([]byte("x"), 1229), runs.StderrFile: {}, runs.TranscriptFile: []byte("transcript-private-sentinel<&")}
						if variant == 3 {
							delete(contents, runs.StdoutFile)
							if e := os.Mkdir(filepath.Join(folder, runs.StdoutFile), 0700); e != nil {
								t.Fatal(e)
							}
							delete(contents, runs.StderrFile)
							if e := os.Symlink(filepath.Join(folder, runs.InputFile), filepath.Join(folder, runs.StderrFile)); e != nil {
								t.Fatal(e)
							}
						}
						for name, body := range contents {
							writeFixture(t, filepath.Join(folder, name), body)
						}
						text := func(name string) *pages.FileText {
							body, ok := contents[name]
							if !ok {
								return nil
							}
							return &pages.FileText{Size: fileSize(int64(len(body))), Text: string(body), URL: base + name}
						}
						d.Input = text(runs.InputFile)
						d.Stdout = text(runs.StdoutFile)
						d.Stderr = text(runs.StderrFile)
						d.Run.TranscriptSize = fileSize(int64(len(contents[runs.TranscriptFile])))
						d.Transcript = &pages.FileLink{Size: d.Run.TranscriptSize, URL: base + runs.TranscriptFile}
						if d.Stdout != nil {
							n := u.StdoutBytes
							if u.Status == store.StatusRunning {
								n = int64(len(contents[runs.StdoutFile]))
							}
							d.Run.StdoutSize = fileSize(n)
						}
						if d.Stderr != nil {
							n := u.StderrBytes
							if u.Status == store.StatusRunning {
								n = int64(len(contents[runs.StderrFile]))
							}
							d.Run.StderrSize = fileSize(n)
						}
						files := []struct {
							path string
							n    int
						}{{".hidden", 69}, {"deep-more", 999}, {"deep/file #?%.json", 1000}, {"deep/nested/large", 1048576}, {"z-tail", 999999}}
						for _, file := range files {
							writeFixture(t, filepath.Join(folder, runs.WorkDir, filepath.FromSlash(file.path)), bytes.Repeat([]byte("f"), file.n))
							names := strings.Split(file.path, "/")
							for j := range names {
								names[j] = url.PathEscape(names[j])
							}
							d.Files = append(d.Files, pages.FileRow{Path: file.path, Size: fileSize(int64(file.n)), URL: base + runs.WorkDir + "/" + strings.Join(names, "/")})
						}
						slices.SortFunc(d.Files, func(a, b pages.FileRow) int { return strings.Compare(a.Path, b.Path) })
						if e := os.Symlink(filepath.Join(folder, runs.InputFile), filepath.Join(folder, runs.WorkDir, "link")); e != nil {
							t.Fatal(e)
						}
						if e := os.Symlink(filepath.Join(folder, runs.WorkDir, "deep"), filepath.Join(folder, runs.WorkDir, "dir-link")); e != nil {
							t.Fatal(e)
						}
					}
				}
				assertPage(t, serve(f.cfg, request("GET", base+"?arbitrary=yes", "alice")), 200, "run", d)
				// The current model may change while the historical run keeps its model.
				model := "updated-prompt-model"
				if _, _, e := f.cfg.Store.Update(context.Background(), p.ID, store.Change{Model: &model}); e != nil {
					t.Fatal(e)
				}
				assertPage(t, serve(f.cfg, request("GET", base, "alice")), 200, "run", d)
			})
		}
	}
}

type entryState struct {
	Mode  fs.FileMode
	Size  int64
	Bytes string
}

func snapshot(t *testing.T, root string) map[string]entryState {
	t.Helper()
	handle, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	}()
	m := map[string]entryState{}
	if err := fs.WalkDir(handle.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		state := entryState{Mode: info.Mode(), Size: info.Size()}
		if info.Mode().IsRegular() {
			b, err := handle.ReadFile(path)
			if err != nil {
				return err
			}
			state.Bytes = string(b)
		}
		m[path] = state
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m
}

// R-D7IF-B858
func TestReadOnly(t *testing.T) {
	f := setup(t)
	p := create(t, f, "fixture", "alice")
	never := create(t, f, "never", "alice")
	u := add(t, f, p, 1, store.StatusQueued, store.Ending{})
	writeFixture(t, filepath.Join(f.cfg.Runs.Folder(u), runs.InputFile), []byte("fixture-input"))
	writeFixture(t, filepath.Join(f.cfg.Runs.Folder(u), runs.WorkDir, "output"), []byte("fixture-output"))
	before := snapshot(t, f.root)
	ps, e := f.cfg.Store.List(context.Background(), "alice")
	if e != nil {
		t.Fatal(e)
	}
	rs, e := f.cfg.Store.Runs(context.Background(), p.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"GET", "HEAD", "POST"} {
		for _, path := range []string{"/", "/about", "/never/", "/fixture/", "/fixture", "/fixture/runs/" + u.ID + "/", "/nope/", "/fixture/other"} {
			serve(f.cfg, request(method, path, "alice"))
		}
	}
	after := snapshot(t, f.root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("run tree changed")
	}
	got, e := f.cfg.Store.List(context.Background(), "alice")
	if e != nil || !reflect.DeepEqual(got, ps) {
		t.Fatal("catalog changed", e)
	}
	runsAfter, e := f.cfg.Store.Runs(context.Background(), p.ID)
	if e != nil || !reflect.DeepEqual(runsAfter, rs) {
		t.Fatal("runs changed", e)
	}
	if _, e := os.Lstat(filepath.Join(f.root, never.ID)); !os.IsNotExist(e) {
		t.Fatal("created never-run folder", e)
	}
}

// R-D9Y8-2RMM
func TestConcurrentHandler(t *testing.T) {
	f := setup(t)
	p := create(t, f, "fixture", "alice")
	u := add(t, f, p, 1, store.StatusQueued, store.Ending{})
	f.cfg.Banner = bannerValue
	handler := identity.Require(pages.Handler(f.cfg))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			for _, path := range []string{"/", "/fixture/", "/about", "/nope/", "/fixture", "/fixture/runs/" + u.ID + "/"} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, request("GET", path, "alice"))
				if w.Code != 200 && w.Code != 404 && w.Code != 301 {
					t.Errorf("concurrent %s: %d", path, w.Code)
				}
			}
		})
	}
	wg.Wait()
}

// R-CHWJ-A1KN R-YPJR-6Q3W
func TestPromptOptionalFields(t *testing.T) {
	f := setup(t)
	for i, groups := range [][]string{nil, {"files"}, {"bash", "suite", "files"}} {
		for _, present := range []bool{false, true} {
			name := fmt.Sprintf("prompt-%d-%t", i, present)
			draft := store.Draft{Name: name, Owner: "alice", Model: "fixture-model", Prompt: "prompt-fixture"}
			draft.Tools = groups
			if present {
				draft.System = "system-fixture<&"
				draft.Schema = json.RawMessage(" { \"type\": \"object\" } ")
			}
			p, err := f.cfg.Store.Create(context.Background(), draft)
			if err != nil {
				t.Fatal(err)
			}
			data := pages.PromptData{Banner: wantBanner("alice", page.Level{Name: p.Name, URL: "/" + p.Name + "/"}), Prompt: pages.PromptCard{ID: p.ID, Name: p.Name, Model: p.Model, Text: p.Prompt, System: p.System, Schema: string(p.Schema), Created: p.Created.UTC().Format(pages.CardLayout), CreatedAt: p.Created.UTC().Format(time.RFC3339), KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}}
			for index, group := range groups {
				data.Prompt.Tools = append(data.Prompt.Tools, pages.ToolGroup{Name: group, First: index == 0, Last: index == len(groups)-1})
			}
			assertPage(t, serve(f.cfg, request("GET", "/"+name+"/", "alice")), 200, "prompt", data)
		}
	}
}

// R-CBT1-D6V6 R-D8QB-OZVX
func TestBannerServicesRewrite(t *testing.T) {
	f := setup(t)
	f.cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
	for _, auth := range []string{"https://auth.first.example.test", "http://auth.second.example.test"} {
		writeFixture(t, f.cfg.ServicesPath, []byte(`{"services":[{"name":"auth","url":"`+auth+`","description":"","socket":"","enabled":true,"mcp":false}]}`))
		f.calls = nil
		expected := page.User{Email: "alice@example.test", ProfileURL: auth + "/", LogoutURL: auth + "/logout"}
		assertPage(t, serve(f.cfg, request("GET", "/about", "alice")), 200, "about", pages.AboutData{Banner: func() page.Banner {
			b := bannerValue(expected)
			b.Trail = []page.Level{{Name: "about", URL: "/about"}}
			return b
		}(), Description: pages.Description})
		if !reflect.DeepEqual(f.calls, []page.User{expected}) {
			t.Fatal("banner services", f.calls)
		}
	}
}

// R-YWV5-HCK2 R-YVN9-3KTD R-YUFC-PT2O
func TestTools(t *testing.T) {
	f := setup(t)
	data := pages.ToolsData{Banner: wantBanner("alice", page.Level{Name: "tools", URL: "/tools"})}
	assertPage(t, serve(f.cfg, request("GET", "/tools?fixture=query", "alice")), 200, "tools", data)
	addTool := func(name, description string) {
		mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct{}]{Name: name, Description: description, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
	}
	addTool("fixture_z", "first-fixture<&.\nsecond-private-fixture")
	addTool("fixture_a", "single-fixture.")
	data.Tools = []pages.Tool{{Name: "fixture_z", Description: "first-fixture<&."}, {Name: "fixture_a", Description: "single-fixture."}}
	f.db.SetFailing(true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertPage(t, serve(f.cfg, request("GET", "/tools?fixture=query", "alice").WithContext(ctx)), 200, "tools", data)
	addTool("fixture_later", "later-fixture.")
	data.Tools = append(data.Tools, pages.Tool{Name: "fixture_later", Description: "later-fixture."})
	get := serve(f.cfg, request("GET", "/tools", "alice"))
	assertPage(t, get, 200, "tools", data)
	head := serve(f.cfg, request("HEAD", "/tools", "alice"))
	if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
		t.Fatal("tools HEAD differs")
	}
	post := serve(f.cfg, request("POST", "/tools", "alice"))
	if post.Code != 405 || post.Body.Len() != 0 || !reflect.DeepEqual(post.Header(), http.Header{"Allow": []string{"GET, HEAD"}}) {
		t.Fatal("tools method refusal differs")
	}
}
