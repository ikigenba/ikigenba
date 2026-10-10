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

	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/events/internal/cli"
	"github.com/ikigenba/ikigenba/events/internal/pages"
)

// R-9K3A-GTGR R-6E6K-49GH R-6FEG-I176 R-06XG-JBD4 R-6KA0-OOQW R-6LHX-2GHL R-9NQZ-M4OU
// R-6NXP-TZYZ R-6P5M-7RPO R-6QDI-LJGD R-C0NJ-9HL4 R-5MS9-KEY1
func TestBinaryWiring(t *testing.T) {
	t.Setenv("IKIGENBA_SERVICES", "")
	commit, release := "0123456789abcdef0123456789abcdef01234567", "test-release"
	t.Setenv(version.CommitVariable, commit)
	t.Setenv(version.ReleaseVariable, release)
	id := version.Read()
	display := version.Display()
	identityEnv := []string{version.CommitVariable + "=" + commit, version.ReleaseVariable + "=" + release}
	binary := filepath.Join(t.TempDir(), "events")
	build := exec.Command("go", "build")
	build.Args = append(build.Args, "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, tc := range []struct {
		args     []string
		out, err string
		code     int
	}{
		{[]string{"--version"}, display + "\n", "", cli.ExitSuccess},
		{[]string{"manifest"}, cli.Manifest, "", cli.ExitSuccess},
		{[]string{"bogus"}, "", "events: unknown command 'bogus'\n\nsee 'events --help' for usage\n", cli.ExitUsage},
		{nil, "", "events: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
	} {
		command := &exec.Cmd{Path: binary, Args: append([]string{binary}, tc.args...)}
		command.Env = identityEnv
		command.Dir = t.TempDir()
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != tc.code || stdout.String() != tc.out || stderr.String() != tc.err {
			t.Fatalf("args %v: code %d stdout %q stderr %q", tc.args, code, stdout.String(), stderr.String())
		}
	}
	command := &exec.Cmd{Path: binary, Args: []string{binary, "--version"}, Env: []string{}, Dir: t.TempDir()}
	var emptyOut, emptyErr bytes.Buffer
	command.Stdout, command.Stderr = &emptyOut, &emptyErr
	if err := command.Run(); err != nil || emptyOut.String() != "\n" || emptyErr.Len() != 0 {
		t.Fatalf("unset version: %v stdout %q stderr %q", err, emptyOut.String(), emptyErr.String())
	}
	for _, start := range []struct {
		name       string
		signal     syscall.Signal
		eventsIcon bool
	}{{"SIGTERM", syscall.SIGTERM, false}, {"SIGINT", syscall.SIGINT, false}, {"events-icon", syscall.SIGTERM, true}} {
		t.Run(start.name, func(t *testing.T) {
			signal := start.signal
			short, err := os.MkdirTemp("", "binary-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(short) })
			socket := filepath.Join(short, "events.sock")
			notifyPath := filepath.Join(short, "notify.sock")
			telemetryPath := filepath.Join(short, "trail.sock")
			for _, path := range []string{socket, notifyPath, telemetryPath} {
				if len(path) >= 107 {
					t.Fatalf("socket path too long: %s", path)
				}
			}
			notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = notify.Close() })
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			inherited, err := listener.File()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = inherited.Close() })
			trailListener, err := net.Listen("unix", telemetryPath)
			if err != nil {
				t.Fatal(err)
			}
			type event struct {
				Service   string         `json:"service"`
				Name      string         `json:"event"`
				RequestID string         `json:"request_id"`
				User      string         `json:"user"`
				Attrs     map[string]any `json:"attrs"`
			}
			var mutex sync.Mutex
			var trail []event
			trailServer := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/ingest" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("trail body: %v", err)
					w.WriteHeader(500)
					return
				}
				var record event
				if err := json.Unmarshal(body, &record); err != nil {
					t.Errorf("trail decode: %v", err)
					w.WriteHeader(500)
					return
				}
				mutex.Lock()
				trail = append(trail, record)
				mutex.Unlock()
				w.WriteHeader(http.StatusNoContent)
			})}
			go func() { _ = trailServer.Serve(trailListener) }()
			t.Cleanup(func() { _ = trailServer.Close() })
			servicesPath := filepath.Join(short, "services.json")
			entry := map[string]any{"name": "telemetry", "enabled": true, "socket": telemetryPath, "url": "", "description": "", "mcp": false}
			if signal == syscall.SIGINT {
				entry["icon"] = "telemetry-icon-fixture"
			}
			entries := []any{entry}
			ownIcon := "events-icon-fixture"
			if start.eventsIcon {
				entries = append(entries, map[string]any{"name": "events", "enabled": false, "socket": "", "url": "", "description": "", "mcp": true, "icon": ownIcon})
			}
			serviceFile, err := json.Marshal(map[string]any{"services": entries})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(servicesPath, serviceFile, 0600); err != nil {
				t.Fatal(err)
			}
			working := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`)
			command.Args = append(command.Args, binary)
			command.Env = append([]string{"IKIGENBA_SERVICES=" + servicesPath, "NOTIFY_SOCKET=" + notifyPath}, identityEnv...)
			command.Dir = working
			command.ExtraFiles = []*os.File{inherited}
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var processErr error
			go func() { processErr = command.Wait(); close(done) }()
			t.Cleanup(func() {
				cancel()
				<-done
			})
			if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var notification [1024]byte
			size, _, err := notify.ReadFromUnix(notification[:])
			if err != nil {
				t.Fatalf("readiness: %v", err)
			}
			if string(notification[:size]) != "READY=1" {
				t.Fatalf("notification %q", notification[:size])
			}
			transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			}}
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			get := func(path string) string {
				t.Helper()
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://events.space.test"+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("X-User-Id", "binary-user")
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if err != nil || closeErr != nil || response.StatusCode != http.StatusOK {
					t.Fatalf("GET %s: status %d read %v close %v", path, response.StatusCode, err, closeErr)
				}
				return string(body)
			}
			about := get("/about")
			t.Setenv(services.Variable, servicesPath)
			kit := page.New(appEvents.ServiceName, id)
			pg := pages.New(pages.Config{Banner: kit.Banner, ServicesPath: servicesPath})
			expected := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://events.space.test/about", nil)
			request.Header.Set("X-User-Id", "binary-user")
			pg.About(expected, request)
			if expected.Code != http.StatusOK || about != expected.Body.String() {
				t.Fatal("main did not supply identity and banner", about, expected.Body.String())
			}
			discoverBody := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"` + mcp.ProtocolVersion + `","io.modelcontextprotocol/clientCapabilities":{}}}}`
			discover, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://events.space.test/mcp", strings.NewReader(discoverBody))
			if err != nil {
				t.Fatal(err)
			}
			discover.Header.Set("X-User-Id", "binary-user")
			discover.Header.Set("Content-Type", "application/json")
			discover.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
			discover.Header.Set("Mcp-Method", "server/discover")
			answer, err := client.Do(discover)
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Result struct {
					Meta map[string]json.RawMessage `json:"_meta"`
				} `json:"result"`
			}
			decodeErr := json.NewDecoder(answer.Body).Decode(&result)
			closeErr := answer.Body.Close()
			if answer.StatusCode != http.StatusOK || decodeErr != nil || closeErr != nil {
				t.Fatalf("discover: status %d decode %v close %v", answer.StatusCode, decodeErr, closeErr)
			}
			var serverInfo map[string]string
			if err := json.Unmarshal(result.Result.Meta["io.modelcontextprotocol/serverInfo"], &serverInfo); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(serverInfo, map[string]string{"name": appEvents.ServiceName, "version": display}) {
				t.Fatal("binary MCP identity", serverInfo)
			}
			info, err := os.Stat(filepath.Join(working, "state", "events.db"))
			if err != nil || !info.Mode().IsRegular() {
				t.Fatalf("database not created: %v", err)
			}
			if err := command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				if processErr != nil {
					t.Fatalf("signal %s: %v", signal, processErr)
				}
			case <-ctx.Done():
				t.Fatal("child failed to exit")
			}
			if _, err := os.Stat(socket); err != nil {
				t.Fatalf("inherited socket removed: %v", err)
			}
			queued, err := net.DialTimeout("unix", socket, time.Second)
			if err != nil {
				t.Fatalf("inherited socket no longer queues: %v", err)
			}
			if err := listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			accepted, err := listener.AcceptUnix()
			if err != nil {
				t.Fatalf("parent duplicate stopped accepting: %v", err)
			}
			_ = accepted.Close()
			_ = queued.Close()
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("streams stdout %q stderr %q", stdout.String(), stderr.String())
			}
			mutex.Lock()
			records := append([]event(nil), trail...)
			mutex.Unlock()
			started := -1
			for index, record := range records {
				if record.Service != "events" {
					t.Fatalf("service %#v", record)
				}
				if record.Name == "service.started" {
					started = index
					break
				}
				if record.Name != "sibling.called" {
					t.Fatalf("event before start %#v", record)
				}
			}
			if started < 0 {
				t.Fatalf("no started event: %#v", records)
			}
			first := records[started]
			if first.RequestID != "" || first.User != "" || !reflect.DeepEqual(first.Attrs, map[string]any{"version": display}) {
				t.Fatalf("started %#v", first)
			}
			last := records[len(records)-1]
			reason := "SIGTERM"
			if signal == syscall.SIGINT {
				reason = "SIGINT"
			}
			if last.Name != "service.stopping" || last.RequestID != "" || last.User != "" || !reflect.DeepEqual(last.Attrs, map[string]any{"reason": reason}) {
				t.Fatalf("last %#v", last)
			}
		})
	}
}
