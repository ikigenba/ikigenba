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
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/dummy/internal/cli"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

// R-Z2CP-8T9E R-Z3KL-ML03 R-Z4SI-0CQS
func TestMainWiring(t *testing.T) {
	root := mainProjectRoot(t)
	binary := filepath.Join(t.TempDir(), "dummy")
	build := exec.Command("go")
	build.Args = []string{"go", "build", "-o", binary, "./cmd/dummy"}
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build dummy: %v\n%s", err, output)
	}

	for _, test := range []struct {
		name   string
		args   []string
		exit   int
		stdout string
		stderr string
	}{
		{name: "version", args: []string{"--version"}, exit: cli.ExitSuccess, stdout: cli.Version + "\n"},
		{name: "invalid command", args: []string{"bogus"}, exit: cli.ExitUsage, stderr: "dummy: unknown command 'bogus'\n\nsee 'dummy --help' for usage\n"},
		{name: "bare without socket", exit: cli.ExitUsage, stderr: "dummy: no socket was passed in\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3000 dummy'\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, exit := runBinary(t, binary, test.args)
			if exit != test.exit || stdout != test.stdout || stderr != test.stderr {
				t.Errorf("exit=%d stdout=%q stderr=%q; want exit=%d stdout=%q stderr=%q", exit, stdout, stderr, test.exit, test.stdout, test.stderr)
			}
		})
	}

	for _, sig := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		t.Run(sig.String(), func(t *testing.T) {
			serveAndSignal(t, binary, sig)
		})
	}
}

// launcherButton matches a button start tag whose class is launcher.
var launcherButton = regexp.MustCompile(`<button\s[^>]*\bclass="launcher"[^>]*>`)

func mainProjectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func runBinary(t *testing.T, binary string, args []string) (string, string, int) {
	t.Helper()
	commandArgs := append([]string{binary}, args...)
	command := &exec.Cmd{Path: binary, Args: commandArgs}
	command.Env = []string{}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("run dummy: %v", err)
	}
	return stdout.String(), stderr.String(), exitError.ExitCode()
}

// R-Z60E-E4HH R-Z78A-RW86 R-7J0U-GFGT
func serveAndSignal(t *testing.T, binary string, sig os.Signal) {
	t.Helper()
	directory, err := os.MkdirTemp("", "dummy-exec-")
	if err != nil {
		t.Fatalf("create short socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	socketPath := filepath.Join(directory, "serve.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close parent listener: %v", err)
		}
	}()
	listener.SetUnlinkOnClose(false)
	passedFile, err := listener.File()
	if err != nil {
		t.Fatalf("duplicate socket for child: %v", err)
	}
	defer func() {
		if err := passedFile.Close(); err != nil {
			t.Errorf("close passed socket file: %v", err)
		}
	}()
	decoyFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open non-listener for descriptor 4: %v", err)
	}
	defer func() {
		if err := decoyFile.Close(); err != nil {
			t.Errorf("close descriptor 4: %v", err)
		}
	}()

	notifyPath := filepath.Join(directory, "notify.sock")
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listen for readiness: %v", err)
	}
	defer func() {
		if err := notify.Close(); err != nil {
			t.Errorf("close readiness listener: %v", err)
		}
	}()
	if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set readiness deadline: %v", err)
	}

	command := exec.Command("/bin/sh")
	command.Args = []string{"/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary}
	command.Env = []string{"NOTIFY_SOCKET=" + notifyPath}
	if sig == syscall.SIGTERM {
		servicesPath := filepath.Join(directory, "services.json")
		if err := os.WriteFile(servicesPath, []byte(`{"services":[{"name":"dummy","url":"/widgets","icon":"","enabled":true}]}`), 0600); err != nil {
			t.Fatal(err)
		}
		command.Env = append(command.Env, "IKIGENBA_SERVICES="+servicesPath)
	}
	// Only descriptor 3 is a listener. Readiness proves the child took it.
	command.ExtraFiles = []*os.File{passedFile, decoyFile}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start serve case: %v", err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()

	var datagram [32]byte
	n, _, err := notify.ReadFromUnix(datagram[:])
	if err != nil {
		t.Fatalf("wait for readiness: %v", err)
	}
	if got := string(datagram[:n]); got != "READY=1" {
		t.Fatalf("readiness = %q, want READY=1", got)
	}
	{
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://dummy/widgets", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-User-Id", "test-user")
		req.Header.Set("X-User-Email", "user@example.test")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read child response: %v, close: %v", err, closeErr)
		}
		if sig == syscall.SIGTERM && !launcherButton.Match(body) {
			t.Error("main did not pass appkit banner source to handler")
		}
		assertVersionFooter(t, string(body))
	}
	if err := command.Process.Signal(sig); err != nil {
		t.Fatalf("send %v: %v", sig, err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case err = <-waited:
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		err = <-waited
		finished = true
		t.Fatalf("serve case for %v did not exit after signal: %v", sig, err)
	}
	finished = true
	if err != nil {
		t.Errorf("serve case for %v exited with error: %v", sig, err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("serve case for %v: stdout=%q stderr=%q", sig, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Errorf("socket path after child exit: %v", err)
	}
	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Errorf("socket no longer accepts queued connections: %v", err)
	} else if err := connection.Close(); err != nil {
		t.Errorf("close queued connection: %v", err)
	}
}

func assertVersionFooter(t *testing.T, body string) {
	t.Helper()
	starts := elementStart("footer").FindAllStringIndex(body, -1)
	if len(starts) != 1 {
		t.Fatalf("whole response contains %d footer start tags, want 1", len(starts))
	}
	content := body[starts[0][1]:]
	end := elementEnd("footer").FindStringIndex(content)
	if end == nil {
		t.Fatal("footer has no following end tag")
	}
	got := normaliseFooterContent(content[:end[0]])
	if want := panel.ServiceName + " " + cli.Version; got != want {
		t.Errorf("normalised footer = %q, want %q", got, want)
	}
}

func elementStart(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)<` + name + `(?:[^a-z0-9>][^>]*|)>`)
}

func elementEnd(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)</` + name + `(?:[^a-z0-9>][^>]*|)>`)
}

func normaliseFooterContent(content string) string {
	blocks := regexp.MustCompile(`(?i)<(script|style)(?:[^a-z0-9>][^>]*|)>`)
	for {
		start := blocks.FindStringSubmatchIndex(content)
		if start == nil {
			break
		}
		end := elementEnd(content[start[2]:start[3]]).FindStringIndex(content[start[1]:])
		if end == nil {
			content = content[:start[0]]
			break
		}
		content = content[:start[0]] + content[start[1]+end[1]:]
	}
	for {
		start := strings.IndexByte(content, '<')
		if start == -1 {
			break
		}
		end := strings.IndexByte(content[start:], '>')
		if end == -1 {
			content = content[:start]
			break
		}
		content = content[:start] + content[start+end+1:]
	}
	return strings.Join(strings.Fields(html.UnescapeString(content)), " ")
}
