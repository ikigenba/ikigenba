package gateway_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"html"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"unicode"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	assets "github.com/ikigenba/ikigenba/mcp"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func pageConfig(t *testing.T, path string, banner func(page.User) page.Banner) gateway.Config {
	t.Helper()
	t.Setenv(services.Variable, "")
	writer, _ := handlerTelemetry(t, nil)
	return gateway.Config{ServicesPath: path, Banner: banner, MCP: gateway.NewServer("test", writer), Telemetry: writer}
}

type markupTag struct {
	name       string
	start, end int
	attrs      map[string][]string
	bareAttrs  map[string]int
}

func asciiSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func asciiAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func readTags(s string, name string, closing bool) []markupTag {
	var tags []markupTag
	for offset := 0; offset < len(s); {
		start := strings.IndexByte(s[offset:], '<')
		if start < 0 {
			break
		}
		start += offset
		end := strings.IndexByte(s[start:], '>')
		if end < 0 {
			break
		}
		end += start + 1
		i := start + 1
		isClosing := i < end && s[i] == '/'
		if isClosing {
			i++
		}
		begin := i
		for i < end && (asciiAlnum(s[i]) || s[i] == '-') {
			i++
		}
		tagName := strings.ToLower(s[begin:i])
		matches := name == "" && tagName != "" || len(name)+begin < end && strings.EqualFold(s[begin:begin+len(name)], name) && !asciiAlnum(s[begin+len(name)])
		if matches && closing == isClosing {
			attrs := make(map[string][]string)
			bareAttrs := make(map[string]int)
			for i < end {
				white := i
				for i < end && asciiSpace(s[i]) {
					i++
				}
				if i == white {
					break
				}
				attrStart := i
				for i < end && !asciiSpace(s[i]) && !strings.ContainsRune("\"'<>/=", rune(s[i])) {
					i++
				}
				if i == attrStart {
					break
				}
				attrName := strings.ToLower(s[attrStart:i])
				if i < end && s[i] == '=' {
					i++
					if i >= end || s[i] != '"' {
						break
					}
					i++
					valueStart := i
					for i < end && s[i] != '"' {
						i++
					}
					if i >= end {
						break
					}
					attrs[attrName] = append(attrs[attrName], html.UnescapeString(s[valueStart:i]))
					i++
				} else {
					bareAttrs[attrName]++
				}
			}
			tags = append(tags, markupTag{name: tagName, start: start, end: end, attrs: attrs, bareAttrs: bareAttrs})
		}
		offset = end
	}
	return tags
}

func elementContent(s string, tag markupTag) (string, bool) {
	ends := readTags(s[tag.end:], tag.name, true)
	if len(ends) == 0 {
		return "", false
	}
	return s[tag.end : tag.end+ends[0].start], true
}

func normalise(s string) string {
	for {
		start := strings.IndexByte(s, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(s[start:], '>')
		if end < 0 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+1:]
	}
	return strings.Join(strings.FieldsFunc(html.UnescapeString(s), unicode.IsSpace), " ")
}

func visibleText(s string) string {
	bodies := readTags(s, "body", false)
	if len(bodies) == 0 {
		return ""
	}
	content, found := elementContent(s, bodies[0])
	if !found {
		return ""
	}
	return normalise(content)
}

func attributed(s, name, value string) []markupTag {
	var selected []markupTag
	for _, tag := range readTags(s, "", false) {
		for _, v := range tag.attrs[name] {
			if v == value {
				selected = append(selected, tag)
				break
			}
		}
	}
	return selected
}

func oneTag(t *testing.T, tags []markupTag) markupTag {
	t.Helper()
	if len(tags) != 1 {
		t.Fatalf("expected one tag, got %#v", tags)
	}
	return tags[0]
}

func firstTag(t *testing.T, tags []markupTag) markupTag {
	t.Helper()
	if len(tags) == 0 {
		t.Fatal("expected at least one tag")
	}
	return tags[0]
}

