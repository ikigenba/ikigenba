package enrichers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-ARDE-JIX6 R-AQ5I-5R6H R-AXGW-GDMN R-AV13-OU59 R-B9NW-A31L
func TestInputProvenanceEncoding(t *testing.T) {
	n := int64(84)
	input := enrichers.InputSource{sources.FactAddress, "places", "places:ChIJ1"}
	for _, tc := range []struct {
		v    any
		want string
		dst  any
	}{
		{input, `{"fact":"address","source":"places","record":"places:ChIJ1"}`, &enrichers.InputSource{}},
		{enrichers.InputSource{}, `{"fact":"","source":"","record":""}`, &enrichers.InputSource{}},
		{enrichers.MinutesFound{&n, []enrichers.InputSource{input}}, `{"minutes":84,"input_sources":[{"fact":"address","source":"places","record":"places:ChIJ1"}]}`, &enrichers.MinutesFound{}},
		{enrichers.RouteFound{nil, nil, "", "", "", "", "", 0, []enrichers.InputSource{input}}, `{"address":"","precision":"","status":"","why":"","request_error":"","cost_nanos":0,"input_sources":[{"fact":"address","source":"places","record":"places:ChIJ1"}]}`, &enrichers.RouteFound{}},
		{enrichers.WebsiteSearchFound{enrichers.WebsiteSearchRun{ID: "r", Status: "failed"}, nil, nil, "", "", "", nil, "", []enrichers.InputSource{input}}, `{"run":{"id":"r","status":"failed","cost_nanos":0},"input_sources":[{"fact":"address","source":"places","record":"places:ChIJ1"}]}`, &enrichers.WebsiteSearchFound{}},
		{enrichers.LinkFound{KnownSinceSource: "fmcsa", NewestFilingSource: "ppp"}, `{"categories":null,"fleet_verdict":"","headcounts":null,"known_since_source":"fmcsa","newest_filing_source":"ppp"}`, &enrichers.LinkFound{}},
	} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Fatal(string(b), err)
		}
		if err = json.Unmarshal(b, tc.dst); err != nil {
			t.Fatal(err)
		}
		again, err := json.Marshal(tc.dst)
		if err != nil || !bytes.Equal(b, again) {
			t.Fatal(string(again), err)
		}
	}
	for _, tc := range []struct {
		minutes      *int64
		source, want string
	}{
		{&n, "places", `{"minutes":84,"input_sources":[{"fact":"address","source":"places","record":"places:ChIJ1"}]}`},
		{nil, "places", `{"input_sources":[{"fact":"address","source":"places","record":"places:ChIJ1"}]}`},
		{&n, "", `{"minutes":84}`},
	} {
		s := enrichers.Subject{Zip: "55072", Minutes: tc.minutes, AddressSource: tc.source, AddressRecord: "places:ChIJ1"}
		before, _ := json.Marshal(s)
		r := (enrichers.DriveMinutes{}).Derive(s)
		if string(r.Found) != tc.want || (enrichers.DriveMinutes{}).Inputs(s) != "55072" {
			t.Fatal(string(r.Found))
		}
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("mutated subject")
		}
	}
}

