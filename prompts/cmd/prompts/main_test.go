package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	appkitdb "appkit/db"
	"appkit/manifest"

	promptdb "prompts/internal/db"

	"registry"
)

func TestResolveStorageRootsUsesRootedStateAndCacheDefaults(t *testing.T) {
	// R-LBH5-4LO0
	// R-LCP1-IDEP
	roots, err := resolveStorageRoots(func(key string) string {
		if key == "IKIGENBA_ROOT" {
			return "/opt"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("resolve storage roots: %v", err)
	}

	if want := "/opt/prompts/state"; roots.stateDir != want {
		t.Errorf("state directory = %q, want %q", roots.stateDir, want)
	}
	if want := "/opt/prompts/state/runs"; roots.runsDir != want {
		t.Errorf("runs directory = %q, want %q", roots.runsDir, want)
	}
	if want := "/opt/prompts/cache"; roots.cacheDir != want {
		t.Errorf("cache directory = %q, want %q", roots.cacheDir, want)
	}
	if roots.cacheDir == roots.stateDir {
		t.Errorf("cache directory %q must differ from state directory", roots.cacheDir)
	}
}

func TestResolveStorageRootsHonorsExplicitPathOverrides(t *testing.T) {
	// R-LDWX-W55E
	testRoot := t.TempDir()
	dbPath := filepath.Join(testRoot, "durable", "custom", "prompts.sqlite")
	generationPath := filepath.Join(testRoot, "reclaimable", "elsewhere", "epoch")
	env := map[string]string{
		"IKIGENBA_ROOT":           "/opt",
		"PROMPTS_DB_PATH":         dbPath,
		"PROMPTS_GENERATION_PATH": generationPath,
	}
	roots, err := resolveStorageRoots(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("resolve storage roots: %v", err)
	}

	if want := filepath.Dir(dbPath); roots.stateDir != want {
		t.Errorf("state directory = %q, want override directory %q", roots.stateDir, want)
	}
	if want := filepath.Dir(generationPath); roots.cacheDir != want {
		t.Errorf("cache directory = %q, want override directory %q", roots.cacheDir, want)
	}
	if want := filepath.Join(filepath.Dir(dbPath), "runs"); roots.runsDir != want {
		t.Errorf("runs directory = %q, want %q", roots.runsDir, want)
	}
}

// R-M51H-QWOL
func TestResolveManifestRootFollowsOverrideRootAndDevelopmentFallbackLadder(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "explicit override wins",
			env: map[string]string{
				"PROMPTS_MANIFEST_ROOT": "/x",
				"IKIGENBA_ROOT":         "/y",
			},
			want: "/x",
		},
		{
			name: "suite root",
			env:  map[string]string{"IKIGENBA_ROOT": "/y"},
			want: "/y",
		},
		{
			name: "development fallback",
			want: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveManifestRoot(func(key string) string { return tt.env[key] }); got != tt.want {
				t.Fatalf("resolveManifestRoot() = %q, want %q", got, tt.want)
			}
		})
	}
}

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
		if bytes.HasPrefix(line, []byte("PROMPTS_DB_PATH=")) || bytes.HasPrefix(line, []byte("PROMPTS_GENERATION_PATH=")) {
			t.Fatalf("committed manifest.env contains runtime path line %q", line)
		}
	}
}

// R-8IAN-FB87
func TestManifestLibraryByteEqualsCommittedFile(t *testing.T) {
	spec := promptsSpec()
	consumes := make([]string, 0, len(spec.Consumers))
	for _, consumer := range spec.Consumers {
		consumes = append(consumes, consumer.Source)
	}
	extras := make([]manifest.KV, 0, len(spec.ManifestExtras))
	for _, extra := range spec.ManifestExtras {
		extras = append(extras, manifest.KV{Key: extra.Key, Value: extra.Value})
	}
	got := manifest.Emit(manifest.Fields{
		App:      spec.App,
		Mount:    spec.Mount,
		Default:  spec.Default,
		Port:     spec.Port,
		MCP:      spec.MCP,
		Feed:     spec.Feed,
		Consumes: consumes,
		Extras:   extras,
	})
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}

	if got != string(committed) {
		t.Fatalf("manifest.Emit output != committed etc/manifest.env\n--- emit ---\n%s\n--- committed ---\n%s", got, committed)
	}
}

