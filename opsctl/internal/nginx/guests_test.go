package nginx_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

// guestsServiceBlock expresses the two exact differences from the strict fixture.
func guestsServiceBlock(name string, defaultService bool, hostName, apexName string) string {
	strict := wiredServiceBlock(name, 0, defaultService, hostName, apexName, 0)
	checkEnd := strings.Index(strict, "    location @auth_redirect")
	checkStart := strings.Index(strict, "    location = /_ikigenba/check {")
	openCheck := strings.ReplaceAll(strict[checkStart:checkEnd], "/check", "/check/open")
	strict = strict[:checkEnd] + openCheck + strict[checkEnd:]
	browserStart := strings.Index(strict, "    location / {")
	browser := strings.Replace(strict[browserStart:], "auth_request     /_ikigenba/check;", "auth_request     /_ikigenba/check/open;", 1)
	browser = strings.Replace(browser, "        error_page       401 = @auth_redirect;\n", "", 1)
	return strict[:browserStart] + browser
}

// R-W6L5-TDPO R-X7R6-367W R-EPD0-PHUT R-WBGR-CGOG
func TestRenderGuestsExactShapeAndBlockSelection(t *testing.T) {
	for _, authState := range []string{"absent", "unrouted", "enabled", "disabled"} {
		for _, guests := range []string{"", "guests = false\n", "guests = true\n"} {
			for _, mcp := range []string{"", "mcp = false\n", "mcp = true\ndescription = 'Tools'\n"} {
				root := t.TempDir()
				if authState != "absent" {
					authManifest := "guests = true\n"
					if authState != "unrouted" {
						authManifest += "app = 'auth'\n"
					}
					writeManifest(t, root, "auth", authManifest)
				}
				writeManifest(t, root, "notes", "app = 'notes'\ndefault = true\n"+guests+mcp)
				writeManifest(t, root, "disabled", "app = 'disabled'\n"+guests+mcp)
				env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
					if command.Args[3] == "ikigenba-disabled.socket" || authState == "disabled" && command.Args[3] == "ikigenba-auth.socket" {
						return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
					}
					return host.Result{}, nil
				}}
				got, err := nginx.Render(context.Background(), env, "space.example.test", "notes")
				if err != nil {
					t.Fatal(err)
				}
				want := baseForHost("space.example.test", true)
				switch authState {
				case "enabled":
					want += unwiredServiceBlock(false, "space.example.test", "")
				case "disabled":
					want += disabledServiceBlock("auth", "")
				}
				want += disabledServiceBlock("disabled", "")
				switch {
				case authState == "absent" || authState == "unrouted":
					want += serviceBlock("notes", 0, true, "space.example.test", "example.test")
				case guests == "guests = true\n":
					want += guestsServiceBlock("notes", true, "space.example.test", "example.test")
				default:
					want += wiredServiceBlock("notes", 0, true, "space.example.test", "example.test", 0)
				}
				if string(got) != want {
					t.Fatalf("auth=%s guests=%q mcp=%q\ngot:\n%s\nwant:\n%s", authState, guests, mcp, got, want)
				}
			}
		}
	}
}

// R-WCON-Q8F5 R-W90Y-KX72 R-WA8U-YOXR R-F57P-OIHU R-EO54-BQ44
func TestRenderGuestsIdentityAndRequestDirectives(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "auth", "app = 'auth'\n")
	writeManifest(t, root, "notes", "app = 'notes'\nguests = true\n")
	got, err := nginx.Render(context.Background(), enabledEnv(root), "space.example.test", "")
	if err != nil {
		t.Fatal(err)
	}
	block := serverBlockFor(t, string(got), "notes.space.example.test")
	for _, location := range []string{"= /_ikigenba/check", "= /_ikigenba/check/open"} {
		check := locationFor(t, block, location)
		endpoint := strings.TrimPrefix(location, "= /_ikigenba")
		for _, directive := range []string{
			"internal;", "proxy_pass              http://unix:/run/ikigenba/auth.sock:" + endpoint + ";",
			"proxy_pass_request_body off;", "proxy_set_header        Content-Length \"\";",
			"proxy_set_header        X-User-Id    \"\";", "proxy_set_header        X-User-Email \"\";",
			"proxy_set_header        X-Request-Id $request_id;",
			"proxy_set_header        X-Original-Method $request_method;",
			"proxy_set_header        X-Original-Host   $host;", "proxy_set_header        X-Original-URI    $request_uri;",
		} {
			if strings.Count(check, directive) != 1 {
				t.Fatalf("%s lacks unique %q: %s", location, directive, check)
			}
		}
	}
	for _, location := range []string{"/", "= /mcp", "^~ /mcp/", "~ /(info/refs|git-upload-pack|git-receive-pack)$"} {
		section := locationFor(t, block, location)
		check := "/_ikigenba/check"
		if location == "/" {
			check += "/open"
			if strings.Contains(section, "error_page") {
				t.Fatalf("guest browser intercepts status: %s", section)
			}
		}
		for _, directive := range []string{
			"auth_request     " + check + ";", "auth_request_set $auth_user_id    $upstream_http_x_user_id;",
			"auth_request_set $auth_user_email $upstream_http_x_user_email;",
			"proxy_set_header X-User-Id         $auth_user_id;", "proxy_set_header X-User-Email      $auth_user_email;",
			"proxy_set_header X-Request-Id      $request_id;",
		} {
			if strings.Count(section, directive) != 1 {
				t.Fatalf("%s lacks unique %q: %s", location, directive, section)
			}
		}
		for _, header := range []string{"X-User-Id", "X-User-Email", "X-Request-Id"} {
			if strings.Count(section, header) != 1 {
				t.Fatalf("%s has additional %s directive: %s", location, header, section)
			}
		}
	}
	if strings.Count(block, "return 302 https://auth.space.example.test/?return=$scheme://$host$request_uri;") != 1 {
		t.Fatal("guest block lacks authenticator redirect")
	}
}
