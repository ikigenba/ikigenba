package main

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/server"
	"github.com/ikigenba/ikigenba/auth/internal/store"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

// TestMainWiring is the one test that builds and executes auth. It verifies
// the real process boundary, including socket activation and both signals.
func TestMainWiring(t *testing.T) {
	// R-3WNI-4FXO
	// R-P02O-R3KF R-P2IH-IN1T R-P666-NY9W
	// R-STSR-5OE3 R-M6Y4-3SXT
	// R-3FKW-RNJY: this test imports the module's packages by their
	// github.com/ikigenba/ikigenba/auth/internal/... paths.
	// R-2B1J-WL7R: the serve cases run the binary bare with the Google settings
	// and a socket on descriptor 3; they prove it opens state/auth.db in its
	// working directory, draws its banner from IKIGENBA_SERVICES, and stops
	// silently with exit 0 on SIGTERM and on SIGINT.
	// R-2DHC-O4P5: while serving, the only listening socket it holds is the one
	// passed as descriptor 3.
	binary := buildBinary(t)
	assertStatic(t, binary)

	for _, tc := range []struct {
		name, wantOut, wantErr string
		wantCode               int
		args                   []string
		env                    []string
	}{
		{name: "version", args: []string{"--version"}, wantOut: version.Version + "\n"},
		{name: "manifest", args: []string{"manifest"}, wantOut: wantManifest},
		{name: "bogus", args: []string{"bogus"}, wantErr: "auth: unknown command 'bogus'\n\nsee 'auth --help' for usage\n", wantCode: 2},
		{name: "bare", env: googleEnv(), wantErr: "auth: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", wantCode: 2},
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

const wantManifest = `app = "auth"
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"
`

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
	var sessionID string
	if sig == syscall.SIGTERM {
		services := filepath.Join(shortDir, "services.json")
		if err := os.WriteFile(services, []byte(`{"services":[{"name":"auth","url":"/","icon":"","enabled":true},{"name":"Wiring probe","url":"https://probe.example.test/","icon":"","enabled":true}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd.Env = append(cmd.Env, "IKIGENBA_SERVICES="+services)
		st, err := store.Open(filepath.Join(work, "state", "auth.db"), bytes.NewReader(bytes.Repeat([]byte{0x42}, 4096)))
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		u, err := st.UpsertUserOnLogin("https://accounts.google.com", "subject", "user@example.test", now)
		if err != nil {
			t.Fatal(err)
		}
		session, err := st.CreateSession(u.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		sessionID = session.ID
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
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
	assertOnlyPassedListener(t, cmd.Process.Pid, file)
	if sessionID != "" {
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://auth/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: server.SessionCookieName, Value: sessionID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte(`popovertarget="services"`)) || !bytes.Contains(body, []byte("https://probe.example.test/")) {
			t.Fatalf("main banner source has no launcher drawn from IKIGENBA_SERVICES: %s", body)
		}
		// R-1O20-M7FN: the real executable's banner page ends its body with
		// the footer carrying the release value exported by internal/version.
		bodyContent := regexp.MustCompile(`(?s)<body\b[^>]*>(.*)</body>`).FindSubmatch(body)
		if len(bodyContent) != 2 {
			t.Fatalf("main page has no body element: %s", body)
		}
		footer := regexp.MustCompile(`(?s)<footer\b[^>]*>(.*?)</footer>[ \t\r\n\f\v]*$`).FindSubmatch(bodyContent[1])
		if len(footer) != 2 {
			t.Fatalf("main page body does not end with a footer: %s", bodyContent[1])
		}
		text := html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(string(footer[1]), ""))
		if text != "auth "+version.Version {
			t.Fatalf("main footer text = %q, want %q", text, "auth "+version.Version)
		}
	}

	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	code := childCode(t, cmd.Wait())
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("%s code=%d stdout=%q stderr=%q", sig, code, stdout.String(), stderr.String())
	}
	// R-NI60-D0O6
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

// assertOnlyPassedListener proves the child holds exactly one listening
// socket, the one the test passed it as descriptor 3: it matches the socket
// inodes in the child's descriptor table against the listening entries of the
// child's own view of /proc/net.
func assertOnlyPassedListener(t *testing.T, pid int, passed *os.File) {
	t.Helper()
	// Stat, not Fd: Fd would switch the shared socket to blocking mode.
	info, err := passed.Stat()
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("socket stat is %T", info.Sys())
	}
	passedInode := strconv.FormatUint(st.Ino, 10)
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]string{}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		if inode, ok := strings.CutPrefix(target, "socket:["); ok {
			held[strings.TrimSuffix(inode, "]")] = entry.Name()
		}
	}
	// The child may hold the passed socket under another descriptor number
	// (a dup of 3); it is the same socket, identified by its inode.
	if _, ok := held[passedInode]; !ok {
		t.Fatalf("child does not hold the passed socket %s: held=%v", passedInode, held)
	}
	listening := map[string]bool{}
	for _, table := range []struct {
		name                 string
		inodeCol, stateCol   int
		stateWant, flagsWant string
		flagsCol             int
	}{
		{name: "tcp", inodeCol: 9, stateCol: 3, stateWant: "0A", flagsCol: -1},
		{name: "tcp6", inodeCol: 9, stateCol: 3, stateWant: "0A", flagsCol: -1},
		// __SO_ACCEPTCON (0x10000) in the flags column marks a listening Unix socket.
		{name: "unix", inodeCol: 6, stateCol: -1, flagsCol: 3, flagsWant: "00010000"},
	} {
		body, err := os.ReadFile(fmt.Sprintf("/proc/%d/net/%s", pid, table.name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(body)), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) <= table.inodeCol {
				continue
			}
			if table.stateCol >= 0 && fields[table.stateCol] != table.stateWant {
				continue
			}
			if table.flagsCol >= 0 && fields[table.flagsCol] != table.flagsWant {
				continue
			}
			listening[fields[table.inodeCol]] = true
		}
	}
	if !listening[passedInode] {
		t.Fatalf("passed socket %s is not listed as listening", passedInode)
	}
	for inode, fd := range held {
		if listening[inode] && inode != passedInode {
			t.Fatalf("child holds another listening socket on descriptor %s (inode %s)", fd, inode)
		}
	}
}
