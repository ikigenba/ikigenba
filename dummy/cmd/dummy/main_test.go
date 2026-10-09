package main

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
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/dummy/internal/cli"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

// R-M1JO-5KOP R-M2RK-JCFE R-K1I1-7X3J R-M6F9-ONNH R-M7N6-2FE6 R-M8V2-G74V R-K7LJ-4RT0 R-MA2Y-TYVK
func TestMainWiring(t *testing.T) {
	commit, release := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", "test-release"
	t.Setenv(version.CommitVariable, commit)
	t.Setenv(version.ReleaseVariable, release)
	display := version.Display()
	env := []string{version.CommitVariable + "=" + commit, version.ReleaseVariable + "=" + release}
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
		env    []string
	}{
		{name: "version", args: []string{"--version"}, exit: cli.ExitSuccess, stdout: display + "\n", env: env},
		{name: "version without identity", args: []string{"--version"}, exit: cli.ExitSuccess, stdout: "\n"},
		{name: "invalid command", args: []string{"bogus"}, exit: cli.ExitUsage, stderr: "dummy: unknown command 'bogus'\n\nsee 'dummy --help' for usage\n"},
		{name: "bare without socket", exit: cli.ExitUsage, stderr: "dummy: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, exit := runBinary(t, binary, test.args, test.env)
			if exit != test.exit || stdout != test.stdout || stderr != test.stderr {
				t.Errorf("exit=%d stdout=%q stderr=%q; want exit=%d stdout=%q stderr=%q", exit, stdout, stderr, test.exit, test.stdout, test.stderr)
			}
		})
	}

	for _, sig := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		t.Run(sig.String(), func(t *testing.T) {
			serveAndSignal(t, binary, sig, env, display)
		})
	}
}

func mainProjectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func runBinary(t *testing.T, binary string, args, env []string) (string, string, int) {
	t.Helper()
	commandArgs := append([]string{binary}, args...)
	command := &exec.Cmd{Path: binary, Args: commandArgs}
	command.Env = append([]string{}, env...)
	command.Dir = t.TempDir()
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

// R-DPQ2-9T5Q R-5ZTA-PV8G
func serveAndSignal(t *testing.T, binary string, sig os.Signal, env []string, display string) {
	t.Helper()
	directory, err := os.MkdirTemp("", "dummy-exec-")
	if err != nil {
		t.Fatalf("create short socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	capture := &telemetry.Capture{}
	ingestSocket := filepath.Join(directory, "telemetry.sock")
	ingestListener, err := net.Listen("unix", ingestSocket)
	if err != nil {
		t.Fatal(err)
	}
	ingest := &http.Server{Handler: telemetry.IngestHandler(capture), ReadHeaderTimeout: time.Second}
	go func() { _ = ingest.Serve(ingestListener) }()
	t.Cleanup(func() { _ = ingest.Close() })
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
	command.Dir = t.TempDir()
	command.Args = []string{"/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary}
	servicesPath := filepath.Join(directory, "services.json")
	command.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notifyPath)
	if sig == syscall.SIGTERM {
		writeServices(t, servicesPath, "First description", ingestSocket)
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
		servicesValue := ""
		if sig == syscall.SIGTERM {
			servicesValue = servicesPath
		}
		t.Setenv(services.Variable, servicesValue)
		banner := page.New(panel.ServiceName, display).Banner(page.User{Email: "user@example.test", ProfileURL: panel.ProfileURL(req.Host, ""), LogoutURL: panel.LogoutURL(req.Host, "")})
		if sig == syscall.SIGTERM && string(banner.Icon) != mainServiceIcon {
			t.Fatalf("banner icon = %q, want services file icon %q", banner.Icon, mainServiceIcon)
		}
		assertAppkitFrame(t, string(body), banner)
		assertMCPWiring(t, client, display)
		if sig == syscall.SIGTERM {
			assertDiscovery(t, client, "First description", true)
			writeServices(t, servicesPath, "Second description", ingestSocket)
			assertDiscovery(t, client, "Second description", true)
		} else {
			assertDiscovery(t, client, "", false)
		}
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
	if stdout.Len() != 0 {
		t.Errorf("stdout=%q", stdout.String())
	}
	reason := "SIGTERM"
	if sig == os.Interrupt {
		reason = "SIGINT"
	}
	if sig == syscall.SIGTERM {
		if stderr.Len() != 0 {
			t.Errorf("stderr=%q", stderr.String())
		}
		events := capture.Events()
		if len(events) < 2 {
			t.Fatalf("events=%v", events)
		}
		first, last := events[0], events[len(events)-1]
		if first.Name != "service.started" || first.Service != panel.ServiceName || first.RequestID != "" || first.User != "" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": display}) {
			t.Errorf("first event=%+v", first)
		}
		if last.Name != "service.stopping" || last.Attrs["reason"] != reason {
			t.Errorf("last event=%+v", last)
		}
		found := false
		for _, e := range events {
			if e.Name == "tool.called" && e.RequestID == "binary-tool-request" && e.User == "test-user" && e.Attrs["tool"] == "list_widgets" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing tool.called: %+v", events)
		}
	} else {
		lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
		if len(lines) < 2 {
			t.Fatalf("stderr=%q", stderr.String())
		}
		var events []map[string]any
		for _, line := range lines {
			prefix := panel.ServiceName + ": undelivered event: "
			if !strings.HasPrefix(line, prefix) {
				t.Fatalf("unexpected line %q", line)
			}
			var event map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
		if events[0]["event"] != "service.started" || events[len(events)-1]["event"] != "service.stopping" {
			t.Errorf("events=%v", events)
		}
		attrs, _ := events[len(events)-1]["attrs"].(map[string]any)
		if attrs["reason"] != reason {
			t.Errorf("stop attrs=%v", attrs)
		}
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
	info, err := os.Stat(filepath.Join(command.Dir, "state", "dummy.db"))
	if err != nil || !info.Mode().IsRegular() {
		t.Errorf("database absent: %v", err)
	}
}

// R-VN0V-44AG
func assertAppkitFrame(t *testing.T, body string, banner page.Banner) {
	t.Helper()
	for _, name := range []string{"banner", "footer"} {
		var expected bytes.Buffer
		if err := page.Templates().ExecuteTemplate(&expected, name, banner); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, expected.String()) {
			t.Errorf("body missing rendered %s", name)
		}
	}
}

const mainServiceIcon = `<svg viewBox="0 0 24 24"><path d="M4 4h16v16H4z"/></svg>`

func writeServices(t *testing.T, path, description, telemetrySocket string) {
	t.Helper()
	entry := map[string]any{"name": panel.ServiceName, "url": "/widgets", "description": description, "socket": "dummy.sock", "enabled": true, "mcp": true, "icon": mainServiceIcon}
	data, err := json.Marshal(map[string]any{"services": []any{entry, map[string]any{"name": telemetry.ServiceName, "socket": telemetrySocket, "url": "", "description": "", "enabled": true, "mcp": false}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".new", data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
}

// R-M57D-AVWS
func assertMCPWiring(t *testing.T, httpClient *http.Client, display string) {
	t.Helper()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://dummy/mcp", HTTPClient: httpClient, Name: "test", Version: "test"})
	result, err := client.CallTool(context.Background(), identity.Caller{UserID: "test-user", RequestID: "binary-tool-request"}, "list_widgets", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError() {
		t.Fatal("list_widgets returned a tool error")
	}
	data, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(fields["_meta"], &meta); err != nil {
		t.Fatal(err)
	}
	var info map[string]string
	if err := json.Unmarshal(meta["io.modelcontextprotocol/serverInfo"], &info); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info, map[string]string{"name": panel.ServiceName, "version": display}) {
		t.Errorf("serverInfo=%v", info)
	}
	var resultBody struct {
		StructuredContent struct {
			Widgets []struct{ ID, Name string } `json:"widgets"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(data, &resultBody); err != nil {
		t.Fatal(err)
	}
	if resultBody.StructuredContent.Widgets == nil || len(resultBody.StructuredContent.Widgets) != 0 {
		t.Errorf("fresh widgets=%+v", resultBody)
	}
}

// R-E3SQ-JFOR R-E68J-AZ65
func assertDiscovery(t *testing.T, client *http.Client, instructions string, present bool) {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "server/discover", "params": map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": mcp.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://dummy/mcp", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-User-Id", "test-user")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
	request.Header.Set("Mcp-Method", "server/discover")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("discovery status=%d", response.StatusCode)
	}
	var decoded struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	value, ok := decoded.Result["instructions"]
	if ok != present {
		t.Fatalf("instructions presence=%t want %t", ok, present)
	}
	if present {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			t.Fatal(err)
		}
		if text != instructions {
			t.Errorf("instructions=%q want %q", text, instructions)
		}
	}
}
