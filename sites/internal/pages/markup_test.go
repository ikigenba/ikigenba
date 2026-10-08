package pages_test

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

type attribute struct {
	name, value string
	equal       bool
}
type tag struct {
	name, raw  string
	start, end int
}

func asciiSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}
func nameByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// These readers implement D06's deliberately narrow written-markup vocabulary.
func tags(s, name string) []tag {
	re := regexp.MustCompile(`<` + regexp.QuoteMeta(asciiLower(name)) + `(?:>|[^a-z0-9>][^>]*>)`)
	var out []tag
	for _, p := range re.FindAllStringIndex(asciiLower(s), -1) {
		out = append(out, tag{name, s[p[0]:p[1]], p[0], p[1]})
	}
	return out
}

func allTags(s string) []tag {
	var out []tag
	for offset := 0; offset < len(s); {
		i := strings.IndexByte(s[offset:], '<')
		if i < 0 {
			break
		}
		i += offset
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			break
		}
		j += i
		k := i + 1
		for k < j && nameByte(s[k]) {
			k++
		}
		if k > i+1 {
			out = append(out, tag{strings.ToLower(s[i+1 : k]), s[i : j+1], i, j + 1})
		}
		offset = j + 1
	}
	return out
}

func (x tag) attributes() []attribute {
	s := x.raw
	i := 1
	for i < len(s) && nameByte(s[i]) {
		i++
	}
	var out []attribute
	for i < len(s) {
		start := i
		for i < len(s) && asciiSpace(s[i]) {
			i++
		}
		if i == start {
			break
		}
		start = i
		for i < len(s) && !asciiSpace(s[i]) && !strings.ContainsRune(`"'<>/=`, rune(s[i])) {
			i++
		}
		if start == i {
			break
		}
		a := attribute{name: s[start:i]}
		if i+1 < len(s) && s[i:i+2] == `="` {
			i += 2
			start = i
			for i < len(s) && s[i] != '"' {
				i++
			}
			if i == len(s) {
				break
			}
			a.equal, a.value = true, html.UnescapeString(s[start:i])
			i++
		}
		out = append(out, a)
	}
	return out
}

func (x tag) values(name string) []string {
	var out []string
	for _, a := range x.attributes() {
		if a.equal && asciiLower(a.name) == asciiLower(name) {
			out = append(out, a.value)
		}
	}
	return out
}

func (x tag) bare(name string) bool {
	for _, a := range x.attributes() {
		if !a.equal && asciiLower(a.name) == asciiLower(name) {
			return true
		}
	}
	return false
}

func (x tag) has(name, value string) bool {
	for _, v := range x.values(name) {
		if v == value {
			return true
		}
	}
	return false
}

func (x tag) class(k string) bool {
	for _, v := range x.values("class") {
		for _, field := range strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v' }) {
			if field == k {
				return true
			}
		}
	}
	return false
}

func (x tag) content(s string) (string, bool) {
	ends := tags(s[x.end:], "/"+x.name)
	if len(ends) == 0 {
		return "", false
	}
	return s[x.end : x.end+ends[0].start], true
}

func normal(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			break
		}
		s = s[i+j+1:]
	}
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}

func visible(s string) string {
	bs := tags(s, "body")
	if len(bs) == 0 {
		return ""
	}
	x, ok := bs[0].content(s)
	if !ok {
		return ""
	}
	return normal(x)
}

func selected(s, attr, value string) []tag {
	var out []tag
	for _, x := range allTags(s) {
		if x.has(attr, value) {
			out = append(out, x)
		}
	}
	return out
}

func one(t *testing.T, xs []tag, name string) tag {
	t.Helper()
	if len(xs) != 1 || !strings.EqualFold(xs[0].name, name) {
		t.Fatalf("expected one %s, found %#v", name, xs)
	}
	return xs[0]
}

func content(t *testing.T, s string, x tag) string {
	t.Helper()
	c, ok := x.content(s)
	if !ok {
		t.Fatalf("no end tag for %s", x.raw)
	}
	return c
}

func text(t *testing.T, s string, x tag, want string) {
	t.Helper()
	if got := normal(content(t, s, x)); got != want {
		t.Fatalf("%s text %q, want %q", x.raw, got, want)
	}
}

func attr(t *testing.T, x tag, name, want string) {
	t.Helper()
	if !x.has(name, want) {
		t.Fatalf("%s missing %s=%q", x.raw, name, want)
	}
}

