package main

import (
	"appkit"
	"appkit/config"
	"appkit/manifest"
	"appkit/server"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"eventplane/consumer"
	"eventplane/outbox"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
	"registry"
	"scripts/internal/consume"
	"scripts/internal/repos"
	"scripts/internal/script"
	"strconv"
	"strings"
	"testing"
	"time"

	appkitdatabase "appkit/db"

	appweb "appkit/web"

	scriptsdb "scripts/internal/db"
)

// R-2A9S-R2E9
func TestVersionPlaneCompositionUsesRegistryAndNoPinnedPort(t *testing.T) {
	plane := newVersionPlane()
	client, ok := plane.(*repos.Client)
	if !ok {
		t.Fatalf("version plane type = %T, want *repos.Client", plane)
	}
	if got, want := client.BaseURL(), registry.BaseURL("repos"); got != want {
		t.Fatalf("version plane base = %q, want %q", got, want)
	}
	svc := &script.Service{Plane: plane}
	if svc.Plane == nil {
		t.Fatal("assembled Service.Plane is nil")
	}

	files, err := filepath.Glob("../../internal/repos/*.go")
	if err != nil {
		t.Fatalf("glob internal/repos: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("internal/repos contains no Go files")
	}
	for _, name := range files {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if bytes.Contains(body, []byte("127.0.0.1:30")) {
			t.Errorf("%s contains a pinned service port", name)
		}
	}
}

func TestScriptsFeedFramesRoutedCompletionEvent(t *testing.T) {
	// R-85Y5-KID2
	conn, err := appkitdatabase.Open(filepath.Join(t.TempDir(), "scripts.db"))
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := appkitdatabase.LoadMigrations(scriptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdatabase.Migrate(context.Background(), conn, migs); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ob, err := outbox.New(conn, outbox.Options{Source: "scripts", Registry: script.Events})
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	store := script.NewStore(conn)
	store.Outbox = ob
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sc := script.Script{ID: "script-1", OwnerID: "owner-1", OwnerEmail: "owner@example.com", Name: "nightly-export", Body: "print('ok')", Config: script.Config{Interpreter: "python3", TimeoutSecs: 30}, CreatedAt: now, UpdatedAt: now}
	if err := store.InsertScript(context.Background(), sc); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	run := script.Run{ID: "run-1", ScriptID: sc.ID, Status: script.RunRunning, StartedAt: now, StdoutPath: "runs/run-1/stdout.log", StderrPath: "runs/run-1/stderr.log"}
	if err := store.InsertRun(context.Background(), run); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	zero := 0
	if err := store.FinishRun(context.Background(), script.FinishRunInput{RunID: run.ID, ScriptID: sc.ID, ScriptName: sc.Name, Status: script.RunSucceeded, ExitCode: &zero, EndedAt: now}); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	server := httptest.NewServer(ob.FeedHandler())
	t.Cleanup(server.Close)
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("get feed: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	var event, data string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
		if line == "" && event == "scripts:succeeded/nightly-export" && data != "" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read feed: %v", err)
	}
	if event != "scripts:succeeded/nightly-export" {
		t.Fatalf("SSE event = %q, want scripts:succeeded/nightly-export", event)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		t.Fatalf("decode event envelope: %v", err)
	}
	if _, ok := envelope["type"]; ok {
		t.Fatalf("event envelope must not contain type: %s", data)
	}
	for _, key := range []string{"kind", "subject"} {
		if _, ok := envelope[key]; !ok {
			t.Fatalf("event envelope missing %q: %s", key, data)
		}
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

func TestRunTTLDefaultMatchesCommittedManifest(t *testing.T) {
	// R-HDCE-C6WU
	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}
	var manifestValue string
	for _, line := range strings.Split(string(committed), "\n") {
		if value, ok := strings.CutPrefix(line, "SCRIPTS_RUN_TTL="); ok {
			manifestValue = value
			break
		}
	}
	if manifestValue == "" {
		t.Fatal("committed manifest.env has no SCRIPTS_RUN_TTL value")
	}
	want, err := time.ParseDuration(manifestValue)
	if err != nil {
		t.Fatalf("parse committed SCRIPTS_RUN_TTL %q: %v", manifestValue, err)
	}

	t.Setenv("SCRIPTS_RUN_TTL", "")
	got, err := config.EnvOrDuration(os.Getenv, "SCRIPTS_RUN_TTL", defaultRunTTL)
	if err != nil {
		t.Fatalf("resolve default SCRIPTS_RUN_TTL: %v", err)
	}
	if got != want {
		t.Fatalf("resolved default SCRIPTS_RUN_TTL = %s, committed manifest value = %s", got, want)
	}
}

func TestProductionGoSourceContainsNoBoxPathLiteral(t *testing.T) {
	// R-VKB6-SHHV
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		var forbidden token.Pos
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return forbidden == token.NoPos
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && strings.Contains(value, "/opt") {
				forbidden = literal.Pos()
				return false
			}
			return forbidden == token.NoPos
		})
		if forbidden != token.NoPos {
			position := fset.Position(forbidden)
			return fmt.Errorf("production Go source contains forbidden box-path literal at %s", position)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
	wantSources := []string{"cron", "crm", "ledger", "dropbox", "prompts", "repos"}
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

func TestReposConsumerAndSubjectRouting(t *testing.T) {
	// R-2V03-9602
	wantSources := []string{"cron", "crm", "ledger", "dropbox", "prompts", "repos"}
	spec := scriptsSpec()
	if got := consumesFromConsumers(spec.Consumers); !reflect.DeepEqual(got, wantSources) {
		t.Fatalf("consumer sources = %v, want exactly %v", got, wantSources)
	}
	for _, entry := range spec.Consumers {
		if len(entry.Subscriptions) != 1 {
			t.Fatalf("%s subscription count = %d, want 1", entry.Source, len(entry.Subscriptions))
		}
		sub := entry.Subscriptions[0]
		if sub.Source != entry.Source || sub.Filter != "**" {
			t.Fatalf("%s subscription = %#v, want source %q and filter **", entry.Source, sub, entry.Source)
		}
	}

	ctx := context.Background()
	svc, _ := newConsumerTestService(t)
	sites, err := svc.Create(ctx, "owner@example.com", script.CreateInput{Name: "sites check", Body: "print(1)"})
	if err != nil {
		t.Fatalf("create sites script: %v", err)
	}
	scripts, err := svc.Create(ctx, "owner@example.com", script.CreateInput{Name: "scripts check", Body: "print(2)"})
	if err != nil {
		t.Fatalf("create scripts script: %v", err)
	}
	if _, err := svc.SetTrigger(ctx, "owner@example.com", sites.ID, "repos:push/sites/**"); err != nil {
		t.Fatalf("set sites trigger: %v", err)
	}
	if _, err := svc.SetTrigger(ctx, "owner@example.com", scripts.ID, "repos:push/scripts/**"); err != nil {
		t.Fatalf("set scripts trigger: %v", err)
	}

	got, err := svc.ScriptsForEvent(ctx, "repos", "repos:push/sites/blog")
	if err != nil {
		t.Fatalf("ScriptsForEvent repos push: %v", err)
	}
	if len(got) != 1 || got[0] != sites.ID {
		t.Fatalf("ScriptsForEvent repos push = %v, want only [%s] and not %s", got, sites.ID, scripts.ID)
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
	if _, err := svc.SetTrigger(ctx, "owner@example.com", sc.ID, "crm:contact.created/**"); err != nil {
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
	ev := consumer.Event{Source: "crm", Kind: "contact.created", Subject: "/c1", ID: "evt-1", Payload: payload}
	if err := handler(ctx, ev); err != nil {
		t.Fatalf("well-formed event returned %v, want nil", err)
	}
	spawn := runner.awaitSpawn(t)
	if spawn.run.ScriptID != sc.ID {
		t.Fatalf("spawn script id = %q, want %q", spawn.run.ScriptID, sc.ID)
	}
	if spawn.run.TriggerSource != "crm" || spawn.run.TriggerKind != "contact.created" || spawn.run.TriggerSubject != "/c1" || spawn.run.TriggerEventID != "evt-1" {
		t.Fatalf("spawn trigger fields = %+v, want crm/contact.created/evt-1", spawn.run)
	}
	if string(spawn.input) != string(payload) {
		t.Fatalf("spawn input = %s, want %s", spawn.input, payload)
	}

	err = handler(ctx, consumer.Event{Source: "crm", Kind: "", ID: "", Payload: []byte(`{}`)})
	if err == nil {
		t.Fatal("malformed event returned nil, want ErrSkip-wrapped error")
	}
	if !errors.Is(err, consumer.ErrSkip) {
		t.Fatalf("malformed event error = %v, want errors.Is ErrSkip", err)
	}
	runner.assertNoSpawn(t)
}

func TestWWWSiteLoadsRealShareTree(t *testing.T) {
	// R-8Z2T-SF7W
	root := wwwRoot(t)
	deletedWebPackage := filepath.Join("internal", "web")
	if strings.Contains(root, deletedWebPackage) {
		t.Fatalf("WWW root %q points at deleted internal web package", root)
	}
	if _, err := os.Stat(filepath.Join("..", "..", "internal", "web")); !os.IsNotExist(err) {
		t.Fatalf("deleted internal web package still exists or stat failed unexpectedly: %v", err)
	}

	site := loadWWW(t)
	rec := httptest.NewRecorder()
	if err := site.Render(rec, "landing.html", landingData("scripts-real", "v1.2.3")); err != nil {
		t.Fatalf("render landing.html from share/www: %v", err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>scripts-real") {
		t.Fatalf("share/www landing render = status %d body:\n%s", rec.Code, rec.Body.String())
	}

	for _, rel := range []string{
		"landing.html",
		filepath.Join("static", "tokens.css"),
		filepath.Join("static", "fonts", "space-grotesk.woff2"),
		filepath.Join("static", "fonts", "ibm-plex-sans.woff2"),
		filepath.Join("static", "fonts", "ibm-plex-mono-400.woff2"),
		filepath.Join("static", "fonts", "ibm-plex-mono-500.woff2"),
	} {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("share/www missing %s: %v", rel, err)
		}
		if info.IsDir() || info.Size() == 0 {
			t.Fatalf("share/www/%s is not a non-empty file: dir=%v size=%d", rel, info.IsDir(), info.Size())
		}
	}
}

func TestWWWSiteRendersHTMLWithServiceAndVersion(t *testing.T) {
	// R-LAND-7Q3D
	// R-LAND-3T9H
	// R-LAND-9R5F
	// R-LAND-1S7G
	rec := renderLanding(t, "scripts-test", "v9.8.7")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}

	body := rec.Body.String()
	if count := strings.Count(body, "scripts-test"); count != 3 {
		t.Fatalf("service name count = %d, want 3 in title, heading, and service detail\n%s", count, body)
	}
	if count := strings.Count(body, "v9.8.7"); count != 1 {
		t.Fatalf("version count = %d, want 1\n%s", count, body)
	}

	other := renderLanding(t, "other-service", "build-123")
	normalizedBody := strings.ReplaceAll(strings.ReplaceAll(body, "scripts-test", "{{service}}"), "v9.8.7", "{{version}}")
	normalizedOther := strings.ReplaceAll(strings.ReplaceAll(other.Body.String(), "other-service", "{{service}}"), "build-123", "{{version}}")
	if normalizedBody != normalizedOther {
		t.Fatalf("landing HTML changed beyond service/version substitutions:\n%s\n---\n%s", normalizedBody, normalizedOther)
	}
}

func TestWWWSiteLinksOnlyAppLocalStaticAssets(t *testing.T) {
	// R-ASST-7Y1N
	// R-M8XL-ANIZ
	rec := renderLanding(t, "scripts", "dev")

	body := rec.Body.String()
	if !strings.Contains(body, `href="static/tokens.css"`) {
		t.Fatalf("landing HTML did not link document-relative tokens.css:\n%s", body)
	}
	for _, forbidden := range []string{`href="/static/`, "dashboard", "/srv/dashboard", "https://", "http://"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("landing HTML contains forbidden cross-service asset reference %q:\n%s", forbidden, body)
		}
	}
}

// R-RYDN-YNR5
func TestEveryServedHTMLDocumentCarriesBrandIconLinks(t *testing.T) {
	documents, err := filepath.Glob(filepath.Join(wwwRoot(t), "*.html"))
	if err != nil {
		t.Fatalf("enumerate served HTML documents: %v", err)
	}
	if len(documents) == 0 {
		t.Fatal("share/www contains no served HTML documents")
	}

	site := loadWWW(t)
	links := []struct {
		href       string
		attributes []string
	}{
		{href: "static/favicon.ico", attributes: []string{`rel="icon"`, `sizes="any"`}},
		{href: "static/favicon-32.png", attributes: []string{`rel="icon"`, `type="image/png"`}},
		{href: "static/apple-touch-icon.png", attributes: []string{`rel="apple-touch-icon"`}},
	}
	for _, document := range documents {
		name := filepath.Base(document)
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := site.Render(rec, name, landingData("scripts", "test")); err != nil {
				t.Fatalf("render %s: %v", name, err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("render %s status = %d, want %d", name, rec.Code, http.StatusOK)
			}
			body := rec.Body.String()
			for _, want := range links {
				link := linkMarkupWithHref(t, body, want.href)
				for _, attribute := range want.attributes {
					if !strings.Contains(link, attribute) {
						t.Fatalf("%s link for %s missing %s: %s", name, want.href, attribute, link)
					}
				}
			}
		})
	}
}

func TestWWWSiteRendersHomeLinkToDashboardApex(t *testing.T) {
	// R-HOME-8R2V
	rec := renderLanding(t, "scripts", "dev")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `<main>`+"\n"+`    <a class="home" href="/">Home</a>`) {
		t.Fatalf("landing HTML does not put the Home link first in the body:\n%s", body)
	}
	if count := strings.Count(body, `href="/"`); count != 1 {
		t.Fatalf(`href="/" count = %d, want exactly one dashboard-apex link:\n%s`, count, body)
	}
	for _, want := range []string{
		`.home {`,
		`top: var(--space-8);`,
		`position: relative;`,
		`.home:hover,`,
		`.home:focus-visible {`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("landing HTML missing home style %q:\n%s", want, body)
		}
	}
}

