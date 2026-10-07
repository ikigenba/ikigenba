package web_test

import (
	"bytes"
	"context"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	at "github.com/ikigenba/ikigenba/appkit/telemetry"
	assets "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

type pageSpan struct {
	name       string
	start, end int
	endTag     bool
}

func pageASCIIAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
func pageASCIIWhitespace(c byte) bool { return strings.ContainsRune(" \t\n\r\f", rune(c)) }

// R-RLGM-QHJA: every tag assertion uses the declared spans.
func pageTags(s, name string, end bool) []pageSpan {
	var result []pageSpan
	prefix := "<" + name
	if end {
		prefix = "</" + name
	}
	for i := 0; i < len(s); i++ {
		if len(s)-i <= len(prefix) || !strings.EqualFold(s[i:i+len(prefix)], prefix) || pageASCIIAlnum(s[i+len(prefix)]) {
			continue
		}
		n := strings.IndexByte(s[i:], '>')
		if n >= 0 {
			result = append(result, pageSpan{name, i, i + n + 1, end})
		}
	}
	return result
}
func pageStarts(s string) []pageSpan {
	var result []pageSpan
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		j := i + 1
		for j < len(s) && (pageASCIIAlnum(s[j]) || s[j] == '-') {
			j++
		}
		if j == i+1 {
			continue
		}
		n := strings.IndexByte(s[j:], '>')
		if n >= 0 {
			result = append(result, pageSpan{strings.ToLower(s[i+1 : j]), i, j + n + 1, false})
		}
	}
	return result
}

// R-RMOJ-499Z: attributes are read left to right, without browser heuristics.
func pageAttrs(s string, tag pageSpan, name string) []string {
	return pageReadAttrs(s, tag, name, false)
}

// R-8DJG-V7V9: bare attributes use the same left-to-right reading.
func pageBareAttr(s string, tag pageSpan, name string) bool {
	return len(pageReadAttrs(s, tag, name, true)) != 0
}

func pageReadAttrs(s string, tag pageSpan, name string, bare bool) []string {
	var values []string
	i := tag.start + 1
	for i < tag.end-1 && (pageASCIIAlnum(s[i]) || s[i] == '-') {
		i++
	}
	for i < tag.end-1 {
		begin := i
		for i < tag.end-1 && pageASCIIWhitespace(s[i]) {
			i++
		}
		if i == begin {
			break
		}
		begin = i
		for i < tag.end-1 && !pageASCIIWhitespace(s[i]) && !strings.ContainsRune("\"'<>/=", rune(s[i])) {
			i++
		}
		if begin == i {
			break
		}
		attr := s[begin:i]
		if i < tag.end-1 && s[i] == '=' {
			i++
			if i >= tag.end-1 || s[i] != '"' {
				break
			}
			i++
			begin = i
			for i < tag.end-1 && s[i] != '"' {
				i++
			}
			if i == tag.end-1 {
				break
			}
			if !bare && strings.EqualFold(attr, name) {
				values = append(values, html.UnescapeString(s[begin:i]))
			}
			i++
		} else if bare && strings.EqualFold(attr, name) {
			values = append(values, "")
		}
	}
	return values
}

// R-RNWF-I10O
func pageContent(t *testing.T, s string, tag pageSpan) string {
	t.Helper()
	ends := pageTags(s[tag.end:], tag.name, true)
	if len(ends) == 0 {
		t.Fatalf("no end for %s", tag.name)
	}
	return s[tag.end : tag.end+ends[0].start]
}

// R-RP4B-VSRD
func pageNormal(s string) string {
	var out strings.Builder
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			out.WriteString(s)
			break
		}
		out.WriteString(s[:i])
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			break
		}
		s = s[i+j+1:]
	}
	return strings.Join(strings.Fields(html.UnescapeString(out.String())), " ")
}

