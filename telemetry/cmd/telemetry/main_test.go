package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"appkit"
	appkitdb "appkit/db"
	"appkit/manifest"
	appkitserver "appkit/server"
	appkittelemetry "appkit/telemetry"
	"registry"
	"telemetry/internal/db"
	"telemetry/internal/ingest"
	"telemetry/internal/record"
)

const helperProcessEnv = "TELEMETRY_TEST_HELPER_PROCESS"

func TestTelemetryProcess(t *testing.T) {
	if os.Getenv(helperProcessEnv) != "1" {
		return
	}
	args := strings.Split(os.Getenv("TELEMETRY_TEST_ARGS"), " ")
	os.Args = append([]string{"telemetry"}, args...)
	appMain()
}

func appMain() {
	main()
}

func TestManifestMatchesCommittedFile(t *testing.T) {
	got := []byte(appkit.Manifest(telemetrySpec()))
	want, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest: %v", err)
	}

	// R-V6NF-9LY8
	if !bytes.Equal(got, want) {
		t.Fatalf("manifest bytes differ\ngot:\n%s\nwant:\n%s", got, want)
	}
	portLine := "PORT=" + strconv.Itoa(registry.MustPort("telemetry")) + "\n"
	for _, required := range []string{"MCP=true\n", "TELEMETRY_RETENTION_DAYS=90\n", portLine} {
		if !bytes.Contains(got, []byte(required)) {
			t.Errorf("manifest missing %q", required)
		}
	}
	for _, forbidden := range []string{"FEED=", "CONSUMES="} {
		if bytes.Contains(got, []byte(forbidden)) {
			t.Errorf("manifest unexpectedly contains %q", forbidden)
		}
	}
}

func TestCommittedManifestUsesPortableComposedDataPaths(t *testing.T) {
	committed := readCommittedManifest(t)

	// R-8DF1-W89F
	for _, forbidden := range []string{"/opt/", "TELEMETRY_DB_PATH=", "TELEMETRY_GENERATION_PATH="} {
		if bytes.Contains(committed, []byte(forbidden)) {
			t.Errorf("committed manifest contains non-portable value %q", forbidden)
		}
	}
}

