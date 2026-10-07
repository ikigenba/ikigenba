package server

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
)

type pageTag struct {
	name       string
	start, end int
	raw        string
}

func pageSpace(c byte) bool  { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
func pageLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func pageName(c byte) bool   { return pageLetter(c) || c >= '0' && c <= '9' || c == '-' }

// pageTags implements the lexical tag spans, skipping quoted runs and script data.
func pageTags(text string) []pageTag {
	var tags []pageTag
	for i := 0; i < len(text); i++ {
		if text[i] != '<' || i+1 == len(text) || !pageLetter(text[i+1]) {
			continue
		}
		n := i + 1
		for n < len(text) && pageName(text[n]) {
			n++
		}
		end := n
		quoted := false
		for end < len(text) {
			if text[end] == '"' {
				quoted = !quoted
			}
			if text[end] == '>' && !quoted {
				break
			}
			end++
		}
		if end == len(text) {
			break
		}
		tag := pageTag{text[i+1 : n], i, end + 1, text[i : end+1]}
		tags = append(tags, tag)
		i = end
		if strings.EqualFold(tag.name, "script") {
			if closeAt := strings.Index(strings.ToLower(text[i+1:]), "</script"); closeAt >= 0 {
				i += 1 + closeAt + len("</script>") - 1
			}
		}
	}
	return tags
}

// Attribute references have the tokenizer's ambiguous-ampersand exception.
func pageAttributeText(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '&' {
			out.WriteByte(text[i])
			i++
			continue
		}
		end := i + 1
		if end < len(text) && text[end] == '#' {
			end++
			if end < len(text) && (text[end] == 'x' || text[end] == 'X') {
				end++
			}
			for end < len(text) && (pageLetter(text[end]) || text[end] >= '0' && text[end] <= '9') {
				end++
			}
			if end < len(text) && text[end] == ';' {
				end++
			}
			raw := text[i:end]
			decoded := html.UnescapeString(raw)
			out.WriteString(decoded)
			i = end
			continue
		}
		for end < len(text) && (pageLetter(text[end]) || text[end] >= '0' && text[end] <= '9') {
			end++
		}
		if end < len(text) && text[end] == ';' {
			end++
		}
		consumed := 0
		decoded := ""
		for j := end; j > i+1; j-- {
			raw := text[i:j]
			v := html.UnescapeString(raw)
			if v != raw {
				consumed = j
				decoded = v
				break
			}
		}
		if consumed > 0 && (text[consumed-1] != ';' || decoded == html.UnescapeString(text[i:consumed-1])+";") {
			// Find the shortest decoded prefix: it is the named reference's consumed part.
			for j := i + 2; j < consumed; j++ {
				if v := html.UnescapeString(text[i:j]); v != text[i:j] {
					consumed = j
					decoded = v
					break
				}
			}
			if text[consumed-1] != ';' && consumed < len(text) && (pageLetter(text[consumed]) || text[consumed] >= '0' && text[consumed] <= '9' || text[consumed] == '=') {
				consumed = 0
			}
		}
		if consumed == 0 {
			out.WriteByte('&')
			i++
		} else {
			out.WriteString(decoded)
			i = consumed
		}
	}
	return out.String()
}
func pageAttrs(tag pageTag) map[string][]string {
	attrs := map[string][]string{}
	for i := 1 + len(tag.name); i < len(tag.raw); i++ {
		if tag.raw[i] == '"' {
			i++
			for i < len(tag.raw) && tag.raw[i] != '"' {
				i++
			}
			continue
		}
		if !pageSpace(tag.raw[i]) {
			continue
		}
		j := i + 1
		for j < len(tag.raw) && !pageSpace(tag.raw[j]) && !strings.ContainsRune(`/=>"`, rune(tag.raw[j])) {
			j++
		}
		if j == i+1 || j+1 >= len(tag.raw) || tag.raw[j] != '=' || tag.raw[j+1] != '"' {
			continue
		}
		k := j + 2
		for k < len(tag.raw) && tag.raw[k] != '"' {
			k++
		}
		if k == len(tag.raw) {
			continue
		}
		attrs[tag.raw[i+1:j]] = append(attrs[tag.raw[i+1:j]], pageAttributeText(tag.raw[j+2:k]))
		i = k
	}
	return attrs
}
func pageContent(text string, tag pageTag) string {
	end := strings.Index(text[tag.end:], "</"+tag.name+">")
	if end < 0 {
		return ""
	}
	return text[tag.end : tag.end+end]
}
func pageText(text string) string {
	for _, name := range []string{"script", "style"} {
		for {
			found := false
			for _, tag := range pageTags(text) {
				if tag.name == name {
					end := strings.Index(text[tag.end:], "</"+name+">")
					if end >= 0 {
						text = text[:tag.start] + text[tag.end+end+len(name)+3:]
						found = true
						break
					}
				}
			}
			if !found {
				break
			}
		}
	}
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '<' {
			end := strings.IndexByte(text[i:], '>')
			if end >= 0 {
				i += end
				continue
			}
		}
		out.WriteByte(text[i])
	}
	decoded := html.UnescapeString(out.String())
	out.Reset()
	space := false
	for i := 0; i < len(decoded); i++ {
		if pageSpace(decoded[i]) {
			space = true
			continue
		}
		if space && out.Len() > 0 {
			out.WriteByte(' ')
		}
		space = false
		out.WriteByte(decoded[i])
	}
	return out.String()
}
func pageElements(text, name string) []pageTag {
	var found []pageTag
	for _, tag := range pageTags(text) {
		if tag.name == name {
			found = append(found, tag)
		}
	}
	return found
}
func pageOne(t *testing.T, text, name string) pageTag {
	t.Helper()
	tags := pageElements(text, name)
	if len(tags) != 1 {
		t.Fatalf("%s count = %d in %s", name, len(tags), text)
	}
	return tags[0]
}
func pageAttr(t *testing.T, tag pageTag, name, want string) {
	t.Helper()
	got := pageAttrs(tag)[name]
	if len(got) != 1 || got[0] != want {
		t.Fatalf("%s %s = %#v want %q", tag.name, name, got, want)
	}
}
func pageSequence(t *testing.T, text string, names ...string) []pageTag {
	t.Helper()
	var result []pageTag
	offset := 0
	for _, name := range names {
		rest := text[offset:]
		lead := 0
		for lead < len(rest) && pageSpace(rest[lead]) {
			lead++
		}
		tags := pageTags(rest[lead:])
		if len(tags) == 0 || tags[0].start != 0 || tags[0].name != name {
			t.Fatalf("wanted next %s in %s", name, rest)
		}
		tag := tags[0]
		tag.start += offset + lead
		tag.end += offset + lead
		result = append(result, tag)
		content := pageContent(text, tag)
		offset = tag.end + len(content) + len(name) + 3
	}
	if strings.Trim(text[offset:], " \t\r\n\f") != "" {
		t.Fatalf("extra sequence content: %s", text[offset:])
	}
	return result
}

