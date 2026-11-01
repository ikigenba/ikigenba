package pages_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/figures"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

type fixture struct {
	cfg      pages.Config
	database *db.DB
	writer   *telemetry.Writer
	now      time.Time
	reads    int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	now := func() time.Time { f.reads++; return f.now }
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "sessions.db"), Migrations: sessions.Migrations(), Now: now, Service: pages.ServiceName, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	f.database = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	f.writer = telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: &telemetry.Capture{}, Stderr: io.Discard, Now: func() time.Time { return f.now }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 4096))})
	t.Cleanup(func() { f.writer.Shutdown(context.Background(), "fixture complete") })
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	f.cfg = pages.Config{func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Release: "fixture-release", Commit: "fixture-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Trail: []page.Level{{Name: "discard", URL: "/discard"}}}
	}, set, "", store.New(d, store.Config{Now: now}), mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: f.writer})}
	return f
}

func request(method, target, user string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader("caller body"))
	r.Host = "sessions.fixture.test:8443"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-User-Id", user)
	r.Header["X-User-Email"] = []string{"owner@example.test", "ignored@example.test"}
	return r
}

func (f *fixture) answer(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	identity.Optional(pages.Handler(f.cfg)).ServeHTTP(rec, r)
	return rec
}

func (f *fixture) banner(r *http.Request, trail []page.Level) page.Banner {
	auth := pages.Auth(r, f.cfg.ServicesPath)
	b := f.cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
	b.Trail = trail
	return b
}

func requirePage(t *testing.T, got *httptest.ResponseRecorder, status int, name string, data any) {
	t.Helper()
	if got.Code != status || got.Body.String() != render(t, name, data) {
		t.Fatalf("%s: status %d, rendered bytes differ=%t", name, got.Code, got.Body.String() != render(t, name, data))
	}
	if v := got.Header().Values("Content-Type"); len(v) != 1 || v[0] != "text/html; charset=utf-8" {
		t.Fatalf("content type %v", v)
	}
	if _, ok := got.Header()["Set-Cookie"]; ok {
		t.Fatal("cookie")
	}
}

// R-0UWY-JD25 R-0W4U-X4SU R-1FN9-1GNY R-1GV5-F8EN
// R-1I31-T05C R-1JAY-6RW1 R-1KIU-KJMQ R-1LQQ-YBDF
// R-1WPU-E91O R-1Z5N-5SJ2 R-20DJ-JK9R
func TestPageDecisions(t *testing.T) {
	f := newFixture(t)
	f.cfg.ServicesPath = serviceFile(t, services.Entry{Name: "auth", URL: "https://accounts.fixture.test///"})
	for _, failing := range []bool{false, true} {
		f.database.SetFailing(failing)
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"} {
			for _, target := range []string{"/", "/tools?x=1", "/about?x=1", "/nope?x=1", "/%74ools"} {
				r := request(method, target, "")
				got := f.answer(r)
				if got.Code != 302 || got.Body.Len() != 0 || len(got.Header().Values("Location")) != 1 || got.Header().Get("Location") != pages.SignIn(r, f.cfg.ServicesPath) {
					t.Fatalf("guest %s %s: %d %v %q", method, target, got.Code, got.Header(), got.Body.String())
				}
			}
		}
		for _, target := range []string{"/index.html", "/tools/", "/about/", "/TOOLS", "//", "/%74ools", "/sessions", "/weeks/2026-10-26"} {
			for _, method := range []string{"GET", "POST", "DELETE"} {
				r := request(method, target, "user-a")
				requirePage(t, f.answer(r), 404, "notfound", pages.NotFoundData{Banner: f.banner(r, nil)})
			}
		}
		for _, target := range []string{"/?week=2026-10-12", "/tools?x=1", "/about"} {
			for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
				got := f.answer(request(method, target, "user-a"))
				if got.Code != 405 || got.Body.Len() != 0 || len(got.Header().Values("Allow")) != 1 || got.Header().Get("Allow") != "GET, HEAD" {
					t.Fatalf("method: %d %v", got.Code, got.Header())
				}
			}
		}
		r := request("GET", "/about?ignored=1", "user-a")
		requirePage(t, f.answer(r), 200, "about", pages.AboutData{Banner: f.banner(r, []page.Level{pages.AboutLevel}), Description: pages.Description})
		if failing {
			for _, method := range []string{"GET", "HEAD"} {
				got := f.answer(request(method, "/?anything=yes", "user-a"))
				want := store.Unreachable + "\n"
				if method == "HEAD" {
					want = ""
				}
				if got.Code != 503 || got.Body.String() != want || len(got.Header().Values("Content-Type")) != 1 || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("refusal %d %v %q", got.Code, got.Header(), got.Body.String())
				}
			}
		}
		for _, target := range []string{"/", "/tools?x=1", "/about", "/nope", "/%74ools"} {
			get := f.answer(request("GET", target, "user-a"))
			head := f.answer(request("HEAD", target, "user-a"))
			a, _ := json.Marshal(get.Header())
			b, _ := json.Marshal(head.Header())
			if get.Code != head.Code || !bytes.Equal(a, b) || head.Body.Len() != 0 {
				t.Fatalf("HEAD %s differs", target)
			}
		}
	}
	f.database.SetFailing(false)
	if err := f.database.Close(); err != nil {
		t.Fatal(err)
	}
	if got := f.answer(request("GET", "/", "user-a")); got.Code != 503 || got.Body.String() != store.Unreachable+"\n" {
		t.Fatal("closed database not refused")
	}
}

