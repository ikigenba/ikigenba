package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

	"appkit"
	appdb "appkit/db"
	"appkit/manifest"
	"appkit/server"
	appkitweb "appkit/web"
	"eventplane/correlation"
	"registry"

	"wiki/internal/ask"
	"wiki/internal/compile"
	wikidb "wiki/internal/db"
	"wiki/internal/extract"
	"wiki/internal/llm"
	"wiki/internal/llmtest"
	"wiki/internal/mcp"
	paging "wiki/internal/page"
	"wiki/internal/retrieve"
	"wiki/internal/web"
	"wiki/internal/wiki"
)

func testWWWRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatalf("resolve test www root: %v", err)
	}
	return root
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
		if bytes.HasPrefix(line, []byte("WIKI_DB_PATH=")) || bytes.HasPrefix(line, []byte("WIKI_GENERATION_PATH=")) {
			t.Fatalf("committed manifest.env contains runtime path line %q", line)
		}
	}
}

// R-8IAN-FB87
func TestManifestLibraryByteEqualsCommittedFile(t *testing.T) {
	// R-JFR5-MJW9
	spec := newSpec(wiki.NewConfig)
	got := manifest.Emit(manifest.Fields{
		App:      spec.App,
		Mount:    spec.Mount,
		Default:  spec.Default,
		Port:     spec.Port,
		MCP:      spec.MCP,
		Feed:     spec.Feed,
		Consumes: spec.Consumes,
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

func TestManifestDeclaresAskBodyBudget(t *testing.T) {
	// R-NID8-19Y1
	extras := manifestExtras(newSpec(wiki.NewConfig).ManifestExtras)
	for _, extra := range extras {
		if extra.Key == "ASK_BODY_BUDGET" {
			if extra.Value != strconv.Itoa(wiki.AskBodyBudget) || extra.Value != "98304" {
				t.Fatalf("ASK_BODY_BUDGET = %q, want wiki.AskBodyBudget 98304", extra.Value)
			}
			return
		}
	}
	t.Fatal("manifest extras do not declare ASK_BODY_BUDGET")
}

func TestAskCacheCapacityParsesFailLoudAndIsDeclared(t *testing.T) {
	// R-0IM4-GNBX
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "unset default", want: wiki.AskCacheCapDefault},
		{name: "positive", raw: "37", want: 37},
		{name: "disabled", raw: "0", want: 0},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "non numeric", raw: "many", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := wiki.NewConfig(func(key string) string {
				if key == "ASK_CACHE_CAP" {
					return tt.raw
				}
				return ""
			})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "ASK_CACHE_CAP") {
					t.Fatalf("NewConfig error = %v, want fail-loud ASK_CACHE_CAP error", err)
				}
				return
			}
			if err != nil || cfg.AskCacheCap != tt.want {
				t.Fatalf("NewConfig AskCacheCap = %d, %v; want %d, nil", cfg.AskCacheCap, err, tt.want)
			}
		})
	}
	for _, extra := range manifestExtras(newSpec(wiki.NewConfig).ManifestExtras) {
		if extra.Key == "ASK_CACHE_CAP" {
			if extra.Value != "500" || extra.Value != strconv.Itoa(wiki.AskCacheCapDefault) {
				t.Fatalf("manifest ASK_CACHE_CAP = %q, want 500", extra.Value)
			}
			return
		}
	}
	t.Fatal("manifest extras do not declare ASK_CACHE_CAP")
}

func TestSpecDeclaresServedMCPService(t *testing.T) {
	// R-JDBC-V0EV
	spec := newSpec(wiki.NewConfig)
	if spec.App != "wiki" {
		t.Fatalf("App = %q, want wiki", spec.App)
	}
	if spec.Mount != "/srv/wiki/" {
		t.Fatalf("Mount = %q, want /srv/wiki/", spec.Mount)
	}
	if want := registry.MustPort("wiki"); spec.Port != want {
		t.Fatalf("Port = %d, want registry wiki port %d", spec.Port, want)
	}
	if !spec.MCP {
		t.Fatal("MCP = false, want true")
	}
	if spec.Handlers == nil {
		t.Fatal("Handlers is nil; service would not mount /mcp")
	}
	if spec.Config == nil {
		t.Fatal("Config is nil; service would not read LLM configuration")
	}
	if len(spec.Workers) != 2 {
		t.Fatalf("Workers len = %d, want ingest and embedding catch-up workers", len(spec.Workers))
	}
}

// R-4LKF-FB23
func TestWikiBootsFromOpsctlLayoutAndServesHealth(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "wiki")
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
	if err := os.CopyFS(filepath.Join(shareVersionDir, "www"), os.DirFS(testWWWRoot(t))); err != nil {
		t.Fatalf("copy www fixture: %v", err)
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

	binary := filepath.Join(libexecDir, "wiki-"+version)
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build wiki: %v\n%s", err, out)
	}

	run := filepath.Join(binDir, "run")
	if err := os.Symlink("../libexec/wiki-"+version, run); err != nil {
		t.Fatalf("symlink bin/run: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(run); err != nil || resolved != binary {
		t.Fatalf("bin/run resolves to %q err=%v, want %q", resolved, err, binary)
	}

	port := freeTCPPort(t)
	dbPath := filepath.Join(stateDir, "wiki.db")
	generationPath := filepath.Join(cacheDir, "wiki.db.generation")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	cmd.Env = testEnv(map[string]string{
		"IKIGENBA_DOMAIN": "int.ikigenba.com",
		"IKIGENBA_ROOT":   root,
		"WIKI_IP":         "127.0.0.1",
		"WIKI_PORT":       fmt.Sprintf("%d", port),
	})
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start wiki: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer stopProcess(cancel, done)

	doc := waitForHealth(t, port, done, &stdout, &stderr)
	if got := doc["service"]; got != "wiki" {
		t.Fatalf("health service = %v, want wiki; body=%v", got, doc)
	}
	if got := doc["status"]; got != "ok" {
		t.Fatalf("health status = %v, want ok; body=%v", got, doc)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("wiki did not create DB under state/: %v", err)
	}
	if _, err := os.Stat(generationPath); err != nil {
		t.Fatalf("wiki did not create generation sidecar under cache/: %v", err)
	}
	if filepath.Dir(generationPath) != cacheDir {
		t.Fatalf("generation sidecar path %s is not under cache dir %s", generationPath, cacheDir)
	}
}

