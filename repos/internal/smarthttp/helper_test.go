package smarthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

var epoch = time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

type sequence struct {
	mu sync.Mutex
	n  byte
}

func (s *sequence) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range p {
		s.n++
		p[i] = s.n
	}
	return len(p), nil
}

type timerCall struct {
	duration time.Duration
	fire     chan time.Time
}
type drivenClock struct {
	mu     sync.Mutex
	now    time.Time
	calls  int
	timers chan timerCall
}

func newClock() *drivenClock                   { return &drivenClock{now: epoch, timers: make(chan timerCall, 128)} }
func (c *drivenClock) read() time.Time         { c.mu.Lock(); defer c.mu.Unlock(); c.calls++; return c.now }
func (c *drivenClock) advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }
func (c *drivenClock) count() int              { c.mu.Lock(); defer c.mu.Unlock(); return c.calls }
func (c *drivenClock) after(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.timers <- timerCall{d, ch}
	return ch
}
func (c *drivenClock) take(t *testing.T) timerCall {
	t.Helper()
	ctx := deadline(t)
	select {
	case call := <-c.timers:
		return call
	case <-ctx.Done():
		t.Fatal("timer call not observed")
		return timerCall{}
	}
}

type fixture struct {
	t                *testing.T
	root, executable string
	env              []string
	git              *git.Git
	db               *db.DB
	store            *store.Store
	clock            *drivenClock
	settings         settings.Settings
	limits           *limits.Limits
	writer           *telemetry.Writer
	capture          *telemetry.Capture
}

func setup(t *testing.T) *fixture {
	t.Helper()
	path, err := exec.LookPath("git")
	must(t, err)
	root := t.TempDir()
	env := []string{"HOME=" + root, "XDG_CONFIG_HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(root, "global"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2025-01-02T03:04:05Z", "GIT_COMMITTER_DATE=2025-01-02T03:04:05Z", "PATH=" + filepath.Dir(path)}
	write(t, filepath.Join(root, "global"), "")
	g, err := git.Find(filepath.Dir(path), func() []string { return append([]string(nil), env...) })
	must(t, err)
	c := newClock()
	capture := new(telemetry.Capture)
	w := telemetry.New(telemetry.Config{Service: "repos", Sink: capture, Now: func() time.Time { return epoch }, Rand: new(sequence), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	d, err := db.Open(deadline(t), db.Config{Path: filepath.Join(root, "catalog.db"), Migrations: repos.Migrations(), Now: func() time.Time { return epoch }})
	must(t, err)
	s, err := store.Open(deadline(t), d, store.Config{Root: filepath.Join(root, "repos"), Git: g, Now: func() time.Time { return epoch }, Rand: new(sequence)})
	must(t, err)
	f := &fixture{t: t, root: root, executable: path, env: env, git: g, db: d, store: s, clock: c, settings: settings.Defaults(), writer: w, capture: capture}
	f.settings.ReadSlots = 1
	f.settings.WriteSlots = 1
	f.settings.QueueLength = 1
	f.resetLimits()
	t.Cleanup(func() { must(t, d.Close()); w.Shutdown(deadline(t), "test") })
	return f
}
func (f *fixture) resetLimits() {
	f.limits = limits.New(f.settings, limits.Clock{Now: f.clock.read, After: f.clock.after})
}
func (f *fixture) config() smarthttp.Config {
	return smarthttp.Config{Store: f.store, Git: f.git, Limits: f.limits, Telemetry: f.writer}
}
func (f *fixture) handler() http.Handler {
	return telemetry.Middleware(f.writer, identity.Require(smarthttp.Handler(f.config())))
}
func (f *fixture) create(name string) store.Repo {
	f.t.Helper()
	r, err := f.store.Create(deadline(f.t), "alice", name)
	must(f.t, err)
	return r
}
func (f *fixture) gitRun(dir string, args ...string) string {
	f.t.Helper()
	b, err := f.gitTry(dir, args...)
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s\nevents:%v", args, err, b, f.events())
	}
	return string(b)
}
func (f *fixture) gitTry(dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(deadline(f.t), f.executable, args...)
	cmd.Dir = dir
	cmd.Env = append([]string(nil), f.env...)
	return cmd.CombinedOutput()
}
func (f *fixture) working(name string) string {
	dir := filepath.Join(f.root, name)
	must(f.t, os.MkdirAll(dir, 0700))
	f.gitRun(dir, "init", "-b", "main")
	return dir
}
func (f *fixture) commit(dir, text string) string {
	write(f.t, filepath.Join(dir, "file"), text)
	f.gitRun(dir, "add", "file")
	f.gitRun(dir, "commit", "-m", "fixture "+text)
	return strings.TrimSpace(f.gitRun(dir, "rev-parse", "HEAD"))
}
func (f *fixture) client(dir string, args ...string) string {
	prefix := []string{"-c", "http.extraHeader=X-User-Id: alice", "-c", "http.extraHeader=X-Request-Id: request"}
	return f.gitRun(dir, append(prefix, args...)...)
}
func (f *fixture) server() *httptest.Server {
	s := httptest.NewServer(f.handler())
	f.t.Cleanup(s.Close)
	return s
}
func (f *fixture) request(method, path string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-git-"+strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "git-")+"-request")
	}
	r.Header.Set("X-User-Id", "alice")
	r.Header.Set("X-Request-Id", "request")
	w := httptest.NewRecorder()
	f.handler().ServeHTTP(w, r)
	return w
}
func (f *fixture) events() []telemetry.Event {
	must(f.t, f.writer.Flush(deadline(f.t)))
	return f.capture.Events()
}
func (f *fixture) refs(id string) string {
	return f.gitRun("", "--git-dir="+f.store.Dir(id), "for-each-ref", "--format=%(refname) %(objectname)")
}
func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func write(t *testing.T, path, text string) {
	t.Helper()
	must(t, os.WriteFile(path, []byte(text), 0600))
}
func same(t *testing.T, got, want any) {
	t.Helper()
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if !bytes.Equal(a, b) {
		t.Fatalf("got %s; want %s", a, b)
	}
}
func outcome(t *testing.T, w *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	same(t, w.Code, status)
	same(t, w.Body.String(), body)
	same(t, w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"})
}
func ownEvents(events []telemetry.Event) []telemetry.Event {
	var result []telemetry.Event
	for _, e := range events {
		if e.Name != "request.started" && e.Name != "request.finished" {
			result = append(result, e)
		}
	}
	return result
}
func packet(payload string) string { return fmt.Sprintf("%04x%s", len(payload)+4, payload) }

func findFixtureGit(f *fixture) (*git.Git, error) {
	return git.Find(filepath.Dir(f.executable), func() []string { return append([]string(nil), f.env...) })
}
