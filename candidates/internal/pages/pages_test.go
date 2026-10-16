package pages

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

func fixtureTime() time.Time { return time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC) }

type sequence struct{ n uint64 }

func (s *sequence) Read(b []byte) (int, error) {
	for i := range b {
		s.n++
		b[i] = byte(s.n % 256)
	}
	if len(b) >= 8 {
		binary.LittleEndian.PutUint64(b, s.n)
	}
	return len(b), nil
}
func pageFixture(t *testing.T) (Config, *db.DB) {
	t.Helper()
	now := time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC)
	clock := func() time.Time { return now }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state.db"), Migrations: candidates.Migrations(), Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	s := store.New(d, store.Config{Now: clock, Rand: &sequence{}})
	if err := s.ReplaceZipDrive(context.Background(), candidates.ZipDrive()); err != nil {
		t.Fatal(err)
	}
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return Config{func(u page.User) page.Banner {
		return page.Banner{Service: ServiceName, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, set, "", s}, d
}
func requestPage(cfg Config, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://candidates.example.test"+path, nil)
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Email", "reader@example.test")
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	identity.Require(Handler(cfg)).ServeHTTP(w, r)
	return w
}
func bannerFor(cfg Config) page.Banner {
	return cfg.Banner(page.User{Email: "reader@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"})
}
func comparePage(t *testing.T, cfg Config, path, name string, status int, data any) {
	t.Helper()
	var expected bytes.Buffer
	set, err := page.Templates().ParseFS(candidates.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	if err = set.ExecuteTemplate(&expected, name, data); err != nil {
		t.Fatal(err)
	}
	w := requestPage(cfg, "GET", path, "reader")
	if w.Code != status || w.Body.String() != expected.String() {
		t.Fatalf("%s: status %d want %d; body equal %v", path, w.Code, status, w.Body.String() == expected.String())
	}
	if !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) {
		t.Fatal(w.Header())
	}
	h := requestPage(cfg, "HEAD", path, "reader")
	if h.Code != w.Code || !reflect.DeepEqual(h.Header(), w.Header()) || h.Body.Len() != 0 {
		t.Fatal("HEAD differs", path)
	}
	other := requestPage(cfg, "GET", path, "other")
	if other.Body.String() != w.Body.String() {
		t.Fatal("user visibility")
	}
	if len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("cookie")
	}
}