func TestBuildSpecWiresTwentyMCPTools(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	spec := newSpec(staticConfig(wiki.Config{
		SearchDefault: 8,
		SearchCap:     32,
	}))
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test-version",
		Service:    "wiki",
		Register:   spec.Handlers,
		DB:         conn,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"list","method":"tools/list"}`))
	req.Header.Set("X-Owner-Id", "owner-id")
	req.Header.Set("X-Owner-Email", "owner@example.com")
	req.Header.Set("X-Client-Id", "client-1")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	names := make(map[string]bool, len(got.Result.Tools))
	for _, tool := range got.Result.Tools {
		names[tool.Name] = true
	}
	want := []string{"ingest", "status", "abort", "rerun", "jobs", "jobs_count", "merge", "merges", "ask", "subjects", "claims", "page", "scopes", "scope_create", "scope_delete", "scope_set_visibility", "instructions", "guide", "health", "reflection"}
	if len(names) != len(want) {
		t.Fatalf("tool names = %#v, want exact %v", names, want)
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("tool names = %#v, missing %s", names, name)
		}
	}
}

func TestBuildSpecStatusReportsStoredChainAndJobFallback(t *testing.T) {
	// R-N729-RY1I
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	svc := wiki.NewService(conn, nil, nil, time.Now)
	chainID := "01KZ6V08B73Q7W1G5GR3C2E5MK"
	storedChainJobID, err := svc.Ingest(correlation.WithContext(ctx, chainID), "default", "owner-id", "owner@example.com", "stored chain", "", nil)
	if err != nil {
		t.Fatalf("Ingest stored-chain job: %v", err)
	}
	fallbackJobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "job fallback", "", nil)
	if err != nil {
		t.Fatalf("Ingest fallback job: %v", err)
	}

	h := buildSpecTestHandler(t, conn, newSpec(staticConfig(wiki.Config{
		SearchDefault: 8,
		SearchCap:     32,
	})))
	statusCorrelation := func(jobID string) string {
		t.Helper()
		text := mcpToolCallText(t, h, fmt.Sprintf(`{
			"jsonrpc":"2.0","id":"status","method":"tools/call",
			"params":{"name":"status","arguments":{"job_id":%q}}
		}`, jobID))
		var result struct {
			CorrelationID string `json:"correlation_id"`
		}
		if err := json.Unmarshal([]byte(text), &result); err != nil {
			t.Fatalf("decode status response for %s: %v; text=%s", jobID, err, text)
		}
		return result.CorrelationID
	}

	if got := statusCorrelation(storedChainJobID); got != chainID {
		t.Fatalf("stored-chain job correlation_id = %q, want %q (not job id %q)", got, chainID, storedChainJobID)
	}
	if got := statusCorrelation(fallbackJobID); got != fallbackJobID {
		t.Fatalf("empty-chain job correlation_id = %q, want job id %q", got, fallbackJobID)
	}
}

func TestCompositionRootMCPMountClearsWriteDeadlineBeforeDelegating(t *testing.T) {
	// R-KKIF-QL1Q
	deadlineWriter := &deadlineRecordingResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	outerWriter := &unwrappingMainResponseWriter{ResponseWriter: deadlineWriter}
	req := httptest.NewRequest(http.MethodPost, "/mcp?trace=kept", strings.NewReader("request body"))
	req.Header.Set("X-Test-Header", "kept")
	var gotRequest *http.Request
	var gotBody string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if len(deadlineWriter.deadlines) != 1 || !deadlineWriter.deadlines[0].IsZero() {
			t.Fatalf("write deadlines before MCP handler = %v, want one cleared deadline", deadlineWriter.deadlines)
		}
		gotRequest = r
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read delegated request body: %v", err)
		}
		gotBody = string(body)
	})

	clearWriteDeadline(inner).ServeHTTP(outerWriter, req)

	if gotRequest != req {
		t.Fatalf("delegated request = %p, want original %p", gotRequest, req)
	}
	if gotRequest.Method != http.MethodPost || gotRequest.URL.RequestURI() != "/mcp?trace=kept" || gotRequest.Header.Get("X-Test-Header") != "kept" || gotBody != "request body" {
		t.Fatalf("delegated request changed: method=%q uri=%q header=%q body=%q", gotRequest.Method, gotRequest.URL.RequestURI(), gotRequest.Header.Get("X-Test-Header"), gotBody)
	}
	if len(deadlineWriter.deadlines) != 1 || !deadlineWriter.deadlines[0].IsZero() {
		t.Fatalf("MCP write deadlines = %v, want exactly time.Time{}", deadlineWriter.deadlines)
	}
}

type deadlineRecordingResponseWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *deadlineRecordingResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

type unwrappingMainResponseWriter struct {
	http.ResponseWriter
}

func (w *unwrappingMainResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func TestBuildSpecPageToolReturnsRenderedFooter(t *testing.T) {
	// R-02WN-BXPK
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	internalSubjectID := "01HZX4Q0SUBJECTULID00000001"
	subjects := wiki.NewSubjectStore(conn)
	pages := wiki.NewPageStore(conn)
	for _, subject := range []wiki.Subject{
		{ID: internalSubjectID, Name: "Acme Robotics", NormName: "acme-robotics", Type: "entity"},
		{ID: "subject-tulsa", Name: "Tulsa Launch", NormName: "tulsa-launch", Type: "event"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save subject %s: %v", subject.ID, err)
		}
	}
	for _, page := range []wiki.Page{
		{
			ID:        "page-acme",
			SubjectID: internalSubjectID,
			Title:     "Acme Robotics",
			Body:      "Acme Robotics coordinated Tulsa Launch.",
		},
		{
			ID:        "page-tulsa",
			SubjectID: "subject-tulsa",
			Title:     "Tulsa Launch",
			Body:      "Tulsa Launch was coordinated by Acme Robotics.",
		},
	} {
		if err := pages.Upsert(ctx, page); err != nil {
			t.Fatalf("Upsert page %s: %v", page.ID, err)
		}
	}

	spec := newSpec(staticConfig(wiki.Config{}))
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test-version",
		Service:    "wiki",
		Register:   spec.Handlers,
		DB:         conn,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"id":"page",
		"method":"tools/call",
		"params":{"name":"page","arguments":{"scope":"default","subject":"entity/acme-robotics"}}
	}`))
	req.Header.Set("X-Owner-Id", "owner-id")
	req.Header.Set("X-Owner-Email", "owner@example.com")
	req.Header.Set("X-Client-Id", "client-1")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Subject string `json:"subject"`
		Title   string `json:"title"`
		Body    string `json:"body"`
	}
	decodeMCPToolText(t, rec.Body.Bytes(), &body)
	if body.Subject != "entity/acme-robotics" || body.Title != "Acme Robotics" {
		t.Fatalf("page = %#v, want Acme page", body)
	}
	text := mcpToolText(t, rec.Body.Bytes())
	if strings.Contains(text, internalSubjectID) || strings.Contains(text, "subject_id") || strings.Contains(text, `"path"`) {
		t.Fatalf("page text = %s, want public subject field and no internal ids/path field", text)
	}
	for _, want := range []string{
		"## Links",
		"### Mentions",
		"- [Tulsa Launch](event/tulsa-launch)",
		"### Mentioned by",
		"- [Tulsa Launch](event/tulsa-launch)",
	} {
		if !strings.Contains(body.Body, want) {
			t.Fatalf("page body:\n%s\nmissing %q", body.Body, want)
		}
	}
}