func authPageErrors(body string) []string {
	var errors []string
	bad := func(format string, args ...any) { errors = append(errors, fmt.Sprintf(format, args...)) }
	if len(body) < 15 || !strings.EqualFold(body[:15], "<!DOCTYPE html>") {
		bad("doctype")
	}
	tags := pageTags(body)
	bodies := pageElements(body, "body")
	if len(bodies) != 1 || strings.Count(body, "</body>") != 1 || strings.Index(body, "</body>") < bodies[0].end {
		bad("body count")
		return errors
	}
	bodyAt := bodies[0].start
	titleCount, stylesheet, viewport, feedback := 0, 0, 0, 0
	occupied := make([]bool, len(body))
	for i := 0; i < len(body) && i < 15; i++ {
		occupied[i] = true
	}
	for _, tag := range tags {
		for i := tag.start; i < tag.end; i++ {
			occupied[i] = true
		}
		attrs := pageAttrs(tag)
		lower := strings.ToLower(tag.name)
		if lower == "title" {
			titleCount++
			if tag.name != "title" || tag.start >= bodyAt || !strings.HasPrefix(body[tag.end:], "auth</title>") {
				bad("title")
			}
		}
		if tag.name == "link" && tag.start < bodyAt && len(attrs["rel"]) == 1 && attrs["rel"][0] == "stylesheet" {
			stylesheet++
			if len(attrs["href"]) != 1 || attrs["href"][0] != "/_appkit/theme.css" {
				bad("stylesheet")
			}
		}
		if tag.name == "meta" && tag.start < bodyAt && len(attrs["name"]) == 1 && attrs["name"][0] == "viewport" {
			viewport++
			if len(attrs["content"]) != 1 || attrs["content"][0] != "width=device-width, initial-scale=1" {
				bad("viewport")
			}
		}
		switch lower {
		case "style", "textarea", "xmp", "iframe", "noembed", "noframes", "noscript", "plaintext", "template", "math":
			bad("forbidden element %s", lower)
		}
		if lower == "script" {
			if tag.name == "script" && tag.start < bodyAt && len(attrs["src"]) == 1 && attrs["src"][0] == "/_appkit/feedback.js" {
				feedback++
				if !slices.Contains(pageAttributeNames(tag), "defer") {
					bad("feedback defer")
				}
			} else {
				bad("unplaced script")
			}
			closeAt := strings.Index(strings.ToLower(body[tag.end:]), "</script")
			if tag.name != "script" || closeAt < 0 {
				bad("script close")
			} else {
				end := tag.end + closeAt
				if !strings.HasPrefix(body[end:], "</script>") || strings.Contains(body[tag.end:end], "<!--") {
					bad("script data")
				}
				for i := tag.end; i < end && i < len(body); i++ {
					occupied[i] = true
				}
			}
		}
		// Lex all attribute-name positions, including malformed forms the occurrence reader ignores.
		for i := 1 + len(tag.name); i < len(tag.raw); {
			c := tag.raw[i]
			if pageSpace(c) || c == '/' || c == '>' {
				i++
				continue
			}
			if c == '\'' {
				bad("apostrophe outside value")
				i++
				continue
			}
			start := i
			for i < len(tag.raw) && !pageSpace(tag.raw[i]) && !strings.ContainsRune(`/=>"`, rune(tag.raw[i])) {
				i++
			}
			if start == i {
				bad("attribute grammar")
				i++
				continue
			}
			name := tag.raw[start:i]
			lname := strings.ToLower(name)
			if lname == "srcdoc" || lname == "http-equiv" || lname == "attributionsrc" || strings.HasPrefix(lname, "on") && len(lname) > 2 && allPageLetters(lname[2:]) {
				bad("forbidden attribute %s", name)
			}
			spaces := i
			for i < len(tag.raw) && pageSpace(tag.raw[i]) {
				i++
			}
			if i < len(tag.raw) && tag.raw[i] == '=' {
				if i != spaces || i+1 >= len(tag.raw) || tag.raw[i+1] != '"' {
					bad("attribute quoting")
					i++
					continue
				}
				if resourceName(lname) && (name != lname || start == 0 || !pageSpace(tag.raw[start-1])) {
					bad("resource attribute spelling")
				}
				i += 2
				for i < len(tag.raw) && tag.raw[i] != '"' {
					i++
				}
				if i == len(tag.raw) {
					bad("unclosed quote")
				} else {
					i++
				}
			}
		}
		for name, values := range attrs {
			for _, value := range values {
				if name == "style" {
					bad("inline style")
				}
				if name == "xlink:href" || name == "srcset" || name == "imagesrcset" || name == "ping" || name == "href" && lower != "a" && lower != "link" {
					bad("forbidden href/list")
				}
				if name == "action" || name == "formaction" || name == "href" && lower == "a" {
					if strings.ContainsAny(value, "\t\n\r") || !ownPagePath(value) {
						bad("navigation %q", value)
					}
				}
				if name == "src" || name == "poster" || name == "data" || name == "background" || name == "manifest" || name == "href" && lower == "link" {
					if strings.ContainsAny(value, "\t\n\r") || !strings.HasPrefix(value, "/_appkit/") || pageDotSegment(value) {
						bad("resource %q", value)
					}
				}
			}
		}
		if lower == "svg" {
			if tag.name != "svg" {
				bad("svg case")
			}
			buttons := pageElements(body, "button")
			found := false
			for _, button := range buttons {
				content := pageContent(body, button)
				if tag.start >= button.end && tag.start < button.end+len(content) && strings.HasPrefix(strings.TrimLeft(content, " \t\r\n\f"), tag.raw) {
					found = true
				}
			}
			if !found {
				bad("svg outside button")
			}
			if err := pageIconError(body, tag); err != "" {
				bad("icon: %s", err)
			}
		}
	}
	// The svg pin applies even inside a quoted run or script data.
	lowerBody := pageASCIILower(body)
	for offset := 0; offset < len(body); {
		found := strings.Index(lowerBody[offset:], "<svg")
		if found < 0 {
			break
		}
		pos := offset + found
		offset = pos + 4
		if offset < len(body) && pageName(body[offset]) {
			continue
		}
		matched := false
		for _, tag := range tags {
			if tag.start == pos && tag.name == "svg" {
				matched = true
				break
			}
		}
		if !matched {
			bad("unread svg occurrence")
		}
	}
	if titleCount != 1 || stylesheet != 1 || viewport != 1 || feedback != 1 {
		bad("head counts %d/%d/%d/%d", titleCount, stylesheet, viewport, feedback)
	}
	for i := 0; i < len(body); i++ {
		if occupied[i] || body[i] != '<' {
			continue
		}
		if i+2 >= len(body) || body[i+1] != '/' {
			bad("illegal less-than")
			continue
		}
		j := i + 2
		for j < len(body) && pageName(body[j]) {
			j++
		}
		if j == i+2 || j >= len(body) || body[j] != '>' {
			bad("end tag grammar")
		}
	}
	return errors
}
func allPageLetters(text string) bool {
	for i := range len(text) {
		if !pageLetter(text[i]) {
			return false
		}
	}
	return true
}
func resourceName(name string) bool {
	switch name {
	case "src", "poster", "data", "background", "manifest", "ping", "href", "xlink:href", "action", "formaction", "srcset", "imagesrcset", "style":
		return true
	default:
		return false
	}
}
func ownPagePath(value string) bool {
	return value == "/" || len(value) > 1 && value[0] == '/' && value[1] != '/' && value[1] != '\\'
}
func pageDotSegment(value string) bool {
	value = strings.SplitN(strings.SplitN(value, "?", 2)[0], "#", 2)[0]
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' }) {
		switch strings.ToLower(part) {
		case "..", ".%2e", "%2e.", "%2e%2e":
			return true
		}
	}
	return false
}
func pageAttributeNames(tag pageTag) []string {
	var names []string
	for i := 1 + len(tag.name); i < len(tag.raw); {
		if pageSpace(tag.raw[i]) || tag.raw[i] == '/' || tag.raw[i] == '>' {
			i++
			continue
		}
		if tag.raw[i] == '"' {
			i++
			for i < len(tag.raw) && tag.raw[i] != '"' {
				i++
			}
			i++
			continue
		}
		if tag.raw[i] == '=' {
			i++
			continue
		}
		start := i
		for i < len(tag.raw) && !pageSpace(tag.raw[i]) && !strings.ContainsRune(`/=>"`, rune(tag.raw[i])) {
			i++
		}
		if i == start {
			i++
			continue
		}
		names = append(names, tag.raw[start:i])
	}
	return names
}

