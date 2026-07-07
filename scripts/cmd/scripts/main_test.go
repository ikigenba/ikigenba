package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"testing"
	"time"

	"appkit"
	"appkit/manifest"
	"appkit/server"

	"eventplane/consumer"
	"registry"

	"scripts/internal/consume"
	scriptsdb "scripts/internal/db"
	"scripts/internal/script"
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
		if bytes.HasPrefix(line, []byte("SCRIPTS_DB_PATH=")) || bytes.HasPrefix(line, []byte("SCRIPTS_GENERATION_PATH=")) {
			t.Fatalf("committed manifest.env contains runtime path line %q", line)
		}
	}
}

// R-8IAN-FB87
func TestManifestLibraryByteEqualsCommittedFile(t *testing.T) {
	spec := scriptsSpec()
	got := manifest.Emit(manifest.Fields{
		App:      spec.App,
		Mount:    spec.Mount,
		Default:  spec.Default,
		Port:     spec.Port,
		MCP:      spec.MCP,
		Feed:     spec.Feed,
		Consumes: consumesFromConsumers(spec.Consumers),
		Extras:   manifestExtras(spec.ManifestExtras),
	})
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}

	if got != string(committed) {
		t.Fatalf("manifest.Emit output != committed etc/manifest.env\n--- emit ---\n%s\n--- committed ---\n%s", got, committed)
	}
}

func TestScriptsRuntimeRootUsesGenerationParentCacheDir(t *testing.T) {
	// R-RUNS-CDIR
	root := t.TempDir()
	generationPath := filepath.Join(root, "scripts", "cache", "scripts.db.generation")

	got := scriptsRuntimeRoot(generationPath)
	want := filepath.Join(root, "scripts", "cache")
	if got != want {
		t.Fatalf("scriptsRuntimeRoot(%q) = %q, want %q", generationPath, got, want)
	}
	if got == filepath.Join(root, "scripts") {
		t.Fatalf("scriptsRuntimeRoot returned app root %q; runs must live under cache", got)
	}
}

func TestScriptsSpecUsesRegistryPort(t *testing.T) {
	// R-RGST-SELF
	spec := scriptsSpec()
	if spec.Port != registry.MustPort("scripts") {
		t.Fatalf("scriptsSpec port = %d, want registry scripts port %d", spec.Port, registry.MustPort("scripts"))
	}
	if spec.Port != 3003 {
		t.Fatalf("scriptsSpec port = %d, want behavior-preserving port 3003", spec.Port)
	}

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	main := string(src)
	if !strings.Contains(main, `cfg, err := config.Resolve("scripts", "/srv/scripts/", registry.MustPort("scripts"), os.Getenv)`) {
		t.Fatalf("main.go does not pass registry.MustPort(\"scripts\") to config.Resolve")
	}
	if strings.Contains(main, `Port:  3003`) || strings.Contains(main, `Port: 3003`) {
		t.Fatalf("main.go still contains a bare scripts port literal in scriptsSpec")
	}
}

func TestDropboxFallbackUsesRegistryBaseURL(t *testing.T) {
	// R-RGST-DBOX
	if got := registry.BaseURL("dropbox"); got != "http://127.0.0.1:3200" {
		t.Fatalf("registry.BaseURL(dropbox) = %q, want http://127.0.0.1:3200", got)
	}

	mainSrc, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	main := string(mainSrc)
	if !strings.Contains(main, `dropboxBase := config.EnvOr(os.Getenv, "DROPBOX_BASE_URL", registry.BaseURL("dropbox"))`) {
		t.Fatalf("main.go does not use registry.BaseURL(\"dropbox\") as DROPBOX_BASE_URL fallback")
	}

	dropboxSrc, err := os.ReadFile(filepath.Join("..", "..", "internal", "script", "dropbox.go"))
	if err != nil {
		t.Fatalf("read dropbox.go: %v", err)
	}
	for name, src := range map[string]string{
		"main.go":    main,
		"dropbox.go": string(dropboxSrc),
	} {
		if strings.Contains(src, `"http://127.0.0.1:3200"`) {
			t.Fatalf("%s still contains a quoted dropbox loopback literal", name)
		}
	}
}

