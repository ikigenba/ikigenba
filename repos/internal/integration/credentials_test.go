package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	repogit "github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

var credentialTime = time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)

type credential struct {
	header string
	parts  []string
}

// R-Y0WW-GAO7: Validate the credential definition used by every request below.
func credentials(t *testing.T) []credential {
	t.Helper()
	u, p := "QAZWSXEDCRFVTGBYU", "PLMOKNIJBUHVYGCT"
	b := base64.StdEncoding.EncodeToString([]byte(u + ":" + p))
	for _, value := range []string{u, p} {
		if len(value) < 16 || strings.IndexFunc(value, func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
		}) >= 0 {
			t.Fatal("credential is not sixteen or more ASCII letters and digits")
		}
	}
	for _, value := range []string{u, p, b} {
		for i := 0; i+8 <= len(value); i++ {
			if strings.IndexFunc(value[i:i+8], func(r rune) bool { return r >= 'A' && r <= 'Z' }) < 0 {
				t.Fatal("credential lacks an uppercase letter in an eight-character window")
			}
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(b)
	if err != nil || string(decoded) != u+":"+p {
		t.Fatal("Basic credential is not standard padded base64")
	}
	return []credential{{"Basic " + b, []string{u, p, b}}, {"Bearer " + p, []string{p}}}
}

// Finding no length-eight window excludes every longer secret substring too.
func secretPresent(text string, parts []string) bool {
	for _, part := range parts {
		for i := 0; i+8 <= len(part); i++ {
			if strings.Contains(text, part[i:i+8]) {
				return true
			}
		}
	}
	return false
}

type inspection struct {
	mu       sync.Mutex
	parts    []string
	headers  map[string][]string
	problems []string
	requests map[string]int
}

func newInspection(cs []credential) *inspection {
	a := &inspection{headers: make(map[string][]string), requests: make(map[string]int)}
	for _, c := range cs {
		a.parts = append(a.parts, c.parts...)
		a.headers[c.header] = c.parts
	}
	return a
}

func (a *inspection) check(where, text string) {
	if secretPresent(text, a.parts) {
		a.mu.Lock()
		a.problems = append(a.problems, where+" contains a credential substring")
		a.mu.Unlock()
	}
}

func (a *inspection) problem(text string) {
	a.mu.Lock()
	a.problems = append(a.problems, text)
	a.mu.Unlock()
}

func (a *inspection) request(r *http.Request) {
	a.check("request method", r.Method)
	a.check("request URL", r.URL.String())
	a.check("request host", r.Host)
	values := r.Header.Values("Authorization")
	if len(values) > 0 && (len(values) != 1 || a.headers[values[0]] == nil) {
		a.problem("request does not carry exactly one declared credential header")
	}
	for name, values := range r.Header {
		if strings.EqualFold(name, "Authorization") {
			continue
		}
		a.check("other request header name", name)
		for _, value := range values {
			a.check("other request header value", value)
		}
	}
	if r.Body != nil {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			a.problem("cannot inspect request body: " + err.Error())
		}
		if err := r.Body.Close(); err != nil {
			a.problem("cannot close inspected request body: " + err.Error())
		}
		a.check("request body", string(data))
		r.Body = io.NopCloser(bytes.NewReader(data))
	}
	a.mu.Lock()
	a.requests[r.Method+" "+r.URL.Path]++
	a.mu.Unlock()
}

func (a *inspection) response(r *http.Request, h http.Header, body []byte) {
	parts := a.headers[r.Header.Get("Authorization")]
	for _, values := range h {
		for _, value := range values {
			if secretPresent(value, parts) {
				a.problem("response header contains that request's credential substring")
			}
		}
	}
	if secretPresent(string(body), parts) {
		a.problem("response body contains that request's credential substring")
	}
}

func (a *inspection) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.request(r)
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		a.response(r, rec.Header(), rec.Body.Bytes())
		for key, values := range rec.Header() {
			w.Header()[key] = append([]string(nil), values...)
		}
		w.WriteHeader(rec.Code)
		if _, err := w.Write(rec.Body.Bytes()); err != nil {
			a.problem("cannot relay response: " + err.Error())
		}
	})
}

