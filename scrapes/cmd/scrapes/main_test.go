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
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	app "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
)

// TestBinary is the sole process-executing test: the remaining tests use Run.
func TestBinary(t *testing.T) {
	// R-Z2EM-GKGH R-Z3MI-UC76 R-Z4UF-83XV
	t.Setenv(services.Variable, "")
	t.Setenv(version.CommitVariable, "123456789abcdef")
	release := pages.ServiceName + "-binary-fixture"
	t.Setenv(version.ReleaseVariable, release)
	id := version.Read()
	display := id.String()
	env := []string{version.CommitVariable + "=123456789abcdef", version.ReleaseVariable + "=" + release}
	binary := filepath.Join(t.TempDir(), "scrapes")
	build := exec.CommandContext(t.Context(), "go")
	build.Args = append(build.Args, "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	runCommand := func(args []string, childEnv []string, wantCode int, wantOut, wantErr string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/bin/sh")
		cmd.Path = binary
		cmd.Args = append([]string{binary}, args...)
		cmd.Env = childEnv
		cmd.Dir = t.TempDir()
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("command %v: %v", args, err)
			}
			code = exit.ExitCode()
		}
		if code != wantCode || stdout.String() != wantOut || stderr.String() != wantErr {
			t.Fatalf("command %v: exit %d stdout %q stderr %q; want exit %d stdout %q stderr %q", args, code, stdout.String(), stderr.String(), wantCode, wantOut, wantErr)
		}
	}
	runCommand([]string{"--version"}, env, cli.ExitSuccess, display+"\n", "")
	runCommand([]string{"--version"}, []string{}, cli.ExitSuccess, "\n", "")
	runCommand([]string{"manifest"}, env, cli.ExitSuccess, cli.Manifest, "")
	runCommand([]string{"bogus"}, env, cli.ExitUsage, "", "scrapes: unknown command 'bogus'\n\nsee 'scrapes --help' for usage\n")
	runCommand(nil, env, cli.ExitUsage, "", "scrapes: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n")

	for _, tc := range []struct {
		name      string
		signal    syscall.Signal
		telemetry bool
	}{
		{"about", syscall.SIGTERM, false},
		{"term", syscall.SIGTERM, true},
		{"int", syscall.SIGINT, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// R-Z62B-LVOK R-Z7A7-ZNF9 R-Z8I4-DF5Y R-Z9Q0-R6WN
			short, err := os.MkdirTemp("", "scrapes-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(short) }()
			socket := filepath.Join(short, "app.sock")
			ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			fd, err := ln.File()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = fd.Close() }()
			notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify.sock"), Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = notify.Close() }()
			childEnv := append(append([]string{}, env...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
			capture := &telemetry.Capture{}
			if tc.telemetry {
				sinkPath := filepath.Join(short, "telemetry.sock")
				sinkListener, err := net.Listen("unix", sinkPath)
				if err != nil {
					t.Fatal(err)
				}
				sinkServer := &http.Server{Handler: telemetry.IngestHandler(capture), ReadHeaderTimeout: time.Second}
				served := make(chan error, 1)
				go func() { served <- sinkServer.Serve(sinkListener) }()
				defer func() { _ = sinkServer.Close(); <-served }()
				document, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "telemetry", "url": "http://telemetry.test", "description": "", "socket": sinkPath, "enabled": true, "mcp": false}}})
				if err != nil {
					t.Fatal(err)
				}
				servicesPath := filepath.Join(short, "services.json")
				if err := os.WriteFile(servicesPath, document, 0600); err != nil {
					t.Fatal(err)
				}
				childEnv = append(childEnv, services.Variable+"="+servicesPath)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/sh")
			cmd.Args = append(cmd.Args, "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary)
			cmd.Env, cmd.Dir, cmd.ExtraFiles = childEnv, t.TempDir(), []*os.File{fd}
			var stdout, stderr lockedBuffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 1024)
			n, _, err := notify.ReadFromUnix(buf)
			if err != nil {
				t.Fatalf("readiness: %v; stderr %q", err, stderr.String())
			}
			if string(buf[:n]) != "READY=1" {
				t.Fatalf("readiness %q", buf[:n])
			}
			if info, err := os.Stat(filepath.Join(cmd.Dir, "state", "scrapes.db")); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("database at READY: %v %v", info, err)
			}
			transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			}}
			defer transport.CloseIdleConnections()
			hc := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://scrapes.test/mcp", HTTPClient: hc})
			result, err := client.CallTool(ctx, identity.Caller{UserID: "binary-user", Email: "mg@example.com"}, "scrapers", nil)
			if err != nil || result.IsError() {
				t.Fatalf("scrapers: %v %v", result, err)
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var answer struct {
				Meta map[string]json.RawMessage `json:"_meta"`
			}
			if err := json.Unmarshal(raw, &answer); err != nil {
				t.Fatal(err)
			}
			var info map[string]string
			if err := json.Unmarshal(answer.Meta["io.modelcontextprotocol/serverInfo"], &info); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(info, map[string]string{"name": pages.ServiceName, "version": display}) {
				t.Fatalf("serverInfo %v", info)
			}
			request := func(method, path string, body io.Reader) *http.Response {
				t.Helper()
				req, err := http.NewRequestWithContext(ctx, method, "http://scrapes.sbx.ikigenba.dev"+path, body)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("X-User-Id", "binary-user")
				req.Header.Set("X-User-Email", "mg@example.com")
				req.Header.Set("X-Forwarded-Proto", "https")
				if body != nil {
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
					req.Header.Set("Mcp-Method", "server/discover")
					req.Header.Set("Accept", "application/json, text/event-stream")
				}
				resp, err := hc.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				return resp
			}
			for _, path := range []string{"/", "/tools", "/about"} {
				resp := request(http.MethodGet, path, nil)
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil || resp.StatusCode != 200 {
					t.Fatalf("page %s: %d %v", path, resp.StatusCode, err)
				}
				if path == "/about" && !tc.telemetry {
					templates, err := page.Templates().ParseFS(app.Assets(), "*.html")
					if err != nil {
						t.Fatal(err)
					}
					banner := page.New(pages.ServiceName, id).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
					banner.Trail = []page.Level{pages.AboutLevel}
					var want bytes.Buffer
					if err := templates.ExecuteTemplate(&want, "about", pages.AboutData{Banner: banner, Description: pages.Description}); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(body, want.Bytes()) {
						t.Fatalf("about does not match identity template")
					}
				}
			}
			discovery, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "server/discover", "params": map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": mcp.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}})
			if err != nil {
				t.Fatal(err)
			}
			resp := request(http.MethodPost, "/mcp", bytes.NewReader(discovery))
			discovered, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("discover: %d %v %s", resp.StatusCode, err, discovered)
			}
			var rpc struct {
				Result struct {
					Meta map[string]map[string]string `json:"_meta"`
				} `json:"result"`
				Error json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(discovered, &rpc); err != nil {
				t.Fatal(err)
			}
			if len(rpc.Error) != 0 || !reflect.DeepEqual(rpc.Result.Meta["io.modelcontextprotocol/serverInfo"], info) {
				t.Fatalf("discover: %s", discovered)
			}
			if err := cmd.Process.Signal(tc.signal); err != nil {
				t.Fatal(err)
			}
			err = cmd.Wait()
			waited = true
			if err != nil || stdout.String() != "" || tc.telemetry && stderr.String() != "" {
				t.Fatalf("stop: %v stdout %q stderr %q", err, stdout.String(), stderr.String())
			}
			if tc.telemetry {
				events := capture.Events()
				if len(events) < 2 {
					t.Fatalf("telemetry: %v", events)
				}
				first, last := events[0], events[len(events)-1]
				reason := "SIGTERM"
				if tc.signal == syscall.SIGINT {
					reason = "SIGINT"
				}
				if first.Name != "service.started" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": display}) || last.Name != "service.stopping" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": reason}) {
					t.Fatalf("lifecycle: %v %v", first, last)
				}
				for _, e := range events {
					if strings.HasPrefix(e.Name, "scrape.") || e.Name == "source.called" {
						t.Fatalf("unexpected work: %v", e)
					}
				}
			}
		})
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }
