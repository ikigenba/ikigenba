package source

import (
	"encoding/json"
	"testing"
	"time"
)

// R-0NPW-FSB4
func TestOrderedEnvelopeFields(t *testing.T) {
	a := Address{"street", "city", "state", "zip", "county"}
	p := Person{"party", "role"}
	lat, lon := 1.0, 2.0
	collected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	facts, raw := json.RawMessage(`{"fact":true}`), json.RawMessage(`{"record":true}`)
	r := Row{"scraper", "run", "id", collected, "digest", "name", []string{"alias"}, a, &lat, &lon, []Person{p}, facts, raw}
	if a.Street != "street" || a.City != "city" || a.State != "state" || a.Zip != "zip" || a.County != "county" || p.Name != "party" || p.Role != "role" || r.Scraper != "scraper" || r.Run != "run" || r.SourceID != "id" || r.Collected != collected || r.Digest != "digest" || r.Name != "name" || r.Names[0] != "alias" || r.Address != a || r.Lat != &lat || r.Lon != &lon || r.People[0] != p || string(r.Facts) != string(facts) || string(r.Raw) != string(raw) {
		t.Fatal(r)
	}
}
