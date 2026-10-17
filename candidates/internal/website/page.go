package website

import (
	"bytes"
	"context"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Page holds the outcome of fetching one page.
type Page struct {
	URL, Title, Text string
	StatusCode       int
	Size             int64
	Error            string
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
	go func() { done <- k.fetch(fetchCtx, u.String()) }()
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
		pg.Title, pg.Text = "", ""
	}
	return pg
}

// Text extracts visible text nodes in document order and collapses whitespace.
func Text(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
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
	return strings.Join(strings.Fields(strings.ToValidUTF8(strings.Join(nodes, " "), "\uFFFD")), " ")
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