func TestBuildSpecReadToolsReturnPublicPathsWithoutSubjectIDs(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	internalSubjectID := "01HZX4Q0SUBJECTULID00000001"

	if err := wiki.NewSubjectStore(conn).Save(ctx, wiki.Subject{
		ID:       internalSubjectID,
		Name:     "Acme Robotics",
		NormName: "acme-robotics",
		Type:     "entity",
	}); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	if err := wiki.NewJobStore(conn).InsertIngest(ctx, wiki.Job{
		ID:      "job-123",
		OwnerID: "owner-id", OwnerEmail: "owner@example.com",
		SourceText: "source",
		Status:     wiki.JobDone,
		ReceivedAt: time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("InsertIngest: %v", err)
	}
	if err := wiki.NewClaimStore(conn).Save(ctx, wiki.Claim{
		ID:        "claim-1",
		SubjectID: internalSubjectID,
		JobID:     "job-123",
		Body:      "Acme Robotics runs a Tulsa lab.",
		Kind:      wiki.ClaimKind,
	}); err != nil {
		t.Fatalf("Save claim: %v", err)
	}
	if err := wiki.NewPageStore(conn).Upsert(ctx, wiki.Page{
		ID:        internalSubjectID,
		SubjectID: internalSubjectID,
		Title:     "Acme Robotics",
		Body:      "Acme Robotics overview.",
	}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}

	spec := newSpec(staticConfig(wiki.Config{}))
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test-version",
		Service:    "wiki",
		Register:   spec.Handlers,
		DB:         conn,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	for _, tc := range []struct {
		name    string
		request string
	}{
		{
			name:    "status",
			request: `{"jsonrpc":"2.0","id":"status","method":"tools/call","params":{"name":"status","arguments":{"job_id":"job-123"}}}`,
		},
		{
			name:    "subjects",
			request: `{"jsonrpc":"2.0","id":"subjects","method":"tools/call","params":{"name":"subjects","arguments":{"scope":"default","type":"entity","name":"acme"}}}`,
		},
		{
			name:    "claims",
			request: `{"jsonrpc":"2.0","id":"claims","method":"tools/call","params":{"name":"claims","arguments":{"scope":"default","subject":"entity/acme-robotics"}}}`,
		},
		{
			name:    "page",
			request: `{"jsonrpc":"2.0","id":"page","method":"tools/call","params":{"name":"page","arguments":{"scope":"default","subject":"entity/acme-robotics"}}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := mcpToolCallText(t, srv.Handler, tc.request)
			if tc.name != "claims" && !strings.Contains(text, "entity/acme-robotics") {
				t.Fatalf("tool text = %s, want public path", text)
			}
			for _, forbidden := range []string{internalSubjectID, "SubjectID", "subject_id", "NormName", "norm_name"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("tool text = %s, leaked %q", text, forbidden)
				}
			}
		})
	}
}

func TestPathReadServicesResolveFoldedAndSurvivorPathsIdentically(t *testing.T) {
	// R-AL5R-PL1P
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	survivor := wiki.Subject{
		ID:   "subject-survivor",
		Name: "Winner Widget",
		Type: "entity",
	}
	if err := wiki.NewSubjectStore(conn).Save(ctx, survivor); err != nil {
		t.Fatalf("Save survivor: %v", err)
	}
	if err := wiki.NewAliasStore(conn).Insert(ctx, wiki.Alias{
		Name:      "Folded Widget",
		SubjectID: survivor.ID,
		OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
		CreatedAt: "2026-06-24T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}
	if err := wiki.NewJobStore(conn).InsertIngest(ctx, wiki.Job{
		ID:      "job-1",
		OwnerID: "owner-id", OwnerEmail: "owner@example.com",
		SourceText: "source",
		Status:     wiki.JobDone,
		ReceivedAt: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("InsertIngest: %v", err)
	}
	if err := wiki.NewClaimStore(conn).Save(ctx, wiki.Claim{
		ID:        "claim-1",
		SubjectID: survivor.ID,
		JobID:     "job-1",
		Body:      "Winner Widget shipped the release.",
		Kind:      wiki.ClaimKind,
	}); err != nil {
		t.Fatalf("Save claim: %v", err)
	}
	if err := wiki.NewPageStore(conn).Upsert(ctx, wiki.Page{
		ID:        "page-survivor",
		SubjectID: survivor.ID,
		Title:     "Winner Widget",
		Body:      "Winner Widget overview.",
	}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}

	resolver := wiki.NewResolver(conn)
	folded, err := resolver.ResolveByPath(ctx, "entity/folded-widget")
	if err != nil {
		t.Fatalf("ResolveByPath folded: %v", err)
	}
	current, err := resolver.ResolveByPath(ctx, "entity/winner-widget")
	if err != nil {
		t.Fatalf("ResolveByPath survivor: %v", err)
	}
	if folded.ID != survivor.ID || current.ID != survivor.ID {
		t.Fatalf("resolved folded=%+v survivor=%+v, want same survivor", folded, current)
	}

	pageService := pathPageService{
		resolver: resolver,
		service:  wiki.NewService(conn, nil, nil, time.Now),
	}
	foldedPage, err := pageService.PageByPath(ctx, "entity/folded-widget")
	if err != nil {
		t.Fatalf("PageByPath folded: %v", err)
	}
	currentPage, err := pageService.PageByPath(ctx, "entity/winner-widget")
	if err != nil {
		t.Fatalf("PageByPath survivor: %v", err)
	}
	if !reflect.DeepEqual(foldedPage, currentPage) {
		t.Fatalf("folded page = %#v, survivor page = %#v; want byte-identical projection", foldedPage, currentPage)
	}
	if foldedPage.Path != "entity/winner-widget" || foldedPage.Title != "Winner Widget" || !strings.Contains(foldedPage.Body, "Winner Widget overview.") {
		t.Fatalf("folded page = %#v, want survivor public page shape", foldedPage)
	}

	claimService := pathClaimService{
		resolver: resolver,
		claims:   wiki.NewClaimStore(conn),
	}
	foldedClaims, foldedNext, err := claimService.ListBySubject(ctx, "entity/folded-widget", paging.Params{})
	if err != nil {
		t.Fatalf("ListBySubject folded: %v", err)
	}
	currentClaims, currentNext, err := claimService.ListBySubject(ctx, "entity/winner-widget", paging.Params{})
	if err != nil {
		t.Fatalf("ListBySubject survivor: %v", err)
	}
	if foldedNext != currentNext || !reflect.DeepEqual(foldedClaims, currentClaims) {
		t.Fatalf("folded claims = %#v/%q, survivor claims = %#v/%q; want same survivor claims", foldedClaims, foldedNext, currentClaims, currentNext)
	}
	if len(foldedClaims) != 1 || foldedClaims[0].ID != "claim-1" || foldedClaims[0].Text != "Winner Widget shipped the release." {
		t.Fatalf("folded claims = %#v, want survivor claim projection", foldedClaims)
	}
}

func TestSubjectHandlerWithRealPathPageServiceResolvesAliasInboundLinks(t *testing.T) {
	// R-PODT-EU1H
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := wiki.NewSubjectStore(conn)
	for _, subject := range []wiki.Subject{
		{ID: "subject-w", Name: "Giorgio Vasari", Type: "entity"},
		{ID: "subject-f", Name: "Florence Workshop", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save subject %s: %v", subject.ID, err)
		}
	}
	if err := wiki.NewAliasStore(conn).Insert(ctx, wiki.Alias{
		Name:      "Vasari",
		SubjectID: "subject-w",
		OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
		CreatedAt: "2026-06-24T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}
	pages := wiki.NewPageStore(conn)
	for _, page := range []wiki.Page{
		{ID: "page-w", SubjectID: "subject-w", Title: "Giorgio Vasari", Body: "Giorgio Vasari documented Renaissance artists."},
		{ID: "page-f", SubjectID: "subject-f", Title: "Florence Workshop", Body: "The workshop cited Vasari in its notes."},
	} {
		if err := pages.Upsert(ctx, page); err != nil {
			t.Fatalf("Upsert page %s: %v", page.ID, err)
		}
	}

	pageService := pathPageService{
		resolver: wiki.NewResolver(conn),
		service:  wiki.NewService(conn, nil, nil, time.Now),
		webBase:  "https://int.ikigenba.com/srv/wiki/subject/",
	}
	site, err := appkitweb.Load(testWWWRoot(t))
	if err != nil {
		t.Fatalf("load test site: %v", err)
	}
	handler := web.NewHandler("wiki", "v-test", "/srv/wiki/", site, web.WithPageFinder(pageService))
	folded := httptest.NewRecorder()
	handler.ServeHTTP(folded, httptest.NewRequest(http.MethodGet, "/private/default/subject/entity/vasari", nil))
	current := httptest.NewRecorder()
	handler.ServeHTTP(current, httptest.NewRequest(http.MethodGet, "/private/default/subject/entity/giorgio-vasari", nil))

	if folded.Code != http.StatusOK {
		t.Fatalf("folded status = %d, want 200; body=%s", folded.Code, folded.Body.String())
	}
	if current.Code != http.StatusOK {
		t.Fatalf("current status = %d, want 200; body=%s", current.Code, current.Body.String())
	}
	if folded.Body.String() != current.Body.String() {
		t.Fatalf("folded body differs from current body:\nfolded=%s\ncurrent=%s", folded.Body.String(), current.Body.String())
	}
	body := folded.Body.String()
	for _, want := range []string{
		"<h1>Giorgio Vasari</h1>",
		"<p>Giorgio Vasari documented Renaissance artists.</p>",
		`<nav aria-label="Mentioned by">`,
		`<a href="https://int.ikigenba.com/srv/wiki/subject/entity/florence-workshop">Florence Workshop</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("folded subject page missing %q: %s", want, body)
		}
	}
}

func TestWebSubjectLinksUseAbsoluteAuthServerBase(t *testing.T) {
	// R-8I6N-DL0I
	for _, tc := range []struct {
		name       string
		authServer string
	}{
		{name: "production", authServer: "https://acct.ikigenba.com"},
		{name: "local", authServer: "http://localhost:8080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			conn := migratedDB(t, ctx)
			defer conn.Close()
			if err := wiki.NewSubjectStore(conn).Save(ctx, wiki.Subject{
				ID:   "subject-tsr",
				Name: "TSR",
				Type: "entity",
			}); err != nil {
				t.Fatalf("Save subject: %v", err)
			}
			site, err := appkitweb.Load(testWWWRoot(t))
			if err != nil {
				t.Fatalf("load test site: %v", err)
			}
			spec := newSpec(staticConfig(wiki.Config{}))
			srv, err := server.New(server.Options{
				Addr:       "127.0.0.1:0",
				Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
				ResourceID: tc.authServer + "/srv/wiki/mcp",
				AuthServer: tc.authServer,
				Version:    "test-version",
				Service:    "wiki",
				Register:   spec.Handlers,
				WWW:        site,
				DB:         conn,
			})
			if err != nil {
				t.Fatalf("server.New: %v", err)
			}

			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/private/default/", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			want := `<base href="/srv/wiki/private/default/">`
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("home page missing scoped base %q: %s", want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `href="subject/entity/tsr"`) || strings.Contains(rec.Body.String(), `//srv/wiki/subject/`) {
				t.Fatalf("home page did not compose its subject tail through the scoped base: %s", rec.Body.String())
			}
		})
	}
}

