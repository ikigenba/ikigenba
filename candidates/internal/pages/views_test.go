package pages

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-JNUV-CQY3 R-545R-NE09
func TestFormatting(t *testing.T) {
	if MinuteLayout != "2006-01-02 15:04" || SecondLayout != "2006-01-02 15:04:05 UTC" || DateLayout != "2006-01-02" || ShortlistShown != 30 || ToolsLevel.Name != "tools" || ToolsLevel.URL != "/tools" || AboutLevel.Name != "about" || AboutLevel.URL != "/about" {
		t.Fatal("constants")
	}
	for _, tc := range []struct {
		n int64
		s string
	}{{0, "$0"}, {999, "$999"}, {10000000, "$10,000,000"}} {
		if dollars(tc.n) != tc.s {
			t.Fatal(tc)
		}
	}
	if money(7279999.6) != "$7,280,000" || two(.7) != "0.70" {
		t.Fatal("numbers")
	}
	tm := time.Date(2026, 10, 5, 10, 0, 7, 0, time.FixedZone("offset", 3600))
	if date(tm) != "2026-10-05" || date(time.Time{}) != "" {
		t.Fatal("date")
	}
	a, b := stamp(tm, MinuteLayout)
	if a != "2026-10-05T09:00:07Z" || b != "2026-10-05 09:00" {
		t.Fatal(a, b)
	}
	_, b = stamp(tm, SecondLayout)
	if b != "2026-10-05 09:00:07 UTC" {
		t.Fatal(b)
	}
	a, b = stamp(time.Time{}, SecondLayout)
	if a != "" || b != "" {
		t.Fatal("zero")
	}
}

// R-4RYR-TOLB R-4T6O-7GC0 R-4UEK-L82P R-4VMG-YZTE R-4Y29-QJAS R-4ZA6-4B1H R-8KNN-I5U6 R-56LK-EXHN R-591D-6GZ1 R-7IQW-KU2U
func TestViews(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC)
	hi := int64(24000000)
	mins := int64(31)
	p := store.Pass{ID: "pass_abcdef0123456789", Trigger: store.TriggerRebuild, Status: store.StatusRunning, Started: now, Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Rescored: 8, Unranged: 9, UserID: "starter"}
	if got := passView(p, ""); got.StartedBy != "starter" || got.RunPage != "" || got.Rows != 1 || got.New != 2 || got.Changed != 3 || got.Unchanged != 4 || got.Gone != 5 || got.Created != 6 || got.Linked != 7 || got.Rescored != 8 || got.Unranged != 9 || got.Finished != "" {
		t.Fatal(got)
	}
	p.UserEmail = "starter@example.test"
	if passView(p, "").StartedBy != p.UserEmail {
		t.Fatal("email")
	}
	p.Trigger = "run_fixture"
	if passView(p, "").StartedBy != "" {
		t.Fatal("delivery")
	}
	row := passRow(p)
	if row.StartedText != "2026-10-05 09:00" || row.Rows != 1 || row.Created != 6 || row.Rescored != 8 {
		t.Fatal(row)
	}
	c := store.Candidate{ID: "cand_abcdef0123456789", Status: store.StatusActive, Name: "business", Names: []string{"business", "alias"}, Street: "street", City: "city", State: "MN", Zip: "55072", County: "MN-001", Phone: "phone", Email: "email", People: []sources.Person{{Name: "person", Role: "owner"}}, Class: sources.ClassPrivate, Cargo: []string{"cargo"}, Created: now, Updated: now}
	cv := candidateView(c)
	if cv.Name != c.Name || cv.Street != c.Street || cv.City != c.City || cv.State != c.State || cv.Zip != c.Zip || cv.County != c.County || cv.Phone != c.Phone || cv.Email != c.Email || cv.Class != c.Class || !reflect.DeepEqual(cv.Names, c.Names) || !reflect.DeepEqual(cv.Cargo, c.Cargo) || !reflect.DeepEqual(cv.People, []PersonRow{{"person", "owner"}}) {
		t.Fatal(cv)
	}
	c.Status = store.StatusMerged
	c.MergedInto = "survivor"
	cv = candidateView(c)
	if cv.Name != "" || cv.Phone != "" || cv.Street != "" || cv.Class != "" || cv.MergedInto != "survivor" || cv.Names == nil || len(cv.Names) != 0 || len(cv.People) != 0 || len(cv.Cargo) != 0 {
		t.Fatal(cv)
	}
	a := store.Assessment{Fleet: true, Low: 14400000, High: &hi, Confidence: .7, Filed: now.AddDate(0, 0, -30), Minutes: &mins, Base: 7279999.6, Rules: 1, Scored: now, Exclusions: []sources.Exclusion{{Reason: "signal", Detail: "detail"}}}
	av := assessmentView(a, now)
	if av.Low != "$14,400,000" || av.High != "$24,000,000" || av.Confidence != "0.70" || av.Freshness != "1.00" || av.Effective != "0.70" || av.Stale || av.Minutes != "31" || av.Base != "$7,280,000" || !reflect.DeepEqual(av.Exclusions, []ExclusionRow{{"signal", "detail"}}) {
		t.Fatal(av)
	}
	a.Filed = now.AddDate(-6, 0, 0)
	av = assessmentView(a, now)
	if av.Effective != "0.00" || !av.Stale {
		t.Fatal(av)
	}
	sr := shortlistRow(store.Ranked{Candidate: c, Assessment: a, Effective: .7, Score: 7279999.6}, 2)
	if sr.Rank != 3 || sr.Minutes != 31 || sr.Low != "$14,400,000" || sr.High != "$24,000,000" || sr.Confidence != "0.70" || sr.Score != "$7,280,000" {
		t.Fatal(sr)
	}
	e := estimateView(sources.Estimate{Source: "source", Low: 999, Confidence: .7, Facts: map[string]json.RawMessage{"z": json.RawMessage(` [ 1, 2 ] `), "a": json.RawMessage(` { "k": true } `)}})
	if !reflect.DeepEqual(e.Facts, []FactRow{{"a", `{"k":true}`}, {"z", `[1,2]`}}) || e.High != "" || e.Filed != "" {
		t.Fatal(e)
	}
	l := linkView(store.Link{Scraper: "source", SourceID: "a/b", Run: "run", Reading: sources.Reading{Name: "name", City: "city", Zip: "55072", Filed: now}, Confidence: .8, FirstSeen: now, LastSeen: now, Gone: true}, "")
	if l.RowPage != "" || l.Name != "name" || l.Confidence != "0.80" || l.Filed != "2026-10-05" || !l.Gone {
		t.Fatal(l)
	}
	r := store.Ruling{Kind: store.KindMerge, Candidate: "survivor", Other: "merged", Keys: []string{"key"}, Why: "reason", UserID: "owner", UserEmail: "owner@example.test", At: now}
	rv := rulingView(r, "merged")
	if rv.Other != "survivor" || rv.By != r.UserEmail || rv.User != r.UserID || rv.Why != r.Why || rv.AtText != "2026-10-05 09:00" {
		t.Fatal(rv)
	}
	if rulingView(r, "survivor").Other != "merged" {
		t.Fatal("other")
	}
}

