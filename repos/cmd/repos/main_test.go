package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	busevents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

const binaryReposIcon = `<svg viewBox="0 0 24 24"><path d="M4 4h16v16H4z"/></svg>`

// TestBinary is the sole build/exec/signal test; runtime children receive only
// the explicitly composed environment below. The build tool uses its normal
// installed toolchain/cache environment without reading it in the test.
func TestBinary(t *testing.T) {
	// R-KDXP-VNUC: expected display is computed through the published API.
	t.Setenv(version.CommitVariable, "0123456789abcdef0123456789abcdef01234567")
	t.Setenv(version.ReleaseVariable, "release fixture")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	env := []string{"PATH=" + filepath.Dir(git), "HOME=" + work, "XDG_CONFIG_HOME=" + work,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
		version.CommitVariable + "=0123456789abcdef0123456789abcdef01234567", version.ReleaseVariable + "=release fixture",
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	binary := filepath.Join(work, "repos")
	buildCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}

	// R-KF5M-9FL1: command output and exits agree with the public run seam.
	for _, args := range [][]string{{"--version"}, {"manifest"}, {"bogus"}, nil} {
		var wantOut, wantErr bytes.Buffer
		wantCode := cli.Run(context.Background(), cli.Process{Args: args, LookupEnv: func(string) (string, bool) { return "", false }, Stdout: &wantOut, Stderr: &wantErr, Version: version.Display()})
		ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir, cmd.Env = work, env
		var out, diagnostic bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		err := cmd.Run()
		done()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != wantCode || out.String() != wantOut.String() || diagnostic.String() != wantErr.String() {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q; want %d %q %q", args, code, out.String(), diagnostic.String(), wantCode, wantOut.String(), wantErr.String())
		}
	}
	// R-KF5M-9FL1: absent display variables produce exactly one empty line.
	bareEnv := []string{}
	for _, entry := range env {
		if !strings.HasPrefix(entry, version.CommitVariable+"=") && !strings.HasPrefix(entry, version.ReleaseVariable+"=") {
			bareEnv = append(bareEnv, entry)
		}
	}
	noDisplay := exec.CommandContext(t.Context(), binary, "--version")
	noDisplay.Dir, noDisplay.Env = work, bareEnv
	if output, err := noDisplay.CombinedOutput(); err != nil || string(output) != "\n" {
		t.Fatalf("unset display: %q %v", output, err)
	}
	// A notification that cannot be sent never starts the
	// lifecycle. Starting it before notification would flush a fallback here.
	t.Run("notification-failure-before-started", func(t *testing.T) {
		binaryNotifyFailure(t, binary, env)
	})

	state := t.TempDir()
	first := startBinary(t, binary, state, env)
	// R-RWXX-YP7R: readiness precedes the default state layout and creation.
	for _, name := range []string{"state", "state/repos", "state/repos.db"} {
		info, err := os.Stat(filepath.Join(state, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "state/repos.db" {
			if !info.Mode().IsRegular() {
				t.Fatal("catalog is not regular")
			}
		} else if !info.IsDir() {
			t.Fatalf("%s is not a directory", name)
		}
	}
	entries, err := os.ReadDir(filepath.Join(state, "state/repos"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("initial repositories: %v %v", entries, err)
	}
	first.call(t, "create", json.RawMessage(`{"name":"alpha"}`))
	// R-S49C-9BNX: main wires smart HTTP to the persisted repository.
	response, _ := first.request(t, http.MethodGet, "/alpha.git/info/refs?service=git-upload-pack", nil)
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/x-git-upload-pack-advertisement" {
		t.Fatalf("advertisement: %d %v", response.StatusCode, response.Header)
	}
	// R-U235-0IK3: the page carries the injected display string.
	_, bodyWithoutServices := first.request(t, http.MethodGet, "/", nil)
	if !strings.Contains(bodyWithoutServices, version.Display()) {
		t.Fatal("page lacks display string")
	}
	// R-S1TJ-HS6J: absent services produce no discovery instructions.
	if _, found := first.discover(t)["instructions"]; found {
		t.Fatal("instructions without services")
	}
	first.stop(t, syscall.SIGTERM, false)
	second := startBinary(t, binary, state, env)
	result := second.call(t, "list", nil)
	var structured struct {
		Repos []struct {
			Name string `json:"name"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(result["structuredContent"], &structured); err != nil {
		t.Fatal(err)
	}
	if len(structured.Repos) != 1 || structured.Repos[0].Name != "alpha" {
		t.Fatalf("persisted repos: %s", result["structuredContent"])
	}
	// R-KITB-EQT4: MCP identity metadata is exact, with no extra keys.
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(result["_meta"], &meta); err != nil {
		t.Fatal(err)
	}
	var info map[string]string
	if err := json.Unmarshal(meta["io.modelcontextprotocol/serverInfo"], &info); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info, map[string]string{"name": web.ServiceName, "version": version.Display()}) {
		t.Fatalf("serverInfo: %v", info)
	}
	second.stop(t, syscall.SIGINT, false)

	launcherDir := t.TempDir()
	servicePath := filepath.Join(launcherDir, "services.json")
	listed := []map[string]any{}
	for _, name := range []string{"auth", "dummy", "repos"} {
		icon := `<svg></svg>`
		if name == web.ServiceName {
			icon = binaryReposIcon
		}
		listed = append(listed, map[string]any{"name": name, "url": "https://" + name + ".example.test", "description": "Published " + name, "socket": "", "enabled": true, "mcp": true, "icon": icon})
	}
	writeBinaryServices(t, servicePath, listed)
	list, err := services.Read(servicePath)
	if err != nil || len(list) != 3 {
		t.Fatalf("launcher fixture: %v %v", list, err)
	}
	for i, name := range []string{"auth", "dummy", "repos"} {
		if list[i].Name != name || !list[i].Enabled || list[i].URL != "https://"+name+".example.test" || string(list[i].Icon) != listed[i]["icon"] {
			t.Fatal("invalid launcher fixture")
		}
	}
	launcher := startBinary(t, binary, launcherDir, append(append([]string{}, env...), services.Variable+"="+servicePath))
	_, body := launcher.request(t, http.MethodGet, "/", nil)
	// R-UEA4-U7Z1: compare the binary's page with its kit and template.
	t.Setenv(services.Variable, servicePath)
	request, err := http.NewRequest(http.MethodGet, "http://repos.example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	banner := page.New(web.ServiceName, version.Display()).Banner(page.User{ProfileURL: list[0].URL + "/", LogoutURL: list[0].URL + "/logout"})
	base := clone.Base(request, servicePath)
	templates, err := page.Templates().ParseFS(repos.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var expected bytes.Buffer
	if err := templates.ExecuteTemplate(&expected, "landing", map[string]any{"Banner": banner, "ReposURL": base, "Credentials": clone.Guidance(base)}); err != nil {
		t.Fatal(err)
	}
	if body != expected.String() {
		t.Fatal("binary page differs from rendered landing template")
	}
	for _, description := range []string{"Published repos", "Changed instructions"} {
		listed[2]["description"] = description
		writeBinaryServices(t, servicePath, listed)
		read, err := services.Read(servicePath)
		if err != nil {
			t.Fatal(err)
		}
		entry, ok := read.Find(web.ServiceName)
		if !ok {
			t.Fatal("repos service missing")
		}
		var got string
		if err := json.Unmarshal(launcher.discover(t)["instructions"], &got); err != nil {
			t.Fatal(err)
		}
		if got != entry.Description {
			t.Fatalf("instructions %q want %q", got, entry.Description)
		}
	}
	launcher.stop(t, syscall.SIGTERM, false)

	// R-KK17-SIJT: first/last delivered lifecycle events and silent clean exit.
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			root := t.TempDir()
			socketDir := binarySocketDir(t)
			ln, err := net.Listen("unix", filepath.Join(socketDir, "events"))
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var events []binaryEvent
			var busEvents []busevents.Event
			var ingestErrors []string
			srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == busevents.EmitPath {
					var event busevents.Event
					err := json.NewDecoder(r.Body).Decode(&event)
					mu.Lock()
					if err != nil {
						ingestErrors = append(ingestErrors, "invalid bus event")
					} else {
						busEvents = append(busEvents, event)
					}
					mu.Unlock()
					w.WriteHeader(http.StatusNoContent)
					return
				}
				var event binaryEvent
				err := json.NewDecoder(r.Body).Decode(&event)
				mu.Lock()
				if r.Method != http.MethodPost || r.URL.Path != "/ingest" || err != nil {
					ingestErrors = append(ingestErrors, "invalid ingest request")
				} else {
					events = append(events, event)
				}
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			})}
			serverDone := make(chan error, 1)
			go func() { serverDone <- srv.Serve(ln) }()
			t.Cleanup(func() { _ = srv.Close(); <-serverDone })
			file := filepath.Join(root, "services.json")
			writeBinaryServices(t, file, []map[string]any{{"name": "telemetry", "url": "", "description": "", "socket": ln.Addr().String(), "enabled": true, "mcp": false}, {"name": "events", "url": "", "description": "", "socket": ln.Addr().String(), "enabled": true, "mcp": false}})
			child := startBinary(t, binary, root, append(append([]string{}, env...), services.Variable+"="+file))
			// The binary leaves EventSink nil and must discover the bus socket.
			child.call(t, "create", json.RawMessage(`{"name":"bus"}`))
			proxy := &httputil.ReverseProxy{Director: func(r *http.Request) { r.URL.Scheme = "http"; r.URL.Host = "repos.example.test" }, Transport: child.client.Transport}
			front := httptest.NewServer(proxy)
			defer front.Close()
			clientDir := t.TempDir()
			gitCall := func(args ...string) {
				t.Helper()
				ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
				defer done()
				cmd := exec.CommandContext(ctx, git, args...)
				cmd.Dir, cmd.Env = clientDir, env
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			gitCall("init", "-b", "main")
			if err := os.WriteFile(filepath.Join(clientDir, "note"), []byte("bus fixture\n"), 0600); err != nil {
				t.Fatal(err)
			}
			gitCall("add", "note")
			gitCall("commit", "-m", "bus fixture")
			gitCall("-c", "http.extraHeader=X-User-Id: u", "push", front.URL+"/bus.git", "HEAD:refs/heads/main")
			child.stop(t, sig, true)
			mu.Lock()
			defer mu.Unlock()
			if len(ingestErrors) != 0 || len(events) < 2 {
				t.Fatalf("events=%v errors=%v", events, ingestErrors)
			}
			if len(busEvents) != 1 || busEvents[0].Name != "repo.pushed" || busEvents[0].Service != web.ServiceName || busEvents[0].User != "u" {
				t.Fatalf("binary socket bus events: %+v", busEvents)
			}
			assertLifecycle(t, events[0], "service.started", map[string]any{"version": version.Display()})
			assertLifecycle(t, events[len(events)-1], "service.stopping", map[string]any{"reason": binarySignalName(sig)})
		})
	}
}

type binaryEvent struct {
	Service   string         `json:"service"`
	Name      string         `json:"event"`
	RequestID string         `json:"request_id"`
	User      string         `json:"user"`
	Attrs     map[string]any `json:"attrs"`
}

func assertLifecycle(t *testing.T, e binaryEvent, name string, attrs map[string]any) {
	t.Helper()
	if e.Service != web.ServiceName || e.Name != name || e.RequestID != "" || e.User != "" || !reflect.DeepEqual(e.Attrs, attrs) {
		t.Fatalf("lifecycle event: %+v", e)
	}
}

func writeBinaryServices(t *testing.T, path string, entries []map[string]any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func binarySocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "repos-binary-")
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

type binaryChild struct {
	cmd             *exec.Cmd
	done            chan error
	out, diagnostic bytes.Buffer
	listener        *net.UnixListener
	path            string
	client          *http.Client
	waited          bool
}

func binaryNotifyFailure(t *testing.T, binary string, env []string) {
	t.Helper()
	socketDir := binarySocketDir(t)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(socketDir, "http"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	file, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	missingNotify := filepath.Join(socketDir, "absent-notify")
	if _, err := os.Lstat(missingNotify); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("notification fixture must be absent: %v", err)
	}
	conn, notifyErr := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: missingNotify, Net: "unixgram"})
	if notifyErr == nil {
		_ = conn.Close()
		t.Fatal("notification fixture unexpectedly accepts datagrams")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary)
	cmd.Dir = t.TempDir()
	cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+missingNotify)
	cmd.ExtraFiles = []*os.File{file}
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != cli.ExitServerFailed {
		t.Fatalf("notification failure exit: %v", err)
	}
	want := "repos: " + notifyErr.Error() + "\n"
	if out.Len() != 0 || diagnostic.String() != want {
		t.Fatalf("notification failure: stdout=%q stderr=%q; want only %q", out.String(), diagnostic.String(), want)
	}
	t.Logf("notification failed: exit=%d stdout=%q stderr=%q; no lifecycle fallback", exit.ExitCode(), out.String(), diagnostic.String())
}

func startBinary(t *testing.T, binary, dir string, env []string) *binaryChild {
	t.Helper()
	socketDir := binarySocketDir(t)
	path := filepath.Join(socketDir, "http")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	file, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	notifyPath := filepath.Join(socketDir, "notify")
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = notify.Close() }()
	child := &binaryChild{done: make(chan error, 1), listener: ln, path: path}
	child.cmd = exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary)
	child.cmd.Dir = dir
	child.cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notifyPath)
	child.cmd.ExtraFiles = []*os.File{file}
	child.cmd.Stdout, child.cmd.Stderr = &child.out, &child.diagnostic
	if err := child.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { child.done <- child.cmd.Wait() }()
	t.Cleanup(func() {
		if !child.waited {
			_ = child.cmd.Process.Kill()
			<-child.done
			child.waited = true
		}
	})
	if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	message := make([]byte, 1024)
	n, _, err := notify.ReadFromUnix(message)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if string(message[:n]) != "READY=1" {
		t.Fatalf("readiness: %q", message[:n])
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	child.client = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	return child
}

func (c *binaryChild) request(t *testing.T, method, path string, body io.Reader) (*http.Response, string) {
	t.Helper()
	r, err := http.NewRequest(method, "http://repos.example.test"+path, body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-User-Id", "u")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
		r.Header.Set("Mcp-Method", "server/discover")
	}
	response, err := c.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(response.Body)
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	return response, string(b)
}

func (c *binaryChild) call(t *testing.T, name string, args json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://repos.example.test/mcp", HTTPClient: c.client, Name: "binary-test", Version: version.Display()})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, identity.Caller{UserID: "u"}, name, args)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError() {
		t.Fatalf("tool %s returned error", name)
	}
	b, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(b, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func (c *binaryChild) discover(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "server/discover", "params": map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": mcp.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}})
	if err != nil {
		t.Fatal(err)
	}
	response, body := c.request(t, http.MethodPost, "/mcp", bytes.NewReader(b))
	if response.StatusCode != 200 {
		t.Fatalf("discover: %d %s", response.StatusCode, body)
	}
	var object struct {
		Result map[string]json.RawMessage `json:"result"`
		Error  json.RawMessage            `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &object); err != nil {
		t.Fatal(err)
	}
	if object.Result == nil || len(object.Error) != 0 {
		t.Fatalf("discover: %s", body)
	}
	return object.Result
}

func binarySignalName(sig syscall.Signal) string {
	if sig == syscall.SIGINT {
		return "SIGINT"
	}
	return "SIGTERM"
}

func (c *binaryChild) stop(t *testing.T, sig syscall.Signal, silent bool) {
	t.Helper()
	c.client.CloseIdleConnections()
	// R-RVQ1-KXH2: both supported signals complete a clean run.
	if err := c.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-c.done:
		c.waited = true
		if err != nil {
			t.Fatalf("signal %s: %v stderr=%s", binarySignalName(sig), err, c.diagnostic.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("binary did not stop")
	}
	if c.out.Len() != 0 {
		t.Fatalf("serving stdout: %q", c.out.String())
	}
	if silent {
		if c.diagnostic.Len() != 0 {
			t.Fatalf("serving stderr: %q", c.diagnostic.String())
		}
	} else {
		// R-O3YP-CK5K: no configured sink emits only JSON fallback lines.
		var events []binaryEvent
		for _, line := range strings.Split(strings.TrimSuffix(c.diagnostic.String(), "\n"), "\n") {
			const prefix = "repos: undelivered event: "
			if !strings.HasPrefix(line, prefix) {
				t.Fatalf("unexpected stderr line: %q", line)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &object); err != nil || object == nil {
				t.Fatalf("fallback is not a JSON object: %q", line)
			}
			var event binaryEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
		if !strings.HasSuffix(c.diagnostic.String(), "\n") || len(events) < 2 {
			t.Fatalf("fallback: %q", c.diagnostic.String())
		}
		assertLifecycle(t, events[0], "service.started", map[string]any{"version": version.Display()})
		assertLifecycle(t, events[len(events)-1], "service.stopping", map[string]any{"reason": binarySignalName(sig)})
	}
	// R-QH4Y-KPV9: the parent's retained socket still queues connections.
	if info, err := os.Stat(c.path); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("inherited path removed: %v", err)
	}
	conn, err := net.DialTimeout("unix", c.path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := c.listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	queued, err := c.listener.Accept()
	if err != nil {
		t.Fatalf("retained listener cannot accept: %v", err)
	}
	if err := queued.Close(); err != nil {
		t.Fatal(err)
	}
}
