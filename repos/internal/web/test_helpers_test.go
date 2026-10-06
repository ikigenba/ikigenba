package web

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

type webBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *webBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *webBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type webRandom struct {
	mu sync.Mutex
	n  byte
}

func (r *webRandom) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	for i := range p {
		p[i] = r.n
	}
	return len(p), nil
}

type webFixture struct {
	db          *db.DB
	cfg         Config
	dir         string
	gitPath     string
	gitEnv      []string
	gitCalls    atomic.Int64
	bannerCalls atomic.Int64
	capture     *telemetry.Capture
	stderr      *webBuffer
	busCapture  *events.Capture
	busStderr   *webBuffer
}

func newWebFixture(t *testing.T) *webFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &webFixture{dir: t.TempDir(), capture: &telemetry.Capture{}, stderr: &webBuffer{}}
	var err error
	f.gitPath, err = exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	f.gitEnv = []string{"PATH=" + filepath.Dir(f.gitPath), "HOME=" + f.dir, "XDG_CONFIG_HOME=" + f.dir,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture",
		"GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2001-02-03T04:05:06Z", "GIT_COMMITTER_DATE=2001-02-03T04:05:06Z"}
	g, err := git.Find(filepath.Dir(f.gitPath), func() []string {
		f.gitCalls.Add(1)
		return append([]string(nil), f.gitEnv...)
	})
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC) }
	random := &webRandom{}
	f.db, err = db.Open(t.Context(), db.Config{Path: filepath.Join(f.dir, "repos.db"), Migrations: repos.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.Context(), f.db, store.Config{Root: filepath.Join(f.dir, "repos"), Git: g, Now: now, Rand: random})
	if err != nil {
		t.Fatal(err)
	}
	f.cfg = Config{Store: s, Git: g, Limits: limits.New(settings.Defaults(), limits.Clock{Now: now, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})}
	f.cfg.Banner = func(u page.User) page.Banner {
		f.bannerCalls.Add(1)
		return page.Banner{Service: ServiceName, Version: "fixture-version", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}
	f.setWriter(t, f.capture, random, now)
	f.busCapture, f.busStderr = &events.Capture{}, &webBuffer{}
	f.setEmitter(t, f.busCapture)
	t.Cleanup(func() {
		if err := f.db.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

func (f *webFixture) setWriter(t *testing.T, sink telemetry.Sink, random io.Reader, now func() time.Time) {
	t.Helper()
	f.cfg.Telemetry = telemetry.New(telemetry.Config{Service: ServiceName, Version: "fixture-version", Sink: sink, Stderr: f.stderr,
		Now: now, Rand: random, Sleep: func(context.Context, time.Duration) {}})
	w := f.cfg.Telemetry
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		w.Shutdown(ctx, "test complete")
	})
	f.cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "fixture-version", Telemetry: w, Instructions: func(context.Context) string { return "" }})
}

func webRequest(h http.Handler, method, target, user, id string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	if id != "" {
		r.Header.Set("X-Request-Id", id)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func (f *webFixture) events(t *testing.T) []telemetry.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := f.cfg.Telemetry.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	return f.capture.Events()
}

func (f *webFixture) repo(t *testing.T, owner, name string) store.Repo {
	t.Helper()
	r, err := f.cfg.Store.Create(t.Context(), owner, name)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *webFixture) git(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.gitPath, args...)
	cmd.Dir, cmd.Env = dir, append([]string(nil), f.gitEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}

func webServices(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *webFixture) setEmitter(t *testing.T, sink events.Sink) {
	t.Helper()
	f.cfg.Events = events.New(events.Config{Service: ServiceName, Sink: sink, Stderr: f.busStderr, Now: func() time.Time { return time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC) }, Rand: &webRandom{}, Telemetry: f.cfg.Telemetry, Emits: smarthttp.Emits(), Sleep: func(ctx context.Context, _ time.Duration) { <-ctx.Done() }})
	emitter := f.cfg.Events
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		emitter.Shutdown(ctx)
	})
}
func (f *webFixture) busEvents(t *testing.T) []events.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := f.cfg.Events.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	return f.busCapture.Events()
}