func pageIconError(body string, tag pageTag) string {
	// R-FQZJ-J8UK: auth button icon vocabulary.
	want := map[string]string{"class": "ico", "aria-hidden": "true", "viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round"}
	attrs := pageAttrs(tag)
	if len(attrs) != len(want) || len(pageAttributeNames(tag)) != len(want) {
		return "svg attrs"
	}
	for name, value := range want {
		if len(attrs[name]) != 1 || attrs[name][0] != value {
			return "svg attr " + name
		}
	}
	content := pageContent(body, tag)
	paths := pageElements(content, "path")
	if len(paths) == 0 {
		return "missing paths"
	}
	offset := 0
	for _, path := range paths {
		if strings.Trim(content[offset:path.start], " \t\r\n\f") != "" {
			return "foreign svg content"
		}
		a := pageAttrs(path)
		if len(a) != 1 || len(a["d"]) != 1 || len(pageAttributeNames(path)) != 1 {
			return "path attrs"
		}
		offset = path.end
		if strings.HasPrefix(content[offset:], "</path>") {
			offset += 7
		}
	}
	if strings.Trim(content[offset:], " \t\r\n\f") != "" {
		return "svg trailing content"
	}
	return ""
}
func assertAuthPage(t *testing.T, body string) {
	t.Helper()
	if errors := authPageErrors(fixtureWrittenMarkup(t, body)); len(errors) > 0 {
		t.Fatalf("page errors: %v\n%s", errors, body)
	}
}

func TestPageReadingVocabulary(t *testing.T) {
	// R-VWSW-YH7W R-BSTK-NA80 R-PHJ1-EC2S R-PIQX-S3TH
	text := `<section title="a > <fake src=&#34;x&#34;>"><p data-x="&copy; &copyx &copy= &notin; &#x80;"> A &amp; B </p><script>"<fake>"</script></section>`
	tags := pageTags(text)
	if len(tags) != 3 || tags[0].name != "section" || tags[1].name != "p" || tags[2].name != "script" {
		t.Fatalf("tags = %#v", tags)
	}
	if got := pageAttrs(tags[0])["title"]; len(got) != 1 || got[0] != `a > <fake src="x">` {
		t.Fatalf("attr = %#v", got)
	}
	if got := pageAttrs(tags[1])["data-x"][0]; got != "© &copyx &copy= ∉ €" {
		t.Fatalf("references = %q", got)
	}
	if pageContent(text, tags[1]) != " A &amp; B " || pageText(pageContent(text, tags[0])) != "A & B" {
		t.Fatal("content/normalisation")
	}
	dupe := pageTags(`<input value="one" value="two" data-x="&amp= &amp; &notin; &notit;">`)[0]
	if len(pageAttrs(dupe)["value"]) != 2 || pageAttrs(dupe)["data-x"][0] != "&amp= & ∉ &notit;" {
		t.Fatal("duplicate/ambiguous attribute reads")
	}
	if pageContent(`<p>first</p>later</p>`, pageTags(`<p>first</p>later</p>`)[0]) != "first" {
		t.Fatal("first end tag")
	}
	if pageText("\tA\u00a0 B\n\rC <style>ignored</style><b>&lt;x&gt;</b>") != "A\u00a0 B C <x>" {
		t.Fatal("ASCII whitespace normalisation")
	}
	pageSequence(t, " <strong>T</strong> \n<p>X</p>", "strong", "p")
}