func class(t *testing.T, x tag, want string) {
	t.Helper()
	if !x.class(want) {
		t.Fatalf("%s missing class %q", x.raw, want)
	}
}

func byClass(s, name, k string) []tag {
	var out []tag
	for _, x := range tags(s, name) {
		if x.class(k) {
			out = append(out, x)
		}
	}
	return out
}

// R-DNCL-J31W R-DOKH-WUSL R-DPSE-AMJA R-DR0A-OE9Z R-DTG3-FXRD
func TestMarkupVocabulary(t *testing.T) {
	s := `<BODY><p ID="a&amp;b" class="other&#9;lede" ID="second"> a<b>bold</b>&nbsp;z </P><bodyish>x</bodyish></BODY>`
	p := one(t, tags(s, "p"), "p")
	attr(t, p, "id", "a&b")
	attr(t, p, "id", "second")
	class(t, p, "lede")
	text(t, s, p, "abold z")
	if visible(s) != "abold z x" || normal(" x<unfinished") != "x" || visible("<body>missing end") != "" {
		t.Fatal("normalisation/visible-text vocabulary")
	}
	if len(tags(`<p2>no</p2><P-other>x</P-other><p`, "p")) != 1 {
		t.Fatal("tag boundaries")
	}
	if _, ok := one(t, tags("<p>missing", "p"), "p").content("<p>missing"); ok {
		t.Fatal("invented absent element content")
	}
	x := one(t, tags(`<p id="first" bare class ="ignored" title="unread">`, "p"), "p")
	if len(x.values("id")) != 1 || len(x.values("class")) != 0 || len(x.values("title")) != 0 {
		t.Fatal("attributes did not stop at unsupported syntax")
	}
}

// R-60ND-HV06
func TestBareAttributeVocabulary(t *testing.T) {
	for _, v := range []struct {
		markup string
		bare   bool
	}{
		{`<script src="/_appkit/feedback.js" DeFeR>`, true},
		{`<script	DEFER
src="/_appkit/feedback.js">`, true},
		{`<script defer="">`, false},
		{`<script defer="defer">`, false},
		{`<script deferred defer-other>`, false},
		{`<script title="defer">`, false},
		{`<script class ="ignored" defer>`, false},
	} {
		x := one(t, tags(v.markup, "script"), "script")
		if got := x.bare("defer"); got != v.bare {
			t.Errorf("bare defer in %q = %t, want %t", v.markup, got, v.bare)
		}
	}
}

func whitespaceOnly(s string) bool {
	for i := range len(s) {
		if !asciiSpace(s[i]) {
			return false
		}
	}
	return true
}

// R-DUNZ-TPI2 R-DX3S-L8ZG
func written(t *testing.T, body string, b page.Banner) string {
	t.Helper()
	bannerText := execute(t, page.Templates(), "banner", b)
	footerText := execute(t, page.Templates(), "footer", b)
	bodies := tags(body, "body")
	if len(bodies) == 0 {
		t.Fatal("missing body")
	}
	start := strings.Index(body[bodies[0].end:], bannerText)
	if start < 0 || !whitespaceOnly(body[bodies[0].end:bodies[0].end+start]) {
		t.Fatal("banner absent or does not begin body")
	}
	start += bodies[0].end
	f := strings.Index(body[start+len(bannerText):], footerText)
	if f < 0 {
		t.Fatal("footer absent")
	}
	f += start + len(bannerText)
	ends := tags(body, "/body")
	if len(ends) == 0 || !whitespaceOnly(body[f+len(footerText):ends[len(ends)-1].start]) {
		t.Fatal("footer does not end body")
	}
	return body[:start] + body[start+len(bannerText):f] + body[f+len(footerText):]
}

// These are banners within the shared plain-page and plain-notice domains.
// The display string is arbitrary input, not a release version fixture.
func plainMarkupBanners() []page.Banner {
	return []page.Banner{
		{Service: "sites", Version: "test-build+local"},
		{Service: "sites.dev+preview", Version: "abc123-dirty+local (release.label)"},
		{Service: "sites.dev+preview", Version: ""},
	}
}

