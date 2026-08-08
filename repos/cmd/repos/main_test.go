package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"appkit"
	"appkit/config"
	appdb "appkit/db"
	"appkit/manifest"
	"appkit/server"
	appweb "appkit/web"
	"eventplane/consumer"
	"eventplane/outbox"

	reposdb "repos/internal/db"
	"repos/internal/repos"
	"repos/internal/runner"
)

func TestInstalledLayoutBootsBuiltService(t *testing.T) {
	// R-4LKF-FB23
	const version = "phase-23-test"
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	cacheDir := filepath.Join(root, "cache")
	libexecDir := filepath.Join(root, "libexec")
	binDir := filepath.Join(root, "bin")
	etcVersionDir := filepath.Join(root, "etc", version)
	shareVersionDir := filepath.Join(root, "share", version)
	for _, dir := range []string{stateDir, cacheDir, libexecDir, binDir, etcVersionDir, shareVersionDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	binary := filepath.Join(libexecDir, "repos-"+version)
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build repos binary: %v: %s", err, output)
	}
	runLink := filepath.Join(binDir, "run")
	if err := os.Symlink(filepath.Join("..", "libexec", filepath.Base(binary)), runLink); err != nil {
		t.Fatal(err)
	}

	committedManifest := filepath.Join("..", "..", "etc", "manifest.env")
	manifestBytes, err := os.ReadFile(committedManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcVersionDir, "manifest.env"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	etcCurrent := filepath.Join(root, "etc", "current")
	if err := os.Symlink(version, etcCurrent); err != nil {
		t.Fatal(err)
	}

	wwwDir := filepath.Join(shareVersionDir, "www")
	copyTree(t, filepath.Join("..", "..", "share", "www"), wwwDir)
	shareCurrent := filepath.Join(root, "share", "current")
	if err := os.Symlink(version, shareCurrent); err != nil {
		t.Fatal(err)
	}

	assertSymlinkTarget(t, runLink, binary)
	assertSymlinkTarget(t, etcCurrent, etcVersionDir)
	assertSymlinkTarget(t, shareCurrent, shareVersionDir)

	port := freeLoopbackPort(t)
	dbPath := filepath.Join(stateDir, "repos.db")
	generationPath := filepath.Join(cacheDir, "repos.db.generation")
	command := exec.Command(runLink, "serve", "-port", fmt.Sprint(port))
	command.Dir = root
	command.Env = append(os.Environ(),
		"ANTHROPIC_API_KEY=phase-23-fixture-key",
		"REPOS_STATE_DIR="+stateDir,
		"REPOS_DB_PATH="+dbPath,
		"REPOS_GENERATION_PATH="+generationPath,
		"REPOS_WWW_PATH="+filepath.Join(shareCurrent, "www"),
		"REPOS_LOG_LEVEL=error",
	)
	var processOutput bytes.Buffer
	command.Stdout = &processOutput
	command.Stderr = &processOutput
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			_ = command.Process.Kill()
			_ = command.Wait()
		})
	}
	t.Cleanup(stop)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	var health map[string]any
	healthy := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, requestErr := client.Get(baseURL + "/health")
		if requestErr == nil {
			decodeErr := json.NewDecoder(response.Body).Decode(&health)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil {
				healthy = true
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !healthy {
		stop()
		t.Fatalf("service did not become healthy: %s", processOutput.String())
	}
	if health["service"] != "repos" || health["status"] != "ok" {
		t.Fatalf("health envelope = %#v", health)
	}

	response, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") || !bytes.Contains(body, []byte("repos")) {
		t.Fatalf("landing status=%d content-type=%q body=%s", response.StatusCode, response.Header.Get("Content-Type"), body)
	}
	for _, path := range []string{dbPath, generationPath} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("expected service file %s: %v", path, statErr)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("service file %s mode = %s", path, info.Mode())
		}
	}
}

