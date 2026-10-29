// Package website checks candidate email domains through a bounded HTTP fetcher.
package website

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"golang.org/x/net/html"
)

// Check limits and result diagnostics.
const (
	Concurrency          = 8
	Interval             = 500_000_000
	Timeout              = 10_000_000_000
	BodyCap              = 262144
	Redirects            = 5
	PageBytes            = 8388608
	TextBytes            = 65536
	MinChars             = 200
	UserAgent     string = "Mozilla/5.0 (compatible; Ikigenba website checker)"
	FailStatus    string = "Website returned an unsuccessful status"
	FailEmpty     string = "Website returned an empty page"
	FailType      string = "Website did not return HTML"
	FailTimeout   string = "Website check timed out"
	FailRedirects string = "Website redirected too many times"
	FailRefused   string = "website address refused"
)

// Result holds the last attempt's outcome.
type Result struct {
	State, URL, Title string
	StatusCode        int
	Size              int64
	Error             string
	From              string
	Misses            []Miss
}

// Miss records the last failure of one offered element.
type Miss struct {
	From       string
	StatusCode int
	Size       int64
	Error      string
}

// Checker uses an injected HTTP client and timers for all checks and pacing.
type Checker struct {
	Client *http.Client
	After  func(d time.Duration) <-chan time.Time
}

// Domain returns the non-provider domain of a display email.
func Domain(email string) string {
	return sources.DomainKey("@" + strings.TrimRight(sources.DomainKey(email), "."))
}

func (k *Checker) after(d time.Duration) <-chan time.Time {
	if k.After != nil {
		return k.After(d)
	}
	return time.After(d)
}

// Check tries the secure and plain home pages, with and without www.
func (k *Checker) Check(ctx context.Context, domain string) Result {
	return k.check(ctx, domain, nil)
}

func (k *Checker) check(ctx context.Context, in string, started chan struct{}) Result {
	if started != nil {
		close(started)
	}
	r := Result{State: sources.WebsiteNone}
	var misses []Miss
	for _, element := range URLs(in) {
		for _, address := range attemptURLs(element) {
			if ctx.Err() != nil {
				r.Misses = misses
				return r
			}
			r = k.timedAttempt(ctx, address)
			if ctx.Err() != nil {
				r.Misses = misses
				return r
			}
			if r.Error == "" {
				r.From = element
				r.Misses = misses
				return r
			}
		}
		misses = append(misses, Miss{element, r.StatusCode, r.Size, r.Error})
	}
	r.Misses = misses
	return r
}

func (k *Checker) timedAttempt(ctx context.Context, address string) Result {
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := k.after(Timeout)
	select {
	case <-timer:
		return Result{State: sources.WebsiteNone, Error: FailTimeout}
	default:
	}
	done := make(chan Result, 1)
	go func() { done <- k.attempt(attemptCtx, address) }()
	var r Result
	select {
	case r = <-done:
		select {
		case <-timer:
			r.Error = FailTimeout
		default:
		}
	case <-timer:
		cancel()
		r = <-done
		r.Error = FailTimeout
	case <-ctx.Done():
		cancel()
		r = <-done
	}
	if r.Error != "" {
		r.State = sources.WebsiteNone
		r.URL = ""
		r.Title = ""
	}
	return r
}

// Inputs combines a source website with the email's own domain.
func Inputs(site, email string) string {
	s := strings.TrimSpace(site)
	if !offered(s) {
		s = ""
	}
	d := Domain(email)
	if s != "" && d != "" {
		return s + "\n" + d
	}
	return s + d
}

// OwnerInputs offers the owner's website before source websites and email.
func OwnerInputs(owner, site, email string) string {
	o := strings.TrimSpace(owner)
	if o == "" {
		return Inputs(site, email)
	}
	if strings.TrimSpace(site) == o {
		site = ""
	}
	i := Inputs(site, email)
	if i == "" {
		return o
	}
	return o + "\n" + i
}

