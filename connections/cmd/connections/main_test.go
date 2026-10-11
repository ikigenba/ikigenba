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
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/cli"
	"github.com/ikigenba/ikigenba/connections/internal/pages"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
)

// R-0YTQ-UE2P R-101N-85TE R-119J-LXK3 R-12HF-ZPAS R-M1GL-NUHB R-14X8-R8S6 R-1655-50IV R-17D1-IS9K
func TestBinary(t *testing.T) {
	t.Setenv(services.Variable, "")
	t.Setenv(version.CommitVariable, "abcdef0123456789")
	t.Setenv(version.ReleaseVariable, "test-release")
	id := version.Read()
	display := id.String()
	binary := filepath.Join(t.TempDir(), "connections")
	build := exec.Command("go")
	build.Args = append(build.Args, "build", "-o", binary, ".")
	b, e := build.CombinedOutput()
	if e != nil {
		t.Fatalf("build: %v %s", e, b)
	}
	base := []string{version.CommitVariable + "=abcdef0123456789", version.ReleaseVariable + "=test-release", "GOOGLE_CLIENT_ID=chosen-client", "GOOGLE_CLIENT_SECRET=chosen-secret", "IKIGENBA_CALLBACK_URL=https://callback.example:7443"}
	for _, tc := range []struct {
		args     []string
		env      []string
		out, err string
		code     int
	}{{[]string{"--version"}, base, display + "\n", "", 0}, {[]string{"--version"}, []string{}, "\n", "", 0}, {[]string{"manifest"}, base, cli.Manifest, "", 0}, {[]string{"bogus"}, base, "", "connections: unknown command 'bogus'\n\nsee 'connections --help' for usage\n", 2}, {nil, []string{"GOOGLE_CLIENT_SECRET=secret"}, "", "connections: GOOGLE_CLIENT_ID is not set\n", 2}, {nil, base, "", "connections: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", 2}} {
		cmd := &exec.Cmd{Path: binary, Args: append([]string{binary}, tc.args...)}
		cmd.Env = tc.env
		cmd.Dir = t.TempDir()
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		e = cmd.Run()
		code := 0
		if e != nil {
			var ee *exec.ExitError
			if !errors.As(e, &ee) {
				t.Fatal(e)
			}
			code = ee.ExitCode()
		}
		if code != tc.code || out.String() != tc.out || stderr.String() != tc.err {
			t.Fatalf("args %v: %d %q %q", tc.args, code, out.String(), stderr.String())
		}
	}
	short, e := os.MkdirTemp("", "binary-")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = os.RemoveAll(short) }()
	dir := t.TempDir()
	capture := &telemetry.Capture{}
	telemetryLN, e := net.Listen("unix", filepath.Join(short, "telemetry"))
	if e != nil {
		t.Fatal(e)
	}
	telemetryServer := &http.Server{Handler: telemetry.IngestHandler(capture), ReadHeaderTimeout: time.Second}
	telemetryDone := make(chan error, 1)
	go func() { telemetryDone <- telemetryServer.Serve(telemetryLN) }()
	defer func() {
		_ = telemetryServer.Close()
		select {
		case <-telemetryDone:
		case <-time.After(3 * time.Second):
			t.Error("telemetry server did not stop")
		}
	}()
	file := filepath.Join(short, "services.json")
	data, e := json.Marshal(map[string]any{"services": []map[string]any{{"name": "telemetry", "url": "https://telemetry.example", "description": "test", "socket": telemetryLN.Addr().String(), "enabled": true, "mcp": false}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(file, data, 0600); e != nil {
		t.Fatal(e)
	}
	var state string
	for run, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		ln, e := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(short, "app"), Net: "unix"})
		if e != nil {
			t.Fatal(e)
		}
		ln.SetUnlinkOnClose(false)
		fd, e := ln.File()
		if e != nil {
			t.Fatal(e)
		}
		notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify"), Net: "unixgram"})
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command("/bin/sh")
		cmd.Args = append(cmd.Args, "-c", "LISTEN_PID=$$ LISTEN_FDS=1 exec \"$0\"", binary)
		cmd.Dir = dir
		cmd.ExtraFiles = []*os.File{fd}
		cmd.Env = append(append([]string{}, base...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
		if run == 0 {
			cmd.Env = append(cmd.Env, services.Variable+"="+file)
		}
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		_ = fd.Close()
		_ = ln.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
			}
		})
		if e = notify.SetReadDeadline(time.Now().Add(5 * time.Second)); e != nil {
			t.Fatal(e)
		}
		b = make([]byte, 100)
		n, _, readErr := notify.ReadFromUnix(b)
		_ = notify.Close()
		_ = os.Remove(filepath.Join(short, "notify"))
		if readErr != nil || string(b[:n]) != "READY=1" {
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("killed binary did not finish")
			}
			t.Fatalf("ready: %v %q %s", readErr, b[:n], stderr.String())
		}
		stat, e := os.Stat(filepath.Join(dir, "state", "connections.db"))
		if e != nil || !stat.Mode().IsRegular() {
			t.Fatal(stat, e)
		}
		client := &http.Client{Transport: telemetry.SocketTransport(filepath.Join(short, "app")), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 3 * time.Second}
		request := func(path string) *http.Response {
			req, e := http.NewRequest("GET", "http://connections.sbx.ikigenba.dev"+path, nil)
			if e != nil {
				t.Fatal(e)
			}
			req.Host = "connections.sbx.ikigenba.dev"
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-User-Id", "user")
			req.Header.Set("X-User-Email", "mg@example.com")
			res, e := client.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			return res
		}
		if run == 0 {
			res := request("/google/connect")
			u, e := res.Location()
			_ = res.Body.Close()
			if e != nil || res.StatusCode != 302 || !strings.HasPrefix(u.String(), providers.Table()[0].AuthorizeURL+"?") {
				t.Fatal(res.StatusCode, u, e)
			}
			state = u.Query().Get("state")
		} else {
			res := request("/google/callback?error=access_denied&state=" + state)
			got, e := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			templates, e := page.Templates().ParseFS(connections.Assets(), "*.html")
			if e != nil {
				t.Fatal(e)
			}
			banner := page.New(pages.ServiceName, id).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
			var want bytes.Buffer
			if e = templates.ExecuteTemplate(&want, "notice", pages.NoticeData{Banner: banner, DisplayName: providers.Table()[0].DisplayName, Failure: pages.FailureDenied}); e != nil {
				t.Fatal(e)
			}
			if res.StatusCode != 400 || !bytes.Equal(got, want.Bytes()) {
				t.Fatalf("callback %d got %s want %s", res.StatusCode, got, want.Bytes())
			}
			res = request("/about")
			got, e = io.ReadAll(res.Body)
			_ = res.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			banner.Trail = []page.Level{pages.AboutLevel}
			want.Reset()
			if e = templates.ExecuteTemplate(&want, "about", pages.AboutData{Banner: banner, Description: pages.Description}); e != nil {
				t.Fatal(e)
			}
			if res.StatusCode != 200 || !bytes.Equal(got, want.Bytes()) {
				t.Fatalf("about %d got %s want %s", res.StatusCode, got, want.Bytes())
			}
		}
		mc := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://connections.sbx.ikigenba.dev/mcp", HTTPClient: client})
		result, e := mc.CallTool(context.Background(), identity.Caller{UserID: "user"}, "list", nil)
		if e != nil {
			t.Fatal(e)
		}
		encoded, e := json.Marshal(result)
		if e != nil {
			t.Fatal(e)
		}
		var object map[string]json.RawMessage
		if e = json.Unmarshal(encoded, &object); e != nil {
			t.Fatal(e)
		}
		var meta map[string]json.RawMessage
		if e = json.Unmarshal(object["_meta"], &meta); e != nil {
			t.Fatal(e)
		}
		var info map[string]string
		if e = json.Unmarshal(meta["io.modelcontextprotocol/serverInfo"], &info); e != nil || !reflect.DeepEqual(info, map[string]string{"name": pages.ServiceName, "version": display}) {
			t.Fatal(info, e)
		}
		if e = cmd.Process.Signal(signal); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-done:
			if e != nil {
				t.Fatal(e, stderr.String())
			}
		case <-time.After(7 * time.Second):
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("killed binary did not finish")
			}
			t.Fatal("binary did not stop")
		}
		client.CloseIdleConnections()
		_ = os.Remove(filepath.Join(short, "app"))
		if out.Len() != 0 || run == 0 && stderr.Len() != 0 {
			t.Fatal(out.String(), stderr.String())
		}
		if run == 1 {
			lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
			var stopping struct {
				Name  string          `json:"event"`
				Attrs telemetry.Attrs `json:"attrs"`
			}
			last := lines[len(lines)-1]
			_, payload, ok := strings.Cut(last, ": undelivered event: ")
			if !ok || json.Unmarshal([]byte(payload), &stopping) != nil || stopping.Name != "service.stopping" || !reflect.DeepEqual(stopping.Attrs, telemetry.Attrs{"reason": "SIGINT"}) {
				t.Fatal(last)
			}
		}
	}
	es := capture.Events()
	if len(es) < 2 || es[0].Name != "service.started" || !reflect.DeepEqual(es[0].Attrs, telemetry.Attrs{"version": display}) || es[len(es)-1].Name != "service.stopping" || !reflect.DeepEqual(es[len(es)-1].Attrs, telemetry.Attrs{"reason": "SIGTERM"}) {
		t.Fatal(es)
	}
}
