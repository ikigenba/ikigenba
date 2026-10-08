package page

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
	tag          string
	attrs        map[string]string
	rawAttrs     map[string]string
	children     []*templateElement
	content      string
	text         string
	rawText      string
	textStart    int
	contentStart int
	markupStart  int
	markupEnd    int
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
			node := &templateElement{tag: token[:nameEnd], attrs: map[string]string{}, rawAttrs: map[string]string{}, textStart: len(root.text), contentStart: end + 1, markupStart: pos, markupEnd: end + 1}
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

func templateClass(t *testing.T, node *templateElement, class string) {
	t.Helper()
	if !slices.Contains(strings.Fields(node.attrs["class"]), class) {
		t.Fatalf("%s missing class %s", node.tag, class)
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
	return templateDocument(t, templateOutputWithoutIcons(t, name, data))
}

// Fixture Icons contain distinctive complete values, so removing one complete
// value per service cannot remove a hook merely sharing part of an Icon's bytes.
func templateOutputWithoutIcons(t *testing.T, name string, data Banner) string {
	t.Helper()
	output := templateExecute(t, Templates(), name, data)
	if name == "footer" {
		return output
	}
	icons := []template.HTML{}
	if name == "banner" {
		icons = append(icons, data.Icon)
	}
	for _, service := range data.Services {
		icons = append(icons, service.Icon)
	}
	for _, value := range icons {
		icon := string(value)
		if icon == "" {
			continue
		}
		before, after, found := strings.Cut(output, icon)
		if !found {
			t.Fatalf("missing unaltered Icon insertion %q", icon)
		}
		output = before + after
	}
	return output
}

func templateFixture() Banner {
	return Banner{Service: "notes", Icon: template.HTML(`<svg data-icon="banner"></svg>`), Email: "member@example.test", ProfileURL: "/profile", LogoutURL: "/logout", Services: []Service{
		{Name: "notes", URL: "/notes", Icon: template.HTML(`<svg data-icon="notes"></svg>`), Enabled: true, Current: true},
		{Name: "calendar", URL: "/calendar", Icon: template.HTML(`<svg data-icon="calendar"></svg>`), Enabled: false},
	}}
}

func TestTemplatesSignature(t *testing.T) {
	// R-I900-VFBX
	if _, ok := any(Templates).(func() *template.Template); !ok {
		t.Fatalf("Templates has type %T, want func() *template.Template", Templates)
	}
}

func TestTemplatesEmbeddedDefinitions(t *testing.T) {
	// R-55GA-E3MI
	markup, err := assetsFS.ReadFile("assets/banner.html")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := template.New("reference").Funcs(template.FuncMap{"preloadURL": PreloadURL}).Parse(string(markup))
	if err != nil {
		t.Fatal(err)
	}
	actual := Templates()
	if actual == nil {
		t.Fatal("nil set")
	}
	for _, name := range []string{"banner", "launcher", "footer", "preload"} {
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
	// R-56O6-RVD7
	set := Templates()
	allowed := []string{"appkit", "banner", "launcher", "footer", "preload"}
	// R-YHYW-CTC1
	if set.Name() != "appkit" {
		t.Fatalf("root name %q", set.Name())
	}
	for _, member := range set.Templates() {
		if !slices.Contains(allowed, member.Name()) {
			t.Fatalf("template name %q", member.Name())
		}
	}
}

func TestTemplatesIndependent(t *testing.T) {
	// R-ICNQ-0QK0
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
	// R-57W3-5N3W
	const page = `{{define "page"}}{{template "banner" .}}{{template "launcher" .}}{{template "footer" .}}{{template "preload"}}{{end}}`
	data := templateFixture()
	want := templateExecute(t, Templates(), "banner", data) + templateExecute(t, Templates(), "launcher", data) + templateExecute(t, Templates(), "footer", data) + templateExecute(t, Templates(), "preload", nil)
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
				t.Fatal("consumer did not render banner, launcher, footer, and preload")
			}
		})
	}
}