func TestWWWSiteUsesCronCanonicalStructureForScripts(t *testing.T) {
	rec := renderLanding(t, "scripts", "dev")

	body := rec.Body.String()
	for _, want := range []string{
		`<title>scripts · scripts</title>`,
		`<link rel="stylesheet" href="static/tokens.css">`,
		`<a class="home" href="/">Home</a>`,
		`<section aria-labelledby="page-title">`,
		`<div class="eyebrow">Script runner</div>`,
		`<h1 id="page-title">scripts</h1>`,
		`Scripts runs deterministic Python scripts wired to suite events and publishes completion events back to the event plane.`,
		`<dl aria-label="Service details">`,
		`<dt>API</dt>`,
		`<dd><code>POST /mcp</code></dd>`,
		`class="version"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("landing HTML missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{`class="shell"`, `class="card"`, `Scheduled event emitter`, `minute boundaries`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("landing HTML contains stale markup %q:\n%s", forbidden, body)
		}
	}
}

func TestWWWStaticServesTokensAndFonts(t *testing.T) {
	// R-ASST-5X8M
	// R-ASST-9Z3P
	site := loadWWW(t)
	cases := []struct {
		path        string
		contentType string
	}{
		{path: "/static/tokens.css", contentType: "text/css; charset=utf-8"},
		{path: "/static/fonts/space-grotesk.woff2", contentType: "font/woff2"},
		{path: "/static/fonts/ibm-plex-sans.woff2", contentType: "font/woff2"},
		{path: "/static/fonts/ibm-plex-mono-400.woff2", contentType: "font/woff2"},
		{path: "/static/fonts/ibm-plex-mono-500.woff2", contentType: "font/woff2"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			site.Static().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, tc.contentType)
			}
			if rec.Body.Len() == 0 {
				t.Fatal("body is empty")
			}
		})
	}
}

// R-RZLK-CFHU
func TestAssembledRouterServesBrandIconsWithPinnedContentTypes(t *testing.T) {
	handler := newWWWTestHandler(t)
	cases := []struct {
		path        string
		contentType string
	}{
		{path: "/static/favicon.ico", contentType: "image/x-icon"},
		{path: "/static/favicon-32.png", contentType: "image/png"},
		{path: "/static/apple-touch-icon.png", contentType: "image/png"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d\n%s", tc.path, rec.Code, http.StatusOK, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Fatalf("GET %s Content-Type = %q, want %q", tc.path, got, tc.contentType)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("GET %s returned an empty body", tc.path)
			}
		})
	}
}

func TestWWWTokensCSSDeclaresFontFaces(t *testing.T) {
	// R-M59W-5CAW
	// R-M6HS-J41L
	body := readWWWStatic(t, "/static/tokens.css")
	for _, want := range []string{
		`@font-face`,
		`url('fonts/space-grotesk.woff2')`,
		`url('fonts/ibm-plex-sans.woff2')`,
		`url('fonts/ibm-plex-mono-400.woff2')`,
		`url('fonts/ibm-plex-mono-500.woff2')`,
		`font-family: 'Space Grotesk'`,
		`font-family: 'IBM Plex Mono'`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("tokens.css missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "font-display: swap") {
		t.Fatalf("tokens.css must not use font-display: swap:\n%s", body)
	}
	if strings.Contains(body, "url('/static/fonts/") {
		t.Fatalf("tokens.css must not use origin-absolute font URLs:\n%s", body)
	}
	if fontFaces := strings.Count(body, "@font-face"); fontFaces != strings.Count(body, "font-display: optional") {
		t.Fatalf("font-display: optional count must match @font-face count in:\n%s", body)
	}
}

func TestWWWSitePreloadsDocumentRelativeDisplayFonts(t *testing.T) {
	// R-MA5H-OF9O
	body := renderLanding(t, "scripts", "dev").Body.String()
	css := readWWWStatic(t, "/static/tokens.css")
	for _, font := range []string{"space-grotesk.woff2", "ibm-plex-sans.woff2"} {
		href := "static/fonts/" + font
		link := linkMarkupWithHref(t, body, href)
		for _, want := range []string{`rel="preload"`, `as="font"`, `type="font/woff2"`, "crossorigin"} {
			if !strings.Contains(link, want) {
				t.Fatalf("font preload for %s does not contain %q:\n%s", href, want, link)
			}
		}
		if !strings.Contains(css, `url('fonts/`+font+`')`) {
			t.Fatalf("tokens.css no longer contains matching @font-face src for %s", font)
		}
	}
	for _, forbidden := range []string{`href="/static/fonts/`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("landing HTML contains origin-absolute font preload %q:\n%s", forbidden, body)
		}
	}
}