func TestReposConsumerManifestAndFeedResolutionContract(t *testing.T) {
	// R-SCSS-M166
	spec := promptsSpec()
	var foundRepos bool
	for _, consumer := range spec.Consumers {
		if consumer.Source != "repos" {
			continue
		}
		foundRepos = true
		if len(consumer.Subscriptions) != 1 || consumer.Subscriptions[0].Source != "repos" || consumer.Subscriptions[0].Filter != "**" {
			t.Fatalf("repos consumer subscriptions = %+v, want one repos:** subscription", consumer.Subscriptions)
		}
	}
	if !foundRepos {
		t.Fatal("promptsSpec has no repos consumer")
	}

	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}
	var consumes []string
	for _, line := range strings.Split(string(committed), "\n") {
		if value, ok := strings.CutPrefix(line, "CONSUMES="); ok {
			consumes = strings.Split(value, ",")
			break
		}
	}
	if !containsString(consumes, "repos") {
		t.Fatalf("manifest CONSUMES = %v, want repos", consumes)
	}

	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if regexp.MustCompile(`repos[^\n]*:[0-9]{2,5}`).Match(mainSource) {
		t.Fatal("main.go contains a repos port literal; feed resolution must stay registry-derived")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// R-M69E-4OFA
func TestTuningDefaultsMatchCommittedManifest(t *testing.T) {
	knobs, err := resolveTuningKnobs(func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolve tuning defaults: %v", err)
	}
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(committed)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}

	for key, want := range map[string]int{
		"PROMPTS_MAX_INFLIGHT_CALLS":  knobs.maxInflightCalls,
		"PROMPTS_MAX_CONCURRENT_RUNS": knobs.maxConcurrentRuns,
	} {
		got, err := strconv.Atoi(values[key])
		if err != nil || got != want {
			t.Errorf("manifest %s = %q, resolved default is %d", key, values[key], want)
		}
	}
	gotTTL, err := time.ParseDuration(values["PROMPTS_RUN_TTL"])
	if err != nil || gotTTL != knobs.runTTL {
		t.Errorf("manifest PROMPTS_RUN_TTL = %q, resolved default is %s", values["PROMPTS_RUN_TTL"], knobs.runTTL)
	}
}

