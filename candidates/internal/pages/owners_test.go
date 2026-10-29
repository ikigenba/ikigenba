package pages

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-RWHD-2HEG R-RXP9-G955 R-RYX5-U0VU R-S052-7SMJ
func TestOwnerRowsAndNotes(t *testing.T) {
	at := fixtureTime()
	later := at.Add(25*time.Hour + 23*time.Second)
	a := store.OwnerRecord{ID: "first", Candidate: "candidate", Founded: true, Note: "met at the expo", At: at, Set: store.OwnerValues{Name: new("North Star Hauling"), Headcount: &store.OwnerHeadcount{Count: 26, Year: 2024}}}
	b := store.OwnerRecord{ID: "second", Candidate: "candidate", Note: "new number", At: later, Set: store.OwnerValues{Phone: new("2185550101")}, Cleared: []string{sources.FactName}}
	an := OwnerNoteView{a.Note, "2026-10-05T09:00:07Z", "2026-10-05 09:00"}
	bn := OwnerNoteView{b.Note, "2026-10-06T10:00:30Z", "2026-10-06 10:00"}
	rows, set, founded := ownerViews("candidate", []store.OwnerRecord{b, a})
	want := []OwnerRecordRow{{false, b.Note, bn.At, bn.AtText, []OwnerFactRow{{sources.FactName, true, ""}, {sources.FactPhone, false, `"2185550101"`}}}, {true, a.Note, an.At, an.AtText, []OwnerFactRow{{sources.FactName, false, `"North Star Hauling"`}, {sources.FactHeadcount, false, `{"count":26,"year":2024}`}}}}
	if !reflect.DeepEqual(rows, want) || !reflect.DeepEqual(set, map[string]OwnerNoteView{sources.FactHeadcount: an, sources.FactPhone: bn}) || !reflect.DeepEqual(founded, &an) || ownerURL([]store.OwnerRecord{b, a}) != "" {
		t.Fatal(rows, set, founded)
	}
	a.Candidate = "merged"
	rows, _, founded = ownerViews("candidate", []store.OwnerRecord{b, a})
	if rows[1].Founded || founded != nil {
		t.Fatal(rows, founded)
	}
	c := store.OwnerRecord{Set: store.OwnerValues{Revenue: &store.OwnerRevenue{High: new(int64(2000000))}, Website: new("https://owner-co.test/")}}
	rows, _, _ = ownerViews("candidate", []store.OwnerRecord{c})
	if !reflect.DeepEqual(rows[0].Facts, []OwnerFactRow{{sources.FactRevenue, false, `{"high":2000000}`}, {sources.FactWebsite, false, `"https://owner-co.test/"`}}) || ownerURL([]store.OwnerRecord{c}) != *c.Set.Website {
		t.Fatal(rows)
	}
	a.Cleared = []string{sources.FactPhone}
	rows, _, _ = ownerViews("candidate", []store.OwnerRecord{a})
	if rows[0].Facts[2] != (OwnerFactRow{sources.FactPhone, true, ""}) {
		t.Fatal(rows)
	}
	a.Cleared = nil
	a.Candidate = "candidate"
	b = a
	b.ID = "later"
	a.ID = "earlier"
	a.Note = "first"
	b.Note = "second"
	for _, rs := range [][]store.OwnerRecord{{a, b}, {b, a}} {
		_, set, _ = ownerViews("candidate", rs)
		if set[sources.FactName].Note != "first" {
			t.Fatal(set)
		}
	}
	rows, set, founded = ownerViews("candidate", nil)
	if rows == nil || set == nil || len(rows)+len(set) != 0 || founded != nil || ownerURL(nil) != "" {
		t.Fatal(rows, set, founded)
	}
}

