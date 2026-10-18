package enrichers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-I9B5-6YXP R-JW5H-WNEM R-IBQX-YIF3 R-ICYU-CA5S
func TestDeclaredValues(t *testing.T) {
	type word string
	words := []word{enrichers.KindDriveMinutes, enrichers.KindLinkFacts, enrichers.SourceZipDrive, enrichers.SourceLinks}
	if len(words) != 4 {
		t.Fatal(words)
	}

	const kinds = enrichers.KindDriveMinutes + ":" + enrichers.KindLinkFacts + ":" + enrichers.SourceZipDrive + ":" + enrichers.SourceLinks
	if kinds != "drive_minutes:link_facts:zip_drive:links" {
		t.Fatal(kinds)
	}
	n := int64(84)
	reading := sources.Reading{Name: "owned"}
	link := enrichers.Link{"scraper", "source", true, reading}
	s := enrichers.Subject{"id", "name", "city", "state", "email", "zip", "website", &n, []enrichers.Link{link}, []enrichers.Page{{7, "url", "title", "text", 4}}, &enrichers.LinkFound{KnownSince: "known"}}
	if s.ID != "id" || s.Name != "name" || s.City != "city" || s.State != "state" || s.Website != "website" || s.Email != "email" || s.Zip != "zip" || *s.Minutes != 84 || !s.Links[0].Gone || s.Links[0].Scraper != "scraper" || s.Links[0].SourceID != "source" || s.Links[0].Reading.Name != "owned" || s.Pages[0] != (enrichers.Page{7, "url", "title", "text", 4}) || s.Facts.KnownSince != "known" {
		t.Fatal(s)
	}
	r := enrichers.Result{"origin", json.RawMessage(`{}`), 0.5}
	if r.Source != "origin" || string(r.Found) != "{}" || r.Cost != 0.5 {
		t.Fatal(r)
	}
	cases := []struct {
		value any
		want  string
	}{
		{enrichers.Category{"n", "s"}, `{"name":"n","source":"s"}`},
		{enrichers.LinkFound{"a", "b", "c", []enrichers.Category{{"n", "s"}}, "pass", &n, &n}, `{"known_since":"a","newest_filing":"b","government_match":"c","categories":[{"name":"n","source":"s"}],"fleet_verdict":"pass","power_units":84,"drivers":84}`},
		{enrichers.LinkFound{}, `{"categories":null,"fleet_verdict":""}`},
		{enrichers.MinutesFound{&n}, `{"minutes":84}`}, {enrichers.MinutesFound{}, `{}`},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.value)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%T = %s, %v", tc.value, b, err)
		}
	}
	var lf enrichers.LinkFound
	if err := json.Unmarshal([]byte(cases[1].want), &lf); err != nil || lf.KnownSince != "a" || lf.NewestFiling != "b" || lf.GovernmentMatch != "c" || lf.Categories[0].Name != "n" || lf.FleetVerdict != "pass" || *lf.PowerUnits != n || *lf.Drivers != n {
		t.Fatal(lf, err)
	}
	var mf enrichers.MinutesFound
	if err := json.Unmarshal([]byte(cases[3].want), &mf); err != nil || *mf.Minutes != n {
		t.Fatal(mf, err)
	}
}