// R-LZWQ-BXGT
func TestLinkDateProvenance(t *testing.T) {
	date := func(s string) time.Time {
		d, e := time.Parse(time.RFC3339, s)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	f := enrichers.Link{Scraper: fmcsa.Name, SourceID: "f", Reading: sources.Reading{KnownSince: date("2004-01-15T00:00:00Z"), Filed: date("2025-03-18T00:00:00Z")}}
	x := enrichers.Link{Scraper: form5500.Name, SourceID: "x", Reading: sources.Reading{KnownSince: date("1995-01-01T00:00:00Z"), Filed: date("2020-01-01T00:00:00Z")}}
	gone := enrichers.Link{Scraper: ppp.Name, Gone: true, Reading: sources.Reading{KnownSince: date("1990-01-01T00:00:00Z"), Filed: date("2026-01-01T00:00:00Z")}}
	for _, tc := range []struct {
		links                []enrichers.Link
		known, filed, ks, fs string
	}{
		{[]enrichers.Link{f, x}, "1995-01-01", "2025-03-18", form5500.Name, fmcsa.Name},
		{[]enrichers.Link{x, f, gone}, "1995-01-01", "2025-03-18", form5500.Name, fmcsa.Name},
		{[]enrichers.Link{x}, "1995-01-01", "2020-01-01", form5500.Name, form5500.Name},
		{nil, "", "", "", ""},
		{[]enrichers.Link{{Scraper: "places"}}, "", "", "", ""},
		{[]enrichers.Link{{Reading: sources.Reading{KnownSince: date("1990-01-01T00:00:00Z")}}, x}, "1990-01-01", "2020-01-01", "", form5500.Name},
		{[]enrichers.Link{{Scraper: fmcsa.Name, Reading: sources.Reading{KnownSince: time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC), Filed: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}, x}, "1995-01-01", "2020-01-01", form5500.Name, form5500.Name},
		// Date provenance compares displayed UTC dates, even when the oldest instant is unranked.
		{[]enrichers.Link{{Reading: sources.Reading{KnownSince: date("1995-01-01T00:00:00Z"), Filed: date("2020-01-01T23:00:00Z")}}, x}, "1995-01-01", "2020-01-01", form5500.Name, form5500.Name},
	} {
		s := enrichers.Subject{Links: tc.links}
		before, _ := json.Marshal(s)
		r := (enrichers.LinkFacts{}).Derive(s)
		var got enrichers.LinkFound
		if err := json.Unmarshal(r.Found, &got); err != nil {
			t.Fatal(err)
		}
		if got.KnownSince != tc.known || got.NewestFiling != tc.filed || got.KnownSinceSource != tc.ks || got.NewestFilingSource != tc.fs {
			t.Fatalf("%s want %+v", r.Found, tc)
		}
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("mutated links")
		}
		if !(enrichers.LinkFacts{}).Valid(r.Found) {
			t.Fatal("invalid result", string(r.Found))
		}
	}
	for _, newer := range []string{fmcsa.Name, ppp.Name} {
		for _, reverse := range []bool{false, true} {
			a := enrichers.Link{Scraper: fmcsa.Name, Reading: sources.Reading{KnownSince: date("2010-01-01T00:00:00Z"), Filed: date("2020-01-01T00:00:00Z")}}
			b := a
			b.Scraper = ppp.Name
			if newer == fmcsa.Name {
				a.Seen = date("2026-01-01T00:00:00Z")
			} else {
				b.Seen = date("2026-01-01T00:00:00Z")
			}
			links := []enrichers.Link{a, b}
			if reverse {
				links = []enrichers.Link{b, a}
			}
			s := enrichers.Subject{Links: links}
			before, _ := json.Marshal(s)
			var got enrichers.LinkFound
			if err := json.Unmarshal((enrichers.LinkFacts{}).Derive(s).Found, &got); err != nil {
				t.Fatal(err)
			}
			if got.KnownSince != "2010-01-01" || got.NewestFiling != "2020-01-01" || got.KnownSinceSource != fmcsa.Name || got.NewestFilingSource != fmcsa.Name {
				t.Fatal(got)
			}
			after, _ := json.Marshal(s)
			if !bytes.Equal(before, after) {
				t.Fatal("mutated links")
			}
		}
	}
}