func TestExactRootRouteDoesNotShadowExistingPaths(t *testing.T) {
	// R-ROUT-8U2J
	// R-ROUT-1V4K
	// R-ROUT-3W6L
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", landingHandler(loadWWW(t), "scripts", "dev"))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "health")
	})
	mux.HandleFunc("GET /feed", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "feed")
	})
	mux.HandleFunc("GET /.well-known/prm", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "prm")
	})
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "mcp")
	})

	root := httptest.NewRecorder()
	mux.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), `<h1 id="page-title">scripts</h1>`) {
		t.Fatalf("root did not dispatch landing handler: status=%d body=%q", root.Code, root.Body.String())
	}

	mcp := httptest.NewRecorder()
	mux.ServeHTTP(mcp, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if mcp.Code != http.StatusOK || mcp.Body.String() != "mcp" {
		t.Fatalf("POST /mcp = status %d body %q, want stub handler", mcp.Code, mcp.Body.String())
	}

	for _, tc := range []struct {
		path string
		body string
	}{
		{path: "/health", body: "health"},
		{path: "/feed", body: "feed"},
		{path: "/.well-known/prm", body: "prm"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if rec.Code != http.StatusOK || rec.Body.String() != tc.body {
				t.Fatalf("GET %s = status %d body %q, want stub handler body %q", tc.path, rec.Code, rec.Body.String(), tc.body)
			}
			if strings.Contains(rec.Body.String(), `<h1 id="page-title">scripts</h1>`) {
				t.Fatalf("GET %s returned landing page: status=%d body=%q", tc.path, rec.Code, rec.Body.String())
			}
		})
	}

	nope := httptest.NewRecorder()
	mux.ServeHTTP(nope, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if nope.Code == http.StatusOK && strings.Contains(nope.Body.String(), `<h1 id="page-title">scripts</h1>`) {
		t.Fatalf("GET /nope returned landing page: status=%d body=%q", nope.Code, nope.Body.String())
	}
	if nope.Code != http.StatusNotFound {
		t.Fatalf("GET /nope status = %d, want %d", nope.Code, http.StatusNotFound)
	}
}

