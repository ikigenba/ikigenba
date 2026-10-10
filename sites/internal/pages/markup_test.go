package pages_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
)

// R-MIXD-7B8Z R-EJ1Z-H4BY
func TestLandingOmitsCatalogDetails(t *testing.T) {
	for _, populated := range []bool{false, true} {
		s := catalog(t)
		var secrets []string
		if populated {
			for _, x := range fixtureSites(t, s) {
				secrets = append(secrets, x.ID, x.Repo, x.Commit)
			}
		}
		cfg := config(t, s)
		cfg.Banner = func(u page.User) page.Banner {
			b := banner(u)
			b.Service = "plain-pages"
			b.Release = "plain-release+local"
			b.Commit = "plain-commit"
			b.Icon = ""
			b.Home = "https://space.example.test/"
			return b
		}
		h := identity.Optional(pages.Handler(cfg))
		for _, user := range []string{"alice", "bob", "other"} {
			w := answer(h, request("GET", "/", user, "user@example.test"))
			if w.Code != 200 {
				t.Fatalf("plain landing: status %d", w.Code)
			}
			body := w.Body.String()
			for _, secret := range secrets {
				if secret != "" && strings.Contains(body, secret) {
					t.Fatalf("catalog detail exposed: %s", secret)
				}
			}
		}
	}
}
