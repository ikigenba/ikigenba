// Package e2e is the cross-package end-to-end layer for webhooks (the D7 tier).
// These tests exercise the most faithful substrate available in the normal gate:
// real temp-file SQLite migrations, real domain services, real HTTP handlers,
// committed nginx routing fragments, and a short-lived real webhooks binary.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	chassis "appkit/db"
	"eventplane/outbox"
	"webhooks/internal/db"
	"webhooks/internal/webhooks"
)

const frontDoorMount = "/srv/webhooks"

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// R-OD12-3CVG — the public ingress tier is unauthenticated, proxied, and injects
// no identity header. Mint a real webhook+secret against a real DB, then POST
// through the ingress handler with ONLY the per-webhook bearer and assert 202;
// the committed nginx fragment is also asserted to leave this tier ungated.
func TestIngressTierAcceptsBearerWithoutOAuth(t *testing.T) {
	conf := readNginxConfig(t)
	block := nginxLocationBlock(t, conf, "location /srv/webhooks/in/ {")
	if strings.Contains(block, "auth_request ") {
		t.Fatalf("public ingress location unexpectedly has auth_request:\n%s", block)
	}
	if strings.Contains(block, "proxy_set_header X-Owner-Email") || strings.Contains(block, "proxy_set_header X-Client-Id") {
		t.Fatalf("public ingress location injects identity headers:\n%s", block)
	}

	h, _, name, secret := newIngressFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/in/"+name, strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /in/%s with valid secret and no OAuth: status = %d, want 202; body=%q", name, rec.Code, rec.Body.String())
	}
}

// R-OE8Y-H4M5 — the MCP tier is gated via auth_request. Both POST and GET to
// /srv/webhooks/mcp with no bearer token must be turned away by nginx before
// reaching the upstream; the committed exact-match location is the contract.
func TestMCPTierGatedWithoutBearer(t *testing.T) {
	block := nginxLocationBlock(t, readNginxConfig(t), "location = /srv/webhooks/mcp {")
	if !strings.Contains(block, "auth_request /_authn;") {
		t.Fatalf("mcp location does not use bearer auth_request:\n%s", block)
	}
	if strings.Contains(block, "proxy_set_header X-Owner-Email $http_") || strings.Contains(block, "proxy_set_header X-Client-Id $http_") {
		t.Fatalf("mcp location forwards caller-supplied identity headers:\n%s", block)
	}
}

// R-OFGU-UWCU — the PRM bootstrap tier is open. The committed nginx exact-match
// location for the RFC 9728 document carries no auth_request, so a token-less MCP
// client can discover the authorization server.
func TestPRMBootstrapTierOpen(t *testing.T) {
	block := nginxLocationBlock(t, readNginxConfig(t), "location = /srv/webhooks/.well-known/oauth-protected-resource {")
	if strings.Contains(block, "auth_request ") {
		t.Fatalf("PRM bootstrap location unexpectedly has auth_request:\n%s", block)
	}
	if !strings.Contains(block, "/.well-known/oauth-protected-resource;") {
		t.Fatalf("PRM bootstrap location does not proxy to upstream metadata route:\n%s", block)
	}
}

// R-OGOR-8O3J — the internal event feed is shielded from the public mount.
// The front-door exact-match location must return 404 while the loopback /feed
// remains a service-internal route.
func TestFeedShieldedFromPublicMount(t *testing.T) {
	block := nginxLocationBlock(t, readNginxConfig(t), "location = /srv/webhooks/feed {")
	if !strings.Contains(block, "return 404;") {
		t.Fatalf("feed shield location does not return 404:\n%s", block)
	}
}

