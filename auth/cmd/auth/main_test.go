package main

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/auth/internal/version"
)

func TestMainWiring(t *testing.T) {
	// R-3GST-5FAN
	// R-3VFL-QO6Z
	// R-3WNI-4FXO
	// R-3Z3A-VZF2
	// R-P02O-R3KF
	// R-P2IH-IN1T
	// R-P666-NY9W
	buildBinary(t)

	versionOut, versionErr, versionCode := runChild(t, nil, "--version")
	if versionCode != 0 || versionErr != "" || versionOut != version.Version+"\n" {
		t.Fatalf("--version code=%d stdout=%q stderr=%q", versionCode, versionOut, versionErr)
	}

	manifestOut, manifestErr, manifestCode := runChild(t, nil, "manifest")
	if manifestCode != 0 || manifestErr != "" || !strings.HasPrefix(manifestOut, "app = \"auth\"\n") {
		t.Fatalf("manifest code=%d stdout=%q stderr=%q", manifestCode, manifestOut, manifestErr)
	}
	file, err := os.ReadFile(filepath.Join(moduleDir(t), "..", "..", "etc", "manifest.toml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if manifestOut != string(file) {
		t.Fatalf("manifest stdout differs from etc/manifest.toml")
	}

	bogusOut, bogusErr, bogusCode := runChild(t, nil, "bogus")
	if bogusCode != 2 || bogusOut != "" || bogusErr != "auth: unknown command 'bogus'\n\nsee 'auth --help' for usage\n" {
		t.Fatalf("bogus code=%d stdout=%q stderr=%q", bogusCode, bogusOut, bogusErr)
	}

	var exits []int
	var streams []string
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		port := freePort(t)
		work := t.TempDir()
		env := []string{
			"PORT=" + port,
			"GOOGLE_CLIENT_ID=client-id",
			"GOOGLE_CLIENT_SECRET=client-secret",
			"WORKSPACE_DOMAIN=example.test",
			"PATH=" + moduleDir(t) + ":/usr/bin:/bin",
		}
		t.Setenv("PATH", moduleDir(t)+":/usr/bin:/bin")
		cmd := exec.CommandContext(t.Context(), "auth")
		cmd.Dir = work
		cmd.Env = env
		if err := os.Mkdir(filepath.Join(work, "state"), 0o700); err != nil {
			t.Fatalf("mkdir state: %v", err)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
		waitReady(t, "127.0.0.1:"+port)
		if err := cmd.Process.Signal(sig); err != nil {
			t.Fatalf("signal %s: %v", sig, err)
		}
		waitErr := cmd.Wait()
		code := exitCode(t, waitErr)
		if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("%s code=%d stdout=%q stderr=%q", sig, code, stdout.String(), stderr.String())
		}
		exits = append(exits, code)
		streams = append(streams, stdout.String()+"|"+stderr.String())
	}
	if exits[0] != exits[1] || streams[0] != streams[1] {
		t.Fatalf("signals diverged exits=%v streams=%q", exits, streams)
	}
	if err := os.Remove(filepath.Join(moduleDir(t), "auth")); err != nil {
		t.Fatalf("remove binary: %v", err)
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", "auth", ".")
	cmd.Dir = moduleDir(t)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build dir=%s: %v\n%s", cmd.Dir, err, out)
	}
	built := filepath.Join(moduleDir(t), "auth")
	if _, statErr := os.Stat(built); statErr != nil {
		t.Fatalf("built binary missing in %s: %v\n%s", moduleDir(t), statErr, out)
	}
	return moduleDir(t)
}

func runChild(t *testing.T, env []string, arg string) (string, string, int) {
	t.Helper()
	t.Setenv("PATH", moduleDir(t)+":/usr/bin:/bin")
	var cmd *exec.Cmd
	switch arg {
	case "--version":
		cmd = exec.CommandContext(t.Context(), "auth", "--version")
	case "manifest":
		cmd = exec.CommandContext(t.Context(), "auth", "manifest")
	case "bogus":
		cmd = exec.CommandContext(t.Context(), "auth", "bogus")
	default:
		t.Fatalf("runChild arg = %q", arg)
	}
	childEnv := append([]string{}, env...)
	childEnv = append(childEnv, "PATH="+moduleDir(t)+":/usr/bin:/bin")
	cmd.Env = childEnv
	cmd.Dir = t.TempDir()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), exitCode(t, err)
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("exit: %v", err)
	}
	return exit.ExitCode()
}

func moduleDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Dir(file)
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return port
}

func waitReady(t *testing.T, addr string) {
	t.Helper()
	for attempt := 0; attempt < 10000; attempt++ {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return
		}
	}
	t.Fatalf("child did not listen on %s", addr)
}
