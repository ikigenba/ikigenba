package passes_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-2BD1-VHD4 R-0YL2-KNPC R-0ZSY-YFG1 R-19K6-0LDL
func TestFullIngestRebuildsOwnerFoundedCandidate(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, true)
	ctx := context.Background()
	name := "Owner founded business"
	r, candidate, err := f.st.AddOwnerRecord(ctx, store.OwnerRecord{Note: "Added by owner", UserID: "owner", Set: store.OwnerValues{Name: &name, Address: &sources.Address{Street: "12 Owner Street", City: "Owner City", State: "MN", Zip: "55072"}}})
	if err != nil || !r.Founded {
		t.Fatal(r, candidate, err)
	}
	if err = f.st.ReplaceZipDrive(ctx, map[string]int64{"55072": 7}); err != nil {
		t.Fatal(err)
	}
	if err = f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE candidates SET name=?,zip=? WHERE id=?", "Stale display", "99999", candidate.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p, err := f.eng.Ingest(ctx, identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Rows != 0 || p.Judged != 1 || p.Derived != 1 {
		t.Fatal(p)
	}
	got, err := f.st.GetCandidate(ctx, candidate.ID)
	if err != nil || !reflect.DeepEqual(got, candidate) {
		t.Fatal(got, err)
	}
	links, err := f.st.Links(ctx, candidate.ID)
	if err != nil || len(links) != 0 {
		t.Fatal(links, err)
	}
	all, err := f.st.ListCandidates(ctx, store.CandidateFilter{}, "", "", 10)
	if err != nil || len(all) != 1 || all[0].ID != candidate.ID {
		t.Fatal(all, err)
	}
	es, err := f.st.Enrichments(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range es {
		if en.Kind == enrichers.KindDriveMinutes && en.Inputs != "55072" {
			t.Fatal("derived before rebuilding owner's display", en)
		}
	}
	judgments, err := f.st.GetJudgments(ctx, candidate.ID)
	if err != nil || !judgments.Calculated.Equal(p.Started) {
		t.Fatal(judgments, err)
	}
	facts, err := f.st.GetFacts(ctx, candidate.ID)
	if err != nil || facts.Minutes == nil || *facts.Minutes != 7 {
		t.Fatal(facts, err)
	}
	for _, pass := range f.list() {
		if pass.Trigger == p.ID {
			t.Fatal("full ingest started a follow-on", pass)
		}
	}
}

// R-W77N-ET0W
func TestWebsitePassStartsFromOwnerURLAndKeepsOwnerDecision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, success, from, foundState, state string
	}{
		{"missing", "", "", sources.WebsiteNone, sources.WebsiteHTTP},
		{"owner", "https://www.owner-co.test/", "http://owner-co.test/", sources.WebsiteHTTPS, sources.WebsiteHTTPS},
		{"email", "https://acme.test", "acme.test", sources.WebsiteHTTPS, sources.WebsiteHTTP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, nil, false)
			ctx := context.Background()
			name, ownerURL, email := "Owner Co", "http://owner-co.test/", "ops@acme.test"
			_, c, err := f.st.AddOwnerRecord(ctx, store.OwnerRecord{Note: "Owner's website", UserID: "owner", Set: store.OwnerValues{Name: &name, Website: &ownerURL, Email: &email}})
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var requests []string
			f.checker(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				requests = append(requests, r.URL.String())
				mu.Unlock()
				response := pageResponse(r)
				if r.URL.String() != tc.success {
					response.StatusCode = http.StatusNotFound
				}
				return response, nil
			})
			p := f.enrich(sources.WebsiteCheck)
			if p.Status != store.StatusDone || p.Due != 1 || p.Fetched != 1 {
				t.Fatal(p)
			}
			waitFollow(t, f, p.ID)
			mu.Lock()
			first := requests[0]
			mu.Unlock()
			if first != "https://owner-co.test/" {
				t.Fatal(first)
			}
			es, err := f.st.Enrichments(ctx, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			var checks []store.Enrichment
			for _, en := range es {
				if en.Kind == sources.WebsiteCheck {
					checks = append(checks, en)
				}
			}
			if len(checks) != 1 || checks[0].Inputs != ownerURL+"\nacme.test" {
				t.Fatal(checks)
			}
			var found website.Result
			if err = json.Unmarshal(checks[0].Found, &found); err != nil || found.State != tc.foundState || found.From != tc.from {
				t.Fatal(found, err)
			}
			facts, err := f.st.GetFacts(ctx, c.ID)
			if err != nil || facts.WebsiteSource != store.WebsiteByOwner || facts.WebsiteURL != ownerURL || facts.Website != tc.state {
				t.Fatal(facts, err)
			}
			records, err := f.st.OwnerRecords(ctx, c.ID)
			if err != nil || len(records) != 1 || !reflect.DeepEqual(records[0].Set.Website, &ownerURL) {
				t.Fatal(records, err)
			}
		})
	}
}