func TestAskPageWiresRealPipelineToAbsoluteMentionsFooter(t *testing.T) {
	// R-AXQR-2TF9
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subject := wiki.Subject{ID: "subject-acme", Name: "Acme Corp", Type: "entity"}
	if err := wiki.NewSubjectStore(conn).Save(ctx, subject); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	if err := wiki.NewPageStore(conn).Upsert(ctx, wiki.Page{
		ID: "page-acme", SubjectID: subject.ID, Title: "Acme Corp", Body: "Acme Corp makes widgets.",
	}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}

	prompts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var call struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/embed":
			_ = json.NewEncoder(w).Encode(map[string]any{"vectors": [][]float32{{1}}})
		case "/completions":
			text := `{"sub_queries":["Acme Corp"],"keywords":["widgets"]}`
			if call.Name == "wiki.ask-synthesis" {
				text = `{"found":true,"text":"Acme Corp makes widgets.","citations":[{"path":"entity/acme-corp","title":"Acme Corp"}]}`
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "test-item", "status": "done", "result": json.RawMessage(text)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer prompts.Close()

	client := llm.New(prompts.URL)
	cache := retrieve.NewVectorCache()
	cache.Upsert(retrieve.VectorEntry{Scope: "default", SubjectID: subject.ID, Title: subject.Name, Vec: []float32{1}})
	vector := retrieve.NewVectorRetriever(func(ctx context.Context, attr llm.Attribution, text string) ([]float32, error) {
		vectors, err := client.Embed(ctx, llm.EmbedSite{Name: "wiki.embed-query", Model: "embed", Dims: 1}, attr, "query", []string{text})
		if err != nil {
			return nil, err
		}
		return vectors[0], nil
	}, cache)
	search := retrieve.NewHybridRetriever(nil, vector, nil, nil, retrieve.FusionConfig{})
	asker := ask.New(search, wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), client,
		ask.DefaultSubjectCallSite(), ask.DefaultSynthesisCallSite())
	service := wiki.NewService(conn, nil, nil, time.Now)
	webBase := "https://acct.ikigenba.com/srv/wiki/subject/"
	site, err := appkitweb.Load(testWWWRoot(t))
	if err != nil {
		t.Fatalf("load test site: %v", err)
	}
	handler := web.NewHandler("wiki", "v-test", "/srv/wiki/", site,
		web.WithAsker(asker),
		web.WithMentioner(mentionAdapter{svc: service, webBase: webBase}),
		web.WithLinkifier(service, webBase),
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/private/default/?q=widgets", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	want := `<a href="https://acct.ikigenba.com/srv/wiki/private/default/subject/entity/acme-corp">Acme Corp</a>`
	if !strings.Contains(body, "Acme Corp</a> makes widgets.") || !strings.Contains(body, `<nav aria-label="Mentions">`) || !strings.Contains(body, want) {
		t.Fatalf("real ask pipeline missing synthesized text or absolute mentions footer link %q: %s", want, body)
	}
}

func TestBuildSpecMatchesDirectMCPToolSurface(t *testing.T) {
	// R-04HB-QM7T
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	spec := newSpec(staticConfig(wiki.Config{}))
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test-version",
		Service:    "wiki",
		Register:   spec.Handlers,
		DB:         conn,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	var direct http.Handler
	_, err = server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test-version",
		Service:    "wiki",
		Register: func(rt *appkit.Router) error {
			var err error
			direct, err = mcp.NewHandler(rt,
				mcp.WithIngestService(surfaceWiki{}),
				mcp.WithJobStatusService(surfaceWiki{}),
				mcp.WithJobAbortService(surfaceWiki{}),
				mcp.WithJobRerunService(surfaceWiki{}),
				mcp.WithJobListService(surfaceWiki{}),
				mcp.WithJobsCountService(surfaceWiki{}),
				mcp.WithMergeService(surfaceWiki{}, surfaceWiki{}),
				mcp.WithMergeListService(surfaceWiki{}),
				mcp.WithAskFunc(surfaceAsk),
				mcp.WithSubjectListService(surfaceWiki{}),
				mcp.WithClaimListService(surfaceWiki{}),
				mcp.WithPagePathService(surfaceWiki{}),
				mcp.WithScopeService(surfaceWiki{}),
			)
			return err
		},
	})
	if err != nil {
		t.Fatalf("direct mcp.NewHandler: %v", err)
	}
	if direct == nil {
		t.Fatal("direct mcp.NewHandler returned nil")
	}

	composedTools := mcpToolSurface(t, srv.Handler, true)
	directTools := mcpToolSurface(t, direct, false)
	if !reflect.DeepEqual(composedTools, directTools) {
		t.Fatalf("tool surface mismatch\ncomposed=%#v\ndirect=%#v", composedTools, directTools)
	}
}