func ofName(tags []markupTag, name string) []markupTag {
	var selected []markupTag
	for _, tag := range tags {
		if tag.name == name {
			selected = append(selected, tag)
		}
	}
	return selected
}

func checkContent(t *testing.T, s string, tag markupTag, expected string) {
	t.Helper()
	content, found := elementContent(s, tag)
	if !found || normalise(content) != expected {
		t.Fatalf("%s content %q, found=%v want %q", tag.name, normalise(content), found, expected)
	}
}

func renderAppkit(t *testing.T, name string, b page.Banner) string {
	t.Helper()
	var out bytes.Buffer
	if err := page.Templates().ExecuteTemplate(&out, name, b); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func feedbackScripts(s string) []markupTag {
	var selected []markupTag
	for _, tag := range readTags(s, "script", false) {
		if slices.Contains(tag.attrs["src"], "/_appkit/feedback.js") {
			selected = append(selected, tag)
		}
	}
	return selected
}

func TestMarkupFeedbackScriptSelection(t *testing.T) {
	for _, tc := range []struct {
		markup string
		count  int
	}{
		{markup: `<script src="/_appkit/feedback.js" defer>`, count: 1},
		{markup: `<script-extra src="/_appkit/feedback.js" defer>`, count: 1},
		{markup: `<ScRiPt-EXTRA SRC="/_appkit/feedback.js" defer>`, count: 1},
		{markup: `<script src="/_appkit/feedback.js" src="/_appkit/feedback.js" defer>`, count: 1},
		{markup: `<script-extra src="/_appkit/feedback.js"><script src="/_appkit/feedback.js">`, count: 2},
		{markup: `<script1 src="/_appkit/feedback.js" defer>`, count: 0},
		{markup: `<script src defer>`, count: 0},
		{markup: `<script src="other.js" defer>`, count: 0},
	} {
		if got := len(feedbackScripts(tc.markup)); got != tc.count {
			t.Errorf("feedback script count for %q = %d, want %d", tc.markup, got, tc.count)
		}
	}
}

// R-CZG0-KNZV
func TestMarkupBareAttributeOccurrences(t *testing.T) {
	for _, tc := range []struct {
		name      string
		markup    string
		bareDefer int
		bareSrc   int
		deferVals []string
	}{
		{name: "bare", markup: `<script defer>`, bareDefer: 1},
		{name: "case and duplicates", markup: `<SCRIPT DeFeR defer DEFER="">`, bareDefer: 2, deferVals: []string{""}},
		{name: "valued only", markup: `<script defer="defer">`, deferVals: []string{"defer"}},
		{name: "empty valued source", markup: `<script src="" defer>`, bareDefer: 1},
		{name: "bare source", markup: `<script src defer>`, bareDefer: 1, bareSrc: 1},
		{name: "all ASCII whitespace", markup: "<script\tdefer\nsrc\rdefer\fdefer\vdefer>", bareDefer: 4, bareSrc: 1},
		{name: "whole attribute name", markup: `<script defer-extra srcset>`, bareDefer: 0},
		{name: "non ASCII whitespace", markup: "<script\u00a0defer>", bareDefer: 0},
		{name: "missing separating whitespace", markup: `<script src="local"defer>`, bareDefer: 0},
		{name: "unquoted value stops reading", markup: `<script defer=plain src>`, bareDefer: 0},
		{name: "single quoted value stops reading", markup: `<script src='local' defer>`, bareDefer: 0},
		{name: "whitespace before equals", markup: `<script defer ="" src>`, bareDefer: 1},
		{name: "slash stops reading", markup: `<script defer / src>`, bareDefer: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tag := oneTag(t, readTags(tc.markup, "script", false))
			if tag.bareAttrs["defer"] != tc.bareDefer || tag.bareAttrs["src"] != tc.bareSrc || !slices.Equal(tag.attrs["defer"], tc.deferVals) {
				t.Fatalf("bare attributes=%v valued attributes=%v; want defer bare=%d src bare=%d defer values=%q", tag.bareAttrs, tag.attrs, tc.bareDefer, tc.bareSrc, tc.deferVals)
			}
		})
	}
}