// R-IE6Q-Q1WH R-IFEN-3TN6 R-LYKV-TAVJ R-K28Z-TI43 R-M3GH-CDUB R-K3GW-79US R-IKA8-MWLY R-MGVD-JUZY
func TestRegistry(t *testing.T) {
	expected := []string{enrichers.KindDriveMinutes, enrichers.KindLinkFacts, sources.WebsiteCheck, enrichers.KindSiteText, enrichers.KindBrief}
	first := enrichers.Kinds()
	first[0] = "changed"
	if !slices.Equal(enrichers.Kinds(), expected) {
		t.Fatal("shared kinds")
	}
	for _, ck := range []*website.Checker{nil, {}} {
		reg := enrichers.New(enrichers.Config{Website: ck})
		all := reg.All()
		if len(all) != 5 {
			t.Fatalf("registry length %d", len(all))
		}
		for i, e := range all {
			if e.Kind() != expected[i] || e.Fetched() != (i >= 2) {
				t.Fatal(i, e)
			}
			found, ok := reg.Get(expected[i])
			if !ok || !reflect.DeepEqual(found, e) {
				t.Fatal("lookup", i)
			}
			_, derived := e.(enrichers.Deriver)
			_, fetched := e.(enrichers.Fetcher)
			_, prompted := e.(enrichers.Prompter)
			if derived != (i < 2) || fetched != (i == 2) || prompted != (i >= 3) {
				t.Fatal("interfaces", i)
			}
			inputs := e.Inputs(enrichers.Subject{})
			_ = inputs
			if derived {
				d := e.(enrichers.Deriver)
				result := d.Derive(enrichers.Subject{})
				_ = result
			}
			if fetched && e.(enrichers.Fetcher).Ready() != (ck != nil) {
				t.Fatal("fetcher interface readiness")
			}
			for _, raw := range []string{`{}`, `{"minutes":84}`, `{"categories":[],"fleet_verdict":"unknown"}`, `{"State":"https"}`, `{"run":"run_fixture","status":"exited","exit_code":0,"pages":3,"cost_nanos":0}`, `null`, `[]`, ``} {
				if reg.Valid(e.Kind(), json.RawMessage(raw)) != e.Valid(json.RawMessage(raw)) {
					t.Fatal("dispatch", raw)
				}
			}
		}
		if all[2].(enrichers.WebsiteCheck).Checker != ck {
			t.Fatal("checker lost")
		}
		all[0] = nil
		if reg.All()[0] == nil {
			t.Fatal("shared registry storage")
		}
		for _, bad := range []string{"", "website", "LINK_FACTS", "site-text", "BRIEF"} {
			e, ok := reg.Get(bad)
			if e != nil || ok || reg.Valid(bad, json.RawMessage(`{}`)) {
				t.Fatal(bad)
			}
		}
	}
}

// R-INXX-S7U1
func TestLinkFingerprint(t *testing.T) {
	e := enrichers.LinkFacts{}
	if e.Inputs(enrichers.Subject{}) != "" {
		t.Fatal("empty links")
	}
	a := enrichers.Link{Scraper: "one", SourceID: "a", Reading: sources.Reading{Category: "a"}}
	b := enrichers.Link{Scraper: "two", SourceID: "b", Gone: true, Reading: sources.Reading{FleetVerdict: sources.FleetPass}}
	s := enrichers.Subject{ID: "id", Links: []enrichers.Link{b, a}}
	before, _ := json.Marshal(s)
	lines := []string{}
	for _, l := range s.Links {
		v, _ := json.Marshal(l.Reading)
		state := "live"
		if l.Gone {
			state = "gone"
		}
		lines = append(lines, sources.SourceKey(l.Scraper, l.SourceID)+"\t"+state+"\t"+string(v)+"\n")
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "")))
	want := hex.EncodeToString(sum[:])
	if e.Inputs(s) != want {
		t.Fatal("fingerprint")
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("subject changed")
	}
	if e.Inputs(enrichers.Subject{Email: "other", Links: []enrichers.Link{a, b}}) != want {
		t.Fatal("order or unrelated fields affected inputs")
	}
	if e.Inputs(enrichers.Subject{Links: []enrichers.Link{b}}) == "" {
		t.Fatal("gone omitted")
	}
	for _, change := range []func(*enrichers.Link){func(l *enrichers.Link) { l.Gone = !l.Gone }, func(l *enrichers.Link) { l.Reading.Filed = time.Now() }, func(l *enrichers.Link) { l.Reading.Category = "different" }, func(l *enrichers.Link) { l.Reading.FleetVerdict = sources.FleetFail }} {
		changed := a
		change(&changed)
		if e.Inputs(enrichers.Subject{Links: []enrichers.Link{b, changed}}) == want {
			t.Fatal("change not fingerprinted")
		}
	}
}

