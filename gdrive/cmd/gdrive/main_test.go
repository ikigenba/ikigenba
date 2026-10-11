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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/cli"
	"github.com/ikigenba/ikigenba/gdrive/internal/pages"
)

// R-RL57-RG84 R-RMD4-57YT R-RNL0-IZPI R-ROSW-WRG7 R-RQ0T-AJ6W R-RR8P-OAXL R-RSGM-22OA
func TestBinary(t *testing.T) {
	t.Setenv(services.Variable, "")
	t.Setenv(version.CommitVariable, "abcdef0123456789")
	t.Setenv(version.ReleaseVariable, "test-release")
	id := version.Read()
	display := id.String()
	dir := t.TempDir()
	binary := filepath.Join(dir, "gdrive")
	goPath, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	build := &exec.Cmd{Path: goPath, Args: []string{"go", "build", "-o", binary, "."}}
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, b)
	}
	env := []string{version.CommitVariable + "=abcdef0123456789", version.ReleaseVariable + "=test-release"}
	for _, c := range []struct {
		args        []string
		env         []string
		out, stderr string
		code        int
	}{{[]string{"--version"}, env, display + "\n", "", 0}, {[]string{"manifest"}, env, cli.Manifest, "", 0}, {[]string{"bogus"}, env, "", "gdrive: unknown command 'bogus'\n\nsee 'gdrive --help' for usage\n", 2}, {nil, env, "", "gdrive: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", 2}, {[]string{"--version"}, []string{}, "\n", "", 0}} {
		cmd := &exec.Cmd{Path: binary, Args: append([]string{binary}, c.args...)}
		cmd.Dir = t.TempDir()
		cmd.Env = c.env
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		e := cmd.Run()
		code := 0
		if e != nil {
			var ee *exec.ExitError
			if !errors.As(e, &ee) {
				t.Fatal(e)
			}
			code = ee.ExitCode()
		}
		if code != c.code || out.String() != c.out || stderr.String() != c.stderr {
			t.Fatalf("args %v: %d %q %q", c.args, code, out.String(), stderr.String())
		}
	}
	socketDir, e := os.MkdirTemp("", "gdrive-test-")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = os.RemoveAll(socketDir) }()
	telemetrySocket := filepath.Join(socketDir, "telemetry.sock")
	tl, e := net.Listen("unix", telemetrySocket)
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	var captured []telemetry.Event
	ts := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire struct {
			Name  string          `json:"event"`
			Attrs telemetry.Attrs `json:"attrs"`
		}
		if r.Method != "POST" || r.URL.Path != "/ingest" || json.NewDecoder(r.Body).Decode(&wire) != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		captured = append(captured, telemetry.Event{Name: wire.Name, Attrs: wire.Attrs})
		mu.Unlock()
		w.WriteHeader(204)
	})}
	go func() { _ = ts.Serve(tl) }()
	defer func() { _ = ts.Close() }()
	servicePath := filepath.Join(socketDir, "services.json")
	b, _ := json.Marshal(map[string]any{"services": []map[string]any{{"name": "telemetry", "socket": telemetrySocket, "enabled": true, "mcp": false, "url": "", "description": ""}}})
	if e = os.WriteFile(servicePath, b, 0600); e != nil {
		t.Fatal(e)
	}
	for i, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		work := t.TempDir()
		socket := filepath.Join(socketDir, "app"+string(rune('a'+i))+".sock")
		ln, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
		if e != nil {
			t.Fatal(e)
		}
		ln.SetUnlinkOnClose(false)
		f, e := ln.File()
		if e != nil {
			t.Fatal(e)
		}
		notify := filepath.Join(socketDir, "notify"+string(rune('a'+i))+".sock")
		nc, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notify, Net: "unixgram"})
		if e != nil {
			t.Fatal(e)
		}
		cmd := &exec.Cmd{Path: "/bin/sh", Args: []string{"/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary}}
		cmd.Dir = work
		cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify)
		if i == 0 {
			cmd.Env = append(cmd.Env, services.Variable+"="+servicePath)
		}
		cmd.ExtraFiles = []*os.File{f}
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		_ = f.Close()
		_ = ln.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		defer func() { _ = cmd.Process.Kill() }()
		_ = nc.SetReadDeadline(time.Now().Add(10 * time.Second))
		buf := make([]byte, 128)
		n, _, e := nc.ReadFromUnix(buf)
		_ = nc.Close()
		if e != nil || string(buf[:n]) != "READY=1" {
			t.Fatalf("ready %q %v", buf[:n], e)
		}
		info, e := os.Stat(filepath.Join(work, "state", "gdrive.db"))
		if e != nil || !info.Mode().IsRegular() {
			t.Fatalf("database: %v", e)
		}
		hc := &http.Client{Transport: &http.Transport{DialContext: func(c context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(c, "unix", socket)
		}}, Timeout: 5 * time.Second}
		client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://gdrive.invalid/mcp", HTTPClient: hc})
		result, e := client.CallTool(context.Background(), identity.Caller{UserID: "binary-user", Email: "mg@example.com", RequestID: "binary-request"}, "transfers", nil)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := json.Marshal(result)
		if e != nil {
			t.Fatal(e)
		}
		var decoded struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		}
		if e = json.Unmarshal(raw, &decoded); e != nil {
			t.Fatal(e)
		}
		var serverInfo map[string]string
		if e = json.Unmarshal(decoded.Meta["io.modelcontextprotocol/serverInfo"], &serverInfo); e != nil || !reflect.DeepEqual(serverInfo, map[string]string{"name": pages.ServiceName, "version": display}) {
			t.Fatalf("metadata %s %v", raw, e)
		}
		if i == 1 {
			req, e := http.NewRequest("GET", "http://gdrive.sbx.ikigenba.dev/about", nil)
			if e != nil {
				t.Fatal(e)
			}
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-User-Id", "binary-user")
			req.Header.Set("X-User-Email", "mg@example.com")
			r, e := hc.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			body, e := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			templates, e := page.Templates().ParseFS(gdrive.Assets(), "*.html")
			if e != nil {
				t.Fatal(e)
			}
			banner := page.New(pages.ServiceName, id).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
			banner.Trail = []page.Level{pages.AboutLevel}
			var expected bytes.Buffer
			if e = templates.ExecuteTemplate(&expected, "about", pages.AboutData{Description: pages.Description, Banner: banner}); e != nil {
				t.Fatal(e)
			}
			if r.StatusCode != 200 || !bytes.Equal(body, expected.Bytes()) {
				t.Fatalf("about body mismatch status %d", r.StatusCode)
			}
		}
		if e = cmd.Process.Signal(sig); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-done:
			if e != nil {
				t.Fatalf("stop: %v stderr %s", e, stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Fatal("binary failed to stop")
		}
		if out.Len() != 0 {
			t.Fatal("serve wrote stdout")
		}
		if i == 0 {
			if stderr.Len() != 0 {
				t.Fatalf("stderr %q", stderr.String())
			}
			mu.Lock()
			evs := append([]telemetry.Event{}, captured...)
			mu.Unlock()
			if len(evs) < 2 || evs[0].Name != "service.started" || !reflect.DeepEqual(evs[0].Attrs, telemetry.Attrs{"version": display}) || evs[len(evs)-1].Name != "service.stopping" || !reflect.DeepEqual(evs[len(evs)-1].Attrs, telemetry.Attrs{"reason": "SIGTERM"}) {
				t.Fatalf("events %+v", evs)
			}
		} else if !strings.Contains(stderr.String(), `"reason":"SIGINT"`) {
			t.Fatalf("signal cause missing: %s", stderr.String())
		}
	}
}
