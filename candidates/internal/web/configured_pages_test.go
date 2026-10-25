package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/pages"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/web"
)

// R-5MG9-DY4O
func TestConfiguredJudgedAndUnjudgedPage(t *testing.T) {
	cfg, _, _, _ := fixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"auth","url":"https://chosen-account.example.test///","description":"","socket":"","enabled":true,"mcp":false},{"name":"scrapes","url":"https://chosen-records.example.test//","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.ServicesPath = path
	hi := int64(24000000)
	reading := sources.Reading{Name: "Chosen carrier", Names: []string{"Chosen carrier"}, Street: "Chosen street", City: "Chosen city", State: "MN", Zip: "55072", Phone: "Chosen phone", Email: "Chosen email", People: []sources.Person{{Name: "Chosen person", Role: "owner"}}, Class: sources.ClassPrivate, Cargo: []string{"chosen-cargo"}, FleetVerdict: sources.FleetUnknown, Filed: fixtureTime(), Estimate: &sources.Estimate{Source: "fixture", Low: 14400000, High: &hi, Confidence: .7, Filed: fixtureTime(), Formula: 1, Facts: map[string]json.RawMessage{"chosen": json.RawMessage(` [ 1, 2 ] `)}}}
	if err := cfg.Store.ReplaceZipDrive(ctx, candidates.ZipDrive()); err != nil {
		t.Fatal(err)
	}
	_, ids, err := cfg.Store.Attach(ctx, "run_configured", false, []store.Observation{{Scraper: "fixture", SourceID: "row/a", Digest: "chosen-digest", Reading: reading}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	id := ids[0]
	users := []page.User{}
	cfg.Banner = func(u page.User) page.Banner {
		users = append(users, u)
		return page.Banner{Service: pages.ServiceName, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}
	h := web.Handler(cfg)
	for _, judged := range []bool{false, true} {
		if judged {
			if _, err := cfg.Store.Judge(ctx, ids); err != nil {
				t.Fatal(err)
			}
		}
		c, err := cfg.Store.GetCandidate(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		links, err := cfg.Store.Links(ctx, id)
		if err != nil || len(links) != 1 {
			t.Fatal(links, err)
		}
		b := page.Banner{Service: pages.ServiceName, Email: "reader@example.test", ProfileURL: "https://chosen-account.example.test/", LogoutURL: "https://chosen-account.example.test/logout", Trail: []page.Level{{Name: id, URL: "/candidates/" + id}}}
		want := pages.CandidateData{Banner: b, Enrichments: &pages.EnrichmentsView{Basis: store.BasisRecords, Kinds: pages.KindsView{Present: []string{}, Missing: []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief, enrichers.KindDriveRoute}}}, Candidate: pages.CandidateView{ID: id, Status: store.StatusActive, Name: "Chosen carrier", Names: []string{"Chosen carrier"}, Street: "Chosen street", City: "Chosen city", State: "MN", Zip: "55072", Phone: "Chosen phone", Email: "Chosen email", Website: sources.WebsiteUnknown, People: []pages.PersonRow{{Name: "Chosen person", Role: "owner"}}, Class: sources.ClassPrivate, Cargo: []string{"chosen-cargo"}, Created: c.Created.UTC().Format(time.RFC3339), CreatedText: c.Created.UTC().Format(pages.SecondLayout), Updated: c.Updated.UTC().Format(time.RFC3339), UpdatedText: c.Updated.UTC().Format(pages.SecondLayout)}, Estimates: []pages.EstimateView{}, Links: []pages.LinkView{{Scraper: "fixture", SourceID: "row/a", Run: "run_configured", RowPage: "https://chosen-records.example.test/runs/run_configured/rows/row%2Fa", Name: "Chosen carrier", Names: []string{"Chosen carrier"}, City: "Chosen city", Zip: "55072", Reason: links[0].Reason, Confidence: "1.00", Filed: "2026-10-05", FiledText: "2026-10-05", FirstSeen: "2026-10-05T09:00:07Z", FirstSeenText: "2026-10-05 09:00", LastSeen: "2026-10-05T09:00:07Z", LastSeenText: "2026-10-05 09:00"}}, Rulings: []pages.RulingView{}}
		if judged {
			want.Estimates = []pages.EstimateView{{Source: "fixture", Low: "$14,400,000", High: "$24,000,000", Confidence: "0.70", Filed: "2026-10-05", FiledText: "2026-10-05", Formula: 1, Facts: []pages.FactRow{{Key: "chosen", Value: `[1,2]`}}}}
		}
		if judged {
			a, err := cfg.Store.GetJudgments(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			want.Judgments = &pages.JudgmentsView{OperatingEvidence: []pages.EvidenceRow{}, RevenueLow: "$14,400,000", RevenueHigh: "$24,000,000", RevenueConfidence: "0.70", Rules: int64(sources.Rules), Calculated: a.Calculated.UTC().Format(time.RFC3339), CalculatedText: a.Calculated.UTC().Format(pages.SecondLayout)}
		}
		set, err := page.Templates().ParseFS(candidates.Assets(), "*.html")
		if err != nil {
			t.Fatal(err)
		}
		var expected bytes.Buffer
		if err := set.ExecuteTemplate(&expected, "candidate", want); err != nil {
			t.Fatal(err)
		}
		users = nil
		r := httptest.NewRequest("GET", "https://candidates.example.test/candidates/"+id, nil)
		r.Header.Set("X-User-Id", "reader")
		r.Header.Set("X-User-Email", "reader@example.test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.String() != expected.String() {
			t.Fatalf("judged=%v status=%d body equal=%v", judged, w.Code, w.Body.String() == expected.String())
		}
		if len(users) != 1 || users[0] != (page.User{Email: "reader@example.test", ProfileURL: "https://chosen-account.example.test/", LogoutURL: "https://chosen-account.example.test/logout"}) {
			t.Fatal(users)
		}
	}
}
