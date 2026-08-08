package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"registry"
)

func TestNginxFragmentEnforcesPublicRouteTiers(t *testing.T) {
	// R-G1OF-AAC8
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	conf := string(contents)
	upstream := registry.BaseURL("repos")
	if upstream != "http://127.0.0.1:3007" {
		t.Fatalf("repos registry base URL = %q, want loopback port 3007", upstream)
	}

	prm := nginxBlock(t, conf, "location = /srv/repos/.well-known/oauth-protected-resource {")
	if strings.Contains(prm, "auth_request") {
		t.Fatalf("PRM bootstrap is authentication-gated:\n%s", prm)
	}
	if !strings.Contains(prm, "proxy_pass "+upstream+"/.well-known/oauth-protected-resource;") {
		t.Fatalf("PRM bootstrap does not proxy to the repos upstream:\n%s", prm)
	}
	if !strings.Contains(conf, "location = /srv/repos/feed { return 404; }") {
		t.Fatalf("nginx fragment does not deny the public feed route:\n%s", conf)
	}

	landing := nginxBlock(t, conf, "location = /srv/repos/ {")
	assets := nginxBlock(t, conf, "location /srv/repos/static/ {")
	for name, tier := range map[string]struct {
		block     string
		proxyPass string
	}{
		"landing":       {block: landing, proxyPass: "proxy_pass " + upstream + "/;"},
		"static assets": {block: assets, proxyPass: "proxy_pass " + upstream + "/static/;"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, directive := range []string{
				"auth_request /_session-authn;",
				"error_page 401 = @login_bounce;",
				tier.proxyPass,
			} {
				if !strings.Contains(tier.block, directive) {
					t.Fatalf("session tier missing %q:\n%s", directive, tier.block)
				}
			}
			if strings.Contains(tier.block, "auth_request /_authn;") {
				t.Fatalf("session tier uses bearer authentication:\n%s", tier.block)
			}
		})
	}

	bearer := nginxBlock(t, conf, "location /srv/repos/ {")
	for _, directive := range []string{
		"auth_request /_authn;",
		"auth_request_set $repos_owner  $upstream_http_x_owner_email;",
		"auth_request_set $repos_client $upstream_http_x_client_id;",
		"proxy_set_header X-Owner-Email $repos_owner;",
		"proxy_set_header X-Client-Id  $repos_client;",
		"error_page 500 = @repos_authn_500;",
		"proxy_pass " + upstream + "/;",
	} {
		if !strings.Contains(bearer, directive) {
			t.Fatalf("bearer tier missing %q:\n%s", directive, bearer)
		}
	}
	if strings.Contains(bearer, "error_page 401 = @login_bounce;") {
		t.Fatalf("bearer tier redirects OAuth clients to browser login:\n%s", bearer)
	}
	recovery := nginxBlock(t, conf, "location @repos_authn_500 {")
	for _, directive := range []string{"if ($authn_status = 429)", "add_header Retry-After $authn_retry always;", "return 429;", "return 500;"} {
		if !strings.Contains(recovery, directive) {
			t.Fatalf("429 recovery tier missing %q:\n%s", directive, recovery)
		}
	}
	for _, forbidden := range []string{"server {", "listen ", "ssl_certificate"} {
		if strings.Contains(conf, forbidden) {
			t.Fatalf("location fragment contains vhost directive %q", forbidden)
		}
	}
}

func TestNginxFragmentForwardsSessionOwnerIdentity(t *testing.T) {
	// R-UZVS-S08C
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	landing := nginxBlock(t, string(contents), "location = /srv/repos/ {")
	for _, directive := range []string{
		"auth_request_set $repos_session_owner $upstream_http_x_owner_email;",
		"auth_request_set $repos_session_owner_id $upstream_http_x_owner_id;",
		"auth_request_set $repos_session_owner_name $upstream_http_x_owner_name;",
		"auth_request_set $repos_session_owner_picture $upstream_http_x_owner_picture;",
		"proxy_set_header X-Owner-Email $repos_session_owner;",
		"proxy_set_header X-Owner-Id $repos_session_owner_id;",
		"proxy_set_header X-Owner-Name $repos_session_owner_name;",
		"proxy_set_header X-Owner-Picture $repos_session_owner_picture;",
	} {
		if !strings.Contains(landing, directive) {
			t.Errorf("session landing missing owner identity directive %q:\n%s", directive, landing)
		}
	}
}

func TestNginxFragmentForwardsBearerOwnerAndClientIdentity(t *testing.T) {
	// R-V13P-5RZ1
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	bearer := nginxBlock(t, string(contents), "location /srv/repos/ {")
	for _, directive := range []string{
		"auth_request_set $repos_owner  $upstream_http_x_owner_email;",
		"auth_request_set $repos_owner_id      $upstream_http_x_owner_id;",
		"auth_request_set $repos_owner_name    $upstream_http_x_owner_name;",
		"auth_request_set $repos_owner_picture $upstream_http_x_owner_picture;",
		"auth_request_set $repos_client $upstream_http_x_client_id;",
		"proxy_set_header X-Owner-Email $repos_owner;",
		"proxy_set_header X-Owner-Id $repos_owner_id;",
		"proxy_set_header X-Owner-Name $repos_owner_name;",
		"proxy_set_header X-Owner-Picture $repos_owner_picture;",
		"proxy_set_header X-Client-Id  $repos_client;",
	} {
		if !strings.Contains(bearer, directive) {
			t.Errorf("bearer tier missing identity directive %q:\n%s", directive, bearer)
		}
	}
}

