package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/tools"
)

type sequence struct {
	mu sync.Mutex
	n  byte
}

func (s *sequence) Read(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	for i := range b {
		b[i] = s.n
	}
	return len(b), nil
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	http                                      *http.Client
	t                                         *testing.T
	root, repo, path, gitPath, gitExec, trace string
	env                                       []string
	st                                        *store.Store
	db                                        *db.DB
	core                                      *runs.Core
	src                                       *source.Source
	writer                                    *telemetry.Writer
	capture                                   *telemetry.Capture
	server                                    *mcp.Server
	client                                    *mcp.Client
	lim                                       *limits.Limits
	after                                     func(time.Duration) <-chan time.Time
	events                                    chan telemetry.Event
	now                                       time.Time
}
type sink struct {
	capture *telemetry.Capture
	events  chan telemetry.Event
}

func (s sink) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(ctx, e)
	s.events <- e
	return nil
}
func setup(t *testing.T, script string, options ...func(*runs.Config)) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	root := t.TempDir()
	g, e := exec.LookPath("git")
	must(t, e)
	py, e := exec.LookPath(runner.Interpreter)
	must(t, e)
	path := filepath.Dir(g) + ":" + filepath.Dir(py)
	h := &fixture{t: t, root: root, repo: filepath.Join(root, "repos", "rep_1111111111111111.git"), path: path, gitPath: g, trace: filepath.Join(root, "trace"), capture: &telemetry.Capture{}, events: make(chan telemetry.Event, 256)}
	h.env = []string{"PATH=" + path, "HOME=" + root, "XDG_CONFIG_HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2025-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2025-01-01T00:00:00Z"}
	work := filepath.Join(root, "work")
	must(t, os.MkdirAll(work, 0700))
	h.command(work, "init", "--initial-branch=main")
	must(t, os.WriteFile(filepath.Join(work, "main.py"), []byte(script), 0600))
	h.command(work, "add", ".")
	h.command(work, "commit", "-m", "fixture")
	must(t, os.MkdirAll(filepath.Dir(h.repo), 0700))
	h.command(root, "clone", "--bare", work, h.repo)
	h.command(h.repo, "config", "ikigenba.owner", "alice")
	h.env = append(h.env, "GIT_TRACE2_EVENT="+h.trace)
	h.now = time.Date(2025, 1, 2, 3, 4, 5, 456000000, time.FixedZone("east", 3600))
	now := func() time.Time { return h.now }
	h.db, e = db.Open(context.Background(), db.Config{Path: filepath.Join(root, "catalog.db"), Migrations: scripts.Migrations(), Now: now})
	must(t, e)
	h.st = store.New(h.db, store.Config{Now: now, Rand: &sequence{}})
	h.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	h.lim = limits.New(settings.Defaults(), limits.Clock{After: func(d time.Duration) <-chan time.Time { return h.after(d) }})
	gitDir := filepath.Join(root, "git-bin")
	must(t, os.MkdirAll(gitDir, 0700))
	h.gitExec = filepath.Join(gitDir, "git")
	must(t, os.Symlink(g, h.gitExec))
	gg, e := git.Find(gitDir, func() []string { return append([]string{}, h.env...) })
	must(t, e)
	h.src = source.New(source.Config{Repos: filepath.Dir(h.repo), Git: gg, Limits: h.lim})
	h.writer = telemetry.New(telemetry.Config{Service: "scripts", Sink: sink{h.capture, h.events}, Stderr: io.Discard, Now: now, Rand: &sequence{}})
	coreConfig := runs.Config{MaxActive: 100, MaxQueued: 100, Store: h.st, Source: h.src, Writer: h.writer, Runs: filepath.Join(root, "runs"), Path: path, Services: filepath.Join(root, "services"), ScriptSeconds: 30, OutputMaxBytes: 128, KeepDays: 1, KeepCount: 2, Now: now, ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: &sequence{}}
	for _, option := range options {
		option(&coreConfig)
	}
	h.core = runs.New(coreConfig)
	// R-2W8K-10WN R-2XGG-ESNC R-1473-0JA4 R-3KMJ-OFQJ
	h.server = mcp.NewServer(mcp.ServerConfig{Name: "scripts", Version: "test", Telemetry: h.writer})
	func(register func(*mcp.Server, tools.Config)) {
		register(h.server, tools.Config{Store: h.st, Source: h.src, Runs: h.core, Telemetry: h.writer})
	}(tools.Register)
	httpServer := httptest.NewServer(identity.Require(h.server))
	h.http = httpServer.Client()
	h.client = mcp.NewClient(mcp.ClientConfig{Endpoint: httpServer.URL, HTTPClient: h.http, Name: "fixture", Version: "test"})
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h.core.Drain(ctx)
		httpServer.Close()
		h.writer.Shutdown(context.Background(), "test")
		_ = h.db.Close()
		cleanupRoot, err := os.OpenRoot(root)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = cleanupRoot.Close() }()
		_ = fs.WalkDir(cleanupRoot.FS(), ".", func(p string, d fs.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				_ = cleanupRoot.Chmod(p, 0700)
			}
			return nil
		})
	})
	return h
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func (h *fixture) command(dir string, args ...string) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, h.gitPath, args...)
	c.Dir = dir
	c.Env = append([]string{}, h.env...)
	b, e := c.CombinedOutput()
	if e != nil {
		h.t.Fatalf("git %v: %v %s", args, e, b)
	}
}
func caller() identity.Caller { return identity.Caller{UserID: "alice", RequestID: "req-test"} }
func (h *fixture) call(tool string, a any) mcp.Result {
	h.t.Helper()
	b, e := json.Marshal(a)
	must(h.t, e)
	return h.raw(tool, string(b), caller())
}
func (h *fixture) raw(tool, a string, u identity.Caller) mcp.Result {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, e := h.client.CallTool(ctx, u, tool, json.RawMessage(a))
	must(h.t, e)
	return r
}
func object(t *testing.T, r mcp.Result) json.RawMessage {
	t.Helper()
	if r.IsError() {
		t.Fatalf("refusal %s", resultBytes(t, r))
	}
	var o struct {
		Structured json.RawMessage               `json:"structuredContent"`
		Content    []struct{ Type, Text string } `json:"content"`
	}
	must(t, json.Unmarshal([]byte(resultBytes(t, r)), &o))
	if len(o.Content) != 1 || o.Content[0].Type != "text" || o.Content[0].Text != string(o.Structured) {
		t.Fatal("inconsistent success", resultBytes(t, r))
	}
	return o.Structured
}
func resultBytes(t *testing.T, r mcp.Result) string {
	t.Helper()
	b, e := r.MarshalJSON()
	must(t, e)
	return string(b)
}
func refusal(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	if !r.IsError() {
		t.Fatal("wanted refusal", resultBytes(t, r))
	}
	var o map[string]json.RawMessage
	must(t, json.Unmarshal([]byte(resultBytes(t, r)), &o))
	delete(o, "_meta")
	b, e := json.Marshal(o)
	must(t, e)
	var expected map[string]json.RawMessage
	must(t, json.Unmarshal([]byte(resultBytes(t, mcp.ErrorResult(want))), &expected))
	bb, e := json.Marshal(expected)
	must(t, e)
	if string(b) != string(bb) {
		t.Fatalf("want %s got %s", bb, b)
	}
}
func decode[T any](t *testing.T, r mcp.Result) T {
	t.Helper()
	var v T
	must(t, json.Unmarshal(object(t, r), &v))
	return v
}
func (h *fixture) create(name string) tools.Script {
	return decode[tools.Script](h.t, h.call("create", tools.CreateArgs{Name: name, Repo: "rep_1111111111111111"}))
}
func (h *fixture) flush() {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	must(h.t, h.writer.Flush(ctx))
}

