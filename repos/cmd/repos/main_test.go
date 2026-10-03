package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

// TestBinary is the sole build/exec/signal test; runtime children receive only
// the explicitly composed environment below. The build tool uses its normal
// installed toolchain/cache environment without reading it in the test.
func TestBinary(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	env := []string{"PATH=" + filepath.Dir(git), "HOME=" + work, "XDG_CONFIG_HOME=" + work,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
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

	// R-RUI5-75QD: command output and exits agree with the public run seam.
	for _, args := range [][]string{{"--version"}, {"manifest"}, {"bogus"}, nil} {
		var wantOut, wantErr bytes.Buffer
		wantCode := cli.Run(context.Background(), cli.Process{Args: args, LookupEnv: func(string) (string, bool) { return "", false }, Stdout: &wantOut, Stderr: &wantErr})
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
	// R-QTBY-EFA7: a notification that cannot be sent never starts the
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
	// R-RZDQ-Q8P5: exact plain footer text derives its version from cli.Version.
	_, page := first.request(t, http.MethodGet, "/", nil)
	footerCount, footerText := binaryFooter(page)
	if footerCount != 1 || footerText != web.ServiceName+" "+cli.Version {
		t.Fatalf("footer: count=%d text=%q", footerCount, footerText)
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
	// R-S0LN-40FU: MCP identity metadata is exact, with no extra keys.
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(result["_meta"], &meta); err != nil {
		t.Fatal(err)
	}
	var info map[string]string
	if err := json.Unmarshal(meta["io.modelcontextprotocol/serverInfo"], &info); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info, map[string]string{"name": web.ServiceName, "version": cli.Version}) {
		t.Fatalf("serverInfo: %v", info)
	}
	second.stop(t, syscall.SIGINT, false)

	launcherDir := t.TempDir()
	servicePath := filepath.Join(launcherDir, "services.json")
	listed := []map[string]any{}
	for _, name := range []string{"auth", "dummy", "repos"} {
		listed = append(listed, map[string]any{"name": name, "url": "https://" + name + ".example.test", "description": "Published " + name, "socket": "", "enabled": true, "mcp": true, "icon": `<svg></svg>`})
	}
	writeBinaryServices(t, servicePath, listed)
	list, err := services.Read(servicePath)
	if err != nil || len(list) != 3 {
		t.Fatalf("launcher fixture: %v %v", list, err)
	}
	for i, name := range []string{"auth", "dummy", "repos"} {
		if list[i].Name != name || !list[i].Enabled || list[i].URL != "https://"+name+".example.test" || string(list[i].Icon) != `<svg></svg>` {
			t.Fatal("invalid launcher fixture")
		}
	}
	launcher := startBinary(t, binary, launcherDir, append(append([]string{}, env...), services.Variable+"="+servicePath))
	_, body := launcher.request(t, http.MethodGet, "/", nil)
	// R-970J-DHQW: appkit's real kit loads the ordered service entries in main.
	assertBinaryLauncher(t, body, list[2].URL)
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

	// R-S31F-VJX8: first/last delivered lifecycle events and silent clean exit.
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
			var ingestErrors []string
			srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			writeBinaryServices(t, file, []map[string]any{{"name": "telemetry", "url": "", "description": "", "socket": ln.Addr().String(), "enabled": true, "mcp": false}})
			child := startBinary(t, binary, root, append(append([]string{}, env...), services.Variable+"="+file))
			child.stop(t, sig, true)
			mu.Lock()
			defer mu.Unlock()
			if len(ingestErrors) != 0 || len(events) < 2 {
				t.Fatalf("events=%v errors=%v", events, ingestErrors)
			}
			assertLifecycle(t, events[0], "service.started", map[string]any{"version": cli.Version})
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
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://repos.example.test/mcp", HTTPClient: c.client, Name: "binary-test", Version: cli.Version})
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
		// R-QTBY-EFA7: no configured sink emits only JSON fallback lines.
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
		assertLifecycle(t, events[0], "service.started", map[string]any{"version": cli.Version})
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

func binaryFooter(body string) (int, string) {
	lower := strings.ToLower(body)
	count, content := 0, ""
	for offset := 0; offset < len(lower); {
		i := strings.Index(lower[offset:], "<footer")
		if i < 0 {
			break
		}
		i += offset
		offset = i + len("<footer")
		if offset >= len(lower) || (lower[offset] != '>' && !binarySpace(lower[offset])) {
			continue
		}
		count++
		end := strings.IndexByte(lower[offset:], '>')
		if end < 0 {
			continue
		}
		start := offset + end + 1
		footerEnd := strings.Index(lower[start:], "</footer>")
		if footerEnd >= 0 {
			content = html.UnescapeString(strings.Trim(body[start:start+footerEnd], " \t\r\n\f"))
		}
	}
	return count, content
}

type binaryTag struct {
	name  string
	attrs map[string][]string
}

func (tag binaryTag) matches(name string) bool {
	return tag.name == name || strings.HasPrefix(tag.name, name+"-")
}

func binarySpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\f' }
func binaryName(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-'
}

// binaryTags reads start spans and double-quoted attribute occurrences by D04.
func binaryTags(body string) []binaryTag {
	var result []binaryTag
	for offset := 0; offset < len(body); {
		start := strings.IndexByte(body[offset:], '<')
		if start < 0 {
			break
		}
		start += offset
		end := strings.IndexByte(body[start:], '>')
		if end < 0 {
			break
		}
		end += start
		offset = start + 1
		p := start + 1
		for p < end && binaryName(body[p]) {
			p++
		}
		if p == start+1 {
			continue
		}
		tag := binaryTag{name: strings.ToLower(body[start+1 : p]), attrs: map[string][]string{}}
		for p < end {
			if !binarySpace(body[p]) {
				break
			}
			for p < end && binarySpace(body[p]) {
				p++
			}
			nameStart := p
			for p < end && !binarySpace(body[p]) && !strings.ContainsRune(`"'<>/=`, rune(body[p])) {
				p++
			}
			if p == nameStart {
				break
			}
			name := strings.ToLower(body[nameStart:p])
			if p < end && body[p] == '=' {
				p++
				if p >= end || body[p] != '"' {
					break
				}
				p++
				valueStart := p
				for p < end && body[p] != '"' {
					p++
				}
				if p >= end {
					break
				}
				tag.attrs[name] = append(tag.attrs[name], html.UnescapeString(body[valueStart:p]))
				p++
			}
		}
		result = append(result, tag)
	}
	return result
}

func assertBinaryLauncher(t *testing.T, body, url string) {
	t.Helper()
	buttons, scripts, current := 0, 0, 0
	for _, tag := range binaryTags(body) {
		if tag.matches("button") {
			launcher := false
			for _, value := range tag.attrs["class"] {
				for _, class := range strings.FieldsFunc(value, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '\f' }) {
					if class == "launcher" {
						launcher = true
					}
				}
			}
			if launcher {
				buttons++
			}
		}
		if tag.matches("script") {
			scripts++
			if !reflect.DeepEqual(tag.attrs["src"], []string{"/_appkit/launcher.js"}) {
				t.Fatalf("launcher script: %v", tag.attrs)
			}
		}
		if len(tag.attrs["aria-current"]) > 0 {
			current++
			if !tag.matches("a") || !reflect.DeepEqual(tag.attrs["aria-current"], []string{"page"}) || !reflect.DeepEqual(tag.attrs["href"], []string{url}) {
				t.Fatalf("current launcher entry: %v", tag)
			}
		}
	}
	if buttons != 1 || scripts != 1 || current != 1 {
		t.Fatalf("launcher counts: buttons=%d scripts=%d current=%d", buttons, scripts, current)
	}
}
