package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
	"github.com/ikigenba/ikigenba/campaigns/internal/cli"
	"github.com/ikigenba/ikigenba/campaigns/internal/pages"
	"github.com/ikigenba/ikigenba/campaigns/internal/tools"
)

// R-B1K4-CN78 R-B2S0-QEXX R-B3ZX-46OM R-B57T-HYFB R-B6FP-VQ60 R-B7NM-9HWP R-B8VI-N9NE
func TestBinary(t *testing.T) {
	t.Setenv(services.Variable, "")
	const commit = "abcdeff01234567890-dirty"
	const release = "binary-fixture"
	t.Setenv(version.CommitVariable, commit)
	t.Setenv(version.ReleaseVariable, release)
	id := version.Read()
	display := id.String()
	bin := filepath.Join(t.TempDir(), "campaigns")
	build := exec.Command("go", "build", "-o")
	build.Args = append(build.Args, bin, ".")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %s: %v", b, e)
	}
	env := []string{version.CommitVariable + "=" + commit, version.ReleaseVariable + "=" + release}
	for _, c := range []struct {
		args        []string
		env         []string
		out, errout string
		code        int
	}{{[]string{"--version"}, env, display + "\n", "", 0}, {[]string{"--version"}, nil, "\n", "", 0}, {[]string{"manifest"}, env, cli.Manifest, "", 0}, {[]string{"bogus"}, env, "", "campaigns: unknown command 'bogus'\n\nsee 'campaigns --help' for usage\n", 2}, {nil, env, "", "campaigns: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", 2}} {
		cmd := exec.Command("/bin/sh", "-c", `exec "$0" "$@"`)
		cmd.Args = append(cmd.Args, bin)
		cmd.Args = append(cmd.Args, c.args...)
		cmd.Env = append([]string{}, c.env...)
		cmd.Dir = t.TempDir()
		var out, errout bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errout
		e := cmd.Run()
		code := 0
		if e != nil {
			var ee *exec.ExitError
			if !errors.As(e, &ee) {
				t.Fatal(e)
			}
			code = ee.ExitCode()
		}
		if code != c.code || out.String() != c.out || errout.String() != c.errout {
			t.Fatalf("%v: code %d out %q err %q", c.args, code, out.String(), errout.String())
		}
	}
	socketDir, e := os.MkdirTemp("", "campaigns-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	captureSocket := filepath.Join(socketDir, "capture")
	captureLn, e := net.Listen("unix", captureSocket)
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	var captured []telemetry.Event
	captureServer := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ingest" {
			var wire struct {
				Event string          `json:"event"`
				Attrs telemetry.Attrs `json:"attrs"`
			}
			if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
				t.Error(err)
			}
			mu.Lock()
			captured = append(captured, telemetry.Event{Name: wire.Event, Attrs: wire.Attrs})
			mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = captureServer.Serve(captureLn) }()
	defer func() { _ = captureServer.Close() }()
	servicePath := filepath.Join(t.TempDir(), "services.json")
	serviceBytes, e := json.Marshal(map[string]any{"services": []map[string]any{{"name": "telemetry", "url": "https://telemetry.test", "description": "capture", "socket": captureSocket, "enabled": true, "mcp": false}, {"name": "events", "url": "https://events.test", "description": "capture", "socket": captureSocket, "enabled": true, "mcp": false}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(servicePath, serviceBytes, 0600); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	var campaign string
	for i, signal := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		socket := filepath.Join(socketDir, "serve"+string(rune('0'+i)))
		ln, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
		if e != nil {
			t.Fatal(e)
		}
		ln.SetUnlinkOnClose(false)
		file, e := ln.File()
		if e != nil {
			t.Fatal(e)
		}
		notify := filepath.Join(socketDir, "notify"+string(rune('0'+i)))
		nc, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notify, Net: "unixgram"})
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`)
		cmd.Args = append(cmd.Args, bin)
		cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify)
		if i == 0 {
			cmd.Env = append(cmd.Env, services.Variable+"="+servicePath)
		}
		cmd.Dir = dir
		cmd.ExtraFiles = []*os.File{file}
		var out, errout bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errout
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		_ = file.Close()
		_ = ln.Close()
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		if e = nc.SetReadDeadline(time.Now().Add(5 * time.Second)); e != nil {
			t.Fatal(e)
		}
		b := make([]byte, 64)
		n, _, e := nc.ReadFromUnix(b)
		_ = nc.Close()
		if e != nil || string(b[:n]) != "READY=1" {
			t.Fatalf("ready: %q %v", b[:n], e)
		}
		info, e := os.Stat(filepath.Join(dir, "state", "campaigns.db"))
		if e != nil || !info.Mode().IsRegular() {
			t.Fatal(info, e)
		}
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}}
		hc := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://campaigns/mcp", HTTPClient: hc})
		caller := identity.Caller{UserID: "owner", Email: "mgreenly+fake00@gmail.com", RequestID: "binary-request"}
		if i == 0 {
			result, e := client.CallTool(context.Background(), caller, "create_campaign", json.RawMessage(`{"name":"binary campaign","channels":["mail"],"destination":"https://destination.test/"}`))
			if e != nil || result.IsError() {
				t.Fatal(result, e)
			}
			var raw struct {
				Structured tools.Campaign `json:"structuredContent"`
			}
			j, _ := json.Marshal(result)
			if e = json.Unmarshal(j, &raw); e != nil {
				t.Fatal(e)
			}
			campaign = raw.Structured.ID
			if campaign == "" {
				t.Fatalf("missing campaign: %s", j)
			}
		}
		result, e := client.CallTool(context.Background(), caller, "campaigns", nil)
		if e != nil || result.IsError() {
			t.Fatal(result, e)
		}
		j, e := json.Marshal(result)
		if e != nil {
			t.Fatal(e)
		}
		var raw struct {
			Meta       map[string]json.RawMessage `json:"_meta"`
			Structured struct {
				Campaigns []tools.CampaignSummary `json:"campaigns"`
			} `json:"structuredContent"`
		}
		if e = json.Unmarshal(j, &raw); e != nil {
			t.Fatal(e)
		}
		var serverInfo map[string]string
		if e = json.Unmarshal(raw.Meta["io.modelcontextprotocol/serverInfo"], &serverInfo); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(serverInfo, map[string]string{"name": pages.ServiceName, "version": display}) {
			t.Fatal(serverInfo)
		}
		if len(raw.Structured.Campaigns) != 1 || raw.Structured.Campaigns[0].ID != campaign {
			t.Fatalf("persistence: %s", j)
		}
		if i == 1 {
			req, e := http.NewRequest(http.MethodGet, "http://campaigns/about", nil)
			if e != nil {
				t.Fatal(e)
			}
			req.Host = "campaigns.sbx.ikigenba.dev"
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-User-Id", caller.UserID)
			req.Header.Set("X-User-Email", caller.Email)
			response, e := hc.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			body, e := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			set, e := pages.Load()
			if e != nil {
				t.Fatal(e)
			}
			banner := page.New(pages.ServiceName, id).Banner(page.User{Email: caller.Email, ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
			banner.Trail = []page.Level{pages.AboutLevel}
			want := httptest.NewRecorder()
			set.Write(want, req, 200, "about", pages.AboutData{Banner: banner, Description: pages.Description})
			if response.StatusCode != 200 || !bytes.Equal(body, want.Body.Bytes()) {
				t.Fatal("about identity differs")
			}
		}
		transport.CloseIdleConnections()
		if e = cmd.Process.Signal(signal); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-exited:
			if e != nil {
				t.Fatal(e, errout.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("child did not stop")
		}
		if out.Len() != 0 {
			t.Fatal(out.String())
		}
		if i == 0 && errout.Len() != 0 {
			t.Fatal(errout.String())
		}
		if i == 1 {
			lines := strings.Split(strings.TrimSpace(errout.String()), "\n")
			for j, line := range lines {
				const prefix = "campaigns: undelivered event: "
				if !strings.HasPrefix(line, prefix) {
					t.Fatal(line)
				}
				var ev struct {
					Name  string          `json:"event"`
					Attrs telemetry.Attrs `json:"attrs"`
				}
				if e = json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &ev); e != nil {
					t.Fatal(e)
				}
				if j == len(lines)-1 && (ev.Name != "service.stopping" || !reflect.DeepEqual(ev.Attrs, telemetry.Attrs{"reason": "SIGINT"})) {
					t.Fatal(ev)
				}
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) < 2 {
		t.Fatal("missing lifecycle")
	}
	first, last := captured[0], captured[len(captured)-1]
	if first.Name != "service.started" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": display}) || last.Name != "service.stopping" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "SIGTERM"}) {
		t.Fatal(first, last)
	}
}
