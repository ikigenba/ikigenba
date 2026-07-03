package web

import (
	"os"
	"strings"
	"testing"
)

func TestNginxLandingLocationIsExactMatchAndSessionGated(t *testing.T) {
	conf := readNginxConfig(t)
	block := nginxLocationBlock(t, conf, "location = /srv/crm/ {")

	// R-NGNX-2H5J
	if strings.Contains(conf, "location /srv/crm/ {\n    auth_request /_session-authn;") {
		t.Fatalf("prefix /srv/crm/ location is session-gated instead of exact landing location:\n%s", conf)
	}
	if block == "" {
		t.Fatal("landing exact-match location block is empty")
	}
	if prefixBlock := nginxLocationBlock(t, conf, "location /srv/crm/ {"); prefixBlock == block {
		t.Fatal("exact landing location was not distinct from bearer-gated prefix location")
	}

	// R-NGNX-4K7L
	if !strings.Contains(block, "auth_request /_session-authn;") {
		t.Fatalf("landing location missing session auth_request:\n%s", block)
	}
	if strings.Contains(block, "auth_request /_authn;") {
		t.Fatalf("landing location is bearer-gated instead of session-gated:\n%s", block)
	}
	if !strings.Contains(block, "proxy_set_header X-Owner-Email $crm_session_owner;") {
		t.Fatalf("landing location does not forward session owner identity:\n%s", block)
	}

	// R-NGNX-6M9N
	if !strings.Contains(block, "proxy_pass http://127.0.0.1:3100/;") {
		t.Fatalf("landing location does not proxy to upstream root with trailing slash:\n%s", block)
	}
}

func TestNginxExistingServiceLocationsSurvive(t *testing.T) {
	conf := readNginxConfig(t)

	// R-NGNX-8P1Q
	prefix := nginxLocationBlock(t, conf, "location /srv/crm/ {")
	if !strings.Contains(prefix, "auth_request /_authn;") {
		t.Fatalf("service prefix location missing bearer auth_request:\n%s", prefix)
	}
	if !strings.Contains(conf, "location = /srv/crm/feed { return 404; }") {
		t.Fatalf("feed denial location missing:\n%s", conf)
	}
	prm := nginxLocationBlock(t, conf, "location = /srv/crm/.well-known/oauth-protected-resource {")
	if strings.Contains(prm, "auth_request") {
		t.Fatalf("PRM bootstrap location unexpectedly gated:\n%s", prm)
	}
	if !strings.Contains(prm, "proxy_pass http://127.0.0.1:3100/.well-known/oauth-protected-resource;") {
		t.Fatalf("PRM bootstrap location missing upstream proxy_pass:\n%s", prm)
	}
}

func TestNginxStaticLocationIsSessionGatedAndProxiesStaticHandler(t *testing.T) {
	conf := readNginxConfig(t)
	block := nginxLocationBlock(t, conf, "location /srv/crm/static/ {")

	// R-SWNU-U5QA
	for _, want := range []string{
		"auth_request /_session-authn;",
		"proxy_pass http://127.0.0.1:3100/static/;",
		"proxy_set_header Host $host;",
		"proxy_set_header X-Forwarded-Proto $scheme;",
		"proxy_http_version 1.1;",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("static location missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "auth_request /_authn;") {
		t.Fatalf("static location is bearer-gated instead of session-gated:\n%s", block)
	}

	if landing := nginxLocationBlock(t, conf, "location = /srv/crm/ {"); !strings.Contains(landing, "auth_request /_session-authn;") {
		t.Fatalf("landing exact location changed unexpectedly:\n%s", landing)
	}
	if prefix := nginxLocationBlock(t, conf, "location /srv/crm/ {"); !strings.Contains(prefix, "auth_request /_authn;") {
		t.Fatalf("bearer prefix location changed unexpectedly:\n%s", prefix)
	}
	if !strings.Contains(conf, "location = /srv/crm/feed { return 404; }") {
		t.Fatalf("feed denial location changed unexpectedly:\n%s", conf)
	}
	prm := nginxLocationBlock(t, conf, "location = /srv/crm/.well-known/oauth-protected-resource {")
	if strings.Contains(prm, "auth_request") {
		t.Fatalf("PRM bootstrap location changed unexpectedly:\n%s", prm)
	}
}

func readNginxConfig(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../../etc/nginx.conf")
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
