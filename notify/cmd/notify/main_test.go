package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"appkit"
	"appkit/manifest"
	"appkit/server"
	"eventplane/consumer"
	"notify/internal/push"
	"registry"
)

// R-8DF1-W89F
func TestCommittedManifestIsPortable(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}
	if bytes.Contains(committed, []byte("/opt/")) {
		t.Fatalf("committed manifest.env contains on-box /opt/ path:\n%s", committed)
	}
	for _, line := range bytes.Split(committed, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("NOTIFY_DB_PATH=")) || bytes.HasPrefix(line, []byte("NOTIFY_GENERATION_PATH=")) {
			t.Fatalf("committed manifest.env contains runtime path line %q", line)
		}
	}
}

// R-8IAN-FB87
func TestManifestLibraryByteEqualsCommittedFile(t *testing.T) {
	got := manifest.Emit(manifest.Fields{
		App:      "notify",
		Mount:    "/srv/notify/",
		Default:  false,
		Port:     registry.MustPort("notify"),
		MCP:      true,
		Consumes: []string{"crm", "prompts"},
	})
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}

	if got != string(committed) {
		t.Fatalf("manifest.Emit output != committed etc/manifest.env\n--- emit ---\n%s\n--- committed ---\n%s", got, committed)
	}
}

// R-4LKF-FB23
func TestNotifyBootsFromOpsctlLayoutAndServesHealth(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "notify")
	stateDir := filepath.Join(appRoot, "state")
	cacheDir := filepath.Join(appRoot, "cache")
	libexecDir := filepath.Join(appRoot, "libexec")
	binDir := filepath.Join(appRoot, "bin")
	etcDir := filepath.Join(appRoot, "etc")
	shareDir := filepath.Join(appRoot, "share")
	for _, dir := range []string{stateDir, cacheDir, libexecDir, binDir, etcDir, shareDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	versionBytes, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	version := strings.TrimSpace(string(versionBytes))
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
		t.Fatalf("VERSION = %q, want v-prefixed SemVer", version)
	}

	committedManifest, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}
	etcVersionDir := filepath.Join(etcDir, version)
	shareVersionDir := filepath.Join(shareDir, version)
	for _, dir := range []string{etcVersionDir, shareVersionDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	shippedManifest := filepath.Join(etcVersionDir, "manifest.env")
	if err := os.WriteFile(shippedManifest, committedManifest, 0o644); err != nil {
		t.Fatalf("write shipped manifest.env: %v", err)
	}
	if err := os.Symlink(version, filepath.Join(etcDir, "current")); err != nil {
		t.Fatalf("symlink etc/current: %v", err)
	}
	if err := os.Symlink(version, filepath.Join(shareDir, "current")); err != nil {
		t.Fatalf("symlink share/current: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Join(etcDir, "current")); err != nil || resolved != etcVersionDir {
		t.Fatalf("etc/current resolves to %q err=%v, want %q", resolved, err, etcVersionDir)
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Join(shareDir, "current")); err != nil || resolved != shareVersionDir {
		t.Fatalf("share/current resolves to %q err=%v, want %q", resolved, err, shareVersionDir)
	}
	selectedManifest, err := os.ReadFile(filepath.Join(etcDir, "current", "manifest.env"))
	if err != nil {
		t.Fatalf("read selected manifest.env: %v", err)
	}
	if !bytes.Equal(selectedManifest, committedManifest) {
		t.Fatalf("selected manifest.env differs from committed authored file\n--- selected ---\n%s\n--- committed ---\n%s", selectedManifest, committedManifest)
	}

	binary := filepath.Join(libexecDir, "notify-"+version)
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build notify: %v\n%s", err, out)
	}

	run := filepath.Join(binDir, "run")
	if err := os.Symlink("../libexec/notify-"+version, run); err != nil {
		t.Fatalf("symlink bin/run: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(run); err != nil || resolved != binary {
		t.Fatalf("bin/run resolves to %q err=%v, want %q", resolved, err, binary)
	}

	crmFeed := newIdleFeedServer(t)
	promptsFeed := newIdleFeedServer(t)
	ntfy := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(ntfy.Close)

	port := freeTCPPort(t)
	dbPath := filepath.Join(stateDir, "notify.db")
	generationPath := filepath.Join(cacheDir, "notify.db.generation")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	crmFeedKey := "NOTIFY_CRM_" + "FEED_URL"
	promptsFeedKey := "NOTIFY_PROMPTS_" + "FEED_URL"
	cmd.Env = testEnv(map[string]string{
		"IKIGENBA_DOMAIN":        "",
		"IKIGENBA_ROOT":          "",
		"NOTIFY_IP":              "127.0.0.1",
		"NOTIFY_PORT":            fmt.Sprintf("%d", port),
		"NOTIFY_DB_PATH":         dbPath,
		"NOTIFY_GENERATION_PATH": generationPath,
		crmFeedKey:               crmFeed.URL + "/feed",
		promptsFeedKey:           promptsFeed.URL + "/feed",
		"NOTIFY_NTFY_BASE_URL":   ntfy.URL,
		"NTFY_TOPIC":             "notify-test",
		"NTFY_API_KEY":           "notify-test-token",
	})
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start notify: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer stopProcess(cancel, done)

	doc := waitForHealth(t, port, done, &stdout, &stderr)
	if got := doc["service"]; got != "notify" {
		t.Fatalf("health service = %v, want notify; body=%v", got, doc)
	}
	if got := doc["status"]; got != "ok" {
		t.Fatalf("health status = %v, want ok; body=%v", got, doc)
	}
	if _, ok := doc["details"].(map[string]any); !ok {
		t.Fatalf("health details = %#v, want JSON object", doc["details"])
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("notify did not create DB under state/: %v", err)
	}
	if _, err := os.Stat(generationPath); err != nil {
		t.Fatalf("notify did not create generation sidecar under cache/: %v", err)
	}
	if filepath.Dir(generationPath) != cacheDir {
		t.Fatalf("generation sidecar path %s is not under cache dir %s", generationPath, cacheDir)
	}
}

func TestNotifySpecDeclaresConsumersInOrder(t *testing.T) {
	// R-4DG9-3Q97
	spec := notifySpec()
	if len(spec.Consumers) != 2 {
		t.Fatalf("len(spec.Consumers) = %d, want 2", len(spec.Consumers))
	}

	crm := spec.Consumers[0]
	if crm.Source != "crm" {
		t.Fatalf("spec.Consumers[0].Source = %q, want crm", crm.Source)
	}
	if want := []consumer.Subscription{push.Subscription()}; !reflect.DeepEqual(crm.Subscriptions, want) {
		t.Fatalf("crm subscriptions = %#v, want %#v", crm.Subscriptions, want)
	}

	prompts := spec.Consumers[1]
	if prompts.Source != "prompts" {
		t.Fatalf("spec.Consumers[1].Source = %q, want prompts", prompts.Source)
	}
	if want := push.PromptsSubscriptions(); !reflect.DeepEqual(prompts.Subscriptions, want) {
		t.Fatalf("prompts subscriptions = %#v, want %#v", prompts.Subscriptions, want)
	}
}

func TestNotifyConsumerHandlersPushSubscribedEventsOnly(t *testing.T) {
	// R-4EO5-HHZW
	spec := notifySpec()
	rt := newTestRouter(t)

	for _, tc := range []struct {
		source       string
		event        consumer.Event
		unsubscribed consumer.Event
		wantTitle    string
		wantBody     string
	}{
		{
			source: "crm",
			event: consumer.Event{
				Type:    "contact.created",
				ID:      "01JCONTACT",
				Source:  "crm",
				Payload: json.RawMessage(`{"display_name":"Ada Lovelace"}`),
			},
			unsubscribed: consumer.Event{
				Type:    "contact.updated",
				ID:      "01JCONTACTUP",
				Source:  "crm",
				Payload: json.RawMessage(`{"display_name":"Ada Lovelace"}`),
			},
			wantTitle: "New contact",
			wantBody:  "Ada Lovelace",
		},
		{
			source: "prompts",
			event: consumer.Event{
				Type:    "run.succeeded",
				ID:      "01JRUNOK",
				Source:  "prompts",
				Payload: json.RawMessage(`{"session_id":"s1","session_name":"nightly scan","trigger_event":"cron.nightly","scheduled_for":"2026-06-06T08:00:00Z"}`),
			},
			unsubscribed: consumer.Event{
				Type:    "run.cancelled",
				ID:      "01JRUNCANCEL",
				Source:  "prompts",
				Payload: json.RawMessage(`{"session_name":"nightly scan"}`),
			},
			wantTitle: "Run succeeded",
			wantBody:  "nightly scan",
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			ntfy := newNtfyRecorder(t)
			t.Setenv("NOTIFY_NTFY_BASE_URL", ntfy.srv.URL)
			t.Setenv("NTFY_TOPIC", "topic")
			t.Setenv("NTFY_API_KEY", "tok")

			entry, ok := findConsumer(spec.Consumers, tc.source)
			if !ok {
				t.Fatalf("consumer %q not found", tc.source)
			}
			h := entry.Handler(rt)
			if err := h(context.Background(), tc.event); err != nil {
				t.Fatalf("%s subscribed event returned %v, want nil", tc.source, err)
			}
			post := ntfy.waitForPost(t)
			if post.method != http.MethodPost {
				t.Fatalf("method = %q, want POST", post.method)
			}
			if post.path != "/topic" {
				t.Fatalf("path = %q, want /topic", post.path)
			}
			if post.auth != "Bearer tok" {
				t.Fatalf("Authorization = %q, want bearer token", post.auth)
			}
			if post.title != tc.wantTitle {
				t.Fatalf("Title = %q, want %q", post.title, tc.wantTitle)
			}
			if post.body != tc.wantBody {
				t.Fatalf("body = %q, want %q", post.body, tc.wantBody)
			}

			if err := h(context.Background(), tc.unsubscribed); err != nil {
				t.Fatalf("%s unsubscribed event returned %v, want nil", tc.source, err)
			}
			ntfy.assertNoPost(t)
		})
	}
}