// R-WO6W-W0CL R-DYBO-Z0Q5 R-DZJL-CSGU R-5Y7K-QBIS R-61V9-VMQV R-E1ZE-4BY8
// R-EK9V-UW2N
func TestPlainPagesCommonMarkup(t *testing.T) {
	for _, plain := range plainMarkupBanners() {
		s := catalog(t)
		cfg := config(t, s)
		cfg.Banner = func(u page.User) page.Banner {
			b := plain
			b.Email, b.ProfileURL, b.LogoutURL = u.Email, u.ProfileURL, u.LogoutURL
			return b
		}
		h := identity.Optional(pages.Handler(cfg))
		for _, path := range []string{"/", "/about"} {
			r := request("GET", path, "alice", "alice@example.test")
			w := answer(h, r)
			if w.Code != http.StatusOK {
				t.Fatalf("page status %d", w.Code)
			}
			b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, ""), LogoutURL: urls.AuthLogout(r, "")})
			m := written(t, w.Body.String(), b)
			want := "sites"
			if path == "/about" {
				want = "About sites"
				text(t, m, one(t, selected(m, "id", "about-name"), "dd"), b.Service)
				text(t, m, one(t, selected(m, "id", "about-version"), "dd"), b.Version)
			}
			title := one(t, tags(m, "title"), "title")
			end := one(t, tags(m, "/title"), "/title")
			body := one(t, tags(m, "body"), "body")
			if title.end > end.start || end.end > body.start {
				t.Fatal("title not before body")
			}
			text(t, m, title, want)
			text(t, m, one(t, tags(m, "h1"), "h1"), want)
			commonHead(t, m)
			feedbackHead(t, m)
			onlyFeedbackScripts(t, m)
			if len(tags(m, "style")) != 0 {
				t.Fatal("page carries style")
			}
			for _, x := range allTags(m) {
				for _, name := range []string{"style", "srcset", "imagesrcset"} {
					if len(x.values(name)) != 0 {
						t.Fatalf("forbidden attribute %s", name)
					}
				}
				if x.name == "meta" && len(x.values("http-equiv")) != 0 {
					t.Fatal("http-equiv")
				}
			}
			localResources(t, m)
			if strings.Contains(visible(w.Body.String()), "alice@example.test") {
				t.Fatal("email exposed as visible text")
			}
		}
	}
}

func commonHead(t *testing.T, m string) {
	t.Helper()
	body := one(t, tags(m, "body"), "body")
	style := one(t, selected(m, "rel", "stylesheet"), "link")
	attr(t, style, "href", "/_appkit/theme.css")
	view := one(t, selected(m, "name", "viewport"), "meta")
	attr(t, view, "content", "width=device-width, initial-scale=1")
	if style.start >= body.start || view.start >= body.start {
		t.Fatal("head hooks not before body")
	}
}

func faviconHead(t *testing.T, m string) {
	t.Helper()
	var icons []tag
	for _, x := range tags(m, "link") {
		if x.has("rel", "icon") {
			icons = append(icons, x)
		}
	}
	icon := one(t, icons, "link")
	attr(t, icon, "href", "/_appkit/favicon.svg")
	attr(t, icon, "type", "image/svg+xml")
	bodies := tags(m, "body")
	if len(bodies) == 0 || icon.start >= bodies[0].start {
		t.Fatal("favicon link not before body")
	}
}

// R-JS3L-K12R
func TestPlainPagesFavicon(t *testing.T) {
	cfg := config(t, catalog(t))
	h := identity.Optional(pages.Handler(cfg))
	for _, path := range []string{"/", "/about"} {
		r := request("GET", path, "alice", "alice@example.test")
		b := banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, ""), LogoutURL: urls.AuthLogout(r, "")})
		faviconHead(t, written(t, answer(h, r).Body.String(), b))
	}
}

// R-JUJE-BKK5
func TestNoticeFavicon(t *testing.T) {
	s := load(t)
	b := page.Banner{Service: "notice-sites", Version: "test.build+local"}
	for _, name := range []string{"notfound", "unavailable"} {
		w := answer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.Write(w, r, http.StatusServiceUnavailable, name, pages.NoticeData{Banner: b})
		}), request("GET", "/", "", ""))
		faviconHead(t, w.Body.String())
	}
}

func feedbackHead(t *testing.T, m string) {
	t.Helper()
	var feedback []tag
	for _, x := range tags(m, "script") {
		if x.has("src", "/_appkit/feedback.js") {
			feedback = append(feedback, x)
		}
	}
	x := one(t, feedback, "script")
	body := one(t, tags(m, "body"), "body")
	if x.start >= body.start {
		t.Fatal("feedback script not before body")
	}
	if len(x.values("defer")) == 0 && !x.bare("defer") {
		t.Fatal("feedback script is not deferred")
	}
}

