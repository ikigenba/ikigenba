package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"registry"
)

type nginxLocation struct {
	exact  bool
	target string
	body   string
}

var locationStart = regexp.MustCompile(`(?m)^\s*location\s+(?:(=)\s+)?(\S+)\s*\{`)

func TestNginxFragmentIsLocationOnlyAndMatchesRegistry(t *testing.T) {
	config := readNginxFragment(t)
	locations := parseNginxLocations(t, config)

	// R-W60I-CYGD
	forbidden := regexp.MustCompile(`(?m)^\s*(server|listen|ssl_certificate|http)\b`)
	if match := forbidden.FindString(config); match != "" {
		t.Fatalf("nginx fragment contains forbidden top-level directive %q", strings.TrimSpace(match))
	}
	proxyPass := regexp.MustCompile(`(?m)^\s*proxy_pass\s+(\S+);`)
	passes := proxyPass.FindAllStringSubmatch(config, -1)
	if len(passes) == 0 {
		t.Fatal("nginx fragment has no proxy_pass directives")
	}
	wantOrigin := "http://127.0.0.1:" + strconv.Itoa(registry.MustPort("telemetry"))
	for _, pass := range passes {
		if pass[1] != wantOrigin && !strings.HasPrefix(pass[1], wantOrigin+"/") {
			t.Errorf("proxy_pass target %q does not use registry origin %q", pass[1], wantOrigin)
		}
	}
	for _, location := range locations {
		if strings.HasPrefix(location.target, "/") && !strings.HasPrefix(location.target, "/srv/telemetry/") {
			t.Errorf("location path %q is outside telemetry mount", location.target)
		}
	}
}

func TestNginxMCPIdentityHeadersComeExactlyOnceFromAuthSubrequest(t *testing.T) {
	mcp := requireNginxLocation(t, parseNginxLocations(t, readNginxFragment(t)), "/srv/telemetry/mcp", true)

	// R-W78E-QQ72
	if countDirective(mcp.body, `auth_request\s+/_authn;`) != 1 {
		t.Fatalf("MCP auth_request /_authn count = %d, want 1", countDirective(mcp.body, `auth_request\s+/_authn;`))
	}
	sets := authRequestVariables(mcp.body)
	for _, header := range []string{"X-Owner-Id", "X-Owner-Email", "X-Owner-Name", "X-Owner-Picture", "X-Client-Id"} {
		values := proxyHeaderValues(mcp.body, header)
		if len(values) != 1 {
			t.Errorf("proxy_set_header %s values = %v, want exactly one", header, values)
			continue
		}
		sources := sets[values[0]]
		if len(sources) != 1 || !strings.HasPrefix(sources[0], "$upstream_http_") {
			t.Errorf("proxy_set_header %s uses %q populated from %v, want one auth subrequest header", header, values[0], sources)
		}
	}
}

func TestNginxCatchAllShieldsEveryUnlistedPathAndNeverNamesIngest(t *testing.T) {
	config := readNginxFragment(t)
	locations := parseNginxLocations(t, config)

	// R-W8GB-4HXR
	if count := strings.Count(config, "ingest"); count != 0 {
		t.Fatalf("nginx fragment contains ingest %d times, want 0", count)
	}
	catchAllCount := 0
	for _, location := range locations {
		if location.target != "/srv/telemetry/" || location.exact {
			continue
		}
		catchAllCount++
		if countDirective(location.body, `return\s+404;`) != 1 {
			t.Errorf("catch-all does not contain exactly one return 404")
		}
		if countDirective(location.body, `proxy_pass\s+`) != 0 {
			t.Errorf("catch-all unexpectedly contains proxy_pass")
		}
	}
	if catchAllCount != 1 {
		t.Fatalf("prefix catch-all count = %d, want 1", catchAllCount)
	}
}

