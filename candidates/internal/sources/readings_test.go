package sources_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func stringFacts(f map[string]string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range f {
		b, _ := json.Marshal(v)
		out[k] = b
	}
	return out
}

// R-2I3T-3JAX R-2JBP-HB1M R-2LRI-8UJ0 R-4W6D-T018
func TestRecordTradeNamesAndMailing(t *testing.T) {
	for _, names := range [][]string{nil, {"ACME TRUCKING LLC"}, {"ACME TRUCKING LLC", "ACME FREIGHT", "", "ACME FREIGHT"}} {
		row := scrapes.Row{Name: "DESIGN READY CONTROLS INC", Names: names, Facts: stringFacts(map[string]string{"establishment_name": "Design Ready Controls - BP8", "mail_street": "PO BOX 7", "mail_city": "RUSH CITY", "mail_state": "MN", "mail_zip": "55069"})}
		for _, source := range []sources.Source{fmcsa.New(), ppp.New(), oshaita.New()} {
			got := source.Read(row)
			want := []string{}
			if source.Name() == fmcsa.Name && len(names) > 1 {
				want = []string{"ACME FREIGHT", "ACME FREIGHT"}
			}
			if !reflect.DeepEqual(got.TradeNames, want) || got.Mailing || got.MailingAddress != nil {
				t.Fatalf("%s: %+v", source.Name(), got)
			}
			if len(got.TradeNames) > 0 {
				got.TradeNames[0] = "changed"
				if row.Names[1] != "ACME FREIGHT" {
					t.Fatal("shared row names")
				}
			}
		}
	}
}

// R-HAE6-U105
func TestForm5500TradeNamesAndMailing(t *testing.T) {
	for _, tc := range []struct {
		names         []string
		sponsor, plan string
		want          []string
	}{
		{[]string{"BROWN HAWK LLC DBA NORTHERN LIGHTS STEEL FAB"}, "BROWN HAWK LLC DBA NORTHERN LIGHTS STEEL FAB", "NORTHERN LIGHTS STEEL FABRICATION 401(K) PROFIT SHARING PLAN", []string{"NORTHERN LIGHTS STEEL FAB"}},
		{[]string{"HEGGIES PIZZA, LLC"}, "HEGGIES PIZZA, LLC", "HEGGIES SLICE OF THE PIE", []string{}},
		{[]string{"ACME PAVING INC"}, "", "Acme Paving, Inc. 401(k) Plan", []string{}},
		{[]string{"SMITH CO DBA ACME PAVING"}, "SMITH CO DBA ACME PAVING", "ACME PAVING 401K PLAN", []string{"ACME PAVING"}},
		{[]string{"SMITH CO", "SMITH TRUCKING", ""}, "", "", []string{"SMITH TRUCKING"}},
		{[]string{"SMITH CO", "", " ", "SMITH TRUCKING", "SMITH TRUCKING"}, "SMITH CO DBA SMITH FREIGHT", "SMITH HAULING 401K PLAN", []string{" ", "SMITH TRUCKING", "SMITH TRUCKING", "SMITH FREIGHT"}},
		{[]string{"SMITH TRUCKING"}, "SMITH CO DBA SMITH TRUCKING", "", []string{}},
		{[]string{"SMITH CO DBA SMITH CO"}, "SMITH CO DBA SMITH CO", "", []string{}},
		{[]string{"SMITH CO"}, "SMITH CO DBA !!!", "", []string{}},
		{nil, "", "SMITH HAULING 401K PLAN", []string{}},
		{nil, "", "", []string{}},
	} {
		facts := map[string]string{}
		if tc.sponsor != "" {
			facts["sponsor_name"] = tc.sponsor
		}
		if tc.plan != "" {
			facts["plan_name"] = tc.plan
		}
		r := scrapes.Row{Names: tc.names, Facts: stringFacts(facts)}
		got := form5500.New().Read(r)
		if !reflect.DeepEqual(got.TradeNames, tc.want) {
			t.Fatalf("%v %+v: %v want %v", tc.names, r.Facts, got.TradeNames, tc.want)
		}
	}
	for _, tc := range []struct {
		facts   map[string]string
		mailing bool
		address *sources.Address
	}{
		{nil, true, nil},
		{map[string]string{"loc_state": ""}, true, nil},
		{map[string]string{"mail_street": "PO BOX 7"}, true, nil},
		{map[string]string{"loc_state": "", "mail_street": "PO BOX 7"}, true, nil},
		{map[string]string{"loc_state": "MN"}, false, nil},
		{map[string]string{"loc_state": " "}, false, nil},
		{map[string]string{"loc_state": "MN", "mail_street": " PO BOX 7 ", "mail_city": " RUSH CITY ", "mail_state": " mn ", "mail_zip": " 55069-0007 "}, false, &sources.Address{Street: "PO BOX 7", City: "Rush City", State: "MN", Zip: "55069"}},
		{map[string]string{"loc_state": "MN", "mail_city": "NORTH ST. PAUL"}, false, &sources.Address{City: "North St. Paul"}},
		{map[string]string{"loc_state": "MN", "mail_city": "éCOLE-D'ÉTÉ", "mail_zip": "一二三四五六"}, false, &sources.Address{City: "École-D'Été", Zip: "一二三四五"}},
		{map[string]string{"loc_state": "MN", "mail_zip": " 123 "}, false, &sources.Address{Zip: "123"}},
		{map[string]string{"loc_state": "MN", "mail_street": " ", "mail_city": "", "mail_state": " ", "mail_zip": ""}, false, nil},
	} {
		got := form5500.New().Read(scrapes.Row{Facts: stringFacts(tc.facts)})
		if got.Mailing != tc.mailing || !reflect.DeepEqual(got.MailingAddress, tc.address) {
			t.Fatalf("%v gives mailing=%v address=%+v", tc.facts, got.Mailing, got.MailingAddress)
		}
	}
}
