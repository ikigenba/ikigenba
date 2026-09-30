package appkit

import (
	"bytes"
	"html"
	"html/template"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// templateElement records hooks and content without depending on layout.
type templateElement struct {
	tag         string
	attrs       map[string]string
	rawAttrs    map[string]string
	children    []*templateElement
	content     string
	text        string
	rawText     string
	textStart   int
	markupStart int
	markupEnd   int
}

var templateAttribute = regexp.MustCompile(`([^\s=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s]+)))?`)

func templateDocument(t *testing.T, markup string) *templateElement {
	t.Helper()
	root := &templateElement{}
	stack := []*templateElement{root}
	starts := []int{0}
	for pos := 0; pos < len(markup); {
		if markup[pos] != '<' {
			end := strings.IndexByte(markup[pos:], '<')
			if end < 0 {
				end = len(markup) - pos
			}
			for _, node := range stack {
				node.text += html.UnescapeString(markup[pos : pos+end])
				node.rawText += markup[pos : pos+end]
			}
			pos += end
			continue
		}
		end := pos + 1
		var quote byte
		for ; end < len(markup); end++ {
			char := markup[end]
			if quote != 0 {
				if char == quote {
					quote = 0
				}
				continue
			}
			if char == '"' || char == '\'' {
				quote = char
				continue
			}
			if char == '>' {
				break
			}
		}
		if end == len(markup) {
			t.Fatal("unterminated tag")
		}
		token := strings.TrimSpace(markup[pos+1 : end])
		if strings.HasPrefix(token, "/") {
			if len(stack) == 1 || stack[len(stack)-1].tag != strings.TrimSpace(token[1:]) {
				t.Fatalf("unmatched closing tag %q", token)
			}
			stack[len(stack)-1].content = markup[starts[len(starts)-1]:pos]
			stack[len(stack)-1].markupEnd = end + 1
			stack = stack[:len(stack)-1]
			starts = starts[:len(starts)-1]
		} else {
			nameEnd := strings.IndexAny(token, " \t\r\n/")
			if nameEnd < 0 {
				nameEnd = len(token)
			}
			node := &templateElement{tag: token[:nameEnd], attrs: map[string]string{}, rawAttrs: map[string]string{}, textStart: len(root.text), markupStart: pos, markupEnd: end + 1}
			for _, attr := range templateAttribute.FindAllStringSubmatch(strings.TrimSpace(strings.TrimSuffix(token[nameEnd:], "/")), -1) {
				value := attr[2] + attr[3] + attr[4]
				node.attrs[attr[1]] = html.UnescapeString(value)
				node.rawAttrs[attr[1]] = value
			}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, node)
			if !strings.HasSuffix(token, "/") && !slices.Contains([]string{"input", "link", "meta", "br", "img", "hr"}, node.tag) {
				stack = append(stack, node)
				starts = append(starts, end+1)
			}
		}
		pos = end + 1
	}
	if len(stack) != 1 {
		t.Fatal("unclosed tag")
	}
	return root
}

func templateFind(root *templateElement, tag string) []*templateElement {
	var result []*templateElement
	for _, child := range root.children {
		if tag == "" || child.tag == tag {
			result = append(result, child)
		}
		result = append(result, templateFind(child, tag)...)
	}
	return result
}

func templateOne(t *testing.T, root *templateElement, tag string) *templateElement {
	t.Helper()
	found := templateFind(root, tag)
	if len(found) != 1 {
		t.Fatalf("got %d %s hooks, want one", len(found), tag)
	}
	return found[0]
}

func templateHook(t *testing.T, root *templateElement, tag string, attrs map[string]string, text string, presentAttrs ...string) *templateElement {
	t.Helper()
	for _, node := range templateFind(root, tag) {
		matches := text == "" || node.text == text
		for name, value := range attrs {
			got, present := node.attrs[name]
			if name == "class" {
				matches = matches && slices.Contains(strings.Fields(got), value)
			} else {
				matches = matches && present && got == value
			}
		}
		for _, name := range presentAttrs {
			_, present := node.attrs[name]
			matches = matches && present
		}
		if matches {
			return node
		}
	}
	t.Fatalf("missing %s hook with attributes %v and text %q", tag, attrs, text)
	return nil
}

