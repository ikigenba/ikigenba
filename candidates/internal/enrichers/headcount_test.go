package enrichers_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-Z999-5A4C R-Z4DN-M75K R-BQOQ-UMXX
func TestLinkHeadcounts(t *testing.T) {
	d := time.Date(2020, 4, 28, 23, 30, 0, 0, time.FixedZone("west", -5*3600))
	links := []enrichers.Link{
		{Scraper: "carrier", Reading: sources.Reading{Headcount: &sources.Headcount{Scraper: "ignored", Kind: sources.HeadcountDrivers, Count: 20}}},
		{Gone: true, Scraper: "gone", Reading: sources.Reading{Headcount: &sources.Headcount{Count: 1}}},
		{Scraper: "loan", Reading: sources.Reading{Headcount: &sources.Headcount{Kind: sources.HeadcountJobs, Count: 26, AsOf: d}}},
		{Scraper: "unknown", Reading: sources.Reading{Headcount: &sources.Headcount{Kind: "x", Count: 0, AsOf: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}},
		{Scraper: "empty"},
	}
	s := enrichers.Subject{Links: links}
	before, _ := json.Marshal(s)
	r := (enrichers.LinkFacts{}).Derive(s)
	var got enrichers.LinkFound
	if err := json.Unmarshal(r.Found, &got); err != nil {
		t.Fatal(err)
	}
	want := []enrichers.Headcount{{"carrier", sources.HeadcountDrivers, 20, "", nil}, {"loan", sources.HeadcountJobs, 26, "2020-04-29", nil}, {"unknown", "x", 0, "", nil}}
	if !reflect.DeepEqual(got.Headcounts, want) || r.Source != enrichers.SourceLinks || r.Cost != 0 {
		t.Fatal(got, r)
	}
	if !(enrichers.LinkFacts{}).Valid(r.Found) {
		t.Fatal(string(r.Found))
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("subject changed")
	}
	for _, subject := range []enrichers.Subject{{}, {Links: []enrichers.Link{{}}}, {Links: []enrichers.Link{{Gone: true, Reading: links[0].Reading}}}} {
		r := (enrichers.LinkFacts{}).Derive(subject)
		var f enrichers.LinkFound
		if err := json.Unmarshal(r.Found, &f); err != nil || f.Headcounts == nil || len(f.Headcounts) != 0 {
			t.Fatal(string(r.Found), err)
		}
	}
	b, _ := json.Marshal(enrichers.Headcount{"a", "jobs", 2, "", nil})
	if string(b) != `{"scraper":"a","kind":"jobs","count":2}` {
		t.Fatal(string(b))
	}
}

// R-ZAH5-J1V1
func TestHeadcountFingerprint(t *testing.T) {
	base := enrichers.Subject{Links: []enrichers.Link{{Scraper: "source", SourceID: "record"}}}
	e := enrichers.LinkFacts{}
	empty := e.Inputs(base)
	h := sources.Headcount{Kind: sources.HeadcountJobs, Count: 26, AsOf: time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)}
	base.Links[0].Reading.Headcount = &h
	with := e.Inputs(base)
	if with == empty {
		t.Fatal("headcount ignored")
	}
	for _, change := range []func(*sources.Headcount){func(h *sources.Headcount) { h.Kind = sources.HeadcountDrivers }, func(h *sources.Headcount) { h.Count++ }, func(h *sources.Headcount) { h.AsOf = h.AsOf.AddDate(0, 0, 1) }} {
		changed := h
		change(&changed)
		base.Links[0].Reading.Headcount = &changed
		before, _ := json.Marshal(base)
		if e.Inputs(base) == with {
			t.Fatal("changed headcount ignored")
		}
		after, _ := json.Marshal(base)
		if string(before) != string(after) {
			t.Fatal("mutated")
		}
	}
}