// R-VKB6-SHHV
func TestProductionGoSourceHasNoBoxPathLiteral(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != root && (info.Name() == ".git" || info.Name() == "project") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(source, []byte(`"/opt`)) {
			t.Errorf("%s contains a box-path string literal", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production Go source: %v", err)
	}
}

// R-O1AD-MRKW
func TestAgentsTestsSectionDeclaresTestingFacts(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	testsSection, found := strings.CutPrefix(string(doc), "# prompts")
	if !found {
		t.Fatal("AGENTS.md is missing its prompts heading")
	}
	_, testsSection, found = strings.Cut(testsSection, "## Tests\n")
	if !found {
		t.Fatal("AGENTS.md is missing its Tests section")
	}
	if nextSection := strings.Index(testsSection, "\n## "); nextSection >= 0 {
		testsSection = testsSection[:nextSection]
	}

	declarations := []struct {
		name string
		text string
	}{
		{"default gate", "The default gate is `go test ./...`, run from `prompts/`."},
		{"hermetic and composed layers", "The testing layers present are **hermetic** and **composed**."},
		{"composed boot smokes", "The composed\n  tests are the boot smokes in `cmd/prompts/main_test.go`; every other test is\n  hermetic."},
		{"no live layer", "There is no **live** layer"},
		{"environmental preconditions", "The `git` binary is the one environmental precondition beyond the Go toolchain."},
		{"development GOWORK mode", "Development uses the workspace"},
		{"production GOWORK mode", "the production build uses `GOWORK=off`"},
	}
	for _, declaration := range declarations {
		if !strings.Contains(testsSection, declaration.text) {
			t.Errorf("AGENTS.md Tests section is missing the %s declaration %q", declaration.name, declaration.text)
		}
	}
}

// R-O2IA-0JBL
func TestNonLiveTestsDoNotSkip(t *testing.T) {
	root := filepath.Join("..", "..")
	skipCall := "t." + "Skip"
	needles := []string{skipCall + "(", skipCall + "f(", skipCall + "Now("}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasLiveBuildConstraint(source) {
			return nil
		}
		for lineNumber, line := range strings.Split(string(source), "\n") {
			for _, needle := range needles {
				if strings.Contains(line, needle) {
					rel, relErr := filepath.Rel(root, path)
					if relErr != nil {
						return relErr
					}
					t.Errorf("non-live test calls %s at %s:%d", strings.TrimSuffix(needle, "("), rel, lineNumber+1)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test sources for skips: %v", err)
	}
}

func hasLiveBuildConstraint(source []byte) bool {
	for _, line := range strings.Split(string(source), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false
		}
		expression, found := strings.CutPrefix(line, "//go:build ")
		if found && regexp.MustCompile(`\blive\b`).MatchString(expression) {
			return true
		}
	}
	return false
}

// R-4LKF-FB23
func TestPromptsBootsWithDurableRunStorage(t *testing.T) {
	// R-ZNR1-KI5L
	root := t.TempDir()
	appRoot := filepath.Join(root, "prompts")
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

	binary := filepath.Join(libexecDir, "prompts-"+version)
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build prompts: %v\n%s", err, out)
	}

	run := filepath.Join(binDir, "run")
	if err := os.Symlink("../libexec/prompts-"+version, run); err != nil {
		t.Fatalf("symlink bin/run: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(run); err != nil || resolved != binary {
		t.Fatalf("bin/run resolves to %q err=%v, want %q", resolved, err, binary)
	}

	feedServers := make(map[string]*httptest.Server)
	for _, source := range sources {
		if source == "repos" {
			continue
		}
		feedServers[source] = newIdleFeedServer(t)
	}
	reposConnected := make(chan struct{}, 1)
	feedServers["repos"] = newIdleFeedServerAt(t, registry.BaseURL("repos"), reposConnected)
	dropbox := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(dropbox.Close)

	port := freeTCPPort(t)
	dbPath := filepath.Join(stateDir, "prompts.db")
	generationPath := filepath.Join(cacheDir, "prompts.db.generation")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	cmd.Env = testEnv(map[string]string{
		"IKIGENBA_DOMAIN":             "",
		"IKIGENBA_ROOT":               "",
		"PROMPTS_IP":                  "127.0.0.1",
		"PROMPTS_PORT":                fmt.Sprintf("%d", port),
		"PROMPTS_DB_PATH":             dbPath,
		"PROMPTS_GENERATION_PATH":     generationPath,
		"PROMPTS_WWW_PATH":            filepath.Join("..", "..", "share", "www"),
		"PROMPTS_CRON_FEED_URL":       feedServers["cron"].URL + "/feed",
		"PROMPTS_CRM_FEED_URL":        feedServers["crm"].URL + "/feed",
		"PROMPTS_LEDGER_FEED_URL":     feedServers["ledger"].URL + "/feed",
		"PROMPTS_DROPBOX_FEED_URL":    feedServers["dropbox"].URL + "/feed",
		"PROMPTS_SCRIPTS_FEED_URL":    feedServers["scripts"].URL + "/feed",
		"PROMPTS_PROMPTS_FEED_URL":    feedServers["prompts"].URL + "/feed",
		"DROPBOX_BASE_URL":            dropbox.URL,
		"ANTHROPIC_API_KEY":           "",
		"PROMPTS_MANIFEST_ROOT":       root,
		"PROMPTS_OUTBOX_REAPER_EVERY": "0",
	})
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start prompts: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	firstStopped := false
	defer func() {
		if !firstStopped {
			stopProcess(cancel, done)
		}
	}()

	doc := waitForHealth(t, port, done, &stdout, &stderr)
	if got := doc["service"]; got != "prompts" {
		t.Fatalf("health service = %v, want prompts; body=%v", got, doc)
	}
	if got := doc["status"]; got != "ok" {
		t.Fatalf("health status = %v, want ok; body=%v", got, doc)
	}
	select {
	case <-reposConnected:
	case <-time.After(2 * time.Second):
		t.Fatalf("repos consumer did not connect to registry-derived feed %s/feed", registry.BaseURL("repos"))
	}
	// R-SCSS-M166
	if _, ok := doc["details"].(map[string]any); !ok {
		t.Fatalf("health details = %#v, want JSON object", doc["details"])
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("prompts did not create DB under state/: %v", err)
	}
	if _, err := os.Stat(generationPath); err != nil {
		t.Fatalf("prompts did not create generation sidecar under cache/: %v", err)
	}
	if filepath.Dir(generationPath) != cacheDir {
		t.Fatalf("generation sidecar path %s is not under cache dir %s", generationPath, cacheDir)
	}
	runsDir := filepath.Join(stateDir, "runs")
	if info, err := os.Stat(runsDir); err != nil {
		t.Fatalf("prompts did not create runs under state/: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("runs path is not a directory")
	}
	// Stop after the first successful startup, then leave a durable run artifact.
	stopProcess(cancel, done)
	firstStopped = true
	runID := "durable-run"
	outputPath := filepath.Join(runsDir, runID, "output.jsonl")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("mkdir stale run cache: %v", err)
	}
	wantOutput := "{\"message\":\"durable\"}\n"
	if err := os.WriteFile(outputPath, []byte(wantOutput), 0o644); err != nil {
		t.Fatalf("write stale run cache: %v", err)
	}
	sandboxDir := filepath.Join(runsDir, runID, "sandbox")
	gitCommand(t, "init", "-b", "main", sandboxDir)
	if err := os.WriteFile(filepath.Join(sandboxDir, "prompt.md"), []byte("durable definition"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sandboxDir, "config.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, "-C", sandboxDir, "add", ".")
	gitCommand(t, "-C", sandboxDir, "-c", "user.name=test", "-c", "user.email=test@localhost", "commit", "-m", "pinned")
	definitionSHA := gitCommand(t, "-C", sandboxDir, "rev-parse", "HEAD")
	gitCommand(t, "-C", sandboxDir, "checkout", "-b", "ikigenba/run-"+runID)

	conn, err := appkitdb.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(`INSERT INTO runs
		(id, prompt_id, owner_id, owner_email, prompt_name, status, started_at, log_path, definition_sha)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, runID, "deleted-prompt", "client-durable-smoke", "durable@example.com", "Durable", "succeeded", "2026-08-08T00:00:00Z", outputPath, definitionSHA)
	if closeErr := conn.Close(); err != nil || closeErr != nil {
		t.Fatalf("seed durable run: %v, close: %v", err, closeErr)
	}
	before := snapshotTree(t, filepath.Join(runsDir, runID))

	// A second composition-root startup against the same state must preserve it.
	ctx2, cancel2 := context.WithCancel(context.Background())
	var stdout2, stderr2 bytes.Buffer
	overrideConnected := make(chan struct{}, 1)
	overrideRepos := newObservedIdleFeedServer(t, overrideConnected)
	cmd2 := exec.CommandContext(ctx2, run, "serve")
	cmd2.Env = append(append([]string(nil), cmd.Env...), "PROMPTS_REPOS_FEED_URL="+overrideRepos.URL+"/feed")
	cmd2.Stdout = &stdout2
	cmd2.Stderr = &stderr2
	if err := cmd2.Start(); err != nil {
		t.Fatalf("restart prompts: %v", err)
	}
	done2 := make(chan error, 1)
	go func() { done2 <- cmd2.Wait() }()
	defer stopProcess(cancel2, done2)
	waitForHealth(t, port, done2, &stdout2, &stderr2)
	select {
	case <-overrideConnected:
	case <-time.After(2 * time.Second):
		t.Fatalf("repos consumer did not connect to PROMPTS_REPOS_FEED_URL override %s/feed", overrideRepos.URL)
	}
	if got, err := os.ReadFile(outputPath); err != nil || string(got) != wantOutput {
		t.Fatalf("durable run artifact after restart = %q, %v", got, err)
	}
	if after := snapshotTree(t, filepath.Join(runsDir, runID)); !reflect.DeepEqual(after, before) {
		t.Fatalf("run tree changed across restart\nbefore=%v\nafter=%v", before, after)
	}
	if got := gitCommand(t, "-C", sandboxDir, "rev-parse", "HEAD"); got != definitionSHA {
		t.Fatalf("HEAD after restart = %s, want %s", got, definitionSHA)
	}
	if got := runOutputOverMCP(t, port, runID); got != wantOutput {
		t.Fatalf("run_output after restart = %q, want %q", got, wantOutput)
	}
}

func gitCommand(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// R-RVQ7-98SG
func TestPromptsBootAbortsBeforeListenerWhenVersionPlaneSeedingFails(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	cacheDir := filepath.Join(root, "cache")
	for _, dir := range []string{stateDir, cacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	dbPath := filepath.Join(stateDir, "prompts.db")
	seedBootPrompt(t, dbPath)

	binary := filepath.Join(root, "prompts")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build prompts: %v\n%s", err, out)
	}

	reposListener, err := net.Listen("tcp", "127.0.0.1:3007")
	if err != nil {
		t.Fatalf("listen for failing version plane: %v", err)
	}
	reposServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "version plane rejected seed", http.StatusBadRequest)
	})}
	go func() { _ = reposServer.Serve(reposListener) }()
	t.Cleanup(func() { _ = reposServer.Close() })

	port := freeTCPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, "serve")
	cmd.Env = testEnv(map[string]string{
		"IKIGENBA_DOMAIN":             "",
		"IKIGENBA_ROOT":               "",
		"PROMPTS_IP":                  "127.0.0.1",
		"PROMPTS_PORT":                strconv.Itoa(port),
		"PROMPTS_DB_PATH":             dbPath,
		"PROMPTS_GENERATION_PATH":     filepath.Join(cacheDir, "prompts.db.generation"),
		"PROMPTS_WWW_PATH":            filepath.Join("..", "..", "share", "www"),
		"PROMPTS_MANIFEST_ROOT":       root,
		"PROMPTS_OUTBOX_REAPER_EVERY": "0",
	})
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start prompts: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	listenerOpened := false
	var exitErr error
	exited := false
	for !exited {
		select {
		case exitErr = <-done:
			exited = true
		default:
			conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 20*time.Millisecond)
			if dialErr == nil {
				listenerOpened = true
				_ = conn.Close()
			}
			if ctx.Err() != nil {
				exitErr = <-done
				exited = true
			} else {
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	output := stdout.String() + stderr.String()
	if ctx.Err() != nil {
		t.Fatalf("prompts did not exit after version-plane failure: %v\n%s", exitErr, output)
	}
	if exitErr == nil {
		t.Fatalf("prompts exited zero after version-plane failure\n%s", output)
	}
	if !strings.Contains(output, "version plane") {
		t.Fatalf("startup error does not name version plane: %v\n%s", exitErr, output)
	}
	if listenerOpened {
		t.Fatalf("prompts listener on port %d opened despite failed definition seeding", port)
	}
	probe, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("prompts listener address remains unavailable after failed startup: %v", err)
	}
	_ = probe.Close()
}

func seedBootPrompt(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()
	conn, err := appkitdb.Open(path)
	if err != nil {
		t.Fatalf("open boot fixture DB: %v", err)
	}
	migrations, err := appkitdb.LoadMigrations(promptdb.FS, "migrations")
	if err != nil {
		_ = conn.Close()
		t.Fatalf("load boot fixture migrations: %v", err)
	}
	if err := appkitdb.Migrate(ctx, conn, migrations); err != nil {
		_ = conn.Close()
		t.Fatalf("migrate boot fixture DB: %v", err)
	}
	now := "2026-08-08T00:00:00Z"
	_, err = conn.ExecContext(ctx, `INSERT INTO prompts
		(id, owner_id, owner_email, name, user_prompt, system_prompt, config_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"01SEEDBOOT0000000000000001", "boot-owner", "boot@example.com", "Boot seed", "prompt bytes", "", `{}`, now, now,
	)
	closeErr := conn.Close()
	if err != nil {
		t.Fatalf("insert boot fixture prompt: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close boot fixture DB: %v", closeErr)
	}
}

func runOutputOverMCP(t *testing.T, port int, runID string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "run_output",
			"arguments": map[string]any{"run_id": runID},
		},
	})
	if err != nil {
		t.Fatalf("marshal run_output request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/mcp", port), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new run_output request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner-Id", "client-durable-smoke")
	req.Header.Set("X-Owner-Email", "durable@example.com")
	req.Header.Set("X-Client-Id", "client-durable-smoke")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call run_output after restart: %v", err)
	}
	defer resp.Body.Close()
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		t.Fatalf("decode run_output response: %v", err)
	}
	if rpc.Error != nil {
		t.Fatalf("run_output RPC error: %#v", rpc.Error)
	}
	if len(rpc.Result.Content) != 1 {
		t.Fatalf("run_output content = %#v", rpc.Result.Content)
	}
	return rpc.Result.Content[0].Text
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

func newObservedIdleFeedServer(t *testing.T, connected chan<- struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed" {
			http.NotFound(w, r)
			return
		}
		select {
		case connected <- struct{}{}:
		default:
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

func newIdleFeedServerAt(t *testing.T, baseURL string, connected chan<- struct{}) *httptest.Server {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse feed base URL %q: %v", baseURL, err)
	}
	listener, err := net.Listen("tcp", parsed.Host)
	if err != nil {
		t.Fatalf("listen at feed base URL %q: %v", baseURL, err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed" {
			http.NotFound(w, r)
			return
		}
		select {
		case connected <- struct{}{}:
		default:
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	srv.Listener = listener
	srv.Start()
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
			t.Fatalf("prompts exited before health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
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
	t.Fatalf("prompts never served health at %s: %s\nstdout:\n%s\nstderr:\n%s", url, last, stdout.String(), stderr.String())
	return nil
}

func stopProcess(cancel context.CancelFunc, done <-chan error) {
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