// R-4RYR-TOLB R-4T6O-7GC0 R-4UEK-L82P R-4VMG-YZTE R-4Y29-QJAS R-4ZA6-4B1H
func TestTemplateDataShape(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	b := page.Banner{Service: ServiceName}
	cv := CandidateView{"id", store.StatusActive, "", "name", []string{"alias"}, "street", "city", "MN", "55072", "MN-001", "phone", "email", []PersonRow{{"person", "role"}}, "class", []string{"cargo"}, "", "", "", ""}
	av := AssessmentView{true, "31", "low", "high", "confidence", "", "", "freshness", "effective", false, "point", "base", "score", false, []ExclusionRow{{"reason", "detail"}}, 1, "", ""}
	ev := EstimateView{"source", "low", "high", "confidence", "", "", 1, []FactRow{{"key", "value"}}}
	lv := LinkView{"scraper", "source", "run", "", "name", "city", "55072", "reason", "confidence", "", "", "", "", "", "", false}
	rv := RulingView{"kind", "other", []string{"key"}, "by", "user", "", "", "why"}
	pv := PassView{"id", store.TriggerRebuild, "", store.StatusRunning, "", "starter", "", "", "", "", 1, 2, 3, 4, 5, 6, 7, 8, 9}
	for _, v := range []struct {
		name string
		data any
	}{{"landing", LandingData{b, 120, "min", []ShortlistRow{{1, "id", "name", "city", "MN", 31, "class", "low", "high", "confidence", "score"}}, []PassRow{{"id", "trigger", store.StatusRunning, "", "", "", "", "", 1, 2, 3}}}}, {"candidate", CandidateData{b, cv, &av, []EstimateView{ev}, []LinkView{lv}, []RulingView{rv}}}, {"pass", PassData{b, pv}}, {"tools", ToolsData{b, []ToolRow{{"name", "description"}}}}, {"about", AboutData{b, Description}}, {"notfound", NotFoundData{b}}} {
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, v.name, v.data); err != nil {
			t.Fatal(v.name, err)
		}
	}
}
