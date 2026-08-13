package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"appkit/manifest"
	"registry"
)

// R-8DF1-W89F
func TestCommittedManifestIsPortable(t *testing.T) {
	body := readRepoFile(t, "etc", "manifest.env")
	if bytes.Contains(body, []byte("/opt/")) {
		t.Fatalf("manifest.env contains an on-box path:\n%s", body)
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("ARTIFACTS_DB_PATH=")) || bytes.HasPrefix(line, []byte("ARTIFACTS_GENERATION_PATH=")) {
			t.Fatalf("manifest.env contains runtime path line %q", line)
		}
	}
}

// R-8IAN-FB87
func TestEmittedManifestByteAgreesWithCommittedFile(t *testing.T) {
	got := manifest.Emit(manifest.Fields{
		App:     "artifacts",
		Mount:   "/srv/artifacts/",
		Port:    registry.MustPort("artifacts"),
		MCP:     true,
		Feed:    "/feed",
		Extras:  []manifest.KV{{Key: "ARTIFACTS_MAX_UPLOAD_BYTES", Value: "209715200"}},
		Default: false,
	})
	if want := string(readRepoFile(t, "etc", "manifest.env")); got != want {
		t.Fatalf("emitted manifest differs from committed file\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// R-3BZW-NSZ5
func TestVersionManifestAndUploadLimitContract(t *testing.T) {
	version := strings.TrimSpace(string(readRepoFile(t, "VERSION")))
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
		t.Fatalf("VERSION = %q, want v-prefixed SemVer", version)
	}
	manifestBody := string(readRepoFile(t, "etc", "manifest.env"))
	if !strings.Contains(manifestBody, "ARTIFACTS_MAX_UPLOAD_BYTES=209715200\n") {
		t.Fatalf("manifest.env lacks the upload default:\n%s", manifestBody)
	}

	got, err := loadConfig(func(string) string { return "" })
	if err != nil || got.(config).MaxUploadBytes != defaultMaxUploadBytes {
		t.Fatalf("default config = %#v, %v", got, err)
	}
	for _, invalid := range []string{"nope", "0", "-1"} {
		_, err := loadConfig(func(string) string { return invalid })
		if err == nil || !strings.Contains(err.Error(), "ARTIFACTS_MAX_UPLOAD_BYTES") {
			t.Errorf("loadConfig(%q) error = %v, want named rejection", invalid, err)
		}
	}
	got, err = loadConfig(func(string) string { return "1024" })
	if err != nil || got.(config).MaxUploadBytes != 1024 {
		t.Fatalf("explicit config = %#v, %v", got, err)
	}

	binary := filepath.Join(t.TempDir(), "artifacts")
	build := exec.Command("go", "build", "-ldflags", "-X appkit.version=v0.1.0", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build artifacts for startup rejection: %v\n%s", err, out)
	}
	command := exec.Command(binary, "serve")
	command.Env = append(os.Environ(), "ARTIFACTS_MAX_UPLOAD_BYTES=invalid")
	out, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("serve accepted invalid ARTIFACTS_MAX_UPLOAD_BYTES; output:\n%s", out)
	}
	if !bytes.Contains(out, []byte("ARTIFACTS_MAX_UPLOAD_BYTES")) {
		t.Fatalf("startup rejection does not name ARTIFACTS_MAX_UPLOAD_BYTES:\n%s", out)
	}
}

// R-3AS0-A18G
func TestCompositionUsesRegistryPortAndNoHardcodedLoopbackPort(t *testing.T) {
	mainSource := readRepoFile(t, "cmd", "artifacts", "main.go")
	if !bytes.Contains(mainSource, []byte(`Port:       registry.MustPort("artifacts")`)) {
		t.Fatalf("composition root does not resolve Spec.Port with registry.MustPort: \n%s", mainSource)
	}

	root := filepath.Join("..", "..")
	needle := "127.0.0.1:" + "30"
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(body, []byte(needle)) {
			return fmt.Errorf("%s contains forbidden loopback port prefix", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// R-VKB6-SHHV
func TestProductionGoSourceHasNoOptLiteral(t *testing.T) {
	root := filepath.Join("..", "..")
	needle := []byte{'"', '/', 'o', 'p', 't'}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(body, needle) {
			return fmt.Errorf("%s contains forbidden on-box path literal", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// R-O1AD-MRKW
func TestAgentsDeclaresTestingContract(t *testing.T) {
	document := string(readRepoFile(t, "AGENTS.md"))
	start := strings.Index(document, "## Tests\n")
	if start < 0 {
		t.Fatal("AGENTS.md has no Tests section")
	}
	section := document[start:]
	if end := strings.Index(section[len("## Tests\n"):], "\n## "); end >= 0 {
		section = section[:len("## Tests\n")+end]
	}
	normalized := strings.Join(strings.Fields(section), " ")
	for name, declaration := range map[string]string{
		"default gate":              "Default gate: `go test ./...` from `artifacts/`.",
		"layers":                    "Layers present: **hermetic** and **composed**; there is no **live** layer.",
		"bash precondition":         "a POSIX `bash` with `grep`",
		"browser precondition":      "a headless-Chrome binary",
		"workspace mode":            "GOWORK mode: **workspace** through the repo-root `go.work`",
		"production workspace mode": "production builds use `GOWORK=off`",
	} {
		if !strings.Contains(normalized, declaration) {
			t.Errorf("Tests section missing %s declaration %q", name, declaration)
		}
	}
}

// R-O2IA-0JBL
func TestDefaultTestFilesContainNoSkips(t *testing.T) {
	root := filepath.Join("..", "..")
	verbs := []string{"Skip", "Skipf", "SkipNow"}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, verb := range verbs {
			needle := []byte("t." + verb + "(")
			if bytes.Contains(body, needle) {
				return fmt.Errorf("%s contains forbidden test skip call", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// R-39K3-W9HR
// R-4LKF-FB23
func TestInstalledBinaryServesHealthyVersionedEnvelope(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "artifacts")
	for _, dir := range []string{"bin", "cache", "etc", "libexec", "share", "state"} {
		if err := os.MkdirAll(filepath.Join(appRoot, dir), 0o755); err != nil {
			t.Fatalf("create install directory: %v", err)
		}
	}
	installTestWWW(t, appRoot)
	version := strings.TrimSpace(string(readRepoFile(t, "VERSION")))
	binary := filepath.Join(appRoot, "libexec", "artifacts-"+version)
	build := exec.Command("go", "build", "-ldflags", "-X appkit.version="+version, "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real artifacts binary: %v\n%s", err, out)
	}
	run := filepath.Join(appRoot, "bin", "run")
	if err := os.Symlink(filepath.Join("..", "libexec", "artifacts-"+version), run); err != nil {
		t.Fatalf("link installed binary: %v", err)
	}

	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	cmd.Env = append(os.Environ(),
		"IKIGENBA_DOMAIN=int.ikigenba.com",
		"IKIGENBA_ROOT="+root,
		"ARTIFACTS_IP=127.0.0.1",
		fmt.Sprintf("ARTIFACTS_PORT=%d", port),
		"ARTIFACTS_MAX_UPLOAD_BYTES=209715200",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start installed binary: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()

	doc := waitForHealth(t, port, done, &stdout, &stderr)
	for key, want := range map[string]string{"status": "ok", "service": "artifacts", "version": version} {
		if got := doc[key]; got != want {
			t.Fatalf("health %s = %v, want %q; envelope=%v", key, got, want, doc)
		}
	}
	if _, err := os.Stat(filepath.Join(appRoot, "state", "artifacts.db")); err != nil {
		t.Fatalf("installed service did not create state database: %v", err)
	}
}

// R-P52Q-H8MG
func TestInstalledBinaryMountsIdentityProtectedMCPTools(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "artifacts")
	for _, dir := range []string{"bin", "cache", "etc", "libexec", "share", "state"} {
		if err := os.MkdirAll(filepath.Join(appRoot, dir), 0o755); err != nil {
			t.Fatalf("create install directory: %v", err)
		}
	}
	installTestWWW(t, appRoot)
	version := strings.TrimSpace(string(readRepoFile(t, "VERSION")))
	binary := filepath.Join(appRoot, "libexec", "artifacts-"+version)
	build := exec.Command("go", "build", "-ldflags", "-X appkit.version="+version, "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real artifacts binary: %v\n%s", err, out)
	}
	run := filepath.Join(appRoot, "bin", "run")
	if err := os.Symlink(filepath.Join("..", "libexec", "artifacts-"+version), run); err != nil {
		t.Fatalf("link installed binary: %v", err)
	}

	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	cmd.Env = append(os.Environ(),
		"IKIGENBA_DOMAIN=int.ikigenba.com",
		"IKIGENBA_ROOT="+root,
		"ARTIFACTS_IP=127.0.0.1",
		fmt.Sprintf("ARTIFACTS_PORT=%d", port),
		"ARTIFACTS_MAX_UPLOAD_BYTES=209715200",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start installed binary: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()
	waitForHealth(t, port, done, &stdout, &stderr)

	type rpcResponse struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	post := func(id, method string) rpcResponse {
		t.Helper()
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"method":%q,"params":{}}`, id, method)
		request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/mcp", port), strings.NewReader(body))
		if err != nil {
			t.Fatalf("create %s request: %v", method, err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Owner-Id", "composed-smoke-owner")
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
		if err != nil {
			t.Fatalf("POST /mcp %s: %v", method, err)
		}
		defer response.Body.Close()
		var decoded rpcResponse
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode %s response: %v", method, err)
		}
		if response.StatusCode != http.StatusOK || decoded.JSONRPC != "2.0" || decoded.ID != id || len(decoded.Result) == 0 || len(decoded.Error) != 0 {
			t.Fatalf("%s status=%d response=%+v, want successful JSON-RPC result", method, response.StatusCode, decoded)
		}
		return decoded
	}

	post("initialize", "initialize")
	listed := post("tools", "tools/list")
	var discovery struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listed.Result, &discovery); err != nil {
		t.Fatalf("decode tools/list result: %v", err)
	}
	want := map[string]bool{
		"upload": true, "import": true, "list": true, "get": true,
		"update": true, "set_visibility": true, "delete": true, "guide": true,
	}
	foundDomain := 0
	for _, tool := range discovery.Tools {
		if tool.Name == "health" || tool.Name == "reflection" {
			continue
		}
		if !want[tool.Name] {
			t.Fatalf("tools/list returned unexpected or duplicate tool %q: %+v", tool.Name, discovery.Tools)
		}
		delete(want, tool.Name)
		foundDomain++
	}
	if foundDomain != 8 || len(want) != 0 {
		t.Fatalf("tools/list domain set is incomplete: found=%d omitted=%v tools=%+v", foundDomain, want, discovery.Tools)
	}

	unauthorizedBody := `{"jsonrpc":"2.0","id":"unauthorized","method":"tools/list","params":{}}`
	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/mcp", port), strings.NewReader(unauthorizedBody))
	if err != nil {
		t.Fatalf("create unidentified tools/list request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("POST unidentified /mcp tools/list: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read unidentified tools/list response: %v", err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unidentified tools/list status=%d body=%s, want 401", response.StatusCode, body)
	}
	var unauthorized map[string]json.RawMessage
	if json.Unmarshal(body, &unauthorized) == nil && len(unauthorized["result"]) != 0 {
		t.Fatalf("unidentified tools/list executed a tool: %s", body)
	}
}

func readRepoFile(t *testing.T, elements ...string) []byte {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, elements...)...)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return body
}

func installTestWWW(t *testing.T, appRoot string) {
	t.Helper()
	current := filepath.Join(appRoot, "share", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("create installed share/current: %v", err)
	}
	committed, err := filepath.Abs(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatalf("resolve committed share/www: %v", err)
	}
	if err := os.Symlink(committed, filepath.Join(current, "www")); err != nil {
		t.Fatalf("install share/current/www: %v", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHealth(t *testing.T, port int, done <-chan error, stdout, stderr *bytes.Buffer) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("artifacts exited before health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
		default:
		}
		response, err := http.Get(url)
		if err == nil {
			defer response.Body.Close()
			var doc map[string]any
			if err := json.NewDecoder(response.Body).Decode(&doc); err != nil {
				t.Fatalf("decode health response: %v", err)
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("health status code = %d; body=%v", response.StatusCode, doc)
			}
			return doc
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("health did not become ready\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	return nil
}