func TestNonTestGoFilesDoNotPinLoopbackPorts(t *testing.T) {
	// R-RGST-NLIT
	moduleRoot := filepath.Join("..", "..")
	loopbackPort := regexp.MustCompile(`127\.0\.0\.1:3\d\d\d`)
	standaloneScriptsPort := regexp.MustCompile(`\b3003\b`)

	err := filepath.WalkDir(moduleRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if loopbackPort.Match(src) {
			t.Errorf("%s contains a hard-coded loopback registry port", path)
		}
		if standaloneScriptsPort.Match(src) {
			t.Errorf("%s contains a standalone scripts port token", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk scripts module: %v", err)
	}
}

func TestGoModRequiresRegistrySiblingModule(t *testing.T) {
	// R-RGST-GMOD
	src, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	goMod := string(src)
	if !regexp.MustCompile(`(?m)^\s*registry v0\.0\.0$`).MatchString(goMod) {
		t.Fatalf("go.mod does not require registry v0.0.0:\n%s", goMod)
	}
	if !strings.Contains(goMod, "\nreplace registry => ../registry\n") {
		t.Fatalf("go.mod missing exact registry replace directive")
	}
}

func TestScriptsSpecDeclaresConsumersWithoutLegacyFields(t *testing.T) {
	// R-8WN1-0VQI
	spec := scriptsSpec()
	wantSources := []string{"cron", "crm", "ledger", "dropbox", "prompts"}
	if got := consumesFromConsumers(spec.Consumers); !reflect.DeepEqual(got, wantSources) {
		t.Fatalf("consumer sources = %v, want %v", got, wantSources)
	}
	for _, entry := range spec.Consumers {
		want := consume.Subscriptions([]string{entry.Source})
		if !reflect.DeepEqual(entry.Subscriptions, want) {
			t.Fatalf("%s subscriptions = %#v, want %#v", entry.Source, entry.Subscriptions, want)
		}
		if entry.Handler == nil {
			t.Fatalf("%s Handler is nil", entry.Source)
		}
	}
	if spec.Consumes != nil {
		t.Fatalf("legacy spec.Consumes = %v, want nil", spec.Consumes)
	}
	if spec.Subscriptions != nil {
		t.Fatalf("legacy spec.Subscriptions is set, want nil")
	}
}

func TestScriptsConsumerFactoryFansOutAndSkipsMalformedEvents(t *testing.T) {
	// R-8XUX-ENH7
	ctx := context.Background()
	svc, runner := newConsumerTestService(t)
	oldSvc := svcRef
	svcRef = svc
	t.Cleanup(func() { svcRef = oldSvc })

	sc, err := svc.Create(ctx, "owner@example.com", script.CreateInput{Name: "crm hook", Body: "print(1)"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.SetTrigger(ctx, "owner@example.com", sc.ID, "crm", "contact.*"); err != nil {
		t.Fatalf("SetTrigger: %v", err)
	}

	rt := newConsumerTestRouter(t)
	var factory func(*appkit.Router) consumer.Handler
	for _, entry := range scriptsSpec().Consumers {
		if entry.Source == "crm" {
			factory = entry.Handler
			break
		}
	}
	if factory == nil {
		t.Fatal("scriptsSpec has no crm consumer handler factory")
	}
	handler := factory(rt)

	payload := []byte(`{"contact_id":"c1"}`)
	ev := consumer.Event{Source: "crm", Type: "contact.created", ID: "evt-1", Payload: payload}
	if err := handler(ctx, ev); err != nil {
		t.Fatalf("well-formed event returned %v, want nil", err)
	}
	spawn := runner.awaitSpawn(t)
	if spawn.run.ScriptID != sc.ID {
		t.Fatalf("spawn script id = %q, want %q", spawn.run.ScriptID, sc.ID)
	}
	if spawn.run.TriggerSource != "crm" || spawn.run.TriggerType != "contact.created" || spawn.run.TriggerEventID != "evt-1" {
		t.Fatalf("spawn trigger fields = %+v, want crm/contact.created/evt-1", spawn.run)
	}
	if string(spawn.input) != string(payload) {
		t.Fatalf("spawn input = %s, want %s", spawn.input, payload)
	}

	err = handler(ctx, consumer.Event{Source: "crm", Type: "", ID: "", Payload: []byte(`{}`)})
	if err == nil {
		t.Fatal("malformed event returned nil, want ErrSkip-wrapped error")
	}
	if !errors.Is(err, consumer.ErrSkip) {
		t.Fatalf("malformed event error = %v, want errors.Is ErrSkip", err)
	}
	runner.assertNoSpawn(t)
}

// R-4LKF-FB23
func TestScriptsBootsFromOpsctlLayoutAndServesHealth(t *testing.T) {
	// R-RUNS-BOOT
	root := t.TempDir()
	appRoot := filepath.Join(root, "scripts")
	stateDir := filepath.Join(appRoot, "state")
	cacheDir := filepath.Join(appRoot, "cache")
	runsDir := filepath.Join(cacheDir, "runs")
	appRootRunsDir := filepath.Join(appRoot, "runs")
	libexecDir := filepath.Join(appRoot, "libexec")
	binDir := filepath.Join(appRoot, "bin")
	etcDir := filepath.Join(appRoot, "etc")
	shareDir := filepath.Join(appRoot, "share")
	for _, dir := range []string{stateDir, cacheDir, libexecDir, binDir, etcDir, shareDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if _, err := os.Stat(runsDir); !os.IsNotExist(err) {
		t.Fatalf("cache/runs dir exists before boot (stat err=%v)", err)
	}
	if _, err := os.Stat(appRootRunsDir); !os.IsNotExist(err) {
		t.Fatalf("app-root runs dir exists before boot (stat err=%v)", err)
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

	binary := filepath.Join(libexecDir, "scripts-"+version)
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build scripts: %v\n%s", err, out)
	}

	run := filepath.Join(binDir, "run")
	if err := os.Symlink("../libexec/scripts-"+version, run); err != nil {
		t.Fatalf("symlink bin/run: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(run); err != nil || resolved != binary {
		t.Fatalf("bin/run resolves to %q err=%v, want %q", resolved, err, binary)
	}

	dropbox := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(dropbox.Close)
	port := freeTCPPort(t)
	feedServers := make(map[string]*httptest.Server)
	env := map[string]string{
		"IKIGENBA_DOMAIN":           "int.ikigenba.com",
		"IKIGENBA_ROOT":             root,
		"SCRIPTS_IP":                "127.0.0.1",
		"SCRIPTS_PORT":              fmt.Sprintf("%d", port),
		"DROPBOX_BASE_URL":          dropbox.URL,
		"OUTBOX_RETENTION_DAYS":     "7",
		"OUTBOX_RETENTION_MAX_ROWS": "1000000",
	}
	for _, entry := range scriptsSpec().Consumers {
		source := entry.Source
		feedServers[source] = newIdleFeedServer(t)
		env["SCRIPTS_"+strings.ToUpper(source)+"_FEED_URL"] = feedServers[source].URL + "/feed"
	}

	dbPath := filepath.Join(stateDir, "scripts.db")
	generationPath := filepath.Join(cacheDir, "scripts.db.generation")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	cmd.Env = testEnv(env)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start scripts: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer stopProcess(cancel, done)

	doc := waitForHealth(t, port, done, &stdout, &stderr)
	if got := doc["service"]; got != "scripts" {
		t.Fatalf("health service = %v, want scripts; body=%v", got, doc)
	}
	if got := doc["status"]; got != "ok" {
		t.Fatalf("health status = %v, want ok; body=%v", got, doc)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("scripts did not create DB under state/: %v", err)
	}
	if _, err := os.Stat(generationPath); err != nil {
		t.Fatalf("scripts did not create generation sidecar under cache/: %v", err)
	}
	if filepath.Dir(generationPath) != cacheDir {
		t.Fatalf("generation sidecar path %s is not under cache dir %s", generationPath, cacheDir)
	}
	if entries, err := os.ReadDir(runsDir); err != nil {
		t.Fatalf("scripts did not recreate runs scratch dir: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("runs scratch dir is not empty after boot: %v", entries)
	}
	if _, err := os.Stat(appRootRunsDir); !os.IsNotExist(err) {
		t.Fatalf("app-root runs should not exist; runs are under cache (stat err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "runs")); !os.IsNotExist(err) {
		t.Fatalf("state/runs should not exist; runs are rebuildable outside state (stat err=%v)", err)
	}
}

func manifestExtras(in []appkit.ManifestKV) []manifest.KV {
	out := make([]manifest.KV, 0, len(in))
	for _, kv := range in {
		out = append(out, manifest.KV{Key: kv.Key, Value: kv.Value})
	}
	return out
}

func consumesFromConsumers(in []appkit.Consumer) []string {
	out := make([]string, 0, len(in))
	for _, entry := range in {
		out = append(out, entry.Source)
	}
	return out
}

type consumerTestRunner struct {
	spawns chan consumerTestSpawn
}

type consumerTestSpawn struct {
	run   script.Run
	input []byte
}

func (r *consumerTestRunner) Spawn(run script.Run, input []byte) {
	r.spawns <- consumerTestSpawn{run: run, input: append([]byte(nil), input...)}
}

func (r *consumerTestRunner) Cancel(runID string) bool { return false }

func (r *consumerTestRunner) awaitSpawn(t *testing.T) consumerTestSpawn {
	t.Helper()
	select {
	case spawn := <-r.spawns:
		return spawn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for consumer-triggered run spawn")
		return consumerTestSpawn{}
	}
}

func (r *consumerTestRunner) assertNoSpawn(t *testing.T) {
	t.Helper()
	select {
	case spawn := <-r.spawns:
		t.Fatalf("unexpected spawn for malformed event: %+v", spawn.run)
	case <-time.After(50 * time.Millisecond):
	}
}

func newConsumerTestService(t *testing.T) (*script.Service, *consumerTestRunner) {
	t.Helper()
	ctx := context.Background()
	conn, err := scriptsdb.Open(filepath.Join(t.TempDir(), "scripts.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := scriptsdb.Migrate(ctx, conn); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	runner := &consumerTestRunner{spawns: make(chan consumerTestSpawn, 2)}
	return script.NewService(script.NewStore(conn), t.TempDir(), runner), runner
}

func newConsumerTestRouter(t *testing.T) *appkit.Router {
	t.Helper()
	var rt *appkit.Router
	_, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/scripts/",
		AuthServer: "https://int.ikigenba.com/",
		Version:    "test",
		Service:    "scripts",
		Register: func(r *appkit.Router) error {
			rt = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if rt == nil {
		t.Fatal("server.New did not call Register")
	}
	return rt
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
			t.Fatalf("scripts exited before health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
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
	t.Fatalf("scripts never served health at %s: %s\nstdout:\n%s\nstderr:\n%s", url, last, stdout.String(), stderr.String())
	return nil
}

func stopProcess(cancel context.CancelFunc, done <-chan error) {
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