func TestToolMetadata(t *testing.T) {
	// R-7Q2L-WH86 R-7RAI-A8YV
	h := setup(t, "print(1)\n")
	infos, e := h.client.ListTools(context.Background(), caller())
	must(t, e)
	// R-490J-BUKF R-4JZM-RS8O R-4L7J-5JZD R-88D3-N1CL R-89L0-0T3A R-8ASW-EKTZ R-8C0S-SCKO R-8D8P-64BD R-XWK5-FXBJ R-XXS1-TP28 R-XYZY-7GSX R-Y07U-L8JM R-Y1FQ-Z0AB R-Y2NN-CS10 R-Y3VJ-QJRP R-Y53G-4BIE
	wants := []struct {
		name, input, output string
		effect              mcp.Effect
	}{
		{"list", "{\"type\":\"object\",\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"scripts\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"subscriptions\":{\"type\":\"integer\"},\"last_run\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"}},\"required\":[\"id\",\"status\",\"started\"],\"additionalProperties\":false}},\"required\":[\"id\",\"name\",\"repo\",\"ref\",\"subscriptions\"],\"additionalProperties\":false}}},\"required\":[\"scripts\"],\"additionalProperties\":false}", mcp.Read},
		{"show", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"name\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"subscriptions\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"event\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"}},\"required\":[\"event\",\"created\"],\"additionalProperties\":false}},\"last_run\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"}},\"required\":[\"id\",\"status\",\"started\"],\"additionalProperties\":false}},\"required\":[\"id\",\"name\",\"repo\",\"ref\",\"created\",\"subscriptions\"],\"additionalProperties\":false}", mcp.Read},
		{"create", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"},\"repo\":{\"type\":\"string\",\"description\":\"fixture\"},\"ref\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"name\",\"repo\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"subscriptions\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"event\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"}},\"required\":[\"event\",\"created\"],\"additionalProperties\":false}},\"last_run\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"}},\"required\":[\"id\",\"status\",\"started\"],\"additionalProperties\":false}},\"required\":[\"id\",\"name\",\"repo\",\"ref\",\"created\",\"subscriptions\"],\"additionalProperties\":false}", mcp.Additive},
		{"update", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"},\"ref\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"name\",\"ref\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"subscriptions\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"event\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"}},\"required\":[\"event\",\"created\"],\"additionalProperties\":false}},\"last_run\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"}},\"required\":[\"id\",\"status\",\"started\"],\"additionalProperties\":false}},\"required\":[\"id\",\"name\",\"repo\",\"ref\",\"created\",\"subscriptions\"],\"additionalProperties\":false}", mcp.Additive},
		{"delete", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"name\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"deleted\":{\"type\":\"boolean\"},\"id\":{\"type\":\"string\"}},\"required\":[\"deleted\",\"id\"],\"additionalProperties\":false}", mcp.Destructive},
		{"subscribe", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"},\"event\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"name\",\"event\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"subscriptions\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"event\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"}},\"required\":[\"event\",\"created\"],\"additionalProperties\":false}},\"last_run\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"}},\"required\":[\"id\",\"status\",\"started\"],\"additionalProperties\":false}},\"required\":[\"id\",\"name\",\"repo\",\"ref\",\"created\",\"subscriptions\"],\"additionalProperties\":false}", mcp.Additive},
		{"unsubscribe", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"},\"event\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"name\",\"event\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"subscriptions\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"event\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"}},\"required\":[\"event\",\"created\"],\"additionalProperties\":false}},\"last_run\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"}},\"required\":[\"id\",\"status\",\"started\"],\"additionalProperties\":false}},\"required\":[\"id\",\"name\",\"repo\",\"ref\",\"created\",\"subscriptions\"],\"additionalProperties\":false}", mcp.Destructive},
		{"run", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"},\"ref\":{\"type\":\"string\",\"description\":\"fixture\"},\"input\":{\"type\":\"object\",\"description\":\"fixture\"}},\"required\":[\"name\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"sha\":{\"type\":\"string\"},\"reason\":{\"type\":\"string\"}},\"required\":[\"id\",\"status\"],\"additionalProperties\":false}", mcp.Additive},
		{"runs", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"name\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"runs\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"sha\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"trigger\":{\"type\":\"string\"},\"event\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"},\"finished\":{\"type\":\"string\"},\"truncated\":{\"type\":\"boolean\"},\"reason\":{\"type\":\"string\"}},\"required\":[\"id\",\"ref\",\"trigger\",\"status\",\"started\",\"truncated\"],\"additionalProperties\":false}}},\"required\":[\"runs\"],\"additionalProperties\":false}", mcp.Read},
		{"result", "{\"type\":\"object\",\"properties\":{\"run\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"run\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"script\":{\"type\":\"string\"},\"sha\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"user\":{\"type\":\"string\"},\"request_id\":{\"type\":\"string\"},\"trigger\":{\"type\":\"string\"},\"event\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"},\"finished\":{\"type\":\"string\"},\"stdout_bytes\":{\"type\":\"integer\"},\"stderr_bytes\":{\"type\":\"integer\"},\"truncated\":{\"type\":\"boolean\"},\"reason\":{\"type\":\"string\"},\"stdout\":{\"type\":\"string\"},\"stderr\":{\"type\":\"string\"},\"files\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"path\":{\"type\":\"string\"},\"size\":{\"type\":\"integer\"}},\"required\":[\"path\",\"size\"],\"additionalProperties\":false}},\"files_gone\":{\"type\":\"boolean\"}},\"required\":[\"id\",\"script\",\"ref\",\"user\",\"request_id\",\"trigger\",\"status\",\"started\",\"stdout_bytes\",\"stderr_bytes\",\"truncated\"],\"additionalProperties\":false}", mcp.Read},
		{"cancel", "{\"type\":\"object\",\"properties\":{\"run\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"run\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"sha\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"trigger\":{\"type\":\"string\"},\"event\":{\"type\":\"string\"},\"status\":{\"type\":\"string\"},\"exit_code\":{\"type\":\"integer\"},\"started\":{\"type\":\"string\"},\"finished\":{\"type\":\"string\"},\"truncated\":{\"type\":\"boolean\"},\"reason\":{\"type\":\"string\"}},\"required\":[\"id\",\"ref\",\"trigger\",\"status\",\"started\",\"truncated\"],\"additionalProperties\":false}", mcp.Destructive},
	}
	if len(infos) != len(wants) {
		t.Fatalf("tool count %d", len(infos))
	}
	for i, w := range wants {
		g := infos[i]
		ann, e := json.Marshal(g.Annotations)
		must(t, e)
		wantAnn := `{"ReadOnlyHint":false,"DestructiveHint":false,"IdempotentHint":null,"OpenWorldHint":false}`
		switch w.effect {
		case mcp.Read:
			wantAnn = `{"ReadOnlyHint":true,"DestructiveHint":false,"IdempotentHint":null,"OpenWorldHint":false}`
		case mcp.Destructive:
			wantAnn = `{"ReadOnlyHint":false,"DestructiveHint":true,"IdempotentHint":null,"OpenWorldHint":false}`
		}
		if string(ann) != wantAnn {
			t.Errorf("%s annotations %s", w.name, ann)
		}
		if g.Name != w.name || g.Description == "" || schemaWithoutCopy(t, g.InputSchema) != w.input || string(g.OutputSchema) != w.output || g.Effect() != w.effect {
			t.Errorf("metadata %s: description %q; input %s; output %s; effect %v", w.name, g.Description, g.InputSchema, g.OutputSchema, g.Effect())
		}
	}
}
func TestArgumentShapes(t *testing.T) {
	// R-2YOC-SKE1 R-XFHK-34XT R-XGPG-GWOI R-XHXC-UOF7 R-XJ59-8G5W R-XMSY-DRDZ R-XP8R-5AVD R-XQGN-J2M2 R-XROJ-WUCR
	for _, tt := range []struct {
		value any
		want  string
	}{{tools.ListArgs{}, `{}`}, {tools.ShowArgs{Name: "job"}, `{"name":"job"}`}, {tools.CreateArgs{Name: "job", Repo: "rep"}, `{"name":"job","repo":"rep","ref":null}`}, {tools.UpdateArgs{Name: "job", Ref: "next"}, `{"name":"job","ref":"next"}`}, {tools.DeleteArgs{Name: "job"}, `{"name":"job"}`}, {tools.RunArgs{Name: "job", Input: json.RawMessage(`{}`)}, `{"name":"job","ref":null,"input":{}}`}, {tools.RunsArgs{Name: "job"}, `{"name":"job"}`}, {tools.ResultArgs{Run: "run"}, `{"run":"run"}`}, {tools.CancelArgs{Run: "run"}, `{"run":"run"}`}} {
		b, e := json.Marshal(tt.value)
		must(t, e)
		if string(b) != tt.want {
			t.Fatalf("%T: %s", tt.value, b)
		}
	}
}
func TestCatalogRefusalsAndInvalidArguments(t *testing.T) {
	// R-XSWG-AM3G R-XVC9-25KU R-YB6Y-167V R-YCEU-EXYK R-Y7J8-VUZS
	h := setup(t, "print(1)\n")
	a := h.create("job")
	must(t, os.Remove(h.trace))
	before, e := h.st.List(context.Background(), "alice")
	must(t, e)
	for i, tt := range []struct{ tool, args string }{{"list", `{"name":"job"}`}, {"show", `{"script":"job"}`}, {"create", `{"name":"new"}`}, {"create", `{"name":"new","repo":"rep_1111111111111111","ref":1,"owner":"bob"}`}, {"update", `{"name":"job"}`}, {"update", `{"name":"job","ref":2,"repo":"rep","new_name":"daily"}`}, {"delete", `{"id":"scr"}`}, {"subscribe", `{"name":"job"}`}, {"unsubscribe", `{"name":"job"}`}, {"subscribe", `{"name":"job","event":["repo.pushed"],"ref":"main"}`}, {"unsubscribe", `{"name":"job","event":["repo.pushed"],"ref":"main"}`}, {"run", `{"ref":"main"}`}, {"run", `{"name":"job","input":"text"}`}, {"run", `{"name":"job","input":["sales"]}`}, {"runs", `{"script":"job"}`}, {"result", `{"id":"run"}`}, {"result", `{"run":3}`}, {"cancel", `{"name":"job"}`}} {
		u := caller()
		u.RequestID = fmt.Sprintf("invalid-%d", i)
		beforeState := snapshot(t, h)
		if !h.raw(tt.tool, tt.args, u).IsError() {
			t.Fatal("accepted invalid arguments")
		}
		unchanged(t, h, beforeState)
		noGit(t, h)
		h.flush()
		count := 0
		kind := "read"
		if tt.tool == "create" || tt.tool == "update" || tt.tool == "run" || tt.tool == "subscribe" {
			kind = "additive"
		}
		if tt.tool == "cancel" || tt.tool == "delete" || tt.tool == "unsubscribe" {
			kind = "destructive"
		}
		for _, event := range h.capture.Events() {
			if event.RequestID != u.RequestID {
				continue
			}
			count++
			if event.Name != "tool.called" || event.Attrs["tool"] != tt.tool || event.Attrs["kind"] != kind || event.Attrs["outcome"] != "invalid_arguments" || event.Attrs["duration_us"] != int64(0) {
				t.Fatal(event)
			}
		}
		if count != 1 {
			t.Fatal("invalid argument event count", tt.tool, count)
		}
	}
	h.flush()
	for _, e := range h.capture.Events() {
		if strings.HasPrefix(e.RequestID, "invalid-") && (e.Name != "tool.called" || e.Attrs["outcome"] != "invalid_arguments" || e.Attrs["duration_us"] != int64(0)) {
			t.Fatal(e)
		}
	}
	after, e := h.st.List(context.Background(), "alice")
	must(t, e)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid args changed catalog")
	}
	if _, e = os.Stat(h.trace); !os.IsNotExist(e) {
		t.Fatal("invalid args ran git")
	}
	beforeFailure := snapshot(t, h)
	removeTrace(t, h)
	h.db.SetFailing(true)
	for _, tt := range []struct{ tool, args string }{{"list", `{}`}, {"show", `{"name":"job"}`}, {"create", `{"name":"new","repo":"rep_1111111111111111"}`}, {"update", `{"name":"job","ref":"main"}`}, {"delete", `{"name":"job"}`}, {"subscribe", `{"name":"job","event":"Repo.Pushed"}`}, {"unsubscribe", `{"name":"job","event":"Repo.Pushed"}`}, {"run", `{"name":"job"}`}, {"runs", `{"name":"job"}`}, {"result", `{"run":"run_1111111111111111"}`}, {"cancel", `{"run":"run_1111111111111111"}`}} {
		refusal(t, h.raw(tt.tool, tt.args, caller()), store.Unreachable)
	}
	refusal(t, h.call("create", tools.CreateArgs{Name: "Invalid", Repo: "rep"}), fmt.Sprintf(tools.InvalidName, "Invalid"))
	bad := "..bad"
	refusal(t, h.call("create", tools.CreateArgs{Name: "new", Repo: "rep", Ref: &bad}), fmt.Sprintf(tools.InvalidRef, "..bad"))
	refusal(t, h.call("update", tools.UpdateArgs{Name: a.Name, Ref: bad}), fmt.Sprintf(tools.InvalidRef, "..bad"))
	for _, tool := range []string{"show", "update", "delete", "subscribe", "unsubscribe", "run", "runs"} {
		args := `{"name":"Invalid"}`
		if tool == "subscribe" || tool == "unsubscribe" {
			args = `{"name":"Invalid","event":"Repo.Pushed"}`
		}
		if tool == "update" {
			args = `{"name":"Invalid","ref":"..bad"}`
		}
		refusal(t, h.raw(tool, args, caller()), fmt.Sprintf(tools.MissingScript, "Invalid"))
	}
	for _, tool := range []string{"result", "cancel"} {
		refusal(t, h.raw(tool, `{"run":"job"}`, caller()), fmt.Sprintf(tools.MissingRun, "job"))
	}
	noGit(t, h)
	h.db.SetFailing(false)
	unchanged(t, h, beforeFailure)
}
func TestUnknownAndMissingArguments(t *testing.T) {
	// R-Y9Z1-NEH6 R-Y8R5-9MQH
	h := setup(t, "print(1)\n")
	_, e := h.client.CallTool(context.Background(), caller(), "rename", json.RawMessage(`{}`))
	var rpc *mcp.RPCError
	if !errors.As(e, &rpc) || rpc.Code != -32602 {
		t.Fatal(e)
	}
	h.flush()
	if len(h.capture.Events()) != 0 {
		t.Fatal(h.capture.Events())
	}
	for _, tool := range []string{"list", "show"} {
		var results []mcp.Result
		for _, args := range []string{"", `,"arguments":{}`} {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `"` + args + `}}`
			r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User-Id", "alice")
			w := httptest.NewRecorder()
			identity.Require(h.server).ServeHTTP(w, r)
			var envelope struct {
				Result mcp.Result `json:"result"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			results = append(results, envelope.Result)
		}
		if resultBytes(t, results[0]) != resultBytes(t, results[1]) {
			t.Fatal("missing arguments differs")
		}
		if tool == "list" {
			if string(object(t, results[0])) != `{"scripts":[]}` {
				t.Fatal("missing list")
			}
		} else {
			if !results[0].IsError() {
				t.Fatal("accepted missing argument")
			}
		}
	}
}

func TestCatalogFailsDuringGit(t *testing.T) {
	// R-YB6Y-167V
	for _, tool := range []string{"create", "run"} {
		t.Run(tool, func(t *testing.T) {
			h := setup(t, "print(1)\n")
			if tool == "run" {
				h.create("job")
			}
			h.after = func(time.Duration) <-chan time.Time { h.db.SetFailing(true); return make(chan time.Time) }
			args := `{"name":"job"}`
			if tool == "create" {
				args = `{"name":"job","repo":"rep_1111111111111111"}`
			}
			refusal(t, h.raw(tool, args, caller()), store.Unreachable)
		})
	}
}

func TestCallsCutOffWithoutReturning(t *testing.T) {
	// R-5128-4KME
	for _, tool := range []string{"create", "run"} {
		for _, mode := range []string{"halt", "cancel", "archive-cancel", "archive-halt", "archive-held-cancel"} {
			if tool == "create" && strings.HasPrefix(mode, "archive-") {
				continue
			}
			t.Run(tool+"-"+mode, func(t *testing.T) {
				h := setup(t, "print(1)\n")
				var sc tools.Script
				if tool == "run" {
					sc = h.create("job")
				}
				before := snapshot(t, h)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if mode == "halt" {
					h.lim.Halt()
				} else {
					n := 0
					h.after = func(time.Duration) <-chan time.Time {
						n++
						at := 1
						if strings.HasPrefix(mode, "archive-") {
							at = 2
						}
						if n == at {
							if at == 2 {
								entries, err := os.ReadDir(filepath.Join(h.root, "runs", sc.ID))
								must(t, err)
								if len(entries) != 1 {
									t.Fatal("archive admission lacks run folder", entries)
								}
							}
							switch mode {
							case "archive-held-cancel":
								fifo := filepath.Join(h.root, "held-archive-trace")
								must(t, syscall.Mkfifo(fifo, 0600))
								h.env = append(h.env, "GIT_TRACE="+fifo)
							case "archive-halt":
								h.lim.Halt()
							default:
								cancel()
							}
						}
						return make(chan time.Time)
					}
				}
				args := `{"name":"job"}`
				if tool == "create" {
					args = `{"name":"job","repo":"rep_1111111111111111"}`
				}
				body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
				r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body)).WithContext(ctx)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-User-Id", "alice")
				r.Header.Set("X-Request-Id", "cutoff")
				w := httptest.NewRecorder()
				done := make(chan bool, 1)
				go func() {
					returned := false
					defer func() { done <- returned }()
					identity.Require(h.server).ServeHTTP(w, r)
					returned = true
				}()
				deadline, release := context.WithTimeout(context.Background(), 10*time.Second)
				defer release()
				if mode == "archive-held-cancel" {
					started := false
					for !started {
						b, err := os.ReadFile(h.trace)
						if err != nil && !os.IsNotExist(err) {
							t.Fatal(err)
						}
						for _, line := range strings.Split(string(b), "\n") {
							var e struct {
								Event string
								Argv  []string
							}
							if json.Unmarshal([]byte(line), &e) == nil && e.Event == "start" {
								for _, a := range e.Argv {
									if a == "archive" {
										started = true
									}
								}
							}
						}
						select {
						case <-deadline.Done():
							t.Fatal("held archive git did not start")
						case <-done:
							t.Fatal("held archive request ended before cancellation")
						default:
							runtime.Gosched()
						}
					}
					cancel()
				}
				select {
				case returned := <-done:
					if returned {
						t.Fatal("cutoff returned")
					}
				case <-deadline.Done():
					t.Fatal("cutoff goroutine did not end")
				}
				if w.Body.Len() != 0 || len(w.Header()) != 0 {
					t.Fatal(w.Header(), w.Body)
				}
				h.flush()
				for _, e := range h.capture.Events() {
					if e.RequestID == "cutoff" {
						t.Fatal(e)
					}
				}
				after := snapshot(t, h)
				if !reflect.DeepEqual(before.Scripts, after.Scripts) || !reflect.DeepEqual(before.Runs, after.Runs) {
					t.Fatal("cutoff changed catalog", before, after)
				}
				for path, entry := range after.Files {
					if path != "." && path != sc.ID {
						t.Fatal("cutoff left run folder or data", path, entry)
					}
				}
			})
		}
	}
}
func toolCalledCount(h *fixture) int {
	h.flush()
	n := 0
	for _, event := range h.capture.Events() {
		if event.Name == "tool.called" {
			n++
		}
	}
	return n
}
func assertListing(t *testing.T, h *fixture, want []mcp.ToolInfo) []mcp.ToolInfo {
	t.Helper()
	before := toolCalledCount(h)
	removeTrace(t, h)
	got, err := h.client.ListTools(context.Background(), caller())
	must(t, err)
	if len(got) != 11 || (want != nil && !reflect.DeepEqual(want, got)) {
		t.Fatal("tools/list changed metadata", got)
	}
	if toolCalledCount(h) != before {
		t.Fatal("tools/list emitted tool.called")
	}
	noGit(t, h)
	return got
}
func TestListingAfterCallsDrainAndFailure(t *testing.T) {
	// R-8EGL-JW22 R-192O-JM8W
	h := setup(t, "print(1)\n")
	before := assertListing(t, h, nil)
	if toolCalledCount(h) != 0 {
		t.Fatal("initial listing emitted tool.called")
	}
	calls := []struct {
		tool, args string
		isError    bool
	}{
		{"list", `{}`, false},
		{"create", `{"name":"job","repo":"rep_1111111111111111"}`, false},
		{"show", `{"name":"job"}`, false},
		{"subscribe", `{"name":"job","event":"repo.pushed"}`, false},
		{"subscribe", `{"name":"job","event":"repo.pushed"}`, false},
		{"unsubscribe", `{"name":"job","event":"repo.pushed"}`, false},
		{"update", `{"name":"job","ref":"missing"}`, false},
		{"run", `{"name":"job"}`, false},
		{"runs", `{"name":"job"}`, false},
	}
	for _, call := range calls {
		assertListing(t, h, before)
		r := h.raw(call.tool, call.args, caller())
		if r.IsError() != call.isError {
			t.Fatal(call.tool, resultBytes(t, r))
		}
		assertListing(t, h, before)
	}
	job, err := h.st.Find(context.Background(), "alice", "job")
	must(t, err)
	records, err := h.st.Runs(context.Background(), job.ID)
	must(t, err)
	if len(records) != 1 {
		t.Fatal(records)
	}
	runArgs := fmt.Sprintf(`{"run":%q}`, records[0].ID)
	for _, call := range []struct {
		tool, args string
		isError    bool
	}{
		{"result", runArgs, false}, {"cancel", runArgs, true}, {"delete", `{"name":"job"}`, false},
		{"show", `{"name":"absent"}`, true}, {"create", `{"name":"Invalid","repo":"rep_1111111111111111"}`, true},
		{"update", `{"name":"absent","ref":"main"}`, true}, {"delete", `{"name":"absent"}`, true},
		{"run", `{"name":"absent"}`, true}, {"runs", `{"name":"absent"}`, true}, {"result", `{"run":"absent"}`, true},
	} {
		assertListing(t, h, before)
		r := h.raw(call.tool, call.args, caller())
		if r.IsError() != call.isError {
			t.Fatal(call.tool, resultBytes(t, r))
		}
		assertListing(t, h, before)
	}
	names := []string{"list", "show", "create", "update", "delete", "subscribe", "unsubscribe", "run", "runs", "result", "cancel"}
	for _, name := range names {
		assertListing(t, h, before)
		if r := h.raw(name, `{"extra":1}`, caller()); !r.IsError() {
			t.Fatal("accepted invalid arguments", name)
		}
		assertListing(t, h, before)
	}
	h.core.Drain(context.Background())
	assertListing(t, h, before)
	// Drain's refusal is another answered call, independent of store failure.
	if r := h.raw("run", `{"name":"absent"}`, caller()); !r.IsError() {
		t.Fatal("run after drain succeeded")
	}
	assertListing(t, h, before)
	h.db.SetFailing(true)
	assertListing(t, h, before)
	for _, name := range names {
		args := `{"name":"absent"}`
		switch name {
		case "list":
			args = `{}`
		case "create":
			args = `{"name":"other","repo":"rep_1111111111111111"}`
		case "subscribe", "unsubscribe":
			args = `{"name":"job","event":"repo.pushed"}`
		case "update":
			args = `{"name":"absent","ref":"main"}`
		case "result", "cancel":
			args = runArgs
		}
		assertListing(t, h, before)
		if r := h.raw(name, args, caller()); !r.IsError() {
			t.Fatal("call while failing succeeded", name)
		}
		assertListing(t, h, before)
	}
}
func TestListingWhileFailingBeforeDrain(t *testing.T) {
	// R-8EGL-JW22 R-192O-JM8W
	h := setup(t, "print(1)\n")
	before := assertListing(t, h, nil)
	h.db.SetFailing(true)
	assertListing(t, h, before)
	h.core.Drain(context.Background())
	assertListing(t, h, before)
}
func TestListingAfterLiveRunAndSuccessfulCancel(t *testing.T) {
	// R-8EGL-JW22 R-192O-JM8W
	h, _, record, _ := paused(t)
	before := assertListing(t, h, nil)
	// paused has answered a successful run and waits for its process handshake.
	r := h.call("cancel", tools.CancelArgs{Run: record.ID})
	if r.IsError() {
		t.Fatal(resultBytes(t, r))
	}
	assertListing(t, h, before)
	h.core.Drain(context.Background())
	assertListing(t, h, before)
	h.db.SetFailing(true)
	assertListing(t, h, before)
}

func TestToolCalledPerOutcomeAndKind(t *testing.T) {
	// R-8FOH-XNSR
	h := setup(t, "print(1)\n")
	job := h.create("job")
	n := 0
	for _, tt := range []struct{ tool, args, kind string }{{"list", `{}`, "read"}, {"show", `{"name":"job"}`, "read"}, {"create", `{"name":"other","repo":"rep_1111111111111111"}`, "additive"}, {"update", `{"name":"job","ref":"missing"}`, "additive"}, {"run", `{"name":"job"}`, "additive"}, {"runs", `{"name":"job"}`, "read"}, {"result", "", "read"}, {"cancel", "", "destructive"}, {"delete", `{"name":"job"}`, "destructive"}} {
		args := tt.args
		if tt.tool == "result" || tt.tool == "cancel" {
			ss, e := h.st.Runs(context.Background(), job.ID)
			must(t, e)
			if len(ss) != 1 {
				t.Fatal(ss)
			}
			args = fmt.Sprintf(`{"run":%q}`, ss[0].ID)
		}
		u := caller()
		u.RequestID = fmt.Sprintf("event-%d", n)
		n++
		r := h.raw(tt.tool, args, u)
		outcome := "ok"
		if r.IsError() {
			outcome = "error"
		}
		h.flush()
		count := 0
		domains := map[string]int{}
		for _, e := range h.capture.Events() {
			if e.RequestID != u.RequestID {
				continue
			}
			if e.Name == "tool.called" {
				count++
				if e.Attrs["tool"] != tt.tool || e.Attrs["kind"] != tt.kind || e.Attrs["outcome"] != outcome {
					t.Fatal(e)
				}
			} else {
				domains[e.Name]++
			}
		}
		if count != 1 {
			t.Fatal(tt.tool, count)
		}
		want := map[string]int{}
		if !r.IsError() {
			switch tt.tool {
			case "create":
				want["script.created"] = 1
			case "update":
				want["script.updated"] = 1
			case "delete":
				want["script.deleted"] = 1
			case "run":
				want["run.finished"] = 1
			}
		}
		if !reflect.DeepEqual(domains, want) {
			t.Fatalf("%s domain events: got %v want %v", tt.tool, domains, want)
		}
	}
}

func TestFinalOutputShapes(t *testing.T) {
	// R-IZHY-8LMQ R-7JZ3-ZMIP R-T179-4S0M R-7MEW-R603 R-3PI5-7IPB R-3QQ1-LAG0 R-YM61-H3W4
	h := setup(t, "print(1)\n")
	sc := h.create("job")
	for i, status := range []string{store.StatusExited, store.StatusKilled, store.StatusTimedOut} {
		id := fmt.Sprintf("run_%016x", 100+i)
		started := time.Date(2025, 1, 3+i, 2, 0, 0, 0, time.UTC)
		r, e := h.st.AddRun(context.Background(), store.Run{ID: id, Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: "alice", RequestID: "fixture-record", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: started})
		must(t, e)
		code := 0
		if status == store.StatusExited {
			code = 7
		}
		r, e = h.st.FinishRun(context.Background(), id, store.Ending{Status: status, ExitCode: code, Finished: started.Add(time.Second), StdoutBytes: 3, StderrBytes: 2, Truncated: true})
		must(t, e)
		folder := h.core.Folder(r)
		must(t, os.MkdirAll(filepath.Join(folder, "out"), 0700))
		v := decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: id}))
		if v.SHA == nil || *v.SHA != r.SHA || v.Finished == nil || *v.Finished != started.Add(time.Second).Format(time.RFC3339) || v.Started != started.Format(time.RFC3339) || v.Reason != nil || !v.Truncated || v.StdoutBytes != 3 || v.StderrBytes != 2 || v.Stdout == nil || *v.Stdout != "" || v.Stderr == nil || *v.Stderr != "" || v.Files == nil || len(*v.Files) != 0 {
			t.Fatal(v)
		}
		if status == store.StatusExited {
			if v.ExitCode == nil || *v.ExitCode != 7 {
				t.Fatal(v)
			}
		} else if v.ExitCode != nil {
			t.Fatal(v)
		}
		shown := decode[tools.Script](t, h.call("show", tools.ShowArgs{Name: "job"}))
		if shown.LastRun == nil || shown.LastRun.ID != id || shown.LastRun.Started != v.Started || !reflect.DeepEqual(shown.LastRun.ExitCode, v.ExitCode) {
			t.Fatal(shown)
		}
		refusal(t, h.call("cancel", tools.CancelArgs{Run: id}), fmt.Sprintf(tools.Ended, id))
		root, e := os.OpenRoot(folder)
		must(t, e)
		must(t, root.WriteFile("stdout", []byte("unreadable"), 0000))
		v = decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: id}))
		if *v.Stdout != "" {
			t.Fatal("read unreadable stream")
		}
		must(t, root.RemoveAll("out"))
		must(t, root.Symlink("stdout", "out"))
		v = decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: id}))
		if len(*v.Files) != 0 {
			t.Fatal("listed symlink out")
		}
		must(t, root.Close())
	}
}

// wire independently assembles ordered expected members from fixture records.
type member struct {
	name  string
	value any
}

func wire(t *testing.T, members ...member) string {
	t.Helper()
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range members {
		if i != 0 {
			b.WriteByte(',')
		}
		name, e := json.Marshal(m.name)
		must(t, e)
		value, e := json.Marshal(m.value)
		must(t, e)
		b.Write(name)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.String()
}
func entryMembers(r store.Run) []member {
	v := []member{{"id", r.ID}}
	if r.SHA != "" {
		v = append(v, member{"sha", r.SHA})
	}
	v = append(v, member{"ref", r.Ref}, member{"trigger", r.Trigger})
	if r.Trigger == store.TriggerEvent {
		v = append(v, member{"event", r.Event})
	}
	v = append(v, member{"status", r.Status})
	if r.Status == store.StatusExited {
		v = append(v, member{"exit_code", r.ExitCode})
	}
	v = append(v, member{"started", r.Started.UTC().Format("2006-01-02T15:04:05Z")})
	if r.Status != store.StatusQueued && r.Status != store.StatusRunning {
		v = append(v, member{"finished", r.Finished.UTC().Format("2006-01-02T15:04:05Z")})
	}
	v = append(v, member{"truncated", r.Truncated})
	if r.Status == store.StatusFailed {
		v = append(v, member{"reason", r.Reason})
	}
	return v
}
func expectedEntry(t *testing.T, r store.Run) string { return wire(t, entryMembers(r)...) }
func expectedScript(t *testing.T, s store.Script, created bool) string {
	t.Helper()
	v := []member{{"id", s.ID}, {"name", s.Name}, {"repo", s.Repo}, {"ref", s.Ref}}
	if created {
		v = append(v, member{"created", s.Created.UTC().Format("2006-01-02T15:04:05Z")})
	}
	if created {
		subs := make([]tools.Subscription, 0, len(s.Subscriptions))
		for _, sub := range s.Subscriptions {
			subs = append(subs, tools.Subscription{Event: sub.Event, Created: sub.Created.UTC().Format("2006-01-02T15:04:05Z")})
		}
		v = append(v, member{"subscriptions", subs})
	} else {
		v = append(v, member{"subscriptions", len(s.Subscriptions)})
	}
	if s.Last != nil {
		r := *s.Last
		m := []member{{"id", r.ID}, {"status", r.Status}}
		if r.Status == store.StatusExited {
			m = append(m, member{"exit_code", r.ExitCode})
		}
		m = append(m, member{"started", r.Started.UTC().Format("2006-01-02T15:04:05Z")})
		v = append(v, member{"last_run", json.RawMessage(wire(t, m...))})
	}
	return wire(t, v...)
}
func expectedResult(t *testing.T, r store.Run, out, errout int64, stdout, stderr string, ff []tools.File, gone bool) string {
	t.Helper()
	v := []member{{"id", r.ID}, {"script", r.Script}}
	if r.SHA != "" {
		v = append(v, member{"sha", r.SHA})
	}
	v = append(v, member{"ref", r.Ref}, member{"user", r.User}, member{"request_id", r.RequestID}, member{"trigger", r.Trigger})
	if r.Trigger == store.TriggerEvent {
		v = append(v, member{"event", r.Event})
	}
	v = append(v, member{"status", r.Status})
	if r.Status == store.StatusExited {
		v = append(v, member{"exit_code", r.ExitCode})
	}
	v = append(v, member{"started", r.Started.UTC().Format("2006-01-02T15:04:05Z")})
	if r.Status != store.StatusQueued && r.Status != store.StatusRunning {
		v = append(v, member{"finished", r.Finished.UTC().Format("2006-01-02T15:04:05Z")})
	}
	v = append(v, member{"stdout_bytes", out}, member{"stderr_bytes", errout}, member{"truncated", r.Truncated})
	if r.Status == store.StatusFailed {
		v = append(v, member{"reason", r.Reason})
	}
	if gone {
		v = append(v, member{"files_gone", true})
	} else {
		v = append(v, member{"stdout", stdout}, member{"stderr", stderr}, member{"files", ff})
	}
	return wire(t, v...)
}
func assertWire(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	got := string(object(t, r))
	if got != want {
		t.Fatalf("wire result\nwant %s\n got %s", want, got)
	}
}

type fileState struct {
	Mode fs.FileMode
	Size int64
	Data []byte
	Link string
}
type state struct {
	Scripts []store.Script
	Runs    []store.Run
	Files   map[string]fileState
}

func snapshot(t *testing.T, h *fixture) state {
	t.Helper()
	s := state{Scripts: []store.Script{}, Runs: []store.Run{}, Files: map[string]fileState{}}
	for _, owner := range []string{"alice", "bob"} {
		ss, e := h.st.List(context.Background(), owner)
		must(t, e)
		s.Scripts = append(s.Scripts, ss...)
		for _, sc := range ss {
			rr, e := h.st.Runs(context.Background(), sc.ID)
			must(t, e)
			s.Runs = append(s.Runs, rr...)
		}
	}
	root, e := os.OpenRoot(filepath.Join(h.root, "runs"))
	if os.IsNotExist(e) {
		return s
	}
	must(t, e)
	defer func() { _ = root.Close() }()
	must(t, fs.WalkDir(root.FS(), ".", func(p string, _ fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := root.Lstat(p)
		if e != nil {
			return e
		}
		v := fileState{Mode: info.Mode(), Size: info.Size()}
		if info.Mode().IsRegular() {
			v.Data, e = root.ReadFile(p)
		} else if info.Mode()&os.ModeSymlink != 0 {
			v.Link, e = root.Readlink(p)
		}
		if e != nil {
			return e
		}
		s.Files[p] = v
		return nil
	}))
	return s
}
func unchanged(t *testing.T, h *fixture, before state) {
	t.Helper()
	after := snapshot(t, h)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed\nbefore %#v\nafter %#v", before, after)
	}
}
func removeTrace(t *testing.T, h *fixture) {
	t.Helper()
	e := os.Remove(h.trace)
	if e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func noGit(t *testing.T, h *fixture) {
	t.Helper()
	_, e := os.Stat(h.trace)
	if !os.IsNotExist(e) {
		t.Fatal("unexpected git trace", e)
	}
}
func noDomain(t *testing.T, h *fixture, id string) {
	t.Helper()
	h.flush()
	for _, e := range h.capture.Events() {
		if e.RequestID == id && (strings.HasPrefix(e.Name, "script.") || strings.HasPrefix(e.Name, "run.")) {
			t.Fatal("refusal emitted domain event", e)
		}
	}
}

func TestCompleteWireRecords(t *testing.T) {
	// R-7MEW-R603 R-5O8B-E7PL R-7JZ3-ZMIP R-52A4-ICD3 R-53I0-W43S R-T179-4S0M R-5N0F-0FYW
	h := setup(t, "print(1)\n")
	sc := h.create("wire-job")
	ctx := context.Background()
	var records []store.Run
	for i, status := range []string{store.StatusFailed, store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusRunning} {
		tm := time.Date(2025, 2, i+1, 4, 5, 6, 0, time.FixedZone("west", -3600))
		r := store.Run{ID: fmt.Sprintf("run_%016x", 200+i), Script: sc.ID, SHA: strings.Repeat("c", 40), Ref: "run-only-ref", User: "alice", RequestID: "record-request", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: tm}
		if status == store.StatusFailed {
			r.Status = status
			r.SHA = ""
			r.Finished = tm.Add(2 * time.Second)
			r.Reason = store.ReasonCommitMissing
			r.StdoutBytes = 3
			r.StderrBytes = 2
			r.Truncated = true
		}
		var e error
		r, e = h.st.AddRun(ctx, r)
		must(t, e)
		if status != store.StatusFailed && status != store.StatusRunning {
			code := 0
			if status == store.StatusExited {
				code = 9
			}
			r, e = h.st.FinishRun(ctx, r.ID, store.Ending{Status: status, ExitCode: code, Finished: tm.Add(2 * time.Second), StdoutBytes: 3, StderrBytes: 2, Truncated: true})
			must(t, e)
		}
		rootDir := h.core.Folder(r)
		must(t, os.MkdirAll(filepath.Join(rootDir, "out"), 0700))
		root, e := os.OpenRoot(rootDir)
		must(t, e)
		must(t, root.WriteFile("stdout", []byte("out"), 0600))
		must(t, root.WriteFile("stderr", []byte("er"), 0600))
		must(t, root.WriteFile("out/result", []byte("artifact"), 0600))
		must(t, root.Close())
		assertWire(t, h.call("result", tools.ResultArgs{Run: r.ID}), expectedResult(t, r, 3, 2, "out", "er", []tools.File{{Path: "result", Size: 8}}, false))
		current, e := h.st.Find(ctx, "alice", sc.Name)
		must(t, e)
		assertWire(t, h.call("show", tools.ShowArgs{Name: sc.Name}), expectedScript(t, current, true))
		assertWire(t, h.call("list", tools.ListArgs{}), `{"scripts":[`+expectedScript(t, current, false)+`]}`)
		records = append([]store.Run{r}, records...)
		wantEntries := make([]string, len(records))
		for j, v := range records {
			wantEntries[j] = expectedEntry(t, v)
		}
		assertWire(t, h.call("runs", tools.RunsArgs{Name: sc.Name}), `{"runs":[`+strings.Join(wantEntries, ",")+`]}`)
		must(t, os.RemoveAll(rootDir))
		o, eout := int64(3), int64(2)
		if status == store.StatusRunning {
			o, eout = 0, 0
		}
		assertWire(t, h.call("result", tools.ResultArgs{Run: r.ID}), expectedResult(t, r, o, eout, "", "", nil, true))
	}
}

func TestRepositoryOwnerRefusals(t *testing.T) {
	// R-YG2J-K96N R-4XEI-Z9EB
	for _, condition := range []string{"foreign", "unset", "missing", "git-missing", "deadline"} {
		t.Run(condition, func(t *testing.T) {
			h := setup(t, "print(1)\n")
			switch condition {
			case "foreign":
				h.command(h.repo, "config", "ikigenba.owner", "bob")
			case "unset":
				h.command(h.repo, "config", "--unset", "ikigenba.owner")
			case "missing":
				must(t, os.RemoveAll(h.repo))
			case "git-missing":
				must(t, os.Remove(h.gitExec))
			case "deadline":
				h.after = func(time.Duration) <-chan time.Time { c := make(chan time.Time, 1); c <- time.Unix(1, 0); return c }
			}
			before := snapshot(t, h)
			u := caller()
			u.RequestID = "owner-" + condition
			refusal(t, h.raw("create", `{"name":"refused","repo":"rep_1111111111111111"}`, u), fmt.Sprintf(tools.NoRepository, "rep_1111111111111111"))
			unchanged(t, h, before)
			noDomain(t, h, u.RequestID)
		})
	}
}
func paused(t *testing.T) (*fixture, tools.Script, store.Run, net.Conn) {
	return pausedCause(t, events.Cause{})
}
func pausedCause(t *testing.T, cause events.Cause) (*fixture, tools.Script, store.Run, net.Conn) {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	t.Cleanup(func() { stop(); cancel(); _ = listener.Close() })
	script := fmt.Sprintf("import socket\ns=socket.create_connection(('127.0.0.1',%d))\ns.sendall(b'R')\nwhile True:\n c=s.recv(1)\n if c==b'p':\n  s.sendall(b'P')\n else:\n  break\nraise SystemExit(7)\n", listener.Addr().(*net.TCPAddr).Port)
	h := setup(t, script)
	sc := h.create("live-job")
	var r store.Run
	if cause.ID == "" {
		started := decode[tools.Started](t, h.raw("run", `{"name":"live-job"}`, identity.Caller{UserID: "alice", RequestID: "live-start"}))
		r, e = h.st.RunByID(context.Background(), started.ID)
	} else {
		record, err := h.st.Find(context.Background(), "alice", sc.Name)
		must(t, err)
		r, e = h.core.Run(context.Background(), record, runs.Request{Caller: identity.Caller{UserID: "alice", RequestID: "event-start"}, Cause: cause})
	}
	must(t, e)
	conn, e := listener.Accept()
	must(t, e)
	stopConn := context.AfterFunc(ctx, func() { _ = conn.Close() })
	t.Cleanup(func() { stopConn(); _ = conn.Close() })
	buf := make([]byte, 1)
	_, e = io.ReadFull(conn, buf)
	must(t, e)
	if string(buf) != "R" {
		t.Fatal("script handshake")
	}
	return h, sc, r, conn
}
func stillLive(t *testing.T, c net.Conn) {
	t.Helper()
	_, e := c.Write([]byte("p"))
	must(t, e)
	b := make([]byte, 1)
	_, e = io.ReadFull(c, b)
	must(t, e)
	if string(b) != "P" {
		t.Fatal("live process did not respond")
	}
}
func TestReadAndRefusedCallsPreserveLiveState(t *testing.T) {
	// R-54PX-9VUH R-4XEI-Z9EB R-YDMQ-SPP9 R-XSWG-AM3G R-YM61-H3W4 R-0VVB-3QKN R-5N0F-0FYW R-5O8B-E7PL
	h, sc, r, conn := paused(t)
	for i, tt := range []struct{ tool, args string }{{"list", `{}`}, {"show", `{"name":"live-job"}`}, {"runs", `{"name":"live-job"}`}, {"result", fmt.Sprintf(`{"run":%q}`, r.ID)}} {
		before := snapshot(t, h)
		removeTrace(t, h)
		u := caller()
		u.RequestID = fmt.Sprintf("readonly-%d", i)
		_ = object(t, h.raw(tt.tool, tt.args, u))
		unchanged(t, h, before)
		noGit(t, h)
		stillLive(t, conn)
	}
	for i, tt := range []struct{ tool, args, want string }{{"update", `{"name":"live-job","ref":"..bad"}`, fmt.Sprintf(tools.InvalidRef, "..bad")}, {"delete", `{"name":"absent"}`, fmt.Sprintf(tools.MissingScript, "absent")}, {"run", `{"name":"absent"}`, fmt.Sprintf(tools.MissingScript, "absent")}, {"create", `{"name":"Invalid","repo":"rep_1111111111111111"}`, fmt.Sprintf(tools.InvalidName, "Invalid")}, {"create", `{"name":"live-job","repo":"rep_1111111111111111"}`, fmt.Sprintf(tools.NameTaken, "live-job")}, {"cancel", `{"run":"unknown"}`, fmt.Sprintf(tools.MissingRun, "unknown")}} {
		before := snapshot(t, h)
		removeTrace(t, h)
		u := caller()
		u.RequestID = fmt.Sprintf("refused-live-%d", i)
		refusal(t, h.raw(tt.tool, tt.args, u), tt.want)
		unchanged(t, h, before)
		noGit(t, h)
		noDomain(t, h, u.RequestID)
		stillLive(t, conn)
	}
	// Refused reads are as inert as successful reads, including foreign runs.
	for i, tt := range []struct{ tool, args, user, want string }{
		{"show", `{"name":"absent"}`, "alice", fmt.Sprintf(tools.MissingScript, "absent")},
		{"runs", `{"name":"absent"}`, "alice", fmt.Sprintf(tools.MissingScript, "absent")},
		{"result", `{"run":"run_9999999999999999"}`, "alice", fmt.Sprintf(tools.MissingRun, "run_9999999999999999")},
		{"show", `{"name":"live-job"}`, "bob", fmt.Sprintf(tools.MissingScript, "live-job")},
		{"runs", `{"name":"live-job"}`, "bob", fmt.Sprintf(tools.MissingScript, "live-job")},
		{"result", fmt.Sprintf(`{"run":%q}`, r.ID), "bob", fmt.Sprintf(tools.MissingRun, r.ID)},
	} {
		before := snapshot(t, h)
		removeTrace(t, h)
		u := identity.Caller{UserID: tt.user, RequestID: fmt.Sprintf("refused-read-%d", i)}
		refusal(t, h.raw(tt.tool, tt.args, u), tt.want)
		unchanged(t, h, before)
		noGit(t, h)
		noDomain(t, h, u.RequestID)
		stillLive(t, conn)
	}
	// A deleted run record must refuse result while another run remains live.
	deleted, err := h.st.AddRun(context.Background(), store.Run{ID: "run_7777777777777777", Script: sc.ID, User: "alice", Ref: "main", Trigger: store.TriggerManual, Status: store.StatusFailed, Reason: store.ReasonCommitMissing, Started: r.Started, Finished: r.Started})
	must(t, err)
	must(t, h.st.DeleteRun(context.Background(), deleted.ID))
	beforeDeleted := snapshot(t, h)
	removeTrace(t, h)
	deletedCaller := identity.Caller{UserID: "alice", RequestID: "deleted-run-read"}
	refusal(t, h.raw("result", fmt.Sprintf(`{"run":%q}`, deleted.ID), deletedCaller), fmt.Sprintf(tools.MissingRun, deleted.ID))
	unchanged(t, h, beforeDeleted)
	noGit(t, h)
	noDomain(t, h, deletedCaller.RequestID)
	stillLive(t, conn)
	beforeOwner := snapshot(t, h)
	removeTrace(t, h)
	ownerCaller := identity.Caller{UserID: "alice", RequestID: "valid-owner-invalid-ref"}
	refusal(t, h.raw("create", `{"name":"new-job","repo":"rep_1111111111111111","ref":"..bad"}`, ownerCaller), fmt.Sprintf(tools.InvalidRef, "..bad"))
	unchanged(t, h, beforeOwner)
	noDomain(t, h, ownerCaller.RequestID)
	stillLive(t, conn)
	traceBytes, err := os.ReadFile(h.trace)
	must(t, err)
	starts := 0
	for _, line := range strings.Split(strings.TrimSpace(string(traceBytes)), "\n") {
		var event struct {
			Event string
			Argv  []string
		}
		must(t, json.Unmarshal([]byte(line), &event))
		if event.Event == "start" {
			starts++
			if !reflect.DeepEqual(event.Argv[1:], []string{"config", "--file", filepath.Join(h.repo, "config"), "--get", "ikigenba.owner"}) {
				t.Fatal("invalid-ref started another process", event.Argv)
			}
		}
	}
	if starts != 1 {
		t.Fatal("invalid-ref owner reads", starts)
	}
	// The foreign caller cannot cancel either a live or an ended run.
	before := snapshot(t, h)
	removeTrace(t, h)
	bob := identity.Caller{UserID: "bob", RequestID: "foreign-live"}
	refusal(t, h.raw("cancel", fmt.Sprintf(`{"run":%q}`, r.ID), bob), fmt.Sprintf(tools.MissingRun, r.ID))
	unchanged(t, h, before)
	noGit(t, h)
	noDomain(t, h, bob.RequestID)
	stillLive(t, conn)
	cancelResult := h.raw("cancel", fmt.Sprintf(`{"run":%q}`, r.ID), identity.Caller{UserID: "alice", RequestID: "own-cancel"})
	ended, e := h.st.RunByID(context.Background(), r.ID)
	must(t, e)
	assertWire(t, cancelResult, expectedEntry(t, ended))
	if ended.Status != store.StatusKilled || ended.Ref != r.Ref || ended.Trigger != r.Trigger {
		t.Fatal(ended)
	}
	before = snapshot(t, h)
	removeTrace(t, h)
	bob.RequestID = "foreign-ended"
	refusal(t, h.raw("cancel", fmt.Sprintf(`{"run":%q}`, r.ID), bob), fmt.Sprintf(tools.MissingRun, r.ID))
	unchanged(t, h, before)
	noGit(t, h)
	noDomain(t, h, bob.RequestID)
	ss, e := h.st.Find(context.Background(), "alice", sc.Name)
	must(t, e)
	if ss.Last == nil || ss.Last.Status != store.StatusKilled {
		t.Fatal(ss)
	}
}

// assertShape compares consumer-visible runtime fields, never source text.
func assertShape(t *testing.T, value, expected any, tags bool) {
	t.Helper()
	got, want := reflect.TypeOf(value), reflect.TypeOf(expected)
	if got.NumField() != want.NumField() {
		t.Errorf("%v: fields = %d, want %d", got, got.NumField(), want.NumField())
		return
	}
	for i := 0; i < want.NumField(); i++ {
		g, w := got.Field(i), want.Field(i)
		if w.Tag.Get("description") != "" {
			if g.Tag.Get("description") == "" {
				t.Errorf("%v field %d: empty description", got, i)
			}
			g.Tag = reflect.StructTag(regexp.MustCompile(`description:"(?:[^"\\]|\\.)*"`).ReplaceAllString(string(g.Tag), `description:"fixture"`))
		}
		if g.Name != w.Name || g.Type != w.Type || g.Anonymous != w.Anonymous || g.PkgPath != w.PkgPath || (tags && g.Tag != w.Tag) {
			t.Errorf("%v field %d: %s %v %q, want %s %v %q", got, i, g.Name, g.Type, g.Tag, w.Name, w.Type, w.Tag)
		}
	}
}
func TestPublicTypeShapes(t *testing.T) {
	// R-2W8K-10WN
	assertShape(t, tools.Config{}, struct {
		Store     *store.Store
		Source    *source.Source
		Runs      *runs.Core
		Telemetry *telemetry.Writer
	}{}, false)
	// R-2YOC-SKE1
	assertShape(t, tools.ListArgs{}, struct {
	}{}, false)
	// R-XFHK-34XT
	assertShape(t, tools.ShowArgs{}, struct {
		Name string `json:"name" mcp:"required" description:"fixture"`
	}{}, true)
	// R-XGPG-GWOI
	assertShape(t, tools.CreateArgs{}, struct {
		Name string  `json:"name" mcp:"required" description:"fixture"`
		Repo string  `json:"repo" mcp:"required" description:"fixture"`
		Ref  *string `json:"ref" description:"fixture"`
	}{}, true)
	// R-XHXC-UOF7
	assertShape(t, tools.UpdateArgs{}, struct {
		Name string `json:"name" mcp:"required" description:"fixture"`
		Ref  string `json:"ref" mcp:"required" description:"fixture"`
	}{}, true)
	// R-XJ59-8G5W
	assertShape(t, tools.DeleteArgs{}, struct {
		Name string `json:"name" mcp:"required" description:"fixture"`
	}{}, true)
	// R-XMSY-DRDZ
	assertShape(t, tools.RunArgs{}, struct {
		Name  string          `json:"name" mcp:"required" description:"fixture"`
		Ref   *string         `json:"ref" description:"fixture"`
		Input json.RawMessage `json:"input" description:"fixture"`
	}{}, true)
	// R-XP8R-5AVD
	assertShape(t, tools.RunsArgs{}, struct {
		Name string `json:"name" mcp:"required" description:"fixture"`
	}{}, true)
	// R-XQGN-J2M2
	assertShape(t, tools.ResultArgs{}, struct {
		Run string `json:"run" mcp:"required" description:"fixture"`
	}{}, true)
	// R-XROJ-WUCR
	assertShape(t, tools.CancelArgs{}, struct {
		Run string `json:"run" mcp:"required" description:"fixture"`
	}{}, true)
	// R-IZHY-8LMQ
	assertShape(t, tools.LastRun{}, struct {
		ID       string `json:"id" mcp:"required"`
		Status   string `json:"status" mcp:"required"`
		ExitCode *int   `json:"exit_code"`
		Started  string `json:"started" mcp:"required"`
	}{}, true)
	// R-7F3I-GJJX
	assertShape(t, tools.Script{}, struct {
		ID            string               `json:"id" mcp:"required"`
		Name          string               `json:"name" mcp:"required"`
		Repo          string               `json:"repo" mcp:"required"`
		Ref           string               `json:"ref" mcp:"required"`
		Created       string               `json:"created" mcp:"required"`
		Subscriptions []tools.Subscription `json:"subscriptions" mcp:"required"`
		LastRun       *tools.LastRun       `json:"last_run"`
	}{}, true)
	// R-7GBE-UBAM
	assertShape(t, tools.ListedScript{}, struct {
		ID            string         `json:"id" mcp:"required"`
		Name          string         `json:"name" mcp:"required"`
		Repo          string         `json:"repo" mcp:"required"`
		Ref           string         `json:"ref" mcp:"required"`
		Subscriptions int            `json:"subscriptions" mcp:"required"`
		LastRun       *tools.LastRun `json:"last_run"`
	}{}, true)
	assertShape(t, tools.ScriptList{}, struct {
		Scripts []tools.ListedScript `json:"scripts" mcp:"required"`
	}{}, true)
	// R-J4DJ-ROLI
	assertShape(t, tools.Deleted{}, struct {
		Deleted bool   `json:"deleted" mcp:"required"`
		ID      string `json:"id" mcp:"required"`
	}{}, true)
	// R-J5LG-5GC7
	assertShape(t, tools.Started{}, struct {
		ID     string  `json:"id" mcp:"required"`
		Status string  `json:"status" mcp:"required"`
		SHA    *string `json:"sha"`
		Reason *string `json:"reason"`
	}{}, true)
	// R-7HJB-831B
	assertShape(t, tools.RunEntry{}, struct {
		ID        string  `json:"id" mcp:"required"`
		SHA       *string `json:"sha"`
		Ref       string  `json:"ref" mcp:"required"`
		Trigger   string  `json:"trigger" mcp:"required"`
		Event     *string `json:"event"`
		Status    string  `json:"status" mcp:"required"`
		ExitCode  *int    `json:"exit_code"`
		Started   string  `json:"started" mcp:"required"`
		Finished  *string `json:"finished"`
		Truncated bool    `json:"truncated" mcp:"required"`
		Reason    *string `json:"reason"`
	}{}, true)
	assertShape(t, tools.RunList{}, struct {
		Runs []tools.RunEntry `json:"runs" mcp:"required"`
	}{}, true)
	// R-7IR7-LUS0
	assertShape(t, tools.File{}, struct {
		Path string `json:"path" mcp:"required"`
		Size int64  `json:"size" mcp:"required"`
	}{}, true)
	assertShape(t, tools.RunResult{}, struct {
		ID          string        `json:"id" mcp:"required"`
		Script      string        `json:"script" mcp:"required"`
		SHA         *string       `json:"sha"`
		Ref         string        `json:"ref" mcp:"required"`
		User        string        `json:"user" mcp:"required"`
		RequestID   string        `json:"request_id" mcp:"required"`
		Trigger     string        `json:"trigger" mcp:"required"`
		Event       *string       `json:"event"`
		Status      string        `json:"status" mcp:"required"`
		ExitCode    *int          `json:"exit_code"`
		Started     string        `json:"started" mcp:"required"`
		Finished    *string       `json:"finished"`
		StdoutBytes int64         `json:"stdout_bytes" mcp:"required"`
		StderrBytes int64         `json:"stderr_bytes" mcp:"required"`
		Truncated   bool          `json:"truncated" mcp:"required"`
		Reason      *string       `json:"reason"`
		Stdout      *string       `json:"stdout"`
		Stderr      *string       `json:"stderr"`
		Files       *[]tools.File `json:"files"`
		FilesGone   *bool         `json:"files_gone"`
	}{}, true)
}

