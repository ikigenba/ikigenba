package passes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-9UJ9-AYIO R-9VR5-OQ9D R-9WZ2-2I02
func TestPassWritesMatchDeclaredStoreSequence(t *testing.T) {
	for _, rebuild := range []bool{false, true} {
		t.Run(fmt.Sprint(rebuild), func(t *testing.T) {
			rows := make([]scrapes.Row, passes.BatchSize+3)
			for i := range rows {
				rows[i] = scrapes.Row{Scraper: "fmcsa", Run: "write-run", Collected: time.Unix(100000, 0).UTC(), SourceID: fmt.Sprintf("source-%d", i), Digest: fmt.Sprintf("digest-%d", i), Name: fmt.Sprintf("Business %d", i), Address: scrapes.Address{State: "MN", Zip: "55072"}, Names: []string{}, People: []scrapes.Person{}, Raw: json.RawMessage(`{}`), Facts: map[string]json.RawMessage{"status": json.RawMessage(`"A"`), "power_units": json.RawMessage(`"100"`), "class": json.RawMessage(`"AUTHORIZED FOR HIRE"`), "mcs150_date": json.RawMessage(`"19700101"`)}}
			}
			actual := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/runs/write-run/record" || r.URL.Path == "/scrapers/fmcsa/current" {
					record(w, "write-run", len(rows), store.StatusDone)
					return
				}
				for _, row := range rows {
					wire := map[string]any{"scraper": row.Scraper, "run": row.Run, "source_id": row.SourceID, "digest": row.Digest, "collected": row.Collected, "name": row.Name, "names": row.Names, "people": row.People, "facts": row.Facts, "raw": row.Raw, "address": map[string]string{"state": row.Address.State, "zip": row.Address.Zip}}
					if err := json.NewEncoder(w).Encode(wire); err != nil {
						return
					}
				}
			}, true)
			expected := newFixture(t, nil, false)
			ctx := context.Background()
			// Give both stores unrelated evidence: a delivery must refresh it globally.
			seed := store.Observation{Scraper: "fmcsa", SourceID: "unrelated", Digest: "unrelated", Reading: fmcsa.New().Read(scrapes.Row{Name: "Unrelated", Facts: map[string]json.RawMessage{}})}
			for _, f := range []*fixture{actual, expected} {
				if _, _, err := f.st.Attach(ctx, "previous", false, []store.Observation{seed}); err != nil {
					t.Fatal(err)
				}
			}
			trigger, delivery := "write-run", "write-event"
			if rebuild {
				trigger, delivery = store.TriggerRebuild, ""
			}
			if _, err := expected.st.AddPass(ctx, store.Pass{Trigger: trigger, Delivery: delivery, UserID: "owner"}); err != nil {
				t.Fatal(err)
			}
			var total store.Counts
			touched := map[string]bool{}
			for lo := 0; lo < len(rows); lo += passes.BatchSize {
				hi := min(lo+passes.BatchSize, len(rows))
				batch := make([]store.Observation, hi-lo)
				for i, row := range rows[lo:hi] {
					batch[i] = store.Observation{Scraper: "fmcsa", SourceID: row.SourceID, Digest: row.Digest, Reading: fmcsa.New().Read(row)}
				}
				counts, ids, err := expected.st.Attach(ctx, "write-run", rebuild, batch)
				if err != nil {
					t.Fatal(err)
				}
				total.New += counts.New
				total.Changed += counts.Changed
				total.Unchanged += counts.Unchanged
				total.Created += counts.Created
				total.Linked += counts.Linked
				for _, id := range ids {
					touched[id] = true
				}
			}
			counts, ids, err := expected.st.MarkGone(ctx, "fmcsa", "write-run")
			if err != nil {
				t.Fatal(err)
			}
			total.Gone = counts.Gone
			for _, id := range ids {
				touched[id] = true
			}
			list := make([]string, 0, len(touched))
			for id := range touched {
				list = append(list, id)
			}
			if rebuild {
				counts, err = expected.st.RescoreAll(ctx)
			} else {
				counts, err = expected.st.Rescore(ctx, list)
			}
			if err != nil {
				t.Fatal(err)
			}
			total.Rows = int64(len(rows))
			total.Rescored = counts.Rescored
			total.Unranged = counts.Unranged
			if err = expected.st.RefreshShortlist(ctx); err != nil {
				t.Fatal(err)
			}
			var p store.Pass
			if rebuild {
				p, err = actual.eng.Rebuild(ctx, identity.Caller{UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if out := actual.delivery(delivery, "write-run"); out != events.OK() {
					t.Fatal(out)
				}
				p = actual.list()[0]
			}
			p = actual.ended(p.ID)
			got := store.Counts{Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Rescored: p.Rescored, Unranged: p.Unranged}
			if p.Status != store.StatusDone || got != total {
				t.Fatal(p, total)
			}
			wantCandidates, err := expected.st.ListCandidates(ctx, store.CandidateFilter{}, "", "", len(rows)+1)
			if err != nil {
				t.Fatal(err)
			}
			gotCandidates, err := actual.st.ListCandidates(ctx, store.CandidateFilter{}, "", "", len(rows)+1)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotCandidates, wantCandidates) {
				t.Fatal("candidate records differ from pass writes")
			}
			for _, c := range wantCandidates {
				gotLinks, ge := actual.st.Links(ctx, c.ID)
				wantLinks, we := expected.st.Links(ctx, c.ID)
				if ge != nil || we != nil || !reflect.DeepEqual(gotLinks, wantLinks) {
					t.Fatal("link records differ", ge, we)
				}
				gotEst, ge := actual.st.Estimates(ctx, c.ID)
				wantEst, we := expected.st.Estimates(ctx, c.ID)
				if ge != nil || we != nil || !reflect.DeepEqual(gotEst, wantEst) {
					t.Fatal("estimate records differ", ge, we)
				}
				gotA, ge := actual.st.GetAssessment(ctx, c.ID)
				wantA, we := expected.st.GetAssessment(ctx, c.ID)
				if ge != nil || we != nil || !reflect.DeepEqual(gotA, wantA) {
					t.Fatal("assessment records differ", ge, we)
				}
			}
		})
	}
}
