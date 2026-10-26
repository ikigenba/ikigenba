package tools

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-E7M8-LZQG R-E8U4-ZRH5 R-EA21-DJ7U R-D4PJ-DV3L
func TestHeadcountAnswerShapesAndEncoding(t *testing.T) {
	// Conversions enforce the complete ordered public shape by use.
	_ = struct {
		Candidate           Candidate `json:"candidate"`
		Basis               string    `json:"basis"`
		OperatingConfidence *float64  `json:"operating_confidence,omitempty"`
		RevenueLow          *int64    `json:"revenue_low,omitempty"`
		RevenueHigh         *int64    `json:"revenue_high,omitempty"`
		RevenueConfidence   *float64  `json:"revenue_confidence,omitempty"`
		HeadcountLow        *int64    `json:"headcount_low,omitempty"`
		Categories          []string  `json:"categories"`
		ClassifiedBy        string    `json:"classified_by"`
		FleetVerdict        string    `json:"fleet_verdict"`
		Website             string    `json:"website"`
		WebsiteURL          string    `json:"website_url,omitempty"`
		WebsiteSource       string    `json:"website_source"`
		SiteText            *string   `json:"site_text"`
		Brief               *string   `json:"brief"`
		Minutes             *int64    `json:"minutes,omitempty"`
		MinutesSource       string    `json:"minutes_source"`
		GovernmentMatch     string    `json:"government_match,omitempty"`
		KnownSince          string    `json:"known_since,omitempty"`
		NewestFiling        string    `json:"newest_filing,omitempty"`
		Excluded            bool      `json:"excluded"`
	}(Member{})
	_ = Estimate{"source", 1, nil, .2, nil, "", 3}
	_ = Evidence{"name", "source", .2, "description"}
	_ = Category{"name", "source"}
	_ = Judgments{nil, nil, nil, nil, nil, nil, nil, "", false, 0, ""}
	_ = Facts{"", "", nil, "", false, nil, "", "", nil, nil, "", "", "", nil, "", nil, "", "", "", nil, nil, nil}
	_ = Written{"kind", "at"}
	_ = Headcount{"scraper", "kind", 26, "date", nil}
	candidateJSON, err := json.Marshal(Candidate{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		value any
		want  string
	}{
		{Member{OperatingConfidence: new(.8), RevenueLow: new(int64(10)), RevenueHigh: new(int64(20)), RevenueConfidence: new(.3), HeadcountLow: new(int64(40)), Categories: []string{"chosen-category"}, ClassifiedBy: "chosen-classifier", FleetVerdict: "chosen-fleet", Website: "chosen-website", WebsiteURL: "chosen-url", WebsiteSource: "chosen-source", SiteText: new("chosen-site-text"), Brief: new("chosen-brief"), Minutes: new(int64(17)), MinutesSource: "chosen-minutes-source", GovernmentMatch: "chosen-government", KnownSince: "chosen-known", NewestFiling: "chosen-filing", Excluded: true}, `{"candidate":` + string(candidateJSON) + `,"basis":"","operating_confidence":0.8,"revenue_low":10,"revenue_high":20,"revenue_confidence":0.3,"headcount_low":40,"categories":["chosen-category"],"classified_by":"chosen-classifier","fleet_verdict":"chosen-fleet","website":"chosen-website","website_url":"chosen-url","website_source":"chosen-source","site_text":"chosen-site-text","brief":"chosen-brief","minutes":17,"minutes_source":"chosen-minutes-source","government_match":"chosen-government","known_since":"chosen-known","newest_filing":"chosen-filing","excluded":true}`},
		{Headcount{}, `{"scraper":"","kind":"","count":0}`},
		{Headcount{Hours: new(int64(0))}, `{"scraper":"","kind":"","count":0,"hours":0}`},
		{Headcount{"chosen-scraper", "chosen-kind", 26, "2020-04-28", new(int64(47840))}, `{"scraper":"chosen-scraper","kind":"chosen-kind","count":26,"date":"2020-04-28","hours":47840}`},
		{Judgments{}, `{"operating_evidence":[],"excluded":false,"rules":0,"calculated":""}`},
		{Judgments{OperatingConfidence: new(.8), OperatingEvidence: []Evidence{{"n", "s", .2, "d"}}, RevenueLow: new(int64(10)), RevenueHigh: new(int64(20)), RevenueConfidence: new(.3), HeadcountLow: new(int64(40)), HeadcountHigh: new(int64(100)), HeadcountBasis: "chosen-basis", Excluded: true, Rules: 7, Calculated: "chosen-time"}, `{"operating_confidence":0.8,"operating_evidence":[{"name":"n","source":"s","weight":0.2,"description":"d"}],"revenue_low":10,"revenue_high":20,"revenue_confidence":0.3,"headcount_low":40,"headcount_high":100,"headcount_basis":"chosen-basis","excluded":true,"rules":7,"calculated":"chosen-time"}`},
		{Facts{}, `{"categories":[],"classified_by":"","records_differ":false,"record_categories":[],"fleet_verdict":"","website":"","website_source":"","minutes_source":"","written":[],"headcounts":[],"brief_labels":[]}`},
		{Facts{Headcounts: []Headcount{{"s", "k", 26, "", nil}}, BriefLabels: []string{"one", "two"}}, `{"categories":[],"classified_by":"","records_differ":false,"record_categories":[],"fleet_verdict":"","website":"","website_source":"","minutes_source":"","written":[],"headcounts":[{"scraper":"s","kind":"k","count":26}],"brief_labels":["one","two"]}`},
		{Estimate{}, `{"source":"","low":0,"confidence":0,"facts":{},"formula":0}`},
		{Estimate{"s", 10, new(int64(20)), .3, map[string]json.RawMessage{"chosen": json.RawMessage(`2`)}, "2020-04-28", 7}, `{"source":"s","low":10,"high":20,"confidence":0.3,"facts":{"chosen":2},"filed":"2020-04-28","formula":7}`},
	} {
		b, err := json.Marshal(tc.value)
		if err != nil || string(b) != tc.want {
			t.Fatal(string(b), tc.want, err)
		}
	}
	for _, low := range []*int64{nil, new(int64(0)), new(int64(40))} {
		b, err := json.Marshal(Member{HeadcountLow: low})
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err = json.Unmarshal(b, &object); err != nil {
			t.Fatal(err)
		}
		value, exists := object["headcount_low"]
		if exists != (low != nil) {
			t.Fatal(string(b))
		}
		if exists {
			var n int64
			if err = json.Unmarshal(value, &n); err != nil || n != *low {
				t.Fatal(string(b), err)
			}
		}
	}
}

// R-HJLO-5P2Y R-HKTK-JGTN R-HM1G-X8KC
func TestHeadcountAnswerProjections(t *testing.T) {
	f := store.Facts{Headcounts: []sources.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, AsOf: time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)}, {Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 20}}, BriefLabels: []string{"chosen-label-one", "chosen-label-two"}}
	got := factsOf(f, "")
	if !reflect.DeepEqual(got.Headcounts, []Headcount{{ppp.Name, sources.HeadcountJobs, 26, "2020-04-28", nil}, {fmcsa.Name, sources.HeadcountDrivers, 20, "", nil}}) || !reflect.DeepEqual(got.BriefLabels, f.BriefLabels) {
		t.Fatal(got)
	}
	empty := factsOf(store.Facts{}, "")
	if empty.Headcounts == nil || len(empty.Headcounts) != 0 || empty.BriefLabels == nil || len(empty.BriefLabels) != 0 {
		t.Fatal(empty)
	}
	for _, counts := range [][2]*int64{{nil, nil}, {nil, new(int64(1200))}, {new(int64(40)), nil}, {new(int64(40)), new(int64(100))}, {new(int64(30)), new(int64(30))}} {
		j := store.Judgments{HeadcountLow: counts[0], HeadcountHigh: counts[1]}
		if counts[0] != nil {
			j.HeadcountBasis = "chosen-basis"
		}
		v := judgmentsOf(j)
		if !reflect.DeepEqual(v.HeadcountLow, j.HeadcountLow) || !reflect.DeepEqual(v.HeadcountHigh, j.HeadcountHigh) || v.HeadcountBasis != j.HeadcountBasis {
			t.Fatal(v, j)
		}
		member := memberOf(store.Ranked{Judgments: j})
		if !reflect.DeepEqual(member.HeadcountLow, j.HeadcountLow) {
			t.Fatal(member, j)
		}
	}
}
