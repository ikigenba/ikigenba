package website_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func TestPageShape(t *testing.T) {
	// R-DY9X-8ZC4
	pg := website.Page{"url", "title", "text", "reason", 201, 123, "error"}
	if pg.URL != "url" || pg.Title != "title" || pg.Text != "text" || pg.Reason != "reason" || pg.StatusCode != 201 || pg.Size != 123 || pg.Error != "error" {
		t.Fatal(pg)
	}
}

func TestGetSuccessAndRedirects(t *testing.T) {
	// R-2EOR-MW1G R-2KS9-JQQX R-E814-B59O R-R67B-CO7S
	for _, status := range []int{0, 301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := "<title>  Acme </title><p>" + strings.Repeat("freight ", 45000)
			calls, timers := 0, 0
			address, landed := "http://Acme.test:8080/about", "https://www.acme.test/home"
			k := checker(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.Header.Get("User-Agent") != website.UserAgent {
					t.Error(r)
				}
				want := address
				if calls > 1 {
					want = landed
				}
				if r.URL.String() != want {
					t.Errorf("URL %s want %s", r.URL, want)
				}
				if status != 0 && calls == 1 {
					resp := response(status, "", "")
					resp.Header.Set("Location", landed)
					return resp, nil
				}
				return response(200, "text/html", body), nil
			})
			k.After = func(d time.Duration) <-chan time.Time {
				timers++
				if d != website.Timeout {
					t.Error(d)
				}
				return never(d)
			}
			pg := k.Get(context.Background(), address)
			wantCalls := 1
			if status == 0 {
				landed = address
			} else {
				wantCalls = 2
			}
			if pg.URL != landed || pg.Title != "Acme" || pg.Text != website.Text([]byte(body)) || pg.StatusCode != 200 || pg.Size != int64(len(body)) || pg.Error != "" || calls != wantCalls || timers != 1 {
				t.Fatal(pg, calls, timers)
			}
		})
	}
}

func TestGetFailures(t *testing.T) {
	// R-E814-B59O R-EE4M-7ZZ5
	const address = "https://acme.test/about"
	tests := []struct {
		name                         string
		status                       int
		typ, body, location, failure string
		count                        int
		size                         int64
	}{
		{"status", 404, "text/html", "no", "", website.FailStatus, 1, 0},
		{"empty", 200, "text/html", "", "", website.FailEmpty, 1, 0},
		{"binary", 200, "application/octet-stream", strings.Repeat("x", 1024), "", website.FailType, 1, 1024},
		{"loop", 302, "", "", address, website.FailRedirects, 6, 0},
		{"scheme", 302, "", "", "ftp://acme.test/", website.FailRefused, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, timers := 0, 0
			k := checker(func(r *http.Request) (*http.Response, error) {
				count++
				if r.Method != http.MethodGet || r.Header.Get("User-Agent") != website.UserAgent {
					t.Error(r)
				}
				resp := response(tt.status, tt.typ, tt.body)
				resp.Header.Set("Location", tt.location)
				return resp, nil
			})
			k.After = func(d time.Duration) <-chan time.Time {
				timers++
				if d != website.Timeout {
					t.Error(d)
				}
				return never(d)
			}
			pg := k.Get(context.Background(), address)
			want := website.Page{URL: address, StatusCode: tt.status, Size: tt.size, Error: tt.failure}
			if pg != want || count != tt.count || timers != 1 {
				t.Fatal(pg, want, count, timers)
			}
		})
	}
	k := checker(func(*http.Request) (*http.Response, error) { return nil, errors.New("boom") })
	pg := k.Get(context.Background(), address)
	if pg.URL != "" || pg.StatusCode != 0 || pg.Size != 0 || pg.Title != "" || pg.Text != "" || pg.Reason != "" || !strings.Contains(pg.Error, "boom") {
		t.Fatal(pg)
	}
	// A failed connection after a redirect retains the last response's URL and status.
	k = checker(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != address {
			return nil, errors.New("boom")
		}
		resp := response(301, "", "")
		resp.Header.Set("Location", "/next")
		return resp, nil
	})
	pg = k.Get(context.Background(), address)
	if pg.URL != address || pg.StatusCode != 301 || !strings.Contains(pg.Error, "boom") {
		t.Fatal(pg)
	}
}

