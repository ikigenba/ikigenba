package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/cli"
	"github.com/ikigenba/ikigenba/auth/internal/server/assets"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

func TestMainWiring(t *testing.T) {
	// R-3Z3A-VZF2
	// R-3VFL-QO6Z
	// R-3WNI-4FXO
	// R-P02O-R3KF
	// R-P2IH-IN1T
	// R-P666-NY9W
	// R-YNFB-36HN
	binary := buildBinary(t)
	assertStaticBinary(t, binary)
	// Sources are overwritten only after the binary is built, and before it
	// starts. An init-time read of those paths would cache the mutated bytes.
	embedded := embedBytesThenMutateSources(t)

	versionOut, versionErr, versionCode := runChild(t, nil, "--version")
	if versionCode != 0 || versionErr != "" || versionOut != version.Version+"\n" {
		t.Fatalf("--version code=%d stdout=%q stderr=%q", versionCode, versionOut, versionErr)
	}
	assertChildMatchesRun(t, []string{"--version"}, nil, versionOut, versionErr, versionCode)

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

	assertChildMatchesRun(t, []string{"manifest"}, nil, manifestOut, manifestErr, manifestCode)

	bogusOut, bogusErr, bogusCode := runChild(t, nil, "bogus")
	if bogusCode != 2 || bogusOut != "" || bogusErr != "auth: unknown command 'bogus'\n\nsee 'auth --help' for usage\n" {
		t.Fatalf("bogus code=%d stdout=%q stderr=%q", bogusCode, bogusOut, bogusErr)
	}
	assertChildMatchesRun(t, []string{"bogus"}, nil, bogusOut, bogusErr, bogusCode)

	unsetOut, unsetErr, unsetCode := execAuth(t, "", nil)
	assertChildMatchesRun(t, nil, map[string]string{}, unsetOut, unsetErr, unsetCode)
	if unsetCode != 2 || unsetOut != "" || unsetErr != "auth: PORT is not set\n" {
		t.Fatalf("unset PORT code=%d stdout=%q stderr=%q", unsetCode, unsetOut, unsetErr)
	}
	assertOpenFailureMatchesRun(t)

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
		dbPath := filepath.Join(work, "state", "auth.db")
		info, err := os.Stat(dbPath)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("database %s: %v", dbPath, err)
		}
		assertServedEmbeddedAssets(t, "http://127.0.0.1:"+port, embedded)
		assertOnlyDatabaseOpen(t, cmd.Process.Pid, dbPath)
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
	cmd.Env = append(withoutEnv(os.Environ(), "CGO_ENABLED"), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build dir=%s: %v\n%s", cmd.Dir, err, out)
	}
	built := filepath.Join(moduleDir(t), "auth")
	if _, statErr := os.Stat(built); statErr != nil {
		t.Fatalf("built binary missing in %s: %v\n%s", moduleDir(t), statErr, out)
	}
	return filepath.Join(moduleDir(t), "auth")
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

func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}

func execAuth(t *testing.T, dir string, env []string) (string, string, int) {
	t.Helper()
	t.Setenv("PATH", moduleDir(t)+":/usr/bin:/bin")
	cmd := exec.CommandContext(t.Context(), "auth")
	cmd.Env = append(append([]string{}, env...), "PATH="+moduleDir(t)+":/usr/bin:/bin")
	if dir == "" {
		dir = t.TempDir()
	}
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), exitCode(t, err)
}

func assertChildMatchesRun(t *testing.T, args []string, env map[string]string, stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	got := cli.Run(cli.Process{
		Args: args,
		Getenv: func(name string) string {
			if env == nil {
				return ""
			}
			return env[name]
		},
		Stdout:     &out,
		Stderr:     &errBuf,
		Now:        time.Now,
		Rand:       rand.Reader,
		OIDCIssuer: "https://accounts.google.com",
		DBSource:   "state/auth.db",
	})
	if got != code || out.String() != stdout || errBuf.String() != stderr {
		t.Fatalf("child status is not cli.Run's for %v\nchild %d stdout=%q stderr=%q\nrun %d stdout=%q stderr=%q", args, code, stdout, stderr, got, out.String(), errBuf.String())
	}
}