// R-JSLE-MUH6
func TestHeadcountsValidation(t *testing.T) {
	base := `{"categories":[],"fleet_verdict":"unknown"`
	for _, tail := range []string{`}`, `,"headcounts":[]}`, `,"headcounts":null}`, `,"headcounts":[{"scraper":"ppp","kind":"jobs","count":26,"date":"2020-04-28"}]}`, `,"headcounts":[{"scraper":"","kind":"x","count":-1}]}`} {
		if !(enrichers.LinkFacts{}).Valid(json.RawMessage(base + tail)) {
			t.Fatal(tail)
		}
	}
	for _, tail := range []string{`,"headcounts":{}}`, `,"headcounts":[null]}`, `,"headcounts":[5]}`, `,"headcounts":[{"count":"26"}]}`, `,"headcounts":[{"date":"2020-4-28"}]}`, `,"headcounts":[{"x":1}]}`} {
		if (enrichers.LinkFacts{}).Valid(json.RawMessage(base + tail)) {
			t.Fatal(tail)
		}
	}
}

// R-BQOQ-UMXX R-BRWN-8EOM
func TestLinkHoursPreservedWithoutCombining(t *testing.T) {
	date := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ counts, hours []int64 }{
		{[]int64{357, 357}, []int64{715864, 715864}},
		{[]int64{283, 240}, []int64{588640, 499200}},
	} {
		subject := enrichers.Subject{}
		want := []enrichers.Headcount{}
		for i, n := range tc.counts {
			h := sources.Headcount{Kind: sources.HeadcountEmployees, Count: n, AsOf: date, Hours: &tc.hours[i]}
			subject.Links = append(subject.Links, enrichers.Link{Scraper: "osha_ita", Reading: sources.Reading{Headcount: &h}})
			want = append(want, enrichers.Headcount{Scraper: "osha_ita", Kind: h.Kind, Count: n, Date: "2024-12-31", Hours: &tc.hours[i]})
		}
		before, _ := json.Marshal(subject)
		result := (enrichers.LinkFacts{}).Derive(subject)
		var got enrichers.LinkFound
		if err := json.Unmarshal(result.Found, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Headcounts, want) {
			t.Fatal(got.Headcounts, want)
		}
		after, _ := json.Marshal(subject)
		if string(before) != string(after) {
			t.Fatal("subject changed")
		}
		subject.Links[0].Gone = true
		if err := json.Unmarshal((enrichers.LinkFacts{}).Derive(subject).Found, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Headcounts, want[1:]) {
			t.Fatal(got.Headcounts)
		}
	}
	for _, hours := range []*int64{nil, new(int64), new(int64(47840))} {
		h := sources.Headcount{Kind: sources.HeadcountJobs, Count: 26, Hours: hours}
		r := (enrichers.LinkFacts{}).Derive(enrichers.Subject{Links: []enrichers.Link{{Scraper: "ppp", Reading: sources.Reading{Headcount: &h}}}})
		expected := `{"scraper":"ppp","kind":"jobs","count":26`
		if hours != nil {
			b, _ := json.Marshal(*hours)
			expected += `,"hours":` + string(b)
		}
		expected += `}`
		var got enrichers.LinkFound
		if err := json.Unmarshal(r.Found, &got); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(got.Headcounts[0])
		if string(b) != expected || !reflect.DeepEqual(got.Headcounts[0].Hours, hours) {
			t.Fatal(string(b), expected)
		}
	}
}

// R-BT4J-M6FB
func TestLinkHoursValidation(t *testing.T) {
	prefix := `{"categories":[],"fleet_verdict":"unknown","headcounts":[{"scraper":"osha_ita","kind":"employees","count":23,"date":"2025-12-31"`
	for _, suffix := range []string{`}]}`, `,"hours":47840}]}`, `,"hours":null}]}`, `,"hours":0}]}`, `,"hours":-1}]}`} {
		if !(enrichers.LinkFacts{}).Valid(json.RawMessage(prefix + suffix)) {
			t.Fatal(suffix)
		}
	}
	for _, hours := range []string{`"47840"`, `47840.5`, `true`, `[47840]`} {
		if (enrichers.LinkFacts{}).Valid(json.RawMessage(prefix + `,"hours":` + hours + `}]}`)) {
			t.Fatal(hours)
		}
	}
}
