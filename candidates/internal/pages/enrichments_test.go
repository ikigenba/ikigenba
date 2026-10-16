package pages

import (
	"bytes"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-AFKV-F72T R-AMW9-PTIZ R-AO46-3L9O
func TestEnrichmentAndWebsiteViews(t *testing.T) {
	at := fixtureTime()
	x := store.Enrichment{Kind: sources.WebsiteCheck, State: sources.WebsiteHTTPS, URL: "https://fixture.example.test/", Title: "fixture-title", StatusCode: 200, Size: 1256, Source: "fixture.example.test", At: at}
	want := EnrichmentRow{sources.WebsiteCheck, sources.WebsiteHTTPS, x.URL, x.Title, "200", 1256, "", x.Source, "2026-10-05T09:00:07Z", "2026-10-05 09:00"}
	if got := enrichmentRow(x); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	empty := store.Enrichment{Kind: sources.WebsiteCheck, State: sources.WebsiteNone, Error: "fixture-error"}
	if got := enrichmentRow(empty); !reflect.DeepEqual(got, EnrichmentRow{sources.WebsiteCheck, sources.WebsiteNone, "", "", "", 0, "fixture-error", "", "", ""}) {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		results                          []store.Enrichment
		state, url, title, checked, text string
	}{
		{nil, sources.WebsiteUnknown, "", "", "", ""},
		{[]store.Enrichment{{Kind: "other-check"}}, sources.WebsiteUnknown, "", "", "", ""},
		{[]store.Enrichment{{Kind: "other-check"}, x, empty}, sources.WebsiteHTTPS, x.URL, x.Title, "2026-10-05T09:00:07Z", "2026-10-05 09:00:07 UTC"},
		{[]store.Enrichment{{Kind: sources.WebsiteCheck, State: sources.WebsiteHTTP, URL: "http://fixture.example.test/", Title: "http-title"}}, sources.WebsiteHTTP, "http://fixture.example.test/", "http-title", "", ""},
		{[]store.Enrichment{{Kind: sources.WebsiteCheck, State: sources.WebsiteNone, URL: "https://ignore.example.test/", Title: x.Title, At: at}, x}, sources.WebsiteNone, "", x.Title, "2026-10-05T09:00:07Z", "2026-10-05 09:00:07 UTC"},
	} {
		var got CandidateView
		setWebsite(&got, tc.results)
		if got.Website != tc.state || got.WebsiteURL != tc.url || got.WebsiteTitle != tc.title || got.WebsiteChecked != tc.checked || got.WebsiteCheckedText != tc.text {
			t.Fatal(got, tc)
		}
	}
}

// R-APC2-HD0D R-AQJY-V4R2 R-AJ8K-KIAW R-AKGG-YA1L
func TestWebsiteFiltersAndStoredMember(t *testing.T) {
	f := store.CohortFilters{Website: []string{sources.WebsiteNone, sources.WebsiteHTTP}, WebsiteUnknown: true}
	if got := filtersView(f); !reflect.DeepEqual(got.Website, f.Website) || !got.WebsiteUnknown {
		t.Fatal(got)
	}
	if got := filtersView(store.CohortFilters{}); got.Website == nil || len(got.Website) != 0 || got.WebsiteUnknown {
		t.Fatal(got)
	}
	for _, state := range []string{sources.WebsiteHTTPS, sources.WebsiteUnknown} {
		url := ""
		if state == sources.WebsiteHTTPS {
			url = "https://fixture.example.test/"
		}
		got := memberRow(store.Ranked{Assessment: store.Assessment{Website: state, WebsiteURL: url}}, 0)
		if got.Website != state || got.WebsiteURL != url {
			t.Fatal(got)
		}
	}
}

func capturedCandidate(t *testing.T, cfg Config, id string) CandidateData {
	t.Helper()
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var got CandidateData
	calls := 0
	templates, err := set.templates.Funcs(template.FuncMap{"captureCandidate": func(d CandidateData) string { got = d; calls++; return "" }}).Parse(`{{define "candidate"}}{{captureCandidate .}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	captured := cfg
	captured.Pages = &Set{templates: templates}
	path := "/candidates/" + id
	if response := requestPage(captured, http.MethodGet, path, "reader"); response.Code != 200 || calls != 1 {
		t.Fatal(response.Code, calls)
	}
	comparePage(t, cfg, path, "candidate", 200, got)
	return got
}

// R-ARRV-8WHR R-ASZR-MO8G R-AQJY-V4R2 R-AECZ-1FC4
func TestCandidateEnrichmentsAndJudgedWebsite(t *testing.T) {
	cfg, _ := pageFixture(t)
	ctx := t.Context()
	_, ids, err := cfg.Store.Attach(ctx, "fixture-run", false, []store.Observation{
		{Scraper: "fixture", SourceID: "first-row", Digest: "first", Reading: sources.Reading{Name: "fixture-first", Names: []string{"fixture-first"}, NameKey: "fixture-first"}},
		{Scraper: "fixture", SourceID: "second-row", Digest: "second", Reading: sources.Reading{Name: "fixture-second", Names: []string{"fixture-second"}, NameKey: "fixture-second"}},
	})
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	id := ids[0]
	first := capturedCandidate(t, cfg, id)
	if first.Assessment != nil || first.Candidate.Website != sources.WebsiteUnknown || first.Candidate.WebsiteURL != "" || first.Candidate.WebsiteTitle != "" || first.Candidate.WebsiteChecked != "" || first.Candidate.WebsiteCheckedText != "" || first.Enrichments == nil || len(first.Enrichments) != 0 {
		t.Fatal(first)
	}
	if len(first.Links) != 1 || first.Links[0].RowPage != "" || first.Estimates == nil || first.Rulings == nil || first.Rejects == nil {
		t.Fatal(first)
	}
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	if err = os.WriteFile(servicesPath, []byte(`{"services":[{"name":"scrapes","url":"https://fixture-records.example.test/","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.ServicesPath = servicesPath
	if _, err = cfg.Store.JudgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	cohort, err := cfg.Store.CreateCohort(ctx, store.Cohort{Name: "fixture-cohort", Size: 10, CreatedUserID: "fixture-owner"})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{store.KindReject, store.KindUnreject} {
		if _, err = cfg.Store.Reject(ctx, store.Reject{Kind: kind, Cohort: cohort.ID, Candidate: id, UserID: "fixture-owner", Why: "fixture-reason"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, x := range []store.Enrichment{
		{Candidate: id, Kind: sources.WebsiteCheck, State: sources.WebsiteNone, Error: "fixture-error", Source: "fixture.example.test"},
		{Candidate: id, Kind: sources.WebsiteCheck, State: sources.WebsiteHTTPS, URL: "https://fixture.example.test/", Title: "fixture-title", StatusCode: 200, Size: 1256, Source: "fixture.example.test"},
	} {
		if _, err = cfg.Store.AddEnrichments(ctx, []store.Enrichment{x}); err != nil {
			t.Fatal(err)
		}
	}
	es, err := cfg.Store.Enrichments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got := capturedCandidate(t, cfg, id)
	candidate, err := cfg.Store.GetCandidate(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	expected := candidateView(candidate)
	setWebsite(&expected, es)
	if !reflect.DeepEqual(got.Candidate, expected) || len(got.Enrichments) != 2 || len(got.Rejects) != 2 {
		t.Fatal(got)
	}
	for i, x := range es {
		if !reflect.DeepEqual(got.Enrichments[i], enrichmentRow(x)) {
			t.Fatal(got.Enrichments)
		}
	}
	if len(got.Links) != 1 || got.Links[0].RowPage != "https://fixture-records.example.test/runs/fixture-run/rows/first-row" {
		t.Fatal(got.Links)
	}
	if got.Enrichments[0].State != sources.WebsiteHTTPS || got.Enrichments[1].State != sources.WebsiteNone {
		t.Fatal(got.Enrichments)
	}
	view := pageCohort(t, cfg, cohort.ID)
	if len(view.Members) != 2 {
		t.Fatal(view)
	}
	for _, m := range view.Members {
		if m.Website != sources.WebsiteUnknown || m.WebsiteURL != "" {
			t.Fatal(m)
		}
	}
	if _, _, err = cfg.Store.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[1], Other: id, UserID: "fixture-owner", Why: "fixture-merge"}); err != nil {
		t.Fatal(err)
	}
	merged := capturedCandidate(t, cfg, id)
	c, err := cfg.Store.GetCandidate(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(merged.Candidate, candidateView(c)) || merged.Assessment != nil || merged.Estimates == nil || len(merged.Estimates) != 0 || merged.Links == nil || len(merged.Links) != 0 || merged.Enrichments == nil || len(merged.Enrichments) != 0 || len(merged.Rulings) != 1 || len(merged.Rejects) != 2 {
		t.Fatal(merged)
	}
	b := bannerFor(cfg)
	b.Trail = []page.Level{{Name: id, URL: "/candidates/" + id}}
	if !reflect.DeepEqual(merged.Banner, b) {
		t.Fatal(merged.Banner)
	}
}

// R-AI0O-6QK7 R-838Y-808J R-AU7O-0FZ5
func TestEnrichPassViews(t *testing.T) {
	cfg, _ := pageFixture(t)
	for _, tc := range []struct{ origin, email, user, by string }{
		{store.OriginEnrich, "fixture@example.test", "fixture-user", "fixture@example.test"},
		{store.OriginEnrich, "", "fixture-user", "fixture-user"},
		{store.OriginPass, "fixture@example.test", "fixture-user", ""},
	} {
		trigger := store.TriggerEnrich
		if tc.origin == store.OriginPass {
			trigger = "pass_0123456789abcdef"
		}
		p, err := cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindEnrich, Origin: tc.origin, Trigger: trigger, UserID: tc.user, UserEmail: tc.email})
		if err != nil {
			t.Fatal(err)
		}
		p, err = cfg.Store.FinishPass(t.Context(), p.ID, store.Counts{Due: 6, Fetched: 5, HTTPS: 3, HTTP: 2, None: 1})
		if err != nil {
			t.Fatal(err)
		}
		v := passView(p, "")
		if v.StartedBy != tc.by || v.RunPage != "" || v.Trigger != trigger || v.Due != 6 || v.Fetched != 5 || v.HTTPS != 3 || v.HTTP != 2 || v.None != 1 {
			t.Fatal(v)
		}
		b := bannerFor(cfg)
		b.Trail = []page.Level{{Name: p.ID, URL: "/passes/" + p.ID}}
		comparePage(t, cfg, "/passes/"+p.ID, "pass", 200, PassData{b, v})
	}
}

// R-ALOD-C1SA
func TestWebsiteTemplateStates(t *testing.T) {
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
	for _, state := range []string{sources.WebsiteHTTPS, sources.WebsiteHTTP, sources.WebsiteNone, sources.WebsiteUnknown} {
		url := ""
		if state == sources.WebsiteHTTPS || state == sources.WebsiteHTTP {
			url = "https://fixture.example.test/"
		}
		for _, text := range []string{"", "fixture-value"} {
			for _, status := range []string{store.StatusActive, store.StatusMerged} {
				for _, full := range []bool{false, true} {
					d := CandidateData{Candidate: CandidateView{Status: status, Website: state, WebsiteURL: url, WebsiteTitle: text, WebsiteChecked: text, WebsiteCheckedText: text}}
					if full {
						d.Enrichments = []EnrichmentRow{{State: state, URL: text, Title: text, StatusCode: text, Error: text, At: text, AtText: text, Domain: text}}
					}
					render("candidate", d)
				}
			}
			for _, websites := range [][]string{{}, {sources.WebsiteHTTP}, {sources.WebsiteNone, sources.WebsiteHTTP}} {
				for _, unknown := range []bool{false, true} {
					render("cohort", CohortData{Cohort: CohortView{Filters: FiltersView{Website: websites, WebsiteUnknown: unknown}}, Members: []MemberRow{{Website: state, WebsiteURL: url}}})
				}
			}
		}
	}
	for _, count := range []int64{0, 7} {
		for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
			for _, reason := range []string{store.ReasonScrapes, store.ReasonInterrupted, store.ReasonStore} {
				for _, finished := range []string{"", "fixture-finished"} {
					for _, v := range []PassView{
						{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, StartedBy: "fixture@example.test"},
						{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, StartedBy: "fixture-user"},
						{Kind: store.KindEnrich, Origin: store.OriginPass, Trigger: "pass_0123456789abcdef"},
						{Kind: store.KindJudge, Origin: store.OriginPass, Trigger: "pass_0123456789abcdef"},
					} {
						v.Due, v.Fetched, v.HTTPS, v.HTTP, v.None = count, count, count, count, count
						v.Status, v.Reason, v.Finished, v.FinishedText = status, reason, finished, finished
						render("pass", PassData{Pass: v})
						render("landing", LandingData{Passes: []PassRow{{Kind: v.Kind, Trigger: v.Trigger, Status: status, Reason: reason, Finished: finished, FinishedText: finished}}})
					}
				}
			}
		}
	}
}
