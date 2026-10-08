package nginx_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

// R-ITO3-EWD4 R-IW3W-6FUI R-IUVZ-SO3T
func TestRenderAPIReservationsAndChallengeInBothWiredForms(t *testing.T) {
	for _, guests := range []string{"false", "true"} {
		root := t.TempDir()
		writeManifest(t, root, "auth", "app = 'auth'\n")
		writeManifest(t, root, "notes", "app = 'notes'\nguests = "+guests+"\n")
		candidate, err := nginx.Render(context.Background(), enabledEnv(root), "space.example.test", "")
		if err != nil {
			t.Fatal(err)
		}
		block := serverBlockFor(t, string(candidate), "notes.space.example.test")
		for _, location := range []string{"= /api", "^~ /api/"} {
			want := wiredProxyLocation("notes", location, "@api_unauthorized")
			if got := locationFor(t, block, location); got != want {
				t.Fatalf("guests=%s %s = %q, want %q", guests, location, got, want)
			}
		}
		want := "    location @api_unauthorized {\n" +
			"        default_type text/plain;\n" +
			"        add_header   WWW-Authenticate                 'Bearer realm=\"ikigenba\"' always;\n" + expectedChallengeCORS +
			"        return       401 \"authentication required: sign in or send Authorization: Bearer <token>\\n\";\n" +
			"    }\n"
		if got := locationFor(t, block, "@api_unauthorized"); got != want {
			t.Fatalf("API challenge = %q, want %q", got, want)
		}
		// Exact and protected prefix locations reserve API requests before the
		// generated regular expression; the original request target is proxied.
		order := []string{"include /opt/notes/etc/nginx.conf*;", "location = /mcp {", "location ^~ /mcp/ {", "location = /api {", "location ^~ /api/ {", "location ~ /(info/refs|git-upload-pack|git-receive-pack)$ {", "location / {"}
		previous := -1
		for _, marker := range order {
			at := strings.Index(block, marker)
			if at <= previous {
				t.Fatalf("location order: missing or misplaced %q", marker)
			}
			previous = at
		}
		for _, forbidden := range []string{"proxy_set_header Authorization", "proxy_pass_request_headers off", "rewrite "} {
			if strings.Contains(block, forbidden) {
				t.Fatalf("original request altered by %q", forbidden)
			}
		}

	}
}

// R-RXUX-ZEX6 R-S1IN-4Q59
func TestRenderCORSAtEveryServiceBlockAndChallenge(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		root := t.TempDir()
		for _, name := range []string{"strict", "guests", "disabled"} {
			writeManifest(t, root, name, "app = '"+name+"'\nguests = true\n")
		}
		writeManifest(t, root, "strict", "app = 'strict'\ndefault = true\n")
		if authenticated {
			writeManifest(t, root, "auth", "app = 'auth'\n")
		}
		env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
			if command.Args[3] == "ikigenba-disabled.socket" {
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
			}
			return host.Result{}, nil
		}}
		candidate, err := nginx.Render(context.Background(), env, "space.example.test", "guests")
		if err != nil {
			t.Fatal(err)
		}
		blocks := strings.Split(string(candidate), "server {\n")[1:]
		for index, tail := range blocks {
			block := "server {\n" + tail
			if index < 3 {
				for _, forbidden := range []string{"add_header", "if (", "$ikigenba_cors_"} {
					if strings.Contains(block, forbidden) {
						t.Fatalf("base server carries %q", forbidden)
					}
				}
				continue
			}
			logEnd := strings.Index(block, "access.log ikigenba;\n") + len("access.log ikigenba;\n")
			if logEnd < len("access.log ikigenba;\n") || !strings.HasPrefix(block[logEnd:], expectedServerCORS) {
				t.Fatalf("missing server-level headers/preflight after log: %s", block)
			}
			// The preflight precedes the app include, every location and 503.
			end := logEnd + len(expectedServerCORS)
			if strings.Contains(block[:end], "location ") || strings.Contains(block[:end], "include ") || strings.Contains(block[:end], "return              503") {
				t.Fatalf("preflight follows request handling: %s", block)
			}
			locations := strings.Split(block, "    location ")[1:]
			for _, location := range locations {
				if strings.HasPrefix(location, "@mcp_unauthorized ") || strings.HasPrefix(location, "@mcp_invalid_token ") || strings.HasPrefix(location, "@api_unauthorized ") || strings.HasPrefix(location, "@git_unauthorized ") {
					if !strings.Contains(location, expectedChallengeCORS) || strings.Count(location, "add_header") != 5 {
						t.Fatalf("challenge loses or adds CORS headers: %s", location)
					}
				} else if strings.Contains(location, "add_header") {
					t.Fatalf("ordinary location overrides header inheritance: %s", location)
				}
			}
		}
	}
}