// R-WQ31-XUZE R-WOV5-K38P
func TestSearchInputProvenance(t *testing.T) {
	s := enrichers.Subject{Name: "name", NameSource: "places", NameRecord: "places:ChIJ1", AddressSource: "places", AddressRecord: "places:ChIJ1", EmailSource: "fmcsa", EmailRecord: "fmcsa:1023916"}
	want := []enrichers.InputSource{{sources.FactName, "places", "places:ChIJ1"}, {sources.FactAddress, "places", "places:ChIJ1"}, {sources.FactEmail, "fmcsa", "fmcsa:1023916"}}
	for _, status := range []string{"failed", enrichers.RunExited} {
		calls := 0
		k := &website.Checker{Client: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
		w := enrichers.WebsiteSearch{Checker: k}
		out := prompts.Outcome{ID: "r", Status: status, ExitCode: new(int), Stdout: `{"url":"","why":"none","rejected":[]}`}
		before, _ := json.Marshal(s)
		r := w.Found(context.Background(), s, out, 0)
		var got enrichers.WebsiteSearchFound
		_ = json.Unmarshal(r.Found, &got)
		if !reflect.DeepEqual(got.InputSources, want) || calls != 0 || !w.Valid(r.Found) {
			t.Fatal(string(r.Found), calls)
		}
		if !strings.HasSuffix(string(r.Found), `"input_sources":[{"fact":"name","source":"places","record":"places:ChIJ1"},{"fact":"address","source":"places","record":"places:ChIJ1"},{"fact":"email","source":"fmcsa","record":"fmcsa:1023916"}]}`) {
			t.Fatal(string(r.Found))
		}
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("mutated")
		}
		plain := s
		plain.NameSource = ""
		plain.NameRecord = ""
		plain.AddressSource = ""
		plain.AddressRecord = ""
		plain.EmailSource = ""
		plain.EmailRecord = ""
		if w.Inputs(plain) != w.Inputs(s) || !bytes.Equal(w.Input(plain), w.Input(s)) {
			t.Fatal("provenance affected inputs")
		}
		plainResult := w.Found(context.Background(), plain, out, 0)
		if strings.Contains(string(plainResult.Found), "input_sources") {
			t.Fatal(string(plainResult.Found))
		}
	}
	base := `{"run":{"id":"r","status":"failed","cost_nanos":0},"input_sources":`
	for _, raw := range []string{`[]`, `[{"fact":"name","source":"places","record":"r"}]`} {
		if !(enrichers.WebsiteSearch{}).Valid(json.RawMessage(base + raw + "}")) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`null`, `{}`, `[5]`, `[null]`, `[{"fact":5}]`, `[{"fact":"name","x":1}]`} {
		if (enrichers.WebsiteSearch{}).Valid(json.RawMessage(base + raw + "}")) {
			t.Fatal(raw)
		}
	}
}

// R-BC3P-1MIZ
func TestRouteInputProvenance(t *testing.T) {
	for _, answer := range []string{`{"status":"ZERO_RESULTS"}`, `{"status":"OK","results":[]}`, `malformed`} {
		c := &routes.Client{Key: "fixture", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, HTTP: &http.Client{Transport: routeTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(answer))}, nil
		})}}
		for _, canceled := range []bool{false, true} {
			for _, source := range []string{"", "places"} {
				ctx, cancel := context.WithCancel(context.Background())
				if canceled {
					cancel()
				}
				s := enrichers.Subject{Origin: "base", RecordAddress: sources.Sourced[sources.Address]{Value: sources.Address{Street: "12 Main St"}, Source: source, Key: "places:ChIJ1"}}
				r, tally := (enrichers.DriveRoute{Routes: c}).Route(ctx, s)
				cancel()
				var got enrichers.RouteFound
				_ = json.Unmarshal(r.Found, &got)
				want := []enrichers.InputSource(nil)
				if source != "" {
					want = []enrichers.InputSource{{sources.FactAddress, source, "places:ChIJ1"}}
				}
				if !reflect.DeepEqual(got.InputSources, want) || !(enrichers.DriveRoute{}).Valid(r.Found) {
					t.Fatal(string(r.Found))
				}
				if canceled && (tally != (enrichers.Tally{}) || got.Error != routes.FailCanceled) {
					t.Fatal(tally, got)
				}
			}
		}
	}
}

// R-M608-8S6A
func TestProvenanceValidation(t *testing.T) {
	cases := []struct {
		e              enrichers.Enricher
		valid, invalid []string
	}{
		{enrichers.LinkFacts{}, []string{`{"categories":[],"fleet_verdict":"unknown","known_since_source":"fmcsa","newest_filing_source":"ppp"}`}, []string{`{"categories":[],"fleet_verdict":"unknown","known_since_source":5}`}},
		{enrichers.DriveMinutes{}, []string{`{"minutes":84,"input_sources":[{"fact":"address","source":"places","record":"places:ChIJ1"}]}`, `{"input_sources":[]}`}, []string{`{"minutes":84,"input_sources":{}}`, `{"minutes":84,"input_sources":[5]}`, `{"minutes":84,"input_sources":[{"fact":"address","source":"places","record":"places:ChIJ1","x":1}]}`}},
	}
	for _, client := range []*routes.Client{nil, {}} {
		cases = append(cases, struct {
			e              enrichers.Enricher
			valid, invalid []string
		}{enrichers.DriveRoute{Routes: client}, []string{`{"minutes":47,"miles":38.2,"address":"a","precision":"building","status":"s","why":"","request_error":"","cost_nanos":0,"input_sources":[{"fact":"address","source":"places","record":"places:ChIJ1"}]}`}, []string{`{"input_sources":{}}`, `{"input_sources":[{"x":1}]}`}})
	}
	for _, tc := range cases {
		checkValidity(t, tc.e, tc.valid, tc.invalid)
	}
}

