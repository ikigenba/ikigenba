package server

import (
	"fmt"
	"html"
	"strings"
	"testing"
)

// This reader is test-only. It reads HTML token syntax, not page hooks: names
// below are HTML vocabulary. Ambiguous constructs are rejected rather than
// silently interpreting a document differently from a browser. Its scope is
// the well-nested HTML and SVG emitted by these templates, including comments,
// quoted/unquoted attributes, character references and raw/RCDATA elements.
type htmlFormTarget struct{ method, target string }
type htmlPageReferences struct {
	links, resources, bases, automatic []string
	forms                              []htmlFormTarget
	outsideLinks                       []string
	outsideForms                       []htmlFormTarget
}
type htmlToken struct {
	name                 string
	attrs                map[string]string
	start, end           int
	closing, selfClosing bool
}

func htmlSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func readHTMLTokens(document string) ([]htmlToken, error) {
	var tokens []htmlToken
	for pos := 0; pos < len(document); {
		offset := strings.IndexByte(document[pos:], '<')
		if offset < 0 {
			break
		}
		pos += offset
		start := pos
		if strings.HasPrefix(document[pos:], "<!--") {
			end := strings.Index(document[pos+4:], "-->")
			if end < 0 {
				return nil, fmt.Errorf("unterminated HTML comment")
			}
			pos += 4 + end + 3
			continue
		}
		if strings.HasPrefix(strings.ToLower(document[pos:]), "<!doctype") {
			end := strings.IndexByte(document[pos:], '>')
			if end < 0 {
				return nil, fmt.Errorf("unterminated doctype")
			}
			pos += end + 1
			continue
		}
		pos++
		tok := htmlToken{attrs: map[string]string{}, start: start}
		if pos < len(document) && document[pos] == '/' {
			tok.closing = true
			pos++
		}
		nameStart := pos
		for pos < len(document) && !htmlSpace(document[pos]) && document[pos] != '/' && document[pos] != '>' {
			pos++
		}
		tok.name = strings.ToLower(document[nameStart:pos])
		if tok.name == "" || tok.name[0] < 'a' || tok.name[0] > 'z' {
			return nil, fmt.Errorf("unsupported HTML token at %d", start)
		}
		for {
			for pos < len(document) && htmlSpace(document[pos]) {
				pos++
			}
			if pos >= len(document) {
				return nil, fmt.Errorf("unterminated %s", tok.name)
			}
			if document[pos] == '>' {
				pos++
				break
			}
			if document[pos] == '/' && pos+1 < len(document) && document[pos+1] == '>' {
				tok.selfClosing = true
				pos += 2
				break
			}
			attrStart := pos
			for pos < len(document) && !htmlSpace(document[pos]) && !strings.ContainsRune("=/>\"'<", rune(document[pos])) {
				pos++
			}
			if pos == attrStart {
				return nil, fmt.Errorf("unsupported attribute syntax at %d", pos)
			}
			name := strings.ToLower(document[attrStart:pos])
			for pos < len(document) && htmlSpace(document[pos]) {
				pos++
			}
			value := ""
			if pos < len(document) && document[pos] == '=' {
				pos++
				for pos < len(document) && htmlSpace(document[pos]) {
					pos++
				}
				if pos >= len(document) {
					return nil, fmt.Errorf("unterminated attribute")
				}
				quote := document[pos]
				if quote == '\'' || quote == '"' {
					pos++
					valueStart := pos
					for pos < len(document) && document[pos] != quote {
						pos++
					}
					if pos >= len(document) {
						return nil, fmt.Errorf("unterminated quoted attribute")
					}
					value = document[valueStart:pos]
					pos++
				} else {
					valueStart := pos
					for pos < len(document) && !htmlSpace(document[pos]) && document[pos] != '>' {
						pos++
					}
					value = document[valueStart:pos]
				}
			}
			// The HTML tokenizer retains the first duplicate attribute.
			if _, exists := tok.attrs[name]; !exists {
				tok.attrs[name] = html.UnescapeString(strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n"))
			}
		}
		tok.end = pos
		tokens = append(tokens, tok)
		if !tok.closing && (tok.name == "script" || tok.name == "style" || tok.name == "textarea" || tok.name == "title") {
			lower := strings.ToLower(document[pos:])
			marker := "</" + tok.name
			end := -1
			for search := 0; search < len(lower); {
				found := strings.Index(lower[search:], marker)
				if found < 0 {
					break
				}
				candidate := search + found
				after := candidate + len(marker)
				if after < len(lower) && (htmlSpace(lower[after]) || lower[after] == '>' || lower[after] == '/') {
					end = candidate
					break
				}
				search = after
			}
			if end < 0 {
				return nil, fmt.Errorf("unterminated raw text %s", tok.name)
			}
			// CSS and script content is handled separately from HTML tokens.
			tok.attrs["raw-content"] = document[pos : pos+end]
			tokens[len(tokens)-1] = tok
			pos += end
		}
	}
	return tokens, nil
}

func readPageReferences(t *testing.T, document, banner string) htmlPageReferences {
	t.Helper()
	tokens, err := readHTMLTokens(document)
	if err != nil {
		t.Fatal(err)
	}
	bannerStart, bannerEnd := -1, -1
	if banner != "" {
		bannerStart = strings.Index(document, banner)
		if bannerStart < 0 {
			t.Fatal("returned banner missing")
		}
		bannerEnd = bannerStart + len(banner)
	}
	var refs htmlPageReferences
	var stack []string
	currentFormMethod := "GET"
	currentFormTarget := ""
	inForm := false
	for _, tok := range tokens {
		if tok.closing {
			if tok.name == "form" {
				currentFormMethod = "GET"
				inForm = false
			}
			if len(stack) == 0 || stack[len(stack)-1] != tok.name {
				t.Fatalf("reader needs well-nested HTML at %s", tok.name)
			}
			stack = stack[:len(stack)-1]
			continue
		}
		a := tok.attrs
		inSVG := false
		for _, open := range stack {
			if open == "svg" {
				inSVG = true
			}
		}
		if inSVG {
			switch tok.name {
			case "form", "base", "meta", "iframe", "object", "embed", "input", "button", "link", "source", "audio", "video", "table", "body", "html":
				t.Fatalf("HTML reader needs foreign namespace support for %s", tok.name)
			}
		} else if tok.name == "image" {
			tok.name = "img"
		}
		inBanner := tok.start >= bannerStart && tok.end <= bannerEnd && bannerStart >= 0
		addLink := func(value string) {
			refs.links = append(refs.links, value)
			if !inBanner {
				refs.outsideLinks = append(refs.outsideLinks, value)
			}
		}
		addForm := func(form htmlFormTarget) {
			refs.forms = append(refs.forms, form)
			if !inBanner {
				refs.outsideForms = append(refs.outsideForms, form)
			}
		}
		switch tok.name {
		case "a", "area":
			if value, ok := a["href"]; ok {
				addLink(value)
			} else if inSVG {
				if value, ok := a["xlink:href"]; ok {
					addLink(value)
				}
			}
		case "form":
			for _, open := range stack {
				if open == "form" {
					t.Fatal("nested forms require HTML tree repair")
				}
			}
			method := strings.ToUpper(a["method"])
			if method != "POST" && method != "DIALOG" {
				method = "GET"
			}
			currentFormMethod = method
			currentFormTarget = a["action"]
			inForm = true
			addForm(htmlFormTarget{method, a["action"]})
		case "base":
			if value, ok := a["href"]; ok {
				refs.bases = append(refs.bases, value)
			}
		case "meta":
			if strings.EqualFold(strings.TrimSpace(a["http-equiv"]), "refresh") {
				refs.automatic = append(refs.automatic, a["content"])
			}
		case "link":
			// All link relations that cause loads; harmless relations are omitted.
			for _, rel := range strings.Fields(strings.ToLower(a["rel"])) {
				switch rel {
				case "stylesheet", "icon", "preload", "modulepreload", "prefetch", "dns-prefetch", "preconnect", "manifest":
					if value, ok := a["href"]; ok {
						refs.resources = append(refs.resources, value)
					}
				}
			}
		case "image", "use", "feimage":
			for _, attr := range []string{"href", "xlink:href"} {
				if value, ok := a[attr]; ok && !strings.HasPrefix(value, "#") {
					refs.resources = append(refs.resources, value)
				}
			}
		case "template", "noscript", "plaintext", "xmp", "foreignobject", "animate", "animatemotion", "animatetransform", "set":
			// These need a separate tree-building mode or nested document parser.
			// Fail the reader explicitly rather than silently omit their references.
			t.Fatalf("HTML reader does not yet support %s", tok.name)
		}
		// Select resource attributes by their HTML/SVG semantics, irrespective of
		// which element a particular auth template happens to use.
		resourceAttrs := []string{}
		switch tok.name {
		case "audio", "embed", "frame", "iframe", "img", "script", "source", "track", "video":
			resourceAttrs = append(resourceAttrs, "src")
		case "input":
			if strings.EqualFold(a["type"], "image") {
				resourceAttrs = append(resourceAttrs, "src")
			}
		case "object":
			resourceAttrs = append(resourceAttrs, "data", "codebase")
		case "html":
			resourceAttrs = append(resourceAttrs, "manifest")
		case "body", "table", "td", "th":
			resourceAttrs = append(resourceAttrs, "background")
		}
		if inSVG {
			if tok.name == "script" {
				resourceAttrs = append(resourceAttrs, "href", "xlink:href")
			}
			for _, attr := range []string{"fill", "stroke", "filter", "clip-path", "mask", "cursor", "marker", "marker-start", "marker-mid", "marker-end"} {
				if value, ok := a[attr]; ok {
					refs.resources = append(refs.resources, htmlCSSURLs(t, value)...)
				}
			}
		}
		if tok.name == "video" {
			resourceAttrs = append(resourceAttrs, "poster")
		}
		for _, attr := range resourceAttrs {
			if value, ok := a[attr]; ok {
				refs.resources = append(refs.resources, value)
			}
		}
		if tok.name == "img" || tok.name == "source" {
			if value, ok := a["srcset"]; ok {
				refs.resources = append(refs.resources, htmlSrcsetURLs(value)...)
			}
		}
		if tok.name == "link" {
			if value, ok := a["imagesrcset"]; ok {
				refs.resources = append(refs.resources, htmlSrcsetURLs(value)...)
			}
		}
		if value, ok := a["srcdoc"]; ok && tok.name == "iframe" {
			nested := readPageReferences(t, value, "")
			refs.resources = append(refs.resources, nested.resources...)
			refs.bases = append(refs.bases, nested.bases...)
			refs.automatic = append(refs.automatic, nested.automatic...)
			refs.links = append(refs.links, nested.links...)
			refs.outsideLinks = append(refs.outsideLinks, nested.links...)
			refs.forms = append(refs.forms, nested.forms...)
			refs.outsideForms = append(refs.outsideForms, nested.forms...)
		}
		if value, ok := a["ping"]; ok && (tok.name == "a" || tok.name == "area") {
			for _, target := range strings.Fields(value) {
				addLink(target)
			}
		}
		if _, hasOwner := a["form"]; hasOwner && (tok.name == "button" || tok.name == "input") {
			t.Fatal("HTML reader needs external form association support")
		}
		buttonType := strings.ToLower(a["type"])
		submit := tok.name == "button" && buttonType != "button" && buttonType != "reset" || tok.name == "input" && (buttonType == "submit" || buttonType == "image")
		if inForm && submit {
			value, overrideTarget := a["formaction"]
			if !overrideTarget {
				value = currentFormTarget
			}
			method := currentFormMethod
			override, overrideMethod := a["formmethod"]
			if overrideMethod {
				method = strings.ToUpper(override)
			}
			if method != "POST" && method != "DIALOG" {
				method = "GET"
			}
			if overrideTarget || overrideMethod {
				addForm(htmlFormTarget{method, value})
			}
		}
		if value, ok := a["style"]; ok {
			refs.resources = append(refs.resources, htmlCSSURLs(t, value)...)
		}
		if tok.name == "style" {
			refs.resources = append(refs.resources, htmlCSSURLs(t, a["raw-content"])...)
		}
		if tok.name == "script" && strings.TrimSpace(a["raw-content"]) != "" {
			t.Fatal("HTML reader cannot decide inline script resource references")
		}
		for attr := range a {
			if strings.HasPrefix(attr, "on") {
				t.Fatal("HTML reader cannot decide event script resource references")
			}
		}
		void := false
		switch tok.name {
		case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
			void = true
		}
		if !void && (!tok.selfClosing || !inSVG && tok.name != "svg") {
			stack = append(stack, tok.name)
		}
	}
	if len(stack) != 0 {
		t.Fatalf("HTML reader found unclosed elements %v", stack)
	}
	return refs
}

func htmlSrcsetURLs(value string) []string {
	var urls []string
	for value != "" {
		value = strings.TrimLeft(value, " \t\r\n\f,")
		if value == "" {
			break
		}
		end := 0
		for end < len(value) && !htmlSpace(value[end]) {
			end++
		}
		target := strings.TrimRight(value[:end], ",")
		urls = append(urls, target)
		if end > 0 && value[end-1] == ',' {
			value = value[end:]
			continue
		}
		value = value[end:]
		nesting := 0
		end = 0
		for end < len(value) {
			if value[end] == '(' {
				nesting++
			}
			if value[end] == ')' {
				nesting--
			}
			if value[end] == ',' && nesting == 0 {
				end++
				break
			}
			end++
		}
		value = value[end:]
	}
	return urls
}

func htmlCSSURLs(t *testing.T, value string) []string {
	t.Helper()
	// Backslash escapes need a CSS escape tokenizer. Report unsupported syntax
	// explicitly rather than miss a disguised function name or URL.
	if strings.Contains(value, "\\") {
		t.Fatal("HTML reader needs CSS escape support")
	}
	var urls []string
	var functions []string
	importTarget := false
	for pos := 0; pos < len(value); {
		if strings.HasPrefix(value[pos:], "/*") {
			end := strings.Index(value[pos+2:], "*/")
			if end < 0 {
				t.Fatal("unterminated CSS comment")
			}
			pos += end + 4
			continue
		}
		if htmlSpace(value[pos]) {
			pos++
			continue
		}
		if value[pos] == '\'' || value[pos] == '"' {
			quote := value[pos]
			pos++
			start := pos
			for pos < len(value) && value[pos] != quote {
				pos++
			}
			if pos == len(value) {
				t.Fatal("unterminated CSS string")
			}
			target := value[start:pos]
			pos++
			function := ""
			if len(functions) > 0 {
				function = functions[len(functions)-1]
			}
			if importTarget || function == "image-set" || function == "-webkit-image-set" || function == "image" || function == "src" {
				urls = append(urls, target)
			}
			importTarget = false
			continue
		}
		if value[pos] == ')' {
			if len(functions) > 0 {
				functions = functions[:len(functions)-1]
			}
			pos++
			continue
		}
		if value[pos] == '@' {
			pos++
			start := pos
			for pos < len(value) && cssIdentByte(value[pos]) {
				pos++
			}
			importTarget = strings.EqualFold(value[start:pos], "import")
			continue
		}
		if cssIdentByte(value[pos]) {
			start := pos
			for pos < len(value) && cssIdentByte(value[pos]) {
				pos++
			}
			name := strings.ToLower(value[start:pos])
			if name == "var" && pos < len(value) && value[pos] == '(' {
				t.Fatal("HTML reader needs CSS custom-property substitution support")
			}
			if pos < len(value) && value[pos] == '(' {
				pos++
				if name == "url" {
					for pos < len(value) && htmlSpace(value[pos]) {
						pos++
					}
					target := ""
					if pos < len(value) && (value[pos] == '\'' || value[pos] == '"') {
						quote := value[pos]
						pos++
						start = pos
						for pos < len(value) && value[pos] != quote {
							pos++
						}
						if pos == len(value) {
							t.Fatal("unterminated CSS URL string")
						}
						target = value[start:pos]
						pos++
					} else {
						start = pos
						for pos < len(value) && value[pos] != ')' {
							pos++
						}
						target = strings.TrimSpace(value[start:pos])
					}
					for pos < len(value) && htmlSpace(value[pos]) {
						pos++
					}
					if pos == len(value) || value[pos] != ')' {
						t.Fatal("unterminated CSS URL")
					}
					pos++
					if !strings.HasPrefix(target, "#") {
						urls = append(urls, target)
					}
					importTarget = false
				} else {
					functions = append(functions, name)
				}
			}
			continue
		}
		pos++
	}
	return urls
}

func cssIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}