// R-AJ1T-QRI2 R-58H2-68U5 R-AJ1T-QRI2 R-51PY-VUIV
func TestTemplateSet(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct {
		name string
		data any
	}{{"landing", LandingData{}}, {"candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}}}, {"candidate", CandidateData{Candidate: CandidateView{Status: store.StatusMerged}}}, {"pass", PassData{}}, {"tools", ToolsData{}}, {"about", AboutData{Description: Description}}, {"notfound", NotFoundData{}}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		set.Write(w, r, 201, d.name, d.data)
		var want bytes.Buffer
		if err := set.templates.ExecuteTemplate(&want, d.name, d.data); err != nil {
			t.Fatal(err)
		}
		if w.Code != 201 || w.Body.String() != want.String() {
			t.Fatal(d.name)
		}
	}
	for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
		for _, reason := range []string{"", store.ReasonInterrupted, store.ReasonScrapes, store.ReasonStore} {
			for _, trigger := range []string{store.TriggerRebuild, "run_fixture"} {
				for _, finished := range []string{"", "2026-10-05T09:00:07Z"} {
					data := PassData{Pass: PassView{ID: "pass_fixture", Status: status, Reason: reason, Trigger: trigger, Finished: finished, StartedBy: "starter@example.test", RunPage: "https://records.example.test/run"}}
					if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "pass", data); err != nil {
						t.Fatal(err)
					}
					if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "landing", LandingData{CohortsPage: finished, CohortsNext: finished, PassesPage: finished, PassesNext: finished, Cohorts: []CohortRow{{Name: "name", CreatedBy: "creator", Changed: "value"}, {}}, Passes: []PassRow{{Status: status, Reason: reason, Trigger: trigger, Finished: finished}}}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	for _, flag := range []bool{false, true} {
		for _, text := range []string{"", "value"} {
			data := CandidateData{Candidate: CandidateView{Status: store.StatusActive, Name: text, Names: []string{text}, People: []PersonRow{{Name: text, Role: text}}, Cargo: []string{text}}, Assessment: &AssessmentView{FleetVerdict: sources.FleetUnknown, Excluded: flag, Minutes: text, RevenueLow: text, RevenueHigh: text, NewestFiling: text, OperatingEvidence: []EvidenceRow{{Name: text, Description: text}}}, Estimates: []EstimateView{{High: text, Filed: text, Facts: []FactRow{{Key: text, Value: text}}}}, Links: []LinkView{{RowPage: text, Filed: text, Gone: flag}}, Rulings: []RulingView{{Other: text, Keys: []string{text}, By: text, User: text}}}
			if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", data); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// R-IKZ3-GG4N R-5DCN-PBSX R-ANXF-9UGU R-AWGP-Y8NP R-5K0G-MENA R-5L8D-06DZ R-5MG9-DY4O R-B7FT-E6BY R-B8NP-RY2N R-BB3I-JHK1 R-5RBU-X13G R-5UZK-2CBJ
func TestStaticEmptyAndRefusals(t *testing.T) {
	cfg, d := pageFixture(t)
	b := bannerFor(cfg)
	comparePage(t, cfg, "/", "landing", 200, LandingData{Banner: b, Cohorts: []CohortRow{}, Passes: []PassRow{}})
	b.Trail = []page.Level{AboutLevel}
	comparePage(t, cfg, "/about", "about", 200, AboutData{b, Description})
	b.Trail = []page.Level{ToolsLevel}
	td := ToolsData{Banner: b, Tools: []ToolRow{}}
	for _, e := range tools.Catalog() {
		td.Tools = append(td.Tools, ToolRow{e.Name, e.Description})
	}
	comparePage(t, cfg, "/tools", "tools", 200, td)
	b = bannerFor(cfg)
	comparePage(t, cfg, "/unknown", "notfound", 404, NotFoundData{b})
	d.SetFailing(true)
	for _, p := range []string{"/", "/candidates/cand_0000000000000000", "/passes/pass_0000000000000000", "/cohorts/coh_0000000000000000"} {
		for _, method := range []string{"GET", "HEAD"} {
			w := requestPage(cfg, method, p, "reader")
			want := store.Unreachable + "\n"
			if method == "HEAD" {
				want = ""
			}
			if w.Code != 503 || w.Body.String() != want || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatal(p, w)
			}
		}
	}
	for _, p := range []string{"/", "/tools", "/about", "/candidates/cand_0000000000000000", "/passes/pass_0000000000000000", "/cohorts/coh_0000000000000000"} {
		w := requestPage(cfg, "POST", p, "reader")
		if w.Code != 405 || w.Body.Len() != 0 || w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatal(p, w)
		}
	}
	comparePage(t, cfg, "/tools", "tools", 200, td)
	b.Trail = []page.Level{AboutLevel}
	comparePage(t, cfg, "/about", "about", 200, AboutData{b, Description})
	b.Trail = nil
	comparePage(t, cfg, "/unknown", "notfound", 404, NotFoundData{b})
	w := requestPage(cfg, "POST", "/unknown", "reader")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

// R-5BH5-Y0GF R-B500-MMUK R-B67X-0EL9
func TestInvalidAndMissingPaths(t *testing.T) {
	cfg, d := pageFixture(t)
	b := bannerFor(cfg)
	want := requestPage(cfg, "GET", "/unknown", "reader").Body.String()
	for _, p := range []string{"/candidates/cand_0000000000000000", "/passes/pass_0000000000000000", "/cohorts/coh_0000000000000000"} {
		w := requestPage(cfg, "GET", p, "reader")
		if w.Code != 404 || w.Body.String() != want {
			t.Fatal(p)
		}
	}
	d.SetFailing(true)
	for _, p := range []string{"/candidates/cand_ABCDEF0123456789", "/candidates/cand_abcdef012345678", "/candidates/cand_abcdef01234567890", "/candidates/cand_abcdef0123456789/", "/candidates/%63and_abcdef0123456789", "/cand_abcdef0123456789", "/candidates", "/candidates/", "/passes/cand_abcdef0123456789", "/candidates/pass_abcdef0123456789", "/passes/pass_abcdef012345678", "/passes/pass_abcdef0123456789/", "/passes/", "/cohorts/coh_ABCDEF0123456789", "/cohorts/coh_abcdef012345678", "/cohorts/coh_abcdef01234567890", "/cohorts/coh_abcdef0123456789/", "/cohorts/%63oh_abcdef0123456789", "/coh_abcdef0123456789", "/cohorts", "/cohorts/", "/cohorts/cand_abcdef0123456789", "/candidates/coh_abcdef0123456789"} {
		comparePage(t, cfg, p, "notfound", 404, NotFoundData{b})
	}
}

// R-AU0X-6P6B R-TMLB-KP7S R-AU0X-6P6B R-BH70-GC9I R-BIEW-U407
func TestStoredPages(t *testing.T) {
	cfg, _ := pageFixture(t)
	ctx := context.Background()
	hi := int64(24000000)
	obs := []store.Observation{{Scraper: "fixture", SourceID: "row/a", Digest: "digest", Reading: sources.Reading{Name: "unique-business", Names: []string{"unique-business"}, Street: "unique-street", City: "unique-city", State: "MN", Zip: "55072", Phone: "unique-phone", Email: "unique-email", People: []sources.Person{{Name: "unique-person", Role: "owner"}}, Class: sources.ClassPrivate, Fleet: true, Filed: fixtureTime(), Estimate: &sources.Estimate{Source: "fixture", Low: 14400000, High: &hi, Confidence: .7, Filed: fixtureTime(), Formula: 1, Facts: map[string]json.RawMessage{"z": json.RawMessage(`1`)}}}}}
	_, ids, err := cfg.Store.Attach(ctx, "run_fixture", false, obs)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	if _, err := cfg.Store.Judge(ctx, ids); err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	p, err := cfg.Store.AddPass(ctx, store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "starter", UserEmail: "starter@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := cfg.Store.GetCandidate(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	a, err := cfg.Store.GetAssessment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	links, err := cfg.Store.Links(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	est, err := cfg.Store.Estimates(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	rulings, err := cfg.Store.Rulings(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	b := bannerFor(cfg)
	b.Trail = []page.Level{{Name: id, URL: "/candidates/" + id}}
	av := assessmentView(a)
	cd := CandidateData{Banner: b, Candidate: candidateView(c), Assessment: &av, Estimates: []EstimateView{}, Links: []LinkView{}, Rulings: []RulingView{}}
	for _, e := range est {
		cd.Estimates = append(cd.Estimates, estimateView(e))
	}
	for _, l := range links {
		cd.Links = append(cd.Links, linkView(l, ""))
	}
	comparePage(t, cfg, "/candidates/"+id, "candidate", 200, cd)
	b.Trail = []page.Level{{Name: p.ID, URL: "/passes/" + p.ID}}
	comparePage(t, cfg, "/passes/"+p.ID, "pass", 200, PassData{b, passView(p, "")})
	cohort, err := cfg.Store.CreateCohort(ctx, store.Cohort{Name: "chosen-cohort", Size: 2, CreatedUserID: "creator"})
	if err != nil {
		t.Fatal(err)
	}
	b.Trail = nil
	comparePage(t, cfg, "/", "landing", 200, LandingData{b, []CohortRow{cohortRow(cohort)}, "", "", "", []PassRow{passRow(p)}, "", "", ""})
	landing := requestPage(cfg, "GET", "/", "reader").Body.String()
	for _, value := range []string{c.Phone, c.Email} {
		if strings.Contains(landing, value) {
			t.Fatal("private value on landing")
		}
	}
	for _, path := range []string{"/about", "/tools", "/unknown"} {
		body := requestPage(cfg, "GET", path, "reader").Body.String()
		for _, value := range []string{c.Name, c.Street, c.City, c.Phone, c.Email, c.People[0].Name} {
			if strings.Contains(body, value) {
				t.Fatal("private value", path)
			}
		}
	}
	afterC, _ := cfg.Store.GetCandidate(ctx, id)
	afterA, _ := cfg.Store.GetAssessment(ctx, id)
	afterL, _ := cfg.Store.Links(ctx, id)
	afterE, _ := cfg.Store.Estimates(ctx, id)
	afterR, _ := cfg.Store.Rulings(ctx, id)
	afterP, _ := cfg.Store.GetPass(ctx, p.ID)
	if !reflect.DeepEqual(c, afterC) || !reflect.DeepEqual(a, afterA) || !reflect.DeepEqual(links, afterL) || !reflect.DeepEqual(est, afterE) || !reflect.DeepEqual(rulings, afterR) || !reflect.DeepEqual(p, afterP) {
		t.Fatal("read changed record")
	}
	// Create another candidate and merge the first into it.
	obs[0].SourceID = "other"
	obs[0].Reading.Name = "other-business"
	obs[0].Reading.Names = []string{"other-business"}
	_, more, err := cfg.Store.Attach(ctx, "run_fixture", false, obs)
	if err != nil || len(more) != 1 {
		t.Fatal(more, err)
	}
	r, _, err := cfg.Store.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: more[0], Other: id, Why: "owner chose", UserID: "owner", UserEmail: "owner@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := cfg.Store.GetCandidate(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	b.Trail = []page.Level{{Name: id, URL: "/candidates/" + id}}
	comparePage(t, cfg, "/candidates/"+id, "candidate", 200, CandidateData{Banner: b, Candidate: candidateView(merged), Estimates: []EstimateView{}, Links: []LinkView{}, Rulings: []RulingView{rulingView(r, id)}})
}

// R-AU0X-6P6B R-AU0X-6P6B: external links use the caller's configured services file.
func TestSiblingPageLinks(t *testing.T) {
	cfg, _ := pageFixture(t)
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"scrapes","url":"https://records.example.test///","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.ServicesPath = path
	l := linkView(store.Link{SourceID: "a/b", Run: "run_fixture"}, path)
	if l.RowPage != "https://records.example.test/runs/run_fixture/rows/a%2Fb" {
		t.Fatal(l)
	}
	p, err := cfg.Store.AddPass(context.Background(), store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "run_fixture", Delivery: "evt_fixture", UserID: "irrelevant", UserEmail: "irrelevant@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	view := passView(p, path)
	if view.RunPage != "https://records.example.test/runs/run_fixture" || view.StartedBy != "" {
		t.Fatal(view)
	}
	b := bannerFor(cfg)
	b.Trail = []page.Level{{Name: p.ID, URL: "/passes/" + p.ID}}
	comparePage(t, cfg, "/passes/"+p.ID, "pass", 200, PassData{b, view})
}

func callPageTool(t *testing.T, client *mcp.Client, name, token string, target any) {
	t.Helper()
	args := map[string]string{}
	if token != "" {
		args["page"] = token
	}
	payload, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(t.Context(), identity.Caller{UserID: "reader"}, name, payload)
	if err != nil || result.IsError() {
		t.Fatal(name, result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	if err = json.Unmarshal(encoded, &answer); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(answer.StructuredContent, target); err != nil {
		t.Fatal(err)
	}
}

// R-ALHM-IAZG R-B04F-3JVS R-BB3I-JHK1
func TestLandingTokenRefusals(t *testing.T) {
	cfg, d := pageFixture(t)
	short := (paging.Token{Tool: "cohorts"}).Encode("chosen-name", "chosen-candidate")
	pass := (paging.Token{Tool: "passes"}).Encode("chosen-start", "chosen-pass")
	invalid := []string{"/?passes_page=LAST", "/?cohorts_page=last/", "/?cohorts_page=last&passes_page=last/", "/?cohorts_page=" + (paging.Token{Tool: "passes-before"}).Encode("start", "id"), "/?passes_page=" + (paging.Token{Tool: "cohorts-before"}).Encode("chosen-name", "id"), "/?cohorts_page=bogus", "/?passes_page=bogus", "/?cohorts_page=bogus&passes_page=bogus", "/?cohorts_page=bogus&cohorts_page=", "/?passes_page=bogus&passes_page=", "/?cohorts_page=bogus&passes_page=" + url.QueryEscape(pass), "/?cohorts_page=" + url.QueryEscape(short) + "&passes_page=bogus", "/?cohorts_page=" + url.QueryEscape(pass), "/?passes_page=" + url.QueryEscape(short)}
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		for _, method := range []string{"GET", "HEAD"} {
			want := requestPage(cfg, method, "/unknown", "reader")
			for _, path := range invalid {
				got := requestPage(cfg, method, path, "reader")
				if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
					t.Fatal(method, path, failing, got)
				}
			}
			if failing {
				for _, path := range []string{"/?cohorts_page=last", "/?passes_page=last", "/?cohorts_page=" + (paging.Token{Tool: "cohorts-before"}).Encode("chosen-name", "id"), "/?passes_page=" + (paging.Token{Tool: "passes-before"}).Encode("start", "id"), "/", "/?cohorts_page=" + url.QueryEscape(short), "/?passes_page=" + url.QueryEscape(pass), "/?cohorts_page=" + url.QueryEscape(short) + "&passes_page=" + url.QueryEscape(pass), "/?cohorts_page=&cohorts_page=bogus&passes_page=&passes_page=bogus"} {
					got := requestPage(cfg, method, path, "reader")
					body := store.Unreachable + "\n"
					if method == "HEAD" {
						body = ""
					}
					if got.Code != 503 || got.Body.String() != body || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
						t.Fatal(method, path, got)
					}
				}
			}
		}
	}
	// A nil store makes any attempted store read observable even when its
	// result would otherwise be discarded for the same not-found answer.
	cfg.Store = nil
	for _, method := range []string{"GET", "HEAD"} {
		for _, path := range invalid {
			if got := requestPage(cfg, method, path, "reader"); got.Code != 404 {
				t.Fatal(method, path, got)
			}
		}
	}
	if got := requestPage(cfg, "POST", invalid[0], "reader"); got.Code != 405 {
		t.Fatal(got)
	}
}
