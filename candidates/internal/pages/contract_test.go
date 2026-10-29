package pages

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-9M7V-YK4E R-QF0J-87M4 R-08PY-S5TE
func TestCompleteViewContracts(t *testing.T) {
	start := time.Date(2026, 10, 5, 10, 0, 7, 0, time.FixedZone("offset", 3600))
	end := start.Add(time.Hour)
	hi := int64(24000000)
	c := store.Candidate{ID: "candidate", Status: store.StatusActive, MergedInto: "survivor", Name: "name", Street: "street", City: "city", State: "MN", Zip: "55072", County: "MN-001", Phone: "phone", Email: "email", People: []sources.Sourced[sources.Person]{{Value: sources.Person{Name: "person", Role: "owner"}}, {Value: sources.Person{Name: "other", Role: "officer"}}}, Class: "class", Cargo: []string{"cargo", "other"}, Created: start, Updated: end}
	wantC := CandidateView{OtherEmails: []OtherRow{}, OwnerSet: map[string]OwnerNoteView{}, ID: "candidate", Status: store.StatusActive, MergedInto: "survivor", Name: "name", Street: "street", City: "city", State: "MN", Zip: "55072", County: "MN-001", Phone: "phone", Email: "email", OtherNames: []OtherRow{}, OtherAddresses: []OtherRow{}, OtherPhones: []OtherRow{}, People: []PersonRow{{"person", "owner", ""}, {"other", "officer", ""}}, Class: "class", Cargo: []string{"cargo", "other"}, Created: "2026-10-05T09:00:07Z", CreatedText: "2026-10-05 09:00:07 UTC", Updated: "2026-10-05T10:00:07Z", UpdatedText: "2026-10-05 10:00:07 UTC"}
	if got := candidateView(c); !reflect.DeepEqual(got, wantC) {
		t.Fatalf("candidate got %#v want %#v", got, wantC)
	}
	c.Status = store.StatusMerged
	wantC = CandidateView{OtherEmails: []OtherRow{}, OwnerSet: map[string]OwnerNoteView{}, ID: "candidate", Status: store.StatusMerged, MergedInto: "survivor", OtherNames: []OtherRow{}, OtherAddresses: []OtherRow{}, OtherPhones: []OtherRow{}, People: []PersonRow{}, Cargo: []string{}, Created: "2026-10-05T09:00:07Z", CreatedText: "2026-10-05 09:00:07 UTC", Updated: "2026-10-05T10:00:07Z", UpdatedText: "2026-10-05 10:00:07 UTC"}
	if got := candidateView(c); !reflect.DeepEqual(got, wantC) {
		t.Fatalf("merged %#v", got)
	}
	p := store.Pass{ID: "pass", Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, Status: store.StatusDone, Reason: "reason", Started: start, Finished: end, Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, UserID: "starter", UserEmail: "starter@example.test"}
	wantP := PassView{Parks: []WalkParkRow{}, Cost: "$0.000000", ID: "pass", Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, Status: store.StatusDone, Reason: "reason", StartedBy: "starter@example.test", Started: "2026-10-05T09:00:07Z", StartedText: "2026-10-05 09:00:07 UTC", Finished: "2026-10-05T10:00:07Z", FinishedText: "2026-10-05 10:00:07 UTC", Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9}
	if got := passView(p, ""); !reflect.DeepEqual(got, wantP) {
		t.Fatalf("pass %#v", got)
	}
	wantRow := PassRow{ID: "pass", Kind: store.KindIngest, Trigger: store.TriggerIngest, Status: store.StatusDone, Reason: "reason", Started: "2026-10-05T09:00:07Z", StartedText: "2026-10-05 09:00", Finished: "2026-10-05T10:00:07Z", FinishedText: "2026-10-05 10:00", Rows: 1, Created: 6, Judged: 8}
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
	p.Origin = store.OriginRun
	wantP.Origin = store.OriginRun
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
	op, rc, low := .776, .7, int64(14400000)
	a := store.Judgments{Operating: &op, Evidence: []sources.Evidence{{Name: "signal", Source: "source", Weight: .6, Description: "detail"}, {Name: "other", Weight: .3}, {Name: "third", Weight: .2}}, RevenueLow: &low, RevenueHigh: &hi, RevenueConfidence: &rc, Excluded: true, Rules: int64(sources.Rules), Calculated: end}

	wantA := JudgmentsView{OperatingConfidence: "0.78", HeadcountYears: []string{}, HeadcountSeries: []SeriesRow{}, OperatingEvidence: []EvidenceRow{{"signal", "source", "0.60", "detail"}, {"other", "", "0.30", ""}, {"third", "", "0.20", ""}}, RevenueLow: "$14,400,000", RevenueHigh: "$24,000,000", RevenueConfidence: "0.70", Excluded: true, Rules: int64(sources.Rules), Calculated: "2026-10-05T10:00:07Z", CalculatedText: "2026-10-05 10:00:07 UTC"}
	if got := judgmentsView(a); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("judgments got %#v want %#v", got, wantA)
	}
	if got := judgmentsView(store.Judgments{}); !reflect.DeepEqual(got, JudgmentsView{OperatingEvidence: []EvidenceRow{}, HeadcountYears: []string{}, HeadcountSeries: []SeriesRow{}}) {
		t.Fatalf("empty judgments %#v", got)
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
	wantL := LinkView{Names: []string{}, Scraper: "scraper", SourceID: "source", Run: "run", Name: "name", City: "city", Zip: "55072", Reason: "reason", Confidence: "0.80", Filed: "2026-10-05", FiledText: "2026-10-05", FirstSeen: "2026-10-05T09:00:07Z", FirstSeenText: "2026-10-05 09:00", LastSeen: "2026-10-05T10:00:07Z", LastSeenText: "2026-10-05 10:00", Gone: true}
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

// R-INPY-9LWV
func TestExplicitTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, starter := range []string{"starter@example.test", "user-id"} {
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "pass", PassData{Pass: PassView{Trigger: store.TriggerIngest, Status: store.StatusRunning, StartedBy: starter}}); err != nil {
			t.Fatal(err)
		}
	}
	for bits := 0; bits < 64; bits++ {
		text := func(bit int) string {
			if bits&(1<<bit) != 0 {
				return "chosen-token"
			}
			return ""
		}
		for _, prospectLists := range [][]ProspectListRow{{}, {{Name: "chosen-name", CreatedBy: "chosen-email", Changed: "chosen-at", ChangedText: "chosen-at-text"}, {CreatedUser: "chosen-user"}}} {
			for _, passes := range [][]PassRow{{}, {{Status: store.StatusRunning, Trigger: store.TriggerIngest}}} {
				data := LandingData{ProspectLists: prospectLists, ProspectListsPage: text(0), ProspectListsPrev: text(4), ProspectListsNext: text(1), Passes: passes, PassesPage: text(2), PassesPrev: text(5), PassesNext: text(3)}
				if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "landing", data); err != nil {
					t.Fatal(bits, err)
				}
			}
		}
	}
	for _, shortPage := range []string{"", "last"} {
		for _, passPage := range []string{"", "last"} {
			if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "landing", LandingData{ProspectListsPage: shortPage, PassesPage: passPage}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, status := range []string{store.StatusActive, store.StatusMerged} {
		for _, text := range []string{"", "chosen-value"} {
			for _, full := range []bool{false, true} {
				data := CandidateData{Candidate: CandidateView{OtherEmails: []OtherRow{}, OwnerSet: map[string]OwnerNoteView{}, ID: text, Status: status, MergedInto: text, Name: text, Street: text, City: text, State: text, Zip: text, County: text, Phone: text, Email: text, Class: text, Created: text, CreatedText: text, Updated: text, UpdatedText: text}}
				if full {
					data.Candidate.OtherNames = []OtherRow{{Value: text}}
					data.Candidate.People = []PersonRow{{Name: text, Role: text}}
					data.Candidate.Cargo = []string{text}
					data.Judgments = &JudgmentsView{RevenueLow: text, RevenueHigh: text, OperatingConfidence: text, RevenueConfidence: text, OperatingEvidence: []EvidenceRow{{Name: text, Source: text, Weight: text, Description: text}}}
					data.Estimates = []EstimateView{{High: text, Filed: text, Facts: []FactRow{{Key: text, Value: text}}}}
					data.Links = []LinkView{{RowPage: text, Filed: text}}
					data.Rulings = []RulingView{{Other: text, Keys: []string{text}, By: text, User: "chosen-user"}}
				}
				if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", data); err != nil {
					t.Fatal(status, text, full, err)
				}
			}
		}
	}
	for _, runPage := range []string{"", "https://chosen.example.test/run"} {
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "pass", PassData{Pass: PassView{Trigger: "run_chosen", RunPage: runPage}}); err != nil {
			t.Fatal(err)
		}
	}
	d := CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Estimates: []EstimateView{{Source: "source", Low: "$999", Confidence: "0.70", Facts: []FactRow{}}}, Rulings: []RulingView{{Kind: store.KindInclude, User: "user-id", Why: "chosen", Keys: []string{}}}}
	if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", d); err != nil {
		t.Fatal(err)
	}
}