func TestCompositionRootEnablesChassisWWWAndKeepsMCPWiring(t *testing.T) {
	// R-90AQ-66YL
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	main := string(src)

	for _, want := range []string{
		`WWW:    true,`,
		`rt.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {`,
		`rt.WWW().Render(w, "landing.html", struct{ Service, Version string }{rt.Service(), rt.Version()})`,
		`Health: scriptsHealth,`,
		`contentBase := registry.BaseURL("scripts")`,
		`rt.HandleLoopback("GET /run-content", svc.RunContentHandler())`,
		`handler, err := mcp.NewHandler(svc, contentBase, rt)`,
		`rt.Handle("POST /mcp", rt.RequireIdentity(handler))`,
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("cmd/scripts/main.go missing %q", want)
		}
	}
	webPackage := "web."
	forbidden := []string{
		`"scripts/internal/` + `web"`,
		`rt.Handle("GET /static/"`,
		webPackage + "Landing" + "Handler",
		webPackage + "Static" + "Handler",
	}
	for _, forbidden := range forbidden {
		if strings.Contains(main, forbidden) {
			t.Fatalf("cmd/scripts/main.go still contains %q", forbidden)
		}
	}

	landingLine := lineContaining(t, main, `rt.Handle("GET /{$}"`)
	if strings.Contains(landingLine, "RequireIdentity") {
		t.Fatalf("landing route is identity-gated: %s", landingLine)
	}
}

