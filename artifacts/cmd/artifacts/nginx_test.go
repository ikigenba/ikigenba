package main

import (
	"regexp"
	"strings"
	"testing"
)

// R-4TNH-HPLP
func TestNginxProtectedResourceMetadataIsUngatedInLocationFragment(t *testing.T) {
	fragment := string(readRepoFile(t, "etc", "nginx.conf"))
	block := nginxLocationBlock(t, fragment, "location = /srv/artifacts/.well-known/oauth-protected-resource {")
	assertContains(t, block, "proxy_pass http://127.0.0.1:3009/.well-known/oauth-protected-resource;")
	assertOmits(t, block, "auth_request")
	if regexp.MustCompile(`(?m)^\s*(server\s*\{|listen\s+)`).MatchString(fragment) {
		t.Fatalf("nginx.conf is a location fragment but contains a server or listen directive:\n%s", fragment)
	}
}

// R-4UVD-VHCE
func TestNginxMCPForwardsOnlyIntrospectionIdentity(t *testing.T) {
	fragment := string(readRepoFile(t, "etc", "nginx.conf"))
	block := nginxLocationBlock(t, fragment, "location = /srv/artifacts/mcp {")
	assertContains(t, block, "auth_request /_authn;")
	for _, header := range []string{"owner_id", "owner_email", "owner_name", "owner_picture", "client_id"} {
		assertContains(t, block, "auth_request_set $af_"+header+" $upstream_http_x_"+header+";")
		name := strings.Join(titleWords(strings.Split(header, "_")), "-")
		if got := strings.Count(block, "proxy_set_header X-"+name+" $af_"+header+";"); got != 1 {
			t.Errorf("MCP block forwards X-%s %d times, want exactly once:\n%s", name, got, block)
		}
	}
	if regexp.MustCompile(`proxy_set_header\s+X-(Owner|Client)-[^;]*\$http_`).MatchString(block) {
		t.Fatalf("MCP block proxies request-sourced identity:\n%s", block)
	}
}

// R-4W3A-9933
func TestNginxShieldsFeedContentAndUnknownServicePaths(t *testing.T) {
	fragment := string(readRepoFile(t, "etc", "nginx.conf"))
	feed := nginxLocationBlock(t, fragment, "location = /srv/artifacts/feed {")
	catchAll := nginxLocationBlock(t, fragment, "location /srv/artifacts/ {")
	assertContains(t, feed, "return 404;")
	assertContains(t, catchAll, "return 404;")
	if strings.Contains(fragment, "/content") {
		t.Fatalf("nginx.conf exposes a content location:\n%s", fragment)
	}
}

// R-4XB6-N0TS
func TestNginxLandingAndStaticAreSessionGated(t *testing.T) {
	fragment := string(readRepoFile(t, "etc", "nginx.conf"))
	landing := nginxLocationBlock(t, fragment, "location = /srv/artifacts/ {")
	static := nginxLocationBlock(t, fragment, "location /srv/artifacts/static/ {")
	for _, block := range []string{landing, static} {
		assertContains(t, block, "auth_request /_session-authn;")
		assertOmits(t, block, "auth_request /_authn;")
	}
	assertContains(t, landing, "error_page 401 = @login_bounce;")
	for _, header := range []string{"owner_id", "owner_email", "owner_name", "owner_picture"} {
		assertContains(t, landing, "auth_request_set $af_session_"+header+" $upstream_http_x_"+header+";")
	}
}

// R-4YJ3-0SKH
func TestNginxPublicDownloadsStripIdentityAndCorrelation(t *testing.T) {
	fragment := string(readRepoFile(t, "etc", "nginx.conf"))
	block := nginxLocationBlock(t, fragment, "location /srv/artifacts/f/ {")
	assertPublicIngress(t, block)
	assertContains(t, block, `proxy_set_header X-Correlation-Id "";`)
	assertContains(t, block, "proxy_pass http://127.0.0.1:3009/f/;")
}

// R-4ZQZ-EKB6
func TestNginxUploadIngressIsOpenWithCoarseBodyGuard(t *testing.T) {
	fragment := string(readRepoFile(t, "etc", "nginx.conf"))
	block := nginxLocationBlock(t, fragment, "location /srv/artifacts/u/ {")
	assertPublicIngress(t, block)
	assertContains(t, block, `proxy_set_header X-Correlation-Id "";`)
	assertContains(t, block, "client_max_body_size 1g;")
	assertContains(t, block, "proxy_pass http://127.0.0.1:3009/u/;")
}

// R-50YV-SC1V
func TestNginxPrivateDownloadsForwardSessionIdentity(t *testing.T) {
	fragment := string(readRepoFile(t, "etc", "nginx.conf"))
	block := nginxLocationBlock(t, fragment, "location /srv/artifacts/p/ {")
	assertContains(t, block, "auth_request /_session-authn;")
	assertContains(t, block, "error_page 401 = @login_bounce;")
	for _, header := range []string{"owner_id", "owner_email", "owner_name", "owner_picture"} {
		assertContains(t, block, "auth_request_set $af_session_"+header+" $upstream_http_x_"+header+";")
		name := strings.Join(titleWords(strings.Split(header, "_")), "-")
		assertContains(t, block, "proxy_set_header X-"+name+" $af_session_"+header+";")
	}
	assertContains(t, block, "proxy_pass http://127.0.0.1:3009/p/;")
}

// R-526S-63SK
func TestNginxGatesOverwriteCorrelationAndReemitRateLimit(t *testing.T) {
	fragment := string(readRepoFile(t, "etc", "nginx.conf"))
	for marker, variable := range map[string]string{
		"location = /srv/artifacts/mcp {": "$af_correlation_id",
		"location = /srv/artifacts/ {":    "$af_session_correlation_id",
		"location /srv/artifacts/p/ {":    "$af_session_correlation_id",
	} {
		block := nginxLocationBlock(t, fragment, marker)
		assertContains(t, block, "auth_request_set "+variable+" $upstream_http_x_correlation_id;")
		assertContains(t, block, "proxy_set_header X-Correlation-Id "+variable+";")
		assertContains(t, block, "error_page 500 = @artifacts_authn_500;")
	}
	rateLimit := nginxLocationBlock(t, fragment, "location @artifacts_authn_500 {")
	assertContains(t, rateLimit, "add_header Retry-After $af_retry_after always;")
	assertContains(t, rateLimit, "return 429;")
}

func nginxLocationBlock(t *testing.T, fragment, marker string) string {
	t.Helper()
	start := strings.Index(fragment, marker)
	if start < 0 {
		t.Fatalf("nginx.conf lacks %q", marker)
	}
	depth := 0
	for offset, r := range fragment[start:] {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return fragment[start : start+offset+1]
			}
		}
	}
	t.Fatalf("nginx block %q is unterminated", marker)
	return ""
}

func assertPublicIngress(t *testing.T, block string) {
	t.Helper()
	assertOmits(t, block, "auth_request")
	if regexp.MustCompile(`proxy_set_header\s+X-(Owner|Client)-`).MatchString(block) {
		t.Fatalf("public ingress sets an identity header:\n%s", block)
	}
}

func assertContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("block lacks %q:\n%s", want, body)
	}
}

func assertOmits(t *testing.T, body, forbidden string) {
	t.Helper()
	if strings.Contains(body, forbidden) {
		t.Errorf("block unexpectedly contains %q:\n%s", forbidden, body)
	}
}

func titleWords(words []string) []string {
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return words
}