func TestBuildSpecScopesRuntimeEmbeddingAndDeleteCache(t *testing.T) {
	// R-R1EX-LNNQ
	// R-R6AJ-4QMI
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	if _, err := wiki.NewScopeStore(conn).Create(ctx, "s1"); err != nil {
		t.Fatalf("Create(s1): %v", err)
	}

	prov := &capturingProvider{responses: []string{
		`{"subjects":[{
			"type":"entity",
			"kind":"company",
			"name":"Acme Robotics",
			"occurred_at":"",
			"claims":["Acme Robotics opened a recorded embedding lab."]
		}]}`,
		`{"title":"Acme Robotics","body":"Acme Robotics opened a recorded embedding lab."}`,
		`{"sub_queries":["recorded embedding lab"],"keywords":[],"aliases":[]}`,
		`{"found":true,"text":"Acme Robotics opened the lab.","citations":[{"path":"entity/acme-robotics","title":"Acme Robotics"}]}`,
		`{"sub_queries":["recorded embedding lab"],"keywords":[],"aliases":[]}`,
		`{"sub_queries":["recorded embedding lab"],"keywords":[],"aliases":[]}`,
	}}
	client, embeds := llmtest.NewClientWithEmbeddings(t, prov, [][]float32{{0.6, 0.8}})
	extractSite := extract.DefaultCallSite()
	extractSite.Config.Model = "extract-model"
	compileSite := compile.DefaultCallSite()
	compileSite.Config.Model = "compile-model"
	spec := newSpec(staticConfig(wiki.Config{
		CallSites: wiki.CallSites{
			Extract:      extractSite,
			Compile:      compileSite,
			AskSubject:   ask.DefaultSubjectCallSite(),
			AskSynthesis: ask.DefaultSynthesisCallSite(),
		},
		EmbedSite: wiki.EmbedSite{
			Model: "recorded-page-embed-model",
			Dims:  2,
		},
		LLM: client,
	}))
	h := buildSpecTestHandler(t, conn, spec)
	stopWorker, workerErr := startBuildSpecWorker(t, ctx, spec.Workers[0])
	defer stopWorker()

	var ingest struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal([]byte(mcpToolCallText(t, h, `{
		"jsonrpc":"2.0",
		"id":"ingest",
		"method":"tools/call",
		"params":{"name":"ingest","arguments":{"scope":"s1","text":"Acme Robotics opened a recorded embedding lab.","title":"Recorded lab"}}
	}`)), &ingest); err != nil {
		t.Fatalf("decode ingest response: %v", err)
	}
	status := waitBuildSpecJob(t, ctx, conn, ingest.JobID, wiki.JobDone, workerErr)
	if len(status.Subjects) != 1 {
		t.Fatalf("job subjects = %#v, want one embedded subject", status.Subjects)
	}

	var ownScope struct {
		Found bool `json:"found"`
	}
	ownScopeText := mcpToolCallText(t, h, `{
		"jsonrpc":"2.0","id":"ask-s1","method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"s1","question":"Where is the recorded embedding lab?"}}
	}`)
	if err := json.Unmarshal([]byte(ownScopeText), &ownScope); err != nil || !ownScope.Found {
		t.Fatalf("s1 meaning ask text = %q decoded %+v, %v; want runtime-upserted subject", ownScopeText, ownScope, err)
	}
	var defaultScope struct {
		Found bool `json:"found"`
	}
	if err := json.Unmarshal([]byte(mcpToolCallText(t, h, `{
		"jsonrpc":"2.0","id":"ask-default","method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"default","question":"Where is the recorded embedding lab?"}}
	}`)), &defaultScope); err != nil || defaultScope.Found {
		t.Fatalf("default meaning ask = %+v, %v; want no leaked s1 subject", defaultScope, err)
	}

	mcpToolCallText(t, h, `{
		"jsonrpc":"2.0","id":"delete-s1","method":"tools/call",
		"params":{"name":"scope_delete","arguments":{"name":"s1"}}
	}`)
	mcpToolCallText(t, h, `{
		"jsonrpc":"2.0","id":"recreate-s1","method":"tools/call",
		"params":{"name":"scope_create","arguments":{"name":"s1"}}
	}`)
	var afterDelete struct {
		Found bool `json:"found"`
	}
	if err := json.Unmarshal([]byte(mcpToolCallText(t, h, `{
		"jsonrpc":"2.0","id":"ask-after-delete","method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"s1","question":"Where is the recorded embedding lab?"}}
	}`)), &afterDelete); err != nil || afterDelete.Found {
		t.Fatalf("recreated s1 meaning ask = %+v, %v; want deleted generation evicted without restart", afterDelete, err)
	}

	requests := embeds.Requests()
	if len(requests) != 4 || requests[0].Name != "wiki.embed-page" || requests[0].Role != "document" || requests[0].GroupID == "" || requests[0].GroupID == ingest.JobID || requests[0].Model != "recorded-page-embed-model" || requests[0].Dimensions != 2 {
		t.Fatalf("prompts embedding requests = %#v, want one page embedding and three scoped query embeddings", requests)
	}
}

