package pages_test

import (
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

func preloadHead(t *testing.T, m string) {
	t.Helper()
	var links []tag
	for _, x := range tags(m, "link") {
		if x.has("rel", "preload") {
			links = append(links, x)
		}
	}
	link := one(t, links, "link")
	if !link.has("as", "font") || !link.has("type", "font/woff2") || !link.has("href", page.PreloadURL()) || !link.bare("crossorigin") && !link.has("crossorigin", "") {
		t.Fatalf("font preload attributes: %s", link.raw)
	}
	bodies := tags(m, "body")
	if len(bodies) == 0 || link.start >= bodies[0].start {
		t.Fatal("font preload not before first body")
	}
}

// R-85TU-KI56
func TestPlainPagesPreloadSharedFont(t *testing.T) {
	cfg := config(t, catalog(t))
	h := identity.Optional(pages.Handler(cfg))
	for _, path := range []string{"/", "/about"} {
		r := request("GET", path, "alice", "alice@example.test")
		b := banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, ""), LogoutURL: urls.AuthLogout(r, "")})
		preloadHead(t, written(t, answer(h, r).Body.String(), b))
	}
}

// R-889N-C1MK
func TestNoticePreloadSharedFont(t *testing.T) {
	s := load(t)
	b := page.Banner{Service: "notice-sites", Version: "test.build+local"}
	for _, name := range []string{"notfound", "unavailable"} {
		w := answer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.Write(w, r, http.StatusServiceUnavailable, name, pages.NoticeData{Banner: b})
		}), request("GET", "/", "", ""))
		preloadHead(t, w.Body.String())
	}
}
