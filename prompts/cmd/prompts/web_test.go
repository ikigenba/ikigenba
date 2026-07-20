package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appkitdb "appkit/db"
	appserver "appkit/server"
	appweb "appkit/web"

	"prompts/internal/calls"
	promptsdb "prompts/internal/db"
	"prompts/internal/prompt"
)

func TestPromptsSpecEnablesChassisWWWFromShareTree(t *testing.T) {
	// R-DIAW-ZFMC
	if spec := promptsSpec(); !spec.WWW {
		t.Fatal("promptsSpec().WWW = false, want true")
	}

	site := loadPromptsSite(t)
	rec := renderUITemplate(t, site, "ui-prompts.html", promptsPageData{uiChrome: uiChrome{Service: "prompts-canary", Version: "v2036.01.02"}, Page: 1, Pages: 1})
	if got, want := rec.Header().Get("Content-Type"), "text/html; charset=utf-8"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if body := rec.Body.String(); !strings.Contains(body, "prompts-canary") || !strings.Contains(body, "v2036.01.02") {
		t.Fatalf("UI render did not use chassis-loaded share/www template:\n%s", body)
	}

	staticRec := httptest.NewRecorder()
	site.Static().ServeHTTP(staticRec, httptest.NewRequest(http.MethodGet, "/static/tokens.css", nil))
	if staticRec.Code != http.StatusOK {
		t.Fatalf("site.Static tokens.css status = %d, want %d", staticRec.Code, http.StatusOK)
	}

	dirRec := httptest.NewRecorder()
	site.Static().ServeHTTP(dirRec, httptest.NewRequest(http.MethodGet, "/static/fonts/", nil))
	if dirRec.Code != http.StatusNotFound {
		t.Fatalf("site.Static directory status = %d, want %d", dirRec.Code, http.StatusNotFound)
	}
}

func TestStartExportsPromptsWWWPath(t *testing.T) {
	// R-DJIT-D7D1
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "bin", "start"))
	if err != nil {
		t.Fatalf("read bin/start: %v", err)
	}
	block := shellFunctionBlock(t, string(data), "launch_prompts()")
	want := `export PROMPTS_WWW_PATH="$repo/prompts/share/www"`
	if !strings.Contains(block, want) {
		t.Fatalf("launch_prompts missing %q:\n%s", want, block)
	}
}