func TestBuildSpecMergeRemovesLoserVectorFromLiveCache(t *testing.T) {
	// R-WS3C-J4QB
	// R-14G2-4NDH
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	saveBuildSpecMergeFixture(t, ctx, conn)

	if err := wiki.NewEmbeddingStore(conn).Upsert(ctx, wiki.Embedding{
		SubjectID:   "subject-loser",
		Model:       "merge-cache-model",
		Dims:        2,
		Vec:         []float32{1, 0},
		ContentHash: "old-loser",
		UpdatedAt:   42,
	}); err != nil {
		t.Fatalf("seed loser embedding: %v", err)
	}

	prov := &capturingProvider{responses: []string{
		`{"title":"Winner Subject","body":"Loser fact.\nWinner fact."}`,
		`{"sub_queries":["meaning lane"],"keywords":[],"aliases":[]}`,
		`{"found":true,"text":"The winner retained the merged page.","citations":[{"path":"entity/winner-subject","title":"Winner Subject"}]}`,
	}}
	client, embeds := llmtest.NewClientWithEmbeddings(t, prov, [][]float32{{1, 0}})
	compileSite := compile.DefaultCallSite()
	compileSite.Config.Model = "merge-compile-model"
	askSubjectSite := ask.DefaultSubjectCallSite()
	askSubjectSite.Config.Model = "merge-ask-subject-model"
	askSynthesisSite := ask.DefaultSynthesisCallSite()
	askSynthesisSite.Config.Model = "merge-ask-synthesis-model"
	spec := newSpec(staticConfig(wiki.Config{
		CallSites: wiki.CallSites{
			Compile:      compileSite,
			AskSubject:   askSubjectSite,
			AskSynthesis: askSynthesisSite,
		},
		EmbedSite: wiki.EmbedSite{
			Model: "merge-cache-model",
			Dims:  2,
		},
		SearchDefault: 1,
		LLM:           client,
	}))
	h := buildSpecTestHandler(t, conn, spec)
	stopWorker, workerErr := startBuildSpecWorker(t, ctx, spec.Workers[0])
	defer stopWorker()

	var merge struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal([]byte(mcpToolCallText(t, h, `{
		"jsonrpc":"2.0",
		"id":"merge",
		"method":"tools/call",
		"params":{"name":"merge","arguments":{"scope":"default","from":"entity/loser-subject","to":"entity/winner-subject"}}
	}`)), &merge); err != nil {
		t.Fatalf("decode merge response: %v", err)
	}
	waitBuildSpecJob(t, ctx, conn, merge.JobID, wiki.JobDone, workerErr)

	var answer struct {
		Found bool `json:"found"`
	}
	if err := json.Unmarshal([]byte(mcpToolCallText(t, h, `{
		"jsonrpc":"2.0",
		"id":"ask",
		"method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"default","question":"meaning lane"}}
	}`)), &answer); err != nil {
		t.Fatalf("decode ask response: %v", err)
	}
	if !answer.Found {
		t.Fatalf("ask response = %+v, want winner page found after loser vector eviction", answer)
	}

	requests := embeds.Requests()
	if len(requests) != 2 || requests[0].Name != "wiki.embed-page" || requests[0].GroupID == "" || requests[0].GroupID == merge.JobID || requests[1].Name != "wiki.embed-query" {
		t.Fatalf("prompts embedding requests = %+v, want merge page then ask query", requests)
	}
}