// R-0UMZ-IM7G R-0VUV-WDY5
func TestOwnerCandidateAndFactViews(t *testing.T) {
	c := store.Candidate{Status: store.StatusActive, EIN: "411234567", EINSource: "form5500", OtherEmails: []sources.Sourced[string]{{Value: "info@acme.test", Source: "ppp"}, {Value: "sales@acme.test", Source: sources.Owner}}}
	v := candidateView(c)
	if v.EIN != c.EIN || v.EINSource != c.EINSource || !reflect.DeepEqual(v.OtherEmails, []OtherRow{{"info@acme.test", "ppp"}, {"sales@acme.test", sources.Owner}}) || v.Founded != nil || v.OwnerSet == nil || len(v.OwnerSet) != 0 {
		t.Fatal(v)
	}
	c.EIN = "419876543"
	c.EINSource = sources.Owner
	v = candidateView(c)
	if v.EIN != c.EIN || v.EINSource != sources.Owner {
		t.Fatal(v)
	}
	c.Status = store.StatusMerged
	v = candidateView(c)
	if v.EIN != "" || v.EINSource != "" || v.OtherEmails == nil || len(v.OtherEmails) != 0 || v.OwnerSet == nil || len(v.OwnerSet) != 0 || v.Founded != nil {
		t.Fatal(v)
	}
	v = candidateView(store.Candidate{Status: store.StatusActive})
	if v.EIN != "" || v.EINSource != "" || v.OtherEmails == nil || len(v.OtherEmails) != 0 {
		t.Fatal(v)
	}
	f := store.Facts{KnownSince: time.Date(1988, 1, 1, 0, 0, 0, 0, time.UTC), KnownSinceSource: sources.Owner, OtherKnownSince: []sources.Sourced[time.Time]{{Value: time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC), Source: "form5500"}, {Value: time.Date(2001, 3, 15, 0, 0, 0, 0, time.UTC), Source: "fmcsa"}}, PowerUnits: new(int64(7)), OtherPowerUnits: []sources.Sourced[int64]{{Value: 12, Source: "fmcsa"}}}
	fv := factsView(f, "")
	if fv.KnownSince != "1988-01-01" || fv.KnownSinceSource != sources.Owner || fv.PowerUnits != "7" || !reflect.DeepEqual(fv.OtherKnownSince, []OtherRow{{"1995-01-01", "form5500"}, {"2001-03-15", "fmcsa"}}) || !reflect.DeepEqual(fv.OtherPowerUnits, []OtherRow{{"12", "fmcsa"}}) {
		t.Fatal(fv)
	}
	fv = factsView(store.Facts{}, "")
	if fv.OtherKnownSince == nil || fv.OtherPowerUnits == nil || len(fv.OtherKnownSince)+len(fv.OtherPowerUnits) != 0 {
		t.Fatal(fv)
	}
}