func TestNoWebEmbedsRemainOutsideExistingEmbeddedPackages(t *testing.T) {
	needle := "go:" + "embed"
	for _, root := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path == "../../internal/db" {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}
			if path == "../../internal/runner/runner.go" {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(src), needle) {
				t.Fatalf("%s still contains %s", path, needle)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
}

func TestNginxExactMatchLandingLocationPresent(t *testing.T) {
	// R-NGNX-2A5Q
	conf := readNginxConfig(t)
	exact := nginxLocationBlock(t, conf, "location = /srv/scripts/ {")
	prefix := nginxLocationBlock(t, conf, "location /srv/scripts/ {")
	if exact == prefix {
		t.Fatal("exact-match and prefix landing blocks must be distinct")
	}
}

func TestNginxLandingGatedBySessionAuthnNotBearer(t *testing.T) {
	// R-NGNX-4B7R
	block := nginxLocationBlock(t, readNginxConfig(t), "location = /srv/scripts/ {")
	if !strings.Contains(block, "auth_request /_session-authn;") {
		t.Fatalf("landing block must gate with session auth_request:\n%s", block)
	}
	if strings.Contains(block, "auth_request /_authn;") {
		t.Fatalf("landing block must not gate with bearer auth_request:\n%s", block)
	}
}

func TestNginxLandingProxiesToLoopbackRoot(t *testing.T) {
	// R-NGNX-6C9S
	block := nginxLocationBlock(t, readNginxConfig(t), "location = /srv/scripts/ {")
	want := fmt.Sprintf("proxy_pass http://127.0.0.1:%d/;", registry.MustPort("scripts"))
	if !strings.Contains(block, want) {
		t.Fatalf("landing block must proxy_pass to %q:\n%s", want, block)
	}
}

func TestNginxPreExistingLocationsSurvive(t *testing.T) {
	// R-NGNX-8D1T
	conf := readNginxConfig(t)
	prefix := nginxLocationBlock(t, conf, "location /srv/scripts/ {")
	if !strings.Contains(prefix, "auth_request /_authn;") {
		t.Fatalf("bearer-gated prefix must retain auth_request /_authn:\n%s", prefix)
	}
	for _, want := range []string{
		"location = /srv/scripts/feed { return 404; }",
		"location = /srv/scripts/.well-known/oauth-protected-resource {",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("nginx config missing %q:\n%s", want, conf)
		}
	}
}

func TestNginxStaticAssetsLocationSessionGated(t *testing.T) {
	// R-MBDE-270D
	conf := readNginxConfig(t)
	block := nginxLocationBlock(t, conf, "location /srv/scripts/static/ {")

	for _, want := range []string{
		"auth_request /_session-authn;",
		fmt.Sprintf("proxy_pass http://127.0.0.1:%d/static/;", registry.MustPort("scripts")),
		"proxy_set_header Host $host;",
		"proxy_set_header X-Forwarded-Proto $scheme;",
		"proxy_http_version 1.1;",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("static assets block missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "auth_request /_authn;") {
		t.Fatalf("static assets block must be session-gated, not bearer-gated:\n%s", block)
	}
	if !strings.Contains(conf, "location = /srv/scripts/ {") {
		t.Fatalf("exact landing location must remain present")
	}
	if prefix := nginxLocationBlock(t, conf, "location /srv/scripts/ {"); !strings.Contains(prefix, "auth_request /_authn;") {
		t.Fatalf("bearer-gated prefix changed unexpectedly:\n%s", prefix)
	}
	if !strings.Contains(conf, "location = /srv/scripts/feed { return 404; }") {
		t.Fatalf("feed denial location must remain present")
	}
	if !strings.Contains(conf, "location = /srv/scripts/.well-known/oauth-protected-resource {") {
		t.Fatalf("PRM bootstrap location must remain present")
	}
}

func TestNginxSessionGatedLocationsUseLoginBounce(t *testing.T) {
	// R-465K-NCPV
	conf := readNginxConfig(t)
	for _, opener := range []string{
		"location = /srv/scripts/ {",
		"location /srv/scripts/static/ {",
	} {
		block := nginxLocationBlock(t, conf, opener)
		for _, want := range []string{
			"auth_request /_session-authn;",
			"error_page 401 = @login_bounce;",
		} {
			if !strings.Contains(block, want) {
				t.Fatalf("session-gated block %q missing %q:\n%s", opener, want, block)
			}
		}
	}
}

func TestNginxBearerLocationDoesNotUseLoginBounce(t *testing.T) {
	// R-47DH-14GK
	block := nginxLocationBlock(t, readNginxConfig(t), "location /srv/scripts/ {")
	if !strings.Contains(block, "auth_request /_authn;") {
		t.Fatalf("bearer block must retain bearer auth_request:\n%s", block)
	}
	if strings.Contains(block, "error_page 401 = @login_bounce;") {
		t.Fatalf("bearer block must not redirect 401 responses to login:\n%s", block)
	}
}

func TestNginxLoginBounceOptInRetainsExistingLocationsAndDirectives(t *testing.T) {
	// R-49T9-SNXY
	conf := readNginxConfig(t)
	for _, want := range []string{
		"location = /srv/scripts/.well-known/oauth-protected-resource {",
		"location = /srv/scripts/feed { return 404; }",
		"location = /srv/scripts/ {",
		"location /srv/scripts/static/ {",
		"location /srv/scripts/ {",
		"location @scripts_authn_500 {",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("nginx config missing pre-existing location %q", want)
		}
	}

	for _, check := range []struct {
		opener string
		want   string
	}{
		{"location = /srv/scripts/ {", "auth_request /_session-authn;"},
		{"location = /srv/scripts/ {", "proxy_pass http://127.0.0.1:3003/;"},
		{"location /srv/scripts/static/ {", "auth_request /_session-authn;"},
		{"location /srv/scripts/static/ {", "proxy_pass http://127.0.0.1:3003/static/;"},
	} {
		block := nginxLocationBlock(t, conf, check.opener)
		if !strings.Contains(block, check.want) {
			t.Fatalf("location %q no longer contains %q:\n%s", check.opener, check.want, block)
		}
	}
}