// R-PU0T-78HX R-M9NX-E3ED R-M784-MJWZ
func TestPickedSubjectInputs(t *testing.T) {
	for _, k := range []*website.Checker{nil, {}} {
		w := enrichers.WebsiteCheck{Checker: k}
		for _, tc := range []struct{ site, email, want string }{{"https://acme-places.test/", "", "https://acme-places.test/"}, {"https://acme-places.test/", "joe@gmail.com", "https://acme-places.test/"}, {"https://acme-places.test/", "ops@acme.test", "https://acme-places.test/\nacme.test"}, {"acme.test", "ops@acme.test", "acme.test"}} {
			s := enrichers.Subject{PlacesURL: tc.site, Email: tc.email}
			if w.Inputs(s) != tc.want {
				t.Fatal(w.Inputs(s))
			}
			s.Website = "ignored"
			s.Name = "ignored"
			s.EmailSource = "ignored"
			s.Check = &website.Result{}
			if w.Inputs(s) != tc.want {
				t.Fatal(w.Inputs(s))
			}
		}
	}
	s := enrichers.Subject{Email: "Ops@Acme.test", Check: &website.Result{State: sources.WebsiteHTTPS}, People: []sources.Sourced[sources.Person]{{Value: sources.Person{Name: "chosen", Role: "owner"}, Source: "brief"}}, Links: []enrichers.Link{{Reading: sources.Reading{People: []sources.Person{{Name: "unpicked"}}}}}}
	for _, check := range []*website.Result{nil, {State: sources.WebsiteNone, Error: website.FailStatus}, {State: sources.WebsiteHTTP}} {
		s.EmailCheck = check
		var got enrichers.WebsiteSearchInput
		_ = json.Unmarshal((enrichers.WebsiteSearch{}).Input(s), &got)
		state, why := sources.WebsiteUnknown, ""
		if check != nil {
			state, why = check.State, check.Error
		}
		if got.DomainCheck != state || got.DomainError != why || len(got.People) != 1 || got.People[0].Name != "chosen" {
			t.Fatal(got)
		}
		plain := s
		plain.Check = &website.Result{State: sources.WebsiteNone}
		plain.People = append([]sources.Sourced[sources.Person]{}, s.People...)
		plain.People[0].Source = "another"
		plain.People[0].Key = "another"
		if !bytes.Equal((enrichers.WebsiteSearch{}).Input(s), (enrichers.WebsiteSearch{}).Input(plain)) {
			t.Fatal("irrelevant check or people provenance")
		}
	}
	s.People = nil
	var brief enrichers.BriefInput
	_ = json.Unmarshal((enrichers.Brief{}).Input(s), &brief)
	if len(brief.Facts.People) != 0 {
		t.Fatal("brief re-read link people")
	}
}

// R-PU0T-78HX R-WF3Y-HXB5
func TestOwnerWebsiteControlsCheckAndSearchInputs(t *testing.T) {
	for _, checker := range []*website.Checker{nil, {}} {
		check := enrichers.WebsiteCheck{Checker: checker}
		search := enrichers.WebsiteSearch{Checker: checker}
		for _, siteText := range []*enrichers.SiteTextFound{nil, {Run: "run", Pages: 0}, {Run: "run", Pages: 2}} {
			s := enrichers.Subject{Name: "Name", City: "City", State: "MN", Website: "http://owner.test/", WebsiteSource: sources.Owner, PlacesURL: "https://places.test/", Email: "ops@acme.test", SiteText: siteText, SearchInputs: "old"}
			before := subjectJSON(t, s)
			if got := check.Inputs(s); got != "http://owner.test/\nhttps://places.test/\nacme.test" {
				t.Fatal(got)
			}
			if got := search.Inputs(s); got != "" {
				t.Fatal(got)
			}
			s.Website = ""
			if got := search.Inputs(s); got != "" {
				t.Fatal(got)
			}
			s.Website = "http://owner.test/"
			if !bytes.Equal(before, subjectJSON(t, s)) {
				t.Fatal("subject changed")
			}
			s.WebsiteSource = "Owner"
			if got := check.Inputs(s); got != website.Inputs(s.PlacesURL, s.Email) {
				t.Fatal(got)
			}
		}
	}
}