// R-0X2S-A5OU R-S68K-4NC0 R-03T7-3NW6 R-0VUV-WDY5
func TestOwnerHistoryOnCandidatePages(t *testing.T) {
	cfg, d := pageFixture(t)
	now := fixtureTime()
	cfg.Store = store.New(d, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	r, c, err := cfg.Store.AddOwnerRecord(t.Context(), store.OwnerRecord{Note: "met at the expo", UserID: "owner", Set: store.OwnerValues{Name: new("North Star Hauling"), EIN: new("411234567")}})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	r2, _, err := cfg.Store.AddOwnerRecord(t.Context(), store.OwnerRecord{Candidate: c.ID, Note: "new number", UserID: "owner", Set: store.OwnerValues{Phone: new("2185550101")}, Cleared: []string{sources.FactEIN}})
	if err != nil {
		t.Fatal(err)
	}
	got := capturedCandidate(t, cfg, c.ID)
	rows, set, founded := ownerViews(c.ID, []store.OwnerRecord{r2, r})
	if !reflect.DeepEqual(got.OwnerRecords, rows) || !reflect.DeepEqual(got.Candidate.OwnerSet, set) || !reflect.DeepEqual(got.Candidate.Founded, founded) || got.Candidate.Name != "North Star Hauling" || got.Candidate.NameSource != sources.Owner || got.Candidate.Phone != "2185550101" || got.Candidate.PhoneSource != sources.Owner || got.Candidate.EIN != "" || got.Candidate.EINSource != "" {
		t.Fatal(got)
	}
	_, ids, err := cfg.Store.Attach(t.Context(), "run", false, []store.Observation{{Scraper: "fixture", SourceID: "row", Digest: "digest", Reading: sources.Reading{Name: "survivor", NameKey: "survivor"}}})
	if err != nil {
		t.Fatal(err)
	}
	before := capturedCandidate(t, cfg, ids[0])
	if before.OwnerRecords == nil || len(before.OwnerRecords) != 0 || before.Candidate.OwnerSet == nil || len(before.Candidate.OwnerSet) != 0 || before.Candidate.Founded != nil {
		t.Fatal(before)
	}
	if _, _, err = cfg.Store.Decide(t.Context(), store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: c.ID, Why: "same", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	got = capturedCandidate(t, cfg, ids[0])
	if len(got.OwnerRecords) != 2 || got.OwnerRecords[0].Founded || got.OwnerRecords[1].Founded || got.Candidate.Founded != nil || len(got.Candidate.OwnerSet) != 2 {
		t.Fatal(got)
	}
	got = capturedCandidate(t, cfg, c.ID)
	if got.OwnerRecords == nil || len(got.OwnerRecords) != 0 || got.Candidate.Founded != nil || got.Candidate.OwnerSet == nil || len(got.Candidate.OwnerSet) != 0 {
		t.Fatal(got)
	}
	// A derived row exposes owner precedence and the record alternatives at the HTTP read.
	_, ids, err = cfg.Store.Attach(t.Context(), "facts-run", false, []store.Observation{{Scraper: "form5500", SourceID: "facts-row", Digest: "facts", Reading: sources.Reading{Name: "facts-business", NameKey: "facts-business", PhoneKey: "2185550199", Phone: "2185550199", KnownSince: time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC)}}, {Scraper: "fmcsa", SourceID: "power-row", Digest: "power", Reading: sources.Reading{Name: "facts-business", NameKey: "facts-business", PhoneKey: "2185550199", Phone: "2185550199", PowerUnits: new(int64(12))}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) > 1 && ids[0] != ids[1] {
		if _, _, err = cfg.Store.Decide(t.Context(), store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same business", UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = cfg.Store.Derive(t.Context(), enrichers.KindLinkFacts, "", false, 100); err != nil {
		t.Fatal(err)
	}
	if _, _, err = cfg.Store.AddOwnerRecord(t.Context(), store.OwnerRecord{Candidate: ids[0], Note: "owner facts", UserID: "owner", Set: store.OwnerValues{KnownSince: new(1988), Fleet: new(int64(7))}}); err != nil {
		t.Fatal(err)
	}
	got = capturedCandidate(t, cfg, ids[0])
	if got.Enrichments.Facts.KnownSince != "1988-01-01" || got.Enrichments.Facts.PowerUnits != "7" || len(got.Enrichments.Facts.OtherKnownSince) != 1 || len(got.Enrichments.Facts.OtherPowerUnits) != 1 {
		t.Fatalf("%+v", got.Enrichments.Facts)
	}
}

// R-6I63-BISB
func TestOwnerTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{store.StatusActive, store.StatusMerged} {
		for _, filled := range []bool{false, true} {
			for _, mapState := range []int{0, 1, 2, 3} {
				data := CandidateData{Candidate: CandidateView{Status: status}, Enrichments: &EnrichmentsView{Facts: &FactsView{}}}
				values := []string{}
				next := func() string {
					v := fmt.Sprintf("chosenvalue%03dend", len(values))
					values = append(values, v)
					return v
				}
				if mapState > 0 {
					data.Candidate.OwnerSet = map[string]OwnerNoteView{}
				}
				if mapState >= 2 {
					for _, w := range append(sources.SettableFacts(), "other-fact") {
						at := "at"
						if mapState == 3 {
							at = ""
						}
						data.Candidate.OwnerSet[w] = OwnerNoteView{next(), at, next()}
					}
				}
				if filled {
					data.Candidate.Founded = &OwnerNoteView{next(), "at", next()}
					data.Candidate.EIN = next()
					data.Candidate.EINSource = "chosen-source"
					data.Candidate.OtherEmails = []OtherRow{{next(), "chosen-source"}}
					data.Enrichments.Facts.OtherKnownSince = []OtherRow{{next(), "chosen-source"}}
					data.Enrichments.Facts.OtherPowerUnits = []OtherRow{{next(), "chosen-source"}}
					for _, founded := range []bool{true, false} {
						row := OwnerRecordRow{Founded: founded, Note: next(), At: "at", AtText: next(), Facts: []OwnerFactRow{}}
						for _, w := range sources.SettableFacts() {
							row.Facts = append(row.Facts, OwnerFactRow{w, false, next()}, OwnerFactRow{w, true, ""})
						}
						data.OwnerRecords = append(data.OwnerRecords, row)
					}
					data.OwnerRecords = append(data.OwnerRecords, OwnerRecordRow{Note: next(), At: "at", AtText: next()})
				}
				var body bytes.Buffer
				if err = set.templates.ExecuteTemplate(&body, "candidate", data); err != nil {
					t.Fatal(status, filled, mapState, err)
				}
				if status == store.StatusActive && filled {
					want := []string{data.Candidate.Founded.Note, data.Candidate.Founded.AtText, data.Candidate.EIN, data.Candidate.OtherEmails[0].Value, data.Enrichments.Facts.OtherKnownSince[0].Value, data.Enrichments.Facts.OtherPowerUnits[0].Value}
					for _, r := range data.OwnerRecords {
						want = append(want, r.Note, r.AtText)
						for _, f := range r.Facts {
							if !f.Cleared {
								want = append(want, f.Value)
							}
						}
					}
					if mapState == 2 {
						n := data.Candidate.OwnerSet[sources.FactEIN]
						want = append(want, n.Note, n.AtText)
					}
					for _, v := range want {
						if !strings.Contains(body.String(), v) {
							t.Fatalf("template omitted supplied value %s", v)
						}
					}
				}
			}
		}
	}
}

// R-02LA-PW5H R-03T7-3NW6
func TestOwnerWebsiteDecisionOnPage(t *testing.T) {
	cfg, d := pageFixture(t)
	now := fixtureTime()
	cfg.Store = store.New(d, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	r, c, err := cfg.Store.AddOwnerRecord(t.Context(), store.OwnerRecord{UserID: "owner", Note: "founded", Set: store.OwnerValues{Name: new("website business")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: c.ID, Kind: sources.WebsiteCheck, Inputs: "check", Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: "https://acme.test/"})}}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	r, _, err = cfg.Store.AddOwnerRecord(t.Context(), store.OwnerRecord{Candidate: c.ID, UserID: "owner", Note: "website", Set: store.OwnerValues{Website: new("https://owner-co.test/")}})
	if err != nil {
		t.Fatal(err)
	}
	got := capturedCandidate(t, cfg, c.ID)
	if got.Candidate.WebsiteSource != store.WebsiteByOwner || got.Candidate.Website != sources.WebsiteHTTPS || got.Candidate.WebsiteURL != "https://owner-co.test/" || got.Candidate.WebsiteCheckedURL != "" || got.Candidate.OwnerSet[sources.FactWebsite].Note != r.Note {
		t.Fatal(got.Candidate)
	}
	now = now.Add(time.Second)
	if _, _, err = cfg.Store.AddOwnerRecord(t.Context(), store.OwnerRecord{Candidate: c.ID, UserID: "owner", Note: "clear", Cleared: []string{sources.FactWebsite}}); err != nil {
		t.Fatal(err)
	}
	got = capturedCandidate(t, cfg, c.ID)
	if got.Candidate.WebsiteSource != store.WebsiteByCheck || got.Candidate.WebsiteURL != "https://acme.test/" {
		t.Fatal(got.Candidate)
	}
}

// R-02LA-PW5H
func TestOwnerWebsiteFields(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	check := store.Enrichment{Kind: sources.WebsiteCheck, At: at, Found: encodedJSON(website.Result{State: sources.WebsiteHTTP, From: "https://owner-co.test/", URL: "http://owner-co.test/home", Title: "Owner Co"})}
	legacy := check
	legacy.Found = encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: "https://acme.test/", Title: "Acme"})
	zero := 0
	takeover := store.Enrichment{Kind: enrichers.KindWebsiteSearch, At: at.Add(-time.Hour), Found: encodedJSON(enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{Status: enrichers.RunExited, ExitCode: &zero}, Reason: "empty_site", Website: "https://acme.test/", WebsiteFrom: "check", SiteText: &enrichers.WebsiteSearchSiteText{Status: enrichers.RunExited}, Case: "different", Answer: &enrichers.WebsiteSearchAnswer{URL: "https://acme-trucking.test/", Why: "chosen-why"}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://acme-trucking.test/", Title: "chosen-title"}})}
	for _, tc := range []struct {
		url                       string
		es                        []store.Enrichment
		state, title, stamp, text string
	}{{"https://owner-co.test/", nil, sources.WebsiteHTTPS, "", "", ""}, {"http://owner-co.test/", nil, sources.WebsiteHTTP, "", "", ""}, {"https://owner-co.test/", []store.Enrichment{check}, sources.WebsiteHTTP, "Owner Co", "2026-10-08T12:00:00Z", "2026-10-08 12:00:00 UTC"}, {"https://owner-co.test/", []store.Enrichment{legacy, takeover}, sources.WebsiteHTTPS, "", "", ""}} {
		var got CandidateView
		setWebsite(&got, tc.es, "https://places.test/", tc.url)
		if got.WebsiteSource != store.WebsiteByOwner || got.Website != tc.state || got.WebsiteURL != tc.url || got.WebsiteTitle != tc.title || got.WebsiteWhy != "" || got.WebsiteChecked != tc.stamp || got.WebsiteCheckedText != tc.text || got.WebsiteCheckedURL != "" {
			t.Fatal(got, tc)
		}
	}
}
