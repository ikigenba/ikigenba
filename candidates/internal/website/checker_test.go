package website_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, typ, body string) *http.Response {
	h := make(http.Header)
	if typ != "" {
		h.Set("Content-Type", typ)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}
func never(time.Duration) <-chan time.Time { return make(chan time.Time) }
func instant(d time.Duration) <-chan time.Time {
	if d == website.Interval {
		c := make(chan time.Time, 1)
		c <- time.Time{}
		return c
	}
	return never(d)
}
func checker(f transport) *website.Checker {
	return &website.Checker{Client: &http.Client{Transport: f}, After: never}
}

func await[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	select {
	case v := <-c:
		return v
	case <-deadline.C:
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

func TestVocabularyAndDomain(t *testing.T) {
	type tuning int64
	var untyped = []tuning{website.Concurrency, website.Interval, website.Timeout, website.BodyCap, website.Redirects}
	if len(untyped) != 5 {
		t.Fatal(untyped)
	}

	// R-MPP3-S26X R-HHPI-56JU R-HIXE-IYAJ R-HML3-O9IM R-PXTT-K2JG
	if website.Concurrency != 8 || website.Interval != 500*time.Millisecond || website.Timeout != 10*time.Second || website.BodyCap != 262144 || website.Redirects != 5 {
		t.Fatal("check tuning")
	}
	if website.UserAgent == "" || strings.Contains(website.UserAgent, "\n") {
		t.Fatal("user agent")
	}
	seen := map[string]bool{}
	for _, s := range failureCopy() {
		if s == "" || strings.Contains(s, "\n") || seen[s] {
			t.Fatal("failure copy", s)
		}
		seen[s] = true
	}
	for input, want := range map[string]string{"Ops@Acme-Trucking.COM.": "acme-trucking.com", "ops@acme.com..": "acme.com", "joe@gmail.com": "", "joe@gmail.com.": "", "joe@gmail.com..": "", "x@.": "", "nobody": ""} {
		if got := website.Domain(input); got != want {
			t.Errorf("Domain(%q)=%q want %q", input, got, want)
		}
	}
}
func failureCopy() []string {
	return []string{website.FailStatus, website.FailEmpty, website.FailType, website.FailTimeout, website.FailRedirects, website.FailRefused}
}

func TestAttemptsAndResult(t *testing.T) {
	// R-6DLM-RKIJ R-HQ8S-TKQP R-LYT9-3B18 R-6NCT-TQG3 R-6M4X-FYPE
	urls := []string{"https://acme.test", "https://www.acme.test", "http://acme.test", "http://www.acme.test"}
	for success := -1; success < 4; success++ {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			var requests []string
			var waits []time.Duration
			k := checker(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.String())
				if r.Method != http.MethodGet || r.Header.Get("User-Agent") != website.UserAgent {
					t.Error("request method or user agent")
				}
				if len(requests)-1 == success {
					return response(200, "text/html", "<title> own title </title>"), nil
				}
				return response(404, "text/html", "ignored"), nil
			})
			k.After = func(d time.Duration) <-chan time.Time { waits = append(waits, d); return never(d) }
			got := k.Check(context.Background(), "acme.test")
			n := 4
			if success >= 0 {
				n = success + 1
			}
			if !reflect.DeepEqual(requests, urls[:n]) || len(waits) != n {
				t.Fatalf("requests %v waits %v", requests, waits)
			}
			for _, d := range waits {
				if d != website.Timeout {
					t.Error("unexpected wait", d)
				}
			}
			want := website.Result{State: sources.WebsiteNone, StatusCode: 404, Error: website.FailStatus, Misses: []website.Miss{{From: "acme.test", StatusCode: 404, Error: website.FailStatus}}}
			if success >= 0 {
				scheme := "https"
				if success >= 2 {
					scheme = "http"
				}
				want = website.Result{From: "acme.test", State: scheme, URL: urls[success], Title: "own title", StatusCode: 200, Size: int64(len("<title> own title </title>"))}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v want %+v", got, want)
			}
		})
	}
}

type countedBody struct {
	left int
	read int
}