// R-S31L-RMZB R-S49I-5EQ0 R-S5HE-J6GP R-S6PA-WY7E R-S7X7-APY3
// R-TDDS-AX5P R-S953-OHOS R-SAD0-29FH
// R-SV3A-KD1A R-SWB6-Y4RZ R-SXJ3-BWIO R-D1VT-C7H9 R-D33P-PZ7Y R-T16S-H7QR
// R-KYQ9-8QIO
// R-UDJA-V6WH R-RLCU-FECE R-RP0J-KPKH R-RQ8F-YHB6 R-RSO8-Q0SK
// R-RNSN-6XTS R-RRGC-C91V R-T4UH-MIYU R-TC5V-X5F0
func TestPlainPageMarkupHooksAndText(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "services"}[installed], func(t *testing.T) {
			entries := []map[string]any{}
			if installed {
				entries = append(entries, service("zeta", "Disabled <service> &amp;", false, true), service("alpha", "Enabled & ready", true, true), service("other", "not MCP", true, false), service("mcp", "gateway", true, true))
			}
			cfg := pageConfig(t, servicesFile(t, entries), basicBanner)
			r := pageRequest("GET", "/")
			w := answer(gateway.Handler(cfg), r)
			if w.Code != 200 {
				t.Fatalf("status %d", w.Code)
			}
			body := w.Body.String()
			b := basicBanner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: "https://auth.space.test/", LogoutURL: "https://auth.space.test/logout"})
			banner, footer := renderAppkit(t, "banner", b), renderAppkit(t, "footer", b)
			bodyStart := firstTag(t, readTags(body, "body", false))
			bodyEnds := readTags(body, "body", true)
			if len(bodyEnds) == 0 {
				t.Fatal("body end missing")
			}
			bannerStart := strings.Index(body, banner)
			footerStart := strings.LastIndex(body, footer)
			if bannerStart < bodyStart.end || footerStart < bannerStart+len(banner) || footerStart+len(footer) > bodyEnds[len(bodyEnds)-1].start {
				t.Fatal("banner/footer out of body order")
			}
			if strings.Trim(body[bodyStart.end:bannerStart], " \t\n\r\f\v") != "" || strings.Trim(body[footerStart+len(footer):bodyEnds[len(bodyEnds)-1].start], " \t\n\r\f\v") != "" {
				t.Fatal("markup outside banner/footer boundaries")
			}
			written := body[:bannerStart] + body[bannerStart+len(banner):footerStart] + body[footerStart+len(footer):]
			writtenBody := firstTag(t, readTags(written, "body", false))
			title := oneTag(t, readTags(written, "title", false))
			titleEnd := oneTag(t, readTags(written, "title", true))
			if title.start >= titleEnd.start || titleEnd.end > writtenBody.start {
				t.Fatal("title position")
			}
			checkContent(t, written, title, gateway.ServiceName)
			stylesheet := oneTag(t, ofName(attributed(written, "rel", "stylesheet"), "link"))
			if stylesheet.name != "link" || stylesheet.end > writtenBody.start || !slices.Contains(stylesheet.attrs["href"], "/_appkit/theme.css") {
				t.Fatalf("stylesheet %#v", stylesheet)
			}
			var icons []markupTag
			for _, link := range readTags(written, "link", false) {
				if slices.Contains(link.attrs["rel"], "icon") {
					icons = append(icons, link)
				}
			}
			icon := oneTag(t, icons)
			if icon.end > writtenBody.start || !slices.Contains(icon.attrs["href"], "/_appkit/favicon.svg") || !slices.Contains(icon.attrs["type"], "image/svg+xml") {
				t.Fatalf("favicon %#v", icon)
			}
			viewport := oneTag(t, ofName(attributed(written, "name", "viewport"), "meta"))
			if viewport.name != "meta" || viewport.end > writtenBody.start || !slices.Contains(viewport.attrs["content"], "width=device-width, initial-scale=1") {
				t.Fatalf("viewport %#v", viewport)
			}
			feedback := oneTag(t, feedbackScripts(written))
			if feedback.end > writtenBody.start || len(feedback.attrs["defer"]) == 0 && feedback.bareAttrs["defer"] == 0 {
				t.Fatalf("feedback script position or defer: %#v", feedback)
			}
			for _, script := range readTags(written, "script", false) {
				if script.bareAttrs["src"] != 0 {
					t.Fatalf("bare script source: %#v", script)
				}
				for _, src := range script.attrs["src"] {
					if src != "/_appkit/feedback.js" {
						t.Fatalf("unexpected script source %q", src)
					}
				}
			}
			for _, tag := range readTags(written, "", false) {
				if len(tag.attrs["style"]) != 0 || len(tag.attrs["srcset"]) != 0 {
					t.Fatalf("inline load attributes %#v", tag)
				}
				if tag.name == "a" {
					continue
				}
				for _, attr := range []string{"href", "src", "poster", "data", "background", "manifest"} {
					for _, v := range tag.attrs[attr] {
						local := v == "/" || len(v) > 1 && v[0] == '/' && v[1] != '/' && v[1] != '\\'
						if !local {
							t.Fatalf("external resource %s=%q", attr, v)
						}
					}
				}
			}
			h1 := oneTag(t, readTags(written, "h1", false))
			checkContent(t, written, h1, "Connect MCP Client")
			endpoint := "https://mcp.space.test:8443/mcp"
			endpointTag := oneTag(t, attributed(written, "id", "endpoint"))
			if endpointTag.name != "code" {
				t.Fatal("endpoint not code")
			}
			checkContent(t, written, endpointTag, endpoint)
			if len(readTags(written, "a", false)) != 0 {
				t.Fatal("link in written markup")
			}
			headings := readTags(written, "h2", false)
			if len(headings) != 3 {
				t.Fatalf("headings: %d", len(headings))
			}
			ids := []string{"claude-code", "codex", "endpoint"}
			commands := []string{"claude mcp add --scope project --transport http space-test " + endpoint, "codex mcp add space-test --url " + endpoint, endpoint}
			for i, heading := range headings {
				checkContent(t, written, heading, []string{"Claude Code", "Codex", "Other clients"}[i])
				code := oneTag(t, attributed(written, "id", ids[i]))
				if code.name != "code" || heading.start <= h1.start || code.start <= heading.start || i < 2 && code.start >= headings[i+1].start {
					t.Fatal("section hooks out of order")
				}
				checkContent(t, written, code, commands[i])
			}
			var blocks []markupTag
			for _, tag := range readTags(written, "", false) {
				for _, classes := range tag.attrs["class"] {
					if slices.Contains(strings.FieldsFunc(classes, func(r rune) bool { return strings.ContainsRune(" \t\n\r\f\v", r) }), "secret") {
						blocks = append(blocks, tag)
						break
					}
				}
			}
			if len(blocks) != 3 {
				t.Fatalf("copy blocks: %d", len(blocks))
			}
			for i, block := range blocks {
				content, found := elementContent(written, block)
				if !found {
					t.Fatal("copy block content missing")
				}
				oneTag(t, readTags(content, "code", false))
				button := oneTag(t, readTags(content, "button", false))
				if !slices.Contains(button.attrs["type"], "button") {
					t.Fatal("copy button type")
				}
				checkContent(t, content, button, "Copy")
				id := ids[i]
				oneTag(t, attributed(content, "id", id))
			}
			text := visibleText(written)
			if strings.Contains(text, normalise(r.Header.Get("X-User-Email"))) {
				t.Fatal("email in written visible text")
			}
			wantText := strings.Join([]string{"Connect MCP Client", "Claude Code", commands[0], "Copy", "Codex", commands[1], "Copy", "Other clients", endpoint, "Copy"}, " ")
			if text != wantText {
				t.Fatalf("visible text %q want %q", text, wantText)
			}

		})
	}
}

