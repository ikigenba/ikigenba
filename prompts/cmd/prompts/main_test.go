package main

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
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"appkit/manifest"
)

func TestResolveStorageRootsUsesRootedStateAndCacheDefaults(t *testing.T) {
	// R-LBH5-4LO0
	// R-LCP1-IDEP
	roots, err := resolveStorageRoots(func(key string) string {
		if key == "IKIGENBA_ROOT" {
			return "/opt"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("resolve storage roots: %v", err)
	}

	if want := "/opt/prompts/state"; roots.stateDir != want {
		t.Errorf("state directory = %q, want %q", roots.stateDir, want)
	}
	if want := "/opt/prompts/state/sandboxes"; filepath.Join(roots.stateDir, "sandboxes") != want {
		t.Errorf("sandbox root = %q, want %q", filepath.Join(roots.stateDir, "sandboxes"), want)
	}
	if want := "/opt/prompts/cache"; roots.cacheDir != want {
		t.Errorf("cache directory = %q, want %q", roots.cacheDir, want)
	}
	if want := "/opt/prompts/cache/runs"; roots.runsDir != want {
		t.Errorf("runs directory = %q, want %q", roots.runsDir, want)
	}
	if roots.cacheDir == roots.stateDir {
		t.Errorf("cache directory %q must differ from state directory", roots.cacheDir)
	}
}

func TestResolveStorageRootsHonorsExplicitPathOverrides(t *testing.T) {
	// R-LDWX-W55E
	testRoot := t.TempDir()
	dbPath := filepath.Join(testRoot, "durable", "custom", "prompts.sqlite")
	generationPath := filepath.Join(testRoot, "reclaimable", "elsewhere", "epoch")
	env := map[string]string{
		"IKIGENBA_ROOT":           "/opt",
		"PROMPTS_DB_PATH":         dbPath,
		"PROMPTS_GENERATION_PATH": generationPath,
	}
	roots, err := resolveStorageRoots(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("resolve storage roots: %v", err)
	}

	if want := filepath.Dir(dbPath); roots.stateDir != want {
		t.Errorf("state directory = %q, want override directory %q", roots.stateDir, want)
	}
	if want := filepath.Dir(generationPath); roots.cacheDir != want {
		t.Errorf("cache directory = %q, want override directory %q", roots.cacheDir, want)
	}
	if want := filepath.Join(filepath.Dir(generationPath), "runs"); roots.runsDir != want {
		t.Errorf("runs directory = %q, want %q", roots.runsDir, want)
	}
}

// R-8DF1-W89F
func TestCommittedManifestIsPortable(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}
	if bytes.Contains(committed, []byte("/opt/")) {
		t.Fatalf("committed manifest.env contains on-box /opt/ path:\n%s", committed)
	}
	for _, line := range bytes.Split(committed, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("PROMPTS_DB_PATH=")) || bytes.HasPrefix(line, []byte("PROMPTS_GENERATION_PATH=")) {
			t.Fatalf("committed manifest.env contains runtime path line %q", line)
		}
	}
}

// R-8IAN-FB87
func TestManifestLibraryByteEqualsCommittedFile(t *testing.T) {
	got := manifest.Emit(manifest.Fields{
		App:      "prompts",
		Mount:    "/srv/prompts/",
		Default:  false,
		Port:     3002,
		MCP:      true,
		Feed:     "/feed",
		Consumes: []string{"cron", "crm", "ledger", "dropbox", "scripts", "prompts"},
		Extras: []manifest.KV{
			{Key: "OUTBOX_RETENTION_DAYS", Value: "7"},
			{Key: "OUTBOX_RETENTION_MAX_ROWS", Value: "1000000"},
			{Key: "PROMPTS_CALLS_BODY_RETENTION_DAYS", Value: "30"},
		},
	})
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}

	if got != string(committed) {
		t.Fatalf("manifest.Emit output != committed etc/manifest.env\n--- emit ---\n%s\n--- committed ---\n%s", got, committed)
	}
}