// URLs returns the elements of website-check inputs.
func URLs(in string) []string {
	out := []string{}
	if in == "" {
		return out
	}
	for {
		a, b, ok := strings.Cut(in, "\n")
		if !ok || !offered(a) || b == "" {
			return append(out, in)
		}
		out = append(out, a)
		in = b
	}
}
func offered(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || siteHost(s) == "" {
		return false
	}
	_, err = netip.ParseAddr(u.Hostname())
	return err != nil
}
func attemptURLs(element string) []string {
	hosts := []string{element, "www." + element, element, "www." + element}
	schemes := []string{"https", "https", "http", "http"}
	if !offered(element) {
		out := []string{}
		for i, h := range hosts {
			out = append(out, schemes[i]+"://"+h)
		}
		return out
	}
	u, _ := url.Parse(element)
	base := siteHost(element)
	out := []string{}
	for i := range schemes {
		host := base
		if i%2 == 1 {
			host = "www." + base
		}
		if u.Port() != "" {
			host += ":" + u.Port()
		}
		x := url.URL{Scheme: schemes[i], Host: host, Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
		out = append(out, x.String())
	}
	return out
}

func (k *Checker) attempt(ctx context.Context, address string) Result {
	pg := k.fetch(ctx, address, BodyCap, false)
	r := Result{State: sources.WebsiteNone, StatusCode: pg.StatusCode, Size: pg.Size, Error: pg.Error}
	if pg.Error == "" {
		u, _ := url.Parse(pg.URL)
		r.State, r.URL, r.Title = u.Scheme, pg.URL, pg.Title
	}
	return r
}

func (k *Checker) fetch(ctx context.Context, address string, limit int64, pageText bool) Page {
	r := Page{}
	client := *k.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for redirects := 0; ; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			r.Error = transportError(err)
			return r
		}
		req.Header.Set("User-Agent", UserAgent)
		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(err, errRefused) {
				r.Error = FailRefused
			} else {
				r.Error = transportError(err)
			}
			return r
		}
		r.URL = req.URL.String()
		r.StatusCode = resp.StatusCode
		if isRedirect(resp.StatusCode) && resp.Header.Get("Location") != "" {
			next, e := req.URL.Parse(resp.Header.Get("Location"))
			_ = resp.Body.Close()
			if e != nil {
				r.Error = transportError(e)
				return r
			}
			if next.Scheme != "https" && next.Scheme != "http" {
				r.Error = FailRefused
				return r
			}
			if redirects == Redirects {
				r.Error = FailRedirects
				return r
			}
			address = next.String()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			r.Error = FailStatus
			return r
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
		_ = resp.Body.Close()
		r.Size = int64(len(body))
		typ := resp.Header.Get("Content-Type")
		if typ == "" {
			typ = http.DetectContentType(body)
		}
		typ, _, _ = mime.ParseMediaType(typ)
		switch {
		case len(body) == 0:
			r.Error = FailEmpty
		case typ != "text/html" && typ != "application/xhtml+xml":
			r.Error = FailType
		case readErr != nil:
			r.Error = transportError(readErr)
		default:
			r.URL = req.URL.String()
			r.Title = title(body)
			if pageText {
				r.Text = Text(body)
				if beyondTitle(r.Text, r.Title) < MinChars {
					r.Text = ""
					r.Reason = unreadableReason(body)
				}
			}
		}
		return r
	}
}

func isRedirect(status int) bool {
	return status == 301 || status == 302 || status == 303 || status == 307 || status == 308
}

func title(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var visit func(*html.Node) (string, bool)
	visit = func(n *html.Node) (string, bool) {
		if n.Type == html.ElementNode && n.Data == "title" && n.Namespace == "" {
			var b strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					b.WriteString(c.Data)
				}
			}
			return strings.TrimSpace(strings.ToValidUTF8(b.String(), "\uFFFD")), true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if s, ok := visit(c); ok {
				return s, true
			}
		}
		return "", false
	}
	s, _ := visit(doc)
	return s
}

// CheckAll paces starts and bounds the number of concurrent checks.
func (k *Checker) CheckAll(ctx context.Context, domains []string, each func(string, Result)) {
	slots := make(chan struct{}, Concurrency)
	var wg sync.WaitGroup
	defer wg.Wait()
	for i, d := range domains {
		if i > 0 {
			select {
			case <-k.after(Interval):
			case <-ctx.Done():
				return
			}
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		if ctx.Err() != nil {
			return
		}
		wg.Add(1)
		started := make(chan struct{})
		go func(domain string) {
			defer wg.Done()
			r := k.check(ctx, domain, started)
			<-slots
			if ctx.Err() == nil {
				each(domain, r)
			}
		}(d)
		<-started
	}
}

const failTransportPrefix = "website transport: "

func transportError(err error) string {
	return failTransportPrefix + err.Error()
}