func TestAuthoredManifestContainsOnlyPortableInputs(t *testing.T) {
	// R-8DF1-W89F
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte("/opt/")) {
		t.Fatalf("manifest contains an absolute /opt/ path:\n%s", contents)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(line, "REPOS_DB_PATH=") || strings.HasPrefix(line, "REPOS_GENERATION_PATH=") {
			t.Fatalf("manifest contains composed data path %q", line)
		}
	}
}

func TestAuthoredManifestMatchesCompiledSpecDefaults(t *testing.T) {
	// R-8IAN-FB87
	spec := reposSpec()
	consumes := make([]string, 0, len(spec.Consumers))
	for _, consumer := range spec.Consumers {
		consumes = append(consumes, consumer.Source)
	}
	extras := make([]manifest.KV, 0, len(spec.ManifestExtras))
	for _, extra := range spec.ManifestExtras {
		extras = append(extras, manifest.KV{Key: extra.Key, Value: extra.Value})
	}
	want := manifest.Emit(manifest.Fields{
		App: spec.App, Mount: spec.Mount, Default: spec.Default, Port: spec.Port,
		MCP: spec.MCP, Feed: spec.Feed, Consumes: consumes, Extras: extras,
	})
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(want), committed) {
		t.Fatalf("compiled manifest:\n%s\ncommitted manifest:\n%s", want, committed)
	}
}

func TestSessionEngineDefaultsMatchAuthoredManifest(t *testing.T) {
	// R-L9EG-DDWC
	for _, key := range []string{"REPOS_PROVIDER", "REPOS_MODEL", "REPOS_SESSION_TTL", "REPOS_MAX_SESSIONS"} {
		t.Setenv(key, "")
	}

	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatal(err)
	}
	authored := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			authored[key] = value
		}
	}

	model := runner.DefaultModelConfig(os.Getenv("ANTHROPIC_API_KEY"))
	model.Provider = config.EnvOr(os.Getenv, "REPOS_PROVIDER", model.Provider)
	model.Model = config.EnvOr(os.Getenv, "REPOS_MODEL", model.Model)
	ttl, err := config.EnvOrDuration(os.Getenv, "REPOS_SESSION_TTL", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	maxRun, err := config.EnvOrInt(os.Getenv, "REPOS_MAX_SESSIONS", 2)
	if err != nil {
		t.Fatal(err)
	}

	manifestValue := func(key string) string {
		t.Helper()
		value, ok := authored[key]
		if !ok {
			t.Fatalf("manifest is missing %s", key)
		}
		return value
	}
	if want := manifestValue("REPOS_PROVIDER"); model.Provider != want {
		t.Errorf("REPOS_PROVIDER resolved default = %q, manifest value = %q", model.Provider, want)
	}
	if want := manifestValue("REPOS_MODEL"); model.Model != want {
		t.Errorf("REPOS_MODEL resolved default = %q, manifest value = %q", model.Model, want)
	}
	wantTTL, err := time.ParseDuration(manifestValue("REPOS_SESSION_TTL"))
	if err != nil {
		t.Fatalf("parse manifest REPOS_SESSION_TTL: %v", err)
	}
	if ttl != wantTTL {
		t.Errorf("REPOS_SESSION_TTL resolved default = %s, manifest value = %s", ttl, wantTTL)
	}
	wantMaxRun, err := strconv.Atoi(manifestValue("REPOS_MAX_SESSIONS"))
	if err != nil {
		t.Fatalf("parse manifest REPOS_MAX_SESSIONS: %v", err)
	}
	if maxRun != wantMaxRun {
		t.Errorf("REPOS_MAX_SESSIONS resolved default = %d, manifest value = %d", maxRun, wantMaxRun)
	}
}