func basicBanner(u page.User) page.Banner {
	return page.Banner{Service: gateway.ServiceName, Version: "test", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
}

func servicesFile(t *testing.T, entries []map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "services.json")
	writeServices(t, path, entries)
	return path
}

func writeServices(t *testing.T, path string, entries []map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func service(name, description string, enabled, mcp bool) map[string]any {
	return map[string]any{"name": name, "description": description, "enabled": enabled, "mcp": mcp, "url": "", "socket": ""}
}

func pageRequest(method, path string) *http.Request {
	r := httptest.NewRequest("GET", "https://mcp.space.test:8443"+path, strings.NewReader("arbitrary body"))
	r.Method = method
	r.Header.Set("X-User-Id", "person")
	r.Header.Set("X-User-Email", "person@example.test")
	return r
}

func answer(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// R-RF9C-IJMX R-SK47-4FD1
func TestGatewayTemplateSet(t *testing.T) {
	templates, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	if templates.Lookup("connect") == nil {
		t.Fatal("connect template absent")
	}
	var out bytes.Buffer
	if err := templates.ExecuteTemplate(&out, "connect", map[string]any{"Banner": basicBanner(page.User{}), "Endpoint": "https://mcp.example.test/mcp", "Server": "example-test"}); err != nil {
		t.Fatal(err)
	}
}

// R-SNRW-9QL4 R-RIX1-NUV0 R-SBKW-G166 R-SCSS-TSWV R-SE0P-7KNK R-SF8L-LCE9 R-SHOE-CVVN
func TestConnectExactlyRendersRequestData(t *testing.T) {
	for _, proto := range []string{"", "http", "https", "HTTP", "http, https", " https"} {
		for _, host := range []string{"mcp.space.test:8443", "mcp.space.test:", "mcp.", "space.test:word", "mcp.mcp.space.test", "mcp.space<&>.test:8443"} {
			for _, authURL := range []string{"", "https://accounts.test/base/"} {
				t.Run(proto+"/"+host+"/"+authURL, func(t *testing.T) {
					first := service("zeta", "<b> &amp; text", false, true)
					entries := []map[string]any{first, service("alpha", "enabled", true, true), service("zeta", "ignored duplicate", true, true), service("mcp", "gateway", true, true), service("other", "not MCP", true, false)}
					auth := service("auth", "authentication", true, false)
					auth["url"] = authURL
					entries = append(entries, auth)
					path := servicesFile(t, entries)
					var users []page.User
					cfg := pageConfig(t, path, func(u page.User) page.Banner { users = append(users, u); return basicBanner(u) })
					r := pageRequest("GET", "/?query=ignored")
					r.Host = host
					r.Header.Set("X-Forwarded-Proto", proto)
					r.Header.Set("X-User-Email", "  caller<&>@example.test ")
					w := answer(gateway.Handler(cfg), r)
					scheme := "https"
					if proto == "http" {
						scheme = "http"
					}
					spaces := map[string]string{"mcp.space.test:8443": "space.test", "mcp.space.test:": "space.test", "mcp.": "mcp.", "space.test:word": "space.test:word", "mcp.mcp.space.test": "mcp.space.test", "mcp.space<&>.test:8443": "space<&>.test"}
					origin := authURL
					if origin == "" {
						origin = scheme + "://auth." + spaces[host]
					}
					u := page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: origin + "/", LogoutURL: origin + "/logout"}
					if !slices.Contains(users, u) {
						t.Fatalf("banner calls: %#v want %#v", users, u)
					}
					endpoint := scheme + "://" + host + "/mcp"
					data := map[string]any{"Banner": basicBanner(u), "Endpoint": endpoint, "Server": map[string]string{"mcp.space.test:8443": "space-test", "mcp.space.test:": "space-test", "mcp.": "mcp-", "space.test:word": "space-test-word", "mcp.mcp.space.test": "mcp-space-test", "mcp.space<&>.test:8443": "space----test"}[host]}
					set, err := page.Templates().ParseFS(assets.Assets(), "*.html")
					if err != nil {
						t.Fatal(err)
					}
					var expected bytes.Buffer
					if err := set.ExecuteTemplate(&expected, "connect", data); err != nil {
						t.Fatal(err)
					}
					if w.Code != 200 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || w.Body.String() != expected.String() {
						t.Fatalf("response differs: status=%d headers=%v\n%s\nwant\n%s", w.Code, w.Header(), w.Body.String(), expected.String())
					}
				})
			}
		}
	}
}

// R-SOZS-NIBT
func TestConnectHEADMatchesGET(t *testing.T) {
	h := gateway.Handler(pageConfig(t, servicesFile(t, []map[string]any{service("alpha", "description", true, true)}), basicBanner))
	get := answer(h, pageRequest("GET", "/"))
	head := answer(h, pageRequest("HEAD", "/"))
	if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
		t.Fatalf("GET=%d %v HEAD=%d %v %q", get.Code, get.Header(), head.Code, head.Header(), head.Body.String())
	}
}