func (b *countedBody) Read(p []byte) (int, error) {
	if b.left == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.left)
	for i := range p[:n] {
		p[i] = 'x'
	}
	b.left -= n
	b.read += n
	return n, nil
}
func (*countedBody) Close() error { return nil }

type failedBody struct {
	sent bool
	err  error
}

func (b *failedBody) Read(p []byte) (int, error) {
	if b.sent {
		if b.err != nil {
			return 0, b.err
		}
		return 0, errors.New("body broke")
	}
	b.sent = true
	return copy(p, "<title>partial</title>"), nil
}
func (*failedBody) Close() error { return nil }

func TestBodyClassification(t *testing.T) {
	// R-HTWH-YVYS R-6NCT-TQG3 R-6M4X-FYPE R-HQ8S-TKQP
	tests := []struct {
		typ, body, err string
		status         int
		size           int64
	}{
		{"text/html; charset=utf-8", "<title>A</title>", "", 200, 16},
		{"application/xhtml+xml", "<title>A</title>", "", 200, 16},
		{"", "<html><title>A</title></html>", "", 200, 29},
		{"", "plain", website.FailType, 200, 5},
		{"text/html", "", website.FailEmpty, 200, 0},
		{"application/octet-stream", strings.Repeat("b", 1024), website.FailType, 200, 1024},
		{"text/html", "bad", website.FailStatus, 503, 0},
		{"malformed", "<title>A</title>", website.FailType, 200, 16},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s-%d-%d", tt.typ, tt.status, tt.size), func(t *testing.T) {
			k := checker(func(*http.Request) (*http.Response, error) { return response(tt.status, tt.typ, tt.body), nil })
			got := k.Check(context.Background(), "acme.test")
			if got.StatusCode != tt.status || got.Size != tt.size || got.Error != tt.err {
				t.Fatalf("%+v", got)
			}
			if tt.err != "" && (got.State != sources.WebsiteNone || got.URL != "" || got.Title != "") {
				t.Fatal(got)
			}
		})
	}
	t.Run("cap", func(t *testing.T) {
		b := &countedBody{left: 300000}
		k := checker(func(*http.Request) (*http.Response, error) {
			r := response(200, "text/html", "")
			r.Body = b
			return r, nil
		})
		got := k.Check(context.Background(), "acme.test")
		if got.Size != website.BodyCap || b.read != website.BodyCap || got.Error != "" {
			t.Fatal(got, b.read)
		}
	})
	for _, bodyErr := range []bool{false, true} {
		k := checker(func(*http.Request) (*http.Response, error) {
			if !bodyErr {
				return nil, errors.New("transport broke")
			}
			r := response(200, "text/html", "")
			r.Body = &failedBody{}
			return r, nil
		})
		got := k.Check(context.Background(), "acme.test")
		want := "transport broke"
		if bodyErr {
			want = "body broke"
		}
		if got.State != sources.WebsiteNone || !strings.Contains(got.Error, want) {
			t.Fatal(got)
		}
		for _, s := range failureCopy() {
			if got.Error == s {
				t.Fatal(got)
			}
		}
	}
}

func TestTitleParsing(t *testing.T) {
	// R-HYS3-HYXK
	for body, want := range map[string]string{
		"<html><head><title>  Acme Hauling\n</title>": "Acme Hauling",
		"<p>no title</p>": "",
		"<svg><title>x</title></svg><title>Real</title>":              "Real",
		"<title> first &amp; &lt;b&gt; </title><title>second</title>": "first & <b>",
		"<title> \xff\xfeone\n </title>":                              "�one",
		"<!-- <title>wrong</title> --><TITLE>Right</TITLE>":           "Right",
	} {
		k := checker(func(*http.Request) (*http.Response, error) { return response(200, "text/html", body), nil })
		if got := k.Check(context.Background(), "acme.test"); got.Title != want {
			t.Errorf("%q title=%q want %q", body, got.Title, want)
		}
	}
}