func assertOpenFailureMatchesRun(t *testing.T) {
	t.Helper()
	t.Run("open failure", func(t *testing.T) {
		work := t.TempDir()
		if err := os.MkdirAll(filepath.Join(work, "state", "auth.db"), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		t.Chdir(work)
		env := map[string]string{
			"PORT":                 "80",
			"GOOGLE_CLIENT_ID":     "id",
			"GOOGLE_CLIENT_SECRET": "secret",
			"WORKSPACE_DOMAIN":     "example.test",
		}
		var out, errBuf bytes.Buffer
		code := cli.Run(cli.Process{
			Getenv:     func(name string) string { return env[name] },
			Stdout:     &out,
			Stderr:     &errBuf,
			Now:        time.Now,
			Rand:       rand.Reader,
			OIDCIssuer: "https://accounts.google.com",
			DBSource:   "state/auth.db",
		})
		childEnv := []string{
			"PORT=80",
			"GOOGLE_CLIENT_ID=id",
			"GOOGLE_CLIENT_SECRET=secret",
			"WORKSPACE_DOMAIN=example.test",
		}
		gotOut, gotErr, gotCode := execAuth(t, work, childEnv)
		if code != 1 || gotCode != code || gotOut != out.String() || gotErr != errBuf.String() || !strings.Contains(gotErr, "state/auth.db") {
			t.Fatalf("open failure child %d %q %q run %d %q %q", gotCode, gotOut, gotErr, code, out.String(), errBuf.String())
		}
	})
}

func assertStaticBinary(t *testing.T, path string) {
	t.Helper()
	file, err := elf.Open(path)
	if err != nil {
		t.Fatalf("elf: %v", err)
	}
	defer func() { _ = file.Close() }()
	for _, prog := range file.Progs {
		if prog.Type == elf.PT_INTERP {
			t.Fatal("release binary has a dynamic interpreter")
		}
	}
	needed, err := file.DynString(elf.DT_NEEDED)
	if err != nil {
		return
	}
	if len(needed) != 0 {
		t.Fatalf("release binary needs shared libraries %v", needed)
	}
}

func embedBytesThenMutateSources(t *testing.T) map[string][]byte {
	t.Helper()
	dir := filepath.Join(moduleDir(t), "..", "..", "internal", "server", "assets")
	names := []string{"index.html", "app.js", "style.css"}
	embedded := make(map[string][]byte, len(names))
	original := make(map[string][]byte, len(names))
	mode := make(map[string]os.FileMode, len(names))
	for _, name := range names {
		body, err := assets.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		path := filepath.Join(dir, name)
		disk, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(disk, body) {
			t.Fatalf("source %s does not match the compiled embed", name)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		embedded[name] = append([]byte(nil), body...)
		original[name] = disk
		mode[name] = info.Mode().Perm()
	}
	t.Cleanup(func() {
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(dir, name), original[name], mode[name]); err != nil {
				t.Errorf("restore %s: %v", name, err)
			}
		}
	})
	marker := []byte("\nnot-the-embedded-asset\n")
	for _, name := range names {
		mutated := append(append([]byte(nil), original[name]...), marker...)
		if err := os.WriteFile(filepath.Join(dir, name), mutated, mode[name]); err != nil {
			t.Fatalf("mutate %s: %v", name, err)
		}
	}
	return embedded
}

func assertServedEmbeddedAssets(t *testing.T, base string, embedded map[string][]byte) {
	t.Helper()
	dir := filepath.Join(moduleDir(t), "..", "..", "internal", "server", "assets")
	wantType := map[string]string{
		"index.html": "text/html; charset=utf-8",
		"app.js":     "text/javascript; charset=utf-8",
		"style.css":  "text/css; charset=utf-8",
	}
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		onDisk, err := os.ReadFile(filepath.Clean(filepath.Join(dir, name)))
		if err != nil {
			t.Fatalf("source %s: %v", name, err)
		}
		if bytes.Equal(onDisk, embedded[name]) {
			t.Fatalf("source %s matches the embed while the server is running", name)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/assets/"+name, nil)
		if err != nil {
			cancel()
			t.Fatalf("request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			t.Fatalf("GET %s: %v", name, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != wantType[name] || !bytes.Equal(body, embedded[name]) {
			t.Fatalf("served %s status=%d type=%q (%d bytes)", name, resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
		}
		if bytes.Equal(body, onDisk) {
			t.Fatalf("served %s matches the mutated source, not the embed", name)
		}
	}
}

func assertOnlyDatabaseOpen(t *testing.T, pid int, dbPath string) {
	t.Helper()
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		t.Fatalf("fd: %v", err)
	}
	var regular []string
	var all []string
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(fmt.Sprintf("/proc/%d/fd", pid), entry.Name()))
		if err != nil {
			continue
		}
		all = append(all, target)
		if strings.HasPrefix(target, "socket:") || strings.HasPrefix(target, "pipe:") || strings.HasPrefix(target, "anon_inode:") || strings.HasPrefix(target, "/dev/") || strings.HasPrefix(target, "/proc/") || strings.HasPrefix(target, "/sys/") {
			continue
		}
		path := strings.TrimSuffix(target, " (deleted)")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		base := filepath.Base(path)
		if path != dbPath && base != "auth.db" && !strings.HasPrefix(base, "auth.db-") {
			regular = append(regular, path)
		}
	}
	if len(regular) != 0 {
		t.Fatalf("open files other than the database: %v (all %v)", regular, all)
	}
	seenDB := false
	for _, target := range all {
		if strings.TrimSuffix(target, " (deleted)") == dbPath {
			seenDB = true
		}
	}
	if !seenDB {
		t.Fatalf("database %s is not open (fds %v)", dbPath, all)
	}
}