func TestNginxPRMIsUngatedExactBootstrapRoute(t *testing.T) {
	prm := requireNginxLocation(t, parseNginxLocations(t, readNginxFragment(t)), "/srv/telemetry/.well-known/oauth-protected-resource", true)

	// R-W9O7-I9OG
	if countDirective(prm.body, `auth_request\s+`) != 0 {
		t.Fatal("PRM location unexpectedly contains auth_request")
	}
	wantPath := `proxy_pass\s+http://127\.0\.0\.1:[0-9]+/\.well-known/oauth-protected-resource;`
	if countDirective(prm.body, wantPath) != 1 {
		t.Fatal("PRM location does not proxy exactly once to upstream protected-resource path")
	}
}

func TestNginxCorrelationIDIsOverwrittenAtBothPublicEdges(t *testing.T) {
	locations := parseNginxLocations(t, readNginxFragment(t))
	mcp := requireNginxLocation(t, locations, "/srv/telemetry/mcp", true)
	prm := requireNginxLocation(t, locations, "/srv/telemetry/.well-known/oauth-protected-resource", true)

	// R-WAW3-W1F5
	sets := authRequestVariables(mcp.body)
	corrValues := proxyHeaderValues(mcp.body, "X-Correlation-Id")
	if len(corrValues) != 1 || len(sets[corrValues[0]]) != 1 || sets[corrValues[0]][0] != "$upstream_http_x_correlation_id" {
		t.Fatalf("MCP correlation header values = %v, auth variables = %v; want one variable captured from introspection response", corrValues, sets)
	}
	if prmValues := proxyHeaderValues(prm.body, "X-Correlation-Id"); len(prmValues) != 1 || prmValues[0] != `""` {
		t.Fatalf("PRM X-Correlation-Id values = %v, want exactly one empty string", prmValues)
	}
}

func TestNginxLandingIsExactSessionGatedAndForwardsCapturedIdentity(t *testing.T) {
	landing := requireNginxLocation(t, parseNginxLocations(t, readNginxFragment(t)), "/srv/telemetry/", true)

	// R-67LH-0ZXP
	if countDirective(landing.body, `auth_request\s+/_session-authn;`) != 1 {
		t.Fatalf("landing session auth_request count = %d, want 1", countDirective(landing.body, `auth_request\s+/_session-authn;`))
	}
	if countDirective(landing.body, `auth_request\s+/_authn;`) != 0 {
		t.Fatal("landing is bearer-gated instead of session-gated")
	}
	if countDirective(landing.body, `error_page\s+401\s+=\s+@login_bounce;`) != 1 {
		t.Fatal("landing does not bounce an unauthenticated browser to login")
	}
	if countDirective(landing.body, `proxy_pass\s+http://127\.0\.0\.1:[0-9]+/;`) != 1 {
		t.Fatal("landing does not proxy exactly once to the upstream root path")
	}

	sets := authRequestVariables(landing.body)
	for _, header := range []string{"X-Owner-Email", "X-Owner-Id", "X-Owner-Name", "X-Owner-Picture"} {
		values := proxyHeaderValues(landing.body, header)
		if len(values) != 1 {
			t.Errorf("proxy_set_header %s values = %v, want exactly one", header, values)
			continue
		}
		if !strings.HasPrefix(values[0], "$telemetry_session_") {
			t.Errorf("proxy_set_header %s source = %q, want a telemetry session variable", header, values[0])
			continue
		}
		sources := sets[values[0]]
		wantSource := "$upstream_http_" + strings.ToLower(strings.ReplaceAll(header, "-", "_"))
		if len(sources) != 1 || sources[0] != wantSource {
			t.Errorf("proxy_set_header %s source %q is populated from %v, want exactly %q", header, values[0], sources, wantSource)
		}
	}
	assertCapturedCorrelationHeader(t, landing.body, "$telemetry_session_")
}

