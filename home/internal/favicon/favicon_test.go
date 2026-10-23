package favicon_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/home/internal/favicon"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type body struct {
	io.Reader
	closed bool
	read   int
}

func (b *body) Read(p []byte) (int, error) { n, e := b.Reader.Read(p); b.read += n; return n, e }
func (b *body) Close() error               { b.closed = true; return nil }

type answer struct {
	status        int
	typ, location string
	data          io.Reader
	err           error
}
type site struct {
	t        *testing.T
	answers  map[string]answer
	requests []*http.Request
	bodies   []*body
}

func (s *site) RoundTrip(r *http.Request) (*http.Response, error) {
	s.requests = append(s.requests, r)
	a, ok := s.answers[r.URL.String()]
	if !ok {
		s.t.Errorf("unexpected request %s", r.URL)
		a = answer{status: 404}
	}
	if a.err != nil {
		return nil, a.err
	}
	if a.status == 0 {
		a.status = 200
	}
	if a.data == nil {
		a.data = strings.NewReader("")
	}
	b := &body{Reader: a.data}
	s.bodies = append(s.bodies, b)
	return &http.Response{StatusCode: a.status, Header: http.Header{"Content-Type": {a.typ}, "Location": {a.location}}, Body: b, Request: r}, nil
}
func (s *site) urls() []string {
	r := make([]string, 0, len(s.requests))
	for _, q := range s.requests {
		r = append(r, q.URL.String())
	}
	return r
}
func (s *site) closed() {
	s.t.Helper()
	for _, b := range s.bodies {
		if !b.closed {
			s.t.Error("unclosed response body")
		}
	}
}
func fetch(s *site, u string) ([]byte, string) {
	s.t.Helper()
	f := favicon.New(favicon.Config{HTTP: &http.Client{Transport: s}, UserAgent: "public-identity"})
	b, typ := f.Fetch(context.Background(), u)
	s.closed()
	return b, typ
}

// R-FGXN-CXX1 R-FI5J-QPNQ R-FJDG-4HEF R-FKLC-I954 R-FLT8-W0VT R-FN15-9SMI R-FO91-NKD7 R-FPGY-1C3W R-FWSC-BYK2
func TestInvalidAndConstants(t *testing.T) {
	if favicon.Budget != 10*time.Second || favicon.MaxPageBytes != 1<<20 || favicon.MaxIconBytes != 256<<10 || favicon.MaxRedirects != 5 {
		t.Fatal("limits")
	}
	for _, u := range []string{"", ":bad", "http://[::1", "ftp://x/", "http:", "//host/"} {
		s := &site{t: t}
		b, typ := fetch(s, u)
		if len(b) != 0 || typ != "" || len(s.requests) != 0 {
			t.Fatal(u, b, typ, s.urls())
		}
	}
}

// R-JWLF-PNK1 R-JZ18-H71F R-FT4N-6NBZ R-FUCJ-KF2O R-FVKF-Y6TD R-K1H1-8QIT R-G43Q-ML08 R-K54Q-E1QW
func TestTokenizerOrderingAndCredentials(t *testing.T) {
	page := `<linK rel="icon" href="/unicode-tag"><link rel="İCON" href="/unicode-rel"><!-- <link rel="icon" href="/comment"> --><script>"<link rel='icon' href='/script'>"</script><base href="https://ignored.test/"><LINK REL='apple-touch-icon' HREF='/apple'><link rel="ICON other" rel="apple-touch-icon" href=" /first?a=1&amp;b=2 " href="/ignored"><link rel='shortcut&#9;IcOn' href='//name:password@cdn.test/second'/><link rel='icon' href='data:bad'><link rel='icon' href=''><link rel='icon' href='http://[::1'><link rel='apple-touch-icon-precomposed' href='last'>`
	s := &site{t: t, answers: map[string]answer{
		"https://origin.test/start":           {status: 302, location: (&url.URL{Scheme: "https", Host: "final.test", User: url.UserPassword("user", "pass"), Path: "/dir/page", RawQuery: "query=x"}).String()},
		"https://final.test/dir/page?query=x": {status: 404, typ: "TEXT/HTML; charset=utf-8", data: strings.NewReader(page)},
		"https://final.test/first?a=1&b=2":    {typ: "text/plain", data: strings.NewReader("wrong")},
		"https://cdn.test/second":             {status: 500, typ: "image/png", data: strings.NewReader("bad-status")},
		"https://final.test/apple":            {typ: "image/svg+xml; charset=utf-8", data: strings.NewReader("chosen")},
	}}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"https://origin.test", "https://final.test", "https://cdn.test"} {
		u, err := url.Parse(host)
		if err != nil {
			t.Fatal(err)
		}
		jar.SetCookies(u, []*http.Cookie{{Name: "secret", Value: "value", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}})
	}
	f := favicon.New(favicon.Config{HTTP: &http.Client{Transport: s, Jar: jar}, UserAgent: "public-identity"})
	b, typ := f.Fetch(context.Background(), "https://login:password@origin.test/start")
	s.closed()
	want := []string{"https://origin.test/start", "https://final.test/dir/page?query=x", "https://final.test/first?a=1&b=2", "https://cdn.test/second", "https://final.test/apple"}
	if !reflect.DeepEqual(want, s.urls()) || string(b) != "chosen" || typ != "image/svg+xml" {
		t.Fatal(s.urls(), string(b), typ)
	}
	for _, r := range s.requests {
		if r.Method != http.MethodGet || r.Body != nil || r.URL.User != nil || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("User-Agent") != "public-identity" {
			t.Fatal("request leaked state", r)
		}
	}
}