// R-RQC8-9KI2
func pageVisible(t *testing.T, s string) string {
	t.Helper()
	tags := pageTags(s, "body", false)
	if len(tags) == 0 {
		return ""
	}
	ends := pageTags(s[tags[0].end:], "body", true)
	if len(ends) == 0 {
		return ""
	}
	return pageNormal(s[tags[0].end : tags[0].end+ends[0].start])
}
func pageFirst(t *testing.T, s, name string) pageSpan {
	t.Helper()
	tags := pageTags(s, name, false)
	if len(tags) == 0 {
		t.Fatalf("missing %s start tag", name)
	}
	return tags[0]
}
func pageIs(s string, tag pageSpan, name string) bool {
	after := tag.start + 1 + len(name)
	return after < tag.end && strings.EqualFold(s[tag.start+1:after], name) && !pageASCIIAlnum(s[after])
}
func pageOne(t *testing.T, s, name string) pageSpan {
	t.Helper()
	tags := pageTags(s, name, false)
	if len(tags) != 1 {
		t.Fatalf("%s count: %d", name, len(tags))
	}
	return tags[0]
}
func pageAttrIs(s string, tag pageSpan, name, value string) bool {
	for _, v := range pageAttrs(s, tag, name) {
		if v == value {
			return true
		}
	}
	return false
}
func pageClass(s string, tag pageSpan, name string) bool {
	for _, v := range pageAttrs(s, tag, "class") {
		for _, c := range strings.FieldsFunc(v, func(r rune) bool { return strings.ContainsRune(" \t\n\r\f", r) }) {
			if c == name {
				return true
			}
		}
	}
	return false
}
func pageHook(t *testing.T, s, id, name, class string) pageSpan {
	t.Helper()
	var found []pageSpan
	for _, tag := range pageStarts(s) {
		if pageAttrIs(s, tag, "id", id) {
			found = append(found, tag)
		}
	}
	if len(found) != 1 || !pageIs(s, found[0], name) || class != "" && !pageClass(s, found[0], class) {
		t.Fatalf("hook %s: %v", id, found)
	}
	found[0].name = name
	return found[0]
}
func pageTextIs(t *testing.T, s string, tag pageSpan, want string) {
	t.Helper()
	if got := pageNormal(pageContent(t, s, tag)); got != want {
		t.Fatalf("%s text: %q want %q", tag.name, got, want)
	}
}
func pageOrder(t *testing.T, text string, parts []string) {
	t.Helper()
	for _, part := range parts {
		i := strings.Index(text, part)
		if i < 0 {
			t.Fatalf("missing ordered text %q in %q", part, text)
		}
		text = text[i+len(part):]
	}
}
func pageHandler(t *testing.T, b page.Banner) http.Handler {
	t.Helper()
	t.Setenv(services.Variable, "")
	database, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "pages.db"), Migrations: assets.Migrations(), Now: func() time.Time { return time.Unix(100, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(database)
	w := at.New(at.Config{Service: web.ServiceName, Sink: new(at.Capture), Stderr: io.Discard, Now: func() time.Time { return time.Unix(100, 0) }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{3}, 8192))})
	t.Cleanup(func() {
		w.Shutdown(context.Background(), "test")
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return web.Handler(web.Config{Store: s, Telemetry: w, MCP: mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Telemetry: w}), Banner: func(u page.User) page.Banner {
		b.Email = u.Email
		b.ProfileURL = u.ProfileURL
		b.LogoutURL = u.LogoutURL
		return b
	}})
}

// R-S9UM-DWD6: all hook requests meet the plain request definition.
func pageBody(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	r := httptest.NewRequest("GET", "https://telemetry.example.test"+path, nil)
	r.Header.Set("X-User-Id", "reader")
	r.Header.Set("X-User-Email", "reader@example.test")
	r.Header.Set("X-Request-Id", "fixed-page-request")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	return out.Body.String()
}

// R-RXNM-K6Y8 R-RYVI-XYOX R-S03F-BQFM
func TestPageTemplateSet(t *testing.T) {
	templates, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	b := page.Banner{Service: "chosen-service", Version: "chosen-version"}
	for _, name := range []string{"landing", "about"} {
		if templates.Lookup(name) == nil {
			t.Fatal("missing template", name)
		}
		var data any = struct{ Banner page.Banner }{b}
		if name == "about" {
			data = struct {
				Banner      page.Banner
				Description string
			}{b, web.Description}
		}
		var output bytes.Buffer
		if err := templates.ExecuteTemplate(&output, name, data); err != nil {
			t.Fatal(err)
		}
		if output.Len() == 0 {
			t.Fatal("empty template", name)
		}
	}
}

