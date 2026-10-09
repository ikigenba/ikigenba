package cli_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func serveGit(t *testing.T, f *serveFixture, dir string, input io.Reader, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.gitPath, args...)
	cmd.Dir = dir
	cmd.Env = append([]string(nil), f.gitEnv...)
	cmd.Stdin = input
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}
func servePack(t *testing.T, f *serveFixture) (string, []byte) {
	t.Helper()
	work := filepath.Join(f.dir, "client")
	serveGit(t, f, f.dir, nil, "init", "--initial-branch=main", work)
	if err := os.WriteFile(filepath.Join(work, "file"), []byte("small deterministic fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	serveGit(t, f, work, nil, "add", "file")
	serveGit(t, f, work, nil, "commit", "-m", "fixture")
	sha := strings.TrimSpace(string(serveGit(t, f, work, nil, "rev-parse", "HEAD")))
	pack := serveGit(t, f, work, strings.NewReader(sha+"\n"), "pack-objects", "--stdout", "--revs")
	if !bytes.HasPrefix(pack, []byte("PACK")) {
		t.Fatalf("pack header %q", pack[:4])
	}
	return sha, pack
}
func servePushBody(sha string, pack []byte) []byte {
	command := strings.Repeat("0", 40) + " " + sha + " refs/heads/main\x00report-status quiet\n"
	prefix := fmt.Sprintf("%04x%s0000", len(command)+4, command)
	return append([]byte(prefix), pack...)
}

type serveResponse struct {
	status  int
	headers http.Header
	body    []byte
	err     error
}
type heldServePush struct {
	writer *io.PipeWriter
	rest   []byte
	done   chan serveResponse
	trace  string
	offset int
}

func startServePush(t *testing.T, f *serveFixture, id string, body []byte, partial bool) *heldServePush {
	t.Helper()
	return startServePushNamed(t, f, "alpha", id, body, partial)
}

func startServePushNamed(t *testing.T, f *serveFixture, name, id string, body []byte, partial bool) *heldServePush {
	t.Helper()
	reader, writer := io.Pipe()
	p := &heldServePush{writer: writer, done: make(chan serveResponse, 1), trace: filepath.Join(f.dir, "held-push-trace.json")}
	if partial {
		trace, err := os.ReadFile(p.trace)
		if err != nil {
			t.Fatal(err)
		}
		p.offset = len(trace)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
	req, err := http.NewRequestWithContext(t.Context(), "POST", "http://"+f.listener.Addr().String()+"/"+name+".git/git-receive-pack", reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-Id", "owner")
	req.Header.Set("X-Request-Id", id)
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	go func() {
		resp, err := f.client().Do(req)
		if err != nil {
			result := serveResponse{err: err}
			p.done <- result
			return
		}
		result := serveResponse{status: resp.StatusCode, headers: resp.Header.Clone()}
		result.body, result.err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		p.done <- result
	}()
	if partial {
		// Leave the final half of the pack pending. This is a real receive-pack
		// process holding its repository's lock, with an incomplete request body.
		split := len(body) - len(body)/4
		p.rest = append([]byte(nil), body[split:]...)
		written := make(chan error, 1)
		go func() { _, err := writer.Write(body[:split]); written <- err }()
		select {
		case err := <-written:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("partial push body was not consumed")
		}
	} else {
		go func() { _, _ = writer.Write(body); _ = writer.Close() }()
	}
	return p
}
func finishServePush(t *testing.T, p *heldServePush) serveResponse {
	t.Helper()
	// An aborted header-held request can leave net/http waiting for its client
	// body writer; callers have already observed the server outcome at this point.
	if len(p.rest) != 0 {
		_ = p.writer.Close()
	}
	select {
	case r := <-p.done:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("push did not end")
		return serveResponse{}
	}
}
func takeServeTimer(t *testing.T, f *serveFixture, want time.Duration) serveTimer {
	t.Helper()
	select {
	case timer := <-f.timers:
		if timer.duration != want {
			t.Fatalf("timer %s want %s", timer.duration, want)
		}
		return timer
	case <-time.After(5 * time.Second):
		t.Fatal("timer not armed")
		return serveTimer{}
	}
}
func serveHeldPushTrace(t *testing.T, f *serveFixture) {
	t.Helper()
	trace := filepath.Join(f.dir, "held-push-trace.json")
	global := filepath.Join(f.dir, "held-push-global")
	serveGit(t, f, f.dir, nil, "config", "--file", global, "trace2.eventTarget", trace)
	if err := os.WriteFile(trace, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for i, value := range f.gitEnv {
		if strings.HasPrefix(value, "GIT_CONFIG_GLOBAL=") {
			f.gitEnv[i] = "GIT_CONFIG_GLOBAL=" + global
		}
	}
}

func awaitServePushStarted(t *testing.T, p *heldServePush) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		trace, err := os.ReadFile(p.trace)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(trace[p.offset:], []byte{'\n'}) {
			var event struct {
				Event string
				Argv  []string
			}
			if json.Unmarshal(line, &event) == nil && event.Event == "child_start" && len(event.Argv) == 4 && event.Argv[1] == "receive-pack" && event.Argv[2] == "--stateless-rpc" {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("git did not start the held push")
		default:
			runtime.Gosched()
		}
	}
}

// R-KL94-6AAI
func TestServeRetrySleepAndRepositoryRandomBytes(t *testing.T) {
	f := newServeFixture(t)
	var mu sync.Mutex
	calls := map[string]int{}
	pauses := make(chan time.Duration, 64)
	f.p.Sink = serveSink(t, func(ctx context.Context, e telemetry.Event) error {
		mu.Lock()
		calls[e.Name]++
		n := calls[e.Name]
		mu.Unlock()
		if e.Name == "service.started" && n < 3 {
			return fmt.Errorf("retry %d", n)
		}
		return f.capture.Deliver(ctx, e)
	})
	f.p.Sleep = func(ctx context.Context, d time.Duration) {
		if ctx.Err() != nil {
			t.Error("retry pause context already done")
		}
		pauses <- d
	}
	f.start(t)
	f.flush(t)
	for _, want := range []time.Duration{telemetry.RetryBackoff, telemetry.RetryBackoff * 2} {
		select {
		case got := <-pauses:
			if got != want {
				t.Fatalf("pause %s want %s", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("injected sleep not invoked")
		}
	}
	created := f.tool(t, "create", `{"name":"alpha"}`)
	// All MCP requests here have a supplied request id. The store's first
	// random read is eight copies of the injected source's first byte.
	want := store.IDPrefix + hex.EncodeToString(bytes.Repeat([]byte{1}, 8))
	if created["id"] != want {
		t.Fatalf("repository id %v want %s", created["id"], want)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "state", "repos", want+".git")); err != nil {
		t.Fatal(err)
	}
	f.stop(t, "retry test stop", cli.ExitSuccess)
	if f.stderr.text() != "" {
		t.Fatalf("retry should deliver: %q", f.stderr.text())
	}
}

// R-9Y6A-DUU3 R-KL94-6AAI
func TestServeSelectedGitAndComposedEnvironmentReachChildren(t *testing.T) {
	f := newServeFixture(t)
	selectedDir := filepath.Join(f.dir, "first-path")
	if err := os.Mkdir(selectedDir, 0700); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(selectedDir, "git")
	if err := os.Symlink(f.gitPath, selected); err != nil {
		t.Fatal(err)
	}
	f.set("PATH", selectedDir+":"+filepath.Dir(f.gitPath))
	global := filepath.Join(f.dir, "git-config")
	trace := filepath.Join(f.dir, "trace.json")
	for _, kv := range [][2]string{{"trace2.eventTarget", trace}, {"trace2.envVars", "SERVE_TEST_MARKER,HOME,XDG_CONFIG_HOME,GIT_CONFIG_NOSYSTEM,GIT_CONFIG_GLOBAL,GIT_TERMINAL_PROMPT"}} {
		serveGit(t, f, f.dir, nil, "config", "--file", global, kv[0], kv[1])
	}
	for i, v := range f.gitEnv {
		if strings.HasPrefix(v, "GIT_CONFIG_GLOBAL=") {
			f.gitEnv[i] = "GIT_CONFIG_GLOBAL=" + global
		}
	}
	f.gitEnv = append(f.gitEnv, "SERVE_TEST_MARKER="+f.dir)
	f.start(t)
	f.tool(t, "create", `{"name":"alpha"}`)
	path := "/alpha.git/info/refs?service=git-upload-pack"
	if status, _, _ := f.request(t, path, "trace"); status != http.StatusOK {
		t.Fatalf("selected git advertisement status %d", status)
	}
	traceStarts := func() int {
		data, err := os.ReadFile(filepath.Clean(trace))
		if err != nil {
			t.Fatal(err)
		}
		starts := 0
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			var event struct{ Event, SID string }
			if err := json.Unmarshal(line, &event); err != nil {
				t.Fatal(err)
			}
			if event.Event == "start" && !strings.Contains(event.SID, "/") {
				starts++
			}
		}
		return starts
	}
	beforeMissing := traceStarts()
	// argv[0] names git, not its executable path. Removing the selected
	// candidate proves Run keeps using it despite the working PATH fallback.
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	f.request(t, path, "selected-missing")
	if got := traceStarts(); got != beforeMissing {
		t.Fatalf("git started without selected executable: starts %d want %d", got, beforeMissing)
	}
	if err := os.Symlink(f.gitPath, selected); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := f.request(t, path, "selected-restored"); status != http.StatusOK {
		t.Fatalf("restored selected git advertisement status %d", status)
	}
	if traceStarts() <= beforeMissing {
		t.Fatal("restored selected executable did not start git")
	}
	f.stop(t, "trace stop", cli.ExitSuccess)
	fixtureRoot, err := os.OpenRoot(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fixtureRoot.Close() }()
	data, err := fixtureRoot.ReadFile("trace.json")
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{}
	starts := 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event struct {
			Event string   `json:"event"`
			Param string   `json:"param"`
			Value string   `json:"value"`
			Argv  []string `json:"argv"`
			SID   string   `json:"sid"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event.Event == "def_param" {
			params[event.Param] = event.Value
		}
		if event.Event == "start" && len(event.Argv) > 0 && !strings.Contains(event.SID, "/") {
			starts++
		}
	}
	if starts == 0 {
		t.Fatal("no real git invocation observed")
	}
	for key, want := range map[string]string{"SERVE_TEST_MARKER": f.dir, "HOME": f.dir, "XDG_CONFIG_HOME": f.dir, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": global, "GIT_TERMINAL_PROMPT": "0"} {
		if params[key] != want {
			t.Fatalf("git environment %s=%q want %q", key, params[key], want)
		}
	}
}

// R-KL94-6AAI
func TestServeInjectedQueueAndOperationTimersControlOutcomes(t *testing.T) {
	f := newServeFixture(t)
	serveHeldPushTrace(t, f)
	f.set("QUEUE_SECONDS", "19")
	f.set("OPERATION_SECONDS", "23")
	f.start(t)
	takeServeTimer(t, f, 24*time.Hour)
	f.tool(t, "create", `{"name":"alpha"}`)
	sha, pack := servePack(t, f)
	body := servePushBody(sha, pack)
	first := startServePush(t, f, "running", body, true)
	operation := takeServeTimer(t, f, 23*time.Second)
	awaitServePushStarted(t, first)
	second := startServePush(t, f, "queued", body, false)
	queue := takeServeTimer(t, f, 19*time.Second)
	queue.ch <- f.p.Now()
	refused := finishServePush(t, second)
	if refused.err != nil || refused.status != 503 || string(refused.body) != smarthttp.TooBusy+"\n" || refused.headers.Get("Retry-After") != "19" {
		t.Fatalf("queue timer outcome %+v", refused)
	}
	operation.ch <- f.p.Now()
	cut := finishServePush(t, first)
	if cut.err == nil {
		t.Fatalf("operation timer delivered complete answer: %s", cut.body)
	}
	f.flush(t)
	foundQueue, foundOperation := false, false
	for _, e := range f.capture.Events() {
		if e.Name == "operation.rejected" && e.RequestID == "queued" && e.Attrs["limit"] == "queue_seconds" {
			foundQueue = true
		}
		if e.Name == "operation.timed_out" && e.RequestID == "running" && e.Attrs["limit"] == "operation_seconds" {
			foundOperation = true
		}
	}
	if !foundQueue || !foundOperation {
		t.Fatalf("injected timers did not record outcomes: %v", f.capture.Events())
	}
	f.stop(t, "timers stop", cli.ExitSuccess)
}