// R-FY08-PQAR R-K094-UYS4 R-G5BN-0CQX R-K54Q-E1QW
func TestRedirectChains(t *testing.T) {
	for _, mode := range []string{"limit", "exact", "scheme", "malformed", "no-location"} {
		t.Run(mode, func(t *testing.T) {
			s := &site{t: t, answers: map[string]answer{"https://x/page": {typ: "text/plain"}}}
			for i := 0; i <= favicon.MaxRedirects; i++ {
				u := fmt.Sprintf("https://x/icon/%d", i)
				a := answer{status: 302, location: fmt.Sprintf("/icon/%d", i+1)}
				if i == favicon.MaxRedirects && mode == "exact" {
					a = answer{typ: "IMAGE/PNG; q=ok", data: strings.NewReader("yes")}
				}
				s.answers[u] = a
			}
			s.answers["https://x/favicon.ico"] = answer{status: 302, location: "/icon/0"}
			// The first redirect counts too: start directly at icon/0 for exact-limit coverage.
			s.answers["https://x/page"] = answer{typ: "text/html", data: strings.NewReader(`<link rel="icon" href="/icon/0">`)}
			if mode == "scheme" {
				s.answers["https://x/icon/0"] = answer{status: 302, location: "ftp://other/", typ: "image/png", data: strings.NewReader("redirect")}
			}
			if mode == "malformed" {
				s.answers["https://x/icon/0"] = answer{status: 302, location: "http://[::1"}
			}
			if mode == "no-location" {
				s.answers["https://x/icon/0"] = answer{status: 302, typ: "image/png", data: strings.NewReader("redirect")}
			}
			if mode != "limit" {
				s.answers["https://x/favicon.ico"] = answer{status: 404}
			}
			b, typ := fetch(s, "https://x/page")
			if mode == "exact" {
				if string(b) != "yes" || typ != "image/png" || len(s.requests) != 2+favicon.MaxRedirects {
					t.Fatal(string(b), typ, s.urls())
				}
			} else {
				if len(b) != 0 || typ != "" {
					t.Fatal(b, typ)
				}
				if mode == "limit" && len(s.requests) != 1+2*(1+favicon.MaxRedirects) {
					t.Fatal(s.urls())
				}
				if mode != "limit" && len(s.requests) != 3 {
					t.Fatal(s.urls())
				}
			}
		})
	}
}

// R-K2OX-MI9I R-K3WU-0A07 R-G43Q-ML08 R-G5BN-0CQX R-K54Q-E1QW
func TestIconAcceptanceAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, typ    string
		status, size int
		accepted     bool
	}{{"exact", "image/png", 200, favicon.MaxIconBytes, true}, {"large", "image/png", 200, favicon.MaxIconBytes + 5, false}, {"empty", "image/png", 200, 0, false}, {"wrong-type", "text/plain", 200, 5, false}, {"invalid-type", "image/png; broken", 200, 5, false}, {"wrong-status", "image/png", 500, 5, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &site{t: t, answers: map[string]answer{"https://x/": {typ: "text/plain"}, "https://x/favicon.ico": {status: tc.status, typ: tc.typ, data: bytes.NewReader(bytes.Repeat([]byte{7}, tc.size))}}}
			b, typ := fetch(s, "https://x/")
			if tc.accepted {
				if len(b) != tc.size || typ != "image/png" || !bytes.Equal(b, bytes.Repeat([]byte{7}, tc.size)) {
					t.Fatal(len(b), typ)
				}
			} else if len(b) != 0 || typ != "" {
				t.Fatal(len(b), typ)
			}
			if s.bodies[1].read > favicon.MaxIconBytes+1 {
				t.Fatal(s.bodies[1].read)
			}
		})
	}
	s := &site{t: t, answers: map[string]answer{"https://x/": {typ: "text/html", data: io.MultiReader(strings.NewReader(`<link rel="icon" href="/inside">`), strings.NewReader(strings.Repeat(" ", favicon.MaxPageBytes)), strings.NewReader(`<link rel="icon" href="/outside">`))}, "https://x/inside": {status: 404}, "https://x/favicon.ico": {status: 404}}}
	fetch(s, "https://x/")
	if s.bodies[0].read != favicon.MaxPageBytes || !reflect.DeepEqual(s.urls(), []string{"https://x/", "https://x/inside", "https://x/favicon.ico"}) {
		t.Fatal(s.bodies[0].read, s.urls())
	}
}

