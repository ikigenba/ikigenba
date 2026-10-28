package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func searchVariant(t *testing.T, e store.Enrichment, change func(*enrichers.WebsiteSearchFound)) store.Enrichment {
	t.Helper()
	var found enrichers.WebsiteSearchFound
	contractOK(t, json.Unmarshal(e.Found, &found))
	change(&found)
	var err error
	e.Found, err = json.Marshal(found)
	contractOK(t, err)
	return e
}

type decisionValue struct {
	source, state, url, title, why string
	at                             time.Time
}

func decision(es []store.Enrichment) decisionValue {
	a, b, c, d, e, f := store.WebsiteDecisionOf(es)
	return decisionValue{a, b, c, d, e, f}
}

// R-7BI8-6R0S
func TestWebsiteDecisionTakeoversAndInertSearches(t *testing.T) {
	stamp := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	check := websiteResult("candidate", sources.WebsiteHTTPS)
	var checked website.Result
	contractOK(t, json.Unmarshal(check.Found, &checked))
	checked.URL = "https://acme.test/"
	checked.Title = "recorded title"
	check.Found = websiteFound(checked)
	check.At = stamp
	unknown := check
	unknown.Found = websiteFound(website.Result{State: sources.WebsiteNone, Title: "failed check"})
	search := searchResult(t, "candidate", sources.WebsiteHTTPS, "https://acme-trucking.test/")
	search.At = stamp.Add(time.Second)
	takeover := searchVariant(t, search, func(v *enrichers.WebsiteSearchFound) {
		v.Case = enrichers.SearchCaseDifferent
		v.Website = checked.URL
		v.Reason = enrichers.SearchReasonAgain
		v.WebsiteFrom = store.WebsiteByCheck
	})
	newer := searchResult(t, "candidate", sources.WebsiteHTTP, "http://newer-search.test/")
	newer.At = stamp.Add(2 * time.Second)
	unfound := searchResult(t, "candidate", "", "")
	unfound.At = stamp.Add(3 * time.Second)
	failed := searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) { v.Run.Status = "failed"; v.Run.ExitCode = nil })
	unconfirmed := searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) { v.Confirmation = nil })
	otherBytes := searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) { v.Website = "https://www.acme.test/" })
	noWebsite := searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) { v.Website = "" })
	historicStands := searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) {
		v.Case = enrichers.SearchCaseStands
		v.Answer.URL = "https://acme.test/contact"
	})
	redirectsBack := searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) {
		v.Answer.URL = "https://acme-plumbing.test/"
		v.Confirmation.URL = "https://WWW.Acme.test./login?token=2"
	})
	expectedCheck := decisionValue{store.WebsiteByCheck, sources.WebsiteHTTPS, checked.URL, checked.Title, "", stamp}
	expectedSearch := decisionValue{store.WebsiteBySearch, sources.WebsiteHTTPS, "https://acme-trucking.test/", "confirmed title", "owner's business", search.At}
	expectedNone := decisionValue{"", sources.WebsiteNone, "", "", "owner's business", unfound.At}
	cases := []struct {
		name string
		es   []store.Enrichment
		want decisionValue
	}{
		{"empty", nil, decisionValue{"", sources.WebsiteUnknown, "", "", "", time.Time{}}},
		{"unknown check", []store.Enrichment{unknown}, decisionValue{"", sources.WebsiteUnknown, "", "failed check", "", stamp}},
		{"check then legacy search", []store.Enrichment{search, check}, expectedCheck},
		{"legacy search then check", []store.Enrichment{check, search}, expectedCheck},
		{"takeover newest", []store.Enrichment{takeover, check}, expectedSearch},
		{"takeover oldest", []store.Enrichment{check, takeover}, expectedSearch},
		{"historic stands takeover newest", []store.Enrichment{historicStands, check}, expectedSearch},
		{"historic stands takeover oldest", []store.Enrichment{check, historicStands}, expectedSearch},
		{"historic different redirect back newest", []store.Enrichment{redirectsBack, check}, expectedCheck},
		{"historic different redirect back oldest", []store.Enrichment{check, redirectsBack}, expectedCheck},
		{"different bytes", []store.Enrichment{otherBytes, check}, expectedCheck},
		{"absent searched website", []store.Enrichment{noWebsite, check}, expectedCheck},
		{"failed takeover", []store.Enrichment{failed, check}, expectedCheck},
		{"unconfirmed takeover", []store.Enrichment{unconfirmed, check}, expectedCheck},
		{"confirmed survives unfound", []store.Enrichment{unfound, failed, search}, expectedSearch},
		{"unknown permits search", []store.Enrichment{unknown, search}, expectedSearch},
		{"unfound survives unconfirmed", []store.Enrichment{unconfirmed, failed, unfound, unknown}, expectedNone},
		{"takeover permits newest confirmed", []store.Enrichment{newer, check, takeover}, decisionValue{store.WebsiteBySearch, sources.WebsiteHTTP, "http://newer-search.test/", "confirmed title", "owner's business", newer.At}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contractEqual(t, decision(tc.es), tc.want)
			// Inserting an inert search anywhere preserves every decision component.
			for _, inert := range []store.Enrichment{
				searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) {
					v.Case = enrichers.SearchCaseStands
					v.Confirmation.URL = checked.URL
				}),
				redirectsBack,
				searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) {
					v.Website = "https://found.test/"
					v.Answer.URL = "https://found-co.test/"
					v.Confirmation.URL = "https://found.test/?token=2"
				}),
				searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) { v.Case = enrichers.SearchCaseStands; v.Confirmation = nil }),
				searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) {
					v.Case = enrichers.SearchCaseNone
					v.Answer.URL = ""
					v.Confirmation = nil
				}),
				searchVariant(t, takeover, func(v *enrichers.WebsiteSearchFound) { v.Confirmation.State = sources.WebsiteNone }),
			} {
				for i := 0; i <= len(tc.es); i++ {
					es := append([]store.Enrichment{}, tc.es[:i]...)
					es = append(es, inert)
					es = append(es, tc.es[i:]...)
					contractEqual(t, decision(es), tc.want)
				}
			}
		})
	}
}