func TestNginxBearerIdentityUsesScriptsNamedPlumbing(t *testing.T) {
	// R-4EOV-BQWQ
	conf := readNginxConfig(t)
	block := nginxLocationBlock(t, conf, "location /srv/scripts/ {")
	for _, want := range []string{
		"auth_request_set $scripts_owner         $upstream_http_x_owner_email;",
		"auth_request_set $scripts_client        $upstream_http_x_client_id;",
		"proxy_set_header X-Owner-Email   $scripts_owner;",
		"proxy_set_header X-Client-Id     $scripts_client;",
		"error_page 500 = @scripts_authn_500;",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("bearer block missing scripts identity directive %q:\n%s", want, block)
		}
	}
	if !strings.Contains(conf, "location @scripts_authn_500 {") {
		t.Fatalf("nginx config missing scripts authn re-emit location:\n%s", conf)
	}
	if !strings.Contains(block, "auth_request /_authn;") {
		t.Fatalf("bearer block must remain bearer-gated:\n%s", block)
	}
}

func TestNginxContainsNoPromptsNamedHeritage(t *testing.T) {
	// R-4FWR-PINF
	if conf := readNginxConfig(t); strings.Contains(conf, "prompts") {
		t.Fatalf("nginx config retains prompts-named heritage:\n%s", conf)
	}
}

func TestNginxUngatedPRMStripsClientCorrelationID(t *testing.T) {
	// R-ENOZ-F427
	conf := readNginxConfig(t)
	block := nginxLocationBlock(t, conf, "location = /srv/scripts/.well-known/oauth-protected-resource {")
	strip := `proxy_set_header X-Correlation-Id "";`
	if strings.Count(block, strip) != 1 {
		t.Fatalf("PRM bootstrap block must contain exactly one correlation-ID strip directive:\n%s", block)
	}
	if strings.Contains(block, "auth_request") || strings.Contains(block, "upstream_http_x_correlation_id") {
		t.Fatalf("ungated PRM bootstrap block must not contain correlation auth plumbing:\n%s", block)
	}
	if strings.Count(conf, strip) != 1 {
		t.Fatalf("correlation-ID empty-string strip must occur only in the PRM bootstrap block; got %d occurrences", strings.Count(conf, strip))
	}
}

func TestNginxGatedLocationsCaptureAndForwardDistinctCorrelationIDs(t *testing.T) {
	// R-EOWV-SVSW
	conf := readNginxConfig(t)
	tiers := []struct {
		opener   string
		variable string
	}{
		{"location /srv/scripts/ {", "$scripts_corr"},
		{"location = /srv/scripts/ {", "$scripts_session_corr"},
		{"location /srv/scripts/static/ {", "$scripts_static_corr"},
	}
	for _, tier := range tiers {
		block := nginxLocationBlock(t, conf, tier.opener)
		capture := "auth_request_set " + tier.variable + " $upstream_http_x_correlation_id;"
		forward := "proxy_set_header X-Correlation-Id " + tier.variable + ";"
		if strings.Count(block, capture) != 1 || strings.Count(block, forward) != 1 {
			t.Errorf("gated block %q must capture and forward %s exactly once:\n%s", tier.opener, tier.variable, block)
		}
		for _, other := range tiers {
			if other.variable != tier.variable && strings.Contains(block, other.variable) {
				t.Errorf("gated block %q crosses correlation variable %s into tier for %s:\n%s", tier.opener, other.variable, tier.variable, block)
			}
		}
	}
	if got := strings.Count(conf, "proxy_set_header X-Correlation-Id"); got != 4 {
		t.Errorf("correlation header directive count = %d, want 4", got)
	}
	if got := strings.Count(conf, "upstream_http_x_correlation_id"); got != 3 {
		t.Errorf("correlation auth capture count = %d, want 3", got)
	}
}