func TestProductionGoSourceContainsNoCompiledOptPath(t *testing.T) {
	// R-VKB6-SHHV
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(contents, []byte(`"/opt`)) {
			t.Errorf("production Go source %s contains a compiled /opt path literal", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s to %s: %v", source, destination, err)
	}
}

func assertSymlinkTarget(t *testing.T, link, want string) {
	t.Helper()
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("resolve %s: %v", link, err)
	}
	want, err = filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve target %s: %v", want, err)
	}
	if got != want {
		t.Fatalf("%s resolves to %s, want %s", link, got, want)
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestManifestRenderMatchesCommittedServiceContract(t *testing.T) {
	// R-EISY-2LYZ
	output := []byte(appkit.Manifest(reposSpec()))
	want := "APP=repos\nMOUNT=/srv/repos/\nDEFAULT=false\nPORT=3007\nMCP=true\nFEED=/feed\nCONSUMES=webhooks\nREPOS_PROVIDER=anthropic\nREPOS_MODEL=claude-opus-4-8\nREPOS_SESSION_TTL=30m\nREPOS_MAX_SESSIONS=2\n"
	if string(output) != want {
		t.Fatalf("manifest output:\n%s\nwant:\n%s", output, want)
	}
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, committed) {
		t.Fatalf("manifest render differs from etc/manifest.env:\n%s", committed)
	}
}

