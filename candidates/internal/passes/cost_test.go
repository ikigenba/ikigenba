package passes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	candidates "github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// R-ZZ7H-BWIU: both runs have the same generated evidence except Run.
func generatedRows(t *testing.T, run string) []scrapes.Row {
	t.Helper()
	rows := make([]scrapes.Row, 10000)
	names := map[string]bool{}
	for i := range rows {
		k := i % 5000
		name := fmt.Sprintf("Generated Business %04d", k)
		key := sources.NameKey(name)
		if key == "" || i < 5000 && names[key] {
			t.Fatal("generated match keys are not distinct")
		}
		names[key] = true
		zip := fmt.Sprintf("%05d", k)
		if k%2 == 0 {
			zip = "55072"
		}
		rows[i] = scrapes.Row{Scraper: "fmcsa", Collected: time.Unix(100000, 0).UTC(), Names: []string{}, People: []scrapes.Person{}, Facts: map[string]json.RawMessage{}, Raw: json.RawMessage(`{}`), Run: run, SourceID: fmt.Sprintf("generated-source-%d", i), Digest: fmt.Sprintf("generated-digest-%d", i), Name: name, Address: scrapes.Address{State: "MN", Zip: zip}}
	}
	return rows
}

func costEnded(t *testing.T, f *fixture, trigger string, began time.Time) store.Pass {
	t.Helper()
	deadline := began.Add(120 * time.Second)
	ps, err := f.st.ListPasses(context.Background(), "", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, p := range ps {
		if p.Trigger == trigger {
			id = p.ID
		}
	}
	if id == "" {
		t.Fatalf("no pass recorded for %s", trigger)
	}
	for {
		if !time.Now().Before(deadline) {
			t.Fatalf("pass %s exceeded 120 seconds", trigger)
		}
		for _, event := range f.ec.Events() {
			if event.Name != trail.Finished || event.Attrs["pass"] != id {
				continue
			}
			p, err := f.st.GetPass(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("pass %s exceeded 120 seconds", trigger)
			}
			if p.Status != store.StatusDone {
				t.Fatalf("pass failed: %#v", p)
			}
			t.Logf("pass %s completed in %s", trigger, time.Since(began))
			return p
		}
		// Capture polling leaves SQLite's read pool available to the pass worker.
		// Amortize snapshot allocations over scheduler turns rather than clock waits.
		for range 1024 {
			runtime.Gosched()
		}
	}

}

// R-00FD-PO9J R-01NA-3G08 R-02V6-H7QX
func TestGeneratedRunCostChain(t *testing.T) {
	first, second := "generated-first", "generated-second"
	rows := map[string][]scrapes.Row{first: generatedRows(t, first), second: generatedRows(t, second)}
	for i := range rows[first] {
		a, b := rows[first][i], rows[second][i]
		b.Run = a.Run
		if !reflectRowsEqual(a, b) {
			t.Fatal("generated runs are not alike")
		}
	}
	current := first
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/scrapers/fmcsa/current" {
			record(w, current, 10000, store.StatusDone)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 4 || rows[parts[2]] == nil {
			http.NotFound(w, r)
			return
		}
		switch parts[3] {
		case "record":
			record(w, parts[2], 10000, store.StatusDone)
		case "rows":
			for _, row := range rows[parts[2]] {
				wire := map[string]any{"scraper": row.Scraper, "run": row.Run, "source_id": row.SourceID, "collected": row.Collected, "digest": row.Digest, "name": row.Name, "names": row.Names, "people": row.People, "facts": row.Facts, "raw": row.Raw, "address": map[string]string{"state": row.Address.State, "zip": row.Address.Zip}}
				if err := json.NewEncoder(w).Encode(wire); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}, true)
	if err := f.st.ReplaceZipDrive(context.Background(), candidates.ZipDrive()); err != nil {
		t.Fatal(err)
	}
	assertCounts := func(p store.Pass, want store.Counts) {
		t.Helper()
		got := store.Counts{Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Judged: p.Judged, Unranged: p.Unranged}
		if got != want {
			t.Fatalf("pass %s counts: got %+v, want %+v", p.Trigger, got, want)
		}
	}
	began := time.Now()
	if got := f.delivery("generated-event-first", first); got != events.OK() {
		t.Fatal(got)
	}
	assertCounts(costEnded(t, f, first, began), store.Counts{Rows: 10000, New: 10000, Created: 5000, Linked: 5000})
	current = second
	began = time.Now()
	if got := f.delivery("generated-event-second", second); got != events.OK() {
		t.Fatal(got)
	}
	assertCounts(costEnded(t, f, second, began), store.Counts{Rows: 10000, Unchanged: 10000})
	began = time.Now()
	_, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "cost-owner"})
	if err != nil {
		t.Fatal(err)
	}
	ended := costEnded(t, f, store.TriggerRebuild, began)
	var unranged int64
	cs, err := f.st.ListCandidates(context.Background(), store.CandidateFilter{}, "", "", 10000)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Status != store.StatusActive {
			continue
		}
		a, err := f.st.GetAssessment(context.Background(), c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if a.RevenueLow == nil {
			unranged++
		}
	}
	assertCounts(ended, store.Counts{Rows: 10000, Changed: 10000, Linked: 10000, Judged: 5000, Unranged: unranged})

	// The ending event has been captured and no engine work remains. Prepare
	// inputs before measuring so only each upkeep call contributes allocations.
	table := candidates.ZipDrive()
	ctx := context.Background()
	var before, after runtime.MemStats
	// R-Y4NM-OD2H
	runtime.ReadMemStats(&before)
	err = f.st.ReplaceZipDrive(ctx, table)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 4194304 {
		t.Errorf("ReplaceZipDrive allocated %d bytes; want less than 4194304", allocated)
	} else {
		t.Logf("ReplaceZipDrive allocated %d bytes", allocated)
	}
	// R-Y5VJ-24T6
	runtime.ReadMemStats(&before)
	_, err = f.st.PrunePasses(ctx, 1)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 4194304 {
		t.Errorf("PrunePasses allocated %d bytes; want less than 4194304", allocated)
	} else {
		t.Logf("PrunePasses allocated %d bytes", allocated)
	}
}

func reflectRowsEqual(a, b scrapes.Row) bool {
	// JSON compares every row field, including optional evidence fields.
	left, le := json.Marshal(a)
	right, re := json.Marshal(b)
	return le == nil && re == nil && string(left) == string(right)
}
