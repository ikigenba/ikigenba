package web

import (
	"bytes"
	"html"
	"html/template"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
)

type markupTag struct {
	start, end  int
	name        string
	contentName string
	closing     bool
	attrs       []markupAttr
}

type markupAttr struct {
	name, value string
	valued      bool
}

const markupASCIIWhitespace = " \t\n\v\f\r"

var markupAttribute = regexp.MustCompile(`^[\t\n\v\f\r ]+([^\t\n\v\f\r "'<>/=]+)(="([^"]*)")?`)

func markupAlphanumeric(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func markupTags(s string) []markupTag {
	var tags []markupTag
	for cursor := 0; cursor < len(s); {
		rel := strings.IndexByte(s[cursor:], '<')
		if rel < 0 {
			break
		}
		start := cursor + rel
		cursor = start + 1
		end := strings.IndexByte(s[start:], '>')
		if end < 0 {
			break
		}
		end += start + 1
		nameStart := start + 1
		closing := nameStart < end && s[nameStart] == '/'
		if closing {
			nameStart++
		}
		nameEnd := nameStart
		for nameEnd < end && (markupAlphanumeric(s[nameEnd]) || s[nameEnd] == '-') {
			nameEnd++
		}
		if nameEnd == nameStart {
			continue
		}
		tag := markupTag{start: start, end: end, name: s[nameStart:nameEnd], closing: closing}
		if !closing {
			for remaining := s[nameEnd:end]; ; {
				match := markupAttribute.FindStringSubmatch(remaining)
				if match == nil {
					break
				}
				tag.attrs = append(tag.attrs, markupAttr{name: match[1], value: html.UnescapeString(match[3]), valued: match[2] != ""})
				remaining = remaining[len(match[0]):]
			}
		}
		tags = append(tags, tag)
	}
	return tags
}

func (tag markupTag) named(name string) bool {
	return len(tag.name) >= len(name) && strings.EqualFold(tag.name[:len(name)], name) && (len(tag.name) == len(name) || !markupAlphanumeric(tag.name[len(name)]))
}

func markupNamed(s, name string, closing bool) []markupTag {
	var found []markupTag
	for _, tag := range markupTags(s) {
		if tag.closing == closing && tag.named(name) {
			tag.contentName = name
			found = append(found, tag)
		}
	}
	return found
}

func (tag markupTag) values(name string) []string {
	var found []string
	for _, attr := range tag.attrs {
		if attr.valued && strings.EqualFold(attr.name, name) {
			found = append(found, attr.value)
		}
	}
	return found
}

// R-PB8G-QSV8: bare occurrences use the same left-to-right attribute read.
func (tag markupTag) bare(name string) bool {
	for _, attr := range tag.attrs {
		if !attr.valued && strings.EqualFold(attr.name, name) {
			return true
		}
	}
	return false
}

func (tag markupTag) has(name, value string) bool {
	for _, got := range tag.values(name) {
		if got == value {
			return true
		}
	}
	return false
}

func (tag markupTag) class(value string) bool {
	for _, classes := range tag.values("class") {
		for _, class := range strings.FieldsFunc(classes, func(r rune) bool { return strings.ContainsRune(markupASCIIWhitespace, r) }) {
			if class == value {
				return true
			}
		}
	}
	return false
}

func markupContent(t *testing.T, s string, tag markupTag) string {
	t.Helper()
	name := tag.contentName
	if name == "" {
		name = tag.name
	}
	ends := markupNamed(s[tag.end:], name, true)
	if len(ends) == 0 {
		t.Fatalf("%s element content absent", tag.name)
	}
	return s[tag.end : tag.end+ends[0].start]
}

func markupCollapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func markupNormalize(s string) string {
	var result strings.Builder
	for {
		open := strings.IndexByte(s, '<')
		if open < 0 {
			result.WriteString(s)
			break
		}
		result.WriteString(s[:open])
		end := strings.IndexByte(s[open:], '>')
		if end < 0 {
			break
		}
		s = s[open+end+1:]
	}
	return markupCollapse(html.UnescapeString(result.String()))
}

func markupVisible(t *testing.T, s string) string {
	t.Helper()
	bodies := markupNamed(s, "body", false)
	if len(bodies) == 0 || len(markupNamed(s[bodies[0].end:], "body", true)) == 0 {
		return ""
	}
	return markupNormalize(markupContent(t, s, bodies[0]))
}

func markupOne(t *testing.T, s, name string) markupTag {
	t.Helper()
	found := markupNamed(s, name, false)
	if len(found) != 1 {
		t.Fatalf("%s start tag count %d, want 1", name, len(found))
	}
	return found[0]
}

func markupHook(t *testing.T, s, id, name string) markupTag {
	t.Helper()
	var found []markupTag
	for _, tag := range markupTags(s) {
		if !tag.closing && tag.has("id", id) {
			found = append(found, tag)
		}
	}
	if len(found) != 1 || !found[0].named(name) {
		t.Fatalf("hook %s: got %v, want exactly one %s tag", id, found, name)
	}
	found[0].contentName = name
	return found[0]
}

func markupText(t *testing.T, s string, tag markupTag, want string) {
	t.Helper()
	if got := markupNormalize(markupContent(t, s, tag)); got != want {
		t.Fatalf("%s text %q, want %q", tag.name, got, want)
	}
}

func markupOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		at := strings.Index(text, part)
		if at < 0 {
			t.Fatalf("ordered text %q absent from remaining %q", part, text)
		}
		text = text[at+len(part):]
	}
}

