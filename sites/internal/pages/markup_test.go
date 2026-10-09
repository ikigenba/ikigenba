package pages_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
)

// R-WO6W-W0CL R-EJ1Z-H4BY
func TestLandingOmitsCatalogDetails(t *testing.T) {
	for _, populated := range []bool{false, true} {
		s := catalog(t)
		var secrets []string
		if populated {
			for _, x := range fixtureSites(t, s) {
				secrets = append(secrets, x.ID, x.Repo, x.Commit)
			}
		}
		for _, user := range []string{"alice", "bob", "other"} {
			body := answer(identity.Optional(pages.Handler(config(t, s))), request("GET", "/", user, "user@example.test")).Body.String()
			for _, secret := range secrets {
				if secret != "" && strings.Contains(body, secret) {
					t.Fatalf("catalog detail exposed: %s", secret)
				}
			}
		}
	}
}