func assertSignInCard(t *testing.T, body, host, word, target, footer string) string {
	t.Helper()
	// R-PXDQ-DCPT R-PYLM-R4GI R-PZTJ-4W77 R-Q11F-INXW R-Q29B-WFOL R-3Q46-Q9HS
	inside := pageContent(body, pageOne(t, body, "body"))
	seq := pageSequence(t, inside, "main")
	pageAttr(t, seq[0], "class", "auth-page")
	main := pageContent(inside, seq[0])
	section := pageSequence(t, main, "section")[0]
	pageAttr(t, section, "class", "card")
	card := pageContent(main, section)
	if len(pageElements(body, "header")) != 0 {
		t.Fatal("sign-in header")
	}
	mark := pageOne(t, card, "span")
	pageAttr(t, mark, "class", "mark")
	if len(pageAttrs(mark)["data-service"]) != 0 || pageContent(card, mark) != "ikigenba" || !strings.HasPrefix(strings.TrimLeft(card, " \t\r\n\f"), mark.raw) {
		t.Fatal("bare mark")
	}
	heading := pageOne(t, body, "h1")
	if pageText(pageContent(body, heading)) != "Sign in to "+apexName(host) {
		t.Fatal("sign-in heading")
	}
	link := pageOne(t, card, "a")
	pageAttr(t, link, "class", "button secondary large google")
	pageAttr(t, link, "href", target)
	if pageText(pageContent(card, link)) != word {
		t.Fatal("link word")
	}
	footers := pageElements(card, "footer")
	if footer == "" {
		if len(footers) != 0 {
			t.Fatal("unexpected footer")
		}
	} else {
		if len(footers) != 1 || pageText(pageContent(card, footers[0])) != footer || !strings.HasSuffix(strings.TrimRight(card, " \t\r\n\f"), "</footer>") {
			t.Fatal("footer")
		}
	}
	return card
}
func assertChrome(t *testing.T, body, email string) string {
	t.Helper()
	banner := renderTestBanner(t, testPageBanner(page.User{Email: email, ProfileURL: "/", LogoutURL: "/logout"}))
	data := testPageBanner(page.User{Email: email, ProfileURL: "/", LogoutURL: "/logout"})
	footer := renderTestFooter(t, data)
	if !strings.HasSuffix(body[:strings.LastIndex(body, "</body>")], footer) {
		t.Fatal("missing expected appkit footer at body end")
	}
	written := pageWrittenMarkup(body, banner, footer)
	if written == body {
		t.Fatal("missing expected appkit banner at body start")
	}
	inside := pageContent(written, pageOne(t, written, "body"))
	seq := pageSequence(t, inside, "main")
	return pageContent(inside, seq[0])
}
func assertPageAlert(t *testing.T, text, kind, role, title, message string) {
	t.Helper()
	// R-4TZY-0Z0K
	div := pageOne(t, text, "div")
	pageAttr(t, div, "class", "alert")
	pageAttr(t, div, "data-kind", kind)
	pageAttr(t, div, "role", role)
	content := pageContent(text, div)
	seq := pageSequence(t, content, "strong", "p")
	if pageText(pageContent(content, seq[0])) != title || pageText(pageContent(content, seq[1])) != pageText(message) {
		t.Fatal("alert contents")
	}
}
func assertAuthPageFavicon(t *testing.T, body string) {
	t.Helper()
	// R-YN4H-5WV1: every emitted auth page links exactly one SVG favicon before body.
	bodyAt := pageOne(t, body, "body").start
	var icons []pageTag
	for _, tag := range pageElements(body, "link") {
		if tag.start < bodyAt {
			values := pageAttrs(tag)["rel"]
			if len(values) == 1 && values[0] == "icon" {
				icons = append(icons, tag)
			}
		}
	}
	if len(icons) != 1 {
		t.Fatalf("favicon links before body = %d, want 1", len(icons))
	}
	pageAttr(t, icons[0], "type", "image/svg+xml")
	pageAttr(t, icons[0], "href", "/_appkit/favicon.svg")
}

func assertAuthPagePreload(t *testing.T, body string) {
	t.Helper()
	// R-SAMM-J80A: every auth page has exactly one font preload before body.
	bodyAt := pageOne(t, body, "body").start
	var links []pageTag
	for _, tag := range pageElements(body, "link") {
		if tag.start < bodyAt {
			values := pageAttrs(tag)["rel"]
			if len(values) == 1 && values[0] == "preload" {
				links = append(links, tag)
			}
		}
	}
	if len(links) != 1 {
		t.Fatalf("preload links before body = %d, want 1", len(links))
	}
	pageAttr(t, links[0], "as", "font")
	pageAttr(t, links[0], "type", "font/woff2")
	pageAttr(t, links[0], "href", page.PreloadURL())
	if !slices.Contains(pageAttributeNames(links[0]), "crossorigin") {
		t.Fatal("font preload lacks crossorigin")
	}
}

