package cli_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/mnparcels"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// R-RJYQ-983R
func TestRunMNParcelsCounties(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{"unset", nil},
		{"empty", map[string]string{"MN_PARCELS_COUNTIES": ""}},
		{"single", map[string]string{"MN_PARCELS_COUNTIES": "St. Louis"}},
		{"ordered", map[string]string{"MN_PARCELS_COUNTIES": "Chisago,St. Louis"}},
		{"apostrophe", map[string]string{"MN_PARCELS_COUNTIES": "O'Brien"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("returnCountOnly") == "true" {
					return jsonResponse(r, map[string]any{"count": 1}), nil
				}
				return jsonResponse(r, map[string]any{"features": []any{map[string]any{"attributes": map[string]string{"state_pin": r.URL.Query().Get("where")}}}}), nil
			}}
			f := startRun(t, runOptions{Env: tc.env, Source: source})
			v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "mn_parcels"}))
			cfg, err := settings.Read(func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if err != nil {
				t.Fatal(err)
			}
			if done := f.awaitRun(v["id"].(string)); done["status"] != store.StatusDone || done["rows"] != float64(len(cfg.MNParcelsCounties)) {
				t.Fatal(done)
			}
			if tc.name == "unset" && len(cfg.MNParcelsCounties) != 13 {
				t.Fatal(cfg.MNParcelsCounties)
			}
			seen := map[string]bool{}
			for _, r := range source.Requests() {
				if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.String(), mnparcels.DefaultBaseURL+"?") || r.Header.Get("User-Agent") != pages.ServiceName+" ("+pages.Description+")" {
					t.Fatal(r)
				}
				seen[r.URL.Query().Get("where")] = true
			}
			if len(seen) != len(cfg.MNParcelsCounties) {
				t.Fatal(seen, cfg.MNParcelsCounties)
			}
			for _, county := range cfg.MNParcelsCounties {
				if !seen[mnparcels.Where(county)] {
					t.Fatal(county, seen)
				}
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("stop failed")
			}
		})
	}
}