func TestGetInvalidURLs(t *testing.T) {
	// R-2KS9-JQQX R-2PNV-2TPP
	k := checker(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected request")
		return nil, errors.New("unexpected")
	})
	k.After = func(time.Duration) <-chan time.Time { t.Error("unexpected timer"); return never(0) }
	for _, u := range []string{"ftp://acme.test/", "acme.test/about", "mailto:ops@acme.test", "http://", "", "http://%zz/"} {
		if pg := k.Get(context.Background(), u); pg != (website.Page{Error: website.FailRefused}) {
			t.Fatal(u, pg)
		}
	}
}

func TestGetTimeoutAndCancellation(t *testing.T) {
	// R-EE4M-7ZZ5 R-2QVR-GLGE
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			requests := make(chan *http.Request, 2)
			release := make(chan struct{})
			timer := make(chan time.Time, 1)
			k := checker(func(r *http.Request) (*http.Response, error) {
				requests <- r
				<-r.Context().Done()
				<-release
				return nil, r.Context().Err()
			})
			timers := 0
			k.After = func(d time.Duration) <-chan time.Time {
				timers++
				if d != website.Timeout {
					t.Error(d)
				}
				return timer
			}
			done := make(chan website.Page, 1)
			go func() { done <- k.Get(ctx, "https://acme.test/about") }()
			req := await(t, requests)
			if timeout {
				timer <- time.Time{}
			} else {
				cancel()
			}
			await(t, req.Context().Done())
			select {
			case pg := <-done:
				t.Fatal("returned before transport", pg)
			default:
			}
			close(release)
			pg := await(t, done)
			if pg.Error == "" || pg.Title != "" || pg.Text != "" || pg.Reason != "" || pg.URL != "" || pg.StatusCode != 0 || pg.Size != 0 || (timeout && pg.Error != website.FailTimeout) || timers != 1 {
				t.Fatal(pg, timers)
			}
			select {
			case req := <-requests:
				t.Fatal("extra request", req)
			default:
			}
		})
	}
}

func TestOnSite(t *testing.T) {
	// R-2JKD-5Z08 R-2UJG-LWOH
	for _, site := range []string{"https://acme.com/", "https://www.acme.com/home", "https://acme.com:8443/"} {
		for _, page := range []string{"https://www.acme.com/", "http://acme.com:8080/x", "https://ACME.com/", "https://shop.acme.com/about", "https://acme.com/about"} {
			if !website.OnSite(site, page) {
				t.Error(site, page)
			}
		}
		for _, page := range []string{"https://notacme.com/", "https://acme.com.evil.test/", "https://acme.com@evil.test/", "https://acme.net/", "ftp://acme.com/", "acme.com/about", "https://%zz/"} {
			if website.OnSite(site, page) {
				t.Error(site, page)
			}
		}
	}
	for _, site := range []string{"", "acme.com", "https://www./", "https://www.www.acme.com/", "https://%zz/"} {
		if website.OnSite(site, "https://acme.com/") {
			t.Error(site)
		}
	}
}

func TestGetRealClientGuard(t *testing.T) {
	// R-2S3N-UD73
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if conn != nil {
			_ = conn.Close()
		}
		accepted <- acceptErr
	}()
	client := website.Client()
	defer client.CloseIdleConnections()
	k := &website.Checker{Client: client, After: never}
	for _, u := range []string{"http://0.0.0.0/", "http://10.0.0.1/about", "http://169.254.1.1/", "http://[::]/", "http://" + listener.Addr().String()} {
		if pg := k.Get(context.Background(), u); pg != (website.Page{Error: website.FailRefused}) {
			t.Fatal(u, pg)
		}
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := await(t, accepted); err == nil {
		t.Fatal("inward connection accepted")
	}
}