// R-69VU-AZO7 R-6B3Q-OREW
func markupWritten(t *testing.T, body string, banner page.Banner) string {
	t.Helper()
	templates := page.Templates()
	var bannerText, footerText bytes.Buffer
	if err := templates.ExecuteTemplate(&bannerText, "banner", banner); err != nil {
		t.Fatal(err)
	}
	if err := templates.ExecuteTemplate(&footerText, "footer", banner); err != nil {
		t.Fatal(err)
	}
	bodies := markupNamed(body, "body", false)
	ends := markupNamed(body, "body", true)
	if len(bodies) == 0 || len(ends) == 0 {
		t.Fatal("page lacks body boundaries")
	}
	bannerStart := strings.Index(body[bodies[0].end:], bannerText.String())
	if bannerStart < 0 {
		t.Fatal("exact appkit banner absent")
	}
	bannerStart += bodies[0].end
	bannerEnd := bannerStart + bannerText.Len()
	footerStart := strings.Index(body[bannerEnd:], footerText.String())
	if footerStart < 0 {
		t.Fatal("exact appkit footer absent after banner")
	}
	footerStart += bannerEnd
	footerEnd := footerStart + footerText.Len()
	endBody := ends[len(ends)-1].start
	if footerEnd > endBody || strings.Trim(body[bodies[0].end:bannerStart], markupASCIIWhitespace) != "" || strings.Trim(body[footerEnd:endBody], markupASCIIWhitespace) != "" {
		t.Fatal("appkit banner/footer are not at body boundaries separated by ASCII whitespace")
	}
	return body[:bannerStart] + body[bannerEnd:footerStart] + body[footerEnd:]
}

// R-6CBN-2J5L R-6DJJ-GAWA R-6ERF-U2MZ R-PG42-9VU0 R-PIJV-1FBE R-988F-R9HL R-6IF4-ZDV2 R-6JN1-D5LR R-6KUX-QXCG R-6M2U-4P35
func assertMarkupCommon(t *testing.T, body, written, path, email string) {
	t.Helper()
	title := markupOne(t, written, "title")
	titleEnds := markupNamed(written, "title", true)
	bodyTag := markupOne(t, written, "body")
	if len(titleEnds) != 1 || title.end > titleEnds[0].start || titleEnds[0].end > bodyTag.start {
		t.Fatal("title must have one ordered start/end pair before body")
	}
	wantTitle, wantHeading := ServiceName, "repos"
	if path == "/about" {
		wantTitle, wantHeading = "About "+ServiceName, "About repos"
	}
	markupText(t, written, title, wantTitle)
	markupText(t, written, markupOne(t, written, "h1"), wantHeading)
	var stylesheets, viewports []markupTag
	for _, tag := range markupTags(written) {
		if tag.closing {
			continue
		}
		if tag.named("link") && tag.has("rel", "stylesheet") {
			stylesheets = append(stylesheets, tag)
		}
		if tag.named("meta") && tag.has("name", "viewport") {
			viewports = append(viewports, tag)
		}
		for _, forbidden := range []string{"style", "srcset", "imagesrcset"} {
			if len(tag.values(forbidden)) != 0 {
				t.Fatalf("%s carries forbidden %s", tag.name, forbidden)
			}
		}
		if tag.named("meta") && len(tag.values("http-equiv")) != 0 {
			t.Fatal("meta http-equiv occurrence")
		}
		if tag.named("a") {
			continue
		}
		for _, attribute := range []string{"href", "xlink:href", "src", "poster", "data", "background", "manifest"} {
			for _, value := range tag.values(attribute) {
				if value != "/" && (len(value) < 2 || value[0] != '/' || value[1] == '/' || value[1] == '\\') {
					t.Fatalf("%s %s loads non-local %q", tag.name, attribute, value)
				}
			}
		}
	}
	if len(stylesheets) != 1 || stylesheets[0].end > bodyTag.start || !stylesheets[0].has("href", "/_appkit/theme.css") {
		t.Fatal("stylesheet count/location/href")
	}
	if len(viewports) != 1 || viewports[0].end > bodyTag.start || !viewports[0].has("content", "width=device-width, initial-scale=1") {
		t.Fatal("viewport count/location/content")
	}
	scripts := markupNamed(written, "script", false)
	feedback := 0
	for _, script := range scripts {
		if len(script.values("src")) != 1 || !script.has("src", "/_appkit/feedback.js") || script.bare("src") {
			t.Fatal("written script must have exactly one valued feedback source")
		}
		for _, name := range []string{"href", "xlink:href"} {
			if len(script.values(name)) != 0 || script.bare(name) {
				t.Fatalf("written script carries forbidden %s", name)
			}
		}
		if script.has("src", "/_appkit/feedback.js") {
			feedback++
			if script.end > bodyTag.start || len(script.values("defer")) == 0 && !script.bare("defer") {
				t.Fatal("feedback script must be deferred before body")
			}
		}
	}
	if feedback != 1 {
		t.Fatalf("written feedback script count %d, want 1", feedback)
	}
	if len(markupNamed(written, "style", false)) != 0 || strings.Contains(written, "Repos") {
		t.Fatal("written markup contains style or capitalized service name")
	}
	visible := markupVisible(t, body)
	if strings.Contains(visible, "IKIGENBA_TOKEN=") || strings.Contains(visible, markupCollapse(email)) {
		t.Fatalf("visible text contains token assignment or caller email: %q", visible)
	}
}

