package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
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

	"github/internal/githubapp"
)

func TestTemporaryInstallLayoutBootsAndServesHealth(t *testing.T) {
	// R-4LKF-FB23
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(moduleRoot, "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read authored manifest: %v", err)
	}
	versionBytes, err := os.ReadFile(filepath.Join(moduleRoot, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	version := strings.TrimSpace(string(versionBytes))
	if version == "" {
		t.Fatal("VERSION is empty")
	}

	root := t.TempDir()
	installRoot := filepath.Join(root, "github")
	stateDir := filepath.Join(installRoot, "state")
	cacheDir := filepath.Join(installRoot, "cache")
	libexecDir := filepath.Join(installRoot, "libexec")
	binDir := filepath.Join(installRoot, "bin")
	versionEtcDir := filepath.Join(installRoot, "etc", version)
	for _, dir := range []string{stateDir, cacheDir, libexecDir, binDir, versionEtcDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create install directory %s: %v", dir, err)
		}
	}

	binary := filepath.Join(libexecDir, "github-"+version)
	build := exec.Command("go", "build", "-o", binary, "./cmd/github")
	build.Dir = moduleRoot
	build.Env = withEnv(os.Environ(), map[string]string{"GOWORK": "off"})
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build github binary: %v\n%s", err, output)
	}

	run := filepath.Join(binDir, "run")
	if err := os.Symlink(filepath.Join("..", "libexec", filepath.Base(binary)), run); err != nil {
		t.Fatalf("create bin/run symlink: %v", err)
	}
	installedManifest := filepath.Join(versionEtcDir, "manifest.env")
	if err := os.WriteFile(installedManifest, manifestBytes, 0o644); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	current := filepath.Join(installRoot, "etc", "current")
	if err := os.Symlink(version, current); err != nil {
		t.Fatalf("create etc/current symlink: %v", err)
	}

	assertSymlinkResolvesTo(t, run, binary)
	assertSymlinkResolvesTo(t, current, versionEtcDir)
	installedBytes, err := os.ReadFile(filepath.Join(current, "manifest.env"))
	if err != nil {
		t.Fatalf("read installed current manifest: %v", err)
	}
	if !bytes.Equal(installedBytes, manifestBytes) {
		t.Fatalf("installed manifest differs from authored bytes\ngot:\n%s\nwant:\n%s", installedBytes, manifestBytes)
	}

	privateKey := throwawayPrivateKeyPEM(t)
	port := freeLoopbackPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = withEnv(os.Environ(), map[string]string{
		"GITHUB_PORT":              fmt.Sprint(port),
		"IKIGENBA_APP_ID":          "",
		"IKIGENBA_APP_PRIVATE_KEY": privateKey,
		"IKIGENBA_ROOT":            root,
		"TELEMETRY_ENABLED":        "false",
	})
	if err := cmd.Start(); err != nil {
		t.Fatalf("start installed binary: %v", err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-processDone:
		default:
		}
	})

	health, err := waitForHealth(ctx, port)
	if err != nil {
		t.Fatalf("installed binary did not serve health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if health.Service != "github" || health.Status != "ok" {
		t.Fatalf("health envelope service=%q status=%q, want github/ok", health.Service, health.Status)
	}
	if health.Details == nil {
		t.Fatal("health envelope omitted details")
	}
	if _, ok := health.Details["error"].(string); !ok {
		t.Fatalf("offline health details = %#v, want surfaced reporter error", health.Details)
	}

	dbPath := filepath.Join(stateDir, "github.db")
	if info, err := os.Stat(dbPath); err != nil || info.IsDir() {
		t.Fatalf("database was not created at %s: info=%v err=%v", dbPath, info, err)
	}
	generationPath := filepath.Join(cacheDir, "github.db.generation")
	if info, err := os.Stat(generationPath); err != nil || info.IsDir() {
		t.Fatalf("generation sidecar was not created at %s: info=%v err=%v", generationPath, info, err)
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt installed binary: %v", err)
	}
	select {
	case err := <-processDone:
		if err != nil {
			t.Fatalf("installed binary did not shut down cleanly: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-ctx.Done():
		t.Fatalf("installed binary did not stop: %v\nstdout:\n%s\nstderr:\n%s", ctx.Err(), stdout.String(), stderr.String())
	}
}

func TestAuthoredManifestContainsNoInstallOrComposedDataPaths(t *testing.T) {
	// R-8DF1-W89F
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read authored manifest: %v", err)
	}
	if bytes.Contains(contents, []byte("/opt/")) {
		t.Fatalf("authored manifest contains an absolute /opt/ path:\n%s", contents)
	}
	forbidden := regexp.MustCompile(`(?m)^GITHUB_(?:DB_PATH|GENERATION_PATH)=`)
	if line := forbidden.Find(contents); line != nil {
		t.Fatalf("authored manifest contains composed data path %q", line)
	}
}

func TestAuthoredManifestMatchesSpecEmission(t *testing.T) {
	// R-8IAN-FB87
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read authored manifest: %v", err)
	}
	spec := githubapp.Spec()
	extras := make([]manifest.KV, len(spec.ManifestExtras))
	for i, extra := range spec.ManifestExtras {
		extras[i] = manifest.KV{Key: extra.Key, Value: extra.Value}
	}
	want := manifest.Emit(manifest.Fields{
		App:      spec.App,
		Mount:    spec.Mount,
		Default:  spec.Default,
		Port:     spec.Port,
		MCP:      spec.MCP,
		Feed:     spec.Feed,
		Consumes: spec.Consumes,
		Extras:   extras,
	})
	if !bytes.Equal(contents, []byte(want)) {
		t.Fatalf("authored manifest differs from Spec emission\ngot:\n%s\nwant:\n%s", contents, want)
	}
}

func assertSymlinkResolvesTo(t *testing.T, link, want string) {
	t.Helper()
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("resolve symlink %s: %v", link, err)
	}
	want, err = filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve intended target %s: %v", want, err)
	}
	if got != want {
		t.Fatalf("symlink %s resolves to %s, want %s", link, got, want)
	}
}

func throwawayPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate throwaway RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate loopback port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

type healthEnvelope struct {
	Status  string         `json:"status"`
	Service string         `json:"service"`
	Details map[string]any `json:"details"`
}

func waitForHealth(ctx context.Context, port int) (healthEnvelope, error) {
	client := &http.Client{
		Timeout:   250 * time.Millisecond,
		Transport: &http.Transport{Proxy: nil},
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", port), nil)
		if err != nil {
			return healthEnvelope{}, err
		}
		response, err := client.Do(req)
		if err == nil {
			var body healthEnvelope
			decodeErr := json.NewDecoder(response.Body).Decode(&body)
			closeErr := response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && closeErr == nil {
				return body, nil
			}
			lastErr = fmt.Errorf("status=%d body=%+v decode=%v close=%v", response.StatusCode, body, decodeErr, closeErr)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return healthEnvelope{}, fmt.Errorf("health never became ready: %w (last error: %v)", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func withEnv(base []string, overrides map[string]string) []string {
	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}
