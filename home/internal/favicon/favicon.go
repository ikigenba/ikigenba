// Package favicon fetches a site's icon within a single bounded request budget.
package favicon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Limits bound one fetch and every response it reads.
const (
	Budget       = 10 * time.Second
	MaxPageBytes = 1 << 20
	MaxIconBytes = 256 << 10
	MaxRedirects = 5
)

// Config supplies the client and public request identity.
type Config struct {
	HTTP      *http.Client
	UserAgent string
}

// Fetcher fetches without sending cookies or credentials.
type Fetcher struct {
	client    http.Client
	userAgent string
}

// New copies the client's transport settings and disables cookies and automatic redirects.
func New(cfg Config) *Fetcher {
	c := cfg.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	client := *c
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Fetcher{client: client, userAgent: cfg.UserAgent}
}

func canceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
func mediaType(resp *http.Response) string {
	typ, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return ""
	}
	return typ
}
func isRedirect(resp *http.Response) bool {
	switch resp.StatusCode {
	case 301, 302, 303, 307, 308:
		return resp.Header.Get("Location") != ""
	}
	return false
}

// chain leaves only its final response open for the caller to read.
func (f *Fetcher) chain(ctx context.Context, u *url.URL) (*http.Response, *url.URL, error) {
	for followed := 0; ; followed++ {
		copyURL := *u
		copyURL.User = nil
		u = &copyURL
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, u, err
		}
		req.Header.Set("User-Agent", f.userAgent)
		resp, err := f.client.Do(req)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, u, err
		}
		if !isRedirect(resp) {
			return resp, u, nil
		}
		next, err := u.Parse(resp.Header.Get("Location"))
		if err != nil {
			_ = resp.Body.Close()
			return nil, u, err
		}
		if followed >= MaxRedirects || (next.Scheme != "http" && next.Scheme != "https") {
			return resp, u, nil
		}
		_ = resp.Body.Close()
		u = next
	}
}

const asciiWhitespace = " \t\n\r\f"

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
func asciiFields(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' })
}
func linkHrefs(data []byte) []string {
	z := html.NewTokenizer(bytes.NewReader(data))
	var icons, apple []string
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		if t.Data != "link" {
			continue
		}
		var rel, href string
		seenRel, seenHref := false, false
		for _, a := range t.Attr {
			if a.Key == "rel" && !seenRel {
				rel = a.Val
				seenRel = true
			}
			if a.Key == "href" && !seenHref {
				href = a.Val
				seenHref = true
			}
		}
		icon, touch := false, false
		for _, token := range asciiFields(rel) {
			switch asciiLower(token) {
			case "icon":
				icon = true
			case "apple-touch-icon", "apple-touch-icon-precomposed":
				touch = true
			}
		}
		if icon {
			icons = append(icons, href)
		} else if touch {
			apple = append(apple, href)
		}
	}
	return append(icons, apple...)
}

// Fetch returns the first acceptable icon, or empty values after any cancellation.
func (f *Fetcher) Fetch(ctx context.Context, pageURL string) (data []byte, media string) {
	u, err := url.Parse(pageURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, ""
	}
	u.User = nil
	ctx, cancel := context.WithTimeout(ctx, Budget)
	defer cancel()
	resp, final, err := f.chain(ctx, u)
	if canceled(err) {
		return nil, ""
	}
	if err != nil {
		final = u
	}
	var hrefs []string
	if resp != nil {
		typ := mediaType(resp)
		// Read even non-HTML bodies within the same bound so cancellation is observed.
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxPageBytes))
		_ = resp.Body.Close()
		if canceled(readErr) {
			return nil, ""
		}
		if typ == "text/html" || typ == "application/xhtml+xml" {
			hrefs = linkHrefs(body)
		}
	}
	candidates := make([]*url.URL, 0, len(hrefs)+1)
	for _, href := range hrefs {
		href = strings.Trim(href, asciiWhitespace)
		if href == "" {
			continue
		}
		candidate, err := final.Parse(href)
		if err != nil || (candidate.Scheme != "http" && candidate.Scheme != "https") {
			continue
		}
		candidate.User = nil
		candidates = append(candidates, candidate)
	}
	candidates = append(candidates, &url.URL{Scheme: final.Scheme, Host: final.Host, Path: "/favicon.ico"})
	for _, candidate := range candidates {
		resp, _, err := f.chain(ctx, candidate)
		if canceled(err) {
			return nil, ""
		}
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxIconBytes+1))
		_ = resp.Body.Close()
		if canceled(readErr) {
			return nil, ""
		}
		typ := mediaType(resp)
		if readErr == nil && resp.StatusCode == http.StatusOK && strings.HasPrefix(typ, "image/") && len(body) > 0 && len(body) <= MaxIconBytes {
			return body, typ
		}
	}
	return nil, ""
}