const markupSummary = "Git repositories for the suite's content. Agents create repositories with the tools below and push to them with ordinary git over HTTPS. Each repository belongs to the user who created it, and only that user can see it or reach it."

// R-QSP1-T3N8
func assertMarkupIcon(t *testing.T, written string) {
	t.Helper()
	var icons []markupTag
	for _, tag := range markupNamed(written, "link", false) {
		if tag.has("rel", "icon") {
			icons = append(icons, tag)
		}
	}
	body := markupNamed(written, "body", false)
	if len(icons) != 1 || len(body) == 0 || icons[0].end > body[0].start || !icons[0].has("href", "/_appkit/favicon.svg") || !icons[0].has("type", "image/svg+xml") {
		t.Fatalf("favicon link count/location/href/type: %v", icons)
	}
}

var markupToolNames = []string{"list", "show", "status", "create", "rename", "delete"}
var markupToolDescriptions = []string{
	"The repositories you own, by name.",
	"One of your repositories, with its clone URL and how to give git your token.",
	"How busy repos is, and how close each of your repositories is to its size limit.",
	"Create an empty repository and return it, with its clone URL and how to give git your token.",
	"Give one of your repositories a new name; its id does not change.",
	"Delete one of your repositories and everything in it.",
}

func markupPairs(t *testing.T, content string, names, descriptions []string) ([]markupTag, []markupTag) {
	t.Helper()
	terms, definitions := markupNamed(content, "dt", false), markupNamed(content, "dd", false)
	if len(terms) != len(names) || len(definitions) != len(descriptions) {
		t.Fatalf("definition list counts dt=%d dd=%d, want %d %d", len(terms), len(definitions), len(names), len(descriptions))
	}
	for i := range terms {
		if terms[i].start >= definitions[i].start || i+1 < len(terms) && definitions[i].start >= terms[i+1].start {
			t.Fatal("definition list does not alternate dt/dd")
		}
		markupText(t, content, terms[i], names[i])
		markupText(t, content, definitions[i], descriptions[i])
	}
	return terms, definitions
}

