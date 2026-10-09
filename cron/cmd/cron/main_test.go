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
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/version"
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/cli"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
)

type binaryRun struct {
	cmd       *exec.Cmd
	listener  *net.UnixListener
	client    *http.Client
	out, diag bytes.Buffer
	done      chan error
	stopped   bool
}

func startBinary(t *testing.T, bin, dir string, env []string) *binaryRun {
	t.Helper()
	short, err := os.MkdirTemp("", "cron-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(short); e != nil {
			t.Error(e)
		}
	})
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(short, "serve.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	file, err := listener.File()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify.sock"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = notify.Close() }()
	b := &binaryRun{listener: listener, done: make(chan error, 1)}
	b.cmd = exec.Command("/bin/sh")
	b.cmd.Args = append(b.cmd.Args, "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, bin)
	b.cmd.Dir = dir
	b.cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
	b.cmd.ExtraFiles = []*os.File{file}
	b.cmd.Stdout = &b.out
	b.cmd.Stderr = &b.diag
	if err = b.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { b.done <- b.cmd.Wait() }()
	t.Cleanup(func() {
		if b.stopped {
			return
		}
		select {
		case <-b.done:
		default:
			_ = b.cmd.Process.Kill()
			select {
			case <-b.done:
			case <-time.After(5 * time.Second):
				t.Error("child did not stop")
			}
		}
	})
	if err = notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	n, _, err := notify.ReadFromUnix(buf)
	if err != nil || string(buf[:n]) != "READY=1" {
		t.Fatalf("readiness: %q %v", buf[:n], err)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", listener.Addr().String())
	}}
	b.client = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	return b
}
func (b *binaryRun) stop(t *testing.T, sig syscall.Signal, quiet bool) {
	t.Helper()
	if err := b.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-b.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child shutdown deadline")
	}
	b.stopped = true
	if b.out.Len() != 0 || (quiet && b.diag.Len() != 0) {
		t.Fatalf("serve output stdout=%q stderr=%q", b.out.String(), b.diag.String())
	}
	// R-JLF8-W0GN R-C0S8-NC5Z: the inherited socket remains for its other holder.
	if _, err := os.Stat(b.listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("unix", b.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err = b.listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	accepted, err := b.listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	_ = accepted.Close()
}
func (b *binaryRun) request(t *testing.T, path, method string, payload any, headers map[string]string) []byte {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://cron.sbx.ikigenba.dev"+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-Id", "user-alpha")
	req.Header.Set("X-User-Email", "mg@example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("%s: status %d body %s error %v", path, res.StatusCode, data, err)
	}
	return data
}
func (b *binaryRun) tool(t *testing.T, name string, args json.RawMessage) map[string]any {
	t.Helper()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://cron.sbx.ikigenba.dev/mcp", HTTPClient: b.client, Name: "binary test", Version: "test"})
	r, err := client.CallTool(context.Background(), identity.Caller{UserID: "user-alpha", Email: "mg@example.com"}, name, args)
	if err != nil || r.IsError() {
		t.Fatalf("tool %s: %v %+v", name, err, r)
	}
	data, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func binaryPage(t *testing.T, name, display, servicesPath, authBase string) string {
	t.Helper()
	t.Setenv(services.Variable, servicesPath)
	banner := page.New(pages.ServiceName, display).Banner(page.User{Email: "mg@example.com", ProfileURL: authBase + "/", LogoutURL: authBase + "/logout"})
	ts := page.Templates()
	var data any = banner
	if name != "footer" {
		var err error
		ts, err = ts.ParseFS(cron.Assets(), "*.html")
		if err != nil {
			t.Fatal(err)
		}
		if name == "landing" {
			data = pages.LandingData{Banner: banner, Triggers: []pages.TriggerRow{}}
		} else {
			banner.Trail = []page.Level{{Name: "about", URL: "/about"}}
			data = pages.AboutData{Banner: banner, Description: pages.Description}
		}
	}
	var out bytes.Buffer
	if err := ts.ExecuteTemplate(&out, name, data); err != nil {
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
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func entry(name, url, desc, socket string) map[string]any {
	return map[string]any{"name": name, "url": url, "description": desc, "socket": socket, "enabled": true, "mcp": true, "icon": "test-icon"}
}
func rpcResult(t *testing.T, b *binaryRun, method, protocol string) map[string]any {
	t.Helper()
	params := map[string]any{}
	headers := map[string]string{}
	if method == "server/discover" {
		params["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": mcp.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
		headers["MCP-Protocol-Version"] = mcp.ProtocolVersion
		headers["Mcp-Method"] = method
	} else {
		params = map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "earlier", "version": "test"}}
	}
	data := b.request(t, "/mcp", "POST", map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}, headers)
	var response struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Result == nil {
		t.Fatalf("rpc %s: %s %v", method, data, err)
	}
	return response.Result
}

// TestBinary is the module's only test that builds and executes a process.
// R-8FB6-MZVW R-8GJ3-0RML R-8HQZ-EJDA R-8IYV-SB3Z R-3NY2-4C7B R-Z9RX-5C6T
// R-8MMK-XMC2 R-8NUH-BE2R R-8QAA-2XK5 R-8RI6-GPAU R-PD6S-T4SE R-PEEP-6WJ3
// R-4B45-DZAI R-4CC1-RR17
func TestBinary(t *testing.T) {
	t.Setenv(services.Variable, "")
	t.Setenv(version.CommitVariable, "abcdef1234567890abcdef1234567890abcdef12")
	t.Setenv(version.ReleaseVariable, "binary-test-release")
	display := version.Display()
	env := []string{version.CommitVariable + "=abcdef1234567890abcdef1234567890abcdef12", version.ReleaseVariable + "=binary-test-release"}
	bin := filepath.Join(t.TempDir(), "cron")
	build := exec.Command("go", "build")
	build.Args = append(build.Args, "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	for _, tc := range []struct {
		args, env []string
		code      int
		out       string
		diag      string
	}{
		{args: []string{"--version"}, env: env, out: display + "\n"}, {args: []string{"--version"}, out: "\n"}, {args: []string{"manifest"}, env: env, out: cli.Manifest},
		{args: []string{"bogus"}, env: env, code: cli.ExitUsage, diag: "cron: unknown command 'bogus'\n\nsee 'cron --help' for usage\n"},
		{env: env, code: cli.ExitUsage, diag: "cron: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
	} {
		cmd := &exec.Cmd{Path: bin, Args: append([]string{bin}, tc.args...)}
		cmd.Dir = t.TempDir()
		cmd.Env = append([]string{}, tc.env...)
		var out, diag bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &diag
		err := cmd.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != tc.code || out.String() != tc.out || diag.String() != tc.diag {
			t.Fatalf("args %v: code %d out %q diag %q", tc.args, code, out.String(), diag.String())
		}
	}
	dir := t.TempDir()
	for i, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		b := startBinary(t, bin, dir, env)
		info, err := os.Stat(filepath.Join(dir, "state", "cron.db"))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("database %v %v", info, err)
		}
		landing := string(b.request(t, "/", "GET", nil, nil))
		footer := binaryPage(t, "footer", display, "", "https://auth.sbx.ikigenba.dev")
		if !strings.Contains(landing, footer) {
			t.Fatal("footer differs from template")
		}
		about := string(b.request(t, "/about", "GET", nil, nil))
		if about != binaryPage(t, "about", display, "", "https://auth.sbx.ikigenba.dev") {
			t.Fatal("about differs from template")
		}
		result := b.tool(t, "list", nil)
		meta := result["_meta"].(map[string]any)
		wantInfo := map[string]any{"name": pages.ServiceName, "version": display}
		if !reflect.DeepEqual(meta["io.modelcontextprotocol/serverInfo"], wantInfo) {
			t.Fatal(meta)
		}
		if i == 0 {
			before := time.Now().UTC().Truncate(time.Second)
			b.tool(t, "create", json.RawMessage(`{"slug":"alpha","when":"@daily"}`))
			shown := b.tool(t, "show", json.RawMessage(`{"slug":"alpha"}`))["structuredContent"].(map[string]any)
			after := time.Now().UTC()
			created, parseErr := time.Parse(time.RFC3339, shown["created"].(string))
			if parseErr != nil || created.Before(before) || created.After(after) {
				t.Fatalf("default clock stamp %v error %v bounds %v %v", created, parseErr, before, after)
			}
			b.tool(t, "create", json.RawMessage(`{"slug":"beta","when":"@daily"}`))
			second := b.tool(t, "show", json.RawMessage(`{"slug":"beta"}`))["structuredContent"].(map[string]any)
			idPattern := regexp.MustCompile(`^crn_[0-9a-f]{16}$`)
			firstID, secondID := shown["id"].(string), second["id"].(string)
			if !idPattern.MatchString(firstID) || !idPattern.MatchString(secondID) || firstID == secondID {
				t.Fatalf("default random ids %q %q", firstID, secondID)
			}
			b.tool(t, "delete", json.RawMessage(`{"slug":"beta"}`))
		} else {
			triggers := result["structuredContent"].(map[string]any)["triggers"].([]any)
			if len(triggers) != 1 || triggers[0].(map[string]any)["slug"] != "alpha" {
				t.Fatal(triggers)
			}
		}
		if _, ok := rpcResult(t, b, "server/discover", "")["instructions"]; ok {
			t.Fatal("unset instructions")
		}
		for _, protocol := range []string{"2025-11-25", "2025-06-18"} {
			r := rpcResult(t, b, "initialize", protocol)
			if !reflect.DeepEqual(r["serverInfo"], wantInfo) {
				t.Fatal(r)
			}
			if _, ok := r["instructions"]; ok {
				t.Fatal("legacy unset instructions")
			}
		}
		b.stop(t, sig, false)
	}

	// Unset identity is also propagated to served pages and MCP.
	emptyRun := startBinary(t, bin, t.TempDir(), []string{})
	emptyLanding := string(emptyRun.request(t, "/", "GET", nil, nil))
	if !strings.Contains(emptyLanding, binaryPage(t, "footer", "", "", "https://auth.sbx.ikigenba.dev")) {
		t.Fatal("empty display footer")
	}
	emptyAbout := string(emptyRun.request(t, "/about", "GET", nil, nil))
	if emptyAbout != binaryPage(t, "about", "", "", "https://auth.sbx.ikigenba.dev") {
		t.Fatal("empty display about")
	}
	emptyInfo := emptyRun.tool(t, "list", nil)["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"]
	if !reflect.DeepEqual(emptyInfo, map[string]any{"name": pages.ServiceName, "version": ""}) {
		t.Fatal(emptyInfo)
	}
	emptyRun.stop(t, syscall.SIGTERM, false)
	// Services-driven discovery and page chrome update without a restart.
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	entries := []map[string]any{entry("auth", "https://auth.alternate.example", "auth", "/unused/auth"), entry("dummy", "https://dummy.example", "dummy", "/unused/dummy"), entry("cron", "https://cron.example", "published cron description", "/unused/cron")}
	writeServices(t, servicesPath, entries)
	b := startBinary(t, bin, t.TempDir(), append(append([]string{}, env...), services.Variable+"="+servicesPath))
	checkInstructions := func(want string) {
		for _, method := range []string{"server/discover", "initialize"} {
			protocols := []string{""}
			if method == "initialize" {
				protocols = []string{"2025-11-25", "2025-06-18"}
			}
			for _, p := range protocols {
				r := rpcResult(t, b, method, p)
				v, ok := r["instructions"]
				if want == "" {
					if ok {
						t.Fatal("unexpected instructions", r)
					}
				} else if v != want {
					t.Fatal("instructions", r)
				}
				if method == "initialize" && !reflect.DeepEqual(r["serverInfo"], map[string]any{"name": pages.ServiceName, "version": display}) {
					t.Fatal(r)
				}
			}
		}
	}
	checkInstructions("published cron description")
	body := string(b.request(t, "/", "GET", nil, nil))
	if body != binaryPage(t, "landing", display, servicesPath, "https://auth.alternate.example") {
		t.Fatal("services landing differs from template")
	}
	entries[1]["enabled"] = false
	entries[2]["description"] = "changed published description"
	writeServices(t, servicesPath, entries)
	changed := string(b.request(t, "/", "GET", nil, nil))
	if changed == body || changed != binaryPage(t, "landing", display, servicesPath, "https://auth.alternate.example") {
		t.Fatal("rewritten services landing differs")
	}
	checkInstructions("changed published description")
	// The file is read afresh: no cron entry, malformed, missing, and empty description.
	writeServices(t, servicesPath, entries[:2])
	checkInstructions("")
	if err := os.WriteFile(servicesPath, []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	checkInstructions("")
	if err := os.Remove(servicesPath); err != nil {
		t.Fatal(err)
	}
	checkInstructions("")
	missing := string(b.request(t, "/", "GET", nil, nil))
	if missing != binaryPage(t, "landing", display, servicesPath, "https://auth.sbx.ikigenba.dev") {
		t.Fatal("missing services landing differs from template")
	}
	entries[2]["description"] = ""
	writeServices(t, servicesPath, entries)
	checkInstructions("")
	b.stop(t, syscall.SIGTERM, false)
	// A configured telemetry socket receives the binary's lifecycle and MCP trail.
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			short, err := os.MkdirTemp("", "cron-trail-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(short) }()
			l, err := net.Listen("unix", filepath.Join(short, "trail.sock"))
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var recorded []map[string]any
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/ingest" {
					t.Errorf("ingest route %s %s", r.Method, r.URL.Path)
				}
				var e map[string]any
				if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
					t.Error(err)
				}
				mu.Lock()
				recorded = append(recorded, e)
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			})}
			go func() { _ = server.Serve(l) }()
			defer func() { _ = server.Close() }()
			path := filepath.Join(short, "services.json")
			writeServices(t, path, []map[string]any{entry("telemetry", "https://telemetry.example", "trail", l.Addr().String())})
			run := startBinary(t, bin, t.TempDir(), append(append([]string{}, env...), services.Variable+"="+path))
			run.tool(t, "list", nil)
			run.stop(t, sig, true)
			mu.Lock()
			defer mu.Unlock()
			if len(recorded) < 5 {
				t.Fatalf("events %v", recorded)
			}
			first, last := recorded[0], recorded[len(recorded)-1]
			reason := "SIGTERM"
			if sig == syscall.SIGINT {
				reason = "SIGINT"
			}
			if first["service"] != "cron" || first["event"] != "service.started" || first["request_id"] != "" || first["user"] != "" || !reflect.DeepEqual(first["attrs"], map[string]any{"version": display}) {
				t.Fatal(first)
			}
			if last["event"] != "service.stopping" || last["request_id"] != "" || last["user"] != "" || !reflect.DeepEqual(last["attrs"], map[string]any{"reason": reason}) {
				t.Fatal(last)
			}
			toolIndex := -1
			for i, e := range recorded {
				if e["event"] == "tool.called" {
					attrs := e["attrs"].(map[string]any)
					if e["service"] != "cron" || e["user"] != "user-alpha" || attrs["tool"] != "list" || attrs["kind"] != "read" || attrs["outcome"] != "ok" {
						t.Fatal(e)
					}
					toolIndex = i
				}
			}
			if toolIndex < 0 {
				t.Fatal("missing tool event")
			}
			rid := recorded[toolIndex]["request_id"]
			start, finish := -1, -1
			for i, e := range recorded {
				if e["request_id"] == rid {
					if e["event"] == "request.started" {
						start = i
					}
					if e["event"] == "request.finished" {
						finish = i
					}
				}
			}
			if start < 0 || start >= toolIndex || finish <= toolIndex {
				t.Fatal("request event order", recorded)
			}
		})
	}
}