// R-OI0D-FANP
func TestToolsCatalog(t *testing.T) {
	f := newFixture(t)
	check := func() {
		r := request("GET", "/tools?ignored=1", "user-a")
		rows := make([]pages.ToolRow, 0)
		for _, tool := range f.cfg.MCP.Tools() {
			rows = append(rows, pages.ToolRow{Name: tool.Name, Description: tools.FirstLine(tool.Description)})
		}
		for _, failing := range []bool{false, true} {
			f.database.SetFailing(failing)
			requirePage(t, f.answer(r), 200, "tools", pages.ToolsData{Banner: f.banner(r, []page.Level{pages.ToolsLevel}), Tools: rows})
		}
		f.database.SetFailing(false)
	}
	check()
	tools.Register(f.cfg.MCP, tools.Config{Store: f.cfg.Store, Telemetry: f.writer})
	check()
	const description = "Caller supplied.\nCaller continuation."
	mcp.AddRawTool(f.cfg.MCP, mcp.RawTool[struct{}]{Name: "caller_tool", Description: description, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) {
		return mcp.TextResult("fixture"), nil
	}})
	check()
}

func push(t *testing.T, f *fixture, id, dir string, start time.Time, turns []store.PushTurn) {
	t.Helper()
	if turns == nil {
		turns = []store.PushTurn{{Moment: start.Format(time.RFC3339Nano), Whose: "person"}, {Moment: start.Add(10 * time.Minute).Format(time.RFC3339Nano), Whose: "agent", Model: "fixture-model", Input: 18204}, {Moment: start.Add(40 * time.Minute).Format(time.RFC3339Nano), Whose: "person"}}
	}
	_, err := f.cfg.Store.Push(t.Context(), "pusher", "pusher@example.test", []store.PushSession{{Tool: "claude", ID: id, Host: "fixture-host", Directory: dir, Turns: turns}})
	if err != nil {
		t.Fatal(err)
	}
}

