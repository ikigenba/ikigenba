package tools_test

import (
	"bytes"
	"context"
	"crypto/sha256"
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

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

type harness struct {
	db                   *db.DB
	sequence             atomic.Uint64
	cfg                  tools.Config
	cacheRoot, reposRoot string
	capture              *telemetry.Capture
	server               *mcp.Server
	client               *mcp.Client
	now                  time.Time
	after                func(time.Duration) <-chan time.Time
	gitPath              string
	env                  []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	root := t.TempDir()
	gp, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{cacheRoot: filepath.Join(root, "cache", "sites"), reposRoot: filepath.Join(root, "repositories"), now: time.Date(2024, 2, 3, 4, 5, 6, 123, time.FixedZone("fixture", 3600)), gitPath: gp, capture: &telemetry.Capture{}}
	h.env = []string{"HOME=" + root, "XDG_CONFIG_HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2024-02-03T04:05:06Z", "GIT_COMMITTER_DATE=2024-02-03T04:05:06Z"}
	g, err := git.Find(filepath.Dir(gp), func() []string { return h.env })
	if err != nil {
		t.Fatal(err)
	}
	h.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	l := limits.New(settings.Defaults(), limits.Clock{After: func(d time.Duration) <-chan time.Time { return h.after(d) }})
	c, err := cache.Open(cache.Config{Root: h.cacheRoot, Repos: h.reposRoot, Git: g, Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	h.db, err = db.Open(context.Background(), db.Config{Path: filepath.Join(root, "catalog.db"), Migrations: sites.Migrations(), Now: func() time.Time { return h.now }})
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(h.db, store.Config{Now: func() time.Time { return h.now }, Rand: fixtureRandom(24)})
	w := telemetry.New(telemetry.Config{Service: "sites", Version: "fixture", Sink: h.capture, Now: func() time.Time { return time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC) }, Rand: fixtureRandom(25), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	w.Ready()
	h.cfg = tools.Config{Store: s, Cache: c, Limits: l, Telemetry: w}
	resetServer(t, h)
	t.Cleanup(func() { _ = h.db.Close(); w.Shutdown(context.Background(), "test finished") })
	return h
}
func (h *harness) call(t *testing.T, user, name, args string) mcp.Result {
	t.Helper()
	seq := h.sequence.Add(1)
	r, err := h.client.CallTool(context.Background(), identity.Caller{UserID: user, RequestID: fmt.Sprintf("req_%032x", seq)}, name, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}
func resultObject(t *testing.T, r mcp.Result) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var o struct {
		Structured json.RawMessage               `json:"structuredContent"`
		Content    []struct{ Type, Text string } `json:"content"`
	}
	if err = json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(b, &members); err != nil {
		t.Fatal(err)
	}
	if _, present := members["isError"]; present {
		t.Fatalf("success has isError: %s", b)
	}
	if r.IsError() || len(o.Content) != 1 || o.Content[0].Type != "text" || o.Content[0].Text != string(o.Structured) {
		t.Fatalf("bad success %s", b)
	}
	return o.Structured
}
func refusal(t *testing.T, r mcp.Result) string {
	t.Helper()
	b, _ := json.Marshal(r)
	var o struct {
		Content []struct{ Type, Text string } `json:"content"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	if !r.IsError() || len(o.Content) != 1 || o.Content[0].Type != "text" {
		t.Fatalf("bad refusal %s", b)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(b, &members); err != nil {
		t.Fatal(err)
	}
	delete(members, "_meta")
	want, _ := json.Marshal(mcp.ErrorResult(o.Content[0].Text))
	var expected map[string]json.RawMessage
	_ = json.Unmarshal(want, &expected)
	if len(members) != len(expected) || string(members["content"]) != string(expected["content"]) || string(members["isError"]) != "true" {
		t.Fatalf("refusal members %s", b)
	}
	return o.Content[0].Text
}
func (h *harness) add(t *testing.T, owner, name string, listed bool) store.Site {
	t.Helper()
	s, err := h.cfg.Store.Create(context.Background(), store.Draft{Owner: owner, Name: name, Repo: "rep_0123456789abcdef", Ref: "main", Visibility: store.Public, Listed: listed})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func (h *harness) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.gitPath, args...)
	cmd.Env = h.env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}
func (h *harness) repo(t *testing.T, id, owner string) string {
	t.Helper()
	p := filepath.Join(h.reposRoot, id+".git")
	if err := os.MkdirAll(h.reposRoot, 0700); err != nil {
		t.Fatal(err)
	}
	h.git(t, h.reposRoot, "init", "--bare", "--initial-branch=main", p)
	h.git(t, p, "config", "ikigenba.owner", owner)
	return p
}

func resetServer(t *testing.T, h *harness) {
	t.Helper()
	h.server = mcp.NewServer(mcp.ServerConfig{Name: "sites", Version: "fixture", Telemetry: h.cfg.Telemetry})
	tools.Register(h.server, h.cfg)
	wrapped := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.server.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), "https://sites.example.invalid")))
	}))
	ts := httptest.NewServer(telemetry.Middleware(h.cfg.Telemetry, wrapped))
	h.client = mcp.NewClient(mcp.ClientConfig{Endpoint: ts.URL, HTTPClient: ts.Client()})
	t.Cleanup(ts.Close)
}

func fixtureRandom(seed int) *bytes.Reader {
	data := make([]byte, 0, 65536)
	for i := 0; i < 2048; i++ {
		block := sha256.Sum256(fmt.Appendf(nil, "fixture/%d/%d", seed, i))
		data = append(data, block[:]...)
	}
	return bytes.NewReader(data)
}
