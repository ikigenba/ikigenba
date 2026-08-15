package main

import (
	"appkit/manifest"
	"appkit/server"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github/internal/githubapp"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	appweb "appkit/web"
)

func TestTemporaryInstallLayoutBootsAndServesHealth(t *testing.T) {
	// R-4LKF-FB23
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(moduleRoot, "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read authored manifest: %v", err)
	}
	versionBytes, err := os.ReadFile(filepath.Join(moduleRoot, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	version := strings.TrimSpace(string(versionBytes))
	if version == "" {
		t.Fatal("VERSION is empty")
	}

	root := t.TempDir()
	installRoot := filepath.Join(root, "github")
	stateDir := filepath.Join(installRoot, "state")
	cacheDir := filepath.Join(installRoot, "cache")
	libexecDir := filepath.Join(installRoot, "libexec")
	binDir := filepath.Join(installRoot, "bin")
	versionEtcDir := filepath.Join(installRoot, "etc", version)
	versionShareDir := filepath.Join(installRoot, "share", version)
	for _, dir := range []string{stateDir, cacheDir, libexecDir, binDir, versionEtcDir, versionShareDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create install directory %s: %v", dir, err)
		}
	}

	binary := filepath.Join(libexecDir, "github-"+version)
	build := exec.Command("go", "build", "-o", binary, "./cmd/github")
	build.Dir = moduleRoot
	build.Env = withEnv(os.Environ(), map[string]string{"GOWORK": "off"})
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build github binary: %v\n%s", err, output)
	}

	run := filepath.Join(binDir, "run")
	if err := os.Symlink(filepath.Join("..", "libexec", filepath.Base(binary)), run); err != nil {
		t.Fatalf("create bin/run symlink: %v", err)
	}
	installedManifest := filepath.Join(versionEtcDir, "manifest.env")
	if err := os.WriteFile(installedManifest, manifestBytes, 0o644); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	current := filepath.Join(installRoot, "etc", "current")
	if err := os.Symlink(version, current); err != nil {
		t.Fatalf("create etc/current symlink: %v", err)
	}
	copyTree(t, filepath.Join(moduleRoot, "share", "www"), filepath.Join(versionShareDir, "www"))
	shareCurrent := filepath.Join(installRoot, "share", "current")
	if err := os.Symlink(version, shareCurrent); err != nil {
		t.Fatalf("create share/current symlink: %v", err)
	}

	assertSymlinkResolvesTo(t, run, binary)
	assertSymlinkResolvesTo(t, current, versionEtcDir)
	assertSymlinkResolvesTo(t, shareCurrent, versionShareDir)
	installedBytes, err := os.ReadFile(filepath.Join(current, "manifest.env"))
	if err != nil {
		t.Fatalf("read installed current manifest: %v", err)
	}
	if !bytes.Equal(installedBytes, manifestBytes) {
		t.Fatalf("installed manifest differs from authored bytes\ngot:\n%s\nwant:\n%s", installedBytes, manifestBytes)
	}

	privateKey := throwawayPrivateKeyPEM(t)
	port := freeLoopbackPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, run, "serve")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = withEnv(os.Environ(), map[string]string{
		"GITHUB_PORT":              fmt.Sprint(port),
		"IKIGENBA_APP_ID":          "",
		"IKIGENBA_APP_PRIVATE_KEY": privateKey,
		"IKIGENBA_ROOT":            root,
		"TELEMETRY_ENABLED":        "false",
	})
	if err := cmd.Start(); err != nil {
		t.Fatalf("start installed binary: %v", err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-processDone:
		default:
		}
	})

	health, err := waitForHealth(ctx, port)
	if err != nil {
		t.Fatalf("installed binary did not serve health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if health.Service != "github" || health.Status != "ok" {
		t.Fatalf("health envelope service=%q status=%q, want github/ok", health.Service, health.Status)
	}
	if health.Details == nil {
		t.Fatal("health envelope omitted details")
	}
	if _, ok := health.Details["error"].(string); !ok {
		t.Fatalf("offline health details = %#v, want surfaced reporter error", health.Details)
	}

	dbPath := filepath.Join(stateDir, "github.db")
	if info, err := os.Stat(dbPath); err != nil || info.IsDir() {
		t.Fatalf("database was not created at %s: info=%v err=%v", dbPath, info, err)
	}
	generationPath := filepath.Join(cacheDir, "github.db.generation")
	if info, err := os.Stat(generationPath); err != nil || info.IsDir() {
		t.Fatalf("generation sidecar was not created at %s: info=%v err=%v", generationPath, info, err)
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt installed binary: %v", err)
	}
	select {
	case err := <-processDone:
		if err != nil {
			t.Fatalf("installed binary did not shut down cleanly: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-ctx.Done():
		t.Fatalf("installed binary did not stop: %v\nstdout:\n%s\nstderr:\n%s", ctx.Err(), stdout.String(), stderr.String())
	}
}

func TestAuthoredManifestContainsNoInstallOrComposedDataPaths(t *testing.T) {
	// R-8DF1-W89F
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read authored manifest: %v", err)
	}
	if bytes.Contains(contents, []byte("/opt/")) {
		t.Fatalf("authored manifest contains an absolute /opt/ path:\n%s", contents)
	}
	forbidden := regexp.MustCompile(`(?m)^GITHUB_(?:DB_PATH|GENERATION_PATH)=`)
	if line := forbidden.Find(contents); line != nil {
		t.Fatalf("authored manifest contains composed data path %q", line)
	}
}

func TestAuthoredManifestMatchesSpecEmission(t *testing.T) {
	// R-8IAN-FB87
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read authored manifest: %v", err)
	}
	spec := githubapp.Spec()
	extras := make([]manifest.KV, len(spec.ManifestExtras))
	for i, extra := range spec.ManifestExtras {
		extras[i] = manifest.KV{Key: extra.Key, Value: extra.Value}
	}
	want := manifest.Emit(manifest.Fields{
		App:      spec.App,
		Mount:    spec.Mount,
		Default:  spec.Default,
		Port:     spec.Port,
		MCP:      spec.MCP,
		Feed:     spec.Feed,
		Consumes: spec.Consumes,
		Extras:   extras,
	})
	if !bytes.Equal(contents, []byte(want)) {
		t.Fatalf("authored manifest differs from Spec emission\ngot:\n%s\nwant:\n%s", contents, want)
	}
}

