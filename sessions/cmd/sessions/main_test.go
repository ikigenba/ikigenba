package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

// TestBinary is the sole process test: all other tests use the run seam.
func TestBinary(t *testing.T) {
	// R-2VMS-PCBK R-2WUP-3429: derive identity from the child's chosen environment.
	const commit = "0123456789abcdef-dirty"
	const release = "binary-test-release"
	t.Setenv(version.CommitVariable, commit)
	t.Setenv(version.ReleaseVariable, release)
	t.Setenv(services.Variable, "")
	id := version.Read()
	env := []string{version.CommitVariable + "=" + commit, version.ReleaseVariable + "=" + release}
	root := buildBinary(t)

	// R-2Y2L-GVSY R-7G29-EVHA R-7HA5-SN7Z R-7II2-6EYO R-VIWD-BIQF.
	for _, tc := range []struct {
		arg  string
		env  []string
		out  string
		err  string
		code int
	}{
		{arg: "--version", env: env, out: id.String() + "\n"},
		{arg: "--version", env: []string{}, out: "\n"},
		{arg: "manifest", env: env, out: cli.Manifest},
		{arg: "bogus", env: env, err: "sessions: unknown command 'bogus'\n\nsee 'sessions --help' for usage\n", code: cli.ExitUsage},
		{env: env, err: "sessions: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", code: cli.ExitUsage},
	} {
		dir := binaryWorkdir(t, root)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := binaryCommand(ctx, t, tc.arg)
		cmd.Env, cmd.Dir = tc.env, dir
		var out, diagnostic bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		err := cmd.Run()
		cancel()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != tc.code || out.String() != tc.out || diagnostic.String() != tc.err {
			t.Fatalf("argument %q: code %d, stdout %q, stderr %q", tc.arg, code, out.String(), diagnostic.String())
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("command changed working directory: %v / %v", entries, err)
		}
	}

	// R-4M5A-6CED R-30IE-8FAC R-32Y6-ZYRQ: the second process reads the first's push.
	dir := binaryWorkdir(t, root)
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		capture, path := binarySink(t)
		childEnv := append(append([]string{}, env...), services.Variable+"="+path)
		child := startBinary(t, dir, childEnv)
		info, err := os.Stat(filepath.Join(dir, "state", "sessions.db"))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("database absent at readiness: %v", err)
		}
		if sig == syscall.SIGTERM {
			child.call(t, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: []tools.PushSession{{
				Tool: "codex", ID: "binary-session", Host: "binary-host", Directory: "/binary/work",
				Turns: []tools.PushTurn{{Moment: "2026-10-13T09:00:00Z", Whose: "person"}},
			}}})
		}
		result := child.call(t, tools.ToolListSessions, tools.ListSessionsArgs{})
		var answer struct {
			Structured tools.SessionList `json:"structuredContent"`
			Meta       map[string]any    `json:"_meta"`
		}
		wire, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(wire, &answer); err != nil {
			t.Fatal(err)
		}
		if len(answer.Structured.Sessions) != 1 || answer.Structured.Sessions[0].Tool != "codex" || answer.Structured.Sessions[0].ID != "binary-session" {
			t.Fatalf("held sessions: %+v", answer.Structured)
		}
		wantInfo := map[string]any{"name": pages.ServiceName, "version": id.String()}
		if !reflect.DeepEqual(answer.Meta["io.modelcontextprotocol/serverInfo"], wantInfo) {
			t.Fatalf("serverInfo: %+v", answer.Meta)
		}
		// R-VK49-PAH4: SIGINT uses the same stop boundary with its own cause.
		child.stop(t, sig)
		if child.out.Len() != 0 || child.diagnostic.Len() != 0 {
			t.Fatalf("serve output: stdout %q, stderr %q", child.out.String(), child.diagnostic.String())
		}
		events := capture.Events()
		if len(events) < 2 {
			t.Fatalf("events: %+v", events)
		}
		first, last := events[0], events[len(events)-1]
		if first.Name != "service.started" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": id.String()}) {
			t.Fatalf("first event: %+v", first)
		}
		reason := "SIGTERM"
		if sig == syscall.SIGINT {
			reason = "SIGINT"
		}
		if last.Name != "service.stopping" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": reason}) {
			t.Fatalf("last event: %+v", last)
		}
	}

	// R-31QA-M711: no services file influences the reference banner or child.
	child := startBinary(t, binaryWorkdir(t, root), env)
	req, err := http.NewRequest(http.MethodGet, "http://sessions/about", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "sessions.example.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-User-Id", "binary-user")
	req.Header.Set("X-User-Email", "mg@example.com")
	resp, err := child.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	templates, err := page.Templates().ParseFS(sessions.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	auth := pages.Auth(req, "")
	banner := page.New(pages.ServiceName, id).Banner(page.User{Email: "mg@example.com", ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
	banner.Trail = []page.Level{pages.AboutLevel}
	var expected bytes.Buffer
	if err := templates.ExecuteTemplate(&expected, "about", pages.AboutData{Banner: banner, Description: pages.Description}); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, expected.Bytes()) {
		t.Fatalf("about response: status %d; body differs from identity-bearing template", resp.StatusCode)
	}
	child.stop(t, syscall.SIGTERM)
}

func buildBinary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	tree, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tree.Close() }()
	file, err := tree.OpenFile("sessions", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// The compiler writes its product directly to the test-owned executable file.
	cmd := exec.CommandContext(ctx, "go", "build", "-o", "/dev/stdout", ".")
	var diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = file, &diagnostic
	if err := cmd.Run(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, diagnostic.String())
	}
	if err := file.Chmod(0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func binaryWorkdir(t *testing.T, root string) string {
	t.Helper()
	// Every invocation starts in its own directory beside the fixed executable.
	dir, err := os.MkdirTemp(root, "work-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func binaryCommand(ctx context.Context, t *testing.T, arg string) *exec.Cmd {
	t.Helper()
	switch arg {
	case "--version":
		return exec.CommandContext(ctx, "../sessions", "--version")
	case "manifest":
		return exec.CommandContext(ctx, "../sessions", "manifest")
	case "bogus":
		return exec.CommandContext(ctx, "../sessions", "bogus")
	case "":
		return exec.CommandContext(ctx, "../sessions")
	default:
		t.Fatalf("unknown test command %q", arg)
		return nil
	}
}

type binaryChild struct {
	cmd             *exec.Cmd
	out, diagnostic bytes.Buffer
	done            chan error
	finished        bool
	http            *http.Client
	mcp             *mcp.Client
}

func shortBinaryDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sessions-binary-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func startBinary(t *testing.T, dir string, env []string) *binaryChild {
	t.Helper()
	sockets := shortBinaryDir(t)
	socket := filepath.Join(sockets, "http")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	defer func() { _ = ln.Close() }()
	file, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(sockets, "notify"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = notify.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, "../sessions")
	cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
	cmd.Dir, cmd.ExtraFiles = dir, []*os.File{file}
	child := &binaryChild{cmd: cmd, done: make(chan error, 1)}
	cmd.Stdout, cmd.Stderr = &child.out, &child.diagnostic
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	go func() { child.done <- cmd.Wait() }()
	transport := telemetry.SocketTransport(socket)
	child.http = &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	child.mcp = mcp.NewClient(mcp.ClientConfig{Endpoint: "http://sessions/mcp", HTTPClient: child.http, Name: "binary-test"})
	t.Cleanup(func() {
		cancel()
		if !child.finished {
			_ = cmd.Process.Kill()
			<-child.done
			child.finished = true
		}
		transport.CloseIdleConnections()
	})
	if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message [128]byte
	n, _, err := notify.ReadFromUnix(message[:])
	if err != nil || string(message[:n]) != "READY=1" {
		t.Fatalf("readiness: %q / %v", message[:n], err)
	}
	return child
}

func (c *binaryChild) stop(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := c.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-c.done:
		c.finished = true
		if err != nil {
			t.Fatalf("stop: %v; stdout %q, stderr %q", err, c.out.String(), c.diagnostic.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("binary did not stop")
	}
}

func (c *binaryChild) call(t *testing.T, name string, args any) mcp.Result {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.mcp.CallTool(context.Background(), identity.Caller{UserID: "binary-user", Email: "mg@example.com"}, name, b)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError() {
		t.Fatalf("%s refused: %+v", name, result)
	}
	return result
}

func binarySink(t *testing.T) (*telemetry.Capture, string) {
	t.Helper()
	socket := filepath.Join(shortBinaryDir(t), "telemetry")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	capture := new(telemetry.Capture)
	mux := http.NewServeMux()
	mux.Handle(telemetry.IngestPath, telemetry.IngestHandler(capture))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ln) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	})
	path := filepath.Join(t.TempDir(), "services.json")
	b, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": telemetry.ServiceName, "socket": socket, "url": "http://telemetry.test", "description": "binary telemetry capture", "enabled": true, "mcp": false}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return capture, path
}