// R-6NAQ-IGTU R-6OIM-W8KJ R-6PQJ-A0B8 R-6QYF-NS1X R-6TE8-FBJB R-6UM4-T3A0 R-6VU1-6V0P R-6X1X-KMRE R-6Y9T-YEI3
func assertMarkupLanding(t *testing.T, written, base string) {
	t.Helper()
	summary := markupHook(t, written, "summary", "p")
	if !summary.class("lede") {
		t.Fatal("summary lacks lede class")
	}
	markupText(t, written, summary, markupSummary)
	tools := markupHook(t, written, "tools", "dl")
	if !tools.class("kv") {
		t.Fatal("tools lacks kv class")
	}
	toolContent := markupContent(t, written, tools)
	terms, _ := markupPairs(t, toolContent, markupToolNames, markupToolDescriptions)
	for i, term := range terms {
		if !term.has("data-tool", markupToolNames[i]) {
			t.Fatalf("tool %d data-tool", i)
		}
		content := markupContent(t, toolContent, term)
		markupText(t, content, markupOne(t, content, "code"), markupToolNames[i])
	}
	section := markupHook(t, written, "clone", "section")
	sectionContent := markupContent(t, written, section)
	headings := markupNamed(written, "h2", false)
	if len(headings) != 2 || headings[1].start < section.end || headings[1].end > section.end+len(sectionContent) {
		t.Fatal("landing h2 count or clone heading containment")
	}
	markupText(t, written, headings[0], "MCP tools")
	markupText(t, written, headings[1], "Clone with git")
	cloneURL := markupHook(t, written, "clone-url", "p")
	intro := markupHook(t, written, "credentials-intro", "p")
	helper := markupHook(t, written, "git-helper", "code")
	warning := markupHook(t, written, "credentials-warning", "p")
	previous := section.end
	for _, hook := range []markupTag{cloneURL, intro, helper, warning} {
		if hook.start < previous || hook.end > section.end+len(sectionContent) {
			t.Fatal("clone hooks do not lie in clone section in document order")
		}
		previous = hook.end
	}
	url := clone.URL(base, "<name>")
	urlContent := markupContent(t, written, cloneURL)
	markupText(t, urlContent, markupOne(t, urlContent, "code"), url)
	cloneParagraph := "Clone a repository you own from " + url + ", where <name> is its name."
	markupText(t, written, cloneURL, cloneParagraph)
	credentials := clone.Guidance(base)
	markupText(t, written, intro, markupCollapse(credentials.Intro))
	markupText(t, written, warning, markupCollapse(credentials.Warning))
	helperContent := markupContent(t, written, helper)
	if strings.Contains(helperContent, "<") || html.UnescapeString(helperContent) != credentials.Helper {
		t.Fatalf("credential helper not verbatim: %q", helperContent)
	}
	contained := false
	for _, pre := range markupNamed(written, "pre", false) {
		content := markupContent(t, written, pre)
		if helper.start >= pre.end && helper.end <= pre.end+len(content) {
			contained = true
		}
	}
	if !contained {
		t.Fatal("git-helper is not within pre element content")
	}
	about := markupHook(t, written, "about-link", "a")
	if !about.has("href", "/about") {
		t.Fatal("about link href")
	}
	markupText(t, written, about, "About repos")
	order := []string{"repos", markupSummary, "MCP tools", "Agents manage repositories with these tools, through the MCP gateway's call and mutate."}
	for i, name := range markupToolNames {
		order = append(order, name, markupToolDescriptions[i])
	}
	order = append(order, "Clone with git", cloneParagraph, markupCollapse(credentials.Intro), markupCollapse(credentials.Helper), markupCollapse(credentials.Warning), "About repos")
	markupOrder(t, markupVisible(t, written), order...)
}

// R-6ZHQ-C68S R-70PM-PXZH R-71XJ-3PQ6 R-735F-HHGV
func assertMarkupAbout(t *testing.T, written string, banner page.Banner) {
	t.Helper()
	about := markupHook(t, written, "about", "dl")
	if !about.class("kv") {
		t.Fatal("about lacks kv class")
	}
	content := markupContent(t, written, about)
	_, definitions := markupPairs(t, content, []string{"Name", "Version", "Description"}, []string{banner.Service, banner.Version, Description})
	for i, id := range []string{"about-name", "about-version", "about-description"} {
		if !definitions[i].has("id", id) {
			t.Fatalf("about definition %d lacks %s", i, id)
		}
	}
	home := markupHook(t, written, "home-link", "a")
	if !home.has("href", "/") {
		t.Fatal("home link href")
	}
	markupText(t, written, home, "Back to repos")
	visible := markupVisible(t, written)
	markupOrder(t, visible, "About repos", "Name", banner.Service, "Version", banner.Version, "Description", Description, "Back to repos")
	if len(markupNamed(written, "h2", false)) != 0 || strings.Contains(visible, "MCP tools") || strings.Contains(visible, "Clone with git") {
		t.Fatal("about contains landing sections")
	}
	for _, tag := range markupTags(written) {
		if tag.closing {
			continue
		}
		for _, id := range []string{"tools", "clone", "clone-url", "git-helper"} {
			if tag.has("id", id) {
				t.Fatalf("about carries landing hook %s", id)
			}
		}
	}
}