func onlyFeedbackScripts(t *testing.T, m string) {
	t.Helper()
	for _, x := range tags(m, "script") {
		attr(t, x, "src", "/_appkit/feedback.js")
		for _, value := range x.values("src") {
			if value != "/_appkit/feedback.js" {
				t.Fatalf("unexpected script src in %s", x.raw)
			}
		}
		if x.bare("src") || len(x.values("href")) != 0 || len(x.values("xlink:href")) != 0 {
			t.Fatalf("forbidden script attribute in %s", x.raw)
		}
	}
}

func localResources(t *testing.T, m string) {
	t.Helper()
	for _, x := range allTags(m) {
		if x.name == "a" {
			continue
		}
		for _, name := range []string{"href", "src", "poster", "data", "background", "manifest"} {
			for _, value := range x.values(name) {
				if value != "/" && (len(value) < 2 || value[0] != '/' || value[1] == '/' || value[1] == '\\') {
					t.Fatalf("external resource: %s", x.raw)
				}
			}
		}
	}
}

// R-6336-9EHK
func TestLauncherFollowsBannerServices(t *testing.T) {
	s := catalog(t)
	for _, services := range [][]page.Service{nil, {{Name: "dummy", URL: "https://dummy.test", Icon: "icon", Enabled: true}}} {
		cfg := config(t, s)
		cfg.Banner = func(u page.User) page.Banner { b := banner(u); b.Services = services; return b }
		h := identity.Optional(pages.Handler(cfg))
		for _, path := range []string{"/", "/about"} {
			body := answer(h, request("GET", path, "alice", "")).Body.String()
			if len(services) != 0 {
				one(t, byClass(body, "button", "launcher"), "button")
				var launchers []tag
				for _, x := range tags(body, "script") {
					if x.has("src", "/_appkit/launcher.js") {
						launchers = append(launchers, x)
					} else {
						attr(t, x, "src", "/_appkit/feedback.js")
					}
				}
				one(t, launchers, "script")
			} else {
				if len(byClass(body, "button", "launcher")) != 0 || len(tags(body, "input")) != 0 || strings.Contains(body, "/_appkit/launcher.js") {
					t.Fatal("launcher emitted with empty services")
				}
				for _, x := range tags(body, "script") {
					attr(t, x, "src", "/_appkit/feedback.js")
				}
			}
		}
	}
}

const summary = "Static sites for the suite, served from repositories that repos holds. An agent creates a site from one of your repositories and publishes it at a commit; a public site is open to anyone, and a private one asks a visitor to sign in to this space."

