package pages

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-BHOJ-DFI5 R-AJ8K-KIAW R-BIWF-R78U R-AQD8-1DY8 R-AK9Q-4J8R R-AST0-SXFM
func TestCohortViewContracts(t *testing.T) {
	low, high, minutes, days := int64(5000000), int64(9000000), int64(120), int64(730)
	government := false
	at := fixtureTime()
	f := store.CohortFilters{RevenueMin: &low, RevenueMax: &high, RevenueUnknown: true, MaxMinutes: &minutes, MinutesUnknown: true, Categories: []string{sources.CategoryConstruction, sources.CategoryTrucking}, CategoryUnknown: true, Fleet: sources.FleetPass, FleetUnknown: true, Government: &government, MaxFilingAgeDays: &days, FilingUnknown: true, Class: sources.ClassPrivate, ClassUnknown: true, ShowExcluded: true}
	wantF := FiltersView{"$5,000,000", "$9,000,000", true, "120", true, []string{sources.CategoryConstruction, sources.CategoryTrucking}, true, sources.FleetPass, true, []string{}, false, "false", "730", true, sources.ClassPrivate, true, true}
	if got := filtersView(f); !reflect.DeepEqual(got, wantF) {
		t.Fatalf("filters got %#v want %#v", got, wantF)
	}
	government = true
	wantF.Government = "true"
	if got := filtersView(f); !reflect.DeepEqual(got, wantF) {
		t.Fatal(got)
	}
	if got := filtersView(store.CohortFilters{}); !reflect.DeepEqual(got, FiltersView{Categories: []string{}, Website: []string{}}) {
		t.Fatal(got)
	}
	c := store.Cohort{ID: "chosen-cohort", Name: "chosen-name", Size: 2, Filters: f, Order: []store.CohortOrder{{Field: store.OrderRevenueLow, Direction: store.DirectionAsc}, {Field: store.OrderKnownSince, Direction: store.DirectionDesc}}, CreatedUserID: "creator", CreatedUserEmail: "creator@example.test", Created: at, ChangedUserID: "editor", ChangedUserEmail: "editor@example.test", Changed: at.Add(time.Hour)}
	want := CohortView{"chosen-cohort", "chosen-name", 2, wantF, []OrderRow{{store.OrderRevenueLow, store.DirectionAsc}, {store.OrderKnownSince, store.DirectionDesc}}, "creator@example.test", "creator", "2026-10-05T09:00:07Z", "2026-10-05 09:00:07 UTC", "editor@example.test", "editor", "2026-10-05T10:00:07Z", "2026-10-05 10:00:07 UTC"}
	if got := cohortView(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("cohort got %#v want %#v", got, want)
	}
	if got := cohortRow(c); !reflect.DeepEqual(got, CohortRow{"chosen-cohort", "chosen-name", 2, "creator@example.test", "creator", "2026-10-05T10:00:07Z", "2026-10-05 10:00"}) {
		t.Fatal(got)
	}
	c.CreatedUserEmail = ""
	c.Changed = time.Time{}
	c.Created = time.Time{}
	c.Order = nil
	if got := cohortRow(c); got.CreatedBy != "" || got.CreatedUser != "creator" || got.Changed != "" || got.ChangedText != "" {
		t.Fatal(got)
	}
	if got := cohortView(c); got.Created != "" || got.CreatedText != "" || got.Changed != "" || got.ChangedText != "" || !reflect.DeepEqual(got.Order, []OrderRow{}) {
		t.Fatal(got)
	}
	r := store.Reject{Kind: store.KindReject, Cohort: "cohort", CohortName: "cohort-name", Candidate: "candidate", UserID: "user", UserEmail: "email", At: at, Why: "reason"}
	if got := rejectRow(r); !reflect.DeepEqual(got, RejectRow{"candidate", "email", "user", "2026-10-05T09:00:07Z", "2026-10-05 09:00", "reason"}) {
		t.Fatal(got)
	}
	if got := rejectRecordRow(r); !reflect.DeepEqual(got, RejectRecordRow{store.KindReject, "cohort", "cohort-name", "email", "user", "2026-10-05T09:00:07Z", "2026-10-05 09:00", "reason"}) {
		t.Fatal(got)
	}
	r.Kind = store.KindUnreject
	r.CohortName = ""
	r.UserEmail = ""
	r.At = time.Time{}
	if got := rejectRecordRow(r); !reflect.DeepEqual(got, RejectRecordRow{store.KindUnreject, "cohort", "", "", "user", "", "", "reason"}) {
		t.Fatal(got)
	}
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	d := CohortData{page.Banner{}, want, 5, 0, []MemberRow{{1, "id", "name", "city", "state", "operating", "low", "high", "confidence", []string{"category"}, "chosen-classifier", "class", "verdict", "chosen-website", "https://chosen.example.test/", "site-text-at", "site-text-text", "brief-at", "brief-text", "minutes", "government", "since", "since-text", "filing", "filing-text", true}}, []RejectRow{rejectRow(r)}}
	if d.Members[0].Website != "chosen-website" || d.Members[0].WebsiteURL != "https://chosen.example.test/" {
		t.Fatal("member positional shape")
	}
	if err = set.templates.ExecuteTemplate(&bytes.Buffer{}, "cohort", d); err != nil {
		t.Fatal(err)
	}
}