// R-UELV-YLA4
// R-0FNQ-0XSK — webhooks is wired into bin/start/bin/stop/go.work and actually
// launchable: the real cmd/webhooks binary serves /health with 200 on loopback.
func TestServiceLaunchedHealthOK(t *testing.T) {
	assertFileContains(t, filepath.Join("..", "..", "..", "bin", "start"), "launch_webhooks")
	assertFileContains(t, filepath.Join("..", "..", "..", "bin", "stop"), "webhooks")

	bin := buildBinary(t)
	port := freeTCPPort(t)
	dbPath := filepath.Join(t.TempDir(), "webhooks.db")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = append(os.Environ(),
		"WEBHOOKS_DB_PATH="+dbPath,
		"WEBHOOKS_WWW_PATH="+repoShareWWWPath(t),
		"WEBHOOKS_PORT="+strconv.Itoa(port),
		"WEBHOOKS_RESOURCE_ID=http://127.0.0.1"+frontDoorMount+"/mcp",
		"WEBHOOKS_AUTH_SERVER=http://127.0.0.1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start webhooks: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer stopProcess(cancel, done)

	resp := waitForHealth(t, port, done, &stdout, &stderr)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want 200", resp.StatusCode)
	}
}

// R-UFTS-CD0T — the committed etc/manifest.env advertises the MCP resource
// (MCP=true, MOUNT=/srv/webhooks/) so the dashboard's manifest-driven inventory
// derives the /srv/webhooks/mcp resource. Asserting the committed manifest is the
// source of truth the dashboard reads at boot.
func TestManifestAdvertisesMCPResource(t *testing.T) {
	path := filepath.Join("..", "..", "etc", "manifest.env")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}

	if got := kv["MCP"]; got != "true" {
		t.Fatalf("manifest MCP = %q, want \"true\" (dashboard inventory would not discover the MCP resource)", got)
	}
	if got := kv["MOUNT"]; got != "/srv/webhooks/" {
		t.Fatalf("manifest MOUNT = %q, want \"/srv/webhooks/\"", got)
	}
}

func newIngressFixture(t *testing.T) (http.Handler, *sql.DB, string, string) {
	t.Helper()
	conn := migratedDB(t)
	clk := fixedClock{t: time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)}
	ob, err := outbox.New(conn, outbox.Options{Source: "webhooks", Registry: webhooks.Events, Now: clk.Now})
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}
	svc := webhooks.NewService(conn, clk)
	svc.Outbox = ob
	wh, secret, err := svc.Create(context.Background(), "e2e@example.com", "e2e-ingress", "bearer")
	if err != nil {
		t.Fatalf("mint webhook: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return webhooks.NewIngressHandler(svc, log), conn, wh.Name, secret
}

func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := chassis.Open(filepath.Join(t.TempDir(), "webhooks.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := chassis.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := chassis.Migrate(context.Background(), conn, migs); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return conn
}

func readNginxConfig(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx config: %v", err)
	}
	return string(body)
}

func nginxLocationBlock(t *testing.T, conf, opener string) string {
	t.Helper()
	start := strings.Index(conf, opener)
	if start < 0 {
		t.Fatalf("nginx config does not contain location opener %q", opener)
	}
	bodyStart := start + len(opener)
	depth := 1
	end := bodyStart
	for ; end < len(conf) && depth > 0; end++ {
		switch conf[end] {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	if depth != 0 {
		t.Fatalf("nginx location %q does not have a matching closing brace", opener)
	}
	return conf[start:end]
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(body), want) {
		t.Fatalf("%s does not contain %q", path, want)
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "webhooks.bin")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/webhooks")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build cmd/webhooks: %v\n%s", err, out)
	}
	return bin
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

func repoShareWWWPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatalf("resolve share/www path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "landing.html")); err != nil {
		t.Fatalf("share/www landing missing: %v", err)
	}
	return path
}

func waitForHealth(t *testing.T, port int, done <-chan error, stdout, stderr *bytes.Buffer) *http.Response {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("webhooks exited before health: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		default:
		}
		resp, err := client.Get(url)
		if err == nil {
			return resp
		}
		last = err.Error()
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("webhooks never served health at %s: %s\nstdout:\n%s\nstderr:\n%s", url, last, stdout.String(), stderr.String())
	return nil
}

func stopProcess(cancel context.CancelFunc, done <-chan error) {
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}