// R-YIQW-P5S2
func TestConnectRejectsOtherMethods(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "CUSTOM"} {
		for _, identity := range []string{"signed", "absent", "empty"} {
			r := pageRequest(method, "/")
			setPageIdentity(r, identity)
			w := answer(h, r)
			if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0 {
				t.Fatalf("%s: %d %v %q", method, w.Code, w.Header(), w.Body.String())
			}
		}
	}
}

// R-SMJZ-VYUF R-RK4Y-1MLP
func TestUnknownPathsReturnExact404(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, path := range []string{"/_appkit", "/assets/", "/assets/connect.html", "/logout", "/index.html", "/setup", "/setup.txt", "/setup.sh", "/.well-known", "/.well-known/", "/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resourcex", "/setup.txt/", "/setup.sh/", "/setup.txt/x", "/setup.sh/x", "//", "/nope/", "/x/../", "/./", "/x/%2e%2e/", "/%61ssets/theme.css"} {
		for _, method := range []string{"GET", "HEAD", "POST", "OPTIONS"} {
			for _, identity := range []string{"signed", "absent", "empty"} {
				r := pageRequest(method, path+"?ignored=yes")
				setPageIdentity(r, identity)
				w := answer(h, r)
				body := "not found\n"
				if method == "HEAD" {
					body = ""
				}
				if w.Code != 404 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != body || w.Header().Get("Location") != "" {
					t.Fatalf("%s %s: %d %v %q", method, path, w.Code, w.Header(), w.Body.String())
				}
			}
		}
	}
}