func TestGeneratedAuthPagesShareVocabulary(t *testing.T) {
	// R-08U8-JUAH R-0A24-XM16 R-0BA1-BDRV R-0YG4-L0V2 R-ZB86-LWXV
	// R-0DPU-2X99 R-0EXQ-GOZY R-0G5M-UGQN R-0HDJ-88HC R-0ZO0-YSLR
	// R-0ILF-M081 R-0JTB-ZRYQ R-0L18-DJPF R-0M94-RBG4 R-0NH1-536T
	// R-0W0B-THDO R-0OOX-IUXI R-0PWT-WMO7 R-0R4Q-AEEW
	issuer := newSignInIssuer(t)
	st := openSignInStore(t)
	email := `odd <script src="https://evil/"> & src=x >@green.example`
	workspace := `green<&".example`
	s := signInServer(t, st, issuer, func() time.Time { return signInNow })
	s.cfg.WorkspaceDomain = workspace
	issuer.issueClaims("nonmember", map[string]any{"iss": "https://accounts.google.com", "sub": "nonmember", "aud": "client-id", "exp": 4102444800, "iat": 1700000000, "email": email, "email_verified": true, "hd": "other"})
	state, err := st.CreateLoginState("verifier", "")
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := st.UpsertUserOnLogin("issuer", "subject", email, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	pages := map[string]*httptest.ResponseRecorder{
		"sign-in":       serveSignIn(s, http.MethodGet, "/?return=%3Cfake%20src%3D%22x%22%3E", "auth.green.example", nil, ""),
		"cancelled":     serveSignIn(s, http.MethodGet, "/login/google/callback?error=access_denied", "auth.green.example", nil, ""),
		"nonmember":     serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code=nonmember", "auth.green.example", nil, ""),
		"profile-empty": serveSignIn(s, http.MethodGet, "/", "auth.green.example", cookie, "")}
	for name, values := range map[string]url.Values{"rejected": {"name": {`<fake src="x">`}, "expires": {"bogus"}}, "created": {"name": {`<fake src="x">`}, "expires": {"90d"}}} {
		req := tokenRequest("/tokens", session.ID, values)
		req.Host = "auth.green.example"
		req.Header.Set("Origin", "https://auth.green.example")
		w := httptest.NewRecorder()
		s.httpServer.Handler.ServeHTTP(w, req)
		pages[name] = w
	}
	pages["profile-populated"] = serveSignIn(s, http.MethodGet, "/", "auth.green.example", cookie, "")
	for name, w := range pages {
		t.Run(name, func(t *testing.T) {
			if w.Header().Get("Content-Type") != signInHTMLContentType {
				t.Fatalf("not HTML: %d %s", w.Code, w.Body.String())
			}
			assertAuthPage(t, w.Body.String())
			assertAuthPageFavicon(t, w.Body.String())
			assertAuthPagePreload(t, w.Body.String())
			if strings.HasPrefix(name, "profile") || name == "created" || name == "rejected" {
				assertChrome(t, w.Body.String(), email)
			}
			if strings.Contains(w.Body.String(), `<fake`) || strings.Contains(w.Body.String(), `<script src="https://evil/">`) {
				t.Fatal("external value injected markup")
			}
		})
	}
	profile := pages["profile-populated"].Body.String()
	if !strings.Contains(pageText(profile), pageText(html.EscapeString(email))) || !strings.Contains(pageText(profile), pageText(html.EscapeString(workspace))) {
		t.Fatal("escaped profile values changed")
	}
	input := pageOne(t, pages["rejected"].Body.String(), "input")
	pageAttr(t, input, "value", `<fake src="x">`)
}
func TestAuthPageScannerRejectsUnsafeVocabulary(t *testing.T) {
	base := `<!DOCTYPE html><html><head><title>auth</title><link rel="stylesheet" href="/_appkit/theme.css"><script src="/_appkit/feedback.js" defer></script><meta name="viewport" content="width=device-width, initial-scale=1"></head><body><main><a href="/">safe</a></main></body></html>`
	assertAuthPage(t, base)
	// Head hooks count only occurrences before body; later conforming hooks are allowed.
	laterHooks := strings.Replace(base, `<main><a href="/">safe</a></main>`, `<main><link rel="stylesheet" href="/_appkit/extra.css"><meta name="viewport" content="width=device-width, initial-scale=1"><a href="/">safe</a></main>`, 1)
	assertAuthPage(t, laterHooks)
	prematureEnd := strings.Replace(strings.Replace(base, "<body>", "</body><body>", 1), "</main></body>", "</main>", 1)
	if len(authPageErrors(prematureEnd)) == 0 {
		t.Fatal("body end before body start accepted")
	}
	quotedEnd := strings.Replace(strings.Replace(base, "<body>", `<body data-x="</body>">`, 1), "</main></body>", "</main>", 1)
	if len(authPageErrors(quotedEnd)) == 0 {
		t.Fatal("body end inside start-tag attribute accepted")
	}
	feedback := `<script src="/_appkit/feedback.js" defer></script>`
	for _, replacement := range []string{"", feedback + feedback, `<script src="/_appkit/feedback.js"></script>`, `<script src="/_appkit/feedback.js" Defer></script>`, `<script src="/_appkit/launcher.js" defer></script>`, `<script src="/_appkit/feedback.js" src="/_appkit/feedback.js" defer></script>`} {
		if len(authPageErrors(strings.Replace(base, feedback, replacement, 1))) == 0 {
			t.Fatalf("invalid feedback hook accepted: %s", replacement)
		}
	}
	misplaced := strings.Replace(base, feedback, "", 1)
	misplaced = strings.Replace(misplaced, "<main>", "<main>"+feedback, 1)
	if len(authPageErrors(misplaced)) == 0 {
		t.Fatal("feedback in body accepted")
	}
	cases := []string{strings.TrimPrefix(base, "<!DOCTYPE html>"), strings.Replace(base, "auth</title>", "Auth</title>", 1), strings.Replace(base, "<body>", "<body><TITLE>x</TITLE>", 1), strings.Replace(base, "<body>", "<body><body>", 1),
		`<meta http-equiv="refresh">`, `<a href="https://elsewhere">x</a>`, `<a href="//evil">x</a>`, `<a href="/\evil">x</a>`, `<a href="/x&#10;">x</a>`, `<form action="javascript:x"></form>`, `<button formaction="//evil">x</button>`,
		`<img src="/login/google">`, `<img src="/_appkit/%2E%2e/x">`, `<img src="/_appkit/.%2e/x">`, `<img src="/_appkit/%2e./x">`, `<img src="/_appkit/..\x">`, `<link href="/_appkit/x&#9;">`,
		`<img SRC="/_appkit/x">`, `<img src ="/_appkit/x">`, `<img src='/_appkit/x'>`, `<img/src="/_appkit/x">`, `<img alt="x"src="/_appkit/x">`, `<img src=/_appkit/x>`, `<img alt ="x">`, `<img alt="x" 'x>`,
		`<div onclick="x"></div>`, `<div OnLoad></div>`, `<div srcdoc="x"></div>`, `<div attributionsrc="x"></div>`, `<a ping="/_appkit/x">x</a>`, `<img srcset="/_appkit/a">`, `<link imagesrcset="/_appkit/a">`, `<div href="/">x</div>`, `<svg xlink:href="/_appkit/x"></svg>`,
		`<style>x</style>`, `<p style="x">x</p>`, `<textarea>x</textarea>`, `<xmp>x</xmp>`, `<iframe></iframe>`, `<noembed></noembed>`, `<noframes></noframes>`, `<noscript></noscript>`, `<plaintext>x`, `<template>x</template>`, `<math></math>`,
		`<script>x</script>`, `<!-- comment -->`, `<?instruction>`, `</main extra>`, `<svg></svg>`, `<input value="<svg>">`, `<button><svg class="ico" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" extra><path d="x"></path></svg>x</button>`, `<button><svg class="ico"><image src="/_appkit/x"></image></svg>x</button>`}
	for i, fragment := range cases {
		body := fragment
		if !strings.Contains(body, "<html>") {
			body = strings.Replace(base, `<main><a href="/">safe</a></main>`, fragment, 1)
		}
		if len(authPageErrors(body)) == 0 {
			t.Errorf("unsafe case %d accepted: %s", i, fragment)
		}
	}
	for _, script := range []string{`<script src="/_appkit/feedback.js" defer><!-- escaped --></script>`, `<script src="/_appkit/feedback.js" defer>x</SCRIPT>`, `<SCRIPT src="/_appkit/feedback.js" defer>x</script>`, `<script src="/_appkit/feedback.js" defer>x</script >`} {
		body := strings.Replace(base, feedback, script, 1)
		if len(authPageErrors(body)) == 0 {
			t.Errorf("bad placed script accepted %s", script)
		}
	}
	safe := strings.Replace(base, `<main><a href="/">safe</a></main>`, `<main><input value="<fake src='evil' onclick=x> &copyx"></main>`, 1)
	safe = strings.Replace(safe, `defer></script>`, `defer>var x = '<fake src="evil">';</script>`, 1)
	assertAuthPage(t, safe)
}

func TestSignInPagesFixVisibleText(t *testing.T) {
	// R-QC0I-YLM5 R-QEGB-Q53J R-EFN2-4PHY R-EGUY-IH8N R-QI40-VGBM
	workspace := `workspace<&".example`
	st := openSignInStore(t)
	s := newTestServer(t, Config{Banner: testPageBanner, Store: st, Now: func() time.Time { return signInNow }, WorkspaceDomain: workspace})
	for _, tc := range []struct{ host, returnURL, display string }{
		{"auth.sbx.ikigenba.dev:443", "", ""}, {"localhost:3001", "", ""}, {"auth.sbx.ikigenba.dev", "https://elsewhere.test/path", ""},
		{"auth.sbx.ikigenba.dev", "HTTPS://App.SBX.Ikigenba.Dev:0080/path?x=1", "App.SBX.Ikigenba.Dev:0080"}, {"localhost:3001", "http://LOCALHOST:3000/path", "LOCALHOST:3000"}, {"auth.green.example", "https://app.green.example:/", "app.green.example"},
		{`auth.sbx.ikigenba<&".dev:123`, "", ""}} {
		t.Run(tc.host+tc.returnURL, func(t *testing.T) {
			w := serveSignIn(s, http.MethodGet, "/?return="+percentEncode(tc.returnURL), tc.host, nil, "")
			assertHTMLStatus(t, w, 200)
			assertNoSetCookie(t, w)
			target := "/login/google"
			if tc.returnURL != "" {
				target += "?return=" + percentEncode(tc.returnURL)
			}
			sentence := "Access is limited to Google accounts in the " + workspace + " workspace."
			footer := "You're signing in at " + tc.host + ". One sign-in covers every service in this space."
			if tc.display != "" {
				footer = sentence
				sentence = "Sign in to continue to " + tc.display + "."
			}
			assertSignInCard(t, w.Body.String(), tc.host, "Continue with Google", target, footer)
			assertAuthPage(t, w.Body.String())
			want := "ikigenba Sign in to " + apexName(tc.host) + " " + sentence + " Continue with Google " + footer
			got := pageText(pageContent(w.Body.String(), pageOne(t, w.Body.String(), "body")))
			if got != want {
				t.Fatalf("visible = %q want %q", got, want)
			}
			assertEmptySignInTables(t, st)
		})
	}
}

func TestCancelledAndNonmemberCards(t *testing.T) {
	// R-U5UQ-751L R-QMZM-EJAE R-QO7I-SB13 R-QPFF-62RS R-EI2U-W8ZC R-EJAR-A0Q1
	host := "auth.sbx.ikigenba.dev:443"
	workspace := `green<&".example`
	email := `visitor<&"@other.test`
	issuer := newSignInIssuer(t)
	issuer.issueClaims("refused", map[string]any{"iss": "https://accounts.google.com", "sub": "visitor", "aud": "client-id", "exp": 4102444800, "iat": 1700000000, "email": email, "email_verified": true, "hd": "other"})
	st := openSignInStore(t)
	s := signInServer(t, st, issuer, func() time.Time { return signInNow })
	s.cfg.WorkspaceDomain = workspace
	cancelled := serveSignIn(s, http.MethodGet, "/login/google/callback?error=access_denied", host, nil, "")
	assertHTMLStatus(t, cancelled, 200)
	card := assertSignInCard(t, cancelled.Body.String(), host, "Continue with Google", "/login/google", "")
	assertPageAlert(t, card, "warn", "status", "Sign-in cancelled", "Google didn't grant access, so you weren't signed in. You can try again.")
	want := "ikigenba Sign in to ikigenba.dev Sign-in cancelled Google didn't grant access, so you weren't signed in. You can try again. Continue with Google"
	if pageText(pageContent(cancelled.Body.String(), pageOne(t, cancelled.Body.String(), "body"))) != want {
		t.Fatal("cancelled visible text")
	}
	state, err := st.CreateLoginState("verifier", "")
	if err != nil {
		t.Fatal(err)
	}
	refused := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code=refused", host, nil, "")
	assertHTMLStatus(t, refused, 403)
	footer := "Think you should have access? Ask your " + workspace + " workspace admin to add you."
	message := email + " isn't a verified account in the " + workspace + " workspace. Sign in with your @" + workspace + " account instead."
	card = assertSignInCard(t, refused.Body.String(), host, "Try another account", "/login/google", footer)
	assertPageAlert(t, card, "err", "alert", "Workspace membership required", message)
	want = "ikigenba Sign in to ikigenba.dev Workspace membership required " + message + " Try another account " + footer
	if pageText(pageContent(refused.Body.String(), pageOne(t, refused.Body.String(), "body"))) != want {
		t.Fatal("nonmember visible text")
	}
	for _, body := range []string{cancelled.Body.String(), refused.Body.String()} {
		assertAuthPage(t, body)
		heading := pageOne(t, body, "h1")
		alert := pageOne(t, body, "div")
		if heading.end+len(pageContent(body, heading)) >= alert.start {
			t.Fatal("alert precedes heading")
		}
	}
	assertNoSetCookie(t, cancelled)
	assertNoSetCookie(t, refused)
	assertEmptySignInTables(t, st)
}

func TestProfileFrameAndAccount(t *testing.T) {
	// R-ZZ31-HOCX R-10VX-CKCG R-123T-QC35 R-51R0-BHWD R-0USF-FPMZ
	st := openSignInStore(t)
	email := `member<&"@example.com`
	workspace := `workspace<&".test`
	user, _, err := st.UpsertUserOnLogin("issuer", "profile", email, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Config{Banner: testPageBanner, Store: st, Now: func() time.Time { return signInNow }, WorkspaceDomain: workspace})
	cookie := &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	before := formatSignInIdentity(t, st)
	w := serveSignIn(s, http.MethodGet, "/?return=https%3A%2F%2Felsewhere.test", "auth.sbx.ikigenba.dev", cookie, "")
	assertHTMLStatus(t, w, 200)
	assertNoSetCookie(t, w)
	assertAuthPage(t, w.Body.String())
	main := assertChrome(t, w.Body.String(), email)
	seq := pageSequence(t, main, "h1", "p", "section", "section", "section", "section")
	if pageText(pageContent(main, seq[0])) != "Your account" || pageText(pageContent(main, seq[1])) != "You're signed in to ikigenba.dev." {
		t.Fatal("profile headings")
	}
	for i, title := range []string{"Account", "API tokens", "Create a token", "MCP clients"} {
		card := strings.TrimLeft(pageContent(main, seq[i+2]), " \t\r\n\f")
		head := pageTags(card)[0]
		if head.start != 0 || head.name != "header" {
			t.Fatal("card not headed")
		}
		h2 := pageOne(t, pageContent(card, head), "h2")
		if pageText(pageContent(pageContent(card, head), h2)) != title {
			t.Fatal("card order")
		}
	}
	pageAttr(t, seq[2], "class", "card")
	account := pageContent(main, seq[2])
	dl := pageOne(t, account, "dl")
	pageAttr(t, dl, "class", "kv")
	fields := pageContent(account, dl)
	items := pageSequence(t, fields, "dt", "dd", "dt", "dd", "dt", "dd")
	for i, want := range []string{"Email", email, "Workspace", workspace, "Signed in via", "Google"} {
		if pageText(pageContent(fields, items[i])) != want {
			t.Fatalf("account field %d", i)
		}
	}
	if before != formatSignInIdentity(t, st) {
		t.Fatal("profile changed identity/session")
	}
	tokens, err := st.ListTokens(user.ID)
	if err != nil || len(tokens) != 0 {
		t.Fatalf("profile changed tokens: %#v %v", tokens, err)
	}
}

func TestReturnQueryDecodeAndByteEncoding(t *testing.T) {
	// R-PNB9-4VJK R-PPR1-WF0Y R-ED79-D60K
	issuer := newSignInIssuer(t)
	for _, tc := range []struct{ query, value, encoded string }{
		{"", "", ""}, {"return", "", ""}, {"return=&return=later", "", ""}, {"return=first&return=second", "first", "first"}, {"x=1&ret%75rn=a+b%20c", "a b c", "a%20b%20c"}, {"return=%FF", "\xff", "%FF"}, {"return=%C3%A9", "é", "%C3%A9"},
		{"return=%&return=good", "good", "good"}, {"return=%G0&return=good", "good", "good"}, {"ret%urn=x&return=good", "good", "good"}, {"return=a;b&return=good", "good", "good"}, {"return=a%3Bb", "a;b", "a%3Bb"}, {"return=%00%7F%5C%3F%26%3D%2B%2F%3A", "\x00\x7f\\?&=+/:", "%00%7F%5C%3F%26%3D%2B%2F%3A"}, {"return=AZaz09-._~", "AZaz09-._~", "AZaz09-._~"}} {
		t.Run(tc.query, func(t *testing.T) {
			if returnQuery(tc.query) != tc.value || percentEncode(tc.value) != tc.encoded {
				t.Fatalf("decode/encode %q %q", returnQuery(tc.query), percentEncode(tc.value))
			}
			st := openSignInStore(t)
			s := signInServer(t, st, issuer, func() time.Time { return signInNow })
			page := serveSignIn(s, http.MethodGet, "/?"+tc.query, "auth.green.example", nil, "")
			target := "/login/google"
			if tc.value != "" {
				target += "?return=" + tc.encoded
			}
			pageAttr(t, pageOne(t, page.Body.String(), "a"), "href", target)
			assertEmptySignInTables(t, st)
			start := serveSignIn(s, http.MethodGet, "/login/google?"+tc.query, "auth.green.example", nil, "")
			if start.Code != 302 {
				t.Fatalf("start %d %s", start.Code, start.Body.String())
			}
			location, err := url.Parse(start.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state, err := st.ConsumeLoginState(location.Query().Get("state"))
			if err != nil || state.ReturnURL != tc.value {
				t.Fatalf("stored return = %q %v", state.ReturnURL, err)
			}
		})
	}
}

func TestCallbackUsesTextPolicyForCarriedReturn(t *testing.T) {
	// R-N3TS-N2H8
	issuer := newSignInIssuer(t)
	issuer.issue("member", "return-subject", "member@green.example")
	for _, tc := range []struct{ value, want string }{
		{"HtTp://APP.GREEN.EXAMPLE:0080/a", "HtTp://APP.GREEN.EXAMPLE:0080/a"}, {"https://green.example:/a", "https://green.example:/a"}, {"https://green.example/a?x=%3Cscript%3E", "https://green.example/a?x=%3Cscript%3E"},
		{"https://green.example\\@evil.test/", "/"}, {"https:///green.example/", "/"}, {"https://green.example@evil.test/", "/"}, {"https://%67reen.example/", "/"}, {"https://green.example/\n", "/"}, {"https://green.example:abc/", "/"}, {"https://green.example.evil/", "/"}, {"", "/"},
	} {
		st := openSignInStore(t)
		s := signInServer(t, st, issuer, func() time.Time { return signInNow })
		state, err := st.CreateLoginState("verifier", tc.value)
		if err != nil {
			t.Fatal(err)
		}
		w := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code=member", "auth.green.example", nil, "")
		if w.Code != 302 || w.Header().Get("Location") != tc.want {
			t.Errorf("return %q -> %d %q want %q", tc.value, w.Code, w.Header().Get("Location"), tc.want)
		}
	}
}

func TestAuthPagePreservesExternalBytes(t *testing.T) {
	// R-0ZO0-YSLR: external text preserves bytes, while contributing no markup.
	raw := "\x00<fake src=\"x\">&\xff\x00"
	st := openSignInStore(t)
	s := signInServer(t, st, newSignInIssuer(t), func() time.Time { return signInNow })
	workspace, email := raw+".example", raw+"@green.example"
	s.cfg.WorkspaceDomain = workspace
	anonymous := serveSignIn(s, http.MethodGet, "/", "auth.green.example", nil, "")
	assertAuthPage(t, anonymous.Body.String())
	paragraph := pageOne(t, anonymous.Body.String(), "p")
	if got := pageText(pageContent(anonymous.Body.String(), paragraph)); got != "Access is limited to Google accounts in the "+workspace+" workspace." {
		t.Fatalf("workspace text changed bytes: %q", got)
	}
	user, _, err := st.UpsertUserOnLogin("issuer", "byte-value-subject", email, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateToken(user.ID, raw, "90d", signInNow); err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	profile := serveSignIn(s, http.MethodGet, "/", "auth.green.example", cookie, "")
	assertAuthPage(t, profile.Body.String())
	assertChrome(t, profile.Body.String(), email)
	fields := pageElements(profile.Body.String(), "dd")
	for i, want := range []string{email, workspace} {
		if got := pageText(pageContent(profile.Body.String(), fields[i])); got != want {
			t.Fatalf("account value changed bytes: %q want %q", got, want)
		}
	}
	tokenRead(t, tokenElements(profile.Body.String(), "td")[0], raw)
	req := tokenRequest("/tokens", session.ID, url.Values{"name": {raw}, "expires": {"bad"}})
	req.Host = "auth.green.example"
	req.Header.Set("Origin", "https://auth.green.example")
	rejected := httptest.NewRecorder()
	s.ServeHTTP(rejected, req)
	assertAuthPage(t, rejected.Body.String())
	assertChrome(t, rejected.Body.String(), email)
	pageAttr(t, pageOne(t, rejected.Body.String(), "input"), "value", raw)
}

func pageASCIILower(value string) string {
	bytes := []byte(value)
	for i, c := range bytes {
		if c >= 'A' && c <= 'Z' {
			bytes[i] = c + ('a' - 'A')
		}
	}
	return string(bytes)
}

func TestPagesShowRequestApex(t *testing.T) {
	// R-3Q46-Q9HS: rendered apex text follows the request Host.
	for host, want := range map[string]string{
		"auth.sbx.ikigenba.dev": "ikigenba.dev", "localhost:3001": "localhost",
		"a.b.c:001": "b.c", "a.b:port": "a.b:port", "name": "name", "name:": "name:",
		"auth.A.B:443": "A.B", "auth.a.b.": "b.",
	} {
		t.Run(host, func(t *testing.T) {
			st := openSignInStore(t)
			s := newTestServer(t, Config{Banner: testPageBanner, Store: st, Now: func() time.Time { return signInNow }})
			for _, target := range []string{"/", "/login/google/callback?error=access_denied"} {
				w := serveSignIn(s, http.MethodGet, target, host, nil, "")
				body := w.Body.String()
				if got := pageText(pageContent(body, pageOne(t, body, "h1"))); got != "Sign in to "+want {
					t.Fatalf("%s heading = %q, want %q", target, got, "Sign in to "+want)
				}
			}
			user, _, err := st.UpsertUserOnLogin("issuer", "subject", "member@green.example", signInNow)
			if err != nil {
				t.Fatal(err)
			}
			session, err := st.CreateSession(user.ID, signInNow)
			if err != nil {
				t.Fatal(err)
			}
			w := serveSignIn(s, http.MethodGet, "/", host, &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}, "")
			if !strings.Contains(pageText(w.Body.String()), "You're signed in to "+want+".") {
				t.Fatalf("profile does not show request apex %q: %s", want, w.Body.String())
			}
		})
	}
}
