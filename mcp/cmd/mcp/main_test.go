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
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	appkitmcp "github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/mcp/internal/cli"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

// The sole process test proves main's process, constructor and signal wiring.
// R-UU3K-Z7YX R-UVBH-CZPM R-UWJD-QRGB R-TIVK-SAME
// R-UYZ6-IAXP R-V072-W2OE R-H8WC-Q7J9 R-1JO6-I8D2 R-WSTR-5WZ7
func TestBinary(t *testing.T) {
	binaryDirectory := t.TempDir()
	binaryRoot, err := os.OpenRoot(binaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := binaryRoot.OpenFile("mcp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	rootErr := binaryRoot.Close()
	if err != nil {
		t.Fatal(err)
	}
	if rootErr != nil {
		_ = binary.Close()
		t.Fatal(rootErr)
	}
	// A fixed stdout destination sends the build product into our temporary file.
	build := exec.Command("go", "build", "-o", "/dev/stdout", ".")
	build.Stdout = binary
	var buildStderr bytes.Buffer
	build.Stderr = &buildStderr
	buildErr := build.Run()
	modeErr := binary.Chmod(0700)
	closeErr := binary.Close()
	if buildErr != nil {
		t.Fatalf("build: %v\n%s", buildErr, &buildStderr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if modeErr != nil {
		t.Fatal(modeErr)
	}
	for _, command := range []*exec.Cmd{
		exec.Command("./mcp", "--version"),
		exec.Command("./mcp", "bogus"),
		exec.Command("./mcp"),
	} {
		args := command.Args[1:]
		var wantOut, wantErr bytes.Buffer
		wantCode := cli.Run(context.Background(), cli.Process{Args: args, LookupEnv: func(string) (string, bool) { return "", false }, Pid: 123, Stdout: &wantOut, Stderr: &wantErr, Inherit: func(uintptr) (net.Listener, error) {
			t.Fatal("unexpected inheritance")
			return nil, errors.New("unexpected")
		}})
		command.Dir = binaryDirectory
		command.Env = []string{}
		var out, stderr bytes.Buffer
		command.Stdout, command.Stderr = &out, &stderr
		err := command.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != wantCode || out.String() != wantOut.String() || stderr.String() != wantErr.String() {
			t.Fatalf("args %v: code %d stdout %q stderr %q; want %d %q %q", args, code, out.String(), stderr.String(), wantCode, wantOut.String(), wantErr.String())
		}
	}

	t.Setenv(services.Variable, "")
	directory, err := os.MkdirTemp("", "mcp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	backendSocket := filepath.Join(directory, "backend.sock")
	backendListener, err := net.Listen("unix", backendSocket)
	if err != nil {
		t.Fatal(err)
	}
	backendWriter := telemetry.New(telemetry.Config{Service: "alpha", Sink: &telemetry.Capture{}})
	defer backendWriter.Shutdown(context.Background(), "test")
	backend := appkitmcp.NewServer(appkitmcp.ServerConfig{Name: "alpha", Version: "test", Telemetry: backendWriter})
	type input struct{}
	type output struct {
		Value string `json:"value"`
	}
	appkitmcp.AddTool(backend, appkitmcp.Tool[input, output]{Name: "read", Description: "Read a value.", Effect: appkitmcp.Read, Handler: func(context.Context, identity.Caller, input) (output, error) {
		return output{Value: "backend answer"}, nil
	}})
	backendHTTP := &http.Server{Handler: identity.Require(backend), ReadHeaderTimeout: time.Second}
	go func() { _ = backendHTTP.Serve(backendListener) }()
	t.Cleanup(func() { _ = backendHTTP.Close() })
	file := filepath.Join(directory, "services.json")
	raw, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "alpha", "url": "https://alpha.example.test", "description": "Alpha", "socket": backendSocket, "enabled": true, "mcp": true, "icon": "<svg></svg>"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := services.Read(file)
	if err != nil || len(entries) != 1 || !entries[0].HasIcon {
		t.Fatalf("services fixture: %v %v", entries, err)
	}
	caller := identity.Caller{UserID: "user", RequestID: "binarytrace"}
	backendClient := appkitmcp.NewClient(appkitmcp.ClientConfig{Endpoint: "http://backend/mcp", HTTPClient: binaryHTTP(backendSocket)})
	expected, err := backendClient.CallTool(context.Background(), caller, "read", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	expectedJSON := binaryResult(t, expected)

	trail := &telemetry.Capture{}
	telemetrySocket := filepath.Join(directory, "telemetry.sock")
	telemetryListener, err := net.Listen("unix", telemetrySocket)
	if err != nil {
		t.Fatal(err)
	}
	telemetryHTTP := &http.Server{Handler: telemetry.IngestHandler(trail), ReadHeaderTimeout: time.Second}
	go func() { _ = telemetryHTTP.Serve(telemetryListener) }()
	t.Cleanup(func() { _ = telemetryHTTP.Close() })
	trailFile := filepath.Join(directory, "trail.json")
	trailRaw, err := json.Marshal(map[string]any{"services": []map[string]any{
		{"name": "alpha", "url": "", "description": "", "socket": backendSocket, "enabled": true, "mcp": true},
		{"name": telemetry.ServiceName, "url": "", "description": "", "socket": telemetrySocket, "enabled": true, "mcp": false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(trailFile, trailRaw, 0600); err != nil {
		t.Fatal(err)
	}
	for run, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGTERM, syscall.SIGINT} {
		socket := filepath.Join(directory, "gateway"+string(rune('0'+run))+".sock")
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		inherited, err := listener.File()
		if err != nil {
			t.Fatal(err)
		}
		notifyPath := filepath.Join(directory, "notify"+string(rune('0'+run))+".sock")
		notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = notify.Close() })
		child := exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, "./mcp")
		child.Dir = binaryDirectory
		child.Env = []string{"NOTIFY_SOCKET=" + notifyPath}
		if run == 0 {
			child.Env = append(child.Env, services.Variable+"="+file)
		}
		if run >= 2 {
			child.Env = append(child.Env, services.Variable+"="+trailFile)
		}
		child.ExtraFiles = []*os.File{inherited}
		var stdout, stderr bytes.Buffer
		child.Stdout, child.Stderr = &stdout, &stderr
		if err = child.Start(); err != nil {
			_ = inherited.Close()
			t.Fatal(err)
		}
		if err = inherited.Close(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		stopped := false
		t.Cleanup(func() {
			if !stopped {
				_ = child.Process.Kill()
				<-done
			}
		})
		if err = notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 128)
		n, _, err := notify.ReadFromUnix(buf)
		if err != nil {
			t.Fatalf("readiness: %v", err)
		}
		if string(buf[:n]) != "READY=1" {
			t.Fatalf("readiness %q", buf[:n])
		}
		httpClient := binaryHTTP(socket)
		if run < 2 {
			request, err := http.NewRequest(http.MethodGet, "http://gateway/", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-User-Id", caller.UserID)
			response, err := httpClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("page status %d: %s", response.StatusCode, body)
			}
			if run == 0 {
				launcher := regexp.MustCompile(`(?i)<button\b[^>]*\bclass="launcher"[^>]*>`)
				if !launcher.Match(body) {
					t.Fatalf("launcher absent: %s", body)
				}
			} else {
				lower := strings.ToLower(string(body))
				footers := regexp.MustCompile(`<footer[>\t\n\r\f\v ]`).FindAllStringIndex(lower, -1)
				if len(footers) != 1 {
					t.Fatalf("footer count %d: %s", len(footers), body)
				}
				start := footers[0][0] + strings.IndexByte(lower[footers[0][0]:], '>') + 1
				end := strings.Index(lower[start:], "</footer>")
				if end < 0 || strings.Trim(string(body[start:start+end]), " \t\n\r\f\v") != gateway.ServiceName+" "+cli.Version {
					t.Fatalf("footer: %s", body)
				}
			}
			client := appkitmcp.NewClient(appkitmcp.ClientConfig{Endpoint: "http://gateway/mcp", HTTPClient: httpClient})
			result, err := client.CallTool(context.Background(), caller, "services", nil)
			if err != nil || result.IsError() {
				t.Fatalf("services: %v %v", result, err)
			}
			value := binaryResult(t, result)
			meta, ok := value["_meta"].(map[string]any)
			if !ok || !reflect.DeepEqual(meta["io.modelcontextprotocol/serverInfo"], map[string]any{"name": gateway.ServiceName, "version": cli.Version}) {
				t.Fatalf("serverInfo: %v", value)
			}
			if run == 0 {
				structured, ok := value["structuredContent"].(map[string]any)
				if !ok {
					t.Fatalf("services content: %v", value)
				}
				list, ok := structured["services"].([]any)
				if !ok || len(list) != 1 {
					t.Fatalf("services list: %v", structured)
				}
				service, ok := list[0].(map[string]any)
				if !ok || service["name"] != "alpha" || service["available"] != true {
					t.Fatalf("service: %v", list)
				}
				result, err = client.CallTool(context.Background(), caller, "call", json.RawMessage(`{"service":"alpha","tool":"read"}`))
				if err != nil || result.IsError() {
					t.Fatalf("call: %v %v", result, err)
				}
				if !reflect.DeepEqual(binaryResult(t, result)["structuredContent"], expectedJSON["structuredContent"]) {
					t.Fatalf("call answer: %v", binaryResult(t, result))
				}
			}
		} else if run == 2 {
			client := appkitmcp.NewClient(appkitmcp.ClientConfig{Endpoint: "http://gateway/mcp", HTTPClient: httpClient})
			result, callErr := client.CallTool(context.Background(), caller, "call", json.RawMessage(`{"service":"alpha","tool":"read"}`))
			if callErr != nil || result.IsError() {
				t.Fatalf("call: %v %v", result, callErr)
			}
			if !reflect.DeepEqual(binaryResult(t, result)["structuredContent"], expectedJSON["structuredContent"]) {
				t.Fatal("call content differs")
			}
		}

		if err = child.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		select {
		case err = <-done:
			stopped = true
			if err != nil {
				t.Fatalf("stop %s: %v", sig, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("stop %s timed out", sig)
		}
		if stdout.Len() != 0 {
			t.Fatal(stdout.String())
		}
		if run >= 2 {
			if stderr.Len() != 0 {
				t.Fatal(stderr.String())
			}
			events := trail.Events()
			if run == 3 { // The second telemetry-connected child appends its two lifecycle events.
				events = events[len(events)-2:]
			}
			first, last := events[0], events[len(events)-1]
			if first.Name != "service.started" || first.Service != gateway.ServiceName || first.RequestID != "" || first.User != "" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": cli.Version}) {
				t.Fatalf("started: %#v", first)
			}
			reason := "SIGTERM"
			if sig == syscall.SIGINT {
				reason = "SIGINT"
			}
			if last.Name != "service.stopping" || last.Service != gateway.ServiceName || last.RequestID != "" || last.User != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": reason}) {
				t.Fatalf("stopping: %#v", last)
			}
			if run == 2 {
				names := []string{}
				for _, e := range events {
					if e.Service == gateway.ServiceName && e.RequestID == caller.RequestID {
						names = append(names, e.Name)
						if e.User != caller.UserID {
							t.Fatal(e)
						}
						if e.Name == "sibling.called" && (e.Attrs["target"] != "alpha" || e.Attrs["status"] != int64(200)) {
							t.Fatal(e)
						}
						if e.Name == "tool.called" && (e.Attrs["tool"] != "call" || e.Attrs["kind"] != "read" || e.Attrs["outcome"] != "ok") {
							t.Fatal(e)
						}
					}
				}
				if !reflect.DeepEqual(names, []string{"request.started", "sibling.called", "sibling.called", "tool.called", "request.finished"}) {
					t.Fatal(names)
				}
			}
		}

		httpClient.CloseIdleConnections()
		if _, err = os.Stat(socket); err != nil {
			t.Fatalf("inherited socket path lost: %v", err)
		}
		queued, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			t.Fatalf("inherited accept queue shut down: %v", err)
		}
		if err = listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
			_ = queued.Close()
			t.Fatal(err)
		}
		accepted, err := listener.AcceptUnix()
		_ = queued.Close()
		if err != nil {
			t.Fatalf("remaining owner cannot accept queued connection: %v", err)
		}
		_ = accepted.Close()
	}
}

func binaryHTTP(socket string) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
}
func binaryResult(t *testing.T, result appkitmcp.Result) map[string]any {
	t.Helper()
	raw, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
