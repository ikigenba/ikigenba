package web_test

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	gitpkg "github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/web"
)

type fixture struct {
	cfg                 web.Config
	h                   http.Handler
	capture             *telemetry.Capture
	root, repo, gitPath string
	env                 []string
	after               func(time.Duration) <-chan time.Time
	unpacked            func(string, string)
	seq                 atomic.Uint64
}

func fresh(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	f := &fixture{root: t.TempDir(), capture: &telemetry.Capture{}}
	gp, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	f.gitPath = gp
	f.env = []string{"HOME=" + f.root, "XDG_CONFIG_HOME=" + f.root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2024-02-03T04:05:06Z", "GIT_COMMITTER_DATE=2024-02-03T04:05:06Z"}
	g, e := gitpkg.Find(filepath.Dir(gp), func() []string { return f.env })
	if e != nil {
		t.Fatal(e)
	}
	f.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	l := limits.New(settings.Defaults(), limits.Clock{After: func(d time.Duration) <-chan time.Time { return f.after(d) }})
	c, e := cache.Open(cache.Config{Root: filepath.Join(f.root, "cache"), Repos: filepath.Join(f.root, "repos"), Git: g, Limits: l, Unpacked: func(i, h string) {
		if f.unpacked != nil {
			f.unpacked(i, h)
		}
	}})
	if e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(context.Background(), store.Config{Source: filepath.Join(f.root, "state", "catalog.db"), Now: func() time.Time { return time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC) }, Rand: bytes.NewReader(randomBytes(61))})
	if e != nil {
		t.Fatal(e)
	}
	wr := telemetry.New(telemetry.Config{Service: "sites", Version: "fixture", Sink: f.capture, Stderr: io.Discard, Now: func() time.Time { return time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC) }, Rand: bytes.NewReader(randomBytes(62)), Sleep: func(context.Context, time.Duration) {}})
	f.cfg = web.Config{Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: mcp.NewServer(mcp.ServerConfig{Name: "sites", Version: "fixture", Telemetry: wr}), Store: st, Cache: c, Limits: l, Telemetry: wr, Rand: bytes.NewReader(randomBytes(63))}
	f.h = web.Handler(f.cfg)
	t.Cleanup(func() { _ = st.Close(); wr.Shutdown(context.Background(), "test finished") })
	return f
}
func (f *fixture) get(t *testing.T, method, path, host, user string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.Host = host
	r.Header.Set("X-Request-Id", fmt.Sprintf("req_%032x", f.seq.Add(1)))
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func (f *fixture) events(t *testing.T) []telemetry.Event {
	t.Helper()
	if e := f.cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	return f.capture.Events()
}
func (f *fixture) add(t *testing.T, name, visibility string) store.Site {
	t.Helper()
	s, e := f.cfg.Store.Create(context.Background(), store.Draft{Owner: "user", Name: name, Repo: "rep_0123456789abcdef", Ref: "main", Visibility: visibility, Listed: true})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func (f *fixture) command(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, f.gitPath, args...)
	c.Dir = dir
	c.Env = f.env
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func (f *fixture) repository(t *testing.T) {
	t.Helper()
	if e := os.MkdirAll(filepath.Join(f.root, "repos"), 0700); e != nil {
		t.Fatal(e)
	}
	f.repo = filepath.Join(f.root, "repos", "rep_0123456789abcdef.git")
	f.command(t, f.root, "init", "--bare", "--initial-branch=main", f.repo)
	f.command(t, f.repo, "config", "ikigenba.owner", "user")
}
func (f *fixture) commit(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(f.root, "content")
	if e := os.WriteFile(p, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	blob := f.command(t, f.repo, "hash-object", "-w", p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.gitPath, "mktree")
	cmd.Dir = f.repo
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader("100644 blob " + blob + "\tindex.html\n")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal(e, string(b))
	}
	tree := strings.TrimSpace(string(b))
	sha := f.command(t, f.repo, "commit-tree", tree, "-m", "fixture")
	f.command(t, f.repo, "update-ref", "refs/heads/main", sha)
	return sha
}
func (f *fixture) call(t *testing.T, name, args string) mcp.Result {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "sites"; f.h.ServeHTTP(w, r) }))
	defer ts.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: ts.URL + "/mcp", HTTPClient: ts.Client()})
	r, e := client.CallTool(context.Background(), identity.Caller{UserID: "user", RequestID: fmt.Sprintf("req_%032x", f.seq.Add(1))}, name, json.RawMessage(args))
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func newServer(w *telemetry.Writer) *mcp.Server {
	return mcp.NewServer(mcp.ServerConfig{Name: "sites", Version: "fixture", Telemetry: w})
}

func replaceSmallCache(t *testing.T, f *fixture) {
	t.Helper()
	settingsValue, err := settings.Read(func(key string) (string, bool) {
		if key == "SITE_MAX_BYTES" {
			return "7", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	l := limits.New(settingsValue, limits.Clock{After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	g, err := gitpkg.Find(filepath.Dir(f.gitPath), func() []string { return f.env })
	if err != nil {
		t.Fatal(err)
	}
	c, err := cache.Open(cache.Config{Root: filepath.Join(f.root, "cache"), Repos: filepath.Join(f.root, "repos"), Git: g, Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Cache = c
	f.cfg.Limits = l
	f.cfg.MCP = newServer(f.cfg.Telemetry)
	f.h = web.Handler(f.cfg)
}

func randomBytes(seed byte) []byte {
	b := make([]byte, 65536)
	for i := range b {
		b[i] = seed + byte(i) + byte(i/256)
	}
	return b
}
