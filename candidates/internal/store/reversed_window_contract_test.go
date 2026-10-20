package store_test

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

const (
	startedLast      windowStatement = store.StartedLast
	startedPreceding windowStatement = store.StartedPreceding
)

// R-CY1Q-LSOI R-HGY1-G5D8 R-VKEI-HL3G
func TestReversedPassWindows(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	full := []store.Pass{}
	for i := range 3 {
		f.now = base
		if i == 2 {
			f.now = base.Add(5 * time.Second)
		}
		p := contractNewPass(t, f, store.TriggerIngest)
		if i == 0 {
			_, err := f.s.FinishPass(ctx, p.ID, store.Counts{Rows: 11, Created: 2})
			contractOK(t, err)
		}
		if i == 1 {
			_, err := f.s.FailPass(ctx, p.ID, store.ReasonScrapes)
			contractOK(t, err)
		}
		p, err := f.s.GetPass(ctx, p.ID)
		contractOK(t, err)
		full = append(full, p)
	}
	sort.Slice(full, func(i, j int) bool {
		if !full[i].Started.Equal(full[j].Started) {
			return full[i].Started.Before(full[j].Started)
		}
		return full[i].ID < full[j].ID
	})
	positions := [][2]string{{"", ""}, {"ignored", ""}, {"", "absent"}, {"9999", "absent"}, {base.Add(2 * time.Second).Format(time.RFC3339), full[0].ID}}
	for _, p := range full {
		positions = append(positions, [2]string{p.Started.Format(time.RFC3339), p.ID})
		positions = append(positions, [2]string{p.Started.Format(time.RFC3339), p.ID + "!"})
	}
	for _, pos := range positions {
		for _, n := range []int{1, 2, 5} {
			want := []store.Pass{}
			for _, p := range full {
				started := p.Started.Format(time.RFC3339)
				if pos[1] == "" || started > pos[0] || (started == pos[0] && p.ID > pos[1]) {
					want = append(want, p)
					if len(want) == n {
						break
					}
				}
			}
			got, err := f.s.ListPassesReversed(ctx, pos[0], pos[1], n)
			contractOK(t, err)
			contractEqual(t, got, want)
			ids := []string{}
			for _, p := range want {
				ids = append(ids, p.ID)
			}
			query, args := startedLast, []any{n}
			if pos[1] != "" {
				query, args = startedPreceding, []any{pos[0], pos[1], n}
			}
			contractEqual(t, statementIDs(t, f, query, args...), ids)
		}
	}
}

// R-W7KL-R86N
func TestReversedWindowsRejectInvalidCounts(t *testing.T) {
	f := contractDB(t)
	for _, n := range []int{0, -1, -100} {
		prospectLists, err := f.s.ListProspectListsReversed(context.Background(), "", "", n)
		contractError(t, err, store.ErrUnreachable)
		if prospectLists != nil {
			t.Fatal(prospectLists)
		}
		passes, err := f.s.ListPassesReversed(context.Background(), "", "", n)
		contractError(t, err, store.ErrUnreachable)
		if passes != nil {
			t.Fatal("invalid count returned passes")
		}
	}
}

// R-2CAE-ZPEC
func TestReversedStatementParameters(t *testing.T) {
	f := contractDB(t)
	for _, tc := range []struct {
		query windowStatement
		args  []any
	}{
		{startedLast, []any{3}}, {startedPreceding, []any{"started", "id", 3}},
	} {
		statementIDs(t, f, tc.query, tc.args...)
		contractOK(t, f.d.Read(context.Background(), func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(context.Background(), string(tc.query), tc.args[:len(tc.args)-1]...)
			if err == nil {
				_ = rows.Close()
				t.Fatal("statement accepted too few parameters")
			}
			return nil
		}))
	}
}

// R-PZ73-DFQV
func TestReversedStatementPlans(t *testing.T) {
	for _, populated := range []bool{false, true} {
		f := contractDB(t)
		if populated {
			f = statementFixture(t)
		}
		for range 1 {
			for _, tc := range []struct {
				query          windowStatement
				args           []any
				prefix, suffix string
			}{
				{startedLast, []any{3}, "SCAN ", " INDEX passes_window"},
				{startedPreceding, []any{"started", "id", 3}, "SEARCH ", " INDEX passes_window ((started,id)>(?,?))"},
			} {
				details := statementPlan(t, f, tc.query, tc.args...)
				if len(details) == 0 || !strings.HasPrefix(details[0], tc.prefix) || !strings.HasSuffix(details[0], tc.suffix) {
					t.Fatalf("unexpected leading plan: %v", details)
				}
				for i, detail := range details {
					if strings.Contains(detail, "USE TEMP B-TREE") || (strings.HasPrefix(detail, "SCAN ") && (i != 0 || tc.prefix != "SCAN ")) {
						t.Fatalf("unbounded scan or temporary sort: %v", details)
					}
				}
			}
		}
	}
}