// R-RZ2U-D6NV R-S0AQ-QYEK
func TestRenderCORSMapsGrantOnlySitesOriginAndSelectMethodHeaders(t *testing.T) {
	for _, hostName := range []string{"sbx.ikigenba.dev", "other-2.example.test"} {
		for _, sitesState := range []string{"absent", "unrouted", "enabled", "disabled"} {
			for _, authenticated := range []bool{false, true} {
				root := t.TempDir()
				if sitesState != "absent" {
					manifest := "guests = true\n"
					if sitesState != "unrouted" {
						manifest += "app = 'sites'\n"
					}
					writeManifest(t, root, "sites", manifest)
				}
				if authenticated {
					writeManifest(t, root, "auth", "app = 'auth'\n")
				}
				env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
					if sitesState == "disabled" && command.Args[3] == "ikigenba-sites.socket" {
						return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
					}
					return host.Result{}, nil
				}}
				candidate, err := nginx.Render(context.Background(), env, hostName, "")
				if err != nil {
					t.Fatal(err)
				}
				config := string(candidate)
				if strings.Count(config, "\nmap ") != 7 || !strings.Contains(config, expectedCORSMaps(hostName)) {
					t.Fatal("host must declare the seven exact maps once")
				}
				allowed := "https://sites." + hostName
				origins := []string{allowed, "", "https://elsewhere.example", allowed + ".elsewhere.example", "http://sites." + hostName, strings.ToUpper(allowed), allowed + "/", "prefix" + allowed, strings.Replace(allowed, ".", "X", 1)}
				for _, origin := range origins {
					for _, method := range []string{"OPTIONS", "GET", "POST", "PATCH", "DELETE", "HEAD", "OTHER"} {
						mappedOrigin := mappedValue(t, config, "$ikigenba_cors_origin", origin, map[string]string{"$http_origin": origin})
						credentials := mappedValue(t, config, "$ikigenba_cors_credentials", mappedOrigin, nil)
						wantOrigin, wantCredentials := "", ""
						if origin == allowed {
							wantOrigin, wantCredentials = allowed, "true"
						}
						if mappedOrigin != wantOrigin || credentials != wantCredentials {
							t.Fatalf("origin %q grant = (%q, %q)", origin, mappedOrigin, credentials)
						}
						for _, header := range []struct{ variable, preflight, ordinary string }{
							{"$ikigenba_cors_methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS", ""},
							{"$ikigenba_cors_headers", "Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID", ""},
							{"$ikigenba_cors_max_age", "600", ""},
							{"$ikigenba_cors_expose", "", "Mcp-Session-Id, WWW-Authenticate"},
						} {
							want := ""
							if origin == allowed {
								want = header.ordinary
								if method == "OPTIONS" {
									want = header.preflight
								}
							}
							if got := mappedValue(t, config, header.variable, method+" "+credentials, nil); got != want {
								t.Fatalf("origin %q method %s %s = %q, want %q", origin, method, header.variable, got, want)
							}
						}
					}
				}
			}
		}
	}
}

