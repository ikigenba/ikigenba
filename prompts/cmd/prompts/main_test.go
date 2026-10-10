package main_test

import (
	"bufio"
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
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/cli"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
)

func binaryModel(t *testing.T) string {
	t.Helper()
	for _, e := range agentkit.Catalog() {
		o, err := agent.Offering(e.Model)
		if err == nil && o.Host == agentkit.HostAnthropic {
			return e.Model
		}
	}
	t.Fatal("no Anthropic offering in catalog")
	return ""
}
func binaryAnswer(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
	for _, e := range []string{`{"type":"message_start","message":{"id":"fixture","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, string(delta), `{"type":"content_block_stop","index":0}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`, `{"type":"message_stop"}`} {
		_, _ = io.WriteString(w, "data: "+e+"\n\n")
	}
}
func binaryWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(20 * time.Second):
		t.Fatal("binary did not finish")
		var z T
		return z
	}
}

// R-MDHX-L3A4 R-MEPT-YV0T R-MFXQ-CMRI R-MH5M-QEI7 R-MIDJ-468W R-LZF8-6HY0 R-M0N4-K9OP R-M1V0-Y1FE R-5O4C-7YOX R-MN94-N97O R-MOH1-10YD R-MPOX-ESP2 R-MQWT-SKFR R-HHBT-L6RK R-91VP-25Q6
func TestBinaryWiring(t *testing.T) {
	t.Setenv(services.Variable, "")
	t.Setenv(version.CommitVariable, "abcdef1234567890")
	t.Setenv(version.ReleaseVariable, "fixture-release")
	id := version.Read()
	display := version.Display()
	root := t.TempDir()
	binary := filepath.Join(root, "prompts")
	build := exec.Command("go", "build", "-o", binary, ".")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v\n%s", e, b)
	}
	env := []string{"PATH=/chosen/path", "HOME=" + root, version.CommitVariable + "=abcdef1234567890", version.ReleaseVariable + "=fixture-release"}
	for _, host := range []agentkit.Host{agentkit.HostAnthropic, agentkit.HostOpenAI, agentkit.HostGemini, agentkit.HostXAI, agentkit.HostOpenRouter} {
		env = append(env, agent.KeyVariable(host)+"=binary-fixture-key")
	}
	run := func(args []string, environment []string, input []byte) (string, string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = t.TempDir()
		cmd.Env = environment
		cmd.Stdin = bytes.NewReader(input)
		var out, errout bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errout
		e := cmd.Run()
		code := 0
		if e != nil {
			var exit *exec.ExitError
			if errors.As(e, &exit) {
				code = exit.ExitCode()
			} else {
				t.Fatal(e)
			}
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		return out.String(), errout.String(), code
	}
	for _, tc := range []struct {
		args []string
		out  string
		code int
	}{{[]string{"--version"}, display + "\n", 0}, {[]string{"manifest"}, cli.Manifest, 0}, {[]string{"bogus"}, "", cli.ExitUsage}, {nil, "", cli.ExitUsage}} {
		out, diag, code := run(tc.args, env, nil)
		if out != tc.out || code != tc.code {
			t.Fatalf("command %v: %q %q %d", tc.args, out, diag, code)
		}
		if tc.code == 0 && diag != "" {
			t.Fatal(diag)
		}
		if tc.code != 0 && !strings.HasPrefix(diag, "prompts: ") {
			t.Fatal(diag)
		}
	}
	out, diag, code := run([]string{"--version"}, []string{"PATH=/chosen/path", "HOME=" + root}, nil)
	if out != "\n" || diag != "" || code != 0 {
		t.Fatal(out, diag, code)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { binaryAnswer(w, "binary supplied answer") }))
	defer provider.Close()
	folder := t.TempDir()
	work := filepath.Join(folder, "work")
	if e := os.Mkdir(work, 0700); e != nil {
		t.Fatal(e)
	}
	spec, _ := json.Marshal(agent.Spec{Model: binaryModel(t), Prompt: "supplied prompt", BaseURL: provider.URL, Key: "binary-fixture-key"})
	agentEnv := append(append([]string(nil), env...), "IKIGENBA_RUN_DIR="+folder, "IKIGENBA_WORK_DIR="+work, "IKIGENBA_RUN_ID=prr_0011223344556677", services.Variable+"=")
	out, diag, code = run([]string{agent.Command}, agentEnv, spec)
	if code != 0 || diag != "" || !strings.Contains(out, "binary supplied answer") {
		t.Fatalf("agent %d %q %q", code, out, diag)
	}
	short, e := os.MkdirTemp("", "prompts-binary-")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = os.RemoveAll(short) }()
	socket := filepath.Join(short, "app.sock")
	ln, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = ln.Close() }()
	file, e := ln.File()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = file.Close() }()
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify.sock"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = notify.Close() }()
	telemetrySocket := filepath.Join(short, "telemetry.sock")
	sinkln, e := net.Listen("unix", telemetrySocket)
	if e != nil {
		t.Fatal(e)
	}
	events := make(chan telemetry.Event, 256)
	sink := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire struct {
			Time      time.Time
			Service   string
			Name      string `json:"event"`
			RequestID string `json:"request_id"`
			User      string
			Attrs     telemetry.Attrs
		}
		if e := json.NewDecoder(r.Body).Decode(&wire); e != nil {
			t.Error(e)
		}
		events <- telemetry.Event{Time: wire.Time, Service: wire.Service, Name: wire.Name, RequestID: wire.RequestID, User: wire.User, Attrs: wire.Attrs}
		w.WriteHeader(204)
	}), ReadHeaderTimeout: time.Second}
	go func() { _ = sink.Serve(sinkln) }()
	defer func() { _ = sink.Close() }()
	servicesFile := filepath.Join(root, "services.json")
	writeServices := func(entries []map[string]any) {
		t.Helper()
		b, e := json.Marshal(map[string]any{"services": entries})
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(servicesFile, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	entry := func(name, description, sock string) map[string]any {
		return map[string]any{"name": name, "url": "https://fixture.example", "description": description, "socket": sock, "enabled": true, "mcp": true}
	}
	writeServices([]map[string]any{entry("telemetry", "sink", telemetrySocket)})
	dir := t.TempDir()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	httpc := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://backend/mcp", HTTPClient: httpc, Name: "binary-fixture", Version: "fixture"})
	call := func(name string, args any) mcp.Result {
		t.Helper()
		b, _ := json.Marshal(args)
		if args == nil {
			b = nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r, e := client.CallTool(ctx, identity.Caller{UserID: "binary-user"}, name, b)
		if e != nil || r.IsError() {
			t.Fatalf("%s: %v %#v", name, e, r)
		}
		return r
	}
	get := func(path string) string {
		t.Helper()
		req, _ := http.NewRequest("GET", "http://backend"+path, nil)
		req.Host = "prompts.sbx.ikigenba.dev"
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-User-Id", "binary-user")
		req.Header.Set("X-User-Email", "mg@example.com")
		r, e := httpc.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = r.Body.Close() }()
		b, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 {
			t.Fatalf("%s %d: %s", path, r.StatusCode, b)
		}
		return string(b)
	}
	discover := func() map[string]json.RawMessage {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "server/discover", "params": map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": mcp.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}})
		req, _ := http.NewRequest("POST", "http://backend/mcp", bytes.NewReader(body))
		req.Header.Set("X-User-Id", "binary-user")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
		req.Header.Set("Mcp-Method", "server/discover")
		r, e := httpc.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = r.Body.Close() }()
		var envelope struct{ Result map[string]json.RawMessage }
		if e = json.NewDecoder(r.Body).Decode(&envelope); e != nil || r.StatusCode != 200 {
			t.Fatal(e, r.StatusCode)
		}
		return envelope.Result
	}
	start := func(servicesPath string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer, chan error) {
		t.Helper()
		cmd := exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary)
		cmd.Dir = dir
		cmd.Env = append(append([]string(nil), env...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
		if servicesPath != "" {
			cmd.Env = append(cmd.Env, services.Variable+"="+servicesPath)
		}
		cmd.ExtraFiles = []*os.File{file}
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		_ = notify.SetReadDeadline(time.Now().Add(10 * time.Second))
		b := make([]byte, 128)
		n, _, e := notify.ReadFromUnix(b)
		if e != nil || string(b[:n]) != "READY=1" {
			t.Fatalf("ready %q %v", b[:n], e)
		}
		return cmd, stdout, stderr, done
	}
	stop := func(cmd *exec.Cmd, stdout, stderr *bytes.Buffer, done chan error, sig syscall.Signal) {
		t.Helper()
		if e := cmd.Process.Signal(sig); e != nil {
			t.Fatal(e)
		}
		if e := binaryWait(t, done); e != nil {
			t.Fatal(e)
		}
		if stdout.Len() != 0 {
			t.Fatal(stdout.String())
		}
		lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
		hasSink := false
		for _, v := range cmd.Env {
			if strings.HasPrefix(v, services.Variable+"=") {
				hasSink = true
			}
		}
		if hasSink && stderr.Len() != 0 && (len(lines) != 1 || !strings.HasPrefix(lines[0], "prompts: runs are unavailable: ")) {
			t.Fatal(stderr.String())
		}
		transport.CloseIdleConnections()
	}
	cmd, stdout, stderr, done := start("")
	if info, e := os.Stat(filepath.Join(dir, "state", "prompts.db")); e != nil || !info.Mode().IsRegular() {
		t.Fatal(info, e)
	}
	if entries, e := os.ReadDir(filepath.Join(dir, "state", "runs")); e != nil || len(entries) != 0 {
		t.Fatal(entries, e)
	}
	call("create", map[string]any{"name": "alpha", "model": binaryModel(t), "prompt": "supplied"})
	kit := page.New(pages.ServiceName, id)
	var footer bytes.Buffer
	if e := page.Templates().ExecuteTemplate(&footer, "footer", kit.Banner(page.User{})); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(get("/"), footer.String()) {
		t.Fatal("landing lacks identity footer")
	}
	banner := kit.Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
	banner.Trail = []page.Level{{Name: "about", URL: "/about"}}
	set, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	wantAbout := httptest.NewRecorder()
	set.Write(wantAbout, httptest.NewRequest("GET", "/about", nil), http.StatusOK, "about", pages.AboutData{Banner: banner, Description: pages.Description})
	if got := get("/about"); got != wantAbout.Body.String() {
		t.Fatalf("about differs from identity template: %q", got)
	}
	r := call("list", nil)
	b, _ := r.MarshalJSON()
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if e := json.Unmarshal(b, &envelope); e != nil {
		t.Fatal(e)
	}
	var info struct{ Name, Version string }
	if e := json.Unmarshal(envelope.Meta["io.modelcontextprotocol/serverInfo"], &info); e != nil || info.Name != pages.ServiceName || info.Version != display {
		t.Fatal(info, e)
	}
	if _, ok := discover()["instructions"]; ok {
		t.Fatal("instructions without services")
	}
	stop(cmd, stdout, stderr, done, syscall.SIGTERM)
	queued, e := net.Dial("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = queued.Close() }()
	_ = queued.SetDeadline(time.Now().Add(10 * time.Second))
	_, e = io.WriteString(queued, "GET /about HTTP/1.1\r\nHost: backend\r\nX-User-Id: binary-user\r\nConnection: close\r\n\r\n")
	if e != nil {
		t.Fatal(e)
	}
	cmd, stdout, stderr, done = start(servicesFile)
	response, e := http.ReadResponse(bufio.NewReader(queued), &http.Request{Method: "GET"})
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	_ = response.Body.Close()
	first := binaryWait(t, events)
	if first.Name != "service.started" || first.Service != pages.ServiceName || first.RequestID != "" || first.User != "" || len(first.Attrs) != 1 || first.Attrs["version"] != display {
		t.Fatal(first)
	}
	r = call("list", nil)
	b, _ = r.MarshalJSON()
	var listed struct {
		Structured struct{ Prompts []struct{ Name string } } `json:"structuredContent"`
	}
	if e = json.Unmarshal(b, &listed); e != nil || len(listed.Structured.Prompts) != 1 || listed.Structured.Prompts[0].Name != "alpha" {
		t.Fatal(string(b), e)
	}
	writeServices([]map[string]any{entry("telemetry", "sink", telemetrySocket), entry(pages.ServiceName, "supplied instructions", socket)})
	var instructions string
	if e = json.Unmarshal(discover()["instructions"], &instructions); e != nil || instructions != "supplied instructions" {
		t.Fatal(instructions, e)
	}
	// The banner reads the file again on the next request.
	bannerEntries := []map[string]any{}
	for _, pair := range [][2]string{{"auth", "https://auth.fixture.example"}, {"home", "https://home.fixture.example"}, {"prompts", "https://prompts.fixture.example"}} {
		e := entry(pair[0], "supplied description", "")
		e["url"] = pair[1]
		e["icon"] = "<svg></svg>"
		bannerEntries = append(bannerEntries, e)
	}
	writeServices(bannerEntries)
	bannerGet := func() string {
		t.Helper()
		req, _ := http.NewRequest("GET", "http://backend/", nil)
		req.Host = "prompts.sbx.ikigenba.dev"
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-User-Id", "binary-user")
		req.Header.Set("X-User-Email", "mg@example.com")
		r, e := httpc.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = r.Body.Close() }()
		b, e := io.ReadAll(r.Body)
		if e != nil || r.StatusCode != 200 {
			t.Fatal(e, r.StatusCode)
		}
		return string(b)
	}
	body := bannerGet()
	for _, value := range []string{"mg@example.com", "https://auth.fixture.example/", "https://auth.fixture.example/logout", "https://home.fixture.example"} {
		if !strings.Contains(body, value) {
			t.Fatal("banner lacks supplied value", value)
		}
	}
	bannerEntries[1]["url"] = "https://changed.fixture.example"
	writeServices(bannerEntries)
	body = bannerGet()
	if !strings.Contains(body, "https://changed.fixture.example") || strings.Contains(body, "https://home.fixture.example") {
		t.Fatal("banner did not reread services")
	}
	for _, content := range []string{`{"services":[]}`, `invalid services`} {
		if e = os.WriteFile(servicesFile, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
		if _, ok := discover()["instructions"]; ok {
			t.Fatal("unexpected instructions")
		}
	}
	if e = os.Remove(servicesFile); e != nil {
		t.Fatal(e)
	}
	if _, ok := discover()["instructions"]; ok {
		t.Fatal("instructions for missing services")
	}
	writeServices([]map[string]any{entry("telemetry", "sink", telemetrySocket)})
	stop(cmd, stdout, stderr, done, syscall.SIGINT)
	var last telemetry.Event
	for {
		select {
		case event := <-events:
			last = event
		default:
			goto drained
		}
	}
drained:
	if last.Name != "service.stopping" || last.RequestID != "" || last.User != "" || len(last.Attrs) != 1 || last.Attrs["reason"] != "SIGINT" {
		t.Fatal(last)
	}
	// Observe both signal causes with a sink present.
	cmd, stdout, stderr, done = start(servicesFile)
	first = binaryWait(t, events)
	if first.Name != "service.started" {
		t.Fatal(first)
	}
	stop(cmd, stdout, stderr, done, syscall.SIGTERM)
	last = binaryWait(t, events)
	if last.Name != "service.stopping" || last.Attrs["reason"] != "SIGTERM" {
		t.Fatal(last)
	}
}
