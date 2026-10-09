package callback_test

import (
	"fmt"
	"html"
	"strings"
	"testing"
)

// This reader only retains the parts of HTML tokens relevant to references.
// Attribute values use HTML's character references; duplicate attributes keep
// their first value. Comments and raw text do not produce element tokens.
type htmlElement struct {
	name, namespace string
	css             string
	integration     bool
}

func readSelfContainedHTML(document string) error {
	document = strings.ReplaceAll(document, "\x00", "\ufffd")
	var stack []htmlElement
	appendStyleText := func(text string) error {
		for index := range stack {
			if stack[index].name == "style" {
				stack[index].css += text
				if err := checkCSS(stack[index].css); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for len(document) > 0 {
		start := strings.IndexByte(document, '<')
		text := document
		if start >= 0 {
			text = document[:start]
		}
		if err := appendStyleText(html.UnescapeString(text)); err != nil {
			return err
		}

		if start < 0 {
			return nil
		}
		document = document[start+1:]
		if strings.HasPrefix(document, "!--") {
			document = readHTMLComment(document[3:])
			continue
		}
		foreign := len(stack) > 0 && stack[len(stack)-1].namespace != "html"
		if foreign && strings.HasPrefix(document, "![CDATA[") {
			text := document[8:]
			end := strings.Index(text, "]]>")
			if end < 0 {
				end = len(text)
			}
			if err := appendStyleText(text[:end]); err != nil {
				return err
			}
			document = text[end:]
			if len(document) > 0 {
				document = document[3:]
			}
			continue
		}
		if len(document) == 0 {
			return nil
		}
		if document[0] == '/' {
			end := 1
			for end < len(document) && !htmlSpace(document[end]) && document[end] != '/' && document[end] != '>' {
				end++
			}
			name := asciiLower(document[1:end])
			for index := len(stack) - 1; index >= 0; index-- {
				if stack[index].name == name {
					stack = stack[:index]
					break
				}
			}
		}
		if document[0] == '!' || document[0] == '?' || document[0] == '/' {
			end := strings.IndexByte(document, '>')
			if end < 0 {
				return nil
			}
			document = document[end+1:]
			continue
		}
		if !asciiLetter(document[0]) {
			continue
		}
		end := 0
		for end < len(document) && !htmlSpace(document[end]) && document[end] != '/' && document[end] != '>' {
			end++
		}
		name := asciiLower(document[:end])
		attributes, rest, selfClosing := readHTMLAttributes(document[end:])
		document = rest
		namespace := "html"
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			if !parent.integration {
				namespace = parent.namespace
			}
			if parent.namespace == "math" && parent.name == "annotation-xml" && name == "svg" {
				namespace = "html"
			}
			if parent.namespace == "math" && parent.integration && (name == "mglyph" || name == "malignmark") {
				namespace = "math"
			}
		}
		if namespace != "html" && foreignBreakout(name, attributes) {
			for len(stack) > 0 && stack[len(stack)-1].namespace != "html" && !stack[len(stack)-1].integration {
				stack = stack[:len(stack)-1]
			}
			namespace = "html"
		}
		if namespace == "html" && (name == "svg" || name == "math") {
			namespace = name
		}
		if namespace == "svg" && (name == "set" || name == "animate") {
			if err := checkSVGAnimation(name, attributes); err != nil {
				return err
			}
		}
		if name == "script" {
			return fmt.Errorf("script element is forbidden")
		}
		if name == "param" && asciiLower(attributes["valuetype"]) == "ref" {
			if err := checkURL(attributes["value"]); err != nil {
				return err
			}
		}
		if name == "meta" && asciiLower(attributes["http-equiv"]) == "refresh" {
			return fmt.Errorf("refresh is forbidden")
		}
		for attribute, value := range attributes {
			if strings.HasPrefix(attribute, "on") {
				return fmt.Errorf("event handler %q is forbidden", attribute)
			}
			if attribute == "style" || cssPresentation(attribute) {
				if err := checkCSS(value); err != nil {
					return err
				}
			}
			if attribute == "srcdoc" {
				if err := readSelfContainedHTML(value); err != nil {
					return err
				}
			}
			if urlAttribute(attribute) {
				if err := checkReferences(value, attribute); err != nil {
					return err
				}
			}
		}
		if namespace == "html" {
			switch name {
			case "plaintext":
				return nil
			case "style", "title", "textarea", "xmp", "iframe", "noembed", "noframes":
				text, after := readRawHTML(document, name)
				if name == "style" {
					if err := checkCSS(text); err != nil {
						return err
					}
				}
				document = after
				continue
			}
		}
		if (!selfClosing || namespace == "html") && (namespace != "html" || !htmlVoid(name)) {
			stack = append(stack, htmlElement{name: name, namespace: namespace, integration: integrationPoint(namespace, name, attributes)})
		}
	}
	return nil
}

// SVG animation values carry the type of the attribute they target rather
// than a type fixed by their own attribute name.
func checkSVGAnimation(element string, attributes map[string]string) error {
	target := asciiLower(attributes["attributename"])
	for _, attribute := range []string{"to", "from", "by", "values"} {
		if element == "set" && attribute != "to" {
			continue
		}
		value, exists := attributes[attribute]
		if !exists {
			continue
		}
		values := []string{value}
		if attribute == "values" {
			values = strings.Split(value, ";")
		}
		for _, item := range values {
			if urlAttribute(target) {
				if err := checkReferences(strings.TrimSpace(item), target); err != nil {
					return fmt.Errorf("SVG animation %s: %w", attribute, err)
				}
			}
			if target == "style" || cssPresentation(target) {
				if err := checkCSS(item); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func integrationPoint(namespace, name string, attributes map[string]string) bool {
	if namespace == "svg" {
		return name == "foreignobject" || name == "desc" || name == "title"
	}
	if namespace == "math" {
		switch name {
		case "mi", "mo", "mn", "ms", "mtext":
			return true
		}
		encoding := asciiLower(attributes["encoding"])
		return name == "annotation-xml" && (encoding == "text/html" || encoding == "application/xhtml+xml")
	}
	return false
}

func htmlVoid(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

func foreignBreakout(name string, attributes map[string]string) bool {
	switch name {
	case "b", "big", "blockquote", "body", "br", "center", "code", "dd", "div", "dl", "dt", "em", "embed", "h1", "h2", "h3", "h4", "h5", "h6", "head", "hr", "i", "img", "li", "listing", "menu", "meta", "nobr", "ol", "p", "pre", "ruby", "s", "small", "span", "strong", "strike", "sub", "sup", "table", "tt", "u", "ul", "var":
		return true
	case "font":
		for _, attribute := range []string{"color", "face", "size"} {
			if _, present := attributes[attribute]; present {
				return true
			}
		}
	}
	return false
}

// Comment start, end and end-bang states have distinct closing rules.
func readHTMLComment(input string) string {
	if strings.HasPrefix(input, ">") {
		return input[1:]
	}
	if strings.HasPrefix(input, "->") {
		return input[2:]
	}
	for index := 0; index < len(input); index++ {
		if strings.HasPrefix(input[index:], "-->") {
			return input[index+3:]
		}
		if strings.HasPrefix(input[index:], "--!>") {
			return input[index+4:]
		}
	}
	return ""
}

func readHTMLAttributes(input string) (map[string]string, string, bool) {
	attributes := make(map[string]string)
	for len(input) > 0 {
		input = strings.TrimLeft(input, "\t\n\f\r ")
		if len(input) == 0 {
			break
		}
		if input[0] == '>' {
			return attributes, input[1:], false
		}
		if input[0] == '/' {
			if strings.HasPrefix(input, "/>") {
				return attributes, input[2:], true
			}
			input = input[1:]
			continue
		}
		end := 0
		for end < len(input) && !htmlSpace(input[end]) && input[end] != '=' && input[end] != '/' && input[end] != '>' {
			end++
		}
		if end == 0 {
			end = 1
		}
		name := asciiLower(input[:end])
		input = strings.TrimLeft(input[end:], "\t\n\f\r ")
		value := ""
		if len(input) > 0 && input[0] == '=' {
			input = strings.TrimLeft(input[1:], "\t\n\f\r ")
			if len(input) > 0 && (input[0] == '\'' || input[0] == '"') {
				quote := input[0]
				input = input[1:]
				end = strings.IndexByte(input, quote)
				if end < 0 {
					end = len(input)
				}
				value = input[:end]
				input = input[end:]
				if len(input) > 0 {
					input = input[1:]
				}
			} else {
				end = 0
				for end < len(input) && !htmlSpace(input[end]) && input[end] != '>' {
					end++
				}
				value = input[:end]
				input = input[end:]
			}
		}
		if _, exists := attributes[name]; !exists {
			attributes[name] = html.UnescapeString(value)
		}
	}
	return attributes, "", false
}

func readRawHTML(input, name string) (string, string) {
	lower := asciiLower(input)
	marker := "</" + name
	offset := 0
	for offset < len(input) {
		next := strings.Index(lower[offset:], marker)
		if next < 0 {
			break
		}
		start := offset + next
		after := start + len(marker)
		if after == len(input) || htmlSpace(input[after]) || input[after] == '/' || input[after] == '>' {
			return input[:start], input[start:]
		}
		offset = after
	}
	return input, ""
}

func urlAttribute(name string) bool {
	// HTML (including obsolete features), microdata, SVG and MathML URL
	// attributes. Foreign names are retained with their namespace prefix.
	switch name {
	case "href", "xlink:href", "src", "srcset", "imagesrcset", "action", "formaction", "poster", "data", "cite", "background", "longdesc", "usemap", "profile", "manifest", "code", "codebase", "archive", "classid", "icon", "ping", "lowsrc", "dynsrc", "itemid", "itemtype", "xml:base", "definitionurl", "altimg", "object", "attributionsrc", "datasrc":
		return true
	}
	return false
}

func cssPresentation(name string) bool {
	switch name {
	case "fill", "stroke", "filter", "clip-path", "mask", "marker", "marker-start", "marker-mid", "marker-end", "cursor", "color-profile":
		return true
	}
	return false
}

func checkReferences(value, name string) error {
	if name == "srcset" || name == "imagesrcset" {
		// Each candidate begins with a URL; descriptors follow whitespace.
		for _, candidate := range strings.Split(value, ",") {
			fields := strings.Fields(candidate)
			if len(fields) > 0 {
				if err := checkURL(fields[0]); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
		}
		return nil
	}
	if name == "archive" || name == "ping" || name == "itemtype" || name == "attributionsrc" {
		for _, reference := range strings.Fields(value) {
			if err := checkURL(reference); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		return nil
	}
	if err := checkURL(value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func checkURL(value string) error {
	if value == "" || strings.HasPrefix(value, "#") {
		return nil
	}
	return fmt.Errorf("external reference %q", value)
}

func checkCSS(value string) error {
	lower := asciiLower(value)
	for _, forbidden := range []string{"url(", "image-set(", "@import"} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("CSS contains %q", forbidden)
		}
	}
	return nil
}

func asciiLower(value string) string {
	result := []byte(value)
	for index, char := range result {
		if char >= 'A' && char <= 'Z' {
			result[index] += 'a' - 'A'
		}
	}
	return string(result)
}

func asciiLetter(char byte) bool { return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' }
func htmlSpace(char byte) bool {
	return char == '\t' || char == '\n' || char == '\f' || char == '\r' || char == ' '
}

func TestHTMLDocumentReferenceReader(t *testing.T) {
	for _, document := range []string{
		`<p>text &lt;script&gt;</p>`, `<svg><image href="#local"><set attributeName="href" to="#other"/></image></svg>`, `<svg><animate attributeName="href" values="#local;#other"/></svg>`, `<svg><animate attributeName="opacity" from="0" to="1"/></svg>`, `<svg><foreignObject><style><img src=remote></style></foreignObject></svg>`, `<svg><desc><textarea><img src=remote></textarea></desc></svg>`, `<math><mtext><textarea><img src=remote></textarea></mtext></math>`, `<math><annotation-xml encoding="text/html"><style><img src=remote></style></annotation-xml></math>`, `<svg/><title><img src=remote></title>`,
		`<!-- <img src=remote> --><a href="#local">local</a>`,
		`<textarea><img src=remote></textarea>`,
		`<title><img src=remote></title>`,
		`<img src='' src=ignored>`,
		`<svg><use xlink:href="&#35;local"/></svg>`,
		`<img srcset="#a 1x, #b 2x">`,
	} {
		if err := readSelfContainedHTML(document); err != nil {
			t.Errorf("allowed document %q: %v", document, err)
		}
	}
	for _, document := range []string{
		`<svg><style>u<a></a>rl(remote)</style></svg>`, `<svg><style><![CDATA[p{fill:url(remote)}]]></style></svg>`, `<svg><style><![CDATA[p{fill:IMAGE-SET(remote)}]]></style></svg>`, `<svg><image href="#local"><set attributeName="href" to="remote"/></image></svg>`, `<svg><animate attributeName="href" values="#local;remote"/></svg>`, `<svg><animate attributeName="xlink:href" from="remote" to="#local"/></svg>`, `<svg><animate attributeName="href" by="remote"/></svg>`, `<svg><set attributeName="fill" to="url(remote)"/></svg>`, `<!--><img src=remote>`, `<!--comment--!><img src=remote>`, `<svg><style><a href=remote></a></style></svg>`, `<svg><style>p{fill:URL(remote)}</style></svg>`, `<body background=remote>`, `<param valuetype=ref value=remote>`, `<noscript><img src=remote></noscript>`, `<a ping="#local remote">`, `<img SRC=&#x72;emote>`,
		`<img srcset="#a 1x, remote 2x">`, `<object archive="#local remote">`,
		`<svg><use XLINK:HREF=remote /></svg>`, `<math definitionURL=remote>`, `<svg><title><a href=remote></a></title></svg>`,
		`<meta HTTP-EQUIV=ReFrEsH content="0;url=remote">`, `<script></script>`,
		`<p onclick="anything">`, `<style>p{background:URL(remote)}</style>`,
		`<p style="background:IMAGE-SET('remote' 1x)">`, `<style>@IMPORT 'remote';</style>`,
		`<svg><rect fill="url(#local)"/></svg>`, `<iframe srcdoc="&lt;img src=remote&gt;">`,
	} {
		if err := readSelfContainedHTML(document); err == nil {
			t.Errorf("forbidden document accepted: %q", document)
		}
	}
}