func TestRedirects(t *testing.T) {
	// R-HSOL-L483 R-6NCT-TQG3 R-6M4X-FYPE
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var urls []string
			k := checker(func(r *http.Request) (*http.Response, error) {
				urls = append(urls, r.URL.String())
				if r.URL.Path != "/home" {
					resp := response(status, "text/html", "")
					resp.Header.Set("Location", "/home")
					return resp, nil
				}
				return response(200, "text/html", "<title>landed</title>"), nil
			})
			got := k.Check(context.Background(), "acme.test")
			if got.URL != "https://acme.test/home" || got.State != sources.WebsiteHTTPS || got.Title != "landed" || got.StatusCode != 200 || len(urls) != 2 {
				t.Fatal(got, urls)
			}
		})
	}
	for _, location := range []string{"https://other.test/home", "https://acme.test/home"} {
		k := checker(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() == "http://acme.test" {
				resp := response(301, "", "")
				resp.Header.Set("Location", location)
				return resp, nil
			}
			if r.URL.String() == location {
				return response(200, "text/html", "<title>upgraded</title>"), nil
			}
			return response(404, "", ""), nil
		})
		got := k.Check(context.Background(), "acme.test")
		if got.URL != location || got.State != sources.WebsiteHTTPS || got.Error != "" {
			t.Fatal(got)
		}
	}
	for _, location := range []string{"ftp://outside.test/file", "mailto:somebody@example.test", "/"} {
		count := 0
		k := checker(func(*http.Request) (*http.Response, error) {
			count++
			resp := response(302, "", "")
			resp.Header.Set("Location", location)
			return resp, nil
		})
		got := k.Check(context.Background(), "acme.test")
		want := website.FailRefused
		n := 4
		if location == "/" {
			want = website.FailRedirects
			n = 4 * (website.Redirects + 1)
		}
		if got.Error != want || count != n || got.StatusCode != 302 || got.State != sources.WebsiteNone {
			t.Fatal(got, count)
		}
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	// R-HTWH-YVYS R-HZZZ-VQO9
	timers := make(chan chan time.Time, 4)
	requests := make(chan *http.Request, 4)
	k := checker(func(r *http.Request) (*http.Response, error) {
		requests <- r
		await(t, r.Context().Done())
		return nil, r.Context().Err()
	})
	k.After = func(d time.Duration) <-chan time.Time {
		if d != website.Timeout {
			t.Error(d)
		}
		c := make(chan time.Time, 1)
		timers <- c
		return c
	}
	result := make(chan website.Result, 1)
	go func() { result <- k.Check(context.Background(), "acme.test") }()
	for range 4 {
		timer := await(t, timers)
		r := await(t, requests)
		timer <- time.Time{}
		await(t, r.Context().Done())
	}
	got := await(t, result)
	if got.State != sources.WebsiteNone || got.StatusCode != 0 || got.Error != website.FailTimeout {
		t.Fatal(got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k.After = never
	go func() { result <- k.Check(ctx, "acme.test") }()
	r := await(t, requests)
	cancel()
	await(t, r.Context().Done())
	await(t, result)
	select {
	case r := <-requests:
		t.Fatal("request after cancellation", r.URL)
	default:
	}
}

func TestCheckAllConcurrency(t *testing.T) {
	// R-PZ1P-XUA5
	const n = 10
	arrivals := make(chan string, n)
	release := make(chan struct{})
	callbackRelease := make(chan struct{})
	var active, peak atomic.Int64
	k := checker(func(r *http.Request) (*http.Response, error) {
		v := active.Add(1)
		for old := peak.Load(); v > old; old = peak.Load() {
			if peak.CompareAndSwap(old, v) {
				break
			}
		}
		arrivals <- r.URL.Host
		<-release
		active.Add(-1)
		return response(200, "text/html", "<title>result</title>"), nil
	})
	k.After = instant
	domains := make([]string, n)
	for i := range domains {
		domains[i] = fmt.Sprintf("domain-%d.test", i)
	}
	var mu sync.Mutex
	calls := map[string]website.Result{}
	callbacks := make(chan struct{}, n)
	done := make(chan struct{})
	go func() {
		k.CheckAll(context.Background(), domains, func(d string, r website.Result) {
			mu.Lock()
			calls[d] = r
			mu.Unlock()
			callbacks <- struct{}{}
			<-callbackRelease
		})
		close(done)
	}()
	for range website.Concurrency {
		await(t, arrivals)
	}
	if active.Load() != website.Concurrency {
		t.Fatal(active.Load())
	}
	close(release)
	for range website.Concurrency {
		await(t, callbacks)
	}
	select {
	case <-done:
		t.Fatal("returned before callbacks completed")
	default:
	}
	close(callbackRelease)
	await(t, done)
	if peak.Load() > website.Concurrency || len(calls) != n {
		t.Fatal(peak.Load(), len(calls))
	}
	for _, d := range domains {
		r := calls[d]
		if r.State != sources.WebsiteHTTPS || r.URL != "https://"+d || r.Title != "result" || r.Error != "" {
			t.Fatal(d, r)
		}
	}
}

func TestCheckAllPacingAndCancellation(t *testing.T) {
	// R-I2FS-NA5N R-I3NP-11WC
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			arrivals := make(chan string, 2)
			intervals := make(chan chan time.Time, 2)
			done := make(chan struct{})
			var count atomic.Int64
			k := checker(func(r *http.Request) (*http.Response, error) {
				arrivals <- r.URL.Host
				return response(200, "text/html", "ok"), nil
			})
			k.After = func(d time.Duration) <-chan time.Time {
				if d == website.Timeout {
					return never(d)
				}
				if d != website.Interval {
					t.Error(d)
				}
				ch := make(chan time.Time, 1)
				intervals <- ch
				return ch
			}
			go func() {
				k.CheckAll(ctx, []string{"a.test", "b.test"}, func(string, website.Result) { count.Add(1) })
				close(done)
			}()
			if got := await(t, arrivals); got != "a.test" {
				t.Fatal(got)
			}
			gate := await(t, intervals)
			select {
			case s := <-arrivals:
				t.Fatal("unpaced", s)
			default:
			}
			if cancelled {
				cancel()
			} else {
				gate <- time.Time{}
				if got := await(t, arrivals); got != "b.test" {
					t.Fatal(got)
				}
			}
			await(t, done)
			if !cancelled && count.Load() != 2 {
				t.Fatal(count.Load())
			}
		})
	}
	// Cancellation while a check is in flight must join it and suppress its callback.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	released := make(chan struct{})
	done := make(chan struct{})
	var callbacks atomic.Int64
	var requests atomic.Int64
	k := checker(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		close(started)
		await(t, r.Context().Done())
		<-released
		return nil, r.Context().Err()
	})
	go func() {
		k.CheckAll(ctx, []string{"a.test", "b.test"}, func(string, website.Result) { callbacks.Add(1) })
		close(done)
	}()
	await(t, started)
	cancel()
	select {
	case <-done:
		t.Fatal("returned before transport")
	default:
	}
	close(released)
	await(t, done)
	if callbacks.Load() != 0 || requests.Load() != 1 {
		t.Fatal(callbacks.Load(), requests.Load())
	}
}

func TestTransportErrorCollisions(t *testing.T) {
	// R-HTWH-YVYS
	messages := append(failureCopy(), "")
	for _, message := range messages {
		for _, bodyError := range []bool{false, true} {
			t.Run(fmt.Sprintf("body=%t/error=%q", bodyError, message), func(t *testing.T) {
				k := checker(func(*http.Request) (*http.Response, error) {
					err := errors.New(message)
					if !bodyError {
						return nil, err
					}
					r := response(http.StatusOK, "text/html", "")
					r.Body = &failedBody{err: err}
					return r, nil
				})
				got := k.Check(context.Background(), "acme.test")
				if got.State != sources.WebsiteNone || got.Error == "" || !strings.Contains(got.Error, message) {
					t.Fatal(got)
				}
				for _, reserved := range failureCopy() {
					if got.Error == reserved {
						t.Fatalf("transport error classified as reserved failure: %+v", got)
					}
				}
			})
		}
	}
}