func TestNginxStaticAssetsAreSessionGatedWithoutForwardedIdentity(t *testing.T) {
	static := requireNginxLocation(t, parseNginxLocations(t, readNginxFragment(t)), "/srv/telemetry/static/", false)

	// R-68TD-EROE
	if countDirective(static.body, `auth_request\s+/_session-authn;`) != 1 {
		t.Fatalf("static session auth_request count = %d, want 1", countDirective(static.body, `auth_request\s+/_session-authn;`))
	}
	if countDirective(static.body, `auth_request\s+/_authn;`) != 0 {
		t.Fatal("static assets are bearer-gated instead of session-gated")
	}
	if countDirective(static.body, `error_page\s+401\s+=\s+@login_bounce;`) != 1 {
		t.Fatal("static assets do not bounce an unauthenticated browser to login")
	}
	if countDirective(static.body, `proxy_pass\s+http://127\.0\.0\.1:[0-9]+/static/;`) != 1 {
		t.Fatal("static location does not proxy exactly once to the upstream static path")
	}
	for _, header := range []string{"X-Owner-Email", "X-Owner-Id", "X-Owner-Name", "X-Owner-Picture", "X-Client-Id"} {
		if values := proxyHeaderValues(static.body, header); len(values) != 0 {
			t.Errorf("static location forwards identity header %s from %v", header, values)
		}
	}
	assertCapturedCorrelationHeader(t, static.body, "$telemetry_static_")
}

func assertCapturedCorrelationHeader(t *testing.T, body, variablePrefix string) {
	t.Helper()
	sets := authRequestVariables(body)
	values := proxyHeaderValues(body, "X-Correlation-Id")
	if len(values) != 1 || !strings.HasPrefix(values[0], variablePrefix) {
		t.Fatalf("X-Correlation-Id values = %v, want exactly one captured %s variable", values, variablePrefix)
	}
	if sources := sets[values[0]]; len(sources) != 1 || sources[0] != "$upstream_http_x_correlation_id" {
		t.Fatalf("X-Correlation-Id variable %q populated from %v, want introspection correlation header", values[0], sources)
	}
}

func readNginxFragment(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx fragment: %v", err)
	}
	return string(body)
}

func parseNginxLocations(t *testing.T, config string) []nginxLocation {
	t.Helper()
	matches := locationStart.FindAllStringSubmatchIndex(config, -1)
	locations := make([]nginxLocation, 0, len(matches))
	for _, match := range matches {
		bodyStart := match[1]
		depth := 1
		bodyEnd := -1
		for i := bodyStart; i < len(config); i++ {
			switch config[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					bodyEnd = i
				}
			}
			if bodyEnd >= 0 {
				break
			}
		}
		if bodyEnd < 0 {
			t.Fatalf("unterminated location %q", config[match[4]:match[5]])
		}
		locations = append(locations, nginxLocation{
			exact:  match[2] >= 0,
			target: config[match[4]:match[5]],
			body:   config[bodyStart:bodyEnd],
		})
	}
	return locations
}

func requireNginxLocation(t *testing.T, locations []nginxLocation, target string, exact bool) nginxLocation {
	t.Helper()
	var found []nginxLocation
	for _, location := range locations {
		if location.target == target && location.exact == exact {
			found = append(found, location)
		}
	}
	if len(found) != 1 {
		t.Fatalf("locations for target=%q are %v, want exactly one with exact=%t", target, found, exact)
	}
	return found[0]
}

func authRequestVariables(body string) map[string][]string {
	matches := regexp.MustCompile(`(?m)^\s*auth_request_set\s+(\$[A-Za-z0-9_]+)\s+(\$[A-Za-z0-9_]+);`).FindAllStringSubmatch(body, -1)
	sets := make(map[string][]string, len(matches))
	for _, match := range matches {
		sets[match[1]] = append(sets[match[1]], match[2])
	}
	return sets
}

func proxyHeaderValues(body, header string) []string {
	pattern := `(?m)^\s*proxy_set_header\s+` + regexp.QuoteMeta(header) + `\s+(\S+);`
	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(body, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		values = append(values, match[1])
	}
	return values
}

func countDirective(body, pattern string) int {
	return len(regexp.MustCompile(`(?m)^\s*`+pattern).FindAllString(body, -1))
}
