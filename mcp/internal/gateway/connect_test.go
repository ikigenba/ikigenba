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
				}
			}
			tags = append(tags, markupTag{name: tagName, start: start, end: end, attrs: attrs})
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

// R-S31L-RMZB R-S49I-5EQ0 R-S5HE-J6GP R-S6PA-WY7E R-S7X7-APY3
// R-TDDS-AX5P R-S953-OHOS R-SAD0-29FH
// R-SV3A-KD1A R-SWB6-Y4RZ R-SXJ3-BWIO R-SYQZ-PO9D R-T16S-H7QR
// R-T2EO-UZHG R-T3ML-8R85 R-T4UH-MIYU R-96RZ-EUVY R-TC5V-X5F0
// R-T7AA-E2G8 R-T8I6-RU6X R-T9Q3-5LXM
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
			viewport := oneTag(t, ofName(attributed(written, "name", "viewport"), "meta"))
			if viewport.name != "meta" || viewport.end > writtenBody.start || !slices.Contains(viewport.attrs["content"], "width=device-width, initial-scale=1") {
				t.Fatalf("viewport %#v", viewport)
			}
			if len(readTags(written, "script", false)) != 0 {
				t.Fatal("page script")
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
			checkContent(t, written, oneTag(t, readTags(written, "h1", false)), "Connect an MCP client")
			endpoint := "https://mcp.space.test:8443/mcp"
			endpointTag := oneTag(t, attributed(written, "id", "endpoint"))
			if endpointTag.name != "code" {
				t.Fatal("endpoint not code")
			}
			checkContent(t, written, endpointTag, endpoint)
			profile := oneTag(t, attributed(written, "id", "profile-link"))
			if profile.name != "a" || !slices.Contains(profile.attrs["href"], b.ProfileURL) {
				t.Fatalf("profile link %#v", profile)
			}
			checkContent(t, written, profile, "your profile")
			text := visibleText(written)
			if strings.Contains(text, normalise(r.Header.Get("X-User-Email"))) {
				t.Fatal("email in written visible text")
			}
			for _, phrase := range []string{"Connect an MCP client", "Add this server to your client as a remote (Streamable HTTP) MCP server.", endpoint, "Every request must send the header Authorization: Bearer <token>. Create a token on your profile.", "Services", "The endpoint above reaches every service. To limit a client to some of them, append their names, separated by commas: " + endpoint + "/a,b."} {
				i := strings.Index(text, phrase)
				if i < 0 {
					t.Fatalf("missing/out of order visible phrase %q in %q", phrase, text)
				}
				text = text[i+len(phrase):]
			}
			if !installed {
				if len(readTags(written, "table", false)) != 0 || len(attributed(written, "id", "mcp-services")) != 0 {
					t.Fatal("table with empty services")
				}
				empty := oneTag(t, attributed(written, "id", "no-services"))
				if empty.name != "p" {
					t.Fatal("empty services not p")
				}
				checkContent(t, written, empty, "No MCP services are installed.")
				return
			}
			table := oneTag(t, attributed(written, "id", "mcp-services"))
			if table.name != "table" || len(attributed(written, "id", "no-services")) != 0 || strings.Contains(visibleText(written), "No MCP services are installed.") {
				t.Fatal("incorrect services table state")
			}
			content, found := elementContent(written, table)
			if !found {
				t.Fatal("table content absent")
			}
			ths := readTags(content, "th", false)
			if len(ths) != 4 {
				t.Fatalf("headers %d", len(ths))
			}
			for i, name := range []string{"Name", "Description", "Endpoint", "Status"} {
				checkContent(t, content, ths[i], name)
			}
			var rows []markupTag
			for _, row := range readTags(content, "tr", false) {
				if len(row.attrs["data-service"]) > 0 {
					rows = append(rows, row)
				}
			}
			if len(rows) != 2 {
				t.Fatalf("service rows %d", len(rows))
			}
			for i, expected := range []struct{ name, description, status, available string }{{"alpha", "Enabled & ready", "available", "true"}, {"zeta", "Disabled <service> &amp;", "disabled", "false"}} {
				row := rows[i]
				if !slices.Contains(row.attrs["data-service"], expected.name) || !slices.Contains(row.attrs["data-available"], expected.available) {
					t.Fatalf("row %#v", row)
				}
				rowBody, found := elementContent(content, row)
				if !found {
					t.Fatal("row content absent")
				}
				tds := readTags(rowBody, "td", false)
				if len(tds) != 4 {
					t.Fatalf("cells %d", len(tds))
				}
				for j, value := range []string{expected.name, expected.description, endpoint + "/" + expected.name, expected.status} {
					checkContent(t, rowBody, tds[j], value)
				}
				checkContent(t, rowBody, oneTag(t, readTags(rowBody, "code", false)), endpoint+"/"+expected.name)
				var badges []markupTag
				for _, span := range readTags(rowBody, "span", false) {
					hasBadge := false
					for _, classes := range span.attrs["class"] {
						for _, class := range strings.FieldsFunc(classes, func(r rune) bool { return strings.ContainsRune(" \t\n\r\f\v", r) }) {
							if class == "badge" {
								hasBadge = true
							}
						}
					}
					if hasBadge {
						badges = append(badges, span)
					}
				}
				badge := oneTag(t, badges)
				checkContent(t, rowBody, badge, expected.status)
				if expected.available == "true" {
					if len(badge.attrs["data-reason"]) != 0 {
						t.Fatal("reason on enabled service")
					}
				} else if !slices.Contains(badge.attrs["data-reason"], "disabled") {
					t.Fatal("disabled reason absent")
				}
			}
		})
	}
}