// R-SB2I-RO3V R-SCAF-5FUK
func pageWritten(t *testing.T, body string, b page.Banner) string {
	t.Helper()
	render := func(name string) string {
		var out bytes.Buffer
		if err := page.Templates().ExecuteTemplate(&out, name, b); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	banner, footer := render("banner"), render("footer")
	bodyTag := pageFirst(t, body, "body")
	bannerAt := strings.Index(body[bodyTag.end:], banner)
	if bannerAt < 0 {
		t.Fatal("missing appkit banner")
	}
	bannerAt += bodyTag.end
	if strings.Trim(body[bodyTag.end:bannerAt], " \t\n\r\f") != "" {
		t.Fatal("banner not first")
	}
	footerAt := strings.LastIndex(body, footer)
	ends := pageTags(body, "body", true)
	if len(ends) == 0 || footerAt < bannerAt+len(banner) || footerAt+len(footer) > ends[len(ends)-1].start || strings.Trim(body[footerAt+len(footer):ends[len(ends)-1].start], " \t\n\r\f") != "" {
		t.Fatal("footer not last")
	}
	return body[:bannerAt] + body[bannerAt+len(banner):footerAt] + body[footerAt+len(footer):]
}

func TestPlainPageFrameAndResources(t *testing.T) {
	// R-SDIB-J7L9 R-SEQ7-WZBY R-SFY4-AR2N R-8ERD-8ZLY R-8FZ9-MRCN R-SIDX-2AK1 R-SJLT-G2AQ R-SKTP-TU1F R-27ND-LVJ9
	b := page.Banner{Service: "chosen-service", Version: "chosen-version", Email: "reader@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"}
	h := pageHandler(t, b)
	for _, path := range []string{"/", "/about"} {
		body := pageBody(t, h, path)
		if strings.Contains(pageVisible(t, body), "reader@example.test") {
			t.Fatal("visible email")
		}
		s := pageWritten(t, body, b)
		title := web.ServiceName
		if path == "/about" {
			title = "About " + web.ServiceName
		}
		tt := pageOne(t, s, "title")
		ends := pageTags(s, "title", true)
		bt := pageFirst(t, s, "body")
		if len(ends) != 1 || tt.end > ends[0].start || ends[0].end > bt.start {
			t.Fatal("title placement")
		}
		pageTextIs(t, s, tt, title)
		for _, tc := range []struct{ name, attr, value, other, want string }{{"link", "rel", "stylesheet", "href", "/_appkit/theme.css"}, {"meta", "name", "viewport", "content", "width=device-width, initial-scale=1"}} {
			var matches []pageSpan
			for _, tag := range pageTags(s, tc.name, false) {
				if pageAttrIs(s, tag, tc.attr, tc.value) {
					matches = append(matches, tag)
				}
			}
			if len(matches) != 1 || matches[0].end > bt.start || !pageAttrIs(s, matches[0], tc.other, tc.want) {
				t.Fatal("head hook", tc, matches)
			}
		}
		feedback := pageOne(t, s, "script")
		if !pageAttrIs(s, feedback, "src", "/_appkit/feedback.js") || feedback.end > bt.start || len(pageAttrs(s, feedback, "defer")) == 0 && !pageBareAttr(s, feedback, "defer") {
			t.Fatal("deferred feedback script missing from head")
		}
		for _, tag := range pageStarts(s) {
			if len(pageAttrs(s, tag, "style")) != 0 || len(pageAttrs(s, tag, "srcset")) != 0 {
				t.Fatal("inline resource", tag)
			}
			if pageIs(s, tag, "a") {
				continue
			}
			for _, attr := range []string{"href", "src", "poster", "data", "background", "manifest"} {
				for _, v := range pageAttrs(s, tag, attr) {
					if v != "/" && (len(v) < 2 || v[0] != '/' || v[1] == '/' || v[1] == '\\') {
						t.Fatal("external resource", attr, v)
					}
				}
			}
		}
		pageTextIs(t, s, pageOne(t, s, "h1"), title)
		if strings.Contains(s, "Telemetry") {
			t.Fatal("capitalized service")
		}
	}
}

func TestPlainPageIcon(t *testing.T) {
	// R-BY3S-VM1R
	for _, listed := range []bool{false, true} {
		b := page.Banner{Service: "chosen-service", Version: "chosen-version", Email: "reader@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"}
		if listed {
			b.Services = []page.Service{{Name: "auth", URL: "https://auth.example.test", Icon: template.HTML("icon"), Enabled: true}}
		}
		h := pageHandler(t, b)
		for _, path := range []string{"/", "/about"} {
			s := pageWritten(t, pageBody(t, h, path), b)
			var icons []pageSpan
			for _, tag := range pageTags(s, "link", false) {
				if pageAttrIs(s, tag, "rel", "icon") {
					icons = append(icons, tag)
				}
			}
			if len(icons) != 1 {
				t.Fatalf("%s icon link count: %d", path, len(icons))
			}
			icon := icons[0]
			if icon.end > pageFirst(t, s, "body").start || !pageAttrIs(s, icon, "href", "/_appkit/favicon.svg") || !pageAttrIs(s, icon, "type", "image/svg+xml") {
				t.Fatalf("%s icon hook: %s", path, s[icon.start:icon.end])
			}
		}
	}
}

const pageSummary = "The suite's trail of events. Every service on this host records what it does here: each request it serves, each call it makes to a sibling, each MCP tool it runs, and the events of its own domain."

var pageToolNames = []string{"catalog", "search", "count", "trace"}
var pageToolDescriptions = []string{"The services, the events each records, and the attribute keys each carries.", "The records that match a filter, newest first.", "How many records match a filter, grouped by a field or a time bucket.", "Every record of one request, from every service it touched, oldest first."}

func pageList(t *testing.T, s string, tag pageSpan, count int) (string, []pageSpan, []pageSpan) {
	t.Helper()
	content := pageContent(t, s, tag)
	terms, defs := pageTags(content, "dt", false), pageTags(content, "dd", false)
	if len(terms) != count || len(defs) != count {
		t.Fatal("list counts", len(terms), len(defs))
	}
	for i := range count {
		if terms[i].start >= defs[i].start || i+1 < count && defs[i].start >= terms[i+1].start {
			t.Fatal("list order")
		}
	}
	return content, terms, defs
}
func TestLandingHooksAndVisibleText(t *testing.T) {
	// R-SOHE-Z59I R-SPPB-CX07 R-SQX7-QOQW R-SS54-4GHL R-STD0-I88A
	b := page.Banner{Service: web.ServiceName, Version: "page-version", Email: "reader@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"}
	s := pageWritten(t, pageBody(t, pageHandler(t, b), "/"), b)
	pageTextIs(t, s, pageHook(t, s, "summary", "p", "lede"), pageSummary)
	pageTextIs(t, s, pageOne(t, s, "h2"), "MCP tools")
	content, terms, defs := pageList(t, s, pageHook(t, s, "tools", "dl", "kv"), 4)
	for i, name := range pageToolNames {
		if !pageAttrIs(content, terms[i], "data-tool", name) {
			t.Fatal("tool hook", name)
		}
		pageTextIs(t, content, terms[i], name)
		inner := pageContent(t, content, terms[i])
		pageTextIs(t, inner, pageOne(t, inner, "code"), name)
		pageTextIs(t, content, defs[i], pageToolDescriptions[i])
	}
	link := pageHook(t, s, "about-link", "a", "")
	if !pageAttrIs(s, link, "href", "/about") {
		t.Fatal("about link")
	}
	pageTextIs(t, s, link, "About telemetry")
	parts := []string{"telemetry", pageSummary, "MCP tools", "Agents search the trail with these tools, through the MCP gateway's call."}
	for i, name := range pageToolNames {
		parts = append(parts, name, pageToolDescriptions[i])
	}
	pageOrder(t, pageVisible(t, s), append(parts, "About telemetry"))
}
func TestAboutHooksAndVisibleText(t *testing.T) {
	// R-SUKW-VZYZ R-SVST-9RPO R-SX0P-NJGD
	for _, values := range []struct{ service, version string }{{"chosen-service", "chosen-version"}, {"other-service", "other-version"}} {
		b := page.Banner{Service: values.service, Version: values.version, Email: "reader@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"}
		s := pageWritten(t, pageBody(t, pageHandler(t, b), "/about"), b)
		content, terms, defs := pageList(t, s, pageHook(t, s, "about", "dl", "kv"), 3)
		for i, label := range []string{"Name", "Version", "Description"} {
			pageTextIs(t, content, terms[i], label)
			id := []string{"about-name", "about-version", "about-description"}[i]
			if !pageAttrIs(content, defs[i], "id", id) {
				t.Fatal("about value position", id)
			}
			pageTextIs(t, content, defs[i], []string{b.Service, b.Version, web.Description}[i])
		}
		link := pageHook(t, s, "home-link", "a", "")
		if !pageAttrIs(s, link, "href", "/") {
			t.Fatal("home link")
		}
		pageTextIs(t, s, link, "Back to telemetry")
		pageOrder(t, pageVisible(t, s), []string{"About telemetry", "Name", b.Service, "Version", b.Version, "Description", web.Description, "Back to telemetry"})
	}
}
func TestLauncherDependsOnBannerServices(t *testing.T) {
	// R-8H76-0J3C
	for _, listed := range []bool{false, true} {
		b := page.Banner{Service: web.ServiceName, Version: "page-version"}
		if listed {
			b.Services = []page.Service{{Name: "auth", URL: "https://auth.example.test", Icon: template.HTML("icon"), Enabled: true}}
		}
		h := pageHandler(t, b)
		for _, path := range []string{"/", "/about"} {
			body := pageBody(t, h, path)
			count := 0
			for _, tag := range pageTags(body, "button", false) {
				if pageClass(body, tag, "launcher") {
					count++
				}
			}
			scripts := pageTags(body, "script", false)
			feedback, launcher := 0, 0
			for _, script := range scripts {
				if pageAttrIs(body, script, "src", "/_appkit/feedback.js") {
					feedback++
				}
				if pageAttrIs(body, script, "src", "/_appkit/launcher.js") {
					launcher++
				}
			}
			if listed {
				if count != 1 || len(scripts) != 2 || feedback != 1 || launcher != 1 {
					t.Fatal("missing launcher or feedback", count, scripts)
				}
			} else if count != 0 || len(scripts) != 1 || feedback != 1 || len(pageTags(body, "input", false)) != 0 || strings.Contains(body, "/_appkit/launcher.js") {
				t.Fatal("unexpected launcher or missing feedback")
			}
		}
	}
}