// mappedValue reads the rendered map grammar, choosing literal entries before
// case-sensitive regex entries and resolving values from the supplied request.
func mappedValue(t *testing.T, config, variable, input string, values map[string]string) string {
	t.Helper()
	marker := " " + variable + " {\n"
	start := strings.Index(config, marker)
	if start < 0 {
		t.Fatalf("missing map %s", variable)
	}
	body, _, found := strings.Cut(config[start+len(marker):], "\n}")
	if !found {
		t.Fatalf("unclosed map %s", variable)
	}
	entry := regexp.MustCompile(`^\s*("[^"]*"|default)\s+("[^"]*"|\$[a-z_]+);$`)
	fallback := ""
	type regexEntry struct{ pattern, value string }
	var regexes []regexEntry
	resolve := func(value string) string {
		if strings.HasPrefix(value, "$") {
			return values[value]
		}
		return strings.Trim(value, `"`)
	}
	for _, line := range strings.Split(body, "\n") {
		parts := entry.FindStringSubmatch(line)
		if len(parts) != 3 {
			t.Fatalf("invalid map entry %q", line)
		}
		key := strings.Trim(parts[1], `"`)
		value := resolve(parts[2])
		switch {
		case key == "default":
			fallback = value
		case strings.HasPrefix(key, "~"):
			regexes = append(regexes, regexEntry{key[1:], value})
		case key == input:
			return value
		}
	}
	for _, item := range regexes {
		matcher, err := regexp.Compile(item.pattern)
		if err != nil {
			t.Fatal(err)
		}
		if matcher.MatchString(input) {
			return item.value
		}
	}
	return fallback
}

// R-S2QJ-IHVY
func TestRenderUpstreamOriginInEveryGeneratedProxyLocation(t *testing.T) {
	const hostName = "space.example.test"
	allowed := "https://sites." + hostName
	for _, form := range []string{"plain", "strict", "guests"} {
		t.Run(form, func(t *testing.T) {
			root := t.TempDir()
			manifest := "app = 'notes'\ndefault = true\n"
			if form == "guests" {
				manifest += "guests = true\n"
			}
			writeManifest(t, root, "notes", manifest)
			if form != "plain" {
				writeManifest(t, root, "auth", "app = 'auth'\n")
			}
			candidate, err := nginx.Render(context.Background(), enabledEnv(root), hostName, "notes")
			if err != nil {
				t.Fatal(err)
			}
			config := string(candidate)
			block := serverBlockFor(t, config, "notes."+hostName)
			if !strings.Contains(block, "server_name         notes."+hostName+" "+hostName+" example.test;") {
				t.Fatal("proxy policy must share the service, default and apex names")
			}
			locations := []string{"/"}
			if form != "plain" {
				locations = append(locations, "= /_ikigenba/check", "= /mcp", "^~ /mcp/", "= /api", "^~ /api/", "~ /(info/refs|git-upload-pack|git-receive-pack)$")
			}
			if form == "guests" {
				locations = append(locations, "= /_ikigenba/check/open")
			}
			if strings.Count(block, "proxy_pass ") != len(locations) {
				t.Fatal("not every generated proxy location is covered")
			}
			for _, location := range locations {
				section := locationFor(t, block, location)
				originDirective := regexp.MustCompile(`(?m)^\s*proxy_set_header\s+Origin\s+\$ikigenba_upstream_origin;$`)
				if len(originDirective.FindAllString(section, -1)) != 1 || len(regexp.MustCompile(`(?m)^\s*proxy_set_header\s+Origin\s+`).FindAllString(section, -1)) != 1 {
					t.Fatalf("%s does not uniquely set mapped Origin: %s", location, section)
				}
			}
			for _, origin := range []string{allowed, "", "https://elsewhere.example", allowed + ".elsewhere.example", "http://sites." + hostName, strings.ToUpper(allowed), allowed + "/", "prefix" + allowed, strings.Replace(allowed, ".", "X", 1), allowed + ", " + allowed} {
				grant := mappedValue(t, config, "$ikigenba_cors_origin", origin, map[string]string{"$http_origin": origin})
				want := origin
				if origin == allowed {
					want = ""
				}
				if got := mappedValue(t, config, "$ikigenba_upstream_origin", grant, map[string]string{"$http_origin": origin}); got != want {
					t.Fatalf("Origin %q becomes %q, want %q", origin, got, want)
				}
			}
			if form != "plain" {
				auth := serverBlockFor(t, config, "auth."+hostName)
				proxy := locationFor(t, auth, "/")
				for _, forbidden := range []string{"proxy_set_header Origin", "$ikigenba_upstream_origin", "proxy_pass_request_headers off"} {
					if strings.Contains(proxy, forbidden) {
						t.Fatalf("auth's own proxy alters incoming Origin with %q", forbidden)
					}
				}
				if strings.Count(auth, "proxy_pass ") != 1 {
					t.Fatal("auth proxy coverage incomplete")
				}
			}
		})
	}
}
