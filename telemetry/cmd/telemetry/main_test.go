package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
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
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/cli"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

// TestBinary is the one process test: all command and host wiring is exercised here.
func TestBinary(t *testing.T) {
	// R-TMG4-A1C2
	processContext, cancelProcesses := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelProcesses()
	runProcess := func(cmd *exec.Cmd) error {
		if startErr := cmd.Start(); startErr != nil {
			return startErr
		}
		result := make(chan error, 1)
		go func() { result <- cmd.Wait() }()
		select {
		case waitErr := <-result:
			return waitErr
		case <-processContext.Done():
			_ = cmd.Process.Kill()
			<-result
			return processContext.Err()
		}
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	cacheQuery := exec.CommandContext(processContext, "go", "env", "GOMODCACHE")
	moduleCache, err := cacheQuery.Output()
	if err != nil {
		t.Fatal(err)
	}
	buildDir := t.TempDir()
	binary := filepath.Join(buildDir, "telemetry")
	build := &exec.Cmd{Path: goTool, Args: []string{goTool, "build", "-buildvcs=false", "-o", binary, "."}}
	build.Env = []string{
		"PATH=" + filepath.Dir(goTool) + ":/usr/bin:/bin",
		"HOME=" + buildDir,
		"GOCACHE=" + filepath.Join(buildDir, "cache"),
		"GOMODCACHE=" + strings.TrimSpace(string(moduleCache)),
		"GOPROXY=off", "GONOSUMDB=github.com/ikigenba/ikigenba",
	}
	var buildOutput bytes.Buffer
	build.Stdout, build.Stderr = &buildOutput, &buildOutput
	if buildErr := runProcess(build); buildErr != nil {
		t.Fatalf("build: %v\n%s", buildErr, buildOutput.Bytes())
	}
	workingDir := t.TempDir()
	for _, tc := range []struct {
		args           []string
		code           int
		stdout, stderr string
	}{
		{[]string{"--version"}, cli.ExitSuccess, cli.Version + "\n", ""},
		{[]string{"manifest"}, cli.ExitSuccess, cli.Manifest, ""},
		{[]string{"bogus"}, cli.ExitUsage, "", "telemetry: unknown command 'bogus'\n\nsee 'telemetry --help' for usage\n"},
		{nil, cli.ExitUsage, "", "telemetry: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
	} {
		cmd := &exec.Cmd{Path: binary, Args: append([]string{binary}, tc.args...)}
		cmd.Dir = workingDir
		cmd.Env = []string{}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		runErr := runProcess(cmd)
		code := 0
		if runErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) {
				t.Fatal(runErr)
			}
			code = exitErr.ExitCode()
		}
		if code != tc.code || stdout.String() != tc.stdout || stderr.String() != tc.stderr {
			t.Fatalf("args %q: code %d stdout %q stderr %q", tc.args, code, stdout.String(), stderr.String())
		}
	}
	// R-TOVX-1KTG
	if _, err = os.Stat(filepath.Join(workingDir, "state")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("commands created state: %v", err)
	}

	shortDir, err := os.MkdirTemp("", "telemetry-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cleanupErr := os.RemoveAll(shortDir); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	servicesFile := filepath.Join(workingDir, "services.json")
	writeServices := func(description string) {
		entries := map[string]any{"services": []any{map[string]any{
			"name": web.ServiceName, "url": "http://telemetry.test", "description": description,
			"socket": "telemetry.sock", "enabled": true, "mcp": true, "icon": "<svg></svg>",
		}}}
		data, marshalErr := json.Marshal(entries)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if writeErr := os.WriteFile(servicesFile, data, 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	launch := func(runName string, services bool) (*http.Client, func(syscall.Signal)) {
		socketPath := filepath.Join(shortDir, runName+".sock")
		listener, listenErr := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		t.Cleanup(func() { _ = listener.Close() })
		fd, fileErr := listener.File()
		if fileErr != nil {
			t.Fatal(fileErr)
		}
		t.Cleanup(func() { _ = fd.Close() })
		notifyPath := filepath.Join(shortDir, runName+".notify")
		notify, notifyErr := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
		if notifyErr != nil {
			t.Fatal(notifyErr)
		}
		t.Cleanup(func() { _ = notify.Close() })
		cmd := &exec.Cmd{Path: "/bin/sh", Args: []string{"/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary}}
		cmd.ExtraFiles = []*os.File{fd}
		cmd.Dir = workingDir
		cmd.Env = []string{"NOTIFY_SOCKET=" + notifyPath}
		if services {
			cmd.Env = append(cmd.Env, "IKIGENBA_SERVICES="+servicesFile)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if startErr := cmd.Start(); startErr != nil {
			t.Fatal(startErr)
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		stopped := false
		t.Cleanup(func() {
			if !stopped {
				_ = cmd.Process.Kill()
				<-exited
			}
		})
		if deadlineErr := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); deadlineErr != nil {
			t.Fatal(deadlineErr)
		}
		datagram := make([]byte, 64)
		n, _, readErr := notify.ReadFromUnix(datagram)
		if readErr != nil {
			t.Fatalf("%s readiness: %v", runName, readErr)
		}
		if string(datagram[:n]) != "READY=1" {
			t.Fatalf("readiness %q", datagram[:n])
		}
		info, statErr := os.Stat(filepath.Join(workingDir, "state", "telemetry.db"))
		if statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("working directory database: %v", statErr)
		}
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}}
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		t.Cleanup(transport.CloseIdleConnections)
		stop := func(sig syscall.Signal) {
			// R-TNO0-NT2R R-TXF7-PZ0B
			transport.CloseIdleConnections()
			if signalErr := cmd.Process.Signal(sig); signalErr != nil {
				t.Fatal(signalErr)
			}
			select {
			case waitErr := <-exited:
				stopped = true
				if waitErr != nil || stdout.Len() != 0 || stderr.Len() != 0 {
					t.Fatalf("%s stopped: %v stdout %q stderr %q", runName, waitErr, stdout.String(), stderr.String())
				}
			case <-time.After(6 * time.Second):
				t.Fatal("binary did not stop before drain deadline")
			}
			// R-Q8ON-FNVI: the inherited socket still belongs to its parent.
			if _, pathErr := os.Stat(socketPath); pathErr != nil {
				t.Fatalf("socket path removed: %v", pathErr)
			}
			connection, dialErr := net.DialTimeout("unix", socketPath, time.Second)
			if dialErr != nil {
				t.Fatalf("inherited socket no longer accepts connections: %v", dialErr)
			}
			defer func() { _ = connection.Close() }()
			if deadlineErr := listener.SetDeadline(time.Now().Add(time.Second)); deadlineErr != nil {
				t.Fatal(deadlineErr)
			}
			accepted, acceptErr := listener.AcceptUnix()
			if acceptErr != nil {
				t.Fatalf("parent cannot accept after child exit: %v", acceptErr)
			}
			if closeErr := accepted.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		return client, stop
	}

	first, stopFirst := launch("first", false)
	// R-22RS-2SKH
	landing := binaryRequest(t, first, http.MethodGet, "/", nil, nil, http.StatusOK)
	footerRE := regexp.MustCompile(`(?i)<footer[>\t\n\f\r ]`)
	footers := footerRE.FindAllIndex(landing, -1)
	if len(footers) != 1 {
		t.Fatalf("footer start tags: %s", landing)
	}
	start := footers[0][0]
	openEnd := bytes.IndexByte(landing[start:], '>')
	if openEnd < 0 {
		t.Fatalf("footer start has no closing bracket: %s", landing)
	}
	content := landing[start+openEnd+1:]
	closeTag := regexp.MustCompile(`(?i)</footer>`).FindIndex(content)
	if closeTag == nil {
		t.Fatalf("footer has no end tag: %s", landing)
	}
	if html.UnescapeString(strings.Trim(string(content[:closeTag[0]]), " \t\n\f\r")) != web.ServiceName+" "+cli.Version {
		t.Fatalf("footer: %s", landing)
	}
	// R-TSJM-6W1J
	catalog := binaryCall(t, first, "catalog", nil)
	meta := catalog["_meta"].(map[string]any)
	if !reflect.DeepEqual(meta["io.modelcontextprotocol/serverInfo"], map[string]any{"name": web.ServiceName, "version": cli.Version}) {
		t.Fatalf("catalog serverInfo: %v", meta)
	}
	// R-TUZE-YFIX
	if _, present := binaryDiscover(t, first)["instructions"]; present {
		t.Fatal("instructions returned with services unset")
	}
	// R-TW7B-C79M
	started := binaryRecords(t, binaryCall(t, first, "search", json.RawMessage(`{"services":["telemetry"],"limit":500}`)))
	if len(started) == 0 {
		t.Fatal("missing service.started")
	}
	last := started[len(started)-1]
	if last["service"] != web.ServiceName || last["event"] != "service.started" || last["request_id"] != "" || last["user"] != "" || !reflect.DeepEqual(last["attrs"], map[string]any{"version": cli.Version}) {
		t.Fatalf("start record: %v", last)
	}
	// R-TYN4-3QR0
	event := telemetry.Event{Time: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), Service: "sibling", Name: "sample.recorded", RequestID: "binary-ingest", User: "alice", Attrs: telemetry.Attrs{"value": "one"}}
	eventBytes, err := event.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	binaryRequest(t, first, http.MethodPost, "/ingest", eventBytes, map[string]string{"Content-Type": "application/json"}, http.StatusNoContent)
	trace := binaryCall(t, first, "trace", json.RawMessage(`{"request_id":"binary-ingest"}`))
	var eventValue any
	if err = json.Unmarshal(eventBytes, &eventValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trace["structuredContent"], map[string]any{"records": []any{eventValue}}) {
		t.Fatalf("trace: %v", trace)
	}
	stopFirst(syscall.SIGTERM)

	writeServices("first published description")
	second, stopSecond := launch("second", true)
	assertBinaryStop(t, second, "SIGTERM")
	// R-20BZ-B933
	landing = binaryRequest(t, second, http.MethodGet, "/", nil, nil, http.StatusOK)
	buttonRE := regexp.MustCompile(`(?is)<button(?:[\t\n\f\r ][^>]*)?>`)
	classRE := regexp.MustCompile(`(?i)(?:[\t\n\f\r ])class[\t\n\f\r ]*=[\t\n\f\r ]*(?:"([^"]*)"|'([^']*)'|([^\t\n\f\r >]+))`)
	launcher := false
	for _, button := range buttonRE.FindAll(landing, -1) {
		for _, match := range classRE.FindAllSubmatch(button, -1) {
			for _, value := range match[1:] {
				for _, token := range strings.FieldsFunc(html.UnescapeString(string(value)), binaryASCIISpace) {
					if token == "launcher" {
						launcher = true
					}
				}
			}
		}
	}
	if !launcher {
		t.Fatalf("launcher missing: %s", landing)
	}
	// R-TTRI-KNS8
	for _, description := range []string{"first published description", "updated published description"} {
		writeServices(description)
		if actual := binaryDiscover(t, second)["instructions"]; actual != description {
			t.Fatalf("instructions = %v, want %q", actual, description)
		}
	}
	stopSecond(syscall.SIGINT)

	third, stopThird := launch("third", false)
	assertBinaryStop(t, third, "SIGINT")
	stopThird(syscall.SIGTERM)
}

func binaryASCIISpace(value rune) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\f' || value == '\r'
}

func binaryRequest(t *testing.T, client *http.Client, method, path string, body []byte, headers map[string]string, status int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, "http://telemetry.test"+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/ingest" {
		req.Header.Set("X-User-Id", "alice")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s: status %d body %s", method, path, response.StatusCode, data)
	}
	return data
}

func binaryCall(t *testing.T, httpClient *http.Client, name string, args json.RawMessage) map[string]any {
	t.Helper()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://telemetry.test/mcp", HTTPClient: httpClient})
	result, err := client.CallTool(context.Background(), identity.Caller{UserID: "alice"}, name, args)
	if err != nil || result.IsError() {
		t.Fatalf("%s: error %v result %v", name, err, result)
	}
	data, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func binaryDiscover(t *testing.T, client *http.Client) map[string]any {
	t.Helper()
	request := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "server/discover", "params": map[string]any{"_meta": map[string]any{
		"io.modelcontextprotocol/protocolVersion":    mcp.ProtocolVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}}}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	data = binaryRequest(t, client, http.MethodPost, "/mcp", data, map[string]string{
		"Content-Type": "application/json", "MCP-Protocol-Version": mcp.ProtocolVersion, "Mcp-Method": "server/discover",
	}, http.StatusOK)
	var response struct {
		Result map[string]any `json:"result"`
	}
	if err = json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if response.Result == nil {
		t.Fatalf("discovery has no result: %s", data)
	}
	return response.Result
}

func binaryRecords(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("missing structuredContent: %v", result)
	}
	records, ok := structured["records"].([]any)
	if !ok {
		t.Fatalf("missing records: %v", structured)
	}
	values := make([]map[string]any, len(records))
	for i, record := range records {
		values[i], ok = record.(map[string]any)
		if !ok {
			t.Fatalf("invalid record: %v", record)
		}
	}
	return values
}

func assertBinaryStop(t *testing.T, client *http.Client, reason string) {
	t.Helper()
	records := binaryRecords(t, binaryCall(t, client, "search", json.RawMessage(`{"services":["telemetry"],"events":["service.stopping"],"limit":1}`)))
	if len(records) != 1 || records[0]["request_id"] != "" || records[0]["user"] != "" || !reflect.DeepEqual(records[0]["attrs"], map[string]any{"reason": reason}) {
		t.Fatalf("stop records for %s: %v", reason, records)
	}
}
