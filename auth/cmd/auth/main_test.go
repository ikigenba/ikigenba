package main

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/server/assets"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

// TestMainWiring is the one test that builds and executes auth. It verifies
// the real process boundary, including socket activation and both signals.
func TestMainWiring(t *testing.T) {
	// R-NJQ3-EMLL R-LX6X-1N09 R-3WNI-4FXO
	// R-P02O-R3KF R-P2IH-IN1T R-P666-NY9W
	// R-TL9Z-NUB3 R-M6Y4-3SXT
	binary := buildBinary(t)
	assertStatic(t, binary)
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		body, err := assets.Files.ReadFile(name)
		if err != nil || len(body) == 0 {
			t.Fatalf("embedded %s: %v (%d bytes)", name, err, len(body))
		}
	}

	for _, tc := range []struct {
		name, wantOut, wantErr string
		wantCode               int
		args                   []string
		env                    []string
	}{
		{name: "version", args: []string{"--version"}, wantOut: version.Version + "\n"},
		{name: "manifest", args: []string{"manifest"}, wantOut: manifestFile(t)},
		{name: "bogus", args: []string{"bogus"}, wantErr: "auth: unknown command 'bogus'\n\nsee 'auth --help' for usage\n", wantCode: 2},
		{name: "bare", env: googleEnv(), wantErr: "auth: no socket was passed in\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3001 auth'\n", wantCode: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := execChild(t, binary, tc.env, tc.args...)
			if code != tc.wantCode || out != tc.wantOut || errOut != tc.wantErr {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
			}
		})
	}

	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			assertSocketActivated(t, binary, sig)
		})
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth")
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	cmd := &exec.Cmd{Path: goTool, Args: []string{"go", "build", "-o", path, "."}}
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	return path
}

func manifestFile(t *testing.T) string {
	t.Helper()
	const want = `app = "auth"
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"
`
	body, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Fatalf("manifest file differs from contract: %q", body)
	}
	return string(body)
}

func googleEnv() []string {
	return []string{
		"GOOGLE_CLIENT_ID=client-id",
		"GOOGLE_CLIENT_SECRET=client-secret",
		"WORKSPACE_DOMAIN=example.test",
	}
}

func execChild(t *testing.T, binary string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := &exec.Cmd{Path: binary, Args: append([]string{binary}, args...)}
	cmd.Dir = t.TempDir()
	cmd.Env = append([]string{}, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), childCode(t, err)
}

func childCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("child error: %v", err)
	}
	return exit.ExitCode()
}

func assertSocketActivated(t *testing.T, binary string, sig syscall.Signal) {
	t.Helper()
	shortDir, err := os.MkdirTemp("", "auth-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortDir) })
	socketPath := filepath.Join(shortDir, "auth.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	file, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	notifyPath := filepath.Join(shortDir, "notify.sock")
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = notify.Close() }()
	if err := notify.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}

	work := filepath.Join(shortDir, "work")
	if err := os.MkdirAll(filepath.Join(work, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := &exec.Cmd{Path: "/bin/sh", Args: []string{"/bin/sh", "-c", "LISTEN_PID=$$ LISTEN_FDS=1 exec \"$0\"", binary}}
	cmd.Dir = work
	cmd.Env = append(googleEnv(), "NOTIFY_SOCKET="+notifyPath)
	cmd.ExtraFiles = []*os.File{file}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	message := make([]byte, 64)
	n, _, err := notify.ReadFromUnix(message)
	if err != nil {
		t.Fatalf("read readiness: %v; stderr=%q", err, stderr.String())
	}
	if string(message[:n]) != "READY=1" {
		t.Fatalf("readiness = %q", message[:n])
	}
	dbPath := filepath.Join(work, "state", "auth.db")
	info, err := os.Stat(dbPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("database %s: %v", dbPath, err)
	}
	assertOnlyDatabaseOpen(t, cmd.Process.Pid, dbPath)

	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	code := childCode(t, cmd.Wait())
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("%s code=%d stdout=%q stderr=%q", sig, code, stdout.String(), stderr.String())
	}
	// R-NB6S-Q8EQ
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket path removed: %v", err)
	}
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "unix", socketPath)
	if err != nil {
		t.Fatalf("socket no longer accepts queued connections: %v", err)
	}
	_ = conn.Close()
}

func assertStatic(t *testing.T, path string) {
	t.Helper()
	file, err := elf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	for _, program := range file.Progs {
		if program.Type == elf.PT_INTERP {
			t.Fatal("binary has a dynamic interpreter")
		}
	}
	needed, err := file.DynString(elf.DT_NEEDED)
	if err == nil && len(needed) != 0 {
		t.Fatalf("binary needs shared libraries: %v", needed)
	}
}

func assertOnlyDatabaseOpen(t *testing.T, pid int, dbPath string) {
	t.Helper()
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, "socket:") || strings.HasPrefix(target, "pipe:") ||
			strings.HasPrefix(target, "anon_inode:") || strings.HasPrefix(target, "/dev/") ||
			strings.HasPrefix(target, "/proc/") || strings.HasPrefix(target, "/sys/") {
			continue
		}
		path := strings.TrimSuffix(target, " (deleted)")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		base := filepath.Base(path)
		if path != dbPath && base != "auth.db" && !strings.HasPrefix(base, "auth.db-") {
			t.Fatalf("unexpected open regular file %s", path)
		}
	}
}