func TestCatalogReadsAfterRepositoryRemoval(t *testing.T) {
	// R-52A4-ICD3 R-53I0-W43S R-YDMQ-SPP9
	h := setup(t, "print(1)\n")
	h.create("zulu")
	h.create("alpha")
	ss, err := h.st.List(context.Background(), "alice")
	must(t, err)
	must(t, os.RemoveAll(h.repo))
	removeTrace(t, h)
	entries := make([]string, len(ss))
	for i, s := range ss {
		entries[i] = expectedScript(t, s, false)
		assertWire(t, h.call("show", tools.ShowArgs{Name: s.Name}), expectedScript(t, s, true))
	}
	assertWire(t, h.call("list", tools.ListArgs{}), `{"scripts":[`+strings.Join(entries, ",")+`]}`)
	noGit(t, h)
}

func TestFileListUnreachableEntries(t *testing.T) {
	// R-3QQ1-LAG0
	h := setup(t, "print(1)\n")
	sc := h.create("file-job")
	r, err := h.st.AddRun(context.Background(), store.Run{ID: "run_8888888888888888", Script: sc.ID, User: "alice", Ref: "main", Trigger: store.TriggerManual, Status: store.StatusFailed, Reason: store.ReasonCommitMissing, Started: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Finished: time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC)})
	must(t, err)
	folder := h.core.Folder(r)
	must(t, os.MkdirAll(folder, 0700))
	root, err := os.OpenRoot(folder)
	must(t, err)
	defer func() { _ = root.Close() }()
	assertFiles := func(want []tools.File) {
		t.Helper()
		v := decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: r.ID}))
		if v.Files == nil || !reflect.DeepEqual(*v.Files, want) {
			t.Fatal("file list", v.Files, want)
		}
	}
	assertFiles([]tools.File{}) // absent out
	must(t, root.WriteFile("out", []byte("regular entry"), 0600))
	assertFiles([]tools.File{}) // out is not a directory
	must(t, root.Remove("out"))
	must(t, root.Mkdir("out", 0700))
	must(t, root.Mkdir("out/blocked", 0700))
	must(t, root.WriteFile("out/blocked/probe", []byte("hidden"), 0600))
	must(t, root.WriteFile("out/visible", []byte("visible"), 0600))
	// The owner cannot read this directory even though another user can.
	must(t, root.Chmod("out/blocked", 0004))
	assertFiles([]tools.File{{Path: "visible", Size: 7}})
	must(t, root.Chmod("out/blocked", 0700))
}

