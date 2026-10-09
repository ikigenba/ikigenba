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
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/cli"
	"github.com/ikigenba/ikigenba/webhooks/internal/pages"
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
	short, err := os.MkdirTemp("", "webhooks-")
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
	b.client = &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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
}

func (b *binaryRun) send(t *testing.T, method, path string, body []byte, headers map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, "http://webhooks.sbx.ikigenba.dev"+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, data
}

func (b *binaryRun) page(t *testing.T, path string) string {
	t.Helper()
	code, data := b.send(t, "GET", path, nil, map[string]string{"X-User-Id": "user-alpha", "X-User-Email": "mg@example.com"})
	if code != 200 {
		t.Fatalf("%s: %d %s", path, code, data)
	}
	return string(data)
}

func (b *binaryRun) tool(t *testing.T, name string, args json.RawMessage) map[string]any {
	t.Helper()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://webhooks.sbx.ikigenba.dev/mcp", HTTPClient: b.client, Name: "binary test", Version: "test"})
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

func aboutPage(t *testing.T, display string) string {
	t.Helper()
	t.Setenv(services.Variable, "")
	banner := page.New(pages.ServiceName, display).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
	banner.Trail = []page.Level{pages.AboutLevel}
	ts, err := page.Templates().ParseFS(webhooks.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := ts.ExecuteTemplate(&out, "about", pages.AboutData{Banner: banner, Description: pages.Description}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// TestBinary is the module's only test that builds and executes a process.
// R-XB0S-06R3 R-XC8O-DYHS R-XDGK-RQ8H R-XEOH-5HZ6 R-XFWD-J9PV
func TestBinary(t *testing.T) {
	t.Setenv(services.Variable, "")
	t.Setenv(version.CommitVariable, "abcdef1234567890abcdef1234567890abcdef12")
	t.Setenv(version.ReleaseVariable, "binary-test-release")
	display := version.Display()
	env := []string{version.CommitVariable + "=abcdef1234567890abcdef1234567890abcdef12", version.ReleaseVariable + "=binary-test-release"}
	bin := filepath.Join(t.TempDir(), "webhooks")
	build := exec.Command("go", "build")
	build.Args = append(build.Args, "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	for _, tc := range []struct {
		args, env []string
		code      int
		out, diag string
	}{
		{args: []string{"--version"}, env: env, out: display + "\n"},
		{args: []string{"--version"}, out: "\n"},
		{args: []string{"manifest"}, env: env, out: cli.Manifest},
		{args: []string{"bogus"}, env: env, code: cli.ExitUsage, diag: "webhooks: unknown command 'bogus'\n\nsee 'webhooks --help' for usage\n"},
		{env: env, code: cli.ExitUsage, diag: "webhooks: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
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
	wantInfo := map[string]any{"name": pages.ServiceName, "version": display}
	for i, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		b := startBinary(t, bin, dir, env)
		info, err := os.Stat(filepath.Join(dir, "state", "webhooks.db"))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("database %v %v", info, err)
		}
		if about := b.page(t, "/about"); about != aboutPage(t, display) || !strings.Contains(about, display) {
			t.Fatal("about differs from template")
		}
		result := b.tool(t, "list", nil)
		meta := result["_meta"].(map[string]any)
		if !reflect.DeepEqual(meta["io.modelcontextprotocol/serverInfo"], wantInfo) {
			t.Fatal(meta)
		}
		if i == 0 {
			minted := b.tool(t, "create", json.RawMessage(`{"slug":"tick"}`))["structuredContent"].(map[string]any)
			secret, _ := minted["secret"].(string)
			if !strings.HasPrefix(secret, "whs_") || minted["url"] != "https://webhooks.sbx.ikigenba.dev/in/tick" {
				t.Fatalf("minted %+v", minted)
			}
			// A guest sender: no identity, only the secret.
			if code, body := b.send(t, "POST", "/in/tick", []byte(`{"ref":"main"}`), map[string]string{"X-Webhook-Secret": secret, "Content-Type": "application/json"}); code != http.StatusAccepted || len(body) != 0 {
				t.Fatalf("ingress %d %q", code, body)
			}
			if code, _ := b.send(t, "GET", "/", nil, nil); code != http.StatusFound {
				t.Fatalf("guest landing %d", code)
			}
		} else {
			hooks := result["structuredContent"].(map[string]any)["webhooks"].([]any)
			if len(hooks) != 1 || hooks[0].(map[string]any)["slug"] != "tick" || hooks[0].(map[string]any)["last_received"] == nil {
				t.Fatal(hooks)
			}
		}
		b.stop(t, sig, false)
	}
	// An unset identity reaches the about screen and the server info as the empty string.
	emptyRun := startBinary(t, bin, t.TempDir(), []string{})
	if emptyRun.page(t, "/about") != aboutPage(t, "") {
		t.Fatal("empty display about")
	}
	emptyInfo := emptyRun.tool(t, "list", nil)["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"]
	if !reflect.DeepEqual(emptyInfo, map[string]any{"name": pages.ServiceName, "version": ""}) {
		t.Fatal(emptyInfo)
	}
	emptyRun.stop(t, syscall.SIGINT, false)
	// A configured telemetry socket receives the lifecycle; the stop is quiet.
	short, err := os.MkdirTemp("", "webhooks-trail-")
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
	data, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "telemetry", "url": "https://telemetry.example", "description": "trail", "socket": l.Addr().String(), "enabled": true, "mcp": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	run := startBinary(t, bin, t.TempDir(), append(append([]string{}, env...), services.Variable+"="+path))
	run.tool(t, "list", nil)
	run.stop(t, syscall.SIGTERM, true)
	mu.Lock()
	defer mu.Unlock()
	if len(recorded) < 2 {
		t.Fatalf("events %v", recorded)
	}
	first, last := recorded[0], recorded[len(recorded)-1]
	if first["service"] != "webhooks" || first["event"] != "service.started" || first["request_id"] != "" || first["user"] != "" || !reflect.DeepEqual(first["attrs"], map[string]any{"version": display}) {
		t.Fatal(first)
	}
	if last["event"] != "service.stopping" || last["request_id"] != "" || last["user"] != "" || !reflect.DeepEqual(last["attrs"], map[string]any{"reason": "SIGTERM"}) {
		t.Fatal(last)
	}
}
