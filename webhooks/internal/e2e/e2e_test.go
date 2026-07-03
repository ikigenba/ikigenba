// Package e2e is the cross-package, suite-up end-to-end layer for webhooks (the
// D7 tier). Unlike the package-local integration tests it does NOT use httptest:
// it drives the real binary through the running nginx front door on :8080, the
// dev mirror of the prod path-routed auth chain. The suite must be up
// (../../../bin/start) — these tests bring no server up themselves and a
// down/unreachable :8080 is a hard FAILURE, never a skip, so an all-green run
// genuinely proves the routing tiers.
//
// The ingress test mints a real webhook+secret by opening the running service's
// own SQLite file (WEBHOOKS_DB_PATH, dev default repo-root tmp/webhooks.db) and
// calling the real domain Service, then POSTs the secret through :8080 — the same
// file the live process reads when it validates the bearer.
package e2e

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webhooks/internal/db"
	"webhooks/internal/webhooks"
)

const (
	frontDoor   = "http://localhost:8080"
	loopback    = "http://127.0.0.1:3006"
	frontDoorTo = 2 * time.Second
)

// requireFrontDoor fails (does NOT skip) when the nginx front door on :8080 is
// unreachable. The D7 done bar is explicit: an all-skipped e2e layer because
// :8080 was down is a GAP, not a pass — so make absence loud.
func requireFrontDoor(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "localhost:8080", frontDoorTo)
	if err != nil {
		t.Fatalf("front door :8080 unreachable — bring the suite up with ../../../bin/start: %v", err)
	}
	_ = conn.Close()
}

// webhooksDBPath locates the running service's SQLite file: WEBHOOKS_DB_PATH when
// set (as bin/start exports it), else the dev default repo-root tmp/webhooks.db,
// resolved relative to this package dir (<service>/internal/e2e).
func webhooksDBPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("WEBHOOKS_DB_PATH"); p != "" {
		return p
	}
	def := filepath.Join("..", "..", "..", "tmp", "webhooks.db")
	if _, err := os.Stat(def); err != nil {
		t.Fatalf("cannot locate running webhooks db at %s (set WEBHOOKS_DB_PATH or run bin/start): %v", def, err)
	}
	return def
}

// noRedirectClient returns the raw status without chasing 3xx, so a 404/401 is
// observed verbatim rather than followed.
func noRedirectClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// R-OD12-3CVG — the public ingress tier is unauthenticated, proxied, and injects
// no identity header. Mint a real webhook+secret against the running db, then POST
// through :8080 with ONLY the per-webhook bearer (no OAuth token) and assert 202.
func TestIngressTierAcceptsBearerWithoutOAuth(t *testing.T) {
	requireFrontDoor(t)

	conn, err := db.Open(webhooksDBPath(t))
	if err != nil {
		t.Fatalf("open running webhooks db: %v", err)
	}
	defer conn.Close()

	svc := webhooks.NewService(conn, webhooks.RealClock{})
	name := fmt.Sprintf("e2e-ingress-%d", time.Now().UnixNano())
	if _, secret, err := svc.Create(context.Background(), "e2e@example.com", name); err != nil {
		t.Fatalf("mint webhook %q: %v", name, err)
	} else {
		url := frontDoor + "/srv/webhooks/in/" + name
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"hello":"world"}`))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		// Only the per-webhook secret — deliberately NO OAuth bearer / OAuth chain.
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := noRedirectClient().Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("POST %s with valid secret and no OAuth: status = %d, want 202", url, resp.StatusCode)
		}
	}
}

// R-OE8Y-H4M5 — the MCP tier is gated via auth_request. Both POST and GET to
// /srv/webhooks/mcp with no bearer token must be turned away with 401 by nginx
// (the dashboard introspection hook) before reaching the upstream.
func TestMCPTierGatedWithoutBearer(t *testing.T) {
	requireFrontDoor(t)

	url := frontDoor + "/srv/webhooks/mcp"
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			t.Fatalf("build %s request: %v", method, err)
		}
		resp, err := noRedirectClient().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, url, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s with no bearer: status = %d, want 401", method, url, resp.StatusCode)
		}
	}
}

// R-OFGU-UWCU — the PRM bootstrap tier is open. GET the RFC 9728 doc with no
// token and assert 200, so a token-less MCP client can discover the AS.
func TestPRMBootstrapTierOpen(t *testing.T) {
	requireFrontDoor(t)

	url := frontDoor + "/srv/webhooks/.well-known/oauth-protected-resource"
	resp, err := noRedirectClient().Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s with no token: status = %d, want 200", url, resp.StatusCode)
	}
}

// R-OGOR-8O3J — the internal event feed is shielded from the public mount. GET
// /srv/webhooks/feed through :8080 must be 404 (the exact-match location returns
// 404; the loopback /feed stays internal-only).
func TestFeedShieldedFromPublicMount(t *testing.T) {
	requireFrontDoor(t)

	url := frontDoor + "/srv/webhooks/feed"
	resp, err := noRedirectClient().Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET %s: status = %d, want 404", url, resp.StatusCode)
	}
}

// R-UELV-YLA4 — webhooks is wired into bin/start/bin/stop/go.work and actually
// launched: the running process on :3006 answers GET /health with 200 (the
// appkit chassis serves /health on the loopback port).
func TestServiceLaunchedHealthOK(t *testing.T) {
	url := loopback + "/health"
	resp, err := noRedirectClient().Get(url)
	if err != nil {
		t.Fatalf("GET %s — webhooks not launched on :3006? bring the suite up with ../../../bin/start: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", url, resp.StatusCode)
	}
}

// R-UFTS-CD0T — the committed etc/manifest.env advertises the MCP resource
// (MCP=true, MOUNT=/srv/webhooks/) so the dashboard's manifest-driven inventory
// derives the …/srv/webhooks/mcp resource. Asserting the committed manifest is the
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