// R-4LKF-FB23
func TestPromptsBootsWithDurableSandboxesAndRecreatedRunsCache(t *testing.T) {
	// R-ZMJ5-6QEW
	// R-ZNR1-KI5L
	root := t.TempDir()
	appRoot := filepath.Join(root, "prompts")
	stateDir := filepath.Join(appRoot, "state")
	cacheDir := filepath.Join(appRoot, "cache")
	libexecDir := filepath.Join(appRoot, "libexec")
	binDir := filepath.Join(appRoot, "bin")
	etcDir := filepath.Join(appRoot, "etc")
	shareDir := filepath.Join(appRoot, "share")
	for _, dir := range []string{stateDir, cacheDir, libexecDir, binDir, etcDir, shareDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	versionBytes, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	version := strings.TrimSpace(string(versionBytes))
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
		t.Fatalf("VERSION = %q, want v-prefixed SemVer", version)
	}

	committedManifest, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}
	etcVersionDir := filepath.Join(etcDir, version)
	shareVersionDir := filepath.Join(shareDir, version)
	for _, dir := range []string{etcVersionDir, shareVersionDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	shippedManifest := filepath.Join(etcVersionDir, "manifest.env")
	if err := os.WriteFile(shippedManifest, committedManifest, 0o644); err != nil {
		t.Fatalf("write shipped manifest.env: %v", err)
	}
	if err := os.Symlink(version, filepath.Join(etcDir, "current")); err != nil {
		t.Fatalf("symlink etc/current: %v", err)
	}
	if err := os.Symlink(version, filepath.Join(shareDir, "current")); err != nil {
		t.Fatalf("symlink share/current: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Join(etcDir, "current")); err != nil || resolved != etcVersionDir {
		t.Fatalf("etc/current resolves to %q err=%v, want %q", resolved, err, etcVersionDir)
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Join(shareDir, "current")); err != nil || resolved != shareVersionDir {
		t.Fatalf("share/current resolves to %q err=%v, want %q", resolved, err, shareVersionDir)
	}
	selectedManifest, err := os.ReadFile(filepath.Join(etcDir, "current", "manifest.env"))
	if err != nil {
		t.Fatalf("read selected manifest.env: %v", err)
	}
	if !bytes.Equal(selectedManifest, committedManifest) {
		t.Fatalf("selected manifest.env differs from committed authored file\n--- selected ---\n%s\n--- committed ---\n%s", selectedManifest, committedManifest)
	}

	binary := filepath.Join(libexecDir, "prompts-"+version)
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build prompts: %v\n%s", err, out)
	}

	run := filepath.Join(binDir, "run")
	if err := os.Symlink("../libexec/prompts-"+version, run); err != nil {
		t.Fatalf("symlink bin/run: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(run); err != nil || resolved != binary {
		t.Fatalf("bin/run resolves to %q err=%v, want %q", resolved, err, binary)
	}

	feedServers := make(map[string]*httptest.Server)
	for _, source := range sources {
		feedServers[source] = newIdleFeedServer(t)
	}
	dropbox := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(dropbox.Close)

	port := freeTCPPort(t)
	dbPath := filepath.Join(stateDir, "prompts.db")
	generationPath := filepath.Join(cacheDir, "prompts.db.generation")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	cmd.Env = testEnv(map[string]string{
		"IKIGENBA_DOMAIN":             "",
		"IKIGENBA_ROOT":               "",
		"PROMPTS_IP":                  "127.0.0.1",
		"PROMPTS_PORT":                fmt.Sprintf("%d", port),
		"PROMPTS_DB_PATH":             dbPath,
		"PROMPTS_GENERATION_PATH":     generationPath,
		"PROMPTS_WWW_PATH":            filepath.Join("..", "..", "share", "www"),
		"PROMPTS_CRON_FEED_URL":       feedServers["cron"].URL + "/feed",
		"PROMPTS_CRM_FEED_URL":        feedServers["crm"].URL + "/feed",
		"PROMPTS_LEDGER_FEED_URL":     feedServers["ledger"].URL + "/feed",
		"PROMPTS_DROPBOX_FEED_URL":    feedServers["dropbox"].URL + "/feed",
		"PROMPTS_SCRIPTS_FEED_URL":    feedServers["scripts"].URL + "/feed",
		"PROMPTS_PROMPTS_FEED_URL":    feedServers["prompts"].URL + "/feed",
		"DROPBOX_BASE_URL":            dropbox.URL,
		"ANTHROPIC_API_KEY":           "",
		"PROMPTS_MANIFEST_ROOT":       root,
		"PROMPTS_OUTBOX_REAPER_EVERY": "0",
	})
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start prompts: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	firstStopped := false
	defer func() {
		if !firstStopped {
			stopProcess(cancel, done)
		}
	}()

	doc := waitForHealth(t, port, done, &stdout, &stderr)
	if got := doc["service"]; got != "prompts" {
		t.Fatalf("health service = %v, want prompts; body=%v", got, doc)
	}
	if got := doc["status"]; got != "ok" {
		t.Fatalf("health status = %v, want ok; body=%v", got, doc)
	}
	if _, ok := doc["details"].(map[string]any); !ok {
		t.Fatalf("health details = %#v, want JSON object", doc["details"])
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("prompts did not create DB under state/: %v", err)
	}
	if _, err := os.Stat(generationPath); err != nil {
		t.Fatalf("prompts did not create generation sidecar under cache/: %v", err)
	}
	if filepath.Dir(generationPath) != cacheDir {
		t.Fatalf("generation sidecar path %s is not under cache dir %s", generationPath, cacheDir)
	}
	runsDir := filepath.Join(cacheDir, "runs")
	if info, err := os.Stat(runsDir); err != nil {
		t.Fatalf("prompts did not create runs under cache/: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("runs path is not a directory")
	}
	sandboxesDir := filepath.Join(stateDir, "sandboxes")
	if info, err := os.Stat(sandboxesDir); err != nil {
		t.Fatalf("prompts did not create sandboxes under state/: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("sandboxes path is not a directory")
	}

	// Stop after the first successful startup, then leave a stale cache entry
	// where the next boot must recreate the disposable runs tree.
	stopProcess(cancel, done)
	firstStopped = true
	stale := filepath.Join(runsDir, "stale-run", "output.jsonl")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatalf("mkdir stale run cache: %v", err)
	}
	if err := os.WriteFile(stale, []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("write stale run cache: %v", err)
	}

	// A second composition-root startup against the same state must keep durable
	// sandbox state separate while clearing the disposable runs cache.
	ctx2, cancel2 := context.WithCancel(context.Background())
	var stdout2, stderr2 bytes.Buffer
	cmd2 := exec.CommandContext(ctx2, run, "serve")
	cmd2.Env = cmd.Env
	cmd2.Stdout = &stdout2
	cmd2.Stderr = &stderr2
	if err := cmd2.Start(); err != nil {
		t.Fatalf("restart prompts: %v", err)
	}
	done2 := make(chan error, 1)
	go func() { done2 <- cmd2.Wait() }()
	defer stopProcess(cancel2, done2)
	waitForHealth(t, port, done2, &stdout2, &stderr2)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale run cache survived restart: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "runs")); !os.IsNotExist(err) {
		t.Fatalf("legacy state/runs exists or could not be checked: %v", err)
	}
}

