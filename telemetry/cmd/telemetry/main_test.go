package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"registry"
)

const helperProcessEnv = "TELEMETRY_TEST_HELPER_PROCESS"

func TestTelemetryProcess(t *testing.T) {
	if os.Getenv(helperProcessEnv) != "1" {
		return
	}
	args := strings.Split(os.Getenv("TELEMETRY_TEST_ARGS"), " ")
	os.Args = append([]string{"telemetry"}, args...)
	appMain()
}

func appMain() {
	main()
}

func TestManifestMatchesCommittedFile(t *testing.T) {
	cmd := telemetryCommand("manifest")
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("emit manifest: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest: %v", err)
	}

	// R-V6NF-9LY8
	if !bytes.Equal(got, want) {
		t.Fatalf("manifest bytes differ\ngot:\n%s\nwant:\n%s", got, want)
	}
	portLine := "PORT=" + strconv.Itoa(registry.MustPort("telemetry")) + "\n"
	for _, required := range []string{"MCP=true\n", "TELEMETRY_RETENTION_DAYS=90\n", portLine} {
		if !bytes.Contains(got, []byte(required)) {
			t.Errorf("manifest missing %q", required)
		}
	}
	for _, forbidden := range []string{"FEED=", "CONSUMES="} {
		if bytes.Contains(got, []byte(forbidden)) {
			t.Errorf("manifest unexpectedly contains %q", forbidden)
		}
	}
}

func TestSpecUsesRegistryPortWithoutGoLiteralAndServeHonorsOverride(t *testing.T) {
	registryPort := registry.MustPort("telemetry")

	// R-V7VB-NDOX
	if got := telemetrySpec().Port; got != registryPort {
		t.Fatalf("Spec.Port = %d, want registry port %d", got, registryPort)
	}
	assertPortAbsentFromGoSources(t, registryPort)
	assertServeHonorsPortOverride(t)
}

func assertPortAbsentFromGoSources(t *testing.T, port int) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	digits := []byte(strconv.Itoa(port))
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == "project" {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(body, digits) {
			return fmt.Errorf("registry port literal found in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertServeHonorsPortOverride(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate ephemeral port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release ephemeral port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := telemetryCommand("serve")
	cmd.Env = append(cmd.Env,
		"TELEMETRY_IP=127.0.0.1",
		"TELEMETRY_PORT="+strconv.Itoa(port),
		"TELEMETRY_DB_PATH="+filepath.Join(t.TempDir(), "telemetry.db"),
		"TELEMETRY_ENABLED=false",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start telemetry: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	client := &http.Client{Timeout: time.Second}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	for {
		resp, requestErr := client.Get(url)
		if requestErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /health status = %d", resp.StatusCode)
			}
			return
		}
		select {
		case err := <-done:
			t.Fatalf("telemetry exited before serving: %v\n%s", err, stderr.String())
		case <-ctx.Done():
			t.Fatalf("telemetry did not serve overridden port: %v\n%s", ctx.Err(), stderr.String())
		default:
		}
	}
}

func telemetryCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTelemetryProcess$")
	cmd.Env = append(os.Environ(),
		helperProcessEnv+"=1",
		"TELEMETRY_TEST_ARGS="+strings.Join(args, " "),
	)
	return cmd
}
