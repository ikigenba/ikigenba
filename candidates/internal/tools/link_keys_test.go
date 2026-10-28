package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-5VE3-TVI9
func TestCandidateLinkEIN(t *testing.T) {
	for _, ein := range []string{"123456789", ""} {
		t.Run(ein, func(t *testing.T) {
			f := newFixture(t)
			_, ids, err := f.st.Attach(t.Context(), "chosen-run", false, []store.Observation{{Scraper: "fixture", SourceID: "chosen-row", Digest: "chosen-digest", Reading: sources.Reading{Name: "chosen-business", EINKey: ein}}})
			if err != nil || len(ids) != 1 {
				t.Fatal(ids, err)
			}
			response := f.call("candidate", tools.CandidateArgs{Candidate: ids[0]})
			result := decodeList[tools.Detail](t, response)
			if len(result.Links) != 1 || result.Links[0].EIN != ein {
				t.Fatal(result)
			}
			var answer struct {
				Links []map[string]json.RawMessage `json:"links"`
			}
			if err = json.Unmarshal(decode(t, response).Structured, &answer); err != nil || len(answer.Links) != 1 || string(answer.Links[0]["ein"]) != `"`+ein+`"` {
				t.Fatal(answer, err)
			}
		})
	}
}

// R-R4MF-9E6P
func TestLinkPositionalShape(t *testing.T) {
	v := tools.Link{"scraper", "source", "run", "digest", "name", []string{"alias"}, "street", "city", "state", "zip", "county", "phone", "email", []tools.Person{{Name: "person", Role: "role"}}, "class", []string{"cargo"}, "filed", "fleet", "name-key", "phone-key", "domain-key", "ein", "establishment", new(int64(2024)), new(int64(23)), new(int64(47840)), "reason", .75, "first", "last", true, "walk", "park"}
	if v.Scraper != "scraper" || v.SourceID != "source" || v.Run != "run" || v.Digest != "digest" || v.Name != "name" || v.Names[0] != "alias" || v.Street != "street" || v.City != "city" || v.State != "state" || v.Zip != "zip" || v.County != "county" || v.Phone != "phone" || v.Email != "email" || v.People[0].Name != "person" || v.Class != "class" || v.Cargo[0] != "cargo" || v.Filed != "filed" || v.FleetVerdict != "fleet" || v.NameKey != "name-key" || v.PhoneKey != "phone-key" || v.DomainKey != "domain-key" || v.EIN != "ein" || v.Establishment != "establishment" || *v.Year != 2024 || *v.Employees != 23 || *v.Hours != 47840 || v.Reason != "reason" || v.Confidence != .75 || v.FirstSeen != "first" || v.LastSeen != "last" || !v.Gone || v.Walk != "walk" || v.Park != "park" {
		t.Fatal(v)
	}
}
