package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/home"
	"github.com/ikigenba/ikigenba/home/internal/cli"
	"github.com/ikigenba/ikigenba/home/internal/pages"
)

// R-4F8J-SIHA R-5EYJ-OKRS R-6ZWW-V2U2 R-3Y5Y-FQ3K R-40LR-79KY R-41TN-L1BN R-72CP-MMBG
func TestBinary(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "home")
	build := exec.Command("go")
	build.Args = append(build.Args, "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	commit := strings.Repeat("a1b2c3d4", 5)
	release := "integration-display"
	t.Setenv(version.CommitVariable, commit)
	t.Setenv(version.ReleaseVariable, release)
	display := version.Display()
	env := []string{version.CommitVariable + "=" + commit, version.ReleaseVariable + "=" + release}
	for _, tc := range []struct {
		args     []string
		env      []string
		code     int
		out, err string
	}{
		{[]string{"--version"}, env, 0, display + "\n", ""},
		{[]string{"--version"}, nil, 0, "\n", ""},
		{[]string{"manifest"}, env, 0, cli.Manifest, ""},
		{[]string{"--help"}, env, 0, cli.Usage, ""},
		{[]string{"bogus"}, env, 2, "", "home: unknown command 'bogus'\n\nsee 'home --help' for usage\n"},
		{nil, env, 2, "", "home: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
	} {
		cmd := &exec.Cmd{Path: binary, Args: append([]string{binary}, tc.args...)}
		cmd.Dir = t.TempDir()
		cmd.Env = append([]string{}, tc.env...)
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()
		if code := exitCode(err); code != tc.code || out.String() != tc.out || stderr.String() != tc.err {
			t.Fatalf("%v: exit %d stdout %q stderr %q", tc.args, code, out.String(), stderr.String())
		}
	}
	t.Setenv(services.Variable, "")
	var child *childProcess
	var actual string
	var banner page.Banner
	for _, configured := range []bool{true, false} {
		childEnv := env
		expectedDisplay := display
		if !configured {
			childEnv = []string{}
			t.Setenv(version.CommitVariable, "")
			t.Setenv(version.ReleaseVariable, "")
			expectedDisplay = version.Display()
		}
		child = startChild(t, binary, childEnv)
		actual = child.get(t, "/about")
		banner = page.New(pages.ServiceName, expectedDisplay).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
		banner.Trail = []page.Level{{Name: "about", URL: "/about"}}
		if want := render(t, "about", pages.AboutData{Banner: banner, Description: pages.Description}); actual != want {
			t.Fatal("binary about does not match its banner template")
		}
		child.stop(t, syscall.SIGTERM)
	}
	t.Setenv(version.CommitVariable, commit)
	t.Setenv(version.ReleaseVariable, release)

	servicePath := filepath.Join(t.TempDir(), "services.json")
	writeTiles(t, servicePath, true)
	t.Setenv(services.Variable, servicePath)
	child = startChild(t, binary, append(append([]string{}, env...), services.Variable+"="+servicePath))
	var first string
	for _, enabled := range []bool{true, false} {
		writeTiles(t, servicePath, enabled)
		actual = child.get(t, "/")
		banner = page.New(pages.ServiceName, display).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://accounts.example.test/", LogoutURL: "https://accounts.example.test/logout"})
		banner.Trail = nil
		if want := render(t, "landing", pages.LandingData{Banner: banner, Services: banner.Services}); actual != want {
			t.Fatal("binary landing does not match current services template")
		}
		if enabled {
			first = actual
		} else if actual == first {
			t.Fatal("rewriting services did not change landing")
		}
	}
	child.stop(t, syscall.SIGINT)

	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		capture := new(telemetry.Capture)
		dir := shortDir(t)
		sinkPath := filepath.Join(dir, "trail.sock")
		sinkListener, err := net.Listen("unix", sinkPath)
		if err != nil {
			t.Fatal(err)
		}
		sinkServer := &http.Server{Handler: telemetry.IngestHandler(capture), ReadHeaderTimeout: time.Second}
		done := make(chan error, 1)
		go func() { done <- sinkServer.Serve(sinkListener) }()
		t.Cleanup(func() { _ = sinkServer.Close(); <-done })
		servicesPath := filepath.Join(t.TempDir(), "services.json")
		writeServices(t, servicesPath, []map[string]any{{"name": telemetry.ServiceName, "url": "https://trail.example.test", "description": "test sink", "socket": sinkPath, "enabled": true, "mcp": false}})
		child = startChild(t, binary, append(append([]string{}, env...), services.Variable+"="+servicesPath))
		child.quiet = true
		child.stop(t, signal)
		events := capture.Events()
		if len(events) != 2 {
			t.Fatalf("lifecycle events: %v", events)
		}
		for i, e := range events {
			wantName := "service.started"
			attrs := telemetry.Attrs{"version": display}
			if i == 1 {
				wantName = "service.stopping"
				attrs = telemetry.Attrs{"reason": signalName(signal)}
			}
			if e.Name != wantName || e.Service != pages.ServiceName || e.RequestID != "" || e.User != "" || !reflect.DeepEqual(e.Attrs, attrs) {
				t.Fatalf("lifecycle event: %#v", e)
			}
		}
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return -1
}
func signalName(signal syscall.Signal) string {
	if signal == syscall.SIGINT {
		return "SIGINT"
	}
	return "SIGTERM"
}
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "home-")
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
func render(t *testing.T, name string, data any) string {
	t.Helper()
	set, err := page.Templates().ParseFS(home.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := set.ExecuteTemplate(&out, name, data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
func writeServices(t *testing.T, path string, entries []map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func writeTiles(t *testing.T, path string, enabled bool) {
	t.Helper()
	var entries []map[string]any
	for _, name := range []string{"auth", "home", "cron"} {
		url := "https://" + name + ".example.test"
		if name == "auth" {
			url = "https://accounts.example.test"
		}
		entries = append(entries, map[string]any{"name": name, "url": url, "description": "fixture " + name, "socket": "/fixture/" + name + ".sock", "enabled": name != "cron" || enabled, "mcp": false, "icon": "<svg></svg>"})
	}
	writeServices(t, path, entries)
}

type childProcess struct {
	cmd         *exec.Cmd
	listener    *net.UnixListener
	path        string
	transport   *http.Transport
	out, stderr bytes.Buffer
	waited      bool
	quiet       bool
}

func startChild(t *testing.T, binary string, env []string) *childProcess {
	t.Helper()
	dir := shortDir(t)
	path := filepath.Join(dir, "home.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	file, err := listener.File()
	if err != nil {
		t.Fatal(err)
	}
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(dir, "ready.sock"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = notify.Close() }()
	child := &childProcess{listener: listener, path: path}
	child.cmd = exec.Command("/bin/sh")
	child.cmd.Args = append(child.cmd.Args, "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary)
	child.cmd.Dir = t.TempDir()
	child.cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
	child.cmd.ExtraFiles = []*os.File{file}
	child.cmd.Stdout = &child.out
	child.cmd.Stderr = &child.stderr
	if err := child.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	t.Cleanup(func() {
		if !child.waited {
			_ = child.cmd.Process.Kill()
			_ = child.cmd.Wait()
		}
		_ = listener.Close()
		if child.transport != nil {
			child.transport.CloseIdleConnections()
		}
	})
	if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var buf [128]byte
	n, _, err := notify.ReadFromUnix(buf[:])
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if string(buf[:n]) != "READY=1" {
		t.Fatalf("readiness %q", buf[:n])
	}
	child.transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	return child
}
func (child *childProcess) get(t *testing.T, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://home.sbx.ikigenba.dev"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-User-Id", "fixture-user")
	req.Header.Set("X-User-Email", "mg@example.com")
	client := &http.Client{Transport: child.transport, Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("request %s: %s %s", path, resp.Status, body)
	}
	return string(body)
}
func (child *childProcess) stop(t *testing.T, signal syscall.Signal) {
	t.Helper()
	if err := child.cmd.Process.Signal(signal); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.cmd.Wait() }()
	select {
	case err := <-done:
		child.waited = true
		if err != nil {
			t.Fatalf("stop: %v stderr %q", err, child.stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not stop")
	}
	if child.out.Len() != 0 || (child.quiet && child.stderr.Len() != 0) {
		t.Fatalf("serve outputs stdout %q stderr %q", child.out.String(), child.stderr.String())
	}
	if _, err := os.Stat(child.path); err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("unix", child.path, time.Second)
	if err != nil {
		t.Fatal(fmt.Errorf("inherited listener stopped accepting: %w", err))
	}
	_ = connection.Close()
}