func TestBuildSpecRoutesQueryEmbeddingThroughPrompts(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subject := wiki.Subject{ID: "subject-acme", Name: "Acme Robotics", Type: "entity"}
	if err := wiki.NewSubjectStore(conn).Save(ctx, subject); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	if err := wiki.NewPageStore(conn).Upsert(ctx, wiki.Page{
		ID:        "page-acme",
		SubjectID: subject.ID,
		Title:     "Acme Robotics",
		Body:      "Acme Robotics owns the scheduler.",
	}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}
	if err := wiki.NewEmbeddingStore(conn).Upsert(ctx, wiki.Embedding{
		SubjectID:   subject.ID,
		Model:       "recorded-query-embed-model",
		Dims:        2,
		Vec:         []float32{1, 0},
		ContentHash: "hash-acme",
		UpdatedAt:   42,
	}); err != nil {
		t.Fatalf("Upsert embedding: %v", err)
	}

	prov := &capturingProvider{responses: []string{
		`{"sub_queries":["scheduler owner"],"keywords":["scheduler"],"aliases":[]}`,
		`{"found":true,"text":"Acme Robotics owns the scheduler.","citations":[{"path":"entity/acme-robotics","title":"Acme Robotics"}]}`,
	}}
	client, embeds := llmtest.NewClientWithEmbeddings(t, prov, [][]float32{{1, 0}})
	askSubjectSite := ask.DefaultSubjectCallSite()
	askSubjectSite.Config.Model = "ask-subject-model"
	askSynthesisSite := ask.DefaultSynthesisCallSite()
	askSynthesisSite.Config.Model = "ask-synthesis-model"
	spec := newSpec(staticConfig(wiki.Config{
		CallSites: wiki.CallSites{
			AskSubject:   askSubjectSite,
			AskSynthesis: askSynthesisSite,
		},
		EmbedSite: wiki.EmbedSite{
			Model: "recorded-query-embed-model",
			Dims:  2,
		},
		SearchDefault: 8,
		LLM:           client,
	}))
	h := buildSpecTestHandler(t, conn, spec)

	var answer struct {
		Found  bool   `json:"found"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(mcpToolCallText(t, h, `{
		"jsonrpc":"2.0",
		"id":"ask",
		"method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"default","question":"Who owns the scheduler?"}}
	}`)), &answer); err != nil {
		t.Fatalf("decode ask response: %v", err)
	}
	if !answer.Found || answer.Answer != "[Acme Robotics](https://int.ikigenba.com/srv/wiki/private/default/subject/entity/acme-robotics) owns the scheduler." {
		t.Fatalf("ask response = %#v, want answer produced through composed retriever", answer)
	}

	requests := embeds.Requests()
	if len(requests) != 1 || requests[0].Name != "wiki.embed-query" || requests[0].Role != "query" || requests[0].Model != "recorded-query-embed-model" || requests[0].Dimensions != 2 {
		t.Fatalf("prompts embedding requests = %#v, want one labeled query request from buildSpec retriever", requests)
	}
}

func TestBuildSpecSharesOneAskCacheAcrossMCPAndWeb(t *testing.T) {
	// R-0L1X-86TB
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subject := wiki.Subject{ID: "subject-shared", Name: "Shared Scheduler", Type: "entity"}
	if err := wiki.NewSubjectStore(conn).Save(ctx, subject); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	if err := wiki.NewPageStore(conn).Upsert(ctx, wiki.Page{
		ID: subject.ID, SubjectID: subject.ID, Title: subject.Name, Body: "Shared Scheduler owns the queue.",
	}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}
	if err := wiki.NewEmbeddingStore(conn).Upsert(ctx, wiki.Embedding{
		SubjectID: subject.ID, Model: "shared-model", Dims: 1, Vec: []float32{1}, ContentHash: "seed", UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("Upsert embedding: %v", err)
	}

	provider := &capturingProvider{responses: []string{
		`{"sub_queries":["queue owner"],"keywords":["queue"],"aliases":[]}`,
		`{"found":true,"text":"Shared Scheduler owns the queue.","citations":[{"path":"entity/shared-scheduler","title":"Shared Scheduler"}]}`,
	}}
	client, _ := llmtest.NewClientWithEmbeddings(t, provider, [][]float32{{1}})
	spec := newSpec(staticConfig(wiki.Config{
		CallSites: wiki.CallSites{
			AskSubject:   ask.DefaultSubjectCallSite(),
			AskSynthesis: ask.DefaultSynthesisCallSite(),
		},
		EmbedSite:     wiki.EmbedSite{Model: "shared-model", Dims: 1},
		SearchDefault: 8,
		AskCacheCap:   10,
		LLM:           client,
	}))
	handler := buildSpecTestHandler(t, conn, spec)
	question := "Who owns the queue?"
	answer := mcpToolCallText(t, handler, `{
		"jsonrpc":"2.0","id":"ask","method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"default","question":"Who owns the queue?"}}
	}`)
	if !strings.Contains(answer, "owns the queue") {
		t.Fatalf("MCP answer = %q, want synthesized answer", answer)
	}

	req := httptest.NewRequest(http.MethodGet, "/private/default/?q="+url.QueryEscape(question), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Shared Scheduler") {
		t.Fatalf("web ask status/body = %d/%s, want cached answer", rec.Code, rec.Body.String())
	}
	synthesisCalls := 0
	for _, request := range provider.requests {
		if request.System == ask.DefaultSynthesisCallSite().System {
			synthesisCalls++
		}
	}
	if synthesisCalls != 1 {
		t.Fatalf("synthesis calls after MCP then web = %d, want one shared computation", synthesisCalls)
	}
}

func TestBuildCompilerUsesDefaultCompileCallSite(t *testing.T) {
	prov := &capturingProvider{responses: []string{`{"title":"Acme Robotics","body":"Acme Robotics runs a Tulsa lab."}`}}
	wantSite := compile.DefaultCallSite()
	wantSite.Config.Model = "compile-model"
	cfg := wiki.Config{
		CallSites: wiki.CallSites{Compile: wantSite},
		LLM:       llmtest.NewClient(t, prov),
	}
	compiler := buildCompiler(cfg, cfg.LLM)

	title, body, err := compiler.Compile(context.Background(), llm.Attribution{}, wiki.Subject{
		ID:       "subject-acme",
		Name:     "Acme Robotics",
		NormName: "acme-robotics",
		Type:     "entity",
	}, []wiki.Claim{
		{ID: "claim-001", SubjectID: "subject-acme", Body: "Acme Robotics runs a Tulsa lab."},
	})
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if title != "Acme Robotics" || body != "Acme Robotics runs a Tulsa lab." {
		t.Fatalf("Compile result = %q/%q, want mocked page", title, body)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests len = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if req.Model != wantSite.Config.Model {
		t.Fatalf("request model = %q, want %q from compile.DefaultCallSite", req.Model, wantSite.Config.Model)
	}
	if req.System != wantSite.System {
		t.Fatalf("request system = %q, want %q from compile.DefaultCallSite", req.System, wantSite.System)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("request tools len = %d, want tool-less compile call site", len(req.Tools))
	}
	if wantSite.Config.Temperature != nil || wantSite.Config.Thinking != nil || req.Gen.Temperature != nil || req.Gen.Reasoning.Disabled() {
		t.Fatalf("site/request generation settings = %#v/%#v, want no temperature or thinking pin", wantSite.Config, req.Gen)
	}
}

func decodeMCPToolText(t *testing.T, raw []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(mcpToolText(t, raw)), dst); err != nil {
		t.Fatalf("decode MCP tool text: %v", err)
	}
}

func mcpToolCallText(t *testing.T, h http.Handler, body string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("X-Owner-Id", "owner-id")
	req.Header.Set("X-Owner-Email", "owner@example.com")
	req.Header.Set("X-Client-Id", "client-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return mcpToolText(t, rec.Body.Bytes())
}

func mcpToolText(t *testing.T, raw []byte) string {
	t.Helper()
	var got struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode MCP response: %v", err)
	}
	if len(got.Result.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(got.Result.Content))
	}
	return got.Result.Content[0].Text
}

func mcpToolSurface(t *testing.T, h http.Handler, authenticated bool) []struct {
	Name        string         `json:"name"`
	InputSchema map[string]any `json:"inputSchema"`
} {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"list","method":"tools/list"}`))
	if authenticated {
		req.Header.Set("X-Owner-Id", "owner-id")
		req.Header.Set("X-Owner-Email", "owner@example.com")
		req.Header.Set("X-Client-Id", "client-1")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	return got.Result.Tools
}

func staticConfig(cfg wiki.Config) configLoader {
	return func(func(string) string) (wiki.Config, error) {
		return cfg, nil
	}
}

func migratedDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	conn, err := appdb.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	migs, err := appdb.LoadMigrations(wikidb.FS, "migrations")
	if err != nil {
		conn.Close()
		t.Fatalf("LoadMigrations: %v", err)
	}
	if err := appdb.Migrate(ctx, conn, migs); err != nil {
		conn.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return conn
}

func saveBuildSpecMergeFixture(t *testing.T, ctx context.Context, conn *sql.DB) {
	t.Helper()
	subjects := wiki.NewSubjectStore(conn)
	for _, subject := range []wiki.Subject{
		{ID: "subject-loser", Name: "Loser Subject", Type: "entity"},
		{ID: "subject-winner", Name: "Winner Subject", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save subject %s: %v", subject.ID, err)
		}
	}
	claims := wiki.NewClaimStore(conn)
	for _, claim := range []wiki.Claim{
		{ID: "claim-loser", SubjectID: "subject-loser", JobID: "job-existing", Body: "Loser fact.", Kind: wiki.ClaimKind},
		{ID: "claim-winner", SubjectID: "subject-winner", JobID: "job-existing", Body: "Winner fact.", Kind: wiki.ClaimKind},
	} {
		if err := claims.Save(ctx, claim); err != nil {
			t.Fatalf("Save claim %s: %v", claim.ID, err)
		}
	}
	pages := wiki.NewPageStore(conn)
	for _, page := range []wiki.Page{
		{ID: "subject-loser", SubjectID: "subject-loser", Title: "Loser Subject", Body: "old loser body"},
		{ID: "subject-winner", SubjectID: "subject-winner", Title: "Winner Subject", Body: "old winner body"},
	} {
		if err := pages.Upsert(ctx, page); err != nil {
			t.Fatalf("Upsert page %s: %v", page.ID, err)
		}
	}
}