type errorReader struct {
	data []byte
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

// R-THJP-UMRA R-K1H1-8QIT R-JZ18-H71F R-G5BN-0CQX
func TestErrorsAndCancellation(t *testing.T) {
	for _, stop := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, at := range []string{"page-transport", "page-body", "icon-transport", "icon-body"} {
			t.Run(fmt.Sprint(stop, at), func(t *testing.T) {
				s := &site{t: t, answers: map[string]answer{"https://x/": {typ: "text/html", data: strings.NewReader(`<link rel="icon" href="/first"><link rel="icon" href="/second">`)}, "https://x/first": {typ: "image/png", data: strings.NewReader("ok")}}}
				switch at {
				case "page-transport":
					s.answers["https://x/"] = answer{err: fmt.Errorf("wrapped: %w", stop)}
				case "page-body":
					s.answers["https://x/"] = answer{typ: "text/html", data: &errorReader{data: []byte(`<link rel="icon" href="/first">`), err: fmt.Errorf("wrapped: %w", stop)}}
				case "icon-transport":
					s.answers["https://x/first"] = answer{err: fmt.Errorf("wrapped: %w", stop)}
				case "icon-body":
					s.answers["https://x/first"] = answer{typ: "image/png", data: &errorReader{data: []byte("partial"), err: fmt.Errorf("wrapped: %w", stop)}}
				}
				b, typ := fetch(s, "https://x/")
				want := 1
				if strings.HasPrefix(at, "icon") {
					want = 2
				}
				if len(b) != 0 || typ != "" || len(s.requests) != want {
					t.Fatal(b, typ, s.urls())
				}
			})
		}
	}
	for _, mode := range []string{"page-error", "partial-page", "icon-error", "partial-icon"} {
		t.Run(mode, func(t *testing.T) {
			failure := errors.New("site error")
			s := &site{t: t, answers: map[string]answer{"https://x/": {typ: "text/html", data: strings.NewReader(`<link rel="icon" href="/first">`)}, "https://x/first": {err: failure}, "https://x/favicon.ico": {typ: "image/png", data: strings.NewReader("fallback")}}}
			switch mode {
			case "page-error":
				s.answers["https://x/"] = answer{err: failure}
			case "partial-page":
				s.answers["https://x/"] = answer{typ: "text/html", data: &errorReader{data: []byte(`<link rel="icon" href="/first">`), err: failure}}
			case "partial-icon":
				s.answers["https://x/first"] = answer{typ: "image/png", data: &errorReader{data: []byte("partial"), err: failure}}
			}
			b, typ := fetch(s, "https://x/")
			if string(b) != "fallback" || typ != "image/png" {
				t.Fatal(string(b), typ)
			}
			want := 3
			if mode == "page-error" {
				want = 2
			}
			if len(s.requests) != want {
				t.Fatal(s.urls())
			}
		})
	}
}

