package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
)

const binarySitesIcon = `<svg viewBox="0 0 24 24"><path d="M4 4h16v16H4z"/></svg>`

// R-W4OI-ROHH R-W74B-J7YV R-YX1P-GMHL R-YY9L-UE8A R-28RY-26BD R-W9K4-ARG9
// R-WAS0-OJ6Y R-Z357-DH72 R-R02K-R24N R-WBZX-2AXN R-W7MM-ZP1J
// R-WILQ-FMPS R-66QV-EPPN
func TestBinary(t *testing.T) {
	t.Setenv("IKIGENBA_SERVICES", "")
	t.Setenv(version.CommitVariable, "abcdef0123456789-dirty")
	t.Setenv(version.ReleaseVariable, "release fixture")
	v := version.Display()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	binary := filepath.Join(root, "sites-binary")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v\n%s", e, output)
	}
	env := []string{version.CommitVariable + "=abcdef0123456789-dirty", version.ReleaseVariable + "=release fixture", "PATH=" + filepath.Dir(git), "HOME=" + root, "XDG_CONFIG_HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "TMPDIR=" + root}
	var pending bytes.Buffer
	if err := db.Status(context.Background(), db.Config{Path: filepath.Join(root, "state", "sites.db"), Migrations: sites.Migrations()}, &pending); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args        []string
		code        int
		out, stderr string
	}{
		{[]string{"--version"}, 0, v + "\n", ""},
		{[]string{"manifest"}, 0, cli.Manifest, ""},
		{[]string{"--help"}, 0, cli.Usage, ""},
		{[]string{"db", "status"}, 0, pending.String(), ""},
		{[]string{"db", "status", "extra"}, cli.ExitUsage, "", "sites: unknown command 'extra'\n\nsee 'sites --help' for usage\n"},
		{[]string{"bogus"}, cli.ExitUsage, "", "sites: unknown command 'bogus'\n\nsee 'sites --help' for usage\n"},
		{nil, cli.ExitUsage, "", "sites: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
	} {
		cmd := exec.CommandContext(ctx, binary, c.args...)
		cmd.Dir = root
		cmd.Env = env
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
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
		if code != c.code || out.String() != c.out || stderr.String() != c.stderr {
			t.Fatalf("args %v: exit %d stdout %q stderr %q", c.args, code, out.String(), stderr.String())
		}
	}
	// No code identity in the child means one empty output line.
	emptyEnv := append([]string{}, env[2:]...)
	cmd := exec.CommandContext(ctx, binary, "--version")
	cmd.Dir = root
	cmd.Env = emptyEnv
	var emptyOut, emptyErr bytes.Buffer
	cmd.Stdout = &emptyOut
	cmd.Stderr = &emptyErr
	if err := cmd.Run(); err != nil || emptyOut.String() != "\n" || emptyErr.Len() != 0 {
		t.Fatal(err, emptyOut.String(), emptyErr.String())
	}

	work := filepath.Join(root, "sites")
	if err = os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repos", "state", "repos", "rep_0123456789abcdef.git")
	if err = os.MkdirAll(filepath.Dir(repo), 0700); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.Env = env
		cmd.Dir = root
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	runGit("init", "--bare", "--initial-branch=main", repo)
	runGit("--git-dir="+repo, "config", "ikigenba.owner", "owner")
	// The empty owned repository is sufficient for create, which does not publish.
	for i, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		f := startBinary(t, binary, work, env)
		if i == 0 {
			info, e := os.Stat(filepath.Join(work, "state", "sites.db"))
			if e != nil || !info.Mode().IsRegular() {
				t.Fatalf("database: %v %v", info, e)
			}
			entries, e := os.ReadDir(filepath.Join(work, "cache", "sites"))
			if e != nil || len(entries) != 0 {
				t.Fatalf("cache: %v %v", entries, e)
			}
			f.call(t, "create", `{"name":"alpha","repo":"rep_0123456789abcdef"}`)
		} else {
			result := f.call(t, "list", "")
			content := result["structuredContent"].(map[string]any)
			rows := content["sites"].([]any)
			if len(rows) != 1 || rows[0].(map[string]any)["name"] != "alpha" {
				t.Fatalf("persisted list: %v", content)
			}
		}
		list := f.call(t, "list", "")
		meta := list["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"]
		if !reflect.DeepEqual(meta, map[string]any{"name": pages.ServiceName, "version": v}) {
			t.Fatalf("server info: %v", meta)
		}
		landing := f.get(t, "/")
		footers := regexp.MustCompile(`(?is)<footer(?:\s[^>]*)?>(.*?)</footer>`).FindAllStringSubmatch(landing, -1)
		if len(regexp.MustCompile(`(?i)<footer(?:>|[ \t\r\n\f])`).FindAllString(landing, -1)) != 1 || len(footers) != 1 || html.UnescapeString(strings.Trim(footers[0][1], " \t\r\n\f")) != pages.ServiceName+" "+v {
			t.Fatalf("footer: %v", footers)
		}
		about := f.get(t, "/about")
		version := regexp.MustCompile(`(?is)<dd\b[^>]*\bid="about-version"[^>]*>(.*?)</dd>`).FindStringSubmatch(about)
		if len(version) != 2 || html.UnescapeString(strings.Trim(version[1], " \t\r\n\f")) != v {
			t.Fatalf("about: %s", about)
		}
		f.instructions(t, false, "")
		f.stop(t, sig)
		assertUndelivered(t, f.stderr.String(), sig, v)
	}
	fEmpty := startBinary(t, binary, work, emptyEnv)
	emptyLanding := fEmpty.get(t, "/")
	footer := regexp.MustCompile(`(?is)<footer(?:\s[^>]*)?>(.*?)</footer>`).FindAllStringSubmatch(emptyLanding, -1)
	if len(regexp.MustCompile(`(?i)<footer(?:>|[ \t\r\n\f])`).FindAllString(emptyLanding, -1)) != 1 || len(footer) != 1 || html.UnescapeString(strings.Trim(footer[0][1], " \t\r\n\f")) != pages.ServiceName {
		t.Fatal(footer)
	}
	emptyAbout := fEmpty.get(t, "/about")
	aboutVersion := regexp.MustCompile(`(?is)<dd\b[^>]*\bid="about-version"[^>]*>(.*?)</dd>`).FindStringSubmatch(emptyAbout)
	if len(aboutVersion) != 2 || html.UnescapeString(strings.Trim(aboutVersion[1], " \t\r\n\f")) != "" {
		t.Fatal(emptyAbout)
	}
	emptyList := fEmpty.call(t, "list", "")
	if !reflect.DeepEqual(emptyList["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"], map[string]any{"name": pages.ServiceName, "version": ""}) {
		t.Fatal(emptyList)
	}
	fEmpty.stop(t, syscall.SIGTERM)
	assertUndelivered(t, fEmpty.stderr.String(), syscall.SIGTERM, "")

	services := filepath.Join(root, "services.json")
	writeServices := func(disabled bool, description string) {
		t.Helper()
		data := fmt.Sprintf(`{"services":[{"name":"auth","url":"https://account.example.test","description":"Auth","socket":"/unused","enabled":true,"mcp":false,"icon":"<svg></svg>"},{"name":"dummy","url":"https://dummy.example.test","description":"Dummy","socket":"/unused","enabled":%t,"mcp":false,"icon":"<svg></svg>"},{"name":"sites","url":"https://sites.example.test","description":%q,"socket":"/unused","enabled":true,"mcp":true,"icon":%q}]}`, !disabled, description, binarySitesIcon)
		if e := os.WriteFile(services, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	writeServices(false, "First description")
	f := startBinary(t, binary, work, append(append([]string{}, env...), "IKIGENBA_SERVICES="+services))
	beforeBody := f.get(t, "/")
	assertBinaryBanner(t, beforeBody)
	before := binaryTags(beforeBody)
	assertTagCount(t, before, "form", "", 1)
	assertTagCount(t, before, "", "aria-current", 1)
	assertTagCount(t, before, "", "aria-disabled", 0)
	assertTag(t, before, "a", "class", "profile", map[string]string{"title": "mg@example.com", "href": "https://account.example.test/"})
	assertTag(t, before, "form", "action", "https://account.example.test/logout", nil)
	assertTag(t, before, "button", "class", "launcher", nil)
	assertTag(t, before, "script", "src", "/_appkit/launcher.js", nil)
	assertTag(t, before, "a", "aria-current", "page", map[string]string{"href": "https://sites.example.test"})
	f.instructions(t, true, "First description")
	writeServices(true, "Changed description")
	afterBody := f.get(t, "/")
	assertBinaryBanner(t, afterBody)
	after := binaryTags(afterBody)
	assertTagCount(t, after, "", "aria-disabled", 1)
	assertTag(t, after, "a", "aria-disabled", "true", map[string]string{"title": "dummy is unavailable"})
	f.instructions(t, true, "Changed description")
	// Each broken or absent entry form is observed by the same live server.
	for _, data := range []string{`invalid`, `{"services":[]}`} {
		if e := os.WriteFile(services, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
		f.instructions(t, false, "")
	}
	if e := os.Remove(services); e != nil {
		t.Fatal(e)
	}
	f.instructions(t, false, "")
	f.stop(t, syscall.SIGTERM)
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		short := binaryShortDir(t)
		ln, e := net.Listen("unix", filepath.Join(short, "ingest"))
		if e != nil {
			t.Fatal(e)
		}
		var mu sync.Mutex
		var events []map[string]any
		server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/ingest" {
				t.Errorf("ingest request: %s %s", r.Method, r.URL.Path)
			}
			var event map[string]any
			if e := json.NewDecoder(r.Body).Decode(&event); e != nil {
				t.Error(e)
			}
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		})}
		done := make(chan struct{})
		go func() { _ = server.Serve(ln); close(done) }()
		t.Cleanup(func() { _ = server.Close(); <-done })
		data := fmt.Sprintf(`{"services":[{"name":"telemetry","url":"https://telemetry.example.test","description":"Events","socket":%q,"enabled":true,"mcp":false}]}`, ln.Addr().String())
		if e = os.WriteFile(services, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
		f = startBinary(t, binary, work, append(append([]string{}, env...), "IKIGENBA_SERVICES="+services))
		f.stop(t, sig)
		if f.stdout.String() != "" || f.stderr.String() != "" {
			t.Fatalf("delivered streams: %q %q", f.stdout.String(), f.stderr.String())
		}
		mu.Lock()
		saved := append([]map[string]any{}, events...)
		mu.Unlock()
		assertLifecycle(t, saved, sig, v)
	}
}