func TestAssembledRouterRendersDiskLanding(t *testing.T) {
	// R-WI06-793Q
	// R-EVZ3-VXJZ
	handler := assembledGithubHandler(t, "github-distinct", "v9.8.7")
	rec := get(t, handler, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	if count := strings.Count(rec.Body.String(), "github-distinct"); count != 3 {
		t.Fatalf("service count = %d, want 3\n%s", count, rec.Body.String())
	}
	if count := strings.Count(rec.Body.String(), "v9.8.7"); count != 1 {
		t.Fatalf("version count = %d, want 1\n%s", count, rec.Body.String())
	}
}

func TestCompositionUsesRealShareWWWTree(t *testing.T) {
	// R-WJ82-L0UF
	if !githubapp.Spec().WWW {
		t.Fatal("github Spec.WWW is false")
	}
	handler := assembledGithubHandler(t, "github", "test")
	if landing := get(t, handler, "/"); landing.Code != http.StatusOK || !strings.Contains(landing.Body.String(), "GitHub connector") {
		t.Fatalf("assembled landing = %d %q", landing.Code, landing.Body.String())
	}
	if asset := get(t, handler, "/static/tokens.css"); asset.Code != http.StatusOK || asset.Body.Len() == 0 {
		t.Fatalf("assembled static asset = %d, %d bytes", asset.Code, asset.Body.Len())
	}
	if _, err := appweb.Load(filepath.Join(t.TempDir(), "missing-www")); err == nil {
		t.Fatal("loading a missing share/www root succeeded")
	}
	if _, err := os.Stat(filepath.Join("..", "..", "internal", "web")); !os.IsNotExist(err) {
		t.Fatalf("legacy embedded web package still exists or cannot be checked: %v", err)
	}
}

func TestRenderedLandingUsesOnlyLocalStaticAssets(t *testing.T) {
	// R-WKFY-YSL4
	// R-XSOU-THYE
	body := get(t, assembledGithubHandler(t, "github", "test"), "/").Body.String()
	if !strings.Contains(body, `href="static/tokens.css"`) {
		t.Fatalf("landing does not link mount-relative tokens.css:\n%s", body)
	}
	for _, forbidden := range []string{`href="/static/tokens.css"`, "dashboard", "/srv/dashboard", "http://", "https://"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("landing contains forbidden asset reference %q", forbidden)
		}
	}
}