type capturedNtfyPost struct {
	method string
	path   string
	title  string
	auth   string
	body   string
}

type ntfyRecorder struct {
	mu     sync.Mutex
	posts  []capturedNtfyPost
	posted chan struct{}
	srv    *httptest.Server
}

func newNtfyRecorder(t *testing.T) *ntfyRecorder {
	t.Helper()
	n := &ntfyRecorder{posted: make(chan struct{}, 10)}
	n.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n.mu.Lock()
		n.posts = append(n.posts, capturedNtfyPost{
			method: r.Method,
			path:   r.URL.Path,
			title:  r.Header.Get("Title"),
			auth:   r.Header.Get("Authorization"),
			body:   string(body),
		})
		n.mu.Unlock()
		n.posted <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(n.srv.Close)
	return n
}

func (n *ntfyRecorder) waitForPost(t *testing.T) capturedNtfyPost {
	t.Helper()
	select {
	case <-n.posted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ntfy POST")
	}
	posts := n.snapshot()
	if len(posts) != 1 {
		t.Fatalf("ntfy posts = %d, want exactly 1", len(posts))
	}
	return posts[0]
}

func (n *ntfyRecorder) assertNoPost(t *testing.T) {
	t.Helper()
	select {
	case <-n.posted:
		t.Fatalf("ntfy posts = %d, want no additional POSTs", len(n.snapshot()))
	case <-time.After(50 * time.Millisecond):
	}
}