func runOutputOverMCP(t *testing.T, port int, runID string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "run_output",
			"arguments": map[string]any{"run_id": runID},
		},
	})
	if err != nil {
		t.Fatalf("marshal run_output request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/mcp", port), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new run_output request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner-Id", "client-durable-smoke")
	req.Header.Set("X-Owner-Email", "durable@example.com")
	req.Header.Set("X-Client-Id", "client-durable-smoke")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call run_output after restart: %v", err)
	}
	defer resp.Body.Close()
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		t.Fatalf("decode run_output response: %v", err)
	}
	if rpc.Error != nil {
		t.Fatalf("run_output RPC error: %#v", rpc.Error)
	}
	if len(rpc.Result.Content) != 1 {
		t.Fatalf("run_output content = %#v", rpc.Result.Content)
	}
	return rpc.Result.Content[0].Text
}

func newIdleFeedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testEnv(overrides map[string]string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := overrides[key]; ok {
			continue
		}
		out = append(out, kv)
	}
	for key, value := range overrides {
		out = append(out, key+"="+value)
	}
	return out
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForHealth(t *testing.T, port int, done <-chan error, stdout, stderr *bytes.Buffer) map[string]any {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("prompts exited before health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		default:
		}

		resp, err := client.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if resp.StatusCode == http.StatusOK && readErr == nil && closeErr == nil {
				var doc map[string]any
				if err := json.Unmarshal(body, &doc); err != nil {
					t.Fatalf("decode health JSON: %v\nbody:\n%s", err, body)
				}
				return doc
			}
			last = fmt.Sprintf("status=%d read=%v close=%v body=%s", resp.StatusCode, readErr, closeErr, body)
		} else {
			last = err.Error()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("prompts never served health at %s: %s\nstdout:\n%s\nstderr:\n%s", url, last, stdout.String(), stderr.String())
	return nil
}

func stopProcess(cancel context.CancelFunc, done <-chan error) {
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
