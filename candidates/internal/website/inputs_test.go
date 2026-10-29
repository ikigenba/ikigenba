package website_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-W3JY-9HST R-LWDG-BRJU R-W5ZR-11A7 R-LTXN-K82G
func TestOfferedInputsAndElements(t *testing.T) {
	inputs := website.Inputs
	urls := website.URLs
	for _, tc := range []struct{ site, email, want string }{
		{"", "Ops@Acme-Trucking.COM.", "acme-trucking.com"},
		{" https://www.acme.test/ ", "ops@acme.test", "https://www.acme.test/\nacme.test"},
		{"http://acme.test", "joe@gmail.com", "http://acme.test"},
		{"acme.test", "", ""}, {"ftp://acme.test/", "", ""}, {"https://www./", "", ""},
		{"http://203.0.113.5/", "", ""}, {"https://[2001:db8::1]:8443/", "", ""},
		{"https://acme.test:8443", "", "https://acme.test:8443"},
	} {
		if got := inputs(tc.site, tc.email); got != tc.want {
			t.Fatalf("Inputs(%q,%q)=%q want %q", tc.site, tc.email, got, tc.want)
		}
	}
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", []string{}}, {"https://acme.test/\nacme.test", []string{"https://acme.test/", "acme.test"}},
		{"acme.test", []string{"acme.test"}}, {"https://acme.test/x", []string{"https://acme.test/x"}},
		{"b\nc", []string{"b\nc"}}, {"https://acme.test/\n", []string{"https://acme.test/\n"}},
		{"https://acme.test/\na\nb", []string{"https://acme.test/", "a\nb"}},
	} {
		if got := urls(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("URLs(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// R-LTXN-K82G R-LYT9-3B18 R-6M4X-FYPE R-6NCT-TQG3 R-6ETJ-5C98
func TestOfferedAttemptsAndMisses(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"http://www.Acme.test/home?x=1#top", []string{"https://acme.test/home?x=1", "https://www.acme.test/home?x=1", "http://acme.test/home?x=1", "http://www.acme.test/home?x=1"}},
		{"https://acme.test:8443", []string{"https://acme.test:8443", "https://www.acme.test:8443", "http://acme.test:8443", "http://www.acme.test:8443"}},
		{"https://acme.test/a%2Fb?q=x", []string{"https://acme.test/a%2Fb?q=x", "https://www.acme.test/a%2Fb?q=x", "http://acme.test/a%2Fb?q=x", "http://www.acme.test/a%2Fb?q=x"}},
		{"https://shop.test/\nacme.test", []string{"https://shop.test/", "https://www.shop.test/", "http://shop.test/", "http://www.shop.test/", "https://acme.test", "https://www.acme.test", "http://acme.test", "http://www.acme.test"}},
	} {
		for _, success := range []int{-1, 0, len(tc.want) - 1} {
			requests := []string{}
			waits := []time.Duration{}
			k := checker(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.String())
				if r.Method != http.MethodGet || r.Header.Get("User-Agent") != website.UserAgent {
					t.Error("request metadata")
				}
				if len(requests)-1 == success {
					return response(200, "text/html", "<title>Winner</title>"), nil
				}
				return response(404, "text/html", ""), nil
			})
			k.After = func(d time.Duration) <-chan time.Time { waits = append(waits, d); return never(d) }
			got := k.Check(context.Background(), tc.in)
			n := len(tc.want)
			if success >= 0 {
				n = success + 1
			}
			if !reflect.DeepEqual(requests, tc.want[:n]) || len(waits) != n {
				t.Fatalf("requests=%q waits=%v", requests, waits)
			}
			for _, d := range waits {
				if d != website.Timeout {
					t.Fatal(d)
				}
			}
			elements := website.URLs(tc.in)
			missCount := len(elements)
			if success >= 0 {
				missCount = success / 4
			}
			if len(got.Misses) != missCount {
				t.Fatal(got)
			}
			for i, m := range got.Misses {
				if m != (website.Miss{elements[i], 404, 0, website.FailStatus}) {
					t.Fatal(m)
				}
			}
			if success < 0 {
				if got.State != sources.WebsiteNone || got.From != "" || got.URL != "" || got.Title != "" || got.StatusCode != 404 || got.Size != 0 || got.Error != website.FailStatus {
					t.Fatal(got)
				}
			} else {
				scheme := "https"
				if strings.HasPrefix(tc.want[success], "http:") {
					scheme = "http"
				}
				if got.State != scheme || got.From != elements[success/4] || got.URL != tc.want[success] || got.Title != "Winner" || got.StatusCode != 200 || got.Size != int64(len("<title>Winner</title>")) || got.Error != "" {
					t.Fatal(got)
				}
				if missCount == 0 && got.Misses != nil {
					t.Fatal("success without earlier miss must retain nil misses")
				}
			}
		}
	}
	k := checker(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Host, "shop.test") {
			return response(404, "text/html", ""), nil
		}
		return nil, errors.New("boom")
	})
	got := k.Check(context.Background(), "https://shop.test/\nacme.test")
	if got.StatusCode != 0 || got.Size != 0 || !strings.Contains(got.Error, "boom") || len(got.Misses) != 2 || got.Misses[0].Error != website.FailStatus || got.Misses[1].Error != got.Error {
		t.Fatal(got)
	}
}

// R-H7NN-NKHB R-W3JY-9HST R-W5ZR-11A7
func TestOwnerInputsAndMultipleElements(t *testing.T) {
	ownerInputs := website.OwnerInputs
	for _, tc := range []struct{ owner, site, email, want string }{
		{"", " https://www.acme.test/ ", "ops@acme.test", "https://www.acme.test/\nacme.test"},
		{" http://owner-co.test/ ", "https://places.test/", "ops@acme.test", "http://owner-co.test/\nhttps://places.test/\nacme.test"},
		{"http://owner-co.test/", "", "", "http://owner-co.test/"},
		{"https://places.test/", " https://places.test/ ", "ops@acme.test", "https://places.test/\nacme.test"},
		{"http://acme.test/", "https://acme.test/", "", "http://acme.test/\nhttps://acme.test/"},
	} {
		if got := ownerInputs(tc.owner, tc.site, tc.email); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"http://owner-co.test/\nhttps://places.test/\nacme.test", []string{"http://owner-co.test/", "https://places.test/", "acme.test"}},
		{"http://owner-co.test/\nhttps://places.test/\n", []string{"http://owner-co.test/", "https://places.test/\n"}},
		{"http://owner-co.test/\na\nb", []string{"http://owner-co.test/", "a\nb"}},
	} {
		if got := website.URLs(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got, tc.want)
		}
	}
}