func TestCompiledSpecManifestMatchesCommittedBytes(t *testing.T) {
	spec := telemetrySpec()
	extras := make([]manifest.KV, len(spec.ManifestExtras))
	for i, extra := range spec.ManifestExtras {
		extras[i] = manifest.KV{Key: extra.Key, Value: extra.Value}
	}
	got := []byte(manifest.Emit(manifest.Fields{
		App: spec.App, Mount: spec.Mount, Default: spec.Default, Port: spec.Port,
		MCP: spec.MCP, Feed: spec.Feed, Consumes: spec.Consumes, Extras: extras,
	}))
	want := readCommittedManifest(t)

	// R-8IAN-FB87
	if !bytes.Equal(got, want) {
		t.Fatalf("compiled Spec manifest differs from committed bytes\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestServeBootsFromInstallLayout(t *testing.T) {
	workspaceRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	versionBytes, err := os.ReadFile(filepath.Join(workspaceRoot, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	version := strings.TrimSpace(string(versionBytes))
	if version == "" {
		t.Fatal("VERSION is empty")
	}

	temporaryRoot := t.TempDir()
	installRoot := filepath.Join(temporaryRoot, "telemetry")
	stateDir := filepath.Join(installRoot, "state")
	cacheDir := filepath.Join(installRoot, "cache")
	libexecDir := filepath.Join(installRoot, "libexec")
	binDir := filepath.Join(installRoot, "bin")
	versionEtcDir := filepath.Join(installRoot, "etc", version)
	for _, dir := range []string{stateDir, cacheDir, libexecDir, binDir, versionEtcDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create install directory %s: %v", dir, err)
		}
	}

	binaryPath := filepath.Join(libexecDir, "telemetry-"+version)
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = filepath.Join(workspaceRoot, "cmd", "telemetry")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build staged telemetry binary: %v\n%s", err, output)
	}
	runPath := filepath.Join(binDir, "run")
	if err := os.Symlink(filepath.Join("..", "libexec", filepath.Base(binaryPath)), runPath); err != nil {
		t.Fatalf("link bin/run: %v", err)
	}

	committed := readCommittedManifest(t)
	stagedManifest := filepath.Join(versionEtcDir, "manifest.env")
	if err := os.WriteFile(stagedManifest, committed, 0o644); err != nil {
		t.Fatalf("stage manifest: %v", err)
	}
	currentPath := filepath.Join(installRoot, "etc", "current")
	if err := os.Symlink(version, currentPath); err != nil {
		t.Fatalf("link etc/current: %v", err)
	}

	resolvedRun, err := filepath.EvalSymlinks(runPath)
	if err != nil {
		t.Fatalf("resolve bin/run: %v", err)
	}
	resolvedCurrent, err := filepath.EvalSymlinks(currentPath)
	if err != nil {
		t.Fatalf("resolve etc/current: %v", err)
	}
	selectedManifest, err := os.ReadFile(filepath.Join(currentPath, "manifest.env"))
	if err != nil {
		t.Fatalf("read manifest through etc/current: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate ephemeral port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release ephemeral port: %v", err)
	}
	cmd := exec.Command(runPath, "serve")
	cmd.Env = installSmokeEnv(temporaryRoot, port)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start staged telemetry binary: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second}
	healthURL := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	var response *http.Response
	for response == nil {
		response, err = client.Get(healthURL)
		if err == nil {
			break
		}
		select {
		case processErr := <-done:
			t.Fatalf("staged telemetry exited before serving: %v\n%s", processErr, stderr.String())
		case <-ctx.Done():
			t.Fatalf("staged telemetry did not serve: %v\n%s", ctx.Err(), stderr.String())
		default:
		}
		response = nil
	}
	defer response.Body.Close()
	var health struct {
		Service string `json:"service"`
		Status  string `json:"status"`
		Details struct {
			DroppedTotal int64 `json:"dropped_total"`
		} `json:"details"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatalf("decode health response: %v", err)
	}

	// R-4LKF-FB23
	if resolvedRun != binaryPath {
		t.Errorf("bin/run resolves to %q, want %q", resolvedRun, binaryPath)
	}
	if resolvedCurrent != versionEtcDir {
		t.Errorf("etc/current resolves to %q, want %q", resolvedCurrent, versionEtcDir)
	}
	if !bytes.Equal(selectedManifest, committed) {
		t.Errorf("manifest selected through etc/current differs from committed bytes")
	}
	if response.StatusCode != http.StatusOK || health.Service != "telemetry" || health.Status != "ok" || health.Details.DroppedTotal != 0 {
		t.Errorf("health status=%d body=%+v, want 200 telemetry/ok with dropped_total 0", response.StatusCode, health)
	}
	for _, path := range []string{
		filepath.Join(stateDir, "telemetry.db"),
		filepath.Join(cacheDir, "telemetry.db.generation"),
	} {
		if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
			t.Errorf("composed data file %s was not created as a regular file: %v", path, statErr)
		}
	}
}

func readCommittedManifest(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest: %v", err)
	}
	return body
}

func installSmokeEnv(root string, port int) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "TELEMETRY_") || strings.HasPrefix(entry, "IKIGENBA_ROOT=") || strings.HasPrefix(entry, "IKIGENBA_DOMAIN=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		"IKIGENBA_ROOT="+root,
		"TELEMETRY_IP=127.0.0.1",
		"TELEMETRY_PORT="+strconv.Itoa(port),
		"TELEMETRY_ENABLED=false",
	)
}

func TestSpecUsesRegistryPortWithoutGoLiteralAndServeHonorsOverride(t *testing.T) {
	registryPort := registry.MustPort("telemetry")

	// R-V7VB-NDOX
	if got := telemetrySpec().Port; got != registryPort {
		t.Fatalf("Spec.Port = %d, want registry port %d", got, registryPort)
	}
	assertPortAbsentFromGoSources(t, registryPort)
	assertServeHonorsPortOverride(t)
}

func TestMCPRouteAllowsOnlyPOSTOverComposedServer(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	if !bytes.Contains(mainSource, []byte(`rt.Handle("POST /mcp"`)) || bytes.Contains(mainSource, []byte(`rt.Handle("/mcp"`)) {
		t.Fatal("composition root must register the MCP handler with the method-qualified POST /mcp pattern")
	}

	spec := telemetrySpec()
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open real sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpServer, err := appkitserver.New(appkitserver.Options{
		Addr: listener.Addr().String(), Logger: logger,
		ResourceID: "https://example.test/srv/telemetry/", AuthServer: "https://example.test/",
		Version: "test", Service: spec.App, Health: spec.Health, DB: database,
		Register: spec.Handlers, RecordExclude: spec.TelemetryExclude,
	})
	if err != nil {
		t.Fatalf("compose server: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- httpServer.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		<-serveDone
	})

	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := "http://" + listener.Addr().String()
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		request, err := http.NewRequest(method, baseURL+"/mcp", nil)
		if err != nil {
			t.Fatalf("create %s request: %v", method, err)
		}
		request.Header.Set("X-Owner-Id", "route-test-owner")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("%s /mcp: %v", method, err)
		}
		_ = response.Body.Close()

		// R-NTSI-XWI1
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /mcp status = %d, want exactly 405", method, response.StatusCode)
		}
		if response.StatusCode == http.StatusOK {
			t.Errorf("%s /mcp status = 200, want method-restricted routing", method)
		}
		if allow := response.Header.Get("Allow"); allow != http.MethodPost {
			t.Errorf("%s /mcp Allow = %q, want %q", method, allow, http.MethodPost)
		}
	}

	requestBody := strings.NewReader(`{"jsonrpc":"2.0","id":"route-test","method":"tools/list","params":{}}`)
	request, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", requestBody)
	if err != nil {
		t.Fatalf("create POST request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Owner-Id", "route-test-owner")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer response.Body.Close()
	var rpcResponse struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&rpcResponse); err != nil {
		t.Fatalf("decode POST /mcp JSON-RPC response: %v", err)
	}
	// R-NTSI-XWI1
	if response.StatusCode != http.StatusOK || rpcResponse.JSONRPC != "2.0" || rpcResponse.ID != "route-test" || len(rpcResponse.Result) == 0 || len(rpcResponse.Error) != 0 {
		t.Fatalf("POST /mcp status=%d response=%+v, want successful tools/list JSON-RPC response", response.StatusCode, rpcResponse)
	}
}

func TestIngestPathIsExactlyExcludedAndPostsNeverRecordThemselves(t *testing.T) {
	spec := telemetrySpec()
	// R-VOXX-062N
	if !reflect.DeepEqual(spec.TelemetryExclude, []string{ingest.Path}) {
		t.Fatalf("TelemetryExclude = %v, want exactly registered ingest path %q", spec.TelemetryExclude, ingest.Path)
	}

	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open real sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "http://" + listener.Addr().String()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	recorder := appkittelemetry.New(appkittelemetry.Options{
		Service: "telemetry", IngestURL: baseURL + ingest.Path, Enabled: true,
		Capacity: 32, BatchMax: 32, FlushEvery: time.Hour, Logger: logger,
	})
	httpServer, err := appkitserver.New(appkitserver.Options{
		Addr: listener.Addr().String(), Logger: logger,
		ResourceID: "https://example.test/srv/telemetry/", AuthServer: "https://example.test/",
		Version: "test", Service: spec.App, Health: spec.Health, DB: database,
		Register: spec.Handlers, Recorder: recorder, RecordExclude: spec.TelemetryExclude,
	})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- httpServer.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		<-serveDone
	})

	client := &http.Client{Timeout: 5 * time.Second}
	posted := 0
	for batchIndex, batchSize := range []int{1, 2, 1} {
		records := make([]record.Record, batchSize)
		for i := range records {
			posted++
			records[i] = compositionRecord(fmt.Sprintf("01H000000000000000000000%02d", posted))
		}
		body, err := json.Marshal(ingest.Request{Records: records, Dropped: int64(batchIndex + 1)})
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Post(baseURL+ingest.Path, "", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("ingest POST %d status = %d", batchIndex, response.StatusCode)
		}
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelClose()
	if err := recorder.Close(closeCtx); err != nil {
		t.Fatal(err)
	}

	store := db.NewStore(database)
	stored, _, err := store.Search(context.Background(), db.Query{})
	if err != nil {
		t.Fatal(err)
	}
	// R-VOXX-062N
	if len(stored) != posted {
		t.Fatalf("three ingest POSTs stored %d records, want exactly %d", len(stored), posted)
	}
	for _, item := range stored {
		if item.Service == "telemetry" && strings.Contains(item.Op, ingest.Path) {
			t.Fatalf("ingest POST self-recorded as %+v", item)
		}
	}

	health, err := client.Get(baseURL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	var envelope struct {
		Details map[string]any `json:"details"`
	}
	if err := json.NewDecoder(health.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if health.StatusCode != http.StatusOK || envelope.Details["dropped_total"] != float64(6) {
		t.Fatalf("health status=%d details=%v, want dropped_total 6", health.StatusCode, envelope.Details)
	}
}

func compositionRecord(id string) record.Record {
	return record.Record{
		ID: id, Time: "2026-08-03T04:05:06Z", CorrelationID: "chain-a",
		Service: "service-a", Kind: record.KindRequest, Op: "widgets.read",
		Params: json.RawMessage(`{"visible":true}`), Outcome: record.Outcome{Status: "ok"},
	}
}

func assertPortAbsentFromGoSources(t *testing.T, port int) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	digits := []byte(strconv.Itoa(port))
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == "project" {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(body, digits) {
			return fmt.Errorf("registry port literal found in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertServeHonorsPortOverride(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate ephemeral port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release ephemeral port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := telemetryCommand("serve")
	cmd.Env = append(cmd.Env,
		"TELEMETRY_IP=127.0.0.1",
		"TELEMETRY_PORT="+strconv.Itoa(port),
		"TELEMETRY_DB_PATH="+filepath.Join(t.TempDir(), "telemetry.db"),
		"TELEMETRY_ENABLED=false",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start telemetry: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	client := &http.Client{Timeout: time.Second}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	for {
		resp, requestErr := client.Get(url)
		if requestErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /health status = %d", resp.StatusCode)
			}
			return
		}
		select {
		case err := <-done:
			t.Fatalf("telemetry exited before serving: %v\n%s", err, stderr.String())
		case <-ctx.Done():
			t.Fatalf("telemetry did not serve overridden port: %v\n%s", ctx.Err(), stderr.String())
		default:
		}
	}
}

func telemetryCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTelemetryProcess$")
	cmd.Env = append(os.Environ(),
		helperProcessEnv+"=1",
		"TELEMETRY_TEST_ARGS="+strings.Join(args, " "),
	)
	return cmd
}