// R-T9Q3-5LXM
func TestUnreadableCataloguesShowEmptyServices(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed")
	if err := os.WriteFile(malformed, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", filepath.Join(dir, "absent"), dir, malformed} {
		w := answer(gateway.Handler(pageConfig(t, path, basicBanner)), pageRequest("GET", "/"))
		if w.Code != 200 || len(readTags(w.Body.String(), "table", false)) != 0 {
			t.Fatalf("invalid catalogue response %d %s", w.Code, w.Body.String())
		}
		p := oneTag(t, attributed(w.Body.String(), "id", "no-services"))
		if p.name != "p" {
			t.Fatal("empty hook not p")
		}
		checkContent(t, w.Body.String(), p, "No MCP services are installed.")
	}
}

func TestConnectReadsRewrittenServices(t *testing.T) {
	path := servicesFile(t, []map[string]any{service("alpha", "before", true, true)})
	h := gateway.Handler(pageConfig(t, path, basicBanner))
	first := answer(h, pageRequest("GET", "/"))
	writeServices(t, path, []map[string]any{service("beta", "after", false, true)})
	second := answer(h, pageRequest("GET", "/"))
	if len(attributed(first.Body.String(), "data-service", "alpha")) != 1 || len(attributed(second.Body.String(), "data-service", "alpha")) != 0 || len(attributed(second.Body.String(), "data-service", "beta")) != 1 {
		t.Fatal("catalogue not read afresh")
	}
}

// R-PG32-LER3 R-SNRW-9QL4
func TestConnectKeepsRequestCatalogueSnapshot(t *testing.T) {
	path := servicesFile(t, []map[string]any{service("alpha", "before banner", true, true)})
	cfg := pageConfig(t, path, func(u page.User) page.Banner {
		writeServices(t, path, []map[string]any{service("beta", "after banner", false, true)})
		return basicBanner(u)
	})
	w := answer(gateway.Handler(cfg), pageRequest("GET", "/"))
	if w.Code != 200 || len(attributed(w.Body.String(), "data-service", "alpha")) != 1 || len(attributed(w.Body.String(), "data-service", "beta")) != 0 {
		t.Fatal("page data was not built from the request's listed entries")
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

// R-SIWA-QNMC R-SK47-4FD1
func TestGatewayTemplateSet(t *testing.T) {
	templates, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	if templates.Lookup("connect") == nil {
		t.Fatal("connect template absent")
	}
	var out bytes.Buffer
	if err := templates.ExecuteTemplate(&out, "connect", map[string]any{"Banner": basicBanner(page.User{}), "Endpoint": "https://mcp.example.test/mcp", "Services": []any{}}); err != nil {
		t.Fatal(err)
	}
}

// R-SNRW-9QL4 R-PG32-LER3 R-SBKW-G166 R-SCSS-TSWV R-SE0P-7KNK R-SF8L-LCE9 R-SHOE-CVVN
func TestConnectExactlyRendersRequestData(t *testing.T) {
	for _, proto := range []string{"", "http", "https", "HTTP", "http, https", " https"} {
		for _, host := range []string{"mcp.space.test:8443", "mcp.space.test:", "mcp.", "space.test:word", "mcp.mcp.space.test"} {
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
					spaces := map[string]string{"mcp.space.test:8443": "space.test", "mcp.space.test:": "space.test", "mcp.": "mcp.", "space.test:word": "space.test:word", "mcp.mcp.space.test": "mcp.space.test"}
					origin := authURL
					if origin == "" {
						origin = scheme + "://auth." + spaces[host]
					}
					u := page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: origin + "/", LogoutURL: origin + "/logout"}
					if !slices.Contains(users, u) {
						t.Fatalf("banner calls: %#v want %#v", users, u)
					}
					endpoint := scheme + "://" + host + "/mcp"
					data := map[string]any{"Banner": basicBanner(u), "Endpoint": endpoint, "Services": []map[string]any{
						{"Name": "alpha", "Description": "enabled", "URL": endpoint + "/alpha", "Available": true, "Reason": ""},
						{"Name": "zeta", "Description": "<b> &amp; text", "URL": endpoint + "/zeta", "Available": false, "Reason": "disabled"},
					}}
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

// R-SQ7P-1A2I
func TestConnectRejectsOtherMethods(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "CUSTOM"} {
		w := answer(h, pageRequest(method, "/"))
		if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0 {
			t.Fatalf("%s: %d %v %q", method, w.Code, w.Header(), w.Body.String())
		}
	}
}

// R-SMJZ-VYUF R-SRFL-F1T7
func TestUnknownPathsReturnExact404(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, path := range []string{"/_appkit", "/assets/", "/assets/connect.html", "/logout", "/index.html", "//", "/nope/", "/x/../", "/./", "/x/%2e%2e/", "/%61ssets/theme.css"} {
		for _, method := range []string{"GET", "HEAD", "POST", "OPTIONS"} {
			w := answer(h, pageRequest(method, path+"?ignored=yes"))
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
	for _, path := range []string{"/", "/_appkit/theme.css", "/_appkit/../theme.css", "/logout", "/assets/connect.html", "/unknown"} {
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