// R-E4F6-VVFM R-E5N3-9N6B R-E6UZ-NEX0 R-E82W-16NP R-E9AS-EYEE
// R-EAIO-SQ53 R-EBQL-6HVS R-EE6D-Y1D6 R-EFEA-BT3V R-EGM6-PKUK
// R-EHU3-3CL9 R-EJ1Z-H4BY
func TestLandingHooks(t *testing.T) {
	for _, populated := range []bool{false, true} {
		s := catalog(t)
		var all []store.Site
		if populated {
			all = fixtureSites(t, s)
		}
		cfg := config(t, s)
		h := identity.Optional(pages.Handler(cfg))
		for _, user := range []string{"alice", "bob", "other"} {
			r := request("GET", "/", user, "user@example.test")
			body := answer(h, r).Body.String()
			b := banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, ""), LogoutURL: urls.AuthLogout(r, "")})
			m := written(t, body, b)
			sum := one(t, selected(m, "id", "summary"), "p")
			class(t, sum, "lede")
			text(t, m, sum, summary)
			h2s := tags(m, "h2")
			if len(h2s) != 2 {
				t.Fatal("wrong h2 count")
			}
			text(t, m, h2s[0], "Sites")
			text(t, m, h2s[1], "MCP tools")
			section := one(t, selected(m, "id", "sites"), "section")
			sc := content(t, m, section)
			sh := one(t, tags(sc, "h2"), "h2")
			p := one(t, selected(sc, "id", "site-url"), "p")
			if sh.start >= p.start {
				t.Fatal("site address precedes section heading")
			}
			one(t, selected(m, "id", "site-url"), "p")
			code := one(t, tags(content(t, sc, p), "code"), "code")
			base := urls.SitesURL(r, "")
			text(t, content(t, sc, p), code, base+"/<slug>/")
			text(t, sc, p, "Every site answers at "+base+"/<slug>/, where <slug> is its slug.")
			xs, err := s.Visible(r.Context(), user)
			if err != nil {
				t.Fatal(err)
			}
			if len(xs) == 0 {
				text(t, sc, one(t, selected(sc, "id", "no-sites"), "p"), "No sites yet.")
				one(t, selected(m, "id", "no-sites"), "p")
				if len(selected(m, "id", "site-list")) != 0 {
					t.Fatal("table in empty page")
				}
				for _, x := range allTags(m) {
					if len(x.values("data-site")) != 0 {
						t.Fatal("row in empty page")
					}
				}
			} else {
				if len(selected(m, "id", "no-sites")) != 0 {
					t.Fatal("empty notice in populated page")
				}
				one(t, selected(m, "id", "site-list"), "table")
				table := one(t, selected(sc, "id", "site-list"), "table")
				tc := content(t, sc, table)
				var rows []tag
				for _, x := range allTags(m) {
					if len(x.values("data-site")) != 0 {
						rows = append(rows, x)
					}
				}
				if len(rows) != len(xs) {
					t.Fatal("row count")
				}
				for i, x := range xs {
					if rows[i].name != "tr" || !rows[i].has("data-site", x.Slug) {
						t.Fatal("row order or shape")
					}
					row := one(t, selected(tc, "data-site", x.Slug), "tr")
					rowHooks(t, content(t, tc, row), pages.SiteRow{Slug: x.Slug, Name: x.Name, URL: urls.SiteURL(base, x.Slug), Visibility: x.Visibility, Listed: x.Listed, Published: x.Commit != "", Mine: x.Owner == user})
				}
			}
			toolsHooks(t, m)
			a := one(t, selected(m, "id", "about-link"), "a")
			attr(t, a, "href", "/about")
			text(t, m, a, "About sites")
			v := visible(m)
			for _, piece := range []string{"sites", summary, "Sites", "Every site answers at ", "MCP tools", "Agents manage sites with these tools, through the MCP gateway's call and mutate.", "list", "apex", "Show, set or clear the site the space's apex domain redirects to.", "About sites"} {
				i := strings.Index(v, piece)
				if i < 0 {
					t.Fatalf("visible text missing ordered %q", piece)
				}
				v = v[i+len(piece):]
			}
			for _, x := range all {
				for _, secret := range []string{x.ID, x.Repo, x.Commit} {
					if secret != "" && strings.Contains(body, secret) {
						t.Fatalf("catalog detail exposed: %s", secret)
					}
				}
			}
		}
	}
}

func rowHooks(t *testing.T, m string, x pages.SiteRow) {
	t.Helper()
	name := one(t, byClass(m, "td", "site-name"), "td")
	nc := content(t, m, name)
	a := one(t, tags(nc, "a"), "a")
	class(t, a, "site-link")
	attr(t, a, "href", x.URL)
	text(t, nc, a, x.Name)
	visibility := one(t, byClass(m, "td", "site-visibility"), "td")
	vc := content(t, m, visibility)
	v := one(t, tags(vc, "span"), "span")
	class(t, v, "badge")
	attr(t, v, "data-kind", x.Visibility)
	text(t, vc, v, x.Visibility)
	for _, badge := range []struct {
		td, kind, value string
		show            bool
	}{{"site-listing", "unlisted", "unlisted", !x.Listed}, {"site-owner", "mine", "yours", x.Mine}} {
		c := content(t, m, one(t, byClass(m, "td", badge.td), "td"))
		if !badge.show {
			if len(tags(c, "span")) != 0 {
				t.Fatal("unexpected badge")
			}
			continue
		}
		v := one(t, tags(c, "span"), "span")
		class(t, v, "badge")
		attr(t, v, "data-kind", badge.kind)
		text(t, c, v, badge.value)
	}
	c := content(t, m, one(t, byClass(m, "td", "site-state"), "td"))
	v = one(t, tags(c, "span"), "span")
	class(t, v, "status")
	status, value := "unpublished", "not published"
	if x.Published {
		status, value = "published", "published"
	}
	attr(t, v, "data-status", status)
	text(t, c, v, value)
}