// R-SSNH-STJW R-R3W9-7NFO
func TestNonMCPRoutesNeitherSetCookiesNorContactBackends(t *testing.T) {
	dir, err := os.MkdirTemp("", "mcp-page-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	socket := filepath.Join(dir, "backend.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Error(err)
		}
	})
	entry := service("alpha", "backend", true, true)
	entry["socket"] = socket
	h := gateway.Handler(pageConfig(t, servicesFile(t, []map[string]any{entry}), basicBanner))
	for _, path := range []string{"/", "/setup.txt", "/setup.sh", "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp/a,,b", "/_appkit/theme.css", "/_appkit/../theme.css", "/logout", "/assets/connect.html", "/unknown"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, user := range []string{"person", ""} {
				r := pageRequest(method, path)
				r.Header.Set("X-User-Id", user)
				w := answer(h, r)
				if len(w.Header().Values("Set-Cookie")) != 0 {
					t.Fatalf("cookie on %s %s", method, path)
				}
				conn, err := ln.SyscallConn()
				if err != nil {
					t.Fatal(err)
				}
				var acceptErr error
				var accepted int
				if err := conn.Control(func(fd uintptr) {
					accepted, _, acceptErr = syscall.Accept4(int(fd), syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
				}); err != nil {
					t.Fatal(err)
				}
				if acceptErr == nil {
					_ = syscall.Close(accepted)
					t.Fatalf("backend contacted on %s %s", method, path)
				}
				if !errors.Is(acceptErr, syscall.EAGAIN) {
					t.Fatalf("accept: %v", acceptErr)
				}
			}
		}
	}
}

