package main_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func binaryMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBinary(t *testing.T) {
	// R-9BM7-ZMWS R-K2VG-GMF3 R-L44O-3JLM R-K5B9-85WH R-K6J5-LXN6
	// R-K7R1-ZPDV R-K8YY-DH4K R-KA6U-R8V9 R-LSIN-QYFI
	// R-L5CK-HBCB R-BODX-EEGV R-XW4F-83YL
	t.Setenv(services.Variable, "")
	root := t.TempDir()
	t.Cleanup(func() {
		cleanup, e := os.OpenRoot(root)
		if e != nil {
			t.Error(e)
			return
		}
		defer func() { _ = cleanup.Close() }()
		_ = fs.WalkDir(cleanup.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				info, e := entry.Info()
				if e == nil {
					_ = cleanup.Chmod(path, info.Mode().Perm()|0700)
				}
			}
			return nil
		})
	})
	binary := filepath.Join(root, "binary")
	build := exec.Command("go", "build", "-o", binary, ".")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v\n%s", e, b)
	}
	git, e := exec.LookPath("git")
	binaryMust(t, e)
	python, e := exec.LookPath(runner.Interpreter)
	binaryMust(t, e)
	env := []string{"HOME=" + root, "XDG_CONFIG_HOME=" + root, "PATH=" + filepath.Dir(git) + ":" + filepath.Dir(python), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_COMMITTER_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2025-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2025-01-01T00:00:00Z"}
	dir := filepath.Join(root, "scripts")
	binaryMust(t, os.Mkdir(dir, 0700))
	var pending bytes.Buffer
	binaryMust(t, db.Status(context.Background(), db.Config{Path: filepath.Join(dir, "state", "scripts.db"), Migrations: scripts.Migrations()}, &pending))
	for _, tc := range []struct {
		args     []string
		out, err string
		code     int
	}{
		{[]string{"--version"}, cli.Version + "\n", "", cli.ExitSuccess},
		{[]string{"manifest"}, cli.Manifest, "", cli.ExitSuccess},
		{[]string{"--help"}, cli.Usage, "", cli.ExitSuccess},
		{[]string{"db", "status"}, pending.String(), "", cli.ExitSuccess},
		{[]string{"db", "bogus"}, "", "scripts: unknown command 'bogus'\n\nsee 'scripts --help' for usage\n", cli.ExitUsage},
		{[]string{"db", "status", "extra"}, "", "scripts: unknown command 'extra'\n\nsee 'scripts --help' for usage\n", cli.ExitUsage},
		{[]string{"bogus"}, "", "scripts: unknown command 'bogus'\n\nsee 'scripts --help' for usage\n", cli.ExitUsage},
		{nil, "", "scripts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
	} {
		command := exec.Command(binary, tc.args...)
		command.Dir = dir
		command.Env = env
		var out, diagnostic bytes.Buffer
		command.Stdout = &out
		command.Stderr = &diagnostic
		e := command.Run()
		code := 0
		if e != nil {
			var exit *exec.ExitError
			if !errors.As(e, &exit) {
				t.Fatal(e)
			}
			code = exit.ExitCode()
		}
		if code != tc.code || out.String() != tc.out || diagnostic.String() != tc.err {
			t.Fatalf("args %v: %d %q %q", tc.args, code, out.String(), diagnostic.String())
		}
	}
	work := filepath.Join(root, "work")
	binaryMust(t, os.Mkdir(work, 0700))
	command := func(cwd string, args ...string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, git, args...)
		c.Dir = cwd
		c.Env = env
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %v %s", args, e, b)
		}
	}
	command(work, "init", "--initial-branch=main")
	binaryMust(t, os.WriteFile(filepath.Join(work, "main.py"), []byte("print('fixture')\n"), 0600))
	command(work, "add", ".")
	command(work, "commit", "-m", "fixture")
	repos := filepath.Join(root, "repos", "state", "repos")
	binaryMust(t, os.MkdirAll(repos, 0700))
	repo := filepath.Join(repos, "rep_0102030405060708.git")
	command(root, "clone", "--bare", work, repo)
	command(repo, "config", "ikigenba.owner", "owner")
	command(repo, "config", "ikigenba.name", "fixture")
	short, e := os.MkdirTemp("", "scripts-binary-")
	binaryMust(t, e)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	ln, e := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(short, "app"), Net: "unix"})
	binaryMust(t, e)
	defer func() { _ = ln.Close() }()
	ln.SetUnlinkOnClose(false)
	descriptor, e := ln.File()
	binaryMust(t, e)
	defer func() { _ = descriptor.Close() }()
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify"), Net: "unixgram"})
	binaryMust(t, e)
	defer func() { _ = notify.Close() }()
	telemetrySocket := filepath.Join(short, "trail")
	trail, e := net.Listen("unix", telemetrySocket)
	binaryMust(t, e)
	var mu sync.Mutex
	var events []map[string]any
	eventUpdates := make(chan struct{}, 1)
	trailServer := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ingest" {
			http.Error(w, "unexpected", 400)
			return
		}
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		select {
		case eventUpdates <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = trailServer.Serve(trail) }()
	defer func() { _ = trailServer.Close() }()
	servicePath := filepath.Join(root, "services.json")
	writeServices := func(entries []map[string]any) {
		b, e := json.Marshal(map[string]any{"services": entries})
		binaryMust(t, e)
		binaryMust(t, os.WriteFile(servicePath, b, 0600))
	}
	telemetryEntry := map[string]any{"name": "telemetry", "enabled": true, "url": "https://telemetry.example", "socket": telemetrySocket, "mcp": false, "description": "Trail"}
	writeServices([]map[string]any{telemetryEntry})
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", ln.Addr().String())
	}}
	httpClient := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer httpClient.CloseIdleConnections()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://scripts.example/mcp", HTTPClient: httpClient})
	caller := identity.Caller{UserID: "owner", Email: "mg@example.com", RequestID: "binary-tool"}
	call := func(name string, args any) map[string]any {
		var raw json.RawMessage
		if args != nil {
			b, e := json.Marshal(args)
			binaryMust(t, e)
			raw = b
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r, e := client.CallTool(ctx, caller, name, raw)
		binaryMust(t, e)
		b, e := r.MarshalJSON()
		binaryMust(t, e)
		if r.IsError() {
			t.Fatalf("tool %s: %s", name, b)
		}
		var v map[string]any
		binaryMust(t, json.Unmarshal(b, &v))
		meta := v["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"]
		if !reflect.DeepEqual(meta, map[string]any{"name": pages.ServiceName, "version": cli.Version}) {
			t.Fatalf("serverInfo %v", meta)
		}
		return v["structuredContent"].(map[string]any)
	}
	request := func(path, method, body string) (int, string) {
		req, e := http.NewRequest(method, "http://scripts.example"+path, strings.NewReader(body))
		binaryMust(t, e)
		req.Host = "scripts.sbx.ikigenba.dev"
		req.Header.Set("X-User-Id", "owner")
		req.Header.Set("X-User-Email", "mg@example.com")
		req.Header.Set("X-Forwarded-Proto", "https")
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
			req.Header.Set("Mcp-Method", "server/discover")
		}
		res, e := httpClient.Do(req)
		binaryMust(t, e)
		defer func() { _ = res.Body.Close() }()
		b, e := io.ReadAll(res.Body)
		binaryMust(t, e)
		return res.StatusCode, string(b)
	}
	discover := func(want *string) {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{}}}}`, mcp.ProtocolVersion)
		status, b := request("/mcp", http.MethodPost, body)
		if status != 200 {
			t.Fatalf("discover %d %s", status, b)
		}
		var v struct {
			Result map[string]any `json:"result"`
		}
		binaryMust(t, json.Unmarshal([]byte(b), &v))
		got, ok := v.Result["instructions"]
		if want == nil && ok || want != nil && (!ok || got != *want) {
			t.Fatalf("instructions: %v", v.Result)
		}
	}
	start := func(servicesPath string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
		c := exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary)
		c.Dir = dir
		c.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
		if servicesPath != "" {
			c.Env = append(c.Env, services.Variable+"="+servicesPath)
		}
		c.ExtraFiles = []*os.File{descriptor}
		out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
		c.Stdout = out
		c.Stderr = errOut
		binaryMust(t, c.Start())
		t.Cleanup(func() {
			if c.ProcessState == nil {
				_ = c.Process.Kill()
				_ = c.Wait()
			}
		})
		binaryMust(t, notify.SetReadDeadline(time.Now().Add(10*time.Second)))
		b := make([]byte, 64)
		n, _, e := notify.ReadFromUnix(b)
		binaryMust(t, e)
		if string(b[:n]) != "READY=1" {
			t.Fatalf("notify %q", b[:n])
		}
		return c, out, errOut
	}
	stop := func(c *exec.Cmd, signal syscall.Signal, out, errOut *bytes.Buffer, quiet bool) {
		binaryMust(t, c.Process.Signal(signal))
		done := make(chan error)
		go func() { done <- c.Wait() }()
		select {
		case e := <-done:
			binaryMust(t, e)
		case <-time.After(10 * time.Second):
			t.Fatal("binary did not stop")
		}
		if out.Len() != 0 || quiet && !regexp.MustCompile(`^scripts: runs are unavailable: [^\n]+\n$`).MatchString(errOut.String()) {
			t.Fatalf("streams %q %q", out.String(), errOut.String())
		}
	}
	c, out, errOut := start("")
	info, e := os.Stat(filepath.Join(dir, "state", "scripts.db"))
	binaryMust(t, e)
	if !info.Mode().IsRegular() {
		t.Fatal("catalog not regular")
	}
	entries, e := os.ReadDir(filepath.Join(dir, "state", "runs"))
	binaryMust(t, e)
	if len(entries) != 0 {
		t.Fatal("runs nonempty")
	}
	_, body := request("/", http.MethodGet, "")
	assertBinaryText(t, body, "footer", nil, pages.ServiceName+" "+cli.Version)
	_, body = request("/about", http.MethodGet, "")
	assertBinaryText(t, body, "dd", map[string]string{"id": "about-version"}, cli.Version)
	discover(nil)
	call("create", map[string]any{"name": "alpha", "repo": "rep_0102030405060708"})
	stop(c, syscall.SIGTERM, out, errOut, false)
	queued, e := net.DialTimeout("unix", ln.Addr().String(), time.Second)
	binaryMust(t, e)
	defer func() { _ = queued.Close() }()
	binaryMust(t, queued.SetDeadline(time.Now().Add(10*time.Second)))
	_, e = io.WriteString(queued, "GET /about HTTP/1.1\r\nHost: scripts.example\r\nX-User-Id: owner\r\nConnection: close\r\n\r\n")
	binaryMust(t, e)
	mu.Lock()
	events = nil
	mu.Unlock()
	c, out, errOut = start(servicePath)
	response, e := http.ReadResponse(bufio.NewReader(queued), nil)
	binaryMust(t, e)
	if response.StatusCode != 200 {
		t.Fatalf("queued request %d", response.StatusCode)
	}
	_, e = io.Copy(io.Discard, response.Body)
	binaryMust(t, e)
	binaryMust(t, response.Body.Close())
	listed := call("list", nil)["scripts"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["name"] != "alpha" {
		t.Fatalf("persistent scripts %v", listed)
	}
	discover(nil)
	description := "Description from current file"
	named := map[string]any{"name": "scripts", "enabled": true, "url": "https://scripts.example", "socket": ln.Addr().String(), "mcp": true, "description": description}
	writeServices([]map[string]any{telemetryEntry, named})
	discover(&description)
	description = "Changed description"
	named["description"] = description
	writeServices([]map[string]any{telemetryEntry, named})
	discover(&description)
	binaryMust(t, os.WriteFile(servicePath, []byte("broken"), 0600))
	discover(nil)
	binaryMust(t, os.Remove(servicePath))
	discover(nil)
	writeServices([]map[string]any{telemetryEntry})
	stop(c, syscall.SIGINT, out, errOut, false)
	// The queued request above and deliberate unavailable services file can emit diagnostics.
	// A separate idle run proves the exact, quiet lifecycle for both signals.
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		mu.Lock()
		events = nil
		mu.Unlock()
		c, out, errOut = start(servicePath)
		stop(c, sig, out, errOut, true)
		mu.Lock()
		snapshot := append([]map[string]any(nil), events...)
		mu.Unlock()
		if len(snapshot) != 2 {
			t.Fatalf("lifecycle events %v", snapshot)
		}
		for i, event := range snapshot {
			name, attrs := "service.started", map[string]any{"version": cli.Version}
			if i == 1 {
				name = "service.stopping"
				attrs = map[string]any{"reason": map[syscall.Signal]string{syscall.SIGTERM: "SIGTERM", syscall.SIGINT: "SIGINT"}[sig]}
			}
			if event["service"] != "scripts" || event["event"] != name || event["request_id"] != "" || event["user"] != "" || !reflect.DeepEqual(event["attrs"], attrs) {
				t.Fatalf("event %v", event)
			}
		}
	}
	serviceEntries := []map[string]any{}
	for _, name := range []string{"auth", "dummy", "scripts"} {
		serviceEntries = append(serviceEntries, map[string]any{"name": name, "url": "https://" + name + ".different.example", "socket": filepath.Join(short, name), "enabled": true, "mcp": name == "scripts", "description": name, "icon": "<svg></svg>"})
	}
	writeServices(serviceEntries)
	c, out, errOut = start(servicePath)
	_, body = request("/", http.MethodGet, "")
	assertBinaryTag(t, body, "a", map[string]string{"class": "profile"})
	assertBinaryTag(t, body, "a", map[string]string{"class": "profile", "title": "mg@example.com", "href": "https://auth.different.example/"})
	assertBinaryTag(t, body, "form", nil)
	assertBinaryTag(t, body, "form", map[string]string{"action": "https://auth.different.example/logout"})
	assertBinaryTag(t, body, "button", map[string]string{"class": "launcher"})
	assertBinaryTag(t, body, "script", map[string]string{"src": "/_appkit/launcher.js"})
	assertBinaryAttribute(t, body, "aria-current", "a", map[string]string{"aria-current": "page", "href": "https://scripts.different.example"})
	if binaryAttributeCount(body, "aria-disabled") != 0 {
		t.Fatal("enabled service disabled")
	}
	serviceEntries[1]["enabled"] = false
	writeServices(serviceEntries)
	_, body = request("/", http.MethodGet, "")
	assertBinaryAttribute(t, body, "aria-disabled", "a", map[string]string{"title": "dummy is unavailable"})
	stop(c, syscall.SIGTERM, out, errOut, false)

	// R-O5F3-NEX8
	// Prepare real ended run metadata before tracing. The trace window includes
	// only run-file requests, so permitted setup git/Python cannot mask a leak.
	writeServices([]map[string]any{telemetryEntry})
	c, out, errOut = start(servicePath)
	scriptID := call("create", map[string]any{"name": "files-proof", "repo": "rep_0102030405060708"})["id"].(string)
	neverID := call("create", map[string]any{"name": "never-run", "repo": "rep_0102030405060708"})["id"].(string)
	ctx, cancelRun := context.WithTimeout(context.Background(), 10*time.Second)
	refused, e := client.CallTool(ctx, caller, "run", json.RawMessage(`{"name":"files-proof"}`))
	cancelRun()
	binaryMust(t, e)
	refusal, e := refused.MarshalJSON()
	binaryMust(t, e)
	if !refused.IsError() || !strings.Contains(string(refusal), "runs are unavailable: ") {
		t.Fatalf("unavailable run: %s", refusal)
	}
	stop(c, syscall.SIGTERM, out, errOut, true)
	runID := "run_1122334455667788"
	seedDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }})
	binaryMust(t, e)
	seedStore := store.New(seedDB, store.Config{Now: func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }, Rand: bytes.NewReader(make([]byte, 64))})
	_, e = seedStore.AddRun(context.Background(), store.Run{ID: runID, Script: scriptID, SHA: strings.Repeat("a", 40), Ref: "main", Trigger: store.TriggerManual, Status: store.StatusRunning, User: "owner", Started: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)})
	binaryMust(t, e)
	_, e = seedStore.FinishRun(context.Background(), runID, store.Ending{Status: store.StatusExited, Finished: time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC)})
	binaryMust(t, e)
	binaryMust(t, seedDB.Close())
	runsRoot := filepath.Join(dir, "state", "runs")
	folder := filepath.Join(runsRoot, scriptID, runID)
	binaryMust(t, os.MkdirAll(folder, 0700))
	for _, name := range []string{"input.json", "stdout", "stderr"} {
		binaryMust(t, os.WriteFile(filepath.Join(folder, name), []byte("fixture"), 0600))
	}

	outside := filepath.Join(root, "link-targets")
	binaryMust(t, os.MkdirAll(filepath.Join(outside, "directory"), 0700))
	binaryMust(t, os.WriteFile(filepath.Join(outside, "file"), []byte("outside preserved"), 0600))
	binaryMust(t, os.WriteFile(filepath.Join(outside, "directory", "nested"), []byte("nested preserved"), 0600))
	binaryMust(t, os.MkdirAll(filepath.Join(folder, "out", "nested"), 0700))
	binaryMust(t, os.WriteFile(filepath.Join(folder, "out", "file.txt"), []byte("kept output"), 0600))
	binaryMust(t, os.WriteFile(filepath.Join(folder, "out", "nested", "file.txt"), []byte("nested output"), 0600))
	binaryMust(t, os.Symlink(filepath.Join(outside, "file"), filepath.Join(folder, "out", "link")))
	binaryMust(t, os.Symlink(filepath.Join(outside, "directory"), filepath.Join(folder, "out", "dir-link")))
	binaryMust(t, os.Symlink(filepath.Join(folder, "out", "file.txt"), filepath.Join(folder, "out", "inside-link")))
	traceOut, e := os.CreateTemp(root, "trace-stdout-")
	binaryMust(t, e)
	defer func() { _ = traceOut.Close() }()
	traceErr, e := os.CreateTemp(root, "trace-stderr-")
	binaryMust(t, e)
	defer func() { _ = traceErr.Close() }()
	traced := exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary)
	traced.Dir = dir
	traced.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify.LocalAddr().String(), services.Variable+"="+servicePath)
	traced.ExtraFiles = []*os.File{descriptor}
	traced.Stdout = traceOut
	traced.Stderr = traceErr
	trace := startBinaryTrace(t, traced)
	defer trace.close()
	binaryMust(t, notify.SetReadDeadline(time.Now().Add(10*time.Second)))
	ready := make([]byte, 64)
	n, _, e := notify.ReadFromUnix(ready)
	binaryMust(t, e)
	if string(ready[:n]) != "READY=1" {
		t.Fatalf("trace readiness %q", ready[:n])
	}
	catalogDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }})
	binaryMust(t, e)
	defer func() { _ = catalogDB.Close() }()
	catalog := store.New(catalogDB, store.Config{Now: func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }, Rand: bytes.NewReader(make([]byte, 64))})
	catalogSnapshot := func() []byte {
		scripts, e := catalog.List(context.Background(), "owner")
		binaryMust(t, e)
		all := map[string]any{"scripts": scripts}
		for _, script := range scripts {
			records, e := catalog.Runs(context.Background(), script.ID)
			binaryMust(t, e)
			all[script.ID] = records
		}
		b, e := json.Marshal(all)
		binaryMust(t, e)
		return b
	}
	beforeCatalog := catalogSnapshot()
	beforeRuns := binaryDiskSnapshot(t, runsRoot)
	beforeOutside := binaryDiskSnapshot(t, outside)
	trace.begin()
	prefix := "/files-proof/runs/" + runID + "/"
	cases := []struct {
		path, method string
		status       int
	}{
		{prefix + "input.json", "GET", 200}, {prefix + "stdout", "GET", 200}, {prefix + "stderr", "HEAD", 200},
		{prefix + "out/file.txt", "GET", 200}, {prefix + "out/nested/file.txt", "HEAD", 200},
		{prefix + "out/missing", "GET", 404}, {prefix + "out/", "HEAD", 404},
		{prefix + "out/link", "GET", 404}, {prefix + "out/dir-link/nested", "GET", 404}, {prefix + "out/inside-link", "GET", 404},
		{prefix + "tree/main.py", "GET", 404}, {prefix + "stdout/", "GET", 404}, {prefix + "out/%2e%2e/input.json", "GET", 404},
		{prefix + "input.json", "POST", 405}, {prefix + "out/file.txt", "DELETE", 405},
		{"/never-run/runs/run_ffffffffffffffff/stdout", "GET", 404},
		{"/missing-script/runs/" + runID + "/input.json", "GET", 404},
		{"/files-proof/runs/run_ffffffffffffffff/out/file.txt", "GET", 404},
		{"/alpha/runs/" + runID + "/stdout", "GET", 404},
	}
	wantedCompletions := map[string]bool{}
	for i, tc := range cases {
		requestID := fmt.Sprintf("files-proof-%02d", i)
		wantedCompletions[requestID] = true
		req, e := http.NewRequest(tc.method, "http://scripts.example"+tc.path, nil)
		binaryMust(t, e)
		req.Header.Set("X-User-Id", "owner")
		req.Header.Set("X-Request-Id", requestID)
		res, e := httpClient.Do(req)
		binaryMust(t, e)
		_, e = io.Copy(io.Discard, res.Body)
		binaryMust(t, e)
		binaryMust(t, res.Body.Close())
		if res.StatusCode != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, res.StatusCode)
		}
		if _, present := res.Header["Set-Cookie"]; present {
			t.Fatalf("run-file cookie: %s", tc.path)
		}
	}

	// Complete bodies can be flushed before handlers return. Keep observing until
	// telemetry confirms each handler returned, including its deferred work.
	completionDeadline := time.NewTimer(10 * time.Second)
	defer completionDeadline.Stop()
	for {
		counts := map[string]int{}
		mu.Lock()
		for _, event := range events {
			id, _ := event["request_id"].(string)
			if wantedCompletions[id] && event["event"] == "request.finished" {
				counts[id]++
			}
		}
		mu.Unlock()
		for id, count := range counts {
			if count != 1 {
				t.Fatalf("request %s completed %d times", id, count)
			}
		}
		if len(counts) == len(wantedCompletions) {
			break
		}
		select {
		case <-eventUpdates:
		case <-completionDeadline.C:
			t.Fatalf("file request completions: %d of %d", len(counts), len(wantedCompletions))
		}
	}
	trace.end()
	if !bytes.Equal(beforeCatalog, catalogSnapshot()) {
		t.Fatal("file requests changed catalog")
	}
	if !reflect.DeepEqual(beforeRuns, binaryDiskSnapshot(t, runsRoot)) {
		t.Fatal("file requests changed runs tree")
	}
	if !reflect.DeepEqual(beforeOutside, binaryDiskSnapshot(t, outside)) {
		t.Fatal("file requests changed symbolic-link targets")
	}
	if _, e := os.Lstat(filepath.Join(runsRoot, neverID)); !errors.Is(e, fs.ErrNotExist) {
		t.Fatalf("never-run folder created: %v", e)
	}
	trace.close()

}

func binaryTags(body, tag string) []string {
	if tag == "footer" {
		return regexp.MustCompile(`(?i)<footer(?:>|[\t\n\r\f ][^>]*>)`).FindAllString(body, -1)
	}
	return regexp.MustCompile(`(?i)<`+tag+`(?:>|[^a-z0-9>][^>]*>)`).FindAllString(body, -1)
}

type binaryAttribute struct {
	name, value string
	occurrence  bool
}

func binaryAttributes(start string) []binaryAttribute {
	name := regexp.MustCompile(`^<[A-Za-z0-9-]+`).FindString(start)
	rest := start[len(name):]
	re := regexp.MustCompile(`^[\t\n\r\f ]+([^\t\n\r\f "'<>=/]+)(?:="([^"]*)")?`)
	var attributes []binaryAttribute
	for {
		indices := re.FindStringSubmatchIndex(rest)
		if indices == nil {
			return attributes
		}
		attr := binaryAttribute{name: rest[indices[2]:indices[3]], occurrence: indices[4] >= 0}
		if attr.occurrence {
			attr.value = html.UnescapeString(rest[indices[4]:indices[5]])
		}
		attributes = append(attributes, attr)
		rest = rest[indices[1]:]
	}
}
func binaryHasOccurrence(start, name string) bool {
	for _, attr := range binaryAttributes(start) {
		if attr.occurrence && strings.EqualFold(attr.name, name) {
			return true
		}
	}
	return false
}
func binaryAttributeCount(body, name string) int {
	count := 0
	for _, start := range binaryTags(body, `[a-z][a-z0-9-]*`) {
		if binaryHasOccurrence(start, name) {
			count++
		}
	}
	return count
}
func binaryMatches(start string, attrs map[string]string) bool {
	for name, want := range attrs {
		matched := false
		for _, attr := range binaryAttributes(start) {
			if !attr.occurrence || !strings.EqualFold(attr.name, name) {
				continue
			}
			if name == "class" {
				for _, token := range strings.FieldsFunc(attr.value, func(c rune) bool { return strings.ContainsRune(" \t\n\r\f", c) }) {
					if token == want {
						matched = true
					}
				}
			} else if attr.value == want {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
func assertBinaryTag(t *testing.T, body, tag string, attrs map[string]string) {
	t.Helper()
	count := 0
	for _, s := range binaryTags(body, tag) {
		if binaryMatches(s, attrs) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%s %v count %d; body %s", tag, attrs, count, body)
	}
}
func assertBinaryText(t *testing.T, body, tag string, attrs map[string]string, want string) {
	t.Helper()
	if tag == "footer" {
		assertBinaryTag(t, body, tag, attrs)
	}
	for _, s := range binaryTags(body, tag) {
		if binaryMatches(s, attrs) {
			offset := strings.Index(body, s) + len(s)
			end := strings.Index(strings.ToLower(body[offset:]), "</"+tag+">")
			if end < 0 {
				t.Fatal("missing closing tag")
			}
			got := html.UnescapeString(strings.Trim(body[offset:offset+end], " \t\n\r\f"))
			if got != want {
				t.Fatalf("text %q want %q", got, want)
			}
			return
		}
	}
	t.Fatal("matching text tag absent")
}

func assertBinaryAttribute(t *testing.T, body, attribute, tag string, attrs map[string]string) {
	t.Helper()
	count := 0
	for _, start := range binaryTags(body, `[a-z][a-z0-9]*`) {
		if binaryHasOccurrence(start, attribute) {
			count++
			if !regexp.MustCompile(`(?i)^<`+tag+`[^a-z0-9]`).MatchString(start) || !binaryMatches(start, attrs) {
				t.Fatalf("attribute %s on wrong tag: %s", attribute, start)
			}
		}
	}
	if count != 1 {
		t.Fatalf("attribute %s count %d", attribute, count)
	}
}

type binaryDiskEntry struct {
	mode    fs.FileMode
	size    int64
	content string
}

func binaryDiskSnapshot(t *testing.T, path string) map[string]binaryDiskEntry {
	t.Helper()
	root, e := os.OpenRoot(path)
	binaryMust(t, e)
	defer func() { _ = root.Close() }()
	result := map[string]binaryDiskEntry{}
	binaryMust(t, fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := binaryDiskEntry{mode: info.Mode(), size: info.Size()}
		if info.Mode().IsRegular() {
			b, err := root.ReadFile(name)
			if err != nil {
				return err
			}
			entry.content = string(b)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			value, err := root.Readlink(name)
			if err != nil {
				return err
			}
			entry.content = value
		}
		result[name] = entry
		return nil
	}))
	return result
}

type binaryTraceCommand struct {
	action string
	reply  chan error
}
type binaryTrace struct {
	t        *testing.T
	commands chan binaryTraceCommand
	done     chan error
	closed   bool
}

func startBinaryTrace(t *testing.T, command *exec.Cmd) *binaryTrace {
	t.Helper()
	trace := &binaryTrace{t: t, commands: make(chan binaryTraceCommand), done: make(chan error, 1)}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		command.SysProcAttr = &syscall.SysProcAttr{Ptrace: true}
		if err := command.Start(); err != nil {
			ready <- err
			trace.done <- err
			return
		}
		defer func() { _ = command.Process.Release() }()
		pid := command.Process.Pid
		live := map[int]bool{pid: true}
		defer func() {
			for tid := range live {
				_ = syscall.Kill(tid, syscall.SIGKILL)
			}
			cleanupDeadline := time.Now().Add(2 * time.Second)
			for len(live) > 0 && time.Now().Before(cleanupDeadline) {
				var status syscall.WaitStatus
				tid, err := syscall.Wait4(-1, &status, syscall.WALL|syscall.WNOHANG, nil)
				if err != nil && err != syscall.EINTR {
					break
				}
				if tid > 0 {
					if status.Exited() || status.Signaled() {
						delete(live, tid)
					} else if status.Stopped() {
						_ = syscall.PtraceCont(tid, int(syscall.SIGKILL))
					}
				}
				runtime.Gosched()
			}
		}()
		var status syscall.WaitStatus
		if _, err := syscall.Wait4(pid, &status, 0, nil); err != nil {
			ready <- err
			trace.done <- err
			return
		}
		options := syscall.PTRACE_O_TRACEFORK | syscall.PTRACE_O_TRACEVFORK | syscall.PTRACE_O_TRACECLONE | syscall.PTRACE_O_TRACEEXEC
		if err := syscall.PtraceSetOptions(pid, options); err != nil {
			ready <- err
			trace.done <- err
			return
		}
		if err := syscall.PtraceCont(pid, 0); err != nil {
			ready <- err
			trace.done <- err
			return
		}
		ready <- nil
		watching := false
		processes, execs := 0, 0
		deadline := time.Now().Add(30 * time.Second)
		stopping := false
		for len(live) > 0 {
			tid, err := syscall.Wait4(-1, &status, syscall.WALL|syscall.WNOHANG, nil)
			if err != nil && err != syscall.EINTR {
				trace.done <- err
				return
			}
			if tid > 0 {
				if status.Exited() || status.Signaled() {
					delete(live, tid)
					continue
				}
				if !status.Stopped() {
					continue
				}
				cause := status.TrapCause()
				if cause == syscall.PTRACE_EVENT_FORK || cause == syscall.PTRACE_EVENT_VFORK || cause == syscall.PTRACE_EVENT_CLONE {
					child, err := syscall.PtraceGetEventMsg(tid)
					if err != nil {
						trace.done <- err
						return
					}
					live[int(child)] = true
					thread := false
					if cause == syscall.PTRACE_EVENT_CLONE {
						var registers syscall.PtraceRegs
						if err := syscall.PtraceGetRegs(tid, &registers); err != nil {
							trace.done <- err
							return
						}
						flags := registers.Rdi
						// Linux amd64 clone3 receives a pointer to clone_args; its first field is flags.
						const clone3 = 435
						if registers.Orig_rax == clone3 {
							b := make([]byte, 8)
							if _, err := syscall.PtracePeekData(tid, uintptr(registers.Rdi), b); err != nil {
								trace.done <- err
								return
							}
							flags = binary.LittleEndian.Uint64(b)
						}
						thread = flags&syscall.CLONE_THREAD != 0
					}
					if watching && !thread {
						processes++
					}
				}
				if cause == syscall.PTRACE_EVENT_EXEC && watching {
					execs++
				}
				signal := int(status.StopSignal())
				if signal == int(syscall.SIGTRAP) || signal == int(syscall.SIGSTOP) {
					signal = 0
				}
				if err := syscall.PtraceCont(tid, signal); err != nil && err != syscall.ESRCH {
					trace.done <- err
					return
				}
				continue
			}
			select {
			case control := <-trace.commands:
				switch control.action {
				case "begin":
					watching = true
					processes = 0
					execs = 0
					control.reply <- nil
				case "end":
					watching = false
					if processes != 0 || execs != 0 {
						control.reply <- fmt.Errorf("run-file requests started %d processes and executed %d images", processes, execs)
					} else {
						control.reply <- nil
					}
				case "stop":
					stopping = true
					_ = syscall.Kill(pid, syscall.SIGTERM)
					control.reply <- nil
				}
			default:
			}
			if time.Now().After(deadline) {
				for tid := range live {
					_ = syscall.Kill(tid, syscall.SIGKILL)
				}
				trace.done <- errors.New("trace deadline exceeded")
				return
			}
			runtime.Gosched()
		}
		if !stopping {
			trace.done <- errors.New("traced binary exited before stop")
			return
		}
		trace.done <- nil
	}()
	select {
	case err := <-ready:
		binaryMust(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("trace did not start")
	}
	return trace
}
func (trace *binaryTrace) control(action string) {
	trace.t.Helper()
	command := binaryTraceCommand{action: action, reply: make(chan error, 1)}
	select {
	case trace.commands <- command:
	case err := <-trace.done:
		trace.t.Fatalf("trace stopped: %v", err)
	case <-time.After(10 * time.Second):
		trace.t.Fatal("trace control blocked")
	}
	select {
	case err := <-command.reply:
		binaryMust(trace.t, err)
	case <-time.After(10 * time.Second):
		trace.t.Fatal("trace acknowledgment blocked")
	}
}
func (trace *binaryTrace) begin() { trace.control("begin") }
func (trace *binaryTrace) end()   { trace.control("end") }
func (trace *binaryTrace) close() {
	if trace.closed {
		return
	}
	trace.closed = true
	trace.control("stop")
	select {
	case err := <-trace.done:
		binaryMust(trace.t, err)
	case <-time.After(10 * time.Second):
		trace.t.Fatal("trace stop blocked")
	}
}