func expectedDuration(d time.Duration) pages.Duration {
	seconds := int(d / time.Second)
	return pages.Duration{Seconds: seconds, Hours: seconds / 3600, Minutes: seconds / 60 % 60}
}
func expectedRow(kind string, f figures.Figures) pages.FiguresRow {
	// Group from left chunks, independently of the handler's digit walk.
	text := strconv.FormatInt(f.Tokens.Input+f.Tokens.Cached+f.Tokens.Output+f.Tokens.Reasoning, 10)
	for at := len(text) - 3; at > 0; at -= 3 {
		text = text[:at] + "," + text[at:]
	}
	return pages.FiguresRow{Kind: kind, Attention: expectedDuration(f.Attention), Wall: expectedDuration(f.Wall), Agent: expectedDuration(f.Agent), Tokens: text, Dollars: f.Dollars.Dollars()}
}
func landingData(f *fixture, r *http.Request, l store.Landing) pages.LandingData {
	data := pages.LandingData{Banner: f.banner(r, nil), AwayLimit: expectedDuration(l.AwayLimit), Weeks: make([]pages.Week, 0, len(l.Weeks))}
	for _, w := range l.Weeks {
		week := pages.Week{Start: w.Start.UTC().Format("2006-01-02"), Kinds: make([]pages.FiguresRow, 0, len(w.Kinds)), Total: expectedRow("", w.Total)}
		for _, k := range w.Kinds {
			week.Kinds = append(week.Kinds, expectedRow(string(k.Kind), k.Figures))
			total := int64(w.Total.Attention / time.Second)
			u := int64(k.Figures.Attention / time.Second)
			if k.Kind == figures.KindUntagged && total > 0 {
				week.UntaggedShare = int((u*100*2/total + 1) / 2)
			}
		}
		data.Weeks = append(data.Weeks, week)
	}
	return data
}

