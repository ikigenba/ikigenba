package cli_test

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/mnparcels"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// R-6ETF-W4WI
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
			cfg, err := settings.Read(func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if err != nil {
				t.Fatal(err)
			}
			source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
				query := r.URL.Query()
				if query.Has("returnCountOnly") {
					return jsonResponse(r, map[string]any{"count": 1}), nil
				}
				for _, county := range cfg.MNParcelsCounties {
					prefix := mnparcels.Where(county) + " AND objectid>"
					if strings.HasPrefix(query.Get("where"), prefix) {
						bound, err := strconv.ParseInt(strings.TrimPrefix(query.Get("where"), prefix), 10, 64)
						if err != nil || bound == 1<<63-1 {
							t.Fatalf("invalid page bound: %s", query.Get("where"))
						}
						return jsonResponse(r, map[string]any{"features": []any{map[string]any{"attributes": map[string]any{"objectid": bound + 1, "state_pin": county}}}}), nil
					}
				}
				t.Fatalf("unexpected parcel request: %s", r.URL)
				return nil, nil
			}}
			f := startRun(t, runOptions{Env: tc.env, Source: source})
			v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "mn_parcels"}))
			if done := f.awaitRun(v["id"].(string)); done["status"] != store.StatusDone {
				t.Fatal(done)
			}
			if tc.name == "unset" && !reflect.DeepEqual(cfg.MNParcelsCounties, settings.Defaults().MNParcelsCounties) {
				t.Fatal(cfg.MNParcelsCounties)
			}
			seen := map[string]bool{}
			for _, r := range source.Requests() {
				if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.String(), mnparcels.DefaultBaseURL+"?") || r.Header.Get("User-Agent") != pages.ServiceName+" ("+pages.Description+")" {
					t.Fatal(r)
				}
				query := r.URL.Query()
				where := query.Get("where")
				matched := false
				for _, county := range cfg.MNParcelsCounties {
					if query.Has("returnCountOnly") {
						matched = where == mnparcels.Where(county)
					} else {
						prefix := mnparcels.Where(county) + " AND objectid>"
						if strings.HasPrefix(where, prefix) {
							bound, err := strconv.ParseInt(strings.TrimPrefix(where, prefix), 10, 64)
							matched = err == nil && where == prefix+strconv.FormatInt(bound, 10)
						}
					}
					if matched {
						seen[county] = true
						break
					}
				}
				if !matched {
					t.Fatalf("unexpected where: %s", where)
				}
			}
			for _, county := range cfg.MNParcelsCounties {
				if !seen[county] {
					t.Fatal(county, seen)
				}
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("stop failed")
			}
		})
	}
}