func pageCohort(t *testing.T, cfg Config, id string) CohortData {
	t.Helper()
	v, err := cfg.Store.ReadCohort(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	b := bannerFor(cfg)
	b.Trail = []page.Level{{Name: id, URL: "/cohorts/" + id}}
	d := CohortData{Banner: b, Cohort: cohortView(v.Cohort), Matching: v.Matching, Classified: v.Classified, Members: []MemberRow{}, Rejects: []RejectRow{}}
	for i, m := range v.Members {
		d.Members = append(d.Members, memberRow(m, i))
	}
	for _, r := range v.Rejects {
		d.Rejects = append(d.Rejects, rejectRow(r))
	}
	comparePage(t, cfg, "/cohorts/"+id, "cohort", 200, d)
	return d
}

// R-AV8T-KGX0 R-BLC8-IQQ8 R-GT5X-8L24 R-GUDT-MCST R-4D28-OS0E R-BIEW-U407 R-BH70-GC9I
func TestCohortAndRejectHistoryPages(t *testing.T) {
	cfg, _ := pageFixture(t)
	ctx := t.Context()
	cohort, err := cfg.Store.CreateCohort(ctx, store.Cohort{Name: "chosen-cohort", Size: 2, CreatedUserID: "creator"})
	if err != nil {
		t.Fatal(err)
	}
	empty := pageCohort(t, cfg, cohort.ID)
	if empty.Matching != 0 || len(empty.Members) != 0 || len(empty.Rejects) != 0 {
		t.Fatal(empty)
	}
	ids := []string{}
	for i := 0; i < 5; i++ {
		key := strconv.Itoa(i)
		_, created, e := cfg.Store.Attach(ctx, "chosen-run", false, []store.Observation{{Scraper: "chosen", SourceID: key, Digest: key, Reading: sources.Reading{Name: "business-" + key, Names: []string{"business-" + key}, NameKey: "business-" + key, Phone: "private-phone-" + key, Email: "private-email-" + key}}})
		if e != nil || len(created) != 1 {
			t.Fatal(created, e)
		}
		ids = append(ids, created[0])
	}
	if _, err = cfg.Store.JudgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	full := pageCohort(t, cfg, cohort.ID)
	if full.Matching != 5 || len(full.Members) != 2 || full.Members[0].Rank != 1 || full.Members[1].Rank != 2 {
		t.Fatal(full)
	}
	id := full.Members[0].ID
	for _, kind := range []string{store.KindReject, store.KindUnreject, store.KindReject} {
		if _, err = cfg.Store.Reject(ctx, store.Reject{Kind: kind, Cohort: cohort.ID, Candidate: id, Why: "chosen-" + kind, UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
		pageCohort(t, cfg, cohort.ID)
	}
	// Candidate pages retain every record, including after deletion and merge.
	checkCandidate := func() {
		t.Helper()
		c, e := cfg.Store.GetCandidate(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		rs, e := cfg.Store.Rejects(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		b := bannerFor(cfg)
		b.Trail = []page.Level{{Name: id, URL: "/candidates/" + id}}
		d := CandidateData{Banner: b, Candidate: candidateView(c), Estimates: []EstimateView{}, Links: []LinkView{}, Rulings: []RulingView{}, Rejects: []RejectRecordRow{}}
		for _, r := range rs {
			d.Rejects = append(d.Rejects, rejectRecordRow(r))
		}
		if len(d.Rejects) != 3 {
			t.Fatal(d.Rejects)
		}
		rulings, e := cfg.Store.Rulings(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range rulings {
			d.Rulings = append(d.Rulings, rulingView(r, id))
		}
		if c.Status == store.StatusActive {
			d.Candidate.Website = sources.WebsiteUnknown
			a, e := cfg.Store.GetAssessment(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			av := assessmentView(a)
			d.Assessment = &av
			links, e := cfg.Store.Links(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			for _, l := range links {
				d.Links = append(d.Links, linkView(l, ""))
			}
		}
		comparePage(t, cfg, "/candidates/"+id, "candidate", 200, d)
	}
	checkCandidate()
	for _, path := range []string{"/", "/cohorts/" + cohort.ID} {
		body := requestPage(cfg, "GET", path, "reader").Body.String()
		for i := 0; i < 5; i++ {
			for _, value := range []string{"private-phone-" + strconv.Itoa(i), "private-email-" + strconv.Itoa(i)} {
				if strings.Contains(body, value) {
					t.Fatal("private value", path, value)
				}
			}
		}
	}
	before, err := cfg.Store.GetCohort(ctx, cohort.ID)
	if err != nil {
		t.Fatal(err)
	}
	rejects, err := cfg.Store.Rejects(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cohorts, err := cfg.Store.ListCohorts(ctx, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	pageCohort(t, cfg, cohort.ID)
	after, err := cfg.Store.GetCohort(ctx, cohort.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterRejects, err := cfg.Store.Rejects(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	afterCohorts, err := cfg.Store.ListCohorts(ctx, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(rejects, afterRejects) || !reflect.DeepEqual(cohorts, afterCohorts) {
		t.Fatal("page changed cohort records")
	}
	if _, err = cfg.Store.DeleteCohort(ctx, cohort.ID); err != nil {
		t.Fatal(err)
	}
	comparePage(t, cfg, "/cohorts/"+cohort.ID, "notfound", 404, NotFoundData{bannerFor(cfg)})
	checkCandidate()
	survivor := ids[0]
	if survivor == id {
		survivor = ids[1]
	}
	if _, _, err = cfg.Store.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: id, Why: "chosen-merge", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	checkCandidate()
}

// R-3TJU-KG5A
func TestCohortAndRejectTemplateStates(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, data any) {
		t.Helper()
		if e := set.templates.ExecuteTemplate(&bytes.Buffer{}, name, data); e != nil {
			t.Fatal(name, e)
		}
	}
	for _, text := range []string{"", "chosen-value"} {
		for _, flag := range []bool{false, true} {
			for _, government := range []string{"", "true", "false"} {
				for _, categories := range [][]string{{}, {"chosen-one"}, {"chosen-one", "chosen-two"}} {
					for _, fleet := range []string{sources.FleetPass, sources.FleetFail, sources.FleetUnknown} {
						for _, high := range []string{"", "chosen-high"} {
							for _, orders := range [][]OrderRow{{{store.OrderRevenueLow, store.DirectionAsc}}, {{store.OrderRevenueLow, store.DirectionAsc}, {store.OrderKnownSince, store.DirectionDesc}}} {
								f := FiltersView{RevenueMin: text, RevenueMax: text, RevenueUnknown: flag, MaxMinutes: text, MinutesUnknown: flag, Categories: categories, CategoryUnknown: flag, Fleet: text, FleetUnknown: flag, Government: government, MaxFilingAgeDays: text, FilingUnknown: flag, Class: text, ClassUnknown: flag, ShowExcluded: flag}
								c := CohortView{ID: "chosen-cohort", Name: text, Size: 2, Filters: f, Order: orders, CreatedBy: text, CreatedUser: "chosen-creator", Created: text, CreatedText: text, ChangedBy: text, ChangedUser: "chosen-editor", Changed: text, ChangedText: text}
								d := CohortData{Cohort: c, Members: []MemberRow{}, Rejects: []RejectRow{}}
								render("cohort", d)
								d.Matching = 5
								d.Members = []MemberRow{{Rank: 1, ID: "chosen-candidate", Name: text, City: text, State: text, OperatingConfidence: text, RevenueLow: text, RevenueHigh: high, RevenueConfidence: text, Categories: categories, Class: text, FleetVerdict: fleet, Minutes: text, GovernmentMatch: text, KnownSince: text, KnownSinceText: text, NewestFiling: text, NewestFilingText: text, Excluded: flag}}
								d.Rejects = []RejectRow{{Candidate: "chosen-reject", By: text, User: "chosen-user", At: text, AtText: text, Why: text}}
								render("cohort", d)
								for _, status := range []string{store.StatusActive, store.StatusMerged} {
									for _, kind := range []string{store.KindReject, store.KindUnreject} {
										render("candidate", CandidateData{Facts: &FactsView{FleetVerdict: fleet, PowerUnits: text, Drivers: text}, Candidate: CandidateView{Status: status}, Assessment: &AssessmentView{}, Rejects: []RejectRecordRow{{Kind: kind, Cohort: "chosen-cohort", CohortName: text, By: text, User: "chosen-user", At: text, AtText: text, Why: text}}})
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// R-BH70-GC9I
func TestPagesLeaveEveryRecordUnchanged(t *testing.T) {
	cfg, _ := pageFixture(t)
	var requests atomic.Int64
	sibling := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	socket := filepath.Join(t.TempDir(), "scrapes.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	sibling.Listener = listener
	sibling.Start()
	defer sibling.Close()
	servicePath := filepath.Join(t.TempDir(), "services.json")
	services, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "scrapes", "url": "https://chosen-records.example.test", "socket": socket, "enabled": true, "mcp": false}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(servicePath, services, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.ServicesPath = servicePath
	ctx := t.Context()
	hi := int64(24000)
	_, ids, err := cfg.Store.Attach(ctx, "chosen-run", false, []store.Observation{{Scraper: "chosen", SourceID: "chosen-row", Digest: "chosen-digest", Reading: sources.Reading{Name: "chosen-business", Names: []string{"chosen-business"}, NameKey: "chosen-business", Estimate: &sources.Estimate{Source: "chosen", Low: 12000, High: &hi, Confidence: .7, Formula: 1}}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	if _, err = cfg.Store.JudgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	c, err := cfg.Store.CreateCohort(ctx, store.Cohort{Name: "chosen-cohort", Size: 2, CreatedUserID: "creator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.Reject(ctx, store.Reject{Kind: store.KindReject, Cohort: c.ID, Candidate: id, Why: "chosen-why", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	p, err := cfg.Store.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "chosen-run", Delivery: "chosen-delivery"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() []any {
		t.Helper()
		out := []any{}
		add := func(value any, e error) {
			t.Helper()
			if e != nil {
				t.Fatal(e)
			}
			out = append(out, value)
		}
		add(cfg.Store.GetCandidate(ctx, id))
		add(cfg.Store.Links(ctx, id))
		add(cfg.Store.Estimates(ctx, id))
		add(cfg.Store.GetAssessment(ctx, id))
		add(cfg.Store.Rulings(ctx, id))
		add(cfg.Store.Rejects(ctx, id))
		add(cfg.Store.GetCohort(ctx, c.ID))
		add(cfg.Store.ListCohorts(ctx, "", "", 100))
		add(cfg.Store.ListPasses(ctx, "", "", 100))
		add(cfg.Store.GetPass(ctx, p.ID))
		return out
	}
	before := snapshot()
	for _, path := range []string{"/", "/cohorts/" + c.ID, "/candidates/" + id, "/passes/" + p.ID, "/about", "/tools", "/unknown"} {
		for _, method := range []string{"GET", "HEAD"} {
			got := requestPage(cfg, method, path, "reader")
			if got.Code != 200 && got.Code != 404 {
				t.Fatal(path, got.Code)
			}
			if len(got.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("cookie", path)
			}
		}
	}
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("page changed stored records")
	}
	if requests.Load() != 0 {
		t.Fatal("page called scrapes", requests.Load())
	}
}
