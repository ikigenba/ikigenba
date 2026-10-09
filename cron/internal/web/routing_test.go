package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
)

func assertNoRedirect(t *testing.T, got *httptest.ResponseRecorder) {
	t.Helper()
	switch got.Code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		t.Fatalf("redirect status %d", got.Code)
	}
	if len(got.Header().Values("Location")) != 0 {
		t.Fatalf("Location header sent: %v", got.Header().Values("Location"))
	}
}

// R-YKGQ-6F0V
func TestRoutingUsesDecodedPathWithoutRedirects(t *testing.T) {
	f := setup(t)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Scheduler: f.cfg.Scheduler, MCP: f.cfg.MCP})
	static := page.Static()
	delivery := events.DeliveryHandler(nil)
	declarations := events.DeclarationsHandler(f.cfg.Events, nil)
	for _, tc := range []struct {
		path string
		h    http.Handler
	}{
		{"/", ph}, {"/about", ph}, {"/%61bout", ph}, {"/tools", ph}, {"/%74ools", ph},
		{"/nope", ph}, {"/about/", ph}, {"/tools/", ph}, {"/mcp/", ph},
		{"/events/", ph}, {"/declarations/", ph}, {"/_appkit", ph},
		{"//", ph}, {"/a/../about", ph}, {"/About", ph},
		{"/_appkit/theme.css", static}, {"/%5fappkit/theme.css", static},
		{"/_appkit/", static}, {"/_appkit/theme.css/", static},
		{"/events", delivery}, {"/%65vents", delivery},
		{"/declarations", declarations}, {"/%64eclarations", declarations},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
				for _, user := range []string{"", "signed_in"} {
					for _, conditional := range []bool{false, true} {
						r := request(method, tc.path+"?path=/mcp&target=/about&next=/_appkit/theme.css", user, "routing")
						r.Host = "backend.example.test"
						for _, header := range []string{"X-Original-URL", "X-Rewrite-URL", "X-Forwarded-Uri", "X-Forwarded-Host", "Referer", "Location"} {
							r.Header.Set(header, "https://other.example.test/mcp")
						}
						if conditional {
							r.Header.Set("If-None-Match", "*")
						}
						expected := tc.h
						if r.URL.Path != "/events" && r.URL.Path != "/declarations" {
							expected = identity.Require(expected)
						}
						got := answer(f.h, r)
						assertNoRedirect(t, got)
						sameAnswer(t, got, answer(expected, r.Clone(f.ctx)))
					}
				}
			}
		})
	}
	// Discovery is an MCP request appkit's client cannot send.
	for _, path := range []string{"/mcp", "/%6dcp"} {
		for _, user := range []string{"", "signed_in"} {
			r := request("POST", path+"?path=/about&target=/events", user, "discovery")
			r.Host = "backend.example.test"
			r.Header.Set("X-Original-URL", "/about")
			r.Header.Set("X-Rewrite-URL", "/events")
			r.Header.Set("Content-Type", "application/json")
			body := `{"jsonrpc":"2.0","id":1,"method":"server/discover"}`
			gotRequest := r.Clone(f.ctx)
			gotRequest.Body = io.NopCloser(strings.NewReader(body))
			r.Body = io.NopCloser(strings.NewReader(body))
			got := answer(f.h, gotRequest)
			assertNoRedirect(t, got)
			sameAnswer(t, got, answer(identity.Require(f.cfg.MCP), r))
		}
	}
}
