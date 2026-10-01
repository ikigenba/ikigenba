package main

import (
	"bytes"
	"context"
	"errors"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	// R-M6Y4-3SXT
	// R-3FKW-RNJY: this test imports the module's packages by their
	// github.com/ikigenba/ikigenba/auth/internal/... paths.
	// R-2B1J-WL7R: the serve cases run the binary bare with the Google settings
	// and a socket on descriptor 3; they prove it opens state/auth.db in its
	// working directory, draws its banner from IKIGENBA_SERVICES, and stops
	// silently with exit 0 on SIGTERM and on SIGINT.
	binary := buildBinary(t)

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
	cmd := &exec.Cmd{Path: goTool, Args: []string{"go", "build", "-o", path, "./cmd/auth"}, Dir: "../.."}
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
	// The first run starts with only state/auth.db; the second starts with
	// an empty state directory. Services and sockets are outside work.
	assertRuntimeFiles(t, work, sig == syscall.SIGTERM, false)
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
	if sessionID != "" {
		// R-FLRS-9LZN: main leaves Inherit nil; the response comes from
		// the listening socket supplied as descriptor 3.
		// R-9ZGQ-O2GR: the cgo-free executable serves the live session from
		// a working directory containing only state/auth.db.
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
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
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
	// R-A0ON-1U7G: after either signal, only the database and its named
	// SQLite auxiliary files remain in the working directory.
	assertRuntimeFiles(t, work, true, true)
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

// assertRuntimeFiles checks only the child-owned runtime fixture. It never
// reads the checkout or inspects process descriptors.
func assertRuntimeFiles(t *testing.T, work string, wantDatabase, allowAuxiliary bool) {
	t.Helper()
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state" || !entries[0].IsDir() {
		t.Fatalf("working directory must contain only state/: %v", entries)
	}
	entries, err = os.ReadDir(filepath.Join(work, "state"))
	if err != nil {
		t.Fatal(err)
	}
	foundDatabase := false
	for _, entry := range entries {
		switch entry.Name() {
		case "auth.db":
			foundDatabase = true
		case "auth.db-journal", "auth.db-wal", "auth.db-shm":
			if !allowAuxiliary {
				t.Fatalf("unexpected initial auxiliary file state/%s", entry.Name())
			}
		default:
			t.Fatalf("unexpected runtime file state/%s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("state/%s is not a regular file", entry.Name())
		}
	}
	if foundDatabase != wantDatabase {
		t.Fatalf("database exists = %t, want %t", foundDatabase, wantDatabase)
	}
	if !wantDatabase && len(entries) != 0 {
		t.Fatalf("initial state directory must be empty: %v", entries)
	}
}