func TestLandingHandlerRendersInjectedNameAndVersion(t *testing.T) {
	// R-LAND-NMVR
	rec := serveUI(t, newUIHandler(t, nil, nil), "/ui/")

	body := rec.Body.String()
	for _, want := range []string{"prompts-test", "v45-test"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestLandingHandlerRendersHomeLinkToDashboardApex(t *testing.T) {
	// R-HOME-2T4X
	for _, path := range []string{"/ui/", "/ui/runs"} {
		body := serveUI(t, newUIHandler(t, nil, nil), path).Body.String()
		if !strings.Contains(body, `<a class="home" href="/">Home</a>`) {
			t.Fatalf("body for %s missing Home link:\n%s", path, body)
		}
	}
}

func TestLandingAssetsAreLoadedFromShareWWWAndServed(t *testing.T) {
	// R-LAND-CARB
	www := promptsWWWPath()
	if _, err := os.Stat(filepath.Join(www, "static", "tokens.css")); err != nil {
		t.Fatalf("share/www tokens.css missing: %v", err)
	}
	fonts, err := fs.Glob(os.DirFS(www), "static/fonts/*.woff2")
	if err != nil {
		t.Fatalf("glob fonts: %v", err)
	}
	if len(fonts) == 0 {
		t.Fatal("share/www woff2 fonts missing")
	}

	pageRec := serveUI(t, newUIHandler(t, nil, nil), "/ui/")
	if !strings.Contains(pageRec.Body.String(), `href="/srv/prompts/static/tokens.css"`) {
		t.Fatalf("UI page does not reference tokens.css:\n%s", pageRec.Body.String())
	}

	staticRec := httptest.NewRecorder()
	loadPromptsSite(t).Static().ServeHTTP(staticRec, httptest.NewRequest(http.MethodGet, "/static/tokens.css", nil))

	if staticRec.Code != http.StatusOK {
		t.Fatalf("tokens.css status = %d, want %d", staticRec.Code, http.StatusOK)
	}
	if got := staticRec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Fatalf("tokens.css content type = %q, want text/css", got)
	}
}

func TestLandingMuxMatchesExactRootOnly(t *testing.T) {
	// R-LAND-ROOT
	// R-ZW7P-88WL
	h := newUIHandler(t, nil, nil)
	root := serveUI(t, h, "/")
	if root.Code != http.StatusSeeOther || root.Header().Get("Location") != "ui/" {
		t.Fatalf("GET / = %d Location %q, want 303 and relative ui/", root.Code, root.Header().Get("Location"))
	}

	for _, path := range []string{"/health", "/mcp", "/nope"} {
		rec := serveUI(t, h, path)
		if rec.Code == http.StatusSeeOther && rec.Header().Get("Location") == "ui/" {
			t.Fatalf("path %s received root redirect", path)
		}
	}
}

func TestLandingIsUngated(t *testing.T) {
	// R-LAND-UNGT
	h := newUIHandler(t, nil, nil)
	root := serveUI(t, h, "/")
	if root.Code != http.StatusSeeOther {
		t.Fatalf("root status without identity or bearer = %d, want %d", root.Code, http.StatusSeeOther)
	}

	// R-ZYNH-ZSDZ
	rec := serveUI(t, h, "/ui/")
	if rec.Code != http.StatusOK {
		t.Fatalf("UI status without identity or bearer = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestTokensCSSUsesOptionalRelativeFontSources(t *testing.T) {
	staticRec := httptest.NewRecorder()
	loadPromptsSite(t).Static().ServeHTTP(staticRec, httptest.NewRequest(http.MethodGet, "/static/tokens.css", nil))

	if staticRec.Code != http.StatusOK {
		t.Fatalf("tokens.css status = %d, want %d", staticRec.Code, http.StatusOK)
	}
	css := staticRec.Body.String()

	// R-DFKP-IVZU
	if strings.Contains(css, "font-display: swap") {
		t.Fatalf("tokens.css still contains font-display swap:\n%s", css)
	}
	if got, want := strings.Count(css, "@font-face"), strings.Count(css, "font-display: optional"); got != want {
		t.Fatalf("font-display optional count = %d, want one per %d @font-face blocks", want, got)
	}

	// R-DGSL-WNQJ
	if strings.Contains(css, "url('/static/fonts/") {
		t.Fatalf("tokens.css still contains origin-absolute font URLs:\n%s", css)
	}
	for _, want := range []string{
		"url('fonts/space-grotesk.woff2')",
		"url('fonts/ibm-plex-sans.woff2')",
		"url('fonts/ibm-plex-mono-400.woff2')",
		"url('fonts/ibm-plex-mono-500.woff2')",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("tokens.css missing relative font source %q:\n%s", want, css)
		}
	}
}

func TestLandingHeadUsesRelativeTokensAndPreloadsFonts(t *testing.T) {
	// R-DI0I-AFH8
	// R-DJ8E-O77X
	for _, path := range []string{"/ui/", "/ui/runs"} {
		rec := serveUI(t, newUIHandler(t, nil, nil), path)
		head := renderedHead(t, rec.Body.String())
		if !strings.Contains(head, `href="/srv/prompts/static/tokens.css"`) {
			t.Fatalf("%s head missing mount-literal tokens.css link:\n%s", path, head)
		}
		for _, font := range []string{"space-grotesk.woff2", "ibm-plex-sans.woff2"} {
			want := `<link rel="preload" as="font" type="font/woff2" crossorigin href="/srv/prompts/static/fonts/` + font + `">`
			if !strings.Contains(head, want) {
				t.Fatalf("%s head missing font preload %q:\n%s", path, want, head)
			}
		}
	}
}

func TestPromptsTabRendersRowsNewestFirstAndLinksDetails(t *testing.T) {
	// R-04QZ-WN3G
	rows := []prompt.Prompt{
		{ID: "p-old", Name: "Older prompt", OwnerEmail: "old@example.com", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z"},
		{ID: "p-new", Name: "Newest prompt", OwnerEmail: "new@example.com", CreatedAt: "2026-02-01T00:00:00Z", UpdatedAt: "2026-02-03T00:00:00Z"},
	}
	body := serveUI(t, newUIHandler(t, rows, nil), "/ui/").Body.String()
	for _, row := range rows {
		for _, want := range []string{row.Name, row.OwnerEmail, row.CreatedAt, row.UpdatedAt, `/srv/prompts/ui/prompts/` + row.ID} {
			if !strings.Contains(body, want) {
				t.Fatalf("body missing %q:\n%s", want, body)
			}
		}
	}
	if strings.Index(body, "Newest prompt") > strings.Index(body, "Older prompt") {
		t.Fatalf("prompts not newest-updated first:\n%s", body)
	}
	for _, want := range []string{`class="active" aria-current="page" href="/srv/prompts/ui/">Prompts</a>`, `href="/srv/prompts/ui/runs">Runs</a>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompts nav missing %q:\n%s", want, body)
		}
	}
}

func TestPromptsTabFiltersServerSideByNameAndOwner(t *testing.T) {
	// R-05YW-AEU5
	rows := []prompt.Prompt{
		{ID: "p1", Name: "Nightly Alpha", OwnerEmail: "one@example.com", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		{ID: "p2", Name: "Daily Beta", OwnerEmail: "special-owner@example.com", CreatedAt: "2026-01-02T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z"},
	}
	for _, tc := range []struct{ query, present, absent string }{{"alpha", "Nightly Alpha", "Daily Beta"}, {"SPECIAL-owner", "Daily Beta", "Nightly Alpha"}} {
		body := serveUI(t, newUIHandler(t, rows, nil), "/ui/?q="+tc.query).Body.String()
		if !strings.Contains(body, tc.present) || strings.Contains(body, tc.absent) {
			t.Fatalf("q=%q did not filter rendered HTML:\n%s", tc.query, body)
		}
	}
}

func TestPromptsTabPaginatesFiftyAndPreservesFilter(t *testing.T) {
	// R-076S-O6KU
	rows := make([]prompt.Prompt, 60)
	for i := range rows {
		stamp := fmt.Sprintf("2026-01-01T00:%02d:00Z", i)
		rows[i] = prompt.Prompt{ID: fmt.Sprintf("batch-%02d", i), Name: fmt.Sprintf("Batch prompt %02d", i), OwnerEmail: "batch@example.com", CreatedAt: stamp, UpdatedAt: stamp}
	}
	h := newUIHandler(t, rows, nil)
	first := serveUI(t, h, "/ui/?q=batch")
	if got := strings.Count(first.Body.String(), `data-prompt-id=`); got != 50 {
		t.Fatalf("page 1 rows = %d, want 50", got)
	}
	for _, want := range []string{"Page 1 of 2", `href="/srv/prompts/ui/?page=2&amp;q=batch"`} {
		if !strings.Contains(first.Body.String(), want) {
			t.Fatalf("page 1 missing %q:\n%s", want, first.Body.String())
		}
	}
	second := serveUI(t, h, "/ui/?q=batch&page=2")
	if got := strings.Count(second.Body.String(), `data-prompt-id=`); got != 10 {
		t.Fatalf("page 2 rows = %d, want 10", got)
	}
	if !strings.Contains(second.Body.String(), `href="/srv/prompts/ui/?page=1&amp;q=batch"`) {
		t.Fatalf("Prev does not preserve q:\n%s", second.Body.String())
	}
}

func TestRunsTabRendersRowsOrderDurationTriggerAndLinks(t *testing.T) {
	// R-08EP-1YBJ
	runs := []prompt.Run{
		{ID: "r-old", PromptID: "p-old", PromptName: "Manual older", Status: "running", OwnerEmail: "old@example.com", StartedAt: "2026-01-01T00:00:00Z"},
		{ID: "r-new", PromptID: "p-new", PromptName: "Event newest", Status: "succeeded", OwnerEmail: "new@example.com", StartedAt: "2026-01-02T00:00:00Z", EndedAt: "2026-01-02T00:01:30Z", TriggerSource: "cron", TriggerKind: "tick"},
	}
	body := serveUI(t, newUIHandler(t, nil, runs), "/ui/runs").Body.String()
	for _, want := range []string{"Event newest", "Manual older", "succeeded", "running", "new@example.com", "2026-01-02T00:00:00Z", "1m30s", "cron / tick", `/srv/prompts/ui/prompts/p-new`, `/srv/prompts/ui/runs/r-new`} {
		if !strings.Contains(body, want) {
			t.Fatalf("runs body missing %q:\n%s", want, body)
		}
	}
	if strings.Index(body, "Event newest") > strings.Index(body, "Manual older") {
		t.Fatalf("runs not newest-started first:\n%s", body)
	}
	oldRow := body[strings.Index(body, `data-run-id="r-old"`):]
	oldRow = oldRow[:strings.Index(oldRow, "</tr>")]
	if strings.Contains(oldRow, "cron") || strings.Contains(oldRow, "1m30s") {
		t.Fatalf("running manual row has duration or trigger:\n%s", oldRow)
	}
}

func TestRunsTabFiltersStatusNameAndOwnerServerSide(t *testing.T) {
	// R-09ML-FQ28
	runs := []prompt.Run{
		{ID: "r1", PromptID: "p1", PromptName: "Alpha Job", Status: "failed", OwnerEmail: "first@example.com", StartedAt: "2026-01-02T00:00:00Z"},
		{ID: "r2", PromptID: "p2", PromptName: "Beta Job", Status: "succeeded", OwnerEmail: "special@example.com", StartedAt: "2026-01-01T00:00:00Z"},
	}
	h := newUIHandler(t, nil, runs)
	for _, tc := range []struct{ path, present, absent string }{{"/ui/runs?status=failed", "Alpha Job", "Beta Job"}, {"/ui/runs?q=beta", "Beta Job", "Alpha Job"}, {"/ui/runs?q=SPECIAL", "Beta Job", "Alpha Job"}} {
		body := serveUI(t, h, tc.path).Body.String()
		if !strings.Contains(body, tc.present) || strings.Contains(body, tc.absent) {
			t.Fatalf("%s did not filter rendered HTML:\n%s", tc.path, body)
		}
	}
}

func TestRunsTabFiltersByPromptID(t *testing.T) {
	// R-0AUH-THSX
	runs := []prompt.Run{
		{ID: "r1", PromptID: "p1", PromptName: "Wanted prompt", Status: "failed", OwnerEmail: "one@example.com", StartedAt: "2026-01-02T00:00:00Z"},
		{ID: "r2", PromptID: "p2", PromptName: "Other prompt", Status: "failed", OwnerEmail: "two@example.com", StartedAt: "2026-01-01T00:00:00Z"},
	}
	body := serveUI(t, newUIHandler(t, nil, runs), "/ui/runs?prompt_id=p1").Body.String()
	if !strings.Contains(body, "Wanted prompt") || strings.Contains(body, "Other prompt") {
		t.Fatalf("prompt_id filter did not constrain HTML:\n%s", body)
	}
}

func TestNginxStaticLocationUsesSessionAuth(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx conf: %v", err)
	}
	text := string(conf)
	block := nginxLocationBlock(t, text, "location /srv/prompts/static/ {")

	// R-DKGB-1YYM
	for _, want := range []string{
		"auth_request /_session-authn;",
		"proxy_pass http://127.0.0.1:3002/static/;",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("static location missing %q:\n%s", want, block)
		}
	}
	for _, want := range []string{
		"location = /srv/prompts/.well-known/oauth-protected-resource",
		"location = /srv/prompts/",
		"location = /srv/prompts/feed { return 404; }",
		"location /srv/prompts/ {",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("nginx conf missing existing location %q:\n%s", want, text)
		}
	}
}

func TestNginxBrowseUILocationUsesSessionGateAndOwnerHeaders(t *testing.T) {
	// R-ZXFL-M0NA
	conf, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx conf: %v", err)
	}
	block := nginxLocationBlock(t, string(conf), "location /srv/prompts/ui/ {")
	for _, want := range []string{
		"auth_request /_session-authn;",
		"error_page 401 = @login_bounce;",
		"auth_request_set $prompts_owner         $upstream_http_x_owner_email;",
		"auth_request_set $prompts_owner_id      $upstream_http_x_owner_id;",
		"auth_request_set $prompts_owner_name    $upstream_http_x_owner_name;",
		"auth_request_set $prompts_owner_picture $upstream_http_x_owner_picture;",
		"proxy_set_header X-Owner-Email   $prompts_owner;",
		"proxy_set_header X-Owner-Id      $prompts_owner_id;",
		"proxy_set_header X-Owner-Name    $prompts_owner_name;",
		"proxy_set_header X-Owner-Picture $prompts_owner_picture;",
		"proxy_pass http://127.0.0.1:3002/ui/;",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("browse UI location missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "X-Client-Id") {
		t.Fatalf("browse UI location forwards client identity:\n%s", block)
	}
}

func TestNginxSessionLocationsBounceLogin(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx conf: %v", err)
	}
	text := string(conf)

	// R-3RIS-23TJ
	for _, marker := range []string{
		"location = /srv/prompts/ {",
		"location /srv/prompts/static/ {",
	} {
		block := nginxLocationBlock(t, text, marker)
		for _, want := range []string{
			"auth_request /_session-authn;",
			"error_page 401 = @login_bounce;",
		} {
			if !strings.Contains(block, want) {
				t.Fatalf("session location %q missing %q:\n%s", marker, want, block)
			}
		}
	}
}

func TestNginxBearerLocationDoesNotBounceLogin(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx conf: %v", err)
	}
	block := nginxLocationBlock(t, string(conf), "location /srv/prompts/ {")

	// R-3SQO-FVK8
	if !strings.Contains(block, "auth_request /_authn;") {
		t.Fatalf("bearer location is missing auth_request /_authn;:\n%s", block)
	}
	if strings.Contains(block, "error_page 401 = @login_bounce;") {
		t.Fatalf("bearer location unexpectedly bounces login:\n%s", block)
	}
}

func TestNginxBearerLocationForwardsAllOwnerIdentityHeaders(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx conf: %v", err)
	}
	block := nginxLocationBlock(t, string(conf), "location /srv/prompts/ {")

	// R-7NY0-UIO6
	for _, want := range []string{
		"auth_request /_authn;",
		"auth_request_set $prompts_owner         $upstream_http_x_owner_email;",
		"auth_request_set $prompts_owner_id      $upstream_http_x_owner_id;",
		"auth_request_set $prompts_owner_name    $upstream_http_x_owner_name;",
		"auth_request_set $prompts_owner_picture $upstream_http_x_owner_picture;",
		"proxy_set_header X-Owner-Email   $prompts_owner;",
		"proxy_set_header X-Owner-Id      $prompts_owner_id;",
		"proxy_set_header X-Owner-Name    $prompts_owner_name;",
		"proxy_set_header X-Owner-Picture $prompts_owner_picture;",
		"proxy_set_header X-Client-Id     $prompts_client;",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("bearer location missing %q:\n%s", want, block)
		}
	}
}

func TestNginxSessionLandingForwardsAllOwnerIdentityHeaders(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx conf: %v", err)
	}
	block := nginxLocationBlock(t, string(conf), "location = /srv/prompts/ {")

	// R-7P5X-8AEV
	for _, want := range []string{
		"auth_request /_session-authn;",
		"auth_request_set $prompts_owner         $upstream_http_x_owner_email;",
		"auth_request_set $prompts_owner_id      $upstream_http_x_owner_id;",
		"auth_request_set $prompts_owner_name    $upstream_http_x_owner_name;",
		"auth_request_set $prompts_owner_picture $upstream_http_x_owner_picture;",
		"proxy_set_header X-Owner-Email   $prompts_owner;",
		"proxy_set_header X-Owner-Id      $prompts_owner_id;",
		"proxy_set_header X-Owner-Name    $prompts_owner_name;",
		"proxy_set_header X-Owner-Picture $prompts_owner_picture;",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("session landing location missing %q:\n%s", want, block)
		}
	}
}

func TestNginxLoginBounceOptInRetainsExistingLocations(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx conf: %v", err)
	}
	text := string(conf)

	// R-3TYK-TNAX
	for _, want := range []string{
		"location = /srv/prompts/.well-known/oauth-protected-resource {",
		"location = /srv/prompts/feed { return 404; }",
		"location = /srv/prompts/ {",
		"location /srv/prompts/static/ {",
		"location /srv/prompts/ {",
		"location @prompts_authn_500 {",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("nginx conf missing existing location %q:\n%s", want, text)
		}
	}
	for _, location := range []struct {
		marker    string
		proxyPass string
	}{
		{"location = /srv/prompts/ {", "proxy_pass http://127.0.0.1:3002/;"},
		{"location /srv/prompts/static/ {", "proxy_pass http://127.0.0.1:3002/static/;"},
	} {
		block := nginxLocationBlock(t, text, location.marker)
		for _, want := range []string{"auth_request /_session-authn;", location.proxyPass} {
			if !strings.Contains(block, want) {
				t.Fatalf("session location %q missing retained directive %q:\n%s", location.marker, want, block)
			}
		}
	}
}

func loadPromptsSite(t *testing.T) *appweb.Site {
	t.Helper()
	site, err := appweb.Load(promptsWWWPath())
	if err != nil {
		t.Fatalf("web.Load share/www: %v", err)
	}
	return site
}

func promptsWWWPath() string {
	return filepath.Join("..", "..", "share", "www")
}

func renderUITemplate(t *testing.T, site *appweb.Site, name string, data any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := site.Render(rec, name, data); err != nil {
		t.Fatalf("Render %s: %v", name, err)
	}
	return rec
}

func newUIHandler(t *testing.T, prompts []prompt.Prompt, runs []prompt.Run) http.Handler {
	t.Helper()
	ctx := context.Background()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := appkitdb.LoadMigrations(promptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(ctx, conn, migrations); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	store := prompt.NewStore(conn)
	callStore := calls.NewStore(conn)
	for _, row := range prompts {
		if err := store.InsertPrompt(ctx, row); err != nil {
			t.Fatalf("insert prompt %s: %v", row.ID, err)
		}
	}
	for _, row := range runs {
		if err := store.InsertRun(ctx, row); err != nil {
			t.Fatalf("insert run %s: %v", row.ID, err)
		}
	}
	srv, err := appserver.New(appserver.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/prompts/",
		AuthServer: "https://example.test/",
		Version:    "v45-test",
		Service:    "prompts-test",
		WWW:        loadPromptsSite(t),
		DB:         conn,
		Register: func(rt *appserver.Router) error {
			registerUIRoutes(rt, store, callStore)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return srv.Handler
}

func serveUI(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func renderedHead(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, "<head>")
	end := strings.Index(html, "</head>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("rendered page missing head:\n%s", html)
	}
	return html[start : end+len("</head>")]
}

func nginxLocationBlock(t *testing.T, conf, marker string) string {
	t.Helper()
	start := strings.Index(conf, marker)
	if start < 0 {
		t.Fatalf("nginx conf missing %q:\n%s", marker, conf)
	}
	rest := conf[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("nginx conf location %q is not closed:\n%s", marker, rest)
	}
	return rest[:end+len("\n}")]
}

func shellFunctionBlock(t *testing.T, text, marker string) string {
	t.Helper()
	start := strings.Index(text, marker)
	if start < 0 {
		t.Fatalf("shell file missing %q", marker)
	}
	rest := text[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("shell function %q is not closed:\n%s", marker, rest)
	}
	return rest[:end+len("\n}")]
}