type binaryFixture struct {
	cmd            *exec.Cmd
	done           chan error
	stdout, stderr bytes.Buffer
	listener       *net.UnixListener
	http           *http.Client
	path           string
	stopped        bool
}

func binaryShortDir(t *testing.T) string {
	t.Helper()
	p, e := os.MkdirTemp("", "sites-bin-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(p) })
	return p
}
func startBinary(t *testing.T, binary, work string, env []string) *binaryFixture {
	t.Helper()
	short := binaryShortDir(t)
	path := filepath.Join(short, "http")
	ln, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	ln.SetUnlinkOnClose(false)
	t.Cleanup(func() { _ = ln.Close() })
	file, e := ln.File()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = file.Close() }()
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = notify.Close() }()
	f := &binaryFixture{listener: ln, path: path, done: make(chan error, 1)}
	f.cmd = exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary)
	f.cmd.Dir = work
	f.cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
	f.cmd.ExtraFiles = []*os.File{file}
	f.cmd.Stdout = &f.stdout
	f.cmd.Stderr = &f.stderr
	if e = f.cmd.Start(); e != nil {
		t.Fatal(e)
	}
	go func() { f.done <- f.cmd.Wait() }()
	t.Cleanup(func() {
		if !f.stopped {
			_ = f.cmd.Process.Kill()
			<-f.done
		}
	})
	if e = notify.SetReadDeadline(time.Now().Add(10 * time.Second)); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 100)
	n, _, e := notify.ReadFromUnix(b)
	if e != nil {
		t.Fatalf("readiness: %v", e)
	}
	if string(b[:n]) != "READY=1" {
		t.Fatalf("readiness: %q", b[:n])
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	f.http = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	return f
}
func (f *binaryFixture) stop(t *testing.T, sig syscall.Signal) {
	t.Helper()
	f.http.CloseIdleConnections()
	if e := f.cmd.Process.Signal(sig); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-f.done:
		f.stopped = true
		if e != nil {
			t.Fatalf("signal %v: %v stderr %s", sig, e, f.stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("binary did not stop")
	}
	if f.stdout.Len() != 0 {
		t.Fatalf("stdout: %q", f.stdout.String())
	}
	if _, e := os.Stat(f.path); e != nil {
		t.Fatalf("inherited socket removed: %v", e)
	}
	c, e := net.DialTimeout("unix", f.path, time.Second)
	if e != nil {
		t.Fatalf("socket no longer queues connections: %v", e)
	}
	_ = c.Close()
}
func (f *binaryFixture) get(t *testing.T, path string) string {
	t.Helper()
	r, e := http.NewRequest("GET", "http://sites.sbx.ikigenba.dev"+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("X-User-Id", "owner")
	r.Header.Set("X-User-Email", "mg@example.com")
	r.Header.Set("X-Forwarded-Proto", "https")
	res, e := f.http.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = res.Body.Close() }()
	b, e := io.ReadAll(res.Body)
	if e != nil || res.StatusCode != 200 {
		t.Fatalf("GET: %d %v %s", res.StatusCode, e, b)
	}
	return string(b)
}
func (f *binaryFixture) call(t *testing.T, name, args string) map[string]any {
	t.Helper()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://sites.sbx.ikigenba.dev/mcp", HTTPClient: f.http})
	var arguments json.RawMessage
	if args != "" {
		arguments = json.RawMessage(args)
	}
	r, e := client.CallTool(context.Background(), identity.Caller{UserID: "owner"}, name, arguments)
	if e != nil || r.IsError() {
		t.Fatalf("%s: %v %v", name, e, r)
	}
	b, e := r.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var data map[string]any
	if e = json.Unmarshal(b, &data); e != nil {
		t.Fatal(e)
	}
	return data
}
func (f *binaryFixture) instructions(t *testing.T, present bool, want string) {
	t.Helper()
	for _, method := range []string{"server/discover", "initialize"} {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":{"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{}},"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`, method, mcp.ProtocolVersion, mcp.ProtocolVersion)
		if method == "initialize" {
			body = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`
		}
		r, e := http.NewRequest("POST", "http://sites.sbx.ikigenba.dev/mcp", strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("X-User-Id", "owner")
		r.Header.Set("Content-Type", "application/json")
		if method == "server/discover" {
			r.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
			r.Header.Set("Mcp-Method", method)
		}
		res, e := f.http.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		var data struct {
			Result map[string]any `json:"result"`
		}
		e = json.NewDecoder(res.Body).Decode(&data)
		_ = res.Body.Close()
		if e != nil || res.StatusCode != 200 {
			t.Fatalf("%s response: %d %v", method, res.StatusCode, e)
		}
		v, ok := data.Result["instructions"]
		if ok != present || (present && v != want) {
			t.Fatalf("%s instructions %v present %t want %q %t", method, v, ok, want, present)
		}
	}
}
func assertUndelivered(t *testing.T, text string, sig syscall.Signal, v string) {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		prefix := "sites: undelivered event: "
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("stderr line: %q", line)
		}
		var event map[string]any
		if e := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &event); e != nil {
			t.Fatal(e)
		}
		events = append(events, event)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatal("missing event newline")
	}
	assertLifecycle(t, events, sig, v)
}
func assertLifecycle(t *testing.T, events []map[string]any, sig syscall.Signal, v string) {
	t.Helper()
	if len(events) < 2 {
		t.Fatalf("events: %v", events)
	}
	first, last := events[0], events[len(events)-1]
	reason := "SIGTERM"
	if sig == syscall.SIGINT {
		reason = "SIGINT"
	}
	if first["event"] != "service.started" || last["event"] != "service.stopping" || !reflect.DeepEqual(last["attrs"], map[string]any{"reason": reason}) {
		t.Fatalf("lifecycle: %v", events)
	}
	if !reflect.DeepEqual(first["attrs"], map[string]any{"version": v}) {
		t.Fatalf("started version: %v", first)
	}
	for _, event := range []map[string]any{first, last} {
		if event["service"] != pages.ServiceName || event["request_id"] != "" || event["user"] != "" {
			t.Fatalf("lifecycle identity: %v", event)
		}
	}
}

// R-66QV-EPPN: observe the kit's banner in the binary's response.
func assertBinaryBanner(t *testing.T, body string) {
	t.Helper()
	headers := regexp.MustCompile(`(?is)<header\b[^>]*>(.*?)</header>`).FindAllStringSubmatch(body, -1)
	if len(headers) == 0 {
		t.Fatalf("banner headers: %d", len(headers))
	}
	header := headers[0][1]
	assertTag(t, binaryTags(header), "strong", "class", "mark", map[string]string{"data-service": pages.ServiceName})
	marks := regexp.MustCompile(`(?is)<strong\b[^>]*>(.*?)</strong>`).FindAllStringSubmatch(header, -1)
	if len(marks) != 1 {
		t.Fatalf("banner marks: %d", len(marks))
	}
	mark := marks[0][1]
	assertTag(t, binaryTags(mark), "img", "src", "/_appkit/favicon.svg", map[string]string{"alt": ""})
	assertTagCount(t, binaryTags(mark), "img", "", 1)
	favicon := regexp.MustCompile(`(?is)<img\b[^>]*>`).FindString(mark)
	if !strings.HasPrefix(strings.TrimLeft(mark, " \t\r\n\f"), favicon) {
		t.Fatal("banner mark does not begin with favicon")
	}
	assertTag(t, binaryTags(mark), "span", "class", "service", nil)
	services := regexp.MustCompile(`(?is)<span\b[^>]*>(.*?)</span>`).FindAllStringSubmatch(mark, -1)
	if len(services) != 1 || strings.TrimSpace(services[0][1]) != binarySitesIcon+pages.ServiceName {
		t.Fatalf("banner service icon and name: %v", services)
	}
	visible := func(s string) string {
		return strings.Join(strings.Fields(html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, ""))), " ")
	}
	product := strings.SplitN(mark, services[0][0], 2)[0]
	if visible(product) != "Ikigenba" {
		t.Fatalf("banner product text: %q", visible(product))
	}
	if !strings.HasPrefix(strings.TrimLeft(header, " \t\r\n\f"), marks[0][0]) {
		t.Fatal("banner does not begin with mark")
	}
	afterMark := strings.TrimLeft(strings.SplitN(header, marks[0][0], 2)[1], " \t\r\n\f")
	buttons := regexp.MustCompile(`(?is)<button\b[^>]*>`).FindAllString(afterMark, -1)
	if len(buttons) == 0 || !strings.HasPrefix(afterMark, buttons[0]) {
		t.Fatal("launcher does not immediately follow mark")
	}
	assertTag(t, binaryTags(buttons[0]), "button", "class", "launcher", nil)
	assertTag(t, binaryTags(header), "button", "class", "signout", map[string]string{"type": "submit", "aria-label": "Sign out", "title": "Sign out"})
	for _, button := range regexp.MustCompile(`(?is)<button\b[^>]*>(.*?)</button>`).FindAllStringSubmatch(header, -1) {
		for _, tag := range binaryTags(button[0]) {
			if tag.name == "button" && strings.Contains(" "+tag.attrs["class"]+" ", " signout ") {
				assertTagCount(t, binaryTags(button[1]), "svg", "", 1)
				if visible(button[1]) != "" {
					t.Fatalf("sign-out button has visible text: %q", visible(button[1]))
				}
			}
		}
	}
}

type binaryTag struct {
	name  string
	attrs map[string]string
}

func binaryTags(body string) []binaryTag {
	var tags []binaryTag
	starts := regexp.MustCompile(`(?is)<([a-z][a-z0-9]*)\b([^>]*)>`)
	attrs := regexp.MustCompile(`([a-zA-Z][-a-zA-Z0-9]*)\s*=\s*"([^"]*)"`)
	for _, m := range starts.FindAllStringSubmatch(body, -1) {
		tag := binaryTag{name: strings.ToLower(m[1]), attrs: map[string]string{}}
		for _, a := range attrs.FindAllStringSubmatch(m[2], -1) {
			tag.attrs[strings.ToLower(a[1])] = html.UnescapeString(a[2])
		}
		tags = append(tags, tag)
	}
	return tags
}
func assertTag(t *testing.T, tags []binaryTag, name, key, value string, extra map[string]string) {
	t.Helper()
	count := 0
	for _, tag := range tags {
		v := tag.attrs[key]
		match := v == value
		if key == "class" {
			match = false
			for _, class := range strings.Fields(v) {
				if class == value {
					match = true
				}
			}
		}
		if !match {
			continue
		}
		count++
		if tag.name != name {
			t.Fatalf("tag %s want %s", tag.name, name)
		}
		for k, want := range extra {
			if tag.attrs[k] != want {
				t.Fatalf("%s %s=%q want %q", name, k, tag.attrs[k], want)
			}
		}
	}
	if count != 1 {
		t.Fatalf("%s[%s=%q]: %d tags", name, key, value, count)
	}
}

func assertTagCount(t *testing.T, tags []binaryTag, name, attribute string, want int) {
	t.Helper()
	count := 0
	for _, tag := range tags {
		if name != "" && tag.name != name {
			continue
		}
		if attribute != "" {
			if _, present := tag.attrs[attribute]; !present {
				continue
			}
		}
		count++
	}
	if count != want {
		t.Fatalf("tags named %q carrying %q: %d, want %d", name, attribute, count, want)
	}
}