func TestRenderedLandingPreloadsServedFonts(t *testing.T) {
	// R-WLNV-CKBT
	// R-XTWR-79P3
	handler := assembledGithubHandler(t, "github", "test")
	head := htmlHead(t, get(t, handler, "/").Body.String())
	css := get(t, handler, "/static/tokens.css").Body.String()
	for _, font := range []string{"space-grotesk.woff2", "ibm-plex-sans.woff2"} {
		preload := `<link rel="preload" as="font" type="font/woff2" crossorigin href="static/fonts/` + font + `">`
		if !strings.Contains(head, preload) || !strings.Contains(css, `url('fonts/`+font+`')`) {
			t.Fatalf("landing/CSS do not pair preload and source for %s", font)
		}
	}
}

func TestRenderedLandingUsesCanonicalGithubLayout(t *testing.T) {
	// R-WMVR-QC2I
	// R-7NJI-UTHM
	body := get(t, assembledGithubHandler(t, "github", "test"), "/").Body.String()
	for _, want := range []string{
		`<div class="eyebrow">GitHub connector</div>`,
		`<section aria-labelledby="page-title">`,
		`<h1 id="page-title">github</h1>`,
		`Github connects the suite to GitHub through one shared GitHub App and exposes repository, pull request, and issue actions as MCP tools.`,
		`<dl aria-label="Service details">`, `<dt>Service</dt>`, `<dt>Version</dt>`, `<dt>API</dt>`,
		`<dd class="version">test</dd>`, `<dd><code>POST /mcp</code></dd>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("landing missing %q:\n%s", want, body)
		}
	}
}

func TestRenderedLandingHomeLinkUsesCanonicalAnchoring(t *testing.T) {
	// R-WO3O-43T7
	// R-7PZB-MCZ0
	body := get(t, assembledGithubHandler(t, "github", "test"), "/").Body.String()
	for _, want := range []string{`<main>`, `<a class="home" href="/">Home</a>`, `main {`, `position: relative;`, `.home {`, `position: absolute;`, `top: var(--space-8);`, `.home:hover,`, `.home:focus-visible {`, `color: var(--color-text);`} {
		if !strings.Contains(body, want) {
			t.Fatalf("landing missing canonical Home styling %q", want)
		}
	}
}

func TestAssembledRouterServesTokensAndAllFonts(t *testing.T) {
	// R-WPBK-HVJW
	// R-EX70-9PAO
	handler := assembledGithubHandler(t, "github", "test")
	cases := map[string]string{
		"/static/tokens.css":                    "text/css; charset=utf-8",
		"/static/fonts/space-grotesk.woff2":     "font/woff2",
		"/static/fonts/ibm-plex-sans.woff2":     "font/woff2",
		"/static/fonts/ibm-plex-mono-400.woff2": "font/woff2",
		"/static/fonts/ibm-plex-mono-500.woff2": "font/woff2",
	}
	for path, contentType := range cases {
		rec := get(t, handler, path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != contentType || rec.Body.Len() == 0 {
			t.Errorf("GET %s = %d, type %q, bytes %d; want 200, %q, non-empty", path, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len(), contentType)
		}
	}
}

func TestServedTokensDeclareSelfServedFontFaces(t *testing.T) {
	// R-WQJG-VNAL
	// R-XV4N-L1FS
	css := get(t, assembledGithubHandler(t, "github", "test"), "/static/tokens.css").Body.String()
	for _, want := range []string{`url('fonts/space-grotesk.woff2')`, `url('fonts/ibm-plex-sans.woff2')`, `url('fonts/ibm-plex-mono-400.woff2')`, `url('fonts/ibm-plex-mono-500.woff2')`, `font-family: 'Space Grotesk'`, `font-family: 'IBM Plex Mono'`} {
		if !strings.Contains(css, want) {
			t.Fatalf("tokens.css missing %q", want)
		}
	}
	if strings.Count(css, "@font-face") != 4 || strings.Contains(css, `url('/static/fonts/`) {
		t.Fatalf("tokens.css font-face set is not wholly self-served")
	}
}

func TestServedTokensUseOptionalFontDisplay(t *testing.T) {
	// R-WRRD-9F1A
	// R-XWCJ-YT6H
	css := get(t, assembledGithubHandler(t, "github", "test"), "/static/tokens.css").Body.String()
	faces, optional := strings.Count(css, "@font-face"), strings.Count(css, "font-display: optional")
	if faces == 0 || optional != faces || strings.Contains(css, "font-display: swap") {
		t.Fatalf("font-display optional count = %d for %d faces, swap=%v", optional, faces, strings.Contains(css, "font-display: swap"))
	}
}

func TestExactRootOnAssembledRouterDoesNotShadowRoutes(t *testing.T) {
	// R-WSZ9-N6RZ
	// R-XXKG-CKX6
	handler := assembledGithubHandler(t, "github", "test")
	root := get(t, handler, "/")
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), `<h1 id="page-title">github</h1>`) {
		t.Fatalf("root did not reach landing: %d %q", root.Code, root.Body.String())
	}
	cases := []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/mcp", http.StatusUnauthorized},
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodGet, "/.well-known/oauth-protected-resource", http.StatusOK},
		{http.MethodGet, "/unknown", http.StatusNotFound},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if strings.Contains(rec.Body.String(), `<h1 id="page-title">github</h1>`) {
			t.Errorf("%s %s returned landing", tc.method, tc.path)
		}
		if rec.Code != tc.status {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.status)
		}
	}
}

