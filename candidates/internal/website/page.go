package website

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Page holds the outcome of fetching one page.
type Page struct {
	URL, Title, Text, Reason string
	StatusCode               int
	Size                     int64
	Error                    string
}

// Get fetches the exact URL once, following redirects within one timeout.
func (k *Checker) Get(ctx context.Context, address string) Page {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Page{Error: FailRefused}
	}
	if ctx.Err() != nil {
		return Page{Error: transportError(ctx.Err())}
	}
	fetchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := k.after(Timeout)
	done := make(chan Page, 1)
	go func() { done <- k.fetch(fetchCtx, u.String(), PageBytes, true) }()
	var pg Page
	select {
	case pg = <-done:
		select {
		case <-timer:
			pg.Error = FailTimeout
		default:
		}
	case <-timer:
		cancel()
		pg = <-done
		pg.Error = FailTimeout
	case <-ctx.Done():
		cancel()
		pg = <-done
		pg.Error = transportError(ctx.Err())
	}
	if pg.Error != "" {
		pg.Title, pg.Text, pg.Reason = "", "", ""
	}
	return pg
}

// Stored reasons for an unreadable page.
const (
	ReasonScripted = "scripted"
	ReasonRedirect = "redirect"
	ReasonEmpty    = "empty"
)

// Text extracts readable content with light layout and a bounded UTF-8 result.
func Text(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	text := extractedText(doc)
	t := title(body)
	if beyondTitle(text, t) < MinChars {
		flat := flatText(doc)
		if beyondTitle(flat, t) >= MinChars {
			text = flat
		}
	}
	if len(text) > TextBytes {
		n := TextBytes
		for !utf8.RuneStart(text[n]) {
			n--
		}
		text = text[:n]
	}
	return text
}

func normalText(s string) string {
	return strings.Join(strings.Fields(strings.ToValidUTF8(s, "\uFFFD")), " ")
}

func beyondTitle(s, t string) int {
	n := normalText(t)
	if n != "" && strings.HasPrefix(s, n) {
		rest := s[len(n):]
		first, _ := utf8.DecodeRuneInString(rest)
		if rest == "" || unicode.IsSpace(first) {
			s = strings.TrimLeftFunc(rest, unicode.IsSpace)
		}
	}
	return utf8.RuneCountInString(s)
}

func flatText(doc *html.Node) string {
	var nodes []string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript") {
			return
		}
		if n.Type == html.TextNode {
			nodes = append(nodes, n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return normalText(strings.Join(nodes, " "))
}

func role(n *html.Node) string {
	for _, a := range n.Attr {
		if a.Key == "role" {
			fields := strings.Fields(a.Val)
			if len(fields) > 0 {
				return strings.ToLower(fields[0])
			}
			return ""
		}
	}
	return ""
}

func dropped(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "head", "title", "script", "style", "noscript", "template", "iframe", "noembed", "noframes", "textarea", "select", "button", "svg", "math", "nav", "aside", "dialog":
		return true
	case "header", "footer":
		section := false
		for a := n.Parent; a != nil; a = a.Parent {
			switch a.Data {
			case "article", "aside", "main", "nav", "section":
				section = true
			}
		}
		if !section {
			return true
		}
	}
	switch role(n) {
	case "navigation", "banner", "contentinfo", "dialog", "alertdialog":
		return true
	}
	return false
}

func contentRoots(doc *html.Node) []*html.Node {
	var roots []*html.Node
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if dropped(n) {
			return
		}
		if n.Type == html.ElementNode && (n.Data == "main" || role(n) == "main") {
			roots = append(roots, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	if len(roots) == 0 {
		return []*html.Node{doc}
	}
	return roots
}

func layoutMark(n *html.Node) int {
	if n.Type != html.ElementNode {
		return 0
	}
	switch n.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6", "p", "blockquote", "pre", "ul", "ol", "dl", "table", "figure", "hr":
		return 2
	case "br", "li", "dt", "dd", "tr", "td", "th", "caption", "figcaption", "div", "section", "article", "header", "footer", "main", "address", "form", "fieldset", "legend":
		return 1
	}
	return 0
}

func extractedText(doc *html.Node) string {
	var result, segment strings.Builder
	pending := 0
	mark := func(level int) {
		if s := normalText(segment.String()); s != "" {
			if result.Len() > 0 {
				result.WriteString(strings.Repeat("\n", pending))
			}
			result.WriteString(s)
			pending = 0
		}
		segment.Reset()
		if level > pending {
			pending = level
		}
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if dropped(n) {
			return
		}
		level := layoutMark(n)
		if level > 0 {
			mark(level)
		}
		if n.Type == html.TextNode {
			segment.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
		if level > 0 {
			mark(level)
		}
	}
	for i, root := range contentRoots(doc) {
		if i > 0 {
			mark(2)
		}
		visit(root)
	}
	mark(0)
	return result.String()
}

func unreadableReason(body []byte) string {
	doc, _ := html.Parse(bytes.NewReader(body))
	reason := ReasonEmpty
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Namespace == "" {
				switch n.Data {
				case "frameset", "frame", "iframe":
					reason = ReasonRedirect
				case "meta":
					for _, a := range n.Attr {
						if a.Key == "http-equiv" && strings.EqualFold(strings.TrimSpace(a.Val), "refresh") {
							reason = ReasonRedirect
						}
					}
				}
			}
			if n.Data == "script" && reason != ReasonRedirect {
				reason = ReasonScripted
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return reason
}

// OnSite reports whether page belongs to site's host or one of its subdomains.
func OnSite(site, page string) bool {
	host := siteHost(site)
	u, err := url.Parse(page)
	if host == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	p := siteHost(page)
	return p == host || strings.HasSuffix(p, "."+host)
}

func siteHost(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