func (n *ntfyRecorder) snapshot() []capturedNtfyPost {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]capturedNtfyPost, len(n.posts))
	copy(out, n.posts)
	return out
}

func findConsumer(consumers []appkit.Consumer, source string) (appkit.Consumer, bool) {
	for _, entry := range consumers {
		if entry.Source == source {
			return entry, true
		}
	}
	return appkit.Consumer{}, false
}

func newTestRouter(t *testing.T) *appkit.Router {
	t.Helper()
	var rt *appkit.Router
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "http://resource.test/srv/notify/",
		AuthServer: "http://dashboard.test/",
		Version:    "v0.0.0",
		Service:    "notify",
		Register: func(r *appkit.Router) error {
			rt = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if rt == nil {
		t.Fatal("server.New did not call Register")
	}
	return rt
}

func TestSpecPortComesFromRegistryNotifyPort(t *testing.T) {
	// R-RGSP-4A1K
	if got, ok := registry.Port("notify"); !ok || got != 3201 {
		t.Fatalf("registry.Port(%q) = %d, %v, want 3201, true", "notify", got, ok)
	}
	if got := registry.MustPort("notify"); got != 3201 {
		t.Fatalf("registry.MustPort(%q) = %d, want 3201", "notify", got)
	}
	if got, want := notifySpec().Port, registry.MustPort("notify"); got != want {
		t.Fatalf("notifySpec().Port = %d, want registry.MustPort(%q) = %d", got, "notify", want)
	}
}

func newIdleFeedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testEnv(overrides map[string]string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := overrides[key]; ok {
			continue
		}
		out = append(out, kv)
	}
	for key, value := range overrides {
		out = append(out, key+"="+value)
	}
	return out
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForHealth(t *testing.T, port int, done <-chan error, stdout, stderr *bytes.Buffer) map[string]any {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("notify exited before health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		default:
		}

		resp, err := client.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if resp.StatusCode == http.StatusOK && readErr == nil && closeErr == nil {
				var doc map[string]any
				if err := json.Unmarshal(body, &doc); err != nil {
					t.Fatalf("decode health JSON: %v\nbody:\n%s", err, body)
				}
				return doc
			}
			last = fmt.Sprintf("status=%d read=%v close=%v body=%s", resp.StatusCode, readErr, closeErr, body)
		} else {
			last = err.Error()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("notify never served health at %s: %s\nstdout:\n%s\nstderr:\n%s", url, last, stdout.String(), stderr.String())
	return nil
}

func stopProcess(cancel context.CancelFunc, done <-chan error) {
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