func TestSuccessfulMutationsAndStoppingRunDoNotUseGit(t *testing.T) {
	// R-YDMQ-SPP9
	h, sc, r, _ := paused(t)
	for i, tt := range []struct{ tool, args string }{
		{"update", fmt.Sprintf(`{"name":%q,"ref":"main"}`, sc.Name)},
		{"update", fmt.Sprintf(`{"name":%q,"ref":"future"}`, sc.Name)},
		{"cancel", fmt.Sprintf(`{"run":%q}`, r.ID)},
		{"delete", fmt.Sprintf(`{"name":%q}`, sc.Name)},
	} {
		removeTrace(t, h)
		_ = object(t, h.raw(tt.tool, tt.args, identity.Caller{UserID: "alice", RequestID: fmt.Sprintf("no-git-success-%d", i)}))
		noGit(t, h)
	}
	h.create("stop-job")
	h.core.Drain(context.Background())
	before := snapshot(t, h)
	removeTrace(t, h)
	u := identity.Caller{UserID: "alice", RequestID: "no-git-stopping"}
	refusal(t, h.raw("run", `{"name":"stop-job"}`, u), runs.Stopping)
	unchanged(t, h, before)
	noGit(t, h)
	noDomain(t, h, u.RequestID)
}

// schemaWithoutCopy preserves schema bytes and order while checking description values.
func schemaWithoutCopy(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	return regexp.MustCompile(`"description":("(?:[^"\\]|\\.)*")`).ReplaceAllStringFunc(string(raw), func(member string) string {
		var value string
		must(t, json.Unmarshal([]byte(strings.TrimPrefix(member, `"description":`)), &value))
		if value == "" {
			t.Fatal("empty argument description")
		}
		return `"description":"fixture"`
	})
}

// R-XU4C-ODU5
func TestRefusalCopyFormats(t *testing.T) {
	const formats = tools.MissingScript + tools.MissingRun + tools.InvalidName + tools.InvalidRef + tools.InvalidEvent + tools.NameTaken + tools.NoRepository + tools.NotSubscribed + tools.Ended
	_ = formats
	for _, c := range []struct {
		format string
		count  int
	}{
		{tools.MissingScript, 1}, {tools.MissingRun, 1}, {tools.InvalidName, 1}, {tools.InvalidRef, 1}, {tools.InvalidEvent, 1}, {tools.NameTaken, 1}, {tools.NoRepository, 1}, {tools.NotSubscribed, 2}, {tools.Ended, 1},
	} {
		if strings.Count(c.format, "%s") != c.count || strings.Count(c.format, "%") != c.count {
			t.Fatal("invalid refusal format", c.format)
		}
	}
}