func TestNginxFragmentGitDoorAuthenticatesAndStreams(t *testing.T) {
	// R-J3QD-3HFN
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	gitDoor := nginxBlock(t, string(contents), "location /srv/repos/git/ {")
	upstream := registry.BaseURL("repos")
	for _, directive := range []string{
		"auth_request /_authn;",
		"auth_request_set $repos_git_owner $upstream_http_x_owner_email;",
		"auth_request_set $repos_git_owner_id $upstream_http_x_owner_id;",
		"auth_request_set $repos_git_client $upstream_http_x_client_id;",
		"auth_request_set $repos_git_correlation $upstream_http_x_correlation_id;",
		"proxy_set_header X-Owner-Email $repos_git_owner;",
		"proxy_set_header X-Owner-Id $repos_git_owner_id;",
		"proxy_set_header X-Client-Id $repos_git_client;",
		"proxy_set_header X-Correlation-Id $repos_git_correlation;",
		"client_max_body_size 0;",
		"proxy_request_buffering off;",
		"proxy_buffering off;",
		"proxy_pass " + upstream + "/git/;",
	} {
		if !strings.Contains(gitDoor, directive) {
			t.Errorf("git door missing directive %q:\n%s", directive, gitDoor)
		}
	}
	if strings.Contains(gitDoor, "proxy_pass "+upstream+"/;") {
		t.Errorf("git door proxies to the service root instead of /git/:\n%s", gitDoor)
	}

	var readTimeout string
	for _, line := range strings.Split(gitDoor, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "proxy_read_timeout" {
			readTimeout = strings.TrimSuffix(fields[1], ";")
			break
		}
	}
	if !strings.HasSuffix(readTimeout, "s") {
		t.Fatalf("git door proxy_read_timeout = %q, want a value in seconds", readTimeout)
	}
	seconds, err := strconv.Atoi(strings.TrimSuffix(readTimeout, "s"))
	if err != nil {
		t.Fatalf("parse git door proxy_read_timeout %q: %v", readTimeout, err)
	}
	if seconds < 600 {
		t.Errorf("git door proxy_read_timeout = %ds, want at least 600s", seconds)
	}
}

func TestNginxFragmentCapturesAndForwardsGatedCorrelationIDs(t *testing.T) {
	// R-9DUI-TUQJ
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	conf := string(contents)
	if got := countNginxAuthRequestDirectives(conf); got != 4 {
		t.Fatalf("auth_request directive count = %d, want gate count 4", got)
	}

	for name, gated := range map[string]struct {
		block   string
		capture string
		forward string
	}{
		"session landing": {
			block:   nginxBlock(t, conf, "location = /srv/repos/ {"),
			capture: "auth_request_set $repos_session_correlation $upstream_http_x_correlation_id;",
			forward: "proxy_set_header X-Correlation-Id $repos_session_correlation;",
		},
		"session static assets": {
			block:   nginxBlock(t, conf, "location /srv/repos/static/ {"),
			capture: "auth_request_set $repos_session_correlation $upstream_http_x_correlation_id;",
			forward: "proxy_set_header X-Correlation-Id $repos_session_correlation;",
		},
		"bearer catch-all": {
			block:   nginxBlock(t, conf, "location /srv/repos/ {"),
			capture: "auth_request_set $repos_correlation $upstream_http_x_correlation_id;",
			forward: "proxy_set_header X-Correlation-Id $repos_correlation;",
		},
		"git door": {
			block:   nginxBlock(t, conf, "location /srv/repos/git/ {"),
			capture: "auth_request_set $repos_git_correlation $upstream_http_x_correlation_id;",
			forward: "proxy_set_header X-Correlation-Id $repos_git_correlation;",
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, directive := range []string{gated.capture, gated.forward} {
				if !strings.Contains(gated.block, directive) {
					t.Errorf("gated location missing correlation directive %q:\n%s", directive, gated.block)
				}
			}
			if strings.Contains(gated.block, "$http_x_correlation_id") {
				t.Errorf("gated location forwards client-supplied correlation id:\n%s", gated.block)
			}
			if strings.Contains(gated.block, "proxy_set_header X-Correlation-Id \"\";") {
				t.Errorf("gated location strips the captured correlation id:\n%s", gated.block)
			}
		})
	}
}

func TestNginxFragmentStripsUngatedPRMCorrelationID(t *testing.T) {
	// R-9F2F-7MH8
	contents, err := os.ReadFile(filepath.Join("..", "..", "etc", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	conf := string(contents)
	prm := nginxBlock(t, conf, "location = /srv/repos/.well-known/oauth-protected-resource {")
	if !strings.Contains(prm, "proxy_set_header X-Correlation-Id \"\";") {
		t.Fatalf("ungated PRM bootstrap does not strip the correlation id:\n%s", prm)
	}
	if strings.Contains(prm, "auth_request") {
		t.Fatalf("ungated PRM bootstrap contains an auth request directive:\n%s", prm)
	}

	for _, header := range []string{
		"location = /srv/repos/ {",
		"location /srv/repos/static/ {",
		"location /srv/repos/git/ {",
		"location /srv/repos/ {",
	} {
		block := nginxBlock(t, conf, header)
		if strings.Contains(block, "proxy_set_header X-Correlation-Id \"\";") {
			t.Errorf("gated location %q strips the correlation id:\n%s", header, block)
		}
	}
}

func countNginxAuthRequestDirectives(conf string) int {
	count := 0
	for _, line := range strings.Split(conf, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "auth_request" {
			count++
		}
	}
	return count
}

func nginxBlock(t *testing.T, conf, header string) string {
	t.Helper()
	start := strings.Index(conf, header)
	if start < 0 {
		t.Fatalf("nginx fragment does not contain %q:\n%s", header, conf)
	}
	depth := 0
	for i := start; i < len(conf); i++ {
		switch conf[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return conf[start : i+1]
			}
		}
	}
	t.Fatalf("nginx fragment contains unterminated block %q", header)
	return ""
}