func templateRawAttr(t *testing.T, node *templateElement, name, want string) {
	t.Helper()
	got, present := node.rawAttrs[name]
	if !present || got != want {
		t.Fatalf("raw %s attribute %s = %q (present %v), want %q", node.tag, name, got, present, want)
	}
}

func templateAttr(t *testing.T, node *templateElement, name, want string) {
	t.Helper()
	got, ok := node.attrs[name]
	if !ok || got != want {
		t.Fatalf("%s attribute %s = %q (present %v), want %q", node.tag, name, got, ok, want)
	}
}

func templatePresentAttr(t *testing.T, node *templateElement, name string) {
	t.Helper()
	if _, present := node.attrs[name]; !present {
		t.Fatalf("missing %s attribute %s", node.tag, name)
	}
}

func templateNoAttr(t *testing.T, node *templateElement, name string) {
	t.Helper()
	if _, ok := node.attrs[name]; ok {
		t.Fatalf("unexpected %s attribute %s", node.tag, name)
	}
}

func templateExecute(t *testing.T, set *template.Template, name string, data any) string {
	t.Helper()
	var output bytes.Buffer
	if err := set.ExecuteTemplate(&output, name, data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func templateRender(t *testing.T, name string, data Banner) *templateElement {
	t.Helper()
	return templateDocument(t, templateExecute(t, Templates(), name, data))
}

func templateFixture() Banner {
	return Banner{Service: "notes", Email: "member@example.test", ProfileURL: "/profile", LogoutURL: "/logout", Services: []Service{
		{Name: "notes", URL: "/notes", Icon: template.HTML(`<svg data-icon="notes"></svg>`), Enabled: true, Current: true},
		{Name: "calendar", URL: "/calendar", Icon: template.HTML(`<svg data-icon="calendar"></svg>`), Enabled: false},
	}}
}

func TestTemplatesSignature(t *testing.T) {
	// R-6RW6-28TK
	if _, ok := any(Templates).(func() *template.Template); !ok {
		t.Fatalf("Templates has type %T, want func() *template.Template", Templates)
	}
}

func TestTemplatesEmbeddedDefinitions(t *testing.T) {
	// R-B3I2-CXVU
	markup, err := assetsFS.ReadFile("assets/banner.html")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := template.New("reference").Parse(string(markup))
	if err != nil {
		t.Fatal(err)
	}
	actual := Templates()
	if actual == nil {
		t.Fatal("nil set")
	}
	for _, name := range []string{"banner", "launcher", "footer"} {
		if actual.Lookup(name) == nil {
			t.Fatalf("missing %s", name)
		}
		for _, data := range []Banner{{}, templateFixture()} {
			if got, want := templateExecute(t, actual, name, data), templateExecute(t, expected, name, data); got != want {
				t.Fatalf("%s differs from embedded asset", name)
			}
		}
	}
}

func TestTemplatesNames(t *testing.T) {
	// R-B4PY-QPMJ
	set := Templates()
	allowed := []string{"appkit", "banner", "launcher", "footer"}
	if !slices.Contains(allowed, set.Name()) {
		t.Fatalf("root name %q", set.Name())
	}
	for _, member := range set.Templates() {
		if !slices.Contains(allowed, member.Name()) {
			t.Fatalf("template name %q", member.Name())
		}
	}
}

func TestTemplatesIndependent(t *testing.T) {
	// R-6WRR-LBSC
	first, second := Templates(), Templates()
	if first == second {
		t.Fatal("shared set")
	}
	data := templateFixture()
	baseline := templateExecute(t, second, "banner", data)
	if _, err := first.Parse(`{{define "consumer"}}custom{{end}}{{define "banner"}}changed{{end}}{{define "launcher"}}other{{end}}`); err != nil {
		t.Fatal(err)
	}
	templateExecute(t, first, "banner", data)
	if second.Lookup("consumer") != nil {
		t.Fatal("consumer definition leaked")
	}
	if got := templateExecute(t, second, "banner", data); got != baseline {
		t.Fatal("redefinition or execution affected other set")
	}
	third := Templates()
	if _, err := third.Parse(`{{define "later"}}ok{{end}}`); err != nil {
		t.Fatalf("other execution made fresh set unparseable: %v", err)
	}
	if got := templateExecute(t, third, "banner", data); got != baseline {
		t.Fatal("future set affected")
	}
}

func TestTemplatesConsumerParse(t *testing.T) {
	// R-B5XV-4HD8
	const page = `{{define "page"}}{{template "banner" .}}{{template "launcher" .}}{{template "footer" .}}{{end}}`
	data := templateFixture()
	want := templateExecute(t, Templates(), "banner", data) + templateExecute(t, Templates(), "launcher", data) + templateExecute(t, Templates(), "footer", data)
	for _, mode := range []string{"Parse", "ParseFS"} {
		t.Run(mode, func(t *testing.T) {
			set := Templates()
			var err error
			if mode == "Parse" {
				_, err = set.Parse(page)
			} else {
				_, err = set.ParseFS(fstest.MapFS{"pages/page.html": &fstest.MapFile{Data: []byte(page)}}, "pages/*.html")
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := templateExecute(t, set, "page", data); got != want {
				t.Fatal("consumer did not render banner, launcher, and footer")
			}
		})
	}
}

func templateSignOut(t *testing.T, root *templateElement, logout string) {
	t.Helper()
	for _, form := range templateFind(root, "form") {
		if form.attrs["method"] != "post" || form.attrs["action"] != logout {
			continue
		}
		for _, button := range templateFind(form, "button") {
			if button.attrs["type"] == "submit" && button.text == "Sign out" {
				return
			}
		}
	}
	t.Fatal("missing sign-out form and submit button")
}

func templateCheckBannerIdentity(t *testing.T, root *templateElement, data Banner) {
	t.Helper()
	templateHook(t, root, "strong", map[string]string{"class": "mark", "data-service": data.Service}, "ikigenba")
	templateCheckProfile(t, root, data)
	templateSignOut(t, root, data.LogoutURL)
}

func TestBannerMark(t *testing.T) {
	// R-S1G1-Y5AY
	data := templateFixture()
	mark := templateHook(t, templateRender(t, "banner", data), "strong", map[string]string{"class": "mark", "data-service": data.Service}, "ikigenba")
	templateAttr(t, mark, "data-service", data.Service)
	if mark.text != "ikigenba" {
		t.Fatalf("mark text %q", mark.text)
	}
}

func templateCheckProfile(t *testing.T, root *templateElement, data Banner) {
	t.Helper()
	var profiles []*templateElement
	for _, link := range templateFind(root, "a") {
		if slices.Contains(strings.Fields(link.attrs["class"]), "profile") {
			profiles = append(profiles, link)
		}
	}
	if len(profiles) != 1 {
		t.Fatalf("profile links: %d, want one", len(profiles))
	}
	link := profiles[0]
	url := templateOne(t, templateDocument(t, templateEscape(t, `<a href="{{.}}"></a>`, data.ProfileURL)), "a").attrs["href"]
	title := templateOne(t, templateDocument(t, templateEscape(t, `<a title="{{.}}"></a>`, data.Email)), "a").attrs["title"]
	templateAttr(t, link, "href", url)
	templateAttr(t, link, "aria-label", "Profile")
	templateAttr(t, link, "title", title)
	if strings.TrimSpace(link.text) != "" {
		t.Fatalf("profile contains text %q", link.text)
	}
}

func TestBannerProfile(t *testing.T) {
	// R-S2NY-BX1N
	for _, services := range [][]Service{nil, templateFixture().Services} {
		data := templateFixture()
		data.Services = services
		templateCheckProfile(t, templateRender(t, "banner", data), data)
	}
}

func TestBannerSignOut(t *testing.T) {
	// R-S3VU-POSC
	data := templateFixture()
	templateSignOut(t, templateRender(t, "banner", data), data.LogoutURL)
}

func TestBannerLauncherButton(t *testing.T) {
	// R-72V9-I6HT
	root := templateRender(t, "banner", templateFixture())
	var buttons []*templateElement
	for _, button := range templateFind(root, "button") {
		if slices.Contains(strings.Fields(button.attrs["class"]), "launcher") {
			buttons = append(buttons, button)
		}
	}
	if len(buttons) != 1 {
		t.Fatalf("launcher buttons: %d", len(buttons))
	}
	for key, value := range map[string]string{"popovertarget": "services", "aria-label": "Services", "title": "Services"} {
		templateAttr(t, buttons[0], key, value)
	}
}

func TestBannerInvokesLauncherOnce(t *testing.T) {
	// R-7435-VY8I
	data := templateFixture()
	set := Templates()
	if _, err := set.Parse(`{{define "launcher"}}sentinel:{{.Service}}/{{.Email}}/{{.ProfileURL}}/{{.LogoutURL}}{{range .Services}}/{{.Name}}/{{.URL}}/{{.Enabled}}/{{.Current}}/{{.Icon}}{{end}}:end{{end}}`); err != nil {
		t.Fatal(err)
	}
	launcher := templateExecute(t, set, "launcher", data)
	if got := templateExecute(t, set, "banner", data); strings.Count(got, launcher) != 1 {
		t.Fatal("launcher must execute once with identical data")
	}
}

func TestBannerScript(t *testing.T) {
	// R-75B2-9PZ7
	script := templateOne(t, templateRender(t, "banner", templateFixture()), "script")
	templateAttr(t, script, "src", "/_appkit/launcher.js")
	templatePresentAttr(t, script, "defer")
}

func TestBannerEmptyServices(t *testing.T) {
	// R-S9ZC-MJHT
	for _, services := range [][]Service{nil, {}} {
		data := templateFixture()
		data.Services = services
		root := templateRender(t, "banner", data)
		templateCheckBannerIdentity(t, root, data)
		for _, node := range templateFind(root, "") {
			if node.tag == "script" || node.tag == "input" {
				t.Fatalf("unexpected %s", node.tag)
			}
			templateNoAttr(t, node, "popovertarget")
			if node.attrs["id"] == "services" {
				t.Fatal("unexpected services id")
			}
			for _, class := range strings.Fields(node.attrs["class"]) {
				if class == "launcher" || class == "services" {
					t.Fatalf("unexpected class %s", class)
				}
			}
		}
	}
}

func TestBannerNoStylesheet(t *testing.T) {
	// R-77QV-19GL
	for _, data := range []Banner{{}, templateFixture()} {
		if len(templateFind(templateRender(t, "banner", data), "link")) != 0 {
			t.Fatal("banner emits link")
		}
	}
}

func TestLauncherContainer(t *testing.T) {
	// R-78YR-F17A
	data := templateFixture()
	root := templateRender(t, "launcher", data)
	nav := templateHook(t, root, "nav", map[string]string{"class": "services", "id": "services", "aria-label": "Services"}, "", "popover")
	for key, value := range map[string]string{"id": "services", "aria-label": "Services"} {
		templateAttr(t, nav, key, value)
	}
	templateHook(t, nav, "input", map[string]string{"type": "search", "placeholder": "Find a service", "aria-label": "Find a service"}, "")
	for _, service := range data.Services {
		var found bool
		for _, tile := range templateFind(nav, "li") {
			for _, link := range templateFind(tile, "a") {
				content := strings.TrimLeft(link.content, " \t\r\n")
				icon := string(service.Icon)
				if strings.HasPrefix(content, icon) && strings.TrimSpace(templateDocument(t, strings.TrimPrefix(content, icon)).rawText) == templateEscape(t, "{{.}}", service.Name) {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("service tile %q is missing from nav", service.Name)
		}
	}
	paragraph := templateHook(t, nav, "p", nil, "No service matches .", "hidden")
	templateHook(t, paragraph, "q", nil, "")

}

func TestLauncherSearch(t *testing.T) {
	// R-7A6N-SSXZ
	input := templateHook(t, templateRender(t, "launcher", templateFixture()), "input", map[string]string{"type": "search", "placeholder": "Find a service", "aria-label": "Find a service"}, "")
	for key, value := range map[string]string{"type": "search", "placeholder": "Find a service", "aria-label": "Find a service"} {
		templateAttr(t, input, key, value)
	}
}

func TestLauncherTiles(t *testing.T) {
	// R-S53R-3GJ1
	data := templateFixture()
	data.Services = append(data.Services, data.Services[0])
	for _, services := range [][]Service{data.Services, nil, {}} {
		data.Services = services
		tiles := templateFind(templateRender(t, "launcher", data), "li")
		if len(tiles) != len(services) {
			t.Fatalf("tiles %d, services %d", len(tiles), len(services))
		}
		for index, tile := range tiles {
			link := templateOne(t, tile, "a")
			icon := string(services[index].Icon)
			content := strings.TrimLeft(link.content, " \t\r\n")
			if !strings.HasPrefix(content, icon) {
				t.Fatalf("tile %d icon changed: %q", index, link.content)
			}
			name := templateEscape(t, "{{.}}", services[index].Name)
			if strings.TrimSpace(strings.TrimPrefix(content, icon)) != name {
				t.Fatalf("tile %d name changed: %q", index, link.content)
			}
		}
	}
}

// templateTileLinks applies the explicit per-service li/a contract, allowing
// unrelated anchors elsewhere in the launcher.
func templateTileLinks(t *testing.T, root *templateElement, services []Service) []*templateElement {
	t.Helper()
	tiles := templateFind(root, "li")
	if len(tiles) != len(services) {
		t.Fatalf("tiles %d, services %d", len(tiles), len(services))
	}
	links := make([]*templateElement, len(tiles))
	for index, tile := range tiles {
		links[index] = templateOne(t, tile, "a")
	}
	return links
}

func TestLauncherEnabled(t *testing.T) {
	// R-S6BN-H89Q
	for _, current := range []bool{false, true} {
		data := Banner{Services: []Service{{Name: "enabled", URL: "/service", Enabled: true, Current: current}}}
		link := templateTileLinks(t, templateRender(t, "launcher", data), data.Services)[0]
		templateAttr(t, link, "href", "/service")
		templateNoAttr(t, link, "aria-disabled")
	}
}

func TestLauncherDisabled(t *testing.T) {
	// R-S7JJ-V00F
	for _, current := range []bool{false, true} {
		data := Banner{Services: []Service{{Name: "disabled", URL: "/unused", Current: current}}}
		link := templateTileLinks(t, templateRender(t, "launcher", data), data.Services)[0]
		templateNoAttr(t, link, "href")
		templateAttr(t, link, "aria-disabled", "true")
		templateAttr(t, link, "title", "disabled is unavailable")
	}
}

func TestLauncherCurrent(t *testing.T) {
	// R-7HI2-3FE5
	for _, enabled := range []bool{false, true} {
		for _, current := range []bool{false, true} {
			data := Banner{Services: []Service{{Name: "service", Enabled: enabled, Current: current}}}
			link := templateTileLinks(t, templateRender(t, "launcher", data), data.Services)[0]
			if current {
				templateAttr(t, link, "aria-current", "page")
			} else {
				templateNoAttr(t, link, "aria-current")
			}
		}
	}
}

func TestLauncherNoMatch(t *testing.T) {
	// R-7IPY-H74U
	paragraph := templateHook(t, templateRender(t, "launcher", templateFixture()), "p", nil, "No service matches .", "hidden")
	templatePresentAttr(t, paragraph, "hidden")
	quote := templateOne(t, paragraph, "q")
	if quote.content != "" {
		t.Fatalf("nonempty q %q", quote.content)
	}
	// Read the text around the q hook; no layout or styling is asserted.
	offset := quote.textStart - paragraph.textStart
	if offset < 0 || offset > len(paragraph.text) || paragraph.text[:offset] != "No service matches " || paragraph.text[offset:] != "." {
		t.Fatalf("no-match text %q", paragraph.text)
	}
}

func templateEscape(t *testing.T, markup string, value string) string {
	t.Helper()
	set, err := template.New("escape").Parse(markup)
	if err != nil {
		t.Fatal(err)
	}
	return templateExecute(t, set, "escape", value)
}

func TestTemplatesAutoescaping(t *testing.T) {
	// R-S085-KDK9
	const special = "<> &\"'+\x00"
	for _, url := range []string{"/some path?q=<> &\"'+", "javascript:alert(1)", "https://example.test/a b"} {
		data := Banner{Service: special, Version: "build" + special, Email: "email" + special, ProfileURL: url, LogoutURL: url, Services: []Service{{Name: "enabled" + special, URL: url, Enabled: true}, {Name: "disabled" + special, Enabled: false}}}
		root := templateRender(t, "banner", data)
		referenceAttr := templateOne(t, templateDocument(t, templateEscape(t, `<span title="{{.}}"></span>`, special)), "span").rawAttrs["title"]
		referenceURL := templateOne(t, templateDocument(t, templateEscape(t, `<a href="{{.}}"></a>`, url)), "a").rawAttrs["href"]
		mark := templateHook(t, root, "strong", map[string]string{"class": "mark"}, "ikigenba")
		templateRawAttr(t, mark, "data-service", referenceAttr)
		profile := templateHook(t, root, "a", map[string]string{"class": "profile"}, "")
		email := templateOne(t, templateDocument(t, templateEscape(t, `<span title="{{.}}"></span>`, data.Email)), "span").rawAttrs["title"]
		templateRawAttr(t, profile, "title", email)
		templateRawAttr(t, profile, "href", referenceURL)
		form := templateHook(t, root, "form", map[string]string{"method": "post", "action": html.UnescapeString(referenceURL)}, "")
		templateRawAttr(t, form, "action", referenceURL)
		for _, service := range data.Services {
			name := templateEscape(t, "{{.}}", service.Name)
			link := templateHook(t, root, "a", nil, html.UnescapeString(name))
			if strings.TrimSpace(link.rawText) != name {
				t.Fatalf("service name escaping %q, want %q", link.rawText, name)
			}
			if service.Enabled {
				templateRawAttr(t, link, "href", referenceURL)
			} else {
				title := templateOne(t, templateDocument(t, templateEscape(t, `<span title="{{.}} is unavailable"></span>`, service.Name)), "span").rawAttrs["title"]
				templateRawAttr(t, link, "title", title)
			}
		}
		footer := templateOne(t, templateRender(t, "footer", data), "footer")
		want := templateEscape(t, "{{.}}", data.Service) + " " + templateEscape(t, "{{.}}", data.Version)
		if footer.rawText != want {
			t.Fatalf("footer escaping %q, want %q", footer.rawText, want)
		}
	}
}

func TestFooter(t *testing.T) {
	// R-S8RG-8RR4
	for _, data := range []Banner{{}, {Service: "notes", Version: "development"}, {Service: " <app>& ", Version: " build+\"' \x00"}} {
		output := templateExecute(t, Templates(), "footer", data)
		root := templateDocument(t, output)
		footer := templateOne(t, root, "footer")
		want := templateEscape(t, "{{.}}", data.Service) + " " + templateEscape(t, "{{.}}", data.Version)
		if len(root.children) != 1 || footer.content != want {
			t.Fatalf("footer content %q, want %q, top-level elements %d", footer.content, want, len(root.children))
		}
		if strings.TrimSpace(output[:footer.markupStart]) != "" || strings.TrimSpace(output[footer.markupEnd:]) != "" {
			t.Fatalf("output outside footer %q", output)
		}
	}
}

func TestLauncherIconUnaltered(t *testing.T) {
	// R-7L5R-8QM8
	icons := []template.HTML{`<svg data-icon="a&b"><path d="M0 1"/></svg>`, `<svg><title> A &amp; B </title></svg>`}
	data := Banner{Services: []Service{{Name: "first", Icon: icons[0]}, {Name: "second", Icon: icons[1]}}}
	links := templateTileLinks(t, templateRender(t, "launcher", data), data.Services)
	for index, link := range links {
		if !strings.HasPrefix(strings.TrimLeft(link.content, " \t\r\n"), string(icons[index])) {
			t.Fatalf("icon %d changed: %q", index, link.content)
		}
	}
}
