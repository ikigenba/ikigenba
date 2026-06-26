package web

import (
	"os"
	"strings"
	"testing"
)

// fragmentPath is the notify nginx location fragment, relative to
// notify/internal/web/ (this test's directory).
const fragmentPath = "../../etc/nginx.conf"

func readFragment(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatalf("read %s: %v", fragmentPath, err)
	}
	return string(b)
}

// exactMatchBlock returns the body of the exact-match `location = /srv/notify/ {`
// block (up to its closing brace at column 0), or "" if absent.
func exactMatchBlock(frag string) string {
	const marker = "location = /srv/notify/ {"
	i := strings.Index(frag, marker)
	if i < 0 {
		return ""
	}
	rest := frag[i+len(marker):]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func TestNginxHasExactMatchLandingLocation(t *testing.T) {
	// R-NGNX-3N6X — the fragment contains an exact-match `location = /srv/notify/ {`
	// block, distinct from the prefix form `location /srv/notify/ {`. Require the
	// `= ` form specifically so the prefix location does not satisfy the assertion.
	frag := readFragment(t)
	if !strings.Contains(frag, "location = /srv/notify/ {") {
		t.Fatal("missing exact-match `location = /srv/notify/ {` block")
	}
	// The exact-match marker must be the `= ` form, not the bare prefix that
	// would also be a substring without the `= `.
	if strings.Index(frag, "location = /srv/notify/ {") == strings.Index(frag, "location /srv/notify/ {") {
		t.Fatal("exact-match and prefix locations must be distinct blocks")
	}
}

func TestNginxExactMatchUsesSessionAuthn(t *testing.T) {
	// R-NGNX-5P8Y — the exact-match block gates the landing root with the session
	// hook `auth_request /_session-authn` and does NOT use the bearer hook
	// `auth_request /_authn`.
	block := exactMatchBlock(readFragment(t))
	if block == "" {
		t.Fatal("exact-match `location = /srv/notify/ {` block not found")
	}
	if !strings.Contains(block, "auth_request /_session-authn") {
		t.Errorf("exact-match block missing `auth_request /_session-authn`:\n%s", block)
	}
	if strings.Contains(block, "auth_request /_authn") {
		t.Errorf("exact-match block must NOT gate landing root with bearer `auth_request /_authn`:\n%s", block)
	}
}

func TestNginxExactMatchProxiesToLoopbackRoot(t *testing.T) {
	// R-NGNX-7Q1Z — the exact-match block proxies to the loopback upstream root
	// with a trailing slash (the PORT stays templated as __PORT__).
	block := exactMatchBlock(readFragment(t))
	if block == "" {
		t.Fatal("exact-match `location = /srv/notify/ {` block not found")
	}
	if !strings.Contains(block, "proxy_pass http://127.0.0.1:__PORT__/;") {
		t.Errorf("exact-match block missing `proxy_pass http://127.0.0.1:__PORT__/;`:\n%s", block)
	}
}

func TestNginxPreExistingLocationsSurvive(t *testing.T) {
	// R-NGNX-9R3B — the additive edit preserves the bearer-gated prefix location
	// (with `auth_request /_authn`) and the unauthenticated PRM bootstrap.
	frag := readFragment(t)
	if !strings.Contains(frag, "location /srv/notify/ {") {
		t.Error("bearer-gated prefix `location /srv/notify/ {` missing")
	}
	if !strings.Contains(frag, "auth_request /_authn;") {
		t.Error("bearer-gated prefix must still use `auth_request /_authn;`")
	}
	if !strings.Contains(frag, "location = /srv/notify/.well-known/oauth-protected-resource {") {
		t.Error("PRM bootstrap location missing")
	}
}
