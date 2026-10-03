package gateway_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

// R-O7HP-Q7NU R-O8PM-3ZEJ R-O9XI-HR58
func TestGitInstructionsFollowServicesWithRequestScope(t *testing.T) {
	for _, tc := range []struct {
		name, host, scope string
		proto             []string
	}{
		{name: "default", host: "mcp.space.test:8443", scope: "https://*.space.test:8443"},
		{name: "http", host: "mcp.space.test", proto: []string{"http"}, scope: "http://*.space.test"},
		{name: "https", host: "mcp.space.test", proto: []string{"https"}, scope: "https://*.space.test"},
		{name: "empty-proto", host: "mcp.space.test", proto: []string{""}, scope: "https://*.space.test"},
		{name: "uppercase-proto", host: "mcp.space.test", proto: []string{"HTTP"}, scope: "https://*.space.test"},
		{name: "multiple-proto", host: "mcp.space.test", proto: []string{"http, https"}, scope: "https://*.space.test"},
		{name: "spaced-proto", host: "mcp.space.test", proto: []string{" https"}, scope: "https://*.space.test"},
		{name: "first-proto", host: "mcp.space.test:8080", proto: []string{"http", "https"}, scope: "http://*.space.test:8080"},
		{name: "one-label", host: "mcp.mcp.space.test:8443", scope: "https://*.mcp.space.test:8443"},
		{name: "no-prefix", host: "space.test:8443", scope: "https://*.space.test:8443"},
		{name: "uppercase-prefix", host: "MCP.space.test:8443", scope: "https://*.MCP.space.test:8443"},
		{name: "embedded-prefix", host: "other.mcp.space.test", scope: "https://*.other.mcp.space.test"},
		{name: "bare-prefix", host: "mcp.", scope: "https://*.mcp."},
		{name: "bare-name", host: "mcp", scope: "https://*.mcp"},
		{name: "empty-port", host: "mcp.space.test:", scope: "https://*.space.test:"},
		{name: "nonnumeric-port", host: "mcp.space.test:word", scope: "https://*.space.test:word"},
		{name: "port-after-prefix", host: "mcp.:8443", scope: "https://*.:8443"},
	} {
		for _, state := range []string{"empty", "services", "malformed"} {
			t.Run(tc.name+"/"+state, func(t *testing.T) {
				path := ""
				if state != "empty" {
					path = servicesFile(t, []map[string]any{service("alpha", "installed service", true, true), service("zeta", "disabled service", false, true)})
					if state == "malformed" {
						if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
				}
				var banner page.Banner
				cfg := pageConfig(t, path, func(u page.User) page.Banner {
					banner = basicBanner(u)
					return banner
				})
				r := pageRequest("GET", "/")
				r.Host = tc.host
				r.Header["X-Forwarded-Proto"] = tc.proto
				w := answer(gateway.Handler(cfg), r)
				if w.Code != 200 {
					t.Fatalf("status %d", w.Code)
				}
				written := strings.Replace(w.Body.String(), renderAppkit(t, "banner", banner), "", 1)
				written = strings.Replace(written, renderAppkit(t, "footer", banner), "", 1)
				command := "git config --global credential." + tc.scope + `.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'`
				helper := oneTag(t, attributed(written, "id", "git-helper"))
				if helper.name != "code" {
					t.Fatalf("git-helper is %s, want code", helper.name)
				}
				checkContent(t, written, helper, command)
				var gitHeadings []markupTag
				for _, heading := range readTags(written, "h2", false) {
					content, found := elementContent(written, heading)
					if found && normalise(content) == "Git" {
						gitHeadings = append(gitHeadings, heading)
					}
				}
				heading := oneTag(t, gitHeadings)
				serviceHooks := append(attributed(written, "id", "mcp-services"), attributed(written, "id", "no-services")...)
				for _, hook := range serviceHooks {
					if heading.start <= hook.start {
						t.Fatal("Git heading precedes services")
					}
					if hook.name == "table" {
						end := firstTag(t, readTags(written[hook.end:], "table", true))
						if heading.start < hook.end+end.end {
							t.Fatal("Git heading precedes end of services table")
						}
					}
				}
				bodyEnd := firstTag(t, readTags(written[heading.end:], "body", true))
				text := normalise(written[heading.start : heading.end+bodyEnd.start])
				for _, phrase := range []string{
					"Git",
					"The same token works for git over HTTPS. Keep it in the environment variable IKIGENBA_TOKEN and give it to git with this credential helper, which reads the variable whenever git asks:",
					command,
					"Or set GIT_ASKPASS to a program that prints $IKIGENBA_TOKEN.",
					"Never put the token in a remote's URL, and never use credential.helper store: both write it to disk in plain text.",
				} {
					i := strings.Index(text, phrase)
					if i < 0 {
						t.Fatalf("missing/out of order Git phrase %q in %q", phrase, text)
					}
					text = text[i+len(phrase):]
				}
			})
		}
	}
}
