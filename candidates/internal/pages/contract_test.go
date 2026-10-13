package pages

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-591D-6GZ1 R-8KNN-I5U6 R-7IQW-KU2U
func TestCompleteViewContracts(t *testing.T) {
	start := time.Date(2026, 10, 5, 10, 0, 7, 0, time.FixedZone("offset", 3600))
	end := start.Add(time.Hour)
	hi := int64(24000000)
	minutes := int64(31)
	c := store.Candidate{ID: "candidate", Status: store.StatusActive, MergedInto: "survivor", Name: "name", Names: []string{"name", "alias"}, Street: "street", City: "city", State: "MN", Zip: "55072", County: "MN-001", Phone: "phone", Email: "email", People: []sources.Person{{Name: "person", Role: "owner"}, {Name: "other", Role: "officer"}}, Class: "class", Cargo: []string{"cargo", "other"}, Created: start, Updated: end}
	wantC := CandidateView{ID: "candidate", Status: store.StatusActive, MergedInto: "survivor", Name: "name", Names: []string{"name", "alias"}, Street: "street", City: "city", State: "MN", Zip: "55072", County: "MN-001", Phone: "phone", Email: "email", People: []PersonRow{{"person", "owner"}, {"other", "officer"}}, Class: "class", Cargo: []string{"cargo", "other"}, Created: "2026-10-05T09:00:07Z", CreatedText: "2026-10-05 09:00:07 UTC", Updated: "2026-10-05T10:00:07Z", UpdatedText: "2026-10-05 10:00:07 UTC"}
	if got := candidateView(c); !reflect.DeepEqual(got, wantC) {
		t.Fatalf("candidate got %#v want %#v", got, wantC)
	}
	c.Status = store.StatusMerged
	wantC = CandidateView{ID: "candidate", Status: store.StatusMerged, MergedInto: "survivor", Names: []string{}, People: []PersonRow{}, Cargo: []string{}, Created: "2026-10-05T09:00:07Z", CreatedText: "2026-10-05 09:00:07 UTC", Updated: "2026-10-05T10:00:07Z", UpdatedText: "2026-10-05 10:00:07 UTC"}
	if got := candidateView(c); !reflect.DeepEqual(got, wantC) {
		t.Fatalf("merged %#v", got)
	}
	p := store.Pass{ID: "pass", Trigger: store.TriggerRebuild, Status: store.StatusDone, Reason: "reason", Started: start, Finished: end, Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Rescored: 8, Unranged: 9, UserID: "starter", UserEmail: "starter@example.test"}
	wantP := PassView{ID: "pass", Trigger: store.TriggerRebuild, Status: store.StatusDone, Reason: "reason", StartedBy: "starter@example.test", Started: "2026-10-05T09:00:07Z", StartedText: "2026-10-05 09:00:07 UTC", Finished: "2026-10-05T10:00:07Z", FinishedText: "2026-10-05 10:00:07 UTC", Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Rescored: 8, Unranged: 9}
	if got := passView(p, ""); !reflect.DeepEqual(got, wantP) {
		t.Fatalf("pass %#v", got)
	}
	wantRow := PassRow{ID: "pass", Trigger: store.TriggerRebuild, Status: store.StatusDone, Reason: "reason", Started: "2026-10-05T09:00:07Z", StartedText: "2026-10-05 09:00", Finished: "2026-10-05T10:00:07Z", FinishedText: "2026-10-05 10:00", Rows: 1, Created: 6, Rescored: 8}
	if got := passRow(p); !reflect.DeepEqual(got, wantRow) {
		t.Fatal(got)
	}
	p.UserEmail = ""
	p.Started = time.Time{}
	p.Finished = time.Time{}
	wantP.StartedBy = "starter"
	wantP.Started = ""
	wantP.StartedText = ""
	wantP.Finished = ""
	wantP.FinishedText = ""
	if got := passView(p, ""); !reflect.DeepEqual(got, wantP) {
		t.Fatal(got)
	}
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"scrapes","url":"https://chosen-records.example.test///","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p.Trigger = "run_delivery"
	p.Started = start
	p.Finished = end
	wantP.Trigger = "run_delivery"
	wantP.RunPage = "https://chosen-records.example.test/runs/run_delivery"
	wantP.StartedBy = ""
	wantP.Started = "2026-10-05T09:00:07Z"
	wantP.StartedText = "2026-10-05 09:00:07 UTC"
	wantP.Finished = "2026-10-05T10:00:07Z"
	wantP.FinishedText = "2026-10-05 10:00:07 UTC"
	if got := passView(p, path); !reflect.DeepEqual(got, wantP) {
		t.Fatal(got)
	}
	a := store.Assessment{Fleet: true, Low: 14400000, High: &hi, Confidence: .7, Filed: start, Minutes: &minutes, Exclusions: []sources.Exclusion{{Reason: "signal", Detail: "detail"}, {Reason: "other"}}, Excluded: true, Base: 13440000.4, Rules: 7, Scored: end}
	wantA := AssessmentView{Fleet: true, Minutes: "31", Low: "$14,400,000", High: "$24,000,000", Confidence: "0.70", Filed: "2026-10-05", FiledText: "2026-10-05", Freshness: "1.00", Effective: "0.70", Point: "$19,200,000", Base: "$13,440,000", Score: "$13,440,000", Excluded: true, Exclusions: []ExclusionRow{{"signal", "detail"}, {"other", ""}}, Rules: 7, Scored: "2026-10-05T10:00:07Z", ScoredText: "2026-10-05 10:00:07 UTC"}
	if got := assessmentView(a, end); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("assessment %#v", got)
	}
	a.High = nil
	a.Minutes = nil
	a.Filed = time.Time{}
	a.Scored = time.Time{}
	a.Excluded = false
	a.Exclusions = nil
	a.Fleet = false
	wantA = AssessmentView{Low: "$14,400,000", Confidence: "0.70", Freshness: "0.00", Effective: "0.00", Stale: true, Point: "$14,400,000", Base: "$13,440,000", Score: "$0", Exclusions: []ExclusionRow{}, Rules: 7}
	if got := assessmentView(a, end); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("empty assessment %#v", got)
	}
	e := sources.Estimate{Source: "source", Low: 14400000, High: &hi, Confidence: .7, Filed: start, Formula: 8, Facts: map[string]json.RawMessage{"z": json.RawMessage(` [1, 2] `), "a": json.RawMessage(` {"k": true} `)}}
	wantE := EstimateView{Source: "source", Low: "$14,400,000", High: "$24,000,000", Confidence: "0.70", Filed: "2026-10-05", FiledText: "2026-10-05", Formula: 8, Facts: []FactRow{{"a", `{"k":true}`}, {"z", `[1,2]`}}}
	if got := estimateView(e); !reflect.DeepEqual(got, wantE) {
		t.Fatal(got)
	}
	e.High = nil
	e.Filed = time.Time{}
	e.Facts = nil
	wantE.High = ""
	wantE.Filed = ""
	wantE.FiledText = ""
	wantE.Facts = []FactRow{}
	if got := estimateView(e); !reflect.DeepEqual(got, wantE) {
		t.Fatal(got)
	}
	l := store.Link{Scraper: "scraper", SourceID: "source", Run: "run", Reason: "reason", Confidence: .8, FirstSeen: start, LastSeen: end, Gone: true, Reading: sources.Reading{Name: "name", City: "city", Zip: "55072", Filed: start}}
	wantL := LinkView{Scraper: "scraper", SourceID: "source", Run: "run", Name: "name", City: "city", Zip: "55072", Reason: "reason", Confidence: "0.80", Filed: "2026-10-05", FiledText: "2026-10-05", FirstSeen: "2026-10-05T09:00:07Z", FirstSeenText: "2026-10-05 09:00", LastSeen: "2026-10-05T10:00:07Z", LastSeenText: "2026-10-05 10:00", Gone: true}
	if got := linkView(l, ""); !reflect.DeepEqual(got, wantL) {
		t.Fatal(got)
	}
	l.FirstSeen = time.Time{}
	l.LastSeen = time.Time{}
	l.Reading.Filed = time.Time{}
	l.Gone = false
	wantL.FirstSeen = ""
	wantL.FirstSeenText = ""
	wantL.LastSeen = ""
	wantL.LastSeenText = ""
	wantL.Filed = ""
	wantL.FiledText = ""
	wantL.Gone = false
	if got := linkView(l, ""); !reflect.DeepEqual(got, wantL) {
		t.Fatal(got)
	}
	r := store.Ruling{Kind: "kind", Candidate: "candidate", Other: "other", Keys: []string{"first", "second"}, Why: "why", UserID: "user", UserEmail: "email", At: start}
	wantR := RulingView{Kind: "kind", Other: "other", Keys: []string{"first", "second"}, By: "email", User: "user", At: "2026-10-05T09:00:07Z", AtText: "2026-10-05 09:00", Why: "why"}
	if got := rulingView(r, "candidate"); !reflect.DeepEqual(got, wantR) {
		t.Fatal(got)
	}
	r.Keys = nil
	r.UserEmail = ""
	r.At = time.Time{}
	wantR.Other = "candidate"
	wantR.Keys = []string{}
	wantR.By = ""
	wantR.At = ""
	wantR.AtText = ""
	if got := rulingView(r, "other"); !reflect.DeepEqual(got, wantR) {
		t.Fatal(got)
	}
}

// R-ISRR-T9KH
func TestExplicitTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, starter := range []string{"starter@example.test", "user-id"} {
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "pass", PassData{Pass: PassView{Trigger: store.TriggerRebuild, Status: store.StatusRunning, StartedBy: starter}}); err != nil {
			t.Fatal(err)
		}
	}
	d := CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Estimates: []EstimateView{{Source: "source", Low: "$999", Confidence: "0.70", Facts: []FactRow{}}}, Rulings: []RulingView{{Kind: store.KindInclude, User: "user-id", Why: "chosen", Keys: []string{}}}}
	if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", d); err != nil {
		t.Fatal(err)
	}
}