func TestStubGet(t *testing.T) {
	// R-EFCI-LRPU R-2WZ9-DG5V
	stub, err := website.NewStub()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stub.Close(); err != nil {
			t.Error(err)
		}
	}()
	k := &website.Checker{Client: stub.Client(), After: never}
	for _, u := range []string{"https://secure-co.test/about", "http://secure-co.test/"} {
		pg := k.Get(context.Background(), u)
		if pg.URL != u || pg.Title != "secure-co.test" || !strings.Contains(pg.Text, "secure-co.test") || pg.StatusCode != 200 || pg.Size <= 0 || pg.Reason != "" || pg.Error != "" {
			t.Fatal(pg)
		}
	}
	if pg := k.Get(context.Background(), "http://upgrade-co.test/"); pg.URL != "https://upgrade-co.test/home" || pg.Text == "" || pg.Reason != "" || pg.StatusCode != 200 || pg.Error != "" {
		t.Fatal(pg)
	}
	for _, tt := range []struct {
		host    string
		status  int
		size    int64
		failure string
	}{{"missing.secure-co.test", 404, 0, website.FailStatus}, {"empty.test", 200, 0, website.FailEmpty}, {"binary.test", 200, 1024, website.FailType}} {
		u := "https://" + tt.host + "/about"
		pg := k.Get(context.Background(), u)
		if pg != (website.Page{URL: u, StatusCode: tt.status, Size: tt.size, Error: tt.failure}) {
			t.Fatal(pg)
		}
	}
	for _, host := range []string{"refused.test", "example.com", "unlisted.test"} {
		pg := k.Get(context.Background(), "https://"+host+"/about")
		if pg.URL != "" || pg.Title != "" || pg.Text != "" || pg.Reason != "" || pg.StatusCode != 0 || pg.Size != 0 || pg.Error == "" {
			t.Fatal(pg)
		}
		for _, failure := range failureCopy() {
			if pg.Error == failure {
				t.Fatal(pg)
			}
		}
	}
}

func TestGetHTMLAndReadRules(t *testing.T) {
	// R-E814-B59O R-R67B-CO7S R-EE4M-7ZZ5
	for _, tt := range []struct{ typ, body, title, failure string }{
		{"application/xhtml+xml; charset=utf-8", "<title> X </title>", "X", ""},
		{"", "<html><title>Sniffed</title></html>", "Sniffed", ""},
		{"", "plain bytes", "", website.FailType},
		{"text/html", "<svg><title>Foreign</title></svg><title>Real</title>", "Real", ""},
		{"text/html", "<p>no title</p>", "", ""},
	} {
		k := checker(func(*http.Request) (*http.Response, error) { return response(200, tt.typ, tt.body), nil })
		pg := k.Get(context.Background(), "https://acme.test/about")
		if pg.Title != tt.title || pg.Error != tt.failure || pg.Size != int64(len(tt.body)) {
			t.Fatal(pg, tt)
		}
		if tt.failure == "" && (pg.Text != "" || pg.Reason != website.ReasonEmpty) {
			t.Fatal(pg)
		}
	}
	b := &countedBody{left: website.PageBytes + 1000}
	k := checker(func(*http.Request) (*http.Response, error) {
		r := response(200, "text/html", "")
		r.Body = b
		return r, nil
	})
	pg := k.Get(context.Background(), "https://acme.test/about")
	if pg.Error != "" || pg.Size != website.PageBytes || b.read != website.PageBytes {
		t.Fatal(pg, b.read)
	}
	for _, message := range append(failureCopy(), "") {
		k := checker(func(*http.Request) (*http.Response, error) {
			r := response(200, "text/html", "")
			r.Body = &failedBody{err: errors.New(message)}
			return r, nil
		})
		pg := k.Get(context.Background(), "https://acme.test/about")
		if pg.Title != "" || pg.Text != "" || pg.Error == "" || !strings.Contains(pg.Error, message) {
			t.Fatal(pg)
		}
		for _, reserved := range failureCopy() {
			if pg.Error == reserved {
				t.Fatal(pg)
			}
		}
	}
	calls := 0
	k = checker(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			resp := response(302, "", "")
			resp.Header.Set("Location", "next")
			return resp, nil
		}
		return response(200, "text/html", "<p>landed</p>"), nil
	})
	pg = k.Get(context.Background(), "https://acme.test/about/start")
	if pg.URL != "https://acme.test/about/next" || pg.Error != "" || calls != 2 {
		t.Fatal(pg, calls)
	}
}