// R-1MYN-C344 R-1O6J-PUUT R-1PEG-3MLI R-1QMC-HEC7
// R-1RU8-V62W R-1UA1-MPKA R-1VHY-0HAZ R-2418-OVHU
func TestLandingAndLiveFigures(t *testing.T) {
	f := newFixture(t)
	check := func() {
		r := request("GET", "/?ignored=1", "reader-a")
		l, err := f.cfg.Store.Landing(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		before := f.reads
		got := f.answer(r)
		if l.Held && f.reads-before != 1 {
			t.Fatalf("landing read clock %d times", f.reads-before)
		}
		requirePage(t, got, 200, "landing", landingData(f, r, l))
		r.Header.Set("X-User-Id", "reader-b")
		second := f.answer(r)
		if got.Code != second.Code || got.Body.String() != second.Body.String() {
			t.Fatal("user id changes business figures")
		}
	}
	check()
	start := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if _, _, err := f.cfg.Store.AddRule(t.Context(), "fixture-host", "/work/build", "", "build"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.cfg.Store.SetPrice(t.Context(), store.PriceInput{Model: "fixture-model", Effective: "2026-01-01", Input: "4", Cached: "0.2", Output: "20", Reasoning: "20"}); err != nil {
		t.Fatal(err)
	}
	push(t, f, "build-session", "/work/build", start, nil)
	check()
	push(t, f, "other-session", "/work/other", start, nil)
	check()
	// Four token kinds and a duration above ninety-nine hours reach the page.
	push(t, f, "long-session", "/work/build", start.Add(-101*time.Hour), []store.PushTurn{
		{Moment: start.Add(-101 * time.Hour).Format(time.RFC3339Nano), Whose: "person"},
		{Moment: start.Add(-time.Hour).Format(time.RFC3339Nano), Whose: "agent", Model: "fixture-model", Input: 999, Cached: 1000, Output: 1234567, Reasoning: 18204},
		{Moment: start.Format(time.RFC3339Nano), Whose: "person"},
	})
	check()

	if _, _, err := f.cfg.Store.AddRule(t.Context(), "fixture-host", "/work/other", "", "sell"); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err := f.cfg.Store.SetAwayLimit(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	check()
	if _, _, err := f.cfg.Store.SetPrice(t.Context(), store.PriceInput{Model: "fixture-model", Effective: "2026-01-01", Input: "5", Cached: "0.2", Output: "20", Reasoning: "20"}); err != nil {
		t.Fatal(err)
	}
	check()
	f.now = f.now.AddDate(1, 0, 0)
	check()
}

func TestLandingFractionalSecondsAndRounding(t *testing.T) {
	for _, tc := range []struct {
		untagged, total time.Duration
		want            int
	}{{0, 900 * time.Second, 0}, {900 * time.Second, 900 * time.Second, 100}, {time.Second, 200 * time.Second, 1}, {time.Second, 201 * time.Second, 0}, {2 * time.Second, 3 * time.Second, 67}, {time.Second, 3 * time.Second, 33}, {999 * time.Millisecond, 999 * time.Millisecond, 0}} {
		t.Run(fmt.Sprintf("%s-of-%s", tc.untagged, tc.total), func(t *testing.T) {
			f := newFixture(t)
			start := f.now.Add(-time.Hour)
			if _, _, err := f.cfg.Store.AddRule(t.Context(), "fixture-host", "/tagged", "", "build"); err != nil {
				t.Fatal(err)
			}
			for i, d := range []time.Duration{tc.untagged, tc.total - tc.untagged} {
				dir := "/untagged"
				if i == 1 {
					dir = "/tagged"
				}
				push(t, f, fmt.Sprint("fraction-", i), dir, start, []store.PushTurn{{Moment: start.Format(time.RFC3339Nano), Whose: "person"}, {Moment: start.Format(time.RFC3339Nano), Whose: "agent", Input: 1234567, Model: "unpriced-fixture"}, {Moment: start.Add(d).Format(time.RFC3339Nano), Whose: "person"}})
			}
			l, err := f.cfg.Store.Landing(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			r := request("GET", "/", "user-a")
			data := landingData(f, r, l)
			if data.Weeks[0].UntaggedShare != tc.want {
				t.Fatalf("fixture share %d want %d", data.Weeks[0].UntaggedShare, tc.want)
			}
			requirePage(t, f.answer(r), 200, "landing", data)
		})
	}
}

func snapshot(t *testing.T, s *store.Store) []byte {
	t.Helper()
	ctx := t.Context()
	landing, e1 := s.Landing(ctx)
	rows, e2 := s.Sessions(ctx, store.Filter{}, store.Position{}, 100)
	rules, e3 := s.Rules(ctx)
	prices, e4 := s.Prices(ctx)
	away, e5 := s.AwayLimit(ctx)
	for _, err := range []error{e1, e2, e3, e4, e5} {
		if err != nil {
			t.Fatal(err)
		}
	}
	value, err := json.Marshal([]any{landing, rows, rules, prices, away})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// R-21LF-XC0G R-22TC-B3R5
func TestPagesLeaveRecordsAndSocketsUntouched(t *testing.T) {
	f := newFixture(t)
	dir, err := os.MkdirTemp("", "pages-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	var count atomic.Int64
	var entries []services.Entry
	for _, name := range []string{"auth", "repos"} {
		path := filepath.Join(dir, name+".sock")
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				count.Add(1)
				_, _ = conn.Write([]byte{1})
				_ = conn.Close()
			}
		}()
		t.Cleanup(func() { _ = ln.Close(); <-done })
		entries = append(entries, services.Entry{Name: name, URL: "https://" + name + ".fixture.test", Socket: path, Enabled: true, MCP: true})
	}
	f.cfg.ServicesPath = serviceFile(t, entries...)
	t.Setenv(services.Variable, f.cfg.ServicesPath)
	push(t, f, "kept-session", "/work", f.now.Add(-time.Hour), nil)
	before := snapshot(t, f.cfg.Store)
	for _, tc := range []struct{ method, target, user string }{{"GET", "/", "user-a"}, {"GET", "/tools", "user-a"}, {"GET", "/about", "user-a"}, {"POST", "/nope", "user-a"}, {"DELETE", "/", "user-a"}, {"GET", "/", ""}} {
		got := f.answer(request(tc.method, tc.target, tc.user))
		if _, ok := got.Header()["Set-Cookie"]; ok {
			t.Fatal("cookie")
		}
		if !bytes.Equal(before, snapshot(t, f.cfg.Store)) {
			t.Fatalf("record changed: %s %s", tc.method, tc.target)
		}
	}
	// A completed probe on each listener follows all page attempts on that socket.
	for _, entry := range entries {
		conn, err := net.Dial("unix", entry.Socket)
		if err != nil {
			t.Fatal(err)
		}
		var ack [1]byte
		_, err = io.ReadFull(conn, ack[:])
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if count.Load() != 2 {
		t.Fatalf("socket connections %d, want only the two fixture probes", count.Load())
	}
}
