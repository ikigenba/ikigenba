package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"appkit"
	"appkit/manifest"
)

func TestInstalledLayoutBootsBuiltService(t *testing.T) {
	// R-4LKF-FB23
	const version = "phase-27-test"
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
	command.Env = append(os.Environ(),
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
	want := "APP=repos\nMOUNT=/srv/repos/\nDEFAULT=false\nPORT=3007\nMCP=true\nFEED=/feed\n"
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

func TestReducedSpecHasNoConsumersOrExtrasAndHasCustodyWorker(t *testing.T) {
	spec := reposSpec()
	if spec.Consumers != nil || spec.ManifestExtras != nil || len(spec.Workers) != 1 {
		t.Fatalf("reduced spec consumers=%#v extras=%#v workers=%#v", spec.Consumers, spec.ManifestExtras, spec.Workers)
	}
	if spec.Handlers == nil || spec.Producer == nil || spec.Health == nil {
		t.Fatal("reduced spec is missing handlers, producer, or health")
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