// R-TGBT-GV0L
func TestOneDeadline(t *testing.T) {
	for _, early := range []bool{false, true} {
		ctx := context.Background()
		var caller time.Time
		var cancel context.CancelFunc
		if early {
			caller = time.Now().Add(time.Second)
			ctx, cancel = context.WithDeadline(ctx, caller)
			defer cancel()
		}
		var deadlines []time.Time
		s := &site{t: t, answers: map[string]answer{"https://x/": {status: 302, location: "/next"}, "https://x/next": {typ: "text/plain"}, "https://x/favicon.ico": {status: 404}}}
		rt := roundTrip(func(r *http.Request) (*http.Response, error) {
			d, ok := r.Context().Deadline()
			if !ok {
				t.Error("no deadline")
			}
			deadlines = append(deadlines, d)
			return s.RoundTrip(r)
		})
		f := favicon.New(favicon.Config{HTTP: &http.Client{Transport: rt}, UserAgent: "ua"})
		before := time.Now()
		f.Fetch(ctx, "https://x/")
		after := time.Now()
		s.closed()
		if len(deadlines) != 3 {
			t.Fatal(deadlines)
		}
		for _, d := range deadlines {
			if !d.Equal(deadlines[0]) {
				t.Fatal("deadlines differ")
			}
			if early {
				if !d.Equal(caller) {
					t.Fatal(d, caller)
				}
			} else if d.Before(before.Add(favicon.Budget)) || d.After(after.Add(favicon.Budget)) {
				t.Fatal(d, before, after)
			}
		}
	}
}

// R-G8ZC-5NZ0
func TestConcurrentFetch(t *testing.T) {
	rt := roundTrip(func(r *http.Request) (*http.Response, error) {
		typ, text := "text/plain", ""
		if r.URL.Path == "/favicon.ico" {
			typ, text = "image/png", "icon"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {typ}}, Body: io.NopCloser(strings.NewReader(text)), Request: r}, nil
	})
	f := favicon.New(favicon.Config{HTTP: &http.Client{Transport: rt}, UserAgent: "ua"})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			b, typ := f.Fetch(context.Background(), "https://x/")
			if string(b) != "icon" || typ != "image/png" {
				t.Error(string(b), typ)
			}
		})
	}
	wg.Wait()
}

// R-FT4N-6NBZ R-JZ18-H71F R-K1H1-8QIT
func TestDuplicatesAndAppleOrder(t *testing.T) {
	s := &site{t: t, answers: map[string]answer{
		"https://x/dir/page": {typ: "application/xhtml+xml", data: strings.NewReader(`<link rel="apple-touch-icon-precomposed" href="last"><link rel="icon" href="/same"><link rel="icon" href="/same"><link rel="apple-touch-icon" href="/later">`)},
		"https://x/same":     {status: 404},
		"https://x/dir/last": {status: 404},
		"https://x/later":    {typ: "image/png", data: strings.NewReader("winner")},
	}}
	b, typ := fetch(s, "https://x/dir/page")
	if string(b) != "winner" || typ != "image/png" || !reflect.DeepEqual(s.urls(), []string{"https://x/dir/page", "https://x/same", "https://x/same", "https://x/dir/last", "https://x/later"}) {
		t.Fatal(string(b), typ, s.urls())
	}
}

// R-JWLF-PNK1 R-FY08-PQAR R-K094-UYS4 R-JZ18-H71F
func TestPageRedirectOutcomes(t *testing.T) {
	for _, status := range []int{301, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := &site{t: t, answers: map[string]answer{
				"https://original.test/page":        {status: status, location: "https://other.test/final"},
				"https://other.test/final":          {status: 302, location: "http://[::1"},
				"https://original.test/favicon.ico": {typ: "image/png", data: strings.NewReader("fallback")},
			}}
			b, typ := fetch(s, "https://original.test/page")
			if string(b) != "fallback" || typ != "image/png" || !reflect.DeepEqual(s.urls(), []string{"https://original.test/page", "https://other.test/final", "https://original.test/favicon.ico"}) {
				t.Fatal(string(b), typ, s.urls())
			}
		})
	}
	for _, mode := range []string{"scheme", "limit"} {
		t.Run(mode, func(t *testing.T) {
			s := &site{t: t, answers: map[string]answer{}}
			for i := 0; i <= favicon.MaxRedirects; i++ {
				location := fmt.Sprintf("/page/%d", i+1)
				if mode == "scheme" {
					location = "ftp://elsewhere.test/"
				}
				s.answers[fmt.Sprintf("https://x/page/%d", i)] = answer{status: 302, location: location, typ: "text/html", data: strings.NewReader(`<link rel="icon" href="relative">`)}
			}
			s.answers["https://x/page/relative"] = answer{typ: "image/png", data: strings.NewReader("redirect-body-icon")}
			b, typ := fetch(s, "https://x/page/0")
			if string(b) != "redirect-body-icon" || typ != "image/png" {
				t.Fatal(string(b), typ, s.urls())
			}
			want := 2
			if mode == "limit" {
				want = 2 + favicon.MaxRedirects
			}
			if len(s.requests) != want {
				t.Fatal(s.urls())
			}
		})
	}
}