func TestCompositionRootOptsIntoWWWWithoutHandMountingAssets(t *testing.T) {
	// R-WU76-0YIO
	// R-XYSC-QCNV
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "githubapp", "spec.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, want := range []string{`WWW:        true,`, `rt.Handle("GET /{$}", landingHandler(rt.WWW(), rt.Service(), rt.Version()))`, `rt.Handle("POST /mcp", rt.RequireIdentity(handler))`} {
		if !strings.Contains(text, want) {
			t.Fatalf("spec.go missing %q", want)
		}
	}
	landingLine := lineContaining(t, text, `rt.Handle("GET /{$}"`)
	if strings.Contains(landingLine, "RequireIdentity") {
		t.Fatalf("landing is identity gated: %s", landingLine)
	}
	for _, forbidden := range []string{"github/internal/" + "web", `rt.Handle("GET /static/"`, `rt.Handle("GET /favicon.ico"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("spec.go contains forbidden hand-wiring %q", forbidden)
		}
	}
}

func TestAssembledRouterServesBrandContract(t *testing.T) {
	// R-RYDN-YNR5
	// R-RZLK-CFHU
	// R-8MFA-HUNC
	handler := assembledGithubHandler(t, "github", "test")
	landing := get(t, handler, "/")
	head := htmlHead(t, landing.Body.String())
	for _, link := range []string{
		`<link rel="icon" sizes="any" href="static/favicon.ico">`,
		`<link rel="icon" type="image/png" href="static/favicon-32.png">`,
		`<link rel="apple-touch-icon" href="static/apple-touch-icon.png">`,
	} {
		if !strings.Contains(head, link) {
			t.Errorf("rendered landing head missing %q", link)
		}
	}
	assets := map[string]string{
		"/static/favicon.ico":          "image/x-icon",
		"/static/favicon-32.png":       "image/png",
		"/static/apple-touch-icon.png": "image/png",
	}
	for path, contentType := range assets {
		rec := get(t, handler, path)
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 || rec.Header().Get("Content-Type") != contentType {
			t.Errorf("GET %s = %d, %d bytes, %q; want 200, non-empty, %q", path, rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"), contentType)
		}
	}
	root := get(t, handler, "/favicon.ico")
	static := get(t, handler, "/static/favicon.ico")
	if root.Code != http.StatusOK || root.Header().Get("Content-Type") != "image/x-icon" || !bytes.Equal(root.Body.Bytes(), static.Body.Bytes()) {
		t.Fatalf("root favicon does not match static favicon: root=%d %q bytesEqual=%v", root.Code, root.Header().Get("Content-Type"), bytes.Equal(root.Body.Bytes(), static.Body.Bytes()))
	}
}

func TestCommittedNginxFragmentPreservesGithubRouting(t *testing.T) {
	// R-EYEW-NH1D
	// R-1GOK-GA2F
	// R-1HWG-U1T4
	// R-42HV-I1HS
	// R-43PR-VT8H
	// R-44XO-9KZ6
	// R-1S5A-Z3ZD
	// R-1TD7-CVQ2
	// R-1UL3-QNGR
	// R-GW5W-UJVL
	confBytes, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	conf := string(confBytes)
	root := nginxLocationBlock(t, conf, `location = /srv/github/ {`)
	static := nginxLocationBlock(t, conf, `location /srv/github/static/ {`)
	bearer := nginxLocationBlock(t, conf, `location /srv/github/ {`)
	prm := nginxLocationBlock(t, conf, `location = /srv/github/.well-known/oauth-protected-resource {`)
	for name, block := range map[string]string{"root": root, "static": static} {
		for _, want := range []string{"auth_request /_session-authn;", "error_page 401 = @login_bounce;"} {
			if !strings.Contains(block, want) {
				t.Errorf("%s location missing %q", name, want)
			}
		}
	}
	for _, want := range []string{
		"auth_request /_authn;",
		"auth_request_set $github_owner  $upstream_http_x_owner_email;",
		"auth_request_set $github_owner_id $upstream_http_x_owner_id;",
		"auth_request_set $github_owner_name $upstream_http_x_owner_name;",
		"auth_request_set $github_owner_picture $upstream_http_x_owner_picture;",
		"auth_request_set $github_client $upstream_http_x_client_id;",
		"auth_request_set $github_correlation $upstream_http_x_correlation_id;",
		"proxy_set_header X-Owner-Id $github_owner_id;",
		"proxy_set_header X-Owner-Email $github_owner;",
		"proxy_set_header X-Owner-Name $github_owner_name;",
		"proxy_set_header X-Owner-Picture $github_owner_picture;",
		"proxy_set_header X-Client-Id  $github_client;",
		"proxy_set_header X-Correlation-Id $github_correlation;",
	} {
		if !strings.Contains(bearer, want) {
			t.Errorf("bearer location missing %q", want)
		}
	}
	if strings.Contains(bearer, "error_page 401 = @login_bounce;") {
		t.Error("bearer location uses login bounce")
	}
	for _, want := range []string{
		"auth_request_set $github_session_owner $upstream_http_x_owner_email;",
		"auth_request_set $github_session_owner_id $upstream_http_x_owner_id;",
		"auth_request_set $github_session_owner_name $upstream_http_x_owner_name;",
		"auth_request_set $github_session_owner_picture $upstream_http_x_owner_picture;",
		"proxy_set_header X-Owner-Id $github_session_owner_id;",
		"proxy_set_header X-Owner-Email $github_session_owner;",
		"proxy_set_header X-Owner-Name $github_session_owner_name;",
		"proxy_set_header X-Owner-Picture $github_session_owner_picture;",
		"auth_request_set $github_session_correlation $upstream_http_x_correlation_id;",
		"proxy_set_header X-Correlation-Id $github_session_correlation;",
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root location missing %q", want)
		}
	}
	if strings.Contains(prm, "auth_request") || !strings.Contains(prm, `proxy_set_header X-Correlation-Id "";`) {
		t.Errorf("public PRM has incorrect auth/correlation wiring:\n%s", prm)
	}
	for _, want := range []string{"location = /srv/github/pr { return 404; }", "location = /srv/github/token { return 404; }", "location @github_authn_500 {"} {
		if !strings.Contains(conf, want) {
			t.Errorf("nginx fragment missing %q", want)
		}
	}
	if strings.Contains(conf, "/srv/github/feed") {
		t.Error("nginx fragment contains a github feed route")
	}
}

func assembledGithubHandler(t *testing.T, service, version string) http.Handler {
	t.Helper()
	t.Setenv("IKIGENBA_APP_ID", "12345")
	t.Setenv("IKIGENBA_GITHUB_ORG", "acme")
	t.Setenv("IKIGENBA_APP_PRIVATE_KEY", throwawayPrivateKeyPEM(t))
	site, err := appweb.Load(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatalf("load share/www: %v", err)
	}
	spec := githubapp.Spec()
	srv, err := server.New(server.Options{
		Addr: "127.0.0.1:0", Logger: slog.Default(), Service: service, Version: version,
		ResourceID: "https://example.test/srv/github/", AuthServer: "https://example.test/",
		WWW: site, Register: spec.Handlers,
	})
	if err != nil {
		t.Fatalf("assemble github router: %v", err)
	}
	return srv.Handler
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func htmlHead(t *testing.T, body string) string {
	t.Helper()
	start, end := strings.Index(body, "<head>"), strings.Index(body, "</head>")
	if start < 0 || end <= start {
		t.Fatalf("response has no complete head:\n%s", body)
	}
	return body[start : end+len("</head>")]
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

func nginxLocationBlock(t *testing.T, conf, start string) string {
	t.Helper()
	offset := strings.Index(conf, start)
	if offset < 0 {
		t.Fatalf("nginx fragment missing location %q", start)
	}
	remaining := conf[offset:]
	end := strings.Index(remaining, "\n}")
	if end < 0 {
		t.Fatalf("nginx location %q is not closed", start)
	}
	return remaining[:end+2]
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("create copy destination %s: %v", destination, err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("read copy source %s: %v", source, err)
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(source, entry.Name())
		destinationPath := filepath.Join(destination, entry.Name())
		if entry.IsDir() {
			copyTree(t, sourcePath, destinationPath)
			continue
		}
		input, err := os.Open(sourcePath)
		if err != nil {
			t.Fatalf("open %s: %v", sourcePath, err)
		}
		output, err := os.Create(destinationPath)
		if err != nil {
			_ = input.Close()
			t.Fatalf("create %s: %v", destinationPath, err)
		}
		_, copyErr := io.Copy(output, input)
		closeOutputErr := output.Close()
		closeInputErr := input.Close()
		if copyErr != nil || closeOutputErr != nil || closeInputErr != nil {
			t.Fatalf("copy %s: copy=%v close-output=%v close-input=%v", sourcePath, copyErr, closeOutputErr, closeInputErr)
		}
	}
}

func assertSymlinkResolvesTo(t *testing.T, link, want string) {
	t.Helper()
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("resolve symlink %s: %v", link, err)
	}
	want, err = filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve intended target %s: %v", want, err)
	}
	if got != want {
		t.Fatalf("symlink %s resolves to %s, want %s", link, got, want)
	}
}

func throwawayPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate throwaway RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate loopback port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

type healthEnvelope struct {
	Status  string         `json:"status"`
	Service string         `json:"service"`
	Details map[string]any `json:"details"`
}

func waitForHealth(ctx context.Context, port int) (healthEnvelope, error) {
	client := &http.Client{
		Timeout:   250 * time.Millisecond,
		Transport: &http.Transport{Proxy: nil},
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", port), nil)
		if err != nil {
			return healthEnvelope{}, err
		}
		response, err := client.Do(req)
		if err == nil {
			var body healthEnvelope
			decodeErr := json.NewDecoder(response.Body).Decode(&body)
			closeErr := response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && closeErr == nil {
				return body, nil
			}
			lastErr = fmt.Errorf("status=%d body=%+v decode=%v close=%v", response.StatusCode, body, decodeErr, closeErr)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return healthEnvelope{}, fmt.Errorf("health never became ready: %w (last error: %v)", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func withEnv(base []string, overrides map[string]string) []string {
	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}