func TestNginxBearerForwardsAllOwnerIdentityHeaders(t *testing.T) {
	// R-LXR5-KN5G
	block := nginxLocationBlock(t, readNginxConfig(t), "location /srv/scripts/ {")
	assertNginxIdentityDirectives(t, block, []string{
		"auth_request_set $scripts_owner         $upstream_http_x_owner_email;",
		"auth_request_set $scripts_owner_id      $upstream_http_x_owner_id;",
		"auth_request_set $scripts_owner_name    $upstream_http_x_owner_name;",
		"auth_request_set $scripts_owner_picture $upstream_http_x_owner_picture;",
		"auth_request_set $scripts_client        $upstream_http_x_client_id;",
		"proxy_set_header X-Owner-Id      $scripts_owner_id;",
		"proxy_set_header X-Owner-Email   $scripts_owner;",
		"proxy_set_header X-Owner-Name    $scripts_owner_name;",
		"proxy_set_header X-Owner-Picture $scripts_owner_picture;",
		"proxy_set_header X-Client-Id     $scripts_client;",
	})
}

func TestNginxSessionForwardsAllOwnerIdentityHeadersWithoutClientID(t *testing.T) {
	// R-LYZ1-YEW5
	block := nginxLocationBlock(t, readNginxConfig(t), "location = /srv/scripts/ {")
	assertNginxIdentityDirectives(t, block, []string{
		"auth_request_set $scripts_session_owner         $upstream_http_x_owner_email;",
		"auth_request_set $scripts_session_owner_id      $upstream_http_x_owner_id;",
		"auth_request_set $scripts_session_owner_name    $upstream_http_x_owner_name;",
		"auth_request_set $scripts_session_owner_picture $upstream_http_x_owner_picture;",
		"proxy_set_header X-Owner-Id      $scripts_session_owner_id;",
		"proxy_set_header X-Owner-Email   $scripts_session_owner;",
		"proxy_set_header X-Owner-Name    $scripts_session_owner_name;",
		"proxy_set_header X-Owner-Picture $scripts_session_owner_picture;",
	})
	if strings.Contains(block, "X-Client-Id") || strings.Contains(block, "x_client_id") {
		t.Fatalf("session block must not capture or forward bearer client identity:\n%s", block)
	}
}

// R-34EZ-J9BF
func TestServeRejectsRunTTLAboveSixHoursAndAcceptsBoundary(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "scripts")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build scripts: %v\n%s", err, out)
	}
	www, err := filepath.Abs(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatalf("resolve www path: %v", err)
	}
	dropbox := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(dropbox.Close)
	feedServers := make(map[string]*httptest.Server)
	baseEnv := map[string]string{
		"IKIGENBA_DOMAIN":           "int.ikigenba.com",
		"SCRIPTS_IP":                "127.0.0.1",
		"SCRIPTS_WWW_PATH":          www,
		"DROPBOX_BASE_URL":          dropbox.URL,
		"OUTBOX_RETENTION_DAYS":     "7",
		"OUTBOX_RETENTION_MAX_ROWS": "1000000",
	}
	for _, entry := range scriptsSpec().Consumers {
		source := entry.Source
		feedServers[source] = newIdleFeedServer(t)
		baseEnv["SCRIPTS_"+strings.ToUpper(source)+"_FEED_URL"] = feedServers[source].URL + "/feed"
	}

	rejectedPort := freeTCPPort(t)
	rejectedEnv := mapsClone(baseEnv)
	rejectedEnv["IKIGENBA_ROOT"] = newServeRoot(t)
	rejectedEnv["SCRIPTS_PORT"] = strconv.Itoa(rejectedPort)
	rejectedEnv["SCRIPTS_RUN_TTL"] = "7h"
	rejected := exec.Command(binary, "serve")
	rejected.Env = testEnv(rejectedEnv)
	output, err := rejected.CombinedOutput()
	if err == nil {
		t.Fatalf("serve with SCRIPTS_RUN_TTL=7h exited zero; output:\n%s", output)
	}
	message := string(output)
	for _, want := range []string{"SCRIPTS_RUN_TTL", "7h", "6h"} {
		if !strings.Contains(message, want) {
			t.Errorf("startup error missing %q:\n%s", want, message)
		}
	}
	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", rejectedPort), 100*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		t.Fatal("serve with rejected TTL started a listener")
	}

	acceptedPort := freeTCPPort(t)
	acceptedEnv := mapsClone(baseEnv)
	acceptedEnv["IKIGENBA_ROOT"] = newServeRoot(t)
	acceptedEnv["SCRIPTS_PORT"] = strconv.Itoa(acceptedPort)
	acceptedEnv["SCRIPTS_RUN_TTL"] = "6h"
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	accepted := exec.CommandContext(ctx, binary, "serve")
	accepted.Env = testEnv(acceptedEnv)
	accepted.Stdout = &stdout
	accepted.Stderr = &stderr
	if err := accepted.Start(); err != nil {
		cancel()
		t.Fatalf("start scripts at 6h boundary: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- accepted.Wait() }()
	defer stopProcess(cancel, done)
	waitForHealth(t, acceptedPort, done, &stdout, &stderr)
}

func mapsClone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func newServeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"state", "cache"} {
		path := filepath.Join(root, "scripts", name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("create serve %s directory: %v", name, err)
		}
	}
	return root
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
	copyTree(t, wwwRoot(t), filepath.Join(shareVersionDir, "www"))
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
		"SCRIPTS_WWW_PATH":          filepath.Join(shareDir, "current", "www"),
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