func templateSignOut(t *testing.T, root *templateElement, logout string) {
	t.Helper()
	form := templateOne(t, root, "form")
	templateClass(t, form, "inline")
	templateAttr(t, form, "method", "post")
	url := templateOne(t, templateDocument(t, templateEscape(t, `<form action="{{.}}"></form>`, logout)), "form").attrs["action"]
	templateAttr(t, form, "action", url)
	button := templateOne(t, form, "button")
	for name, value := range map[string]string{"type": "submit", "aria-label": "Sign out", "title": "Sign out"} {
		templateAttr(t, button, name, value)
	}
	templateClass(t, button, "signout")
	if strings.TrimSpace(button.text) != "" {
		t.Fatalf("signout contains text %q", button.text)
	}
}

func templateCheckBannerMark(t *testing.T, root *templateElement, data Banner) {
	t.Helper()
	var marks []*templateElement
	for _, node := range templateFind(root, "strong") {
		if slices.Contains(strings.Fields(node.attrs["class"]), "mark") {
			marks = append(marks, node)
		}
	}
	if len(marks) != 1 {
		t.Fatalf("mark count %d, want one", len(marks))
	}
	mark := marks[0]
	escaped := templateEscape(t, "{{.}}", data.Service)
	templateAttr(t, mark, "data-service", html.UnescapeString(escaped))
	if len(mark.children) != 2 || mark.children[0].tag != "img" || mark.children[1].tag != "span" {
		t.Fatal("mark must contain img then span")
	}
	image, service := mark.children[0], mark.children[1]
	templateAttr(t, image, "src", "/_appkit/favicon.svg")
	templateAttr(t, image, "alt", "")
	templateClass(t, service, "service")
	before := mark.content[:image.markupStart-mark.contentStart]
	between := mark.content[image.markupEnd-mark.contentStart : service.markupStart-mark.contentStart]
	after := mark.content[service.markupEnd-mark.contentStart:]
	if strings.TrimSpace(before) != "" || strings.TrimSpace(between) != "Ikigenba" || strings.TrimSpace(after) != "" {
		t.Fatalf("unexpected mark content %q", mark.content)
	}
}

func templateCheckBannerService(t *testing.T, root *templateElement, data Banner) {
	t.Helper()
	service := templateHook(t, root, "span", map[string]string{"class": "service"}, "")
	if strings.TrimSpace(service.content) != templateEscape(t, "{{.}}", data.Service) {
		t.Fatalf("service content %q", service.content)
	}
}

func templateCheckBannerIdentity(t *testing.T, root *templateElement, data Banner) {
	t.Helper()
	templateCheckBannerMark(t, root, data)
	templateCheckBannerService(t, root, data)
	templateCheckProfile(t, root, data)
	templateSignOut(t, root, data.LogoutURL)
}

func TestBannerMark(t *testing.T) {
	// R-SIQG-8A9I
	for _, name := range []string{"notes", "<app>&\"'", ""} {
		data := templateFixture()
		data.Service = name
		templateCheckBannerMark(t, templateRender(t, "banner", data), data)
	}
}