// R-IA1Y-Z12O
func TestConnectWrittenMarkupIgnoresOtherServices(t *testing.T) {
	for _, authState := range []string{"absent", "empty", "url"} {
		var authEntries []map[string]any
		if authState != "absent" {
			auth := service("auth", "authentication", true, false)
			if authState == "url" {
				auth["url"] = "http://accounts.test/base/"
			}
			authEntries = append(authEntries, auth)
		}
		cfg := pageConfig(t, "", basicBanner)
		var want string
		for i, other := range [][]map[string]any{
			{},
			{service("alpha", "Enabled & ready", true, true), service("zeta", "Disabled <service> &amp;", false, true)},
			{service("zeta", "Different service", true, false), service("mcp", "gateway", true, true), service("alpha", "Unavailable", false, true), service("alpha", "Duplicate", true, true)},
		} {
			entries := append(append([]map[string]any{}, authEntries...), other...)
			cfg.ServicesPath = servicesFile(t, entries)
			r := pageRequest("GET", "/")
			body := answer(gateway.Handler(cfg), r).Body.String()
			profileURL := "https://auth.space.test/"
			if authState == "url" {
				profileURL = "http://accounts.test/base//"
			}
			b := basicBanner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: profileURL, LogoutURL: strings.TrimSuffix(profileURL, "/") + "/logout"})
			written := strings.Replace(body, renderAppkit(t, "banner", b), "", 1)
			written = strings.Replace(written, renderAppkit(t, "footer", b), "", 1)
			if i == 0 {
				want = written
			} else if written != want {
				t.Fatalf("%s: written markup depends on services", authState)
			}
		}
	}
}

// R-6P2M-PC2C R-RHP5-A34B
func TestConnectServerNamesAndCommands(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, tc := range []struct{ host, server string }{
		{"mcp.sbx.ikigenba.dev", "sbx-ikigenba-dev"},
		{"mcp.sbx.ikigenba.dev:443", "sbx-ikigenba-dev"},
		{"mcp.SBX.Ikigenba.Dev", "sbx-ikigenba-dev"},
		{"MCP.sbx.ikigenba.dev", "mcp-sbx-ikigenba-dev"},
		{"mcp.wip-mcp.localhost:7403", "wip-mcp-localhost"},
		{"mcp.Az09_-é界.test:12", "az09-----test"},
		{"mcp.a\xff\xfe.test", "a---test"},
		{"mcp.mcp.A.Test:", "mcp-a-test"},
		{"mcp.a.test:port", "a-test-port"},
		{"mcp.", "mcp-"},
	} {
		for _, proto := range []string{"http", "https", "HTTPS", ""} {
			r := pageRequest("GET", "/")
			r.Host = tc.host
			r.Header.Set("X-Forwarded-Proto", proto)
			body := answer(h, r).Body.String()
			scheme := "https"
			if proto == "http" {
				scheme = "http"
			}
			endpoint := scheme + "://" + tc.host + "/mcp"
			checkContent(t, body, oneTag(t, attributed(body, "id", "claude-code")), "claude mcp add --scope project --transport http "+tc.server+" "+endpoint)
			checkContent(t, body, oneTag(t, attributed(body, "id", "codex")), "codex mcp add "+tc.server+" --url "+endpoint)
		}
	}
}