func buildSpecTestHandler(t *testing.T, conn *sql.DB, spec appkit.Spec) http.Handler {
	t.Helper()
	site, err := appkitweb.Load(testWWWRoot(t))
	if err != nil {
		t.Fatalf("load test site: %v", err)
	}
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test-version",
		Service:    "wiki",
		Register:   spec.Handlers,
		DB:         conn,
		WWW:        site,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return srv.Handler
}

func startBuildSpecWorker(t *testing.T, parent context.Context, run func(context.Context) error) (func(), <-chan error) {
	t.Helper()
	if run == nil {
		t.Fatal("buildSpec worker is nil")
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx)
	}()
	stop := func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("buildSpec worker returned error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("buildSpec worker did not stop")
		}
	}
	return stop, done
}

func waitBuildSpecJob(t *testing.T, ctx context.Context, conn *sql.DB, jobID, want string, workerErr <-chan error) wiki.JobStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	jobs := wiki.NewJobStore(conn)
	var last wiki.JobStatus
	for time.Now().Before(deadline) {
		select {
		case err := <-workerErr:
			if err != nil {
				t.Fatalf("buildSpec worker returned before job completed: %v", err)
			}
			t.Fatal("buildSpec worker stopped before job completed")
		default:
		}
		status, err := jobs.Status(ctx, jobID)
		if err == nil {
			last = status
			if status.Status == want {
				return status
			}
			if status.Status == wiki.JobFailed {
				t.Fatalf("job %s failed: %s", jobID, status.Error)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s status = %+v, want %s", jobID, last, want)
	return wiki.JobStatus{}
}

func manifestExtras(in []appkit.ManifestKV) []manifest.KV {
	out := make([]manifest.KV, 0, len(in))
	for _, kv := range in {
		out = append(out, manifest.KV{Key: kv.Key, Value: kv.Value})
	}
	return out
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
			t.Fatalf("wiki exited before health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
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
	t.Fatalf("wiki never served health at %s: %s\nstdout:\n%s\nstderr:\n%s", url, last, stdout.String(), stderr.String())
	return nil
}

func stopProcess(cancel context.CancelFunc, done <-chan error) {
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}

type capturingProvider struct {
	responses []string
	requests  []llmtest.Request
}

type surfaceWiki struct{}

func (surfaceWiki) Ingest(context.Context, string, string, string, string, string, []string) (string, error) {
	return "", nil
}

func (surfaceWiki) JobStatus(context.Context, string) (publicJobStatus, error) {
	return publicJobStatus{}, nil
}

func (surfaceWiki) Abort(context.Context, string) (wiki.AbortResult, error) {
	return wiki.AbortResult{}, nil
}

func (surfaceWiki) Rerun(context.Context, string) (wiki.RerunResult, error) {
	return wiki.RerunResult{}, nil
}

func (surfaceWiki) ListJobsInScope(context.Context, string, mcp.JobFilter, paging.Params) ([]wiki.Job, string, error) {
	return []wiki.Job{{ID: "job-1", Status: wiki.JobDone}}, "", nil
}

func (surfaceWiki) CountJobsInScope(context.Context, string, mcp.JobFilter) (int, error) {
	return 1, nil
}

func (surfaceWiki) GetByPath(context.Context, string, string) (wiki.Subject, error) {
	return wiki.Subject{ID: "subject-1", Name: "Acme", NormName: "acme", Type: "entity"}, nil
}

func (surfaceWiki) MergeSubjects(context.Context, string, string, string) (string, error) {
	return "job-merge", nil
}

func (surfaceWiki) ListMergesInScope(context.Context, string, paging.Params) ([]wiki.Alias, string, error) {
	return []wiki.Alias{{NormName: "old acme", SubjectID: "subject-1", Name: "Old Acme", OwnerID: "owner-id", OwnerEmail: "owner@example.com", CreatedAt: "2026-06-24T12:00:00Z"}}, "", nil
}

func (surfaceWiki) Subjects(context.Context, string, string) ([]publicSubject, error) {
	return []publicSubject{{Path: "entity/acme", Type: "entity", Name: "Acme", HasPage: true}}, nil
}

func (surfaceWiki) ListInScope(context.Context, string, string, string, paging.Params) ([]publicSubject, string, error) {
	return []publicSubject{{Path: "entity/acme", Type: "entity", Name: "Acme", HasPage: true}}, "", nil
}

func (surfaceWiki) ClaimsBySubject(context.Context, string) ([]publicClaim, error) {
	return []publicClaim{{ID: "claim-1", Text: "Claim text.", Job: "job-1"}}, nil
}

func (surfaceWiki) ListBySubjectInScope(context.Context, string, string, paging.Params) ([]publicClaim, string, error) {
	return []publicClaim{{ID: "claim-1", Text: "Claim text.", Job: "job-1"}}, "", nil
}

func (surfaceWiki) PageByPathInScope(context.Context, string, string) (publicPage, error) {
	return publicPage{}, nil
}

func (surfaceWiki) Create(context.Context, string) (wiki.Scope, error) { return wiki.Scope{}, nil }
func (surfaceWiki) Get(_ context.Context, name string) (wiki.Scope, error) {
	return wiki.Scope{Name: name, Visibility: "private"}, nil
}
func (surfaceWiki) List(context.Context) ([]wiki.Scope, error)            { return nil, nil }
func (surfaceWiki) SetVisibility(context.Context, string, string) error   { return nil }
func (surfaceWiki) SetInstructions(context.Context, string, string) error { return nil }
func (surfaceWiki) Delete(context.Context, string) error                  { return nil }

func surfaceAsk(context.Context, string, string, string) (askSurfaceAnswer, error) {
	return askSurfaceAnswer{}, nil
}

type askSurfaceAnswer struct {
	Found     bool
	Text      string
	Citations []askSurfaceCitation
}

type askSurfaceCitation struct {
	Path  string
	Title string
}

func (p *capturingProvider) RoundTrip(_ context.Context, req *llmtest.Request) *llmtest.RoundTrip {
	p.requests = append(p.requests, cloneRequest(req))
	text := `{"title":"Untitled","body":"Empty."}`
	if len(p.responses) > 0 {
		text = p.responses[0]
		p.responses = p.responses[1:]
	}
	return llmtest.NewRoundTrip(
		llmtest.Message{Role: llmtest.RoleAssistant, Blocks: []llmtest.Block{llmtest.TextBlock{Text: text}}},
		llmtest.FinishStop,
		llmtest.Usage{InputUncached: 1, Output: 1, Total: 2},
		nil,
		nil,
		0,
		false,
	)
}

func (p *capturingProvider) Name() string {
	return "capturing"
}

func (p *capturingProvider) Pricing(string) (llmtest.Pricing, bool) {
	return llmtest.Pricing{Tiers: []llmtest.RateTier{{MinInputTokens: 0}}}, true
}

func cloneRequest(req *llmtest.Request) llmtest.Request {
	if req == nil {
		return llmtest.Request{}
	}
	return llmtest.Request{
		Model:    req.Model,
		System:   req.System,
		Messages: append([]llmtest.Message(nil), req.Messages...),
		Tools:    append([]llmtest.Tool(nil), req.Tools...),
		Gen:      req.Gen,
	}
}