func toolsHooks(t *testing.T, m string) {
	t.Helper()
	dl := one(t, selected(m, "id", "tools"), "dl")
	class(t, dl, "kv")
	c := content(t, m, dl)
	dts, dds := tags(c, "dt"), tags(c, "dd")
	if len(dts) != 7 || len(dds) != 7 {
		t.Fatal("tool count")
	}
	names := []string{"list", "show", "create", "publish", "update", "delete", "apex"}
	descriptions := []string{"The sites you own, by name.", "One of your sites, with its URL, its repository, and what is published.", "Create a site from one of your repositories and return it; publish it to make it live.", "Publish one of your sites at a commit of its repository: the ref it tracks, or a ref or commit you name.", "Change one of your sites' visibility, whether it is listed, or the ref it tracks.", "Delete one of your sites; its repository is untouched.", "Show, set or clear the site the space's apex domain redirects to."}
	for i, name := range names {
		if dts[i].start >= dds[i].start || i+1 < 7 && dds[i].start >= dts[i+1].start {
			t.Fatal("tool labels/description do not alternate")
		}
		attr(t, dts[i], "data-tool", name)
		text(t, c, dts[i], name)
		dtc := content(t, c, dts[i])
		text(t, dtc, one(t, tags(dtc, "code"), "code"), name)
		text(t, c, dds[i], descriptions[i])
	}
}

// R-EK9V-UW2N R-ELHS-8NTC
func TestAboutHooks(t *testing.T) {
	s := catalog(t)
	cfg := config(t, s)
	r := request("GET", "/about", "alice", "alice@example.test")
	b := banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, ""), LogoutURL: urls.AuthLogout(r, "")})
	m := written(t, answer(identity.Optional(pages.Handler(cfg)), r).Body.String(), b)
	dl := one(t, selected(m, "id", "about"), "dl")
	class(t, dl, "kv")
	c := content(t, m, dl)
	dts, dds := tags(c, "dt"), tags(c, "dd")
	if len(dts) != 3 || len(dds) != 3 {
		t.Fatal("about facts count")
	}
	for i, fact := range []struct{ label, id, value string }{{"Name", "about-name", b.Service}, {"Version", "about-version", b.Version}, {"Description", "about-description", pages.Description}} {
		if dts[i].start >= dds[i].start || i+1 < 3 && dds[i].start >= dts[i+1].start {
			t.Fatal("about labels/values do not alternate")
		}
		text(t, c, dts[i], fact.label)
		attr(t, dds[i], "id", fact.id)
		text(t, c, dds[i], fact.value)
	}
	a := one(t, selected(m, "id", "home-link"), "a")
	attr(t, a, "href", "/")
	text(t, m, a, "Back to sites")
	if len(tags(m, "h2")) != 0 || strings.Contains(visible(m), "MCP tools") || strings.Contains(visible(m), "Every site answers at") {
		t.Fatal("landing sections on about page")
	}
	for _, id := range []string{"sites", "site-list", "no-sites", "site-url", "tools"} {
		if len(selected(m, "id", id)) != 0 {
			t.Fatalf("landing hook %s on about", id)
		}
	}
}

// R-WPET-9S3A R-65IZ-0XYY R-5ZFH-439H R-EP5H-DZ1F R-EQDD-RQS4 R-ERLA-5IIT
func TestNoticeMarkup(t *testing.T) {
	s := load(t)
	for _, b := range plainMarkupBanners() {
		for _, v := range []struct{ name, title, message string }{{"notfound", "Not found", "There is nothing at this address."}, {"unavailable", "Site unavailable", "This site is not available right now. Try again in a moment."}} {
			w := answer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.Write(w, r, 503, v.name, pages.NoticeData{Banner: b}) }), request("GET", "/", "", ""))
			body := w.Body.String()
			footer := execute(t, page.Templates(), "footer", b)
			f := strings.Index(body, footer)
			ends := tags(body, "/body")
			if f < 0 || len(ends) == 0 || !whitespaceOnly(body[f+len(footer):ends[len(ends)-1].start]) {
				t.Fatal("notice footer absent or does not end body")
			}
			if len(tags(body, "header")) != 0 || len(tags(body, "form")) != 0 || len(byClass(body, "strong", "mark")) != 0 || len(byClass(body, "a", "profile")) != 0 || len(tags(body, "style")) != 0 {
				t.Fatal("notice has banner/style")
			}
			for _, x := range allTags(body) {
				if len(x.values("style")) != 0 {
					t.Fatal("notice inline style")
				}
			}
			commonHead(t, body)
			feedbackHead(t, body)
			onlyFeedbackScripts(t, body)
			localResources(t, body)
			title := one(t, tags(body, "title"), "title")
			end := one(t, tags(body, "/title"), "/title")
			bs := one(t, tags(body, "body"), "body")
			if title.end > end.start || end.end > bs.start {
				t.Fatal("notice title not before body")
			}
			text(t, body, title, v.title)
			text(t, body, one(t, tags(body, "h1"), "h1"), v.title)
			text(t, body, one(t, selected(body, "id", v.name), "p"), v.message)
		}
	}
}