// R-HHO3-61CU
func assertMarkupLauncher(t *testing.T, body string, banner page.Banner) {
	t.Helper()
	launchers := 0
	for _, tag := range markupNamed(body, "button", false) {
		if tag.class("launcher") {
			launchers++
		}
	}
	scripts := markupNamed(body, "script", false)
	if len(banner.Services) == 0 {
		if launchers != 0 || len(scripts) != 1 || !scripts[0].has("src", "/_appkit/feedback.js") || len(markupNamed(body, "input", false)) != 0 || strings.Contains(body, "/_appkit/launcher.js") {
			t.Fatal("empty banner services still carries launcher artifacts")
		}
	} else {
		if launchers != 1 || len(scripts) != 2 {
			t.Fatal("nonempty banner services lacks exactly one launcher button and two appkit scripts")
		}
		if (!scripts[0].has("src", "/_appkit/launcher.js") || !scripts[1].has("src", "/_appkit/feedback.js")) &&
			(!scripts[1].has("src", "/_appkit/launcher.js") || !scripts[0].has("src", "/_appkit/feedback.js")) {
			t.Fatal("launcher and feedback sources must occur on distinct script tags")
		}
	}
}

// R-5HU5-I9M8 R-5J21-W1CX R-5K9Y-9T3M R-5LHU-NKUB R-5MPR-1CL0 R-67G1-JG6T
func TestMarkupPages(t *testing.T) {
	for _, fixture := range []struct {
		name, host, proto, service, version string
		published                           bool
		launcher                            []page.Service
	}{
		{name: "fallback-no-launcher", host: "repos.sbx.ikigenba.dev:443", service: ServiceName, version: "fixture-alpha"},
		{name: "published-launcher", host: "repos.preview.test:8080", proto: "http", service: "workspace & tools", version: "fixture-beta", published: true,
			launcher: []page.Service{{Name: "auth", URL: "https://auth.fixture.test", Icon: template.HTML("&bull;"), Enabled: true}, {Name: "repos", URL: "https://repos.fixture.test", Icon: template.HTML("&#9733;"), Enabled: true, Current: true}}},
		{name: "fallback-disabled-launcher", host: "space.test", proto: "HTTPS", service: "work <sample>", version: "fixture-<&>-gamma",
			launcher: []page.Service{{Name: "offline", URL: "https://offline.fixture.test", Icon: template.HTML("*"), Enabled: false}}},
		{name: "published-no-launcher", host: "repos.preview.test:9000", proto: "https", service: "suite service", version: "fixture-delta", published: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			f := newWebFixture(t)
			if fixture.published {
				f.cfg.ServicesPath = filepath.Join(f.dir, "services.json")
				writePageServices(t, f.cfg.ServicesPath, pageService("auth", "https://auth.published.test/account/", ""), pageService("repos", "http://repos.published.test:8080/prefix/", ""), pageService("dummy", "https://dummy.published.test", ""))
				listed, err := services.Read(f.cfg.ServicesPath)
				if err != nil || len(listed) != 3 {
					t.Fatalf("published plain-page fixture: %v %v", listed, err)
				}
			}
			var banner page.Banner
			f.cfg.Banner = func(u page.User) page.Banner {
				banner = page.Banner{Service: fixture.service, Version: fixture.version, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Services: fixture.launcher}
				return banner
			}
			h := Handler(f.cfg)
			for _, path := range []string{"/", "/about"} {
				for _, email := range []string{"person@example.test", "\t person+<sample>@example.test \n"} {
					r := httptest.NewRequest("GET", path+"?ignored=anything", strings.NewReader("ignored payload"))
					r.Host = fixture.host
					r.Header["X-User-Id"] = []string{"caller", "ignored"}
					r.Header.Set("X-User-Email", email)
					r.Header.Set("X-Forwarded-Proto", fixture.proto)
					r.Header.Set("X-Request-Id", "markup-contract")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Code != 200 {
						t.Fatalf("page %s status %d", path, w.Code)
					}
					body := w.Body.String()
					written := markupWritten(t, body, banner)
					assertMarkupIcon(t, written)
					assertMarkupCommon(t, body, written, path, email)
					assertMarkupLauncher(t, body, banner)
					if path == "/" {
						assertMarkupLanding(t, written, clone.Base(r, f.cfg.ServicesPath))
					} else {
						assertMarkupAbout(t, written, banner)
					}
				}
			}
		})
	}
}