func TestBannerServiceIcon(t *testing.T) {
	// R-SJYC-M207
	for _, icon := range []template.HTML{"", `
<svg data-icon="a&b"><title> A &amp; B </title><path d="M0 1"/></svg>`} {
		for _, name := range []string{"notes", "<app>&\"'", ""} {
			data := Banner{Service: name, Icon: icon}
			root := templateDocument(t, templateExecute(t, Templates(), "banner", data))
			service := templateHook(t, root, "span", map[string]string{"class": "service"}, "")
			before, after, found := strings.Cut(service.content, string(icon))
			if !found || strings.TrimSpace(before) != "" || strings.TrimSpace(after) != templateEscape(t, "{{.}}", name) {
				t.Fatalf("service icon or name changed: %q", service.content)
			}
		}
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
	// R-IGBF-61S3
	for _, services := range [][]Service{nil, templateFixture().Services} {
		data := templateFixture()
		data.Services = services
		templateCheckProfile(t, templateRender(t, "banner", data), data)
	}
}

func TestBannerSignOut(t *testing.T) {
	// R-SL68-ZTQW
	data := templateFixture()
	templateSignOut(t, templateRender(t, "banner", data), data.LogoutURL)
}

func TestBannerLauncherButton(t *testing.T) {
	// R-IJZ4-BD06
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

func TestBannerHeaderOrder(t *testing.T) {
	// R-SME5-DLHL
	for _, services := range [][]Service{nil, {}, templateFixture().Services[:1], templateFixture().Services} {
		data := templateFixture()
		data.Services = services
		header := templateOne(t, templateRender(t, "banner", data), "header")
		tags, classes := []string{"strong", "a", "form"}, []string{"mark", "profile", "inline"}
		if len(services) > 0 {
			tags = []string{"strong", "button", "a", "form"}
			classes = []string{"mark", "launcher", "profile", "inline"}
		}
		if len(header.children) != len(tags) {
			t.Fatalf("header children %d, want %d", len(header.children), len(tags))
		}
		for index, child := range header.children {
			if child.tag != tags[index] {
				t.Fatalf("header child %d = %s, want %s", index, child.tag, tags[index])
			}
			templateClass(t, child, classes[index])
		}
	}
}

func TestBannerInvokesLauncherOnce(t *testing.T) {
	// R-IL70-P4QV
	data := templateFixture()
	data.Version = "consumer-build"
	set := Templates()
	if _, err := set.Parse(`{{define "launcher"}}sentinel:{{.Service}}/{{.Version}}/{{.Email}}/{{.ProfileURL}}/{{.LogoutURL}}{{range .Services}}/{{.Name}}/{{.URL}}/{{.Enabled}}/{{.Current}}/{{.Icon}}{{end}}:end{{end}}`); err != nil {
		t.Fatal(err)
	}
	launcher := templateExecute(t, set, "launcher", data)
	if got := templateExecute(t, set, "banner", data); strings.Count(got, launcher) != 1 {
		t.Fatal("launcher must execute once with identical data")
	}
}

func TestBannerScript(t *testing.T) {
	// R-IMEX-2WHK
	script := templateOne(t, templateRender(t, "banner", templateFixture()), "script")
	templateAttr(t, script, "src", "/_appkit/launcher.js")
	templatePresentAttr(t, script, "defer")
}

func TestBannerEmptyServices(t *testing.T) {
	// R-SNM1-RD8A
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
	// R-IOUP-UFYY
	for _, data := range []Banner{{}, templateFixture()} {
		if len(templateFind(templateRender(t, "banner", data), "link")) != 0 {
			t.Fatal("banner emits link")
		}
	}
}

func TestLauncherContainer(t *testing.T) {
	// R-IQ2M-87PN
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
				if strings.TrimSpace(link.rawText) == templateEscape(t, "{{.}}", service.Name) {
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
	// R-IRAI-LZGC
	input := templateHook(t, templateRender(t, "launcher", templateFixture()), "input", map[string]string{"type": "search", "placeholder": "Find a service", "aria-label": "Find a service"}, "")
	for key, value := range map[string]string{"type": "search", "placeholder": "Find a service", "aria-label": "Find a service"} {
		templateAttr(t, input, key, value)
	}
}

func TestLauncherTiles(t *testing.T) {
	// R-ISIE-ZR71
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
			name := templateEscape(t, "{{.}}", services[index].Name)
			if strings.TrimSpace(link.content) != name {
				t.Fatalf("tile %d name changed: %q", index, link.content)
			}
		}
		// Icon placement is checked separately on raw output, before stripping.
		raw := templateDocument(t, templateExecute(t, Templates(), "launcher", data))
		for index, link := range templateTileLinks(t, raw, services) {
			icon := string(services[index].Icon)
			content := strings.TrimLeft(link.content, " \t\r\n")
			if !strings.HasPrefix(content, icon) || strings.TrimSpace(strings.TrimPrefix(content, icon)) != templateEscape(t, "{{.}}", services[index].Name) {
				t.Fatalf("tile %d Icon must precede its name: %q", index, link.content)
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
	// R-ITQB-DIXQ
	for _, current := range []bool{false, true} {
		data := Banner{Services: []Service{{Name: "enabled", URL: "/service", Enabled: true, Current: current}}}
		link := templateTileLinks(t, templateRender(t, "launcher", data), data.Services)[0]
		templateAttr(t, link, "href", "/service")
		templateNoAttr(t, link, "aria-disabled")
	}
}

func TestLauncherDisabled(t *testing.T) {
	// R-IUY7-RAOF
	for _, current := range []bool{false, true} {
		data := Banner{Services: []Service{{Name: "disabled", URL: "/unused", Current: current}}}
		link := templateTileLinks(t, templateRender(t, "launcher", data), data.Services)[0]
		templateNoAttr(t, link, "href")
		templateAttr(t, link, "aria-disabled", "true")
		templateAttr(t, link, "title", "disabled is unavailable")
	}
}

func TestLauncherCurrent(t *testing.T) {
	// R-IW64-52F4
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
	// R-IXE0-IU5T
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
	// R-IYLW-WLWI
	const special = "<> &\"'+\x00"
	for _, url := range []string{"/some path?q=<> &\"'+", "javascript:alert(1)", "https://example.test/a b"} {
		data := Banner{Service: special, Version: "build" + special, Email: "email" + special, ProfileURL: url, LogoutURL: url, Services: []Service{{Name: "enabled" + special, URL: url, Enabled: true}, {Name: "disabled" + special, Enabled: false}}}
		root := templateRender(t, "banner", data)
		referenceAttr := templateOne(t, templateDocument(t, templateEscape(t, `<span title="{{.}}"></span>`, special)), "span").rawAttrs["title"]
		referenceURL := templateOne(t, templateDocument(t, templateEscape(t, `<a href="{{.}}"></a>`, url)), "a").rawAttrs["href"]
		mark := templateHook(t, root, "strong", map[string]string{"class": "mark"}, "")
		templateRawAttr(t, mark, "data-service", referenceAttr)
		serviceText := templateHook(t, mark, "span", map[string]string{"class": "service"}, "")
		if serviceText.rawText != templateEscape(t, "{{.}}", data.Service) {
			t.Fatalf("banner service escaping %q", serviceText.rawText)
		}
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
	// R-J11P-O5DW
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
	// R-IZTT-ADN7
	icons := []template.HTML{`<svg data-icon="a&b"><path d="M0 1"/></svg>`, `<svg><title> A &amp; B </title></svg>`}
	data := Banner{Services: []Service{{Name: "first", Icon: icons[0]}, {Name: "second", Icon: icons[1]}}}
	links := templateTileLinks(t, templateDocument(t, templateExecute(t, Templates(), "launcher", data)), data.Services)
	for index, link := range links {
		if !strings.HasPrefix(strings.TrimLeft(link.content, " \t\r\n"), string(icons[index])) {
			t.Fatalf("icon %d changed: %q", index, link.content)
		}
	}
}

func TestTemplatesIconOutputScope(t *testing.T) {
	// R-G687-LP8Y
	for _, icon := range []template.HTML{
		`<!--fixture-icon-start--><li><a href="/icon" aria-current="page">icon text</a></li><a class="profile">extra profile</a><button class="launcher">extra launcher</button><script></script><link><input><p hidden>No service matches <q>extra</q>.</p><!--fixture-icon-end-->`,
		`<!--fixture-icon-start--></a></li></ul></nav><header><strong class="mark">extra mark</strong></header><nav id="services" class="services"><li><a<!--fixture-icon-end-->`,
		`<!--fixture-icon-start--><nav<!--fixture-icon-end-->`,
		`<!--fixture-icon-start-->No service matches <!--fixture-icon-end-->`,
	} {
		data := templateFixture()
		data.Icon = icon
		// Repeated identical icons are distinct insertions; bytes such as <nav
		// and the no-match text also occur outside those insertions.
		for index := range data.Services {
			data.Services[index].Icon = icon
		}
		for _, name := range []string{"banner", "launcher"} {
			root := templateRender(t, name, data)
			if name == "banner" {
				if len(templateFind(root, "link")) != 0 {
					t.Fatal("banner counted a link from an Icon")
				}
				templateCheckBannerIdentity(t, root, data)
				templateOne(t, root, "script")
				button := templateHook(t, root, "button", map[string]string{"class": "launcher"}, "")
				mark := templateHook(t, root, "strong", map[string]string{"class": "mark"}, "")
				var adjacent bool
				for _, header := range templateFind(root, "header") {
					index := slices.Index(header.children, mark)
					adjacent = adjacent || index >= 0 && index+1 < len(header.children) && header.children[index+1] == button
				}
				if !adjacent {
					t.Fatal("Icon changed launcher/mark placement")
				}
			}
			nav := templateHook(t, root, "nav", map[string]string{"class": "services", "id": "services", "aria-label": "Services"}, "", "popover")
			templateAttr(t, nav, "id", "services")
			templateHook(t, nav, "input", map[string]string{"type": "search", "placeholder": "Find a service", "aria-label": "Find a service"}, "")
			links := templateTileLinks(t, nav, data.Services)
			for index, link := range links {
				service := data.Services[index]
				if strings.TrimSpace(link.text) != service.Name {
					t.Fatalf("Icon text counted toward service %q: %q", service.Name, link.text)
				}
				if service.Enabled {
					templateAttr(t, link, "href", service.URL)
					templateNoAttr(t, link, "aria-disabled")
				} else {
					templateNoAttr(t, link, "href")
					templateAttr(t, link, "aria-disabled", "true")
					templateAttr(t, link, "title", service.Name+" is unavailable")
				}
				if service.Current {
					templateAttr(t, link, "aria-current", "page")
				} else {
					templateNoAttr(t, link, "aria-current")
				}
			}
			paragraph := templateHook(t, nav, "p", nil, "No service matches .", "hidden")
			templatePresentAttr(t, paragraph, "hidden")
			quote := templateOne(t, paragraph, "q")
			if paragraph.text != "No service matches ." || quote.content != "" {
				t.Fatal("Icon changed no-match text")
			}
		}
		footer := templateOne(t, templateRender(t, "footer", data), "footer")
		if footer.text != data.Service+" "+data.Version {
			t.Fatal("Icon changed footer text")
		}
	}
}

func TestTemplatesPreloadURLFunction(t *testing.T) {
	// R-5ABV-X6LA
	for _, mode := range []string{"Parse", "ParseFS"} {
		set := Templates()
		const markup = `{{define "consumerURL"}}{{preloadURL}}{{end}}`
		var err error
		if mode == "Parse" {
			_, err = set.Parse(markup)
		} else {
			_, err = set.ParseFS(fstest.MapFS{"consumer.html": &fstest.MapFile{Data: []byte(markup)}}, "consumer.html")
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := templateExecute(t, set, "consumerURL", nil); got != PreloadURL() {
			t.Fatalf("preloadURL = %q, want %q", got, PreloadURL())
		}
	}
}

func TestPreloadTemplate(t *testing.T) {
	// R-593Z-JEUL
	set, err := Templates().Parse(`{{define "head"}}{{template "preload"}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	output := templateExecute(t, set, "head", nil)
	root := templateDocument(t, output)
	link := templateOne(t, root, "link")
	want := map[string]string{"rel": "preload", "as": "font", "type": "font/woff2", "crossorigin": "", "href": PreloadURL()}
	if len(link.attrs) != len(want) || len(root.children) != 1 {
		t.Fatalf("preload output = %q", output)
	}
	for name, value := range want {
		templateAttr(t, link, name, value)
	}
	if strings.TrimSpace(output[:link.markupStart]) != "" || strings.TrimSpace(output[link.markupEnd:]) != "" {
		t.Fatalf("output outside link = %q", output)
	}
}