func TestAssembledRoutesGateMCPAndCreateRowsFromOwnerIdentity(t *testing.T) {
	// R-EL8Q-U5GD
	// R-IEYB-SNAO
	root := t.TempDir()
	dbPath := filepath.Join(root, "repos.db")
	conn, err := appdb.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := reposdb.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}

	remoteRoot := filepath.Join(root, "remotes")
	if err := os.MkdirAll(remoteRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, "", "init", "--bare", filepath.Join(remoteRoot, "fixture.git"))
	gitConfig := filepath.Join(root, "gitconfig")
	configText := fmt.Sprintf("[url \"file://%s/\"]\n\tinsteadOf = https://github.com/ikigenba/\n", filepath.ToSlash(remoteRoot))
	if err := os.WriteFile(gitConfig, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	t.Setenv("REPOS_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("ANTHROPIC_API_KEY", "fixture-key")

	var peerMu sync.Mutex
	var peerRequests []*http.Request
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		peerMu.Lock()
		peerRequests = append(peerRequests, request.Clone(context.Background()))
		peerMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet && request.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"token":"fixture-token","expires_at":"2026-07-20T14:00:00Z"}`)
			return
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer peer.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = rewriteTransport{target: peer.URL, base: originalTransport}
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	site, err := appweb.Load(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := outbox.New(conn, outbox.Options{
		Source: "repos", DBPath: dbPath, GenerationPath: filepath.Join(root, "generation"), Registry: repos.Events,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := reposSpec()
	subscriptions := func() []consumer.Subscription {
		var all []consumer.Subscription
		for _, entry := range spec.Consumers {
			all = append(all, entry.Subscriptions...)
		}
		return all
	}
	srv, err := server.New(server.Options{
		Addr: "127.0.0.1:0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/repos/", AuthServer: "https://example.test/",
		Version: "test", Service: spec.App, Health: spec.Health, Events: spec.Events,
		Subscriptions: subscriptions, WWW: site, Feed: producer.FeedHandler(), FeedPath: spec.Feed,
		DB: conn, Register: spec.Handlers,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Producer(producer); err != nil {
		t.Fatal(err)
	}

	unauthenticated := httptest.NewRecorder()
	srv.Handler.ServeHTTP(unauthenticated, rpcRequest(t, false))
	if unauthenticated.Code != http.StatusUnauthorized && unauthenticated.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated POST /mcp status = %d, want 401 or 403", unauthenticated.Code)
	}
	authenticated := httptest.NewRecorder()
	srv.Handler.ServeHTTP(authenticated, rpcRequest(t, true))
	if authenticated.Code != http.StatusOK || !strings.Contains(authenticated.Body.String(), `"name":"clone"`) {
		t.Fatalf("authenticated tools/list status=%d body=%s", authenticated.Code, authenticated.Body.String())
	}

	clone := rpcToolCall(t, srv.Handler, "owner-x", "owner@example.com", "clone", map[string]any{"name": "fixture", "owner": "bogus-owner"})
	if clone.Code != http.StatusOK || strings.Contains(clone.Body.String(), `"isError":true`) {
		t.Fatalf("clone status=%d body=%s", clone.Code, clone.Body.String())
	}
	start := rpcToolCall(t, srv.Handler, "owner-x", "owner@example.com", "session_start", map[string]any{"repo": "fixture", "instructions": "work", "owner": "bogus-owner"})
	if start.Code != http.StatusOK || strings.Contains(start.Body.String(), `"isError":true`) {
		t.Fatalf("session_start status=%d body=%s", start.Code, start.Body.String())
	}
	var repoOwnerID, repoOwnerEmail, sessionID, sessionOwnerID, sessionOwnerEmail string
	if err := conn.QueryRow(`SELECT owner_id, owner_email FROM repos WHERE name = 'fixture'`).Scan(&repoOwnerID, &repoOwnerEmail); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT id, owner_id, owner_email FROM sessions WHERE repo_name = 'fixture'`).Scan(&sessionID, &sessionOwnerID, &sessionOwnerEmail); err != nil {
		t.Fatal(err)
	}
	if repoOwnerID != "owner-x" || repoOwnerEmail != "owner@example.com" || sessionOwnerID != "owner-x" || sessionOwnerEmail != "owner@example.com" {
		t.Fatalf("stored identities repo=(%q,%q) session=(%q,%q)", repoOwnerID, repoOwnerEmail, sessionOwnerID, sessionOwnerEmail)
	}
	if response := rpcToolCall(t, srv.Handler, "owner-y", "owner@example.com", "get", map[string]any{"name": "fixture"}); !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("cross-owner get body=%s", response.Body.String())
	}
	if response := rpcToolCall(t, srv.Handler, "owner-y", "owner@example.com", "session_get", map[string]any{"id": sessionID}); !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("cross-owner session_get body=%s", response.Body.String())
	}

	landing := httptest.NewRecorder()
	srv.Handler.ServeHTTP(landing, httptest.NewRequest(http.MethodGet, "/", nil))
	if landing.Code != http.StatusOK || !strings.HasPrefix(landing.Header().Get("Content-Type"), "text/html") || !strings.Contains(landing.Body.String(), "repos") {
		t.Fatalf("landing status=%d content-type=%q body=%s", landing.Code, landing.Header().Get("Content-Type"), landing.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	feedRequest := httptest.NewRequest(http.MethodGet, "/feed?from=tail", nil).WithContext(ctx)
	feedResponse := newSSERecorder()
	done := make(chan struct{})
	go func() {
		srv.Handler.ServeHTTP(feedResponse, feedRequest)
		close(done)
	}()
	select {
	case <-feedResponse.flushed:
		if got := feedResponse.Header().Get("Content-Type"); got != "text/event-stream" {
			t.Fatalf("feed Content-Type = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GET /feed did not establish an SSE response")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("GET /feed did not stop after request cancellation")
	}
	peerMu.Lock()
	defer peerMu.Unlock()
	if len(peerRequests) != 1 || peerRequests[0].URL.Path != "/token" {
		t.Fatalf("assembled peer requests = %#v, want one token request", peerRequests)
	}
}

func rpcToolCall(t *testing.T, handler http.Handler, ownerID, ownerEmail, name string, args map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Owner-Id", ownerID)
	request.Header.Set("X-Owner-Email", ownerEmail)
	request.Header.Set("X-Client-Id", "fixture")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func rpcRequest(t *testing.T, authenticated bool) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authenticated {
		request.Header.Set("X-Owner-Id", "owner-1")
		request.Header.Set("X-Owner-Email", "owner@example.com")
		request.Header.Set("X-Client-Id", "fixture")
	}
	return request
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

type rewriteTransport struct {
	target string
	base   http.RoundTripper
}

func (r rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	target, err := http.NewRequest(http.MethodGet, r.target, nil)
	if err != nil {
		return nil, err
	}
	clone.URL.Scheme = target.URL.Scheme
	clone.URL.Host = target.URL.Host
	return r.base.RoundTrip(clone)
}

type sseRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	once    sync.Once
}

func newSSERecorder() *sseRecorder {
	return &sseRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
}

func (r *sseRecorder) Flush() {
	r.ResponseRecorder.Flush()
	r.once.Do(func() { close(r.flushed) })
}