// R-7CQ4-KIRH R-77UJ-1FSP R-7K1I-V57N
func TestWebsiteTakeoverProjectionPersistsAndSurvivesInertSearch(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, err := f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	list := prospectListCreate(t, f, "takeover")
	check := websiteResult(id, sources.WebsiteHTTPS)
	checked := websiteDecoded(check)
	take := searchVariant(t, searchResult(t, id, sources.WebsiteHTTPS, "https://different.test/"), func(v *enrichers.WebsiteSearchFound) {
		v.Case = enrichers.SearchCaseDifferent
		v.Website = checked.URL
		v.Reason = enrichers.SearchReasonAgain
		v.WebsiteFrom = store.WebsiteByCheck
	})
	inert := searchVariant(t, take, func(v *enrichers.WebsiteSearchFound) {
		v.Case = enrichers.SearchCaseStands
		v.Answer.URL = checked.URL
		v.Confirmation.URL = checked.URL
	})
	for i, e := range []store.Enrichment{check, take, check, inert} {
		f.now = f.now.Add(time.Second)
		_, err = f.s.AddEnrichments(ctx, []store.Enrichment{e})
		contractOK(t, err)
		facts, err := f.s.GetFacts(ctx, id)
		contractOK(t, err)
		if i == 0 {
			contractEqual(t, facts.WebsiteSource, store.WebsiteByCheck)
			continue
		}
		contractEqual(t, facts.WebsiteURL, "https://different.test/")
		contractEqual(t, facts.WebsiteSource, store.WebsiteBySearch)
		contractEqual(t, prospectListRead(t, f, list).Members[0].Facts, facts)
		f.reopen()
		got, err := f.s.GetFacts(ctx, id)
		contractOK(t, err)
		contractEqual(t, got, facts)
	}
	changed := check
	checked.URL = "https://new-record.test/"
	changed.Found = websiteFound(checked)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{changed})
	contractOK(t, err)
	contractEqual(t, factsAt(ctx, t, f.s, id).WebsiteURL, checked.URL)
	contractEqual(t, factsAt(ctx, t, f.s, id).WebsiteSource, store.WebsiteByCheck)
}

