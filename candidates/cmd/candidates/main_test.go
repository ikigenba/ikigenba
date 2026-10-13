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
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/pages"
)

// R-1N7H-5GFZ R-1OFD-J86O R-1PN9-WZXD R-1QV6-ARO2 R-1S32-OJER R-1TAZ-2B5G R-1UIV-G2W5
func TestBinary(t *testing.T) {
	t.Setenv(version.CommitVariable, "abcdef0123456789")
	t.Setenv(version.ReleaseVariable, "test-release")
	t.Setenv(services.Variable, "")
	id := version.Read()
	display := id.String()
	binary := filepath.Join(t.TempDir(), "candidates")
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	build := &exec.Cmd{Path: goPath, Args: []string{"go", "build", "-o", binary, "."}}
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, b)
	}
	baseEnv := []string{version.CommitVariable + "=abcdef0123456789", version.ReleaseVariable + "=test-release"}
	for _, tc := range []struct {
		args, env   []string
		out, stderr string
		exit        int
	}{{[]string{"--version"}, baseEnv, display + "\n", "", 0}, {[]string{"--version"}, nil, "\n", "", 0}, {[]string{"manifest"}, baseEnv, cli.Manifest, "", 0}, {[]string{"bogus"}, baseEnv, "", "candidates: unknown command 'bogus'\n\nsee 'candidates --help' for usage\n", 2}, {nil, baseEnv, "", "candidates: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", 2}} {
		c := &exec.Cmd{Path: binary, Args: append([]string{binary}, tc.args...)}
		c.Env = append([]string{}, tc.env...)
		c.Dir = t.TempDir()
		var out, stderr bytes.Buffer
		c.Stdout = &out
		c.Stderr = &stderr
		err := c.Run()
		exit := 0
		if err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatal(err)
			}
			exit = ee.ExitCode()
		}
		if exit != tc.exit || out.String() != tc.out || stderr.String() != tc.stderr {
			t.Fatalf("args=%v exit=%d out=%q stderr=%q", tc.args, exit, out.String(), stderr.String())
		}
	}
	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(signal.String(), func(t *testing.T) {
			short, err := os.MkdirTemp("", "scratch.")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(short) }()
			notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify"), Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = notify.Close() }()
			_ = notify.SetReadDeadline(time.Now().Add(10 * time.Second))
			ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(short, "app"), Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			file, err := ln.File()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			dir := t.TempDir()
			var out, stderr bytes.Buffer
			env := append(append([]string{}, baseEnv...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
			capture := &telemetry.Capture{}
			var ingest *http.Server
			if signal == syscall.SIGTERM {
				tl, err := net.Listen("unix", filepath.Join(short, "telemetry"))
				if err != nil {
					t.Fatal(err)
				}
				ingest = &http.Server{Handler: telemetry.IngestHandler(capture), ReadHeaderTimeout: time.Second}
				go func() { _ = ingest.Serve(tl) }()
				defer func() { _ = ingest.Close() }()
				path := filepath.Join(short, "services.json")
				body := map[string]any{"services": []map[string]any{{"name": "telemetry", "url": "http://telemetry.test", "description": "fixture", "socket": tl.Addr().String(), "enabled": true, "mcp": false}}}
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				env = append(env, services.Variable+"="+path)
			}
			c := &exec.Cmd{Path: "/bin/sh", Args: []string{"/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary}}
			c.Env = env
			c.Dir = dir
			c.ExtraFiles = []*os.File{file}
			c.Stdout = &out
			c.Stderr = &stderr
			if err = c.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = c.Process.Kill()
					_ = c.Wait()
				}
			}()
			ready := make([]byte, 64)
			n, _, err := notify.ReadFromUnix(ready)
			if err != nil || string(ready[:n]) != "READY=1" {
				t.Fatal("ready", err)
			}
			info, err := os.Stat(filepath.Join(dir, "state", "candidates.db"))
			if err != nil || !info.Mode().IsRegular() {
				t.Fatal(info, err)
			}
			transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", ln.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			hc := &http.Client{Transport: transport}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://candidates.test/mcp", HTTPClient: hc})
			result, err := client.CallTool(ctx, identity.Caller{UserID: "fixture-user"}, "passes", nil)
			if err != nil || result.IsError() {
				t.Fatal(err, result)
			}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var members map[string]json.RawMessage
			if err = json.Unmarshal(data, &members); err != nil {
				t.Fatal(err)
			}
			var meta map[string]json.RawMessage
			if err = json.Unmarshal(members["_meta"], &meta); err != nil {
				t.Fatal(err)
			}
			var serverInfo map[string]string
			if err = json.Unmarshal(meta["io.modelcontextprotocol/serverInfo"], &serverInfo); err != nil || !reflect.DeepEqual(serverInfo, map[string]string{"name": pages.ServiceName, "version": display}) {
				t.Fatal(string(data), err)
			}
			if signal == syscall.SIGINT {
				req, err := http.NewRequestWithContext(ctx, "GET", "http://candidates.test/about", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Host = "candidates.sbx.ikigenba.dev"
				req.Header.Set("X-Forwarded-Proto", "https")
				req.Header.Set("X-User-Id", "fixture-user")
				req.Header.Set("X-User-Email", "mg@example.com")
				resp, err := hc.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil || resp.StatusCode != 200 {
					t.Fatal(err, resp.StatusCode)
				}
				templates, err := page.Templates().ParseFS(candidates.Assets(), "*.html")
				if err != nil {
					t.Fatal(err)
				}
				banner := page.New(pages.ServiceName, id).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
				banner.Trail = []page.Level{pages.AboutLevel}
				var expected bytes.Buffer
				if err = templates.ExecuteTemplate(&expected, "about", pages.AboutData{Banner: banner, Description: pages.Description}); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(actual, expected.Bytes()) {
					t.Fatal("about differs from identity template")
				}
			}
			if err = c.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- c.Wait() }()
			select {
			case err = <-done:
				waited = true
				if err != nil {
					t.Fatal(err, stderr.String())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("binary did not stop")
			}
			if out.Len() != 0 {
				t.Fatal(out.String())
			}
			if signal == syscall.SIGTERM {
				if stderr.Len() != 0 {
					t.Fatal(stderr.String())
				}
				events := capture.Events()
				if len(events) < 2 || events[0].Name != "service.started" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"version": display}) || events[len(events)-1].Name != "service.stopping" || !reflect.DeepEqual(events[len(events)-1].Attrs, telemetry.Attrs{"reason": "SIGTERM"}) {
					t.Fatal(events)
				}
			}
		})
	}
}
