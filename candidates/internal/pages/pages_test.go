package pages

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

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
	}, set, "", s, clock}, d
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

// R-2GH2-BY8N R-2HOY-PPZC R-ISRR-T9KH R-51PY-VUIV
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
					if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "landing", LandingData{Shortlist: []ShortlistRow{{Name: "name", City: "city", State: "MN", High: "value"}, {}}, Passes: []PassRow{{Status: status, Reason: reason, Trigger: trigger, Finished: finished}}}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	for _, flag := range []bool{false, true} {
		for _, text := range []string{"", "value"} {
			data := CandidateData{Candidate: CandidateView{Status: store.StatusActive, Name: text, Names: []string{text}, People: []PersonRow{{Name: text, Role: text}}, Cargo: []string{text}}, Assessment: &AssessmentView{Fleet: flag, Stale: flag, Excluded: flag, Minutes: text, High: text, Filed: text, Exclusions: []ExclusionRow{{Reason: text, Detail: text}}}, Estimates: []EstimateView{{High: text, Filed: text, Facts: []FactRow{{Key: text, Value: text}}}}, Links: []LinkView{{RowPage: text, Filed: text, Gone: flag}}, Rulings: []RulingView{{Other: text, Keys: []string{text}, By: text, User: text}}}
			if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", data); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// R-52XV-9M9K R-8LVJ-VXKV R-5K0G-MENA R-5L8D-06DZ R-5MG9-DY4O R-5NO5-RPVD R-5OW2-5HM2 R-5Q3Y-J9CR R-5RBU-X13G R-5UZK-2CBJ
func TestStaticEmptyAndRefusals(t *testing.T) {
	cfg, d := pageFixture(t)
	b := bannerFor(cfg)
	comparePage(t, cfg, "/", "landing", 200, LandingData{Banner: b, MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: dollars(scoring.DefaultMinRevenue), Shortlist: []ShortlistRow{}, Passes: []PassRow{}})
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
	for _, p := range []string{"/", "/candidates/cand_0000000000000000", "/passes/pass_0000000000000000"} {
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
	for _, p := range []string{"/", "/tools", "/about", "/candidates/cand_0000000000000000", "/passes/pass_0000000000000000"} {
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

// R-5BH5-Y0GF R-5HKN-UV5W R-5ISK-8MWL
func TestInvalidAndMissingPaths(t *testing.T) {
	cfg, d := pageFixture(t)
	b := bannerFor(cfg)
	want := requestPage(cfg, "GET", "/unknown", "reader").Body.String()
	for _, p := range []string{"/candidates/cand_0000000000000000", "/passes/pass_0000000000000000"} {
		w := requestPage(cfg, "GET", p, "reader")
		if w.Code != 404 || w.Body.String() != want {
			t.Fatal(p)
		}
	}
	d.SetFailing(true)
	for _, p := range []string{"/candidates/cand_ABCDEF0123456789", "/candidates/cand_abcdef012345678", "/candidates/cand_abcdef01234567890", "/candidates/cand_abcdef0123456789/", "/candidates/%63and_abcdef0123456789", "/cand_abcdef0123456789", "/candidates", "/candidates/", "/passes/cand_abcdef0123456789", "/candidates/pass_abcdef0123456789", "/passes/pass_abcdef012345678", "/passes/pass_abcdef0123456789/", "/passes/"} {
		comparePage(t, cfg, p, "notfound", 404, NotFoundData{b})
	}
}

// R-7JYS-YLTJ R-7L6P-CDK8 R-8N3G-9PBK R-5SJR-ASU5 R-5TRN-OKKU
func TestStoredPages(t *testing.T) {
	cfg, _ := pageFixture(t)
	ctx := context.Background()
	hi := int64(24000000)
	obs := []store.Observation{{Scraper: "fixture", SourceID: "row/a", Digest: "digest", Reading: sources.Reading{Name: "unique-business", Names: []string{"unique-business"}, Street: "unique-street", City: "unique-city", State: "MN", Zip: "55072", Phone: "unique-phone", Email: "unique-email", People: []sources.Person{{Name: "unique-person", Role: "owner"}}, Class: sources.ClassPrivate, Fleet: true, Filed: cfg.Now(), Estimate: &sources.Estimate{Source: "fixture", Low: 14400000, High: &hi, Confidence: .7, Filed: cfg.Now(), Formula: 1, Facts: map[string]json.RawMessage{"z": json.RawMessage(`1`)}}}}}
	_, ids, err := cfg.Store.Attach(ctx, "run_fixture", false, obs)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	if _, err := cfg.Store.Rescore(ctx, ids); err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	p, err := cfg.Store.AddPass(ctx, store.Pass{Trigger: store.TriggerRebuild, UserID: "starter", UserEmail: "starter@example.test"})
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
	av := assessmentView(a, cfg.Now())
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
	ranked, err := cfg.Store.Shortlist(ctx, store.ShortlistFilter{MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: scoring.DefaultMinRevenue}, 0, ShortlistShown)
	if err != nil || len(ranked) != 1 {
		t.Fatal(ranked, err)
	}
	b.Trail = nil
	comparePage(t, cfg, "/", "landing", 200, LandingData{b, scoring.DefaultMaxMinutes, dollars(scoring.DefaultMinRevenue), []ShortlistRow{shortlistRow(ranked[0], 0)}, []PassRow{passRow(p)}})
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

// R-8LVJ-VXKV: the shortlist cap and complete retained-pass list.
func TestLandingBoundedShortlistAndAllPasses(t *testing.T) {
	cfg, _ := pageFixture(t)
	ctx := context.Background()
	hi := int64(24000000)
	for i := 0; i < 31; i++ {
		key := strconv.Itoa(i)
		reading := sources.Reading{Name: "Business " + key, Names: []string{"Business " + key}, NameKey: "business" + key, Zip: "55072", Fleet: true, Filed: cfg.Now(), Class: sources.ClassPrivate, Estimate: &sources.Estimate{Source: "fixture", Low: 14400000, High: &hi, Confidence: .7, Filed: cfg.Now(), Formula: 1}}
		_, ids, err := cfg.Store.Attach(ctx, "run_fixture", false, []store.Observation{{Scraper: "fixture", SourceID: key, Digest: key, Reading: reading}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Store.Rescore(ctx, ids); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 150; i++ {
		p, err := cfg.Store.AddPass(ctx, store.Pass{Trigger: store.TriggerRebuild, UserID: "starter"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Store.FinishPass(ctx, p.ID, store.Counts{Rows: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	ranked, err := cfg.Store.Shortlist(ctx, store.ShortlistFilter{MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: scoring.DefaultMinRevenue}, 0, 100)
	if err != nil || len(ranked) != 31 {
		t.Fatal(len(ranked), err)
	}
	ps, err := cfg.Store.ListPasses(ctx, 0, 200)
	if err != nil || len(ps) != 150 {
		t.Fatal(len(ps), err)
	}
	data := LandingData{Banner: bannerFor(cfg), MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: dollars(scoring.DefaultMinRevenue), Shortlist: []ShortlistRow{}, Passes: []PassRow{}}
	for i, e := range ranked[:30] {
		data.Shortlist = append(data.Shortlist, shortlistRow(e, i))
	}
	for _, p := range ps {
		data.Passes = append(data.Passes, passRow(p))
	}
	comparePage(t, cfg, "/", "landing", 200, data)
	// A pruned record uses exactly the ordinary unknown-page answer.
	removed, err := cfg.Store.PrunePasses(ctx, 1)
	if err != nil || len(removed) == 0 {
		t.Fatal(removed, err)
	}
	comparePage(t, cfg, "/passes/"+removed[0].ID, "notfound", 404, NotFoundData{Banner: bannerFor(cfg)})
}

// R-7JYS-YLTJ R-8N3G-9PBK: external links use the caller's configured services file.
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
	p, err := cfg.Store.AddPass(context.Background(), store.Pass{Trigger: "run_fixture", Delivery: "evt_fixture", UserID: "irrelevant", UserEmail: "irrelevant@example.test"})
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