type consumerTestPlane struct{}

func (consumerTestPlane) Create(context.Context, string, script.Owner, string) error { return nil }
func (consumerTestPlane) Commit(context.Context, string, map[string]string, string, string) (string, error) {
	return "sha", nil
}
func (consumerTestPlane) Head(context.Context, string, string) (string, error) { return "sha", nil }
func (consumerTestPlane) ReadFile(context.Context, string, string, string) ([]byte, error) {
	return nil, nil
}

func (consumerTestPlane) Rename(context.Context, string, string, script.Owner, string) error {
	return nil
}
func (consumerTestPlane) Delete(context.Context, string, script.Owner, string) error { return nil }
func (consumerTestPlane) RunToken(context.Context, string, time.Duration) (string, string, error) {
	return "token", "clone", nil
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
	conn, err := appkitdatabase.Open(filepath.Join(t.TempDir(), "scripts.db"))
	if err != nil {
		t.Fatalf("appkitdatabase.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := appkitdatabase.LoadMigrations(scriptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("appkitdatabase.LoadMigrations: %v", err)
	}
	if err := appkitdatabase.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("appkitdatabase.Migrate: %v", err)
	}
	runner := &consumerTestRunner{spawns: make(chan consumerTestSpawn, 2)}
	svc := script.NewService(script.NewStore(conn), t.TempDir(), runner)
	svc.Plane = consumerTestPlane{}
	return svc, runner
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

func newWWWTestHandler(t *testing.T) http.Handler {
	t.Helper()
	var rt *appkit.Router
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/scripts/",
		AuthServer: "https://int.ikigenba.com/",
		Version:    "test",
		Service:    "scripts",
		WWW:        loadWWW(t),
		Register: func(r *appkit.Router) error {
			rt = r
			r.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if err := r.WWW().Render(w, "landing.html", landingData(r.Service(), r.Version())); err != nil {
					return
				}
			}))
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New with WWW: %v", err)
	}
	if rt == nil {
		t.Fatal("server.New with WWW did not call Register")
	}
	if srv.Handler == nil {
		t.Fatal("server.New with WWW returned a nil handler")
	}
	return srv.Handler
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

func loadWWW(t *testing.T) *appweb.Site {
	t.Helper()
	site, err := appweb.Load(wwwRoot(t))
	if err != nil {
		t.Fatalf("load share/www: %v", err)
	}
	return site
}

func wwwRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatalf("resolve share/www: %v", err)
	}
	return root
}

func renderLanding(t *testing.T, service, version string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := loadWWW(t).Render(rec, "landing.html", landingData(service, version)); err != nil {
		t.Fatalf("render landing.html: %v", err)
	}
	return rec
}

func landingHandler(site *appweb.Site, service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := site.Render(w, "landing.html", landingData(service, version)); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}

func landingData(service, version string) struct {
	Service string
	Version string
} {
	return struct {
		Service string
		Version string
	}{
		Service: service,
		Version: version,
	}
}

func readWWWStatic(t *testing.T, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	loadWWW(t).Static().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d\n%s", path, rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func lineContaining(t *testing.T, text, needle string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line contains %q", needle)
	return ""
}

func linkMarkupWithHref(t *testing.T, html, href string) string {
	t.Helper()
	hrefPos := strings.Index(html, `href="`+href+`"`)
	if hrefPos == -1 {
		t.Fatalf("HTML missing link href %q:\n%s", href, html)
	}
	start := strings.LastIndex(html[:hrefPos], "<link")
	if start == -1 {
		t.Fatalf("href %q is not inside a link tag:\n%s", href, html)
	}
	endRel := strings.Index(html[hrefPos:], ">")
	if endRel == -1 {
		t.Fatalf("link for href %q has no closing >:\n%s", href, html)
	}
	return html[start : hrefPos+endRel+1]
}

func readNginxConfig(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func nginxLocationBlock(t *testing.T, conf, opener string) string {
	t.Helper()
	start := strings.Index(conf, opener)
	if start == -1 {
		t.Fatalf("nginx config missing %q", opener)
	}
	bodyStart := start + len(opener)
	endRel := strings.Index(conf[bodyStart:], "\n}")
	if endRel == -1 {
		t.Fatalf("nginx config location %q has no closing brace", opener)
	}
	return conf[start : bodyStart+endRel+len("\n}")]
}

func assertNginxIdentityDirectives(t *testing.T, block string, directives []string) {
	t.Helper()
	for _, directive := range directives {
		if count := strings.Count(block, directive); count != 1 {
			t.Fatalf("nginx location directive %q count = %d, want exactly 1:\n%s", directive, count, block)
		}
	}
	for _, header := range []string{"X-Owner-Id", "X-Owner-Email", "X-Owner-Name", "X-Owner-Picture"} {
		if count := strings.Count(block, "proxy_set_header "+header); count != 1 {
			t.Fatalf("nginx location proxy_set_header %s count = %d, want exactly 1:\n%s", header, count, block)
		}
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, info.Mode())
	})
	if err != nil {
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
}