// R-ISTJ-BAST R-UP16-EGCC R-NBNA-NXJF
func TestWebsiteInputsAndValidation(t *testing.T) {
	for _, ck := range []*website.Checker{nil, {}} {
		e := enrichers.WebsiteCheck{Checker: ck}
		if e.Ready() != (ck != nil) {
			t.Fatal("ready")
		}
		for email, want := range map[string]string{"Ops@Acme-Trucking.COM.": "acme-trucking.com", "joe@gmail.com": "", "": ""} {
			s := enrichers.Subject{Email: email}
			if e.Inputs(s) != want || s.Email != email {
				t.Fatal(email)
			}
		}
		for _, raw := range []string{`{"State":"https"}`, `{"State":"http","StatusCode":200,"Size":0}`, `{"State":"none","Candidate":"c","Kind":"website_check","Source":"s","Cost":0,"At":"2026-01-01T00:00:00Z"}`} {
			if !e.Valid(json.RawMessage(raw)) {
				t.Fatal(raw)
			}
		}
		for _, raw := range []string{`{"State":"unknown"}`, `{"State":"https","StatusCode":-1}`, `{"State":"https","Size":-1}`, `{"State":"https","StatusCode":"200"}`, `{}`, `null`, `[]`, ``, `{"State":"https"} {}`} {
			if e.Valid(json.RawMessage(raw)) {
				t.Fatal(raw)
			}
		}
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// R-NCV7-1PA4
func TestWebsiteFetch(t *testing.T) {
	var mu sync.Mutex
	requests := []string{}
	timers := []time.Duration{}
	ck := &website.Checker{Client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, r.URL.String())
		mu.Unlock()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<title>subject</title>"))}, nil
	})}, After: func(d time.Duration) <-chan time.Time {
		mu.Lock()
		timers = append(timers, d)
		mu.Unlock()
		timer := make(chan time.Time, 1)
		if d == website.Interval {
			timer <- time.Time{}
		}
		return timer
	}}
	inputs := []string{"acme.test", "other.test", "third.test"}
	entered := make(chan string, len(inputs))
	release := make(chan struct{})
	defer close(release)
	returned := make(chan struct{})
	got := map[string]enrichers.Result{}
	var f enrichers.Fetcher = enrichers.WebsiteCheck{Checker: ck}
	go func() {
		f.Fetch(context.Background(), inputs, func(in string, r enrichers.Result) {
			mu.Lock()
			got[in] = r
			mu.Unlock()
			entered <- in
			<-release
		})
		close(returned)
	}()
	for range inputs {
		select {
		case <-entered:
		case <-returned:
			t.Fatal("Fetch returned before every callback entered")
		case <-time.After(5 * time.Second):
			t.Fatal("missing callback")
		}
	}
	select {
	case <-returned:
		t.Fatal("Fetch returned while callbacks were blocked")
	case <-time.After(20 * time.Millisecond):
	}
	mu.Lock()
	for _, in := range inputs {
		r, ok := got[in]
		if !ok || r.Source != in || r.Cost != 0 {
			t.Error("result", in, r)
		}
		want := website.Result{State: sources.WebsiteHTTPS, URL: "https://" + in, Title: "subject", StatusCode: 200, Size: 22}
		b, _ := json.Marshal(want)
		if string(r.Found) != string(b) {
			t.Errorf("found %s want %s", r.Found, b)
		}
	}
	if len(got) != len(inputs) {
		t.Error("callback count", len(got))
	}
	slices.Sort(requests)
	wantRequests := []string{"https://acme.test", "https://other.test", "https://third.test"}
	if !slices.Equal(requests, wantRequests) {
		t.Error("requests", requests)
	}
	timeoutCount, intervalCount := 0, 0
	for _, d := range timers {
		switch d {
		case website.Timeout:
			timeoutCount++
		case website.Interval:
			intervalCount++
		default:
			t.Error("unexpected timer", d)
		}
	}
	if timeoutCount != len(inputs) || intervalCount != len(inputs)-1 {
		t.Error("timers", timers)
	}
	mu.Unlock()
	// Releasing one callback at a time proves Fetch waits for the final callback.
	for range len(inputs) - 1 {
		release <- struct{}{}
	}
	select {
	case <-returned:
		t.Fatal("Fetch returned before the last callback finished")
	case <-time.After(20 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Fetch did not return after callbacks")
	}
}

// R-K4OS-L1LH
func TestDerivedEncodingAvoidsPreviousWebsiteFields(t *testing.T) {
	n := int64(1)
	values := []any{enrichers.LinkFound{KnownSince: "a", NewestFiling: "b", GovernmentMatch: "c", Categories: []enrichers.Category{{Name: "n", Source: "s"}}, FleetVerdict: "pass", PowerUnits: &n, Drivers: &n}, enrichers.MinutesFound{Minutes: &n}, enrichers.SiteTextFound{Run: "run", Status: "exited", ExitCode: new(int), Pages: 2, CostNanos: 3}, enrichers.BriefFound{Run: "run", Status: "exited", ExitCode: new(int), CostNanos: 3, Summary: "summary", Categories: []enrichers.BriefCategory{{Name: "retail", Why: "why"}}, Labels: []string{"label"}}}
	for _, v := range values {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(b, &fields); err != nil {
			t.Fatal(err)
		}
		for field := range fields {
			for _, old := range []string{"Candidate", "Kind", "State", "URL", "Title", "StatusCode", "Size", "Error", "Source", "Cost", "At"} {
				if strings.EqualFold(field, old) {
					t.Fatal(field, old)
				}
			}
		}
	}
}
