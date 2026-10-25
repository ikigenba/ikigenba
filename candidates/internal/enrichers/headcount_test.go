package enrichers_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-Z999-5A4C R-Z4DN-M75K R-Z35R-8FEV
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
	want := []enrichers.Headcount{{"carrier", sources.HeadcountDrivers, 20, ""}, {"loan", sources.HeadcountJobs, 26, "2020-04-29"}, {"unknown", "x", 0, ""}}
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
	b, _ := json.Marshal(enrichers.Headcount{"a", "jobs", 2, ""})
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
