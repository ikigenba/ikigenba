package website_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func TestRealClientGuard(t *testing.T) {
	// R-I4VL-ETN1 R-I63H-SLDQ R-HML3-O9IM
	c := website.Client()
	defer c.CloseIdleConnections()
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatal("transport proxy")
	}
	for _, domain := range []string{"127.0.0.1", "0.0.0.0", "10.0.0.1", "169.254.1.1", "192.168.0.1"} {
		k := &website.Checker{Client: c, After: never}
		got := k.Check(context.Background(), domain)
		if got.State != sources.WebsiteNone || got.StatusCode != 0 || got.Error != website.FailRefused {
			t.Fatal(domain, got)
		}
	}
}

func TestStubFixtures(t *testing.T) {
	// R-HNT0-219B R-I7BE-6D4F R-I8JA-K4V4 R-I9R6-XWLT R-IAZ3-BOCI R-IC6Z-PG37 R-IDEW-37TW R-IEMS-GZKL R-IFUO-URBA R-IH2L-8J1Z R-IJIE-02JD R-IKQA-DUA2
	s, err := website.NewStub()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	k := &website.Checker{Client: s.Client(), After: never}
	tests := []struct {
		host, state, url, title, err string
		status                       int
		size                         int64
	}{
		{"secure.test", sources.WebsiteHTTPS, "https://secure.test", "secure.test", "", 200, -1},
		{"secure-2.test", sources.WebsiteHTTPS, "https://secure-2.test", "secure-2.test", "", 200, -1},
		{"secure-a.b.test", sources.WebsiteHTTPS, "https://secure-a.b.test", "secure-a.b.test", "", 200, -1},
		{"plain.test", sources.WebsiteHTTP, "http://plain.test", "plain.test", "", 200, -2},
		{"plain-2.test", sources.WebsiteHTTP, "http://plain-2.test", "plain-2.test", "", 200, -2},
		{"prefixed.test", sources.WebsiteHTTPS, "https://www.prefixed.test", "www.prefixed.test", "", 200, -2},
		{"upgrade.test", sources.WebsiteHTTPS, "https://upgrade.test/home", "upgrade.test", "", 200, -2},
		{"away.test", sources.WebsiteHTTPS, "https://secure.test/", "secure.test", "", 200, -2},
		{"missing.test", sources.WebsiteNone, "", "", website.FailStatus, 404, 0},
		{"empty.test", sources.WebsiteNone, "", "", website.FailEmpty, 200, 0},
		{"binary.test", sources.WebsiteNone, "", "", website.FailType, 200, 1024},
		{"loop.test", sources.WebsiteNone, "", "", website.FailRedirects, -1, -2},
		{"big.test", sources.WebsiteHTTPS, "https://big.test", "big.test", "", 200, website.BodyCap},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := k.Check(context.Background(), tt.host)
			if got.State != tt.state || got.URL != tt.url || got.Title != tt.title || got.Error != tt.err || (tt.status >= 0 && got.StatusCode != tt.status) || (tt.size >= 0 && got.Size != tt.size) || (tt.size == -1 && got.Size <= 0) {
				t.Fatalf("got %+v want %+v", got, tt)
			}
		})
	}
	for _, host := range []string{"refused.test", "example.com", "secure.example.com", "unlisted.test", "secureish.test"} {
		got := k.Check(context.Background(), host)
		if got.State != sources.WebsiteNone || got.StatusCode != 0 || got.Size != 0 || got.Error == "" {
			t.Fatal(host, got)
		}
		for _, s := range failureCopy() {
			if got.Error == s {
				t.Fatal(host, got)
			}
		}
	}
	k.After = func(time.Duration) <-chan time.Time { c := make(chan time.Time, 1); c <- time.Time{}; return c }
	if got := k.Check(context.Background(), "slow.test"); got.State != sources.WebsiteNone || got.StatusCode != 0 || got.Error != website.FailTimeout {
		t.Fatal(got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	k.After = never
	if got := k.Check(context.Background(), "secure.test"); got.State != sources.WebsiteNone || got.StatusCode != 0 {
		t.Fatal(got)
	}
}

func TestStubSilent(t *testing.T) {
	// R-Q7L0-M8H0
	var out bytes.Buffer
	readEnd, writeEnd, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	oldStderr := os.Stderr
	os.Stderr = writeEnd
	defer func() { os.Stderr = oldStderr; _ = writeEnd.Close(); _ = readEnd.Close() }()
	old := log.Writer()
	log.SetOutput(&out)
	defer log.SetOutput(old)
	s, err := website.NewStub()
	if err != nil {
		t.Fatal(err)
	}
	k := &website.Checker{Client: s.Client(), After: never}
	for _, host := range []string{"refused.test", "plain.test", "secure.test"} {
		k.Check(context.Background(), host)
	}
	k.After = func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- time.Time{}; return ch }
	k.Check(context.Background(), "slow.test")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = oldStderr
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatal(err)
	}
	if len(stderr) != 0 {
		t.Fatalf("stub stderr: %s", stderr)
	}
	if out.Len() != 0 {
		t.Fatal(out.String())
	}
}
