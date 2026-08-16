package main

import (
	"appkit"
	"appkit/manifest"
	"bytes"
	"context"
	"encoding/json"
	"eventplane/outbox"
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

	appdb "appkit/db"

	appkitserver "appkit/server"

	reposdb "repos/internal/db"
)

func TestInstalledLayoutBootsBuiltService(t *testing.T) {
	// R-4LKF-FB23
	// R-QYBJ-M8E6
	const version = "phase-27-test"
	installRoot := t.TempDir()
	root := filepath.Join(installRoot, "repos")
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

	manifestBytes, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
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
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"IKIGENBA_ROOT=" + installRoot,
		"REPOS_LOG_LEVEL=error",
		"TELEMETRY_ENABLED=0",
	}
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
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
healthCheck:
	for {
		response, requestErr := client.Get(baseURL + "/health")
		if requestErr == nil {
			decodeErr := json.NewDecoder(response.Body).Decode(&health)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil {
				healthy = true
				break
			}
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			break healthCheck
		}
	}
	if !healthy {
		stop()
		t.Fatalf("service did not become healthy: %s", processOutput.String())
	}
	// R-JPOJ-ZCS5
	bare := filepath.Join(stateDir, "git", "sites", "guard.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit := exec.Command("git", "init", "--bare", "--initial-branch=main", bare)
	if output, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("initialize guard fixture: %v: %s", err, output)
	}
	readURL := baseURL + "/list?kind=sites&name=guard"
	loopback, err := client.Get(readURL)
	if err != nil {
		t.Fatal(err)
	}
	loopbackBody, _ := io.ReadAll(loopback.Body)
	_ = loopback.Body.Close()
	if loopback.StatusCode != http.StatusOK || !bytes.Contains(loopbackBody, []byte(`"entries":[]`)) {
		t.Fatalf("loopback read status=%d body=%q", loopback.StatusCode, loopbackBody)
	}
	proxiedRequest, err := http.NewRequest(http.MethodGet, readURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxiedRequest.Header.Set("X-Forwarded-Proto", "https")
	proxied, err := client.Do(proxiedRequest)
	if err != nil {
		t.Fatal(err)
	}
	proxiedBody, _ := io.ReadAll(proxied.Body)
	_ = proxied.Body.Close()
	if proxied.StatusCode != http.StatusNotFound {
		t.Fatalf("nginx-crossed read status=%d body=%q", proxied.StatusCode, proxiedBody)
	}
	details, _ := health["details"].(map[string]any)
	if health["service"] != "repos" || health["status"] != "ok" || details["repositories"] != float64(0) {
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
	gitRoot := filepath.Join(stateDir, "git")
	if info, err := os.Stat(gitRoot); err != nil || !info.IsDir() {
		t.Fatalf("git root %s was not created under composed IKIGENBA_ROOT state: info=%v err=%v", gitRoot, info, err)
	}
}

func TestAssembledSpecRoutesApplyTheirChassisGates(t *testing.T) {
	// R-EL8Q-U5GD
	t.Setenv("REPOS_STATE_DIR", t.TempDir())
	t.Setenv("REPOS_GIT_BIN", "git")
	t.Setenv("REPOS_MAX_COMMIT_BYTES", "")

	dbPath := filepath.Join(t.TempDir(), "repos.db")
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
	producer, err := outbox.New(conn, outbox.Options{
		Source: "repos", Registry: reposSpec().Events,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := reposSpec()
	server, err := appkitserver.New(appkitserver.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "http://localhost/srv/repos/mcp",
		AuthServer: "http://localhost",
		Version:    "test",
		Service:    spec.App,
		Feed:       producer.FeedHandler(),
		FeedPath:   spec.Feed,
		Register:   spec.Handlers,
		Health:     spec.Health,
		Events:     spec.Events,
		WWW:        loadReposWWW(t),
		DB:         conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Producer(producer); err != nil {
		t.Fatal(err)
	}

	mcpBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	unauthorized := httptest.NewRecorder()
	server.Handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(mcpBody)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("POST /mcp without identity status = %d, want 401", unauthorized.Code)
	}
	authorizedRequest := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(mcpBody))
	authorizedRequest.Header.Set("X-Owner-Id", "owner-test")
	authorized := httptest.NewRecorder()
	server.Handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), `"name":"create"`) {
		t.Fatalf("POST /mcp with identity status=%d body=%s", authorized.Code, authorized.Body.String())
	}

	landing := httptest.NewRecorder()
	server.Handler.ServeHTTP(landing, httptest.NewRequest(http.MethodGet, "/", nil))
	if landing.Code != http.StatusOK || !strings.Contains(landing.Body.String(), "<title>repos · repos</title>") {
		t.Fatalf("GET / status=%d body=%s", landing.Code, landing.Body.String())
	}

	feedContext, cancelFeed := context.WithCancel(context.Background())
	cancelFeed()
	feed := httptest.NewRecorder()
	server.Handler.ServeHTTP(feed, httptest.NewRequest(http.MethodGet, "/feed", nil).WithContext(feedContext))
	if feed.Code != http.StatusOK || feed.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("GET /feed status=%d content-type=%q body=%s", feed.Code, feed.Header().Get("Content-Type"), feed.Body.String())
	}

	proxied := httptest.NewRequest(http.MethodGet, "/content", nil)
	proxied.Header.Set("X-Forwarded-Proto", "https")
	guarded := httptest.NewRecorder()
	server.Handler.ServeHTTP(guarded, proxied)
	if guarded.Code != http.StatusNotFound {
		t.Fatalf("proxied GET /content status=%d body=%s", guarded.Code, guarded.Body.String())
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
	extras := make([]manifest.KV, 0, len(spec.ManifestExtras))
	for _, extra := range spec.ManifestExtras {
		extras = append(extras, manifest.KV{Key: extra.Key, Value: extra.Value})
	}
	want := manifest.Emit(manifest.Fields{
		App: spec.App, Mount: spec.Mount, Default: spec.Default, Port: spec.Port,
		MCP: spec.MCP, Feed: spec.Feed, Extras: extras,
	})
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(want), committed) {
		t.Fatalf("compiled manifest:\n%s\ncommitted manifest:\n%s", want, committed)
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

func TestManifestRenderMatchesCommittedServiceContract(t *testing.T) {
	// R-EISY-2LYZ
	output := []byte(appkit.Manifest(reposSpec()))
	want := "APP=repos\nMOUNT=/srv/repos/\nDEFAULT=false\nPORT=3007\nMCP=true\nFEED=/feed\nREPOS_MAX_COMMIT_BYTES=67108864\n"
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

func TestRuntimeKnobDefaultsAgreeWithAuthoredManifest(t *testing.T) {
	// R-39AL-2CA7
	manifestValues := readManifestValues(t)
	knobs, err := resolveRuntimeKnobs(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strconv.FormatInt(knobs.maxCommitBytes, 10), manifestValues["REPOS_MAX_COMMIT_BYTES"]; got != want {
		t.Fatalf("resolved max commit bytes = %q, manifest = %q", got, want)
	}
	wantKeys := []string{"APP", "MOUNT", "DEFAULT", "PORT", "MCP", "FEED", "REPOS_MAX_COMMIT_BYTES"}
	if len(manifestValues) != len(wantKeys) {
		t.Fatalf("manifest keys = %#v, want exactly %#v", manifestValues, wantKeys)
	}
	for _, key := range wantKeys {
		if _, found := manifestValues[key]; !found {
			t.Errorf("manifest is missing declared key %s", key)
		}
	}
	for _, forbidden := range []string{"REPOS_RUN_TOKEN_TTL", "REPOS_PROVIDER", "REPOS_MODEL", "REPOS_SESSION_TTL", "REPOS_MAX_SESSIONS", "CONSUMES"} {
		if _, found := manifestValues[forbidden]; found {
			t.Errorf("manifest contains retired key %s", forbidden)
		}
	}
	root := filepath.Join("..", "..")
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(contents, []byte("REPOS_RUN_TOKEN_TTL")) {
			t.Errorf("production Go source %s reads retired REPOS_RUN_TOKEN_TTL", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSpecHasNoConsumersAndDeclaresWorkersAndManifestExtras(t *testing.T) {
	spec := reposSpec()
	if spec.Consumers != nil || len(spec.ManifestExtras) != 1 || len(spec.Workers) != 2 {
		t.Fatalf("spec consumers=%#v extras=%#v workers=%#v", spec.Consumers, spec.ManifestExtras, spec.Workers)
	}
	if spec.Handlers == nil || spec.Producer == nil || spec.Health == nil {
		t.Fatal("reduced spec is missing handlers, producer, or health")
	}
}

func readManifestValues(t *testing.T) map[string]string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			t.Fatalf("malformed manifest line %q", line)
		}
		if _, duplicate := values[key]; duplicate {
			t.Fatalf("duplicate manifest key %s", key)
		}
		values[key] = value
	}
	return values
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