// R-0JP2-83HN
func TestMemberRowStoredJudgments(t *testing.T) {
	minutes, low, high, op, rc := int64(84), int64(2400000), int64(3840000), .776, .4
	c := store.Candidate{ID: "chosen-id", Name: "chosen-name", City: "chosen-city", State: "MN", Class: "chosen-class"}
	a := store.Judgments{Operating: &op, RevenueLow: &low, RevenueHigh: &high, RevenueConfidence: &rc, Excluded: true}
	f := store.Facts{Minutes: &minutes, Categories: []sources.Category{{Name: sources.CategoryTrucking, Source: "fmcsa"}, {Name: sources.CategoryConstruction, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "ppp"}}, FleetVerdict: sources.FleetPass, GovernmentMatch: "chosen-government", KnownSince: fixtureTime(), NewestFiling: fixtureTime()}
	want := MemberRow{Rank: 3, ID: c.ID, Name: c.Name, City: c.City, State: c.State, Class: c.Class, OperatingConfidence: "0.78", Minutes: "84", RevenueLow: "$2,400,000", RevenueHigh: "$3,840,000", RevenueConfidence: "0.40", Categories: []string{sources.CategoryConstruction, sources.CategoryTrucking}, ClassifiedBy: enrichers.SourceLinks, FleetVerdict: sources.FleetPass, GovernmentMatch: "chosen-government", KnownSince: "2026-10-05", KnownSinceText: "2026-10-05", NewestFiling: "2026-10-05", NewestFilingText: "2026-10-05", Excluded: true}
	if got := memberRow(store.Ranked{Candidate: c, Judgments: a, Facts: f}, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	a.RevenueHigh = nil
	want.RevenueHigh = ""
	if got := memberRow(store.Ranked{Candidate: c, Judgments: a, Facts: f}, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("minimum %+v", got)
	}
	want = MemberRow{Rank: 1, ID: c.ID, Name: c.Name, City: c.City, State: c.State, Class: c.Class, Categories: []string{}}
	if got := memberRow(store.Ranked{Candidate: c}, 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown %+v", got)
	}
}

// R-QF0J-87M4 R-QPZM-O5AD
func TestPassOrigins(t *testing.T) {
	cfg, _ := pageFixture(t)
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"scrapes","url":"https://chosen-records.example.test/","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.ServicesPath = path
	for _, tc := range []struct{ kind, origin, trigger, by, run string }{
		{store.KindIngest, store.OriginIngest, store.TriggerIngest, "starter@example.test", ""},
		{store.KindJudge, store.OriginCron, store.TriggerJudge, "", ""},
		{store.KindIngest, store.OriginRun, "run_chosen", "", "https://chosen-records.example.test/runs/run_chosen"},
	} {
		input := store.Pass{Kind: tc.kind, Origin: tc.origin, Trigger: tc.trigger, UserID: "starter-id", UserEmail: "starter@example.test"}
		if tc.origin != store.OriginIngest {
			input.Delivery = "delivery-" + tc.origin
		}
		p, err := cfg.Store.AddPass(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		got := passView(p, path)
		if got.Kind != tc.kind || got.Origin != tc.origin || got.StartedBy != tc.by || got.RunPage != tc.run {
			t.Fatal(got)
		}
		b := bannerFor(cfg)
		b.Trail = []page.Level{{Name: p.ID, URL: "/passes/" + p.ID}}
		comparePage(t, cfg, "/passes/"+p.ID, "pass", 200, PassData{Banner: b, Pass: got})
		if _, err = cfg.Store.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
			t.Fatal(err)
		}
	}
}

// R-INPY-9LWV
func TestJudgmentTemplateStates(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, data any) {
		t.Helper()
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, name, data); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, value := range []string{"", "chosen-value"} {
		for _, flag := range []bool{false, true} {
			for _, categories := range [][]string{{}, {"chosen-one"}, {"chosen-one", "chosen-two"}} {
				row := MemberRow{City: value, State: value, OperatingConfidence: value, RevenueLow: value, RevenueHigh: value, RevenueConfidence: value, Categories: categories, Class: value, FleetVerdict: sources.FleetUnknown, Minutes: value, GovernmentMatch: value}
				render("prospect_list", ProspectListData{Members: []MemberRow{row}})
			}
			for _, evidence := range [][]EvidenceRow{{}, {{Name: value, Source: value, Weight: value, Description: value}}} {
				for _, categories := range [][]CategoryRow{{}, {{Name: value, Source: value}}} {
					for _, high := range []string{"", "chosen-high"} {
						d := CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Judgments: &JudgmentsView{OperatingConfidence: value, OperatingEvidence: evidence, RevenueLow: value, RevenueHigh: high, RevenueConfidence: value, Excluded: flag}, Enrichments: &EnrichmentsView{Facts: &FactsView{KnownSince: value, KnownSinceText: value, NewestFiling: value, NewestFilingText: value, Categories: categories, GovernmentMatch: value, FleetVerdict: sources.FleetUnknown, Minutes: value}}}
						render("candidate", d)
					}
				}
			}
		}
	}
	for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
		for _, reason := range []string{"", store.ReasonScrapes, store.ReasonInterrupted, store.ReasonStore} {
			for _, finished := range []string{"", "chosen-finished"} {
				for _, p := range []PassView{{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, StartedBy: "starter@example.test"}, {Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, StartedBy: "starter-id"}, {Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge}, {Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "run_chosen"}, {Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "run_chosen", RunPage: "https://chosen-records.example.test/run"}} {
					p.Status, p.Reason, p.Finished = status, reason, finished
					render("pass", PassData{Pass: p})
					render("landing", LandingData{Passes: []PassRow{{Kind: p.Kind, Trigger: p.Trigger, Status: status, Reason: reason, Finished: finished}}})
				}
			}
		}
	}
}