func (a *inspection) assert(t *testing.T) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, problem := range a.problems {
		t.Error(problem)
	}
	for _, route := range []string{"GET /", "GET /about", "GET /tools", "POST /mcp"} {
		if a.requests[route] == 0 {
			t.Errorf("route %s was never exercised", route)
		}
	}
}

type deterministicBytes struct {
	mu sync.Mutex
	n  byte
}

func (r *deterministicBytes) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

func credentialClock() time.Time                     { return credentialTime }
func credentialAfter(time.Duration) <-chan time.Time { return make(chan time.Time) }

func gitEnvironment(t *testing.T, root string) (string, []string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	return git, []string{
		"PATH=" + filepath.Dir(git), "HOME=" + root, "XDG_CONFIG_HOME=" + root,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_AUTHOR_DATE=2001-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2001-01-01T00:00:00Z",
		"LC_ALL=C",
	}
}

func runGit(t *testing.T, executable string, env []string, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir, cmd.Env = dir, env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git operation failed: %v\n%s", err, output)
	}
}

func fixtureFiles(t *testing.T, root string, a *inspection) {
	t.Helper()
	safe, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := safe.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = fs.WalkDir(safe.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		a.check("fixture path", path)
		if entry.Type().IsRegular() {
			data, err := safe.ReadFile(path)
			if err != nil {
				return err
			}
			a.check("regular file under test directory", string(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func servicesFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "services.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"repos","url":"https://repos.sbx.ikigenba.dev","enabled":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func credentialHTTP(t *testing.T, endpoint, method, path, auth string, caller bool, body string, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if caller {
		identity.Forward(identity.Caller{UserID: "u_fixture", Email: "fixture@example.invalid", RequestID: "req_fixture"}, req)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	for key, values := range response.Header {
		rec.Header()[key] = append([]string(nil), values...)
	}
	rec.Code = response.StatusCode
	if _, err := rec.Body.Write(data); err != nil {
		t.Fatal(err)
	}
	return rec
}

type credentialTransport struct {
	header string
}

func (c credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	if c.header != "" {
		r.Header.Set("Authorization", c.header)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func toolClient(endpoint, auth string) *mcp.Client {
	return mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint + "/mcp", HTTPClient: &http.Client{Transport: credentialTransport{auth}, Timeout: 10 * time.Second}})
}

func callTool(t *testing.T, client *mcp.Client, tool, args string, wantError bool) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, identity.Caller{UserID: "u_fixture", Email: "fixture@example.invalid", RequestID: "req_fixture"}, tool, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError() != wantError {
		t.Fatalf("%s result error=%t, want %t", tool, result.IsError(), wantError)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return fields["structuredContent"]
}

func exerciseCredentials(t *testing.T, endpoint string, cs []credential, a *inspection) {
	t.Helper()
	clientRoot := t.TempDir()
	executable, env := gitEnvironment(t, clientRoot)
	for i, c := range cs {
		for _, path := range []string{"/", "/about", "/tools", "/_appkit/theme.css", "/absent"} {
			response := credentialHTTP(t, endpoint, http.MethodGet, path, c.header, true, "", "")
			if (path == "/" || path == "/about" || path == "/tools") && response.Code != http.StatusOK {
				t.Fatalf("%s status=%d", path, response.Code)
			}
		}
		name := "notes" + strconv.Itoa(i)
		renamed := "archive" + strconv.Itoa(i)
		client := toolClient(endpoint, c.header)
		callTool(t, client, "create", `{"name":"`+name+`"}`, false)
		callTool(t, client, "list", `{}`, false)
		callTool(t, client, "show", `{"repo":"`+name+`"}`, false)
		callTool(t, client, "status", `{}`, false)
		cloneDir := filepath.Join(clientRoot, name)
		config := []string{"-c", "credential.helper=", "-c", "http.extraHeader=X-User-Id: u_fixture", "-c", "http.extraHeader=Authorization: " + c.header}
		runGit(t, executable, env, clientRoot, append(config, "clone", endpoint+"/"+name+".git", cloneDir)...)
		if err := os.WriteFile(filepath.Join(cloneDir, "note.txt"), []byte("small fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, executable, env, cloneDir, "add", "note.txt")
		runGit(t, executable, env, cloneDir, "commit", "-m", "fixture commit")
		fixtureFiles(t, clientRoot, a)
		runGit(t, executable, env, cloneDir, append(config, "push", "origin", "HEAD:refs/heads/main")...)
		// A second clone and a later push force a fetch carrying a non-empty pack.
		otherDir := filepath.Join(clientRoot, "second"+strconv.Itoa(i))
		runGit(t, executable, env, clientRoot, append(config, "clone", endpoint+"/"+name+".git", otherDir)...)
		if err := os.WriteFile(filepath.Join(cloneDir, "note.txt"), []byte("another small fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, executable, env, cloneDir, "commit", "-am", "next fixture commit")
		runGit(t, executable, env, cloneDir, append(config, "push", "origin", "HEAD:refs/heads/main")...)
		runGit(t, executable, env, otherDir, append(config, "fetch", "origin")...)
		callTool(t, client, "rename", `{"repo":"`+name+`","name":"`+renamed+`"}`, false)
		// Keep the pushed repository so the post-stop scan reaches its config,
		// refs, objects and reflogs; exercise deletion on a separate repository.
		discard := "discard" + strconv.Itoa(i)
		callTool(t, client, "create", `{"name":"`+discard+`"}`, false)
		callTool(t, client, "delete", `{"repo":"`+discard+`"}`, false)
		for _, refusal := range []struct{ tool, args string }{
			{"show", `{"repo":"absent"}`}, {"create", `{"name":"INVALID"}`},
			{"rename", `{"repo":"absent","name":"INVALID"}`}, {"delete", `{"repo":"absent"}`},
		} {
			callTool(t, client, refusal.tool, refusal.args, true)
		}
	}
	fixtureFiles(t, clientRoot, a)
}

type eventSink struct {
	capture telemetry.Capture
	failure error
}

func (s *eventSink) Deliver(ctx context.Context, event telemetry.Event) error {
	if err := s.capture.Deliver(ctx, event); err != nil {
		return err
	}
	return s.failure
}

// R-X7O3-6A9L R-X8VZ-K20A R-GUUE-SI18 R-XA3V-XTQZ R-XBBS-BLHO R-XCJO-PD8D: Drive the entire run,
// inspect every delivery attempt and stderr, and inspect every regular state file.
func TestRunDoesNotExportCredentials(t *testing.T) {
	for _, sinkMode := range []string{"delivered", "failed", "rejected"} {
		t.Run(sinkMode, func(t *testing.T) {
			t.Setenv("IKIGENBA_SERVICES", "")
			cs := credentials(t)
			a := newInspection(cs)
			dir := t.TempDir()
			services := servicesFixture(t, dir)
			executable, env := gitEnvironment(t, t.TempDir())
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			short, err := os.MkdirTemp("", "repos-notify-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.RemoveAll(short); err != nil {
					t.Error(err)
				}
			}()
			notifyPath := filepath.Join(short, "notify")
			notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = notify.Close() }()
			lookup := map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "PATH": filepath.Dir(executable), "NOTIFY_SOCKET": notifyPath, "IKIGENBA_SERVICES": services, "DRAIN_SECONDS": "1", "SANDBOX_PRIVATE": "unused"}
			allowed := strings.Fields("DRAIN_SECONDS READ_SLOTS WRITE_SLOTS QUEUE_LENGTH QUEUE_SECONDS OPERATION_SECONDS PUSH_MAX_BYTES REPO_MAX_BYTES MAINTENANCE_HOURS IKIGENBA_SERVICES LISTEN_PID LISTEN_FDS NOTIFY_SOCKET PATH")
			var keysMu sync.Mutex
			var keys []string
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(errors.New("SIGTERM"))
			var stderr bytes.Buffer
			sink := &eventSink{}
			busSink := &busSink{}
			var clockMu sync.Mutex
			current := credentialTime
			now := func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return current }
			sleep := func(_ context.Context, d time.Duration) { clockMu.Lock(); current = current.Add(d); clockMu.Unlock() }
			if sinkMode != "delivered" {
				// Sink errors may themselves carry secrets; Run must still write
				// only the safe event, never the sink's error text.
				sink.failure = errors.New("test sink refuses " + cs[0].header)
				busSink.failure = sink.failure
				if sinkMode == "rejected" {
					sink.failure = errors.Join(telemetry.ErrRejected, sink.failure)
					busSink.failure = errors.Join(events.ErrRejected, busSink.failure)
				}
			}
			finished := make(chan int, 1)
			returned := false
			go func() {
				finished <- cli.Run(ctx, cli.Process{Version: "fixture-display",
					LookupEnv: func(key string) (string, bool) {
						keysMu.Lock()
						keys = append(keys, key)
						keysMu.Unlock()
						value, ok := lookup[key]
						return value, ok
					},
					Environ: func() []string { return append([]string(nil), env...) }, Pid: 42,
					Stdout: io.Discard, Stderr: &stderr, Inherit: func(uintptr) (net.Listener, error) { return ln, nil },
					Now: now, After: credentialAfter, Sleep: sleep,
					Rand: &deterministicBytes{}, Dir: dir, Sink: sink, EventSink: busSink,
					Banner: page.New(web.ServiceName, "fixture-display").Banner,
					MCP: func(w *telemetry.Writer) *mcp.Server {
						return mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: "fixture-display", Telemetry: w})
					},
				})
			}()
			defer func() {
				cancel(errors.New("SIGTERM"))
				if !returned {
					select {
					case <-finished:
					case <-time.After(10 * time.Second):
						t.Error("Run cleanup did not finish")
					}
				}
			}()
			ready := make(chan error, 1)
			go func() {
				data := make([]byte, 64)
				n, _, err := notify.ReadFromUnix(data)
				if err == nil && string(data[:n]) != "READY=1" {
					err = errors.New("unexpected readiness datagram")
				}
				ready <- err
			}()
			select {
			case err := <-ready:
				if err != nil {
					t.Fatal(err)
				}
			case code := <-finished:
				returned = true
				t.Fatalf("Run returned before readiness: %d: %s", code, stderr.String())
			case <-time.After(10 * time.Second):
				t.Fatal("Run never became ready")
			}
			target, err := url.Parse("http://" + ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			proxy := httputil.NewSingleHostReverseProxy(target)
			proxy.ErrorLog = nil
			front := httptest.NewServer(a.wrap(proxy))
			defer front.Close()
			exerciseCredentials(t, front.URL, cs, a)
			cancel(errors.New("SIGTERM"))
			select {
			case code := <-finished:
				returned = true
				if code != cli.ExitSuccess {
					t.Fatalf("Run exit=%d: %s", code, stderr.String())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Run did not stop")
			}
			keysMu.Lock()
			for _, key := range keys {
				if !contains(allowed, key) {
					t.Errorf("Run looked up undeclared variable %s", key)
				}
			}
			keysMu.Unlock()
			events := sink.capture.Events()
			if len(events) == 0 {
				t.Fatal("sink saw no events")
			}
			for _, event := range events {
				a.check("event name", event.Name)
				a.check("event request id", event.RequestID)
				a.check("event user", event.User)
				for _, value := range event.Attrs {
					a.check("event attribute text or decimal", fmt.Sprint(value))
				}
			}
			if sinkMode != "delivered" && stderr.Len() == 0 {
				t.Fatal("failed deliveries did not exercise stderr")
			}
			assertEventTraffic(t, events)
			busEvents := busSink.capture.Events()
			if len(busEvents) == 0 {
				t.Fatal("credential pushes delivered no bus events")
			}
			for _, event := range busEvents {
				a.check("bus event name", event.Name)
				a.check("bus event id", event.ID)
				a.check("bus event request id", event.RequestID)
				a.check("bus event user", event.User)
				a.check("bus event cause", event.Cause)
				for _, value := range event.Attrs {
					a.check("bus event attribute", fmt.Sprint(value))
				}
			}
			if sinkMode == "failed" && !strings.Contains(stderr.String(), "repos: lost event: ") {
				t.Fatal("failed bus deliveries did not exercise lost-event stderr")
			}
			a.check("Run stderr", stderr.String())
			fixtureFiles(t, dir, a)
			a.assert(t)
		})
	}
}

func assertEventTraffic(t *testing.T, events []telemetry.Event) {
	t.Helper()
	names, paths, tools := map[string]int{}, map[string]int{}, map[string]int{}
	for _, event := range events {
		names[event.Name]++
		if event.Name == "request.started" {
			method, _ := event.Attrs["method"].(string)
			path, _ := event.Attrs["path"].(string)
			paths[method+" "+path]++
		}
		if event.Name == "tool.called" {
			tool, _ := event.Attrs["tool"].(string)
			tools[tool]++
		}
	}
	for _, name := range []string{"service.started", "service.stopping", "request.finished", "repo.created", "repo.renamed", "repo.deleted", "repo.fetched", "repo.pushed"} {
		if names[name] == 0 {
			t.Errorf("credential traffic delivered no %s event to the sink", name)
		}
	}
	for _, path := range []string{"GET /", "GET /about", "GET /tools", "POST /mcp"} {
		if paths[path] < 2 {
			t.Errorf("sink did not see both credentials on %s", path)
		}
	}
	for _, tool := range []string{"list", "show", "status", "create", "rename", "delete"} {
		if tools[tool] < 2 {
			t.Errorf("sink did not see both credentials call %s", tool)
		}
	}
	for i := range 2 {
		for _, route := range []string{"GET /notes%d.git/info/refs", "POST /notes%d.git/git-upload-pack", "POST /notes%d.git/git-receive-pack"} {
			if paths[fmt.Sprintf(route, i)] == 0 {
				t.Errorf("sink did not observe git credential route %s", fmt.Sprintf(route, i))
			}
		}
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// R-C1Z1-1DA5 R-VCFB-JSQH: Observe the handler's entire output for pages,
// MCP successes and refusals, all four git routes, and route/identity failures.
func TestHandlerResponsesAndCloneGuidanceIgnoreCredentials(t *testing.T) {
	t.Setenv("IKIGENBA_SERVICES", "")
	cs := credentials(t)
	a := newInspection(cs)
	dir := t.TempDir()
	services := servicesFixture(t, dir)
	executable, env := gitEnvironment(t, t.TempDir())
	g, err := repogit.Find(filepath.Dir(executable), func() []string { return append([]string(nil), env...) })
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "catalog.db"), Migrations: repos.Migrations(), Now: credentialClock})
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), d, store.Config{Root: filepath.Join(dir, "repos"), Git: g, Now: credentialClock, Rand: &deterministicBytes{}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	sink := &eventSink{}
	w := telemetry.New(telemetry.Config{Service: web.ServiceName, Version: "fixture-display", Sink: sink, Stderr: io.Discard, Now: credentialClock, Rand: &deterministicBytes{}})
	w.Ready()
	defer w.Shutdown(context.Background(), "test complete")
	l := limits.New(settings.Defaults(), limits.Clock{Now: credentialClock, After: credentialAfter})
	bus := credentialEmitter(t, w)
	handler := web.Handler(web.Config{Banner: page.New(web.ServiceName, "fixture-display").Banner, MCP: mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: "fixture-display", Telemetry: w}), ServicesPath: services, Store: s, Git: g, Limits: l, Telemetry: w, Events: bus})
	server := httptest.NewServer(a.wrap(handler))
	defer server.Close()
	exerciseCredentials(t, server.URL, cs, a)
	plainClient := toolClient(server.URL, "")
	callTool(t, plainClient, "create", `{"name":"stable"}`, false)
	plain := credentialHTTP(t, server.URL, http.MethodGet, "/", "", true, "", "")
	plainShow := callTool(t, toolClient(server.URL, ""), "show", `{"repo":"stable"}`, false)
	stable, err := s.Find(context.Background(), "u_fixture", "stable")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		with := credentialHTTP(t, server.URL, http.MethodGet, "/", c.header, true, "", "")
		if !bytes.Equal(plain.Body.Bytes(), with.Body.Bytes()) {
			t.Error("landing body depends on Authorization")
		}
		if withShow := callTool(t, toolClient(server.URL, c.header), "show", `{"repo":"stable"}`, false); !bytes.Equal(plainShow, withShow) {
			t.Error("show structuredContent depends on Authorization")
		}
		for _, request := range []struct{ method, path, body, contentType string }{
			{"GET", "/stable.git/info/refs?service=git-upload-pack", "", ""},
			{"GET", "/stable.git/info/refs?service=git-receive-pack", "", ""},
			{"POST", "/stable.git/git-upload-pack", "0000", "application/x-git-upload-pack-request"},
			{"POST", "/stable.git/git-receive-pack", "0000", "application/x-git-receive-pack-request"},
			{"GET", "/stable.git/HEAD", "", ""}, {"HEAD", "/stable.git/", "", ""},
			{"PUT", "/", "neutral body", "text/plain"}, {"POST", "/about", "", ""}, {"POST", "/tools", "", ""},
			{"GET", "/absent.git/info/refs?service=git-upload-pack", "", ""},
			{"GET", "/mcp", "", ""},
		} {
			credentialHTTP(t, server.URL, request.method, request.path, c.header, true, request.body, request.contentType)
		}
		for _, path := range []string{"/", "/about", "/tools", "/mcp", "/stable.git/info/refs?service=git-upload-pack", "/_appkit/theme.css"} {
			response := credentialHTTP(t, server.URL, http.MethodGet, path, c.header, false, "", "")
			if response.Code == http.StatusOK {
				t.Errorf("missing identity unexpectedly succeeded at %s", path)
			}
		}
		callTool(t, toolClient(server.URL, c.header), "create", `{"name":"stable"}`, true)
		release, ok := l.TryHold(stable.ID)
		if !ok {
			t.Fatal("cannot hold repository for busy refusal")
		}
		callTool(t, toolClient(server.URL, c.header), "delete", `{"repo":"stable"}`, true)
		release()
		credentialHTTP(t, server.URL, http.MethodPost, "/mcp", c.header, true, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`, "application/json")
	}
	credentialLimitRefusals(t, cs, a, s, g, w, services)
	l.Drain()
	for _, c := range cs {
		response := credentialHTTP(t, server.URL, http.MethodGet, "/stable.git/info/refs?service=git-upload-pack", c.header, true, "", "")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatal("draining refusal was not exercised")
		}
	}
	if err := os.Remove(filepath.Join(s.Dir(stable.ID), "HEAD")); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		response := credentialHTTP(t, server.URL, http.MethodGet, "/stable.git/info/refs?service=git-upload-pack", c.header, true, "", "")
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), smarthttp.Unavailable) {
			t.Fatal("unavailable refusal was not exercised")
		}
	}
	d.SetFailing(true)
	for _, c := range cs {
		client := toolClient(server.URL, c.header)
		for _, tool := range []string{"list", "status", "show", "create", "rename", "delete"} {
			args := `{}`
			switch tool {
			case "show", "delete":
				args = `{"repo":"stable"}`
			case "create":
				args = `{"name":"another"}`
			case "rename":
				args = `{"repo":"stable","name":"another"}`
			}
			callTool(t, client, tool, args, true)
		}
		response := credentialHTTP(t, server.URL, http.MethodGet, "/stable.git/info/refs?service=git-upload-pack", c.header, true, "", "")
		if response.Code != http.StatusInternalServerError {
			t.Fatal("closed-store refusal was not exercised")
		}
	}
	a.assert(t)
}

func credentialLimitRefusals(t *testing.T, cs []credential, a *inspection, s *store.Store, g *repogit.Git, w *telemetry.Writer, services string) {
	t.Helper()
	for _, mode := range []string{"queue_full", "queue_timeout", "repository_size", "push_size"} {
		cfg := settings.Defaults()
		cfg.ReadSlots, cfg.QueueLength = 1, 1
		if mode == "repository_size" {
			cfg.RepoMaxBytes = 1
		}
		if mode == "push_size" {
			cfg.PushMaxBytes = 1
		}
		armed := make(chan struct{}, 1)
		after := func(d time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			if d == time.Duration(cfg.QueueSeconds)*time.Second {
				if mode == "queue_timeout" {
					ch <- credentialTime
				} else {
					armed <- struct{}{}
				}
			}
			return ch
		}
		l := limits.New(cfg, limits.Clock{Now: credentialClock, After: after})
		bus := credentialEmitter(t, w)
		server := httptest.NewServer(a.wrap(web.Handler(web.Config{
			Banner:       page.New(web.ServiceName, "fixture-display").Banner,
			MCP:          mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: "fixture-display", Telemetry: w}),
			ServicesPath: services, Store: s, Git: g, Limits: l, Telemetry: w, Events: bus,
		})))
		func() {
			defer server.Close()
			if mode == "push_size" {
				credentialPushRefusals(t, server.URL, cs, a)
				return
			}
			path, wantStatus := "/stable.git/info/refs?service=git-upload-pack", http.StatusServiceUnavailable
			if mode == "repository_size" {
				path, wantStatus = "/stable.git/info/refs?service=git-receive-pack", http.StatusInsufficientStorage
			} else {
				active, err := l.Acquire(context.Background(), "occupied", limits.Fetch, false)
				if err != nil {
					t.Fatal(err)
				}
				defer active.Release()
				if mode == "queue_full" {
					ctx, cancel := context.WithCancel(context.Background())
					finished := make(chan error, 1)
					go func() {
						grant, err := l.Acquire(ctx, "queued", limits.Fetch, false)
						if grant != nil {
							grant.Release()
						}
						finished <- err
					}()
					defer func() {
						cancel()
						select {
						case <-finished:
						case <-time.After(5 * time.Second):
							t.Error("queue fixture did not stop")
						}
					}()
					select {
					case <-armed:
					case <-time.After(5 * time.Second):
						t.Fatal("queue fixture never armed")
					}
				}
			}
			for _, c := range cs {
				response := credentialHTTP(t, server.URL, http.MethodGet, path, c.header, true, "", "")
				if response.Code != wantStatus {
					t.Fatalf("%s refusal was not exercised: status %d", mode, response.Code)
				}
			}
		}()
		l.Drain()
	}
}

func credentialPushRefusals(t *testing.T, endpoint string, cs []credential, a *inspection) {
	t.Helper()
	root := t.TempDir()
	executable, env := gitEnvironment(t, root)
	for i, c := range cs {
		dir := filepath.Join(root, "push"+strconv.Itoa(i))
		config := []string{"-c", "credential.helper=", "-c", "http.extraHeader=X-User-Id: u_fixture", "-c", "http.extraHeader=Authorization: " + c.header}
		runGit(t, executable, env, root, append(config, "clone", endpoint+"/stable.git", dir)...)
		if err := os.WriteFile(filepath.Join(dir, "fixture.txt"), []byte("neutral fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, executable, env, dir, "add", "fixture.txt")
		runGit(t, executable, env, dir, "commit", "-m", "size refusal fixture")
		fixtureFiles(t, root, a)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, executable, append(config, "push", "origin", "HEAD:refs/heads/main")...)
		cmd.Dir, cmd.Env = dir, env
		output, err := cmd.CombinedOutput()
		cancel()
		if err == nil || !strings.Contains(string(output), "maximum allowed size") {
			t.Fatalf("push size refusal was not exercised: %v\n%s", err, output)
		}
	}
}

type busSink struct {
	capture events.Capture
	failure error
}

func (s *busSink) Deliver(ctx context.Context, event events.Event) error {
	if err := s.capture.Deliver(ctx, event); err != nil {
		return err
	}
	return s.failure
}
func credentialEmitter(t *testing.T, w *telemetry.Writer) *events.Emitter {
	t.Helper()
	bus := events.New(events.Config{Service: web.ServiceName, Sink: &events.Capture{}, Stderr: io.Discard, Now: credentialClock, Rand: &deterministicBytes{}, Telemetry: w, Emits: smarthttp.Emits()})
	t.Cleanup(func() { bus.Shutdown(context.Background()) })
	return bus
}