// R-7BI8-6R0S R-7CQ4-KIRH R-77UJ-1FSP
func TestWebsiteProjectionUsesHistoricalConfirmationLanding(t *testing.T) {
	for _, tc := range []struct {
		name, recordedCase, answer, landing, source, url string
	}{
		{"stands redirected away", enrichers.SearchCaseStands, "https://acme.test/contact", "https://other.test/", store.WebsiteBySearch, "https://other.test/"},
		{"no case redirected away", "", "https://acme.test/contact", "http://other.test/", store.WebsiteBySearch, "http://other.test/"},
		{"different redirected back", enrichers.SearchCaseDifferent, "https://other.test/", "https://WWW.Acme.test./login?token=2", store.WebsiteByCheck, "https://acme.test/"},
	} {
		for _, checkLast := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/check-first", true: "/check-last"}[checkLast], func(t *testing.T) {
				f := contractDB(t)
				ctx := context.Background()
				id := contractSeed(t, f)
				check := websiteResult(id, sources.WebsiteHTTPS)
				checked := websiteDecoded(check)
				checked.URL = "https://acme.test/"
				check.Found = websiteFound(checked)
				if !checkLast {
					_, err := f.s.AddEnrichments(ctx, []store.Enrichment{check})
					contractOK(t, err)
				}
				search := searchVariant(t, searchResult(t, id, sources.WebsiteHTTPS, tc.answer), func(v *enrichers.WebsiteSearchFound) {
					v.Case, v.Website, v.Reason, v.WebsiteFrom = tc.recordedCase, checked.URL, enrichers.SearchReasonAgain, store.WebsiteByCheck
					v.Confirmation.URL = tc.landing
				})
				// Earlier releases classified the answer URL before following its redirect.
				contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,inputs,found,source,at,cost) VALUES (?,?,?,?,?,?,?)`, id, search.Kind, search.Inputs, string(search.Found), search.Source, f.now.Format(time.RFC3339), search.Cost)
					return err
				}))
				trigger := headcountLinkResult(t, id)
				if checkLast {
					trigger = check
				}
				_, err := f.s.AddEnrichments(ctx, []store.Enrichment{trigger})
				contractOK(t, err)
				assert := func() {
					t.Helper()
					es, err := f.s.Enrichments(ctx, id)
					contractOK(t, err)
					want := decision(es)
					contractEqual(t, want.source, tc.source)
					contractEqual(t, want.url, tc.url)
					facts := factsAt(ctx, t, f.s, id)
					contractEqual(t, facts.WebsiteSource, want.source)
					contractEqual(t, facts.Website, want.state)
					contractEqual(t, facts.WebsiteURL, want.url)
				}
				assert()
				// A newer inert result must not hide the older landing evidence.
				inert := searchVariant(t, search, func(v *enrichers.WebsiteSearchFound) {
					v.Case = enrichers.SearchCaseStands
					v.Answer.URL, v.Confirmation.URL = checked.URL, checked.URL
				})
				_, err = f.s.AddEnrichments(ctx, []store.Enrichment{inert})
				contractOK(t, err)
				assert()
				f.reopen()
				assert()
				contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "DELETE FROM facts WHERE candidate=?", id)
					return err
				}))
				contractOK(t, fillFactsWalk(ctx, f.s))
				assert()
			})
		}
	}
}

// R-7BI8-6R0S R-77UJ-1FSP
func TestWebsiteProjectionWithAbsentConfirmationURL(t *testing.T) {
	for _, confirmation := range []string{`{"state":"https"}`, `{"state":"https","url":null}`, `{"state":"https","url":""}`} {
		t.Run(confirmation, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			id := contractSeed(t, f)
			check := websiteResult(id, sources.WebsiteHTTPS)
			search := searchVariant(t, searchResult(t, id, sources.WebsiteHTTPS, "https://found.test/"), func(v *enrichers.WebsiteSearchFound) {
				v.Website, v.WebsiteFrom, v.Reason, v.Case = websiteDecoded(check).URL, store.WebsiteByCheck, enrichers.SearchReasonAgain, enrichers.SearchCaseDifferent
			})
			var found map[string]json.RawMessage
			contractOK(t, json.Unmarshal(search.Found, &found))
			found["confirmation"] = json.RawMessage(confirmation)
			var err error
			search.Found, err = json.Marshal(found)
			contractOK(t, err)
			contractEqual(t, enrichers.New(enrichers.Config{}).Valid(search.Kind, search.Found), true)
			_, err = f.s.AddEnrichments(ctx, []store.Enrichment{check, search})
			contractOK(t, err)
			facts := factsAt(ctx, t, f.s, id)
			contractEqual(t, facts.Website, sources.WebsiteHTTPS)
			contractEqual(t, facts.WebsiteURL, "")
			contractEqual(t, facts.WebsiteSource, store.WebsiteBySearch)
			f.reopen()
			contractEqual(t, factsAt(ctx, t, f.s, id), facts)
		})
	}
}

// R-WG2I-FA6C
func TestSubjectUsesMatchingSiteTextAndNewestSearchInputs(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := enrichmentCandidate(t, f, "empty-site", "office@business.test")
	assertSite := func(want *enrichers.SiteTextFound, search string) {
		t.Helper()
		s, e := f.s.Subject(ctx, id)
		contractOK(t, e)
		contractEqual(t, s.SiteText, want)
		contractEqual(t, s.SearchInputs, search)
	}
	assertSite(nil, "")
	makeText := func(url, run string, pages int64) store.Enrichment {
		b, e := json.Marshal(enrichers.SiteTextFound{Run: run, Status: enrichers.RunExited, ExitCode: new(int), Pages: pages})
		contractOK(t, e)
		return store.Enrichment{Candidate: id, Kind: enrichers.KindSiteText, Inputs: url, Source: enrichers.SourcePrompts, Found: b}
	}
	add := func(es ...store.Enrichment) { t.Helper(); _, e := f.s.AddEnrichments(ctx, es); contractOK(t, e) }
	check := websiteResult(id, sources.WebsiteHTTPS)
	url := websiteDecoded(check).URL
	add(makeText(url, "first", 0))
	assertSite(nil, "")
	add(check, makeText("https://other.test/", "other", 2))
	first := &enrichers.SiteTextFound{Run: "first", Status: enrichers.RunExited, ExitCode: new(int)}
	assertSite(first, "")
	due, e := f.s.DueAmong(ctx, enrichers.KindWebsiteSearch, []string{id}, false)
	contractOK(t, e)
	contractEqual(t, len(due), 1)
	subject, e := f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, due[0].Inputs, (enrichers.WebsiteSearch{}).Inputs(subject))
	_, all, e := f.s.DueEnrichments(ctx, enrichers.KindWebsiteSearch, "", 10)
	contractOK(t, e)
	contractEqual(t, all, due)
	failed := searchVariant(t, searchResult(t, id, "", ""), func(v *enrichers.WebsiteSearchFound) { v.Run.Status = "failed"; v.Run.ExitCode = nil })
	failed.Inputs = due[0].Inputs
	add(failed)
	assertSite(first, failed.Inputs)
	due, e = f.s.DueAmong(ctx, enrichers.KindWebsiteSearch, []string{id}, false)
	contractOK(t, e)
	contractEqual(t, len(due), 0)
	_, all, e = f.s.DueEnrichments(ctx, enrichers.KindWebsiteSearch, "", 10)
	contractOK(t, e)
	contractEqual(t, len(all), 0)
	due, e = f.s.DueAmong(ctx, enrichers.KindWebsiteSearch, []string{id}, true)
	contractOK(t, e)
	contractEqual(t, len(due), 1)
	latest := makeText(url, "latest", 0)
	first = &enrichers.SiteTextFound{Run: "latest", Status: "timed_out", Pages: 0}
	latest.Found, e = json.Marshal(first)
	contractOK(t, e)
	add(latest)
	assertSite(first, failed.Inputs)
	subject, e = f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, subject.SiteText.Failed(), true)
	due, e = f.s.DueAmong(ctx, enrichers.KindWebsiteSearch, []string{id}, false)
	contractOK(t, e)
	contractEqual(t, len(due), 1)
	contractEqual(t, due[0].Inputs, (enrichers.WebsiteSearch{}).Inputs(subject))
	if due[0].Inputs == failed.Inputs {
		t.Fatal("new failed extraction kept the previous search fingerprint")
	}
	_, all, e = f.s.DueEnrichments(ctx, enrichers.KindWebsiteSearch, "", 10)
	contractOK(t, e)
	contractEqual(t, all, due)
	newer := failed
	newer.Inputs = "latest-search-inputs"
	add(newer)
	assertSite(first, newer.Inputs)
	add(websiteResult(id, sources.WebsiteNone))
	assertSite(nil, newer.Inputs)
}
