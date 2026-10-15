package store_test

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

const (
	shortlistLast      windowStatement = store.ShortlistLast
	shortlistPreceding windowStatement = store.ShortlistPreceding
	startedLast        windowStatement = store.StartedLast
	startedPreceding   windowStatement = store.StartedPreceding
)

func reversedFixture(t *testing.T) *contractFixture {
	t.Helper()
	f := statementFixture(t)
	for _, kind := range []string{"unbounded", "stale", "unfit"} {
		observation := contractObservation(kind, "Extra "+kind, f.now)
		switch kind {
		case "unbounded":
			observation.Reading.Estimate.High = nil
		case "stale":
			observation.Reading.Filed = f.now.AddDate(-6, 0, 0)
			observation.Reading.Estimate.Filed = observation.Reading.Filed
		case "unfit":
			observation.Reading.Fleet = false
		}
		contractAttach(t, f, "extra-run", observation)
	}
	_, err := f.s.JudgeAll(context.Background())
	contractOK(t, err)
	return f
}

func reversedFilters() []store.ShortlistFilter {
	return []store.ShortlistFilter{
		{MaxMinutes: ptrInt(100)}, {MaxMinutes: ptrInt(20)}, {MaxMinutes: ptrInt(100), MinRevenue: ptrInt(6500000)},
		{MaxMinutes: ptrInt(100), Class: sources.ClassForHire}, {MaxMinutes: ptrInt(100), Class: "private"},
		{MaxMinutes: ptrInt(100), Cargo: "machinery"}, {MaxMinutes: ptrInt(100), Cargo: "missing"},
		{MaxMinutes: ptrInt(70), MinRevenue: ptrInt(5500000), Class: "private", Cargo: "machinery"},
	}
}

func reversedShortlistOracle(t *testing.T, f *contractFixture, filter store.ShortlistFilter) []store.Ranked {
	t.Helper()
	ctx := context.Background()
	candidates, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, err)
	out := []store.Ranked{}
	for _, c := range candidates {
		a, e := f.s.GetAssessment(ctx, c.ID)
		contractOK(t, e)
		cargo := filter.Cargo == ""
		for _, v := range c.Cargo {
			cargo = cargo || v == filter.Cargo
		}
		if (filter.MaxMinutes == nil || a.Minutes != nil && *a.Minutes <= *filter.MaxMinutes) && (filter.MinRevenue == nil || a.RevenueLow != nil && *a.RevenueLow >= *filter.MinRevenue) && !a.Excluded && (filter.Class == "" || filter.Class == c.Class) && cargo {
			out = append(out, store.Ranked{Candidate: c, Assessment: a})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Assessment.OperatingKey != out[j].Assessment.OperatingKey {
			return out[i].Assessment.OperatingKey < out[j].Assessment.OperatingKey
		}
		return out[i].Candidate.ID > out[j].Candidate.ID
	})
	return out
}

// R-HEI8-OLVU R-2YLD-JM1J R-3116-B5IX
func TestReversedShortlistWindows(t *testing.T) {
	f := reversedFixture(t)
	ctx := context.Background()
	for _, filter := range reversedFilters() {
		full := reversedShortlistOracle(t, f, filter)
		if filter.MaxMinutes != nil && *filter.MaxMinutes == 100 && filter.MinRevenue == nil && filter.Class == "" && filter.Cargo == "" {
			tied := false
			for i := 1; i < len(full); i++ {
				tied = tied || full[i-1].Assessment.OperatingKey == full[i].Assessment.OperatingKey
			}
			if !tied {
				t.Fatal("fixture lacks score ties")
			}
		}
		positions := []struct {
			score int64
			id    string
		}{{0, ""}, {1 << 62, ""}, {-1, "end"}, {1 << 62, "start"}}
		for _, r := range full {
			positions = append(positions, struct {
				score int64
				id    string
			}{r.Assessment.OperatingKey, r.Candidate.ID})
			// A synthetic position next to this id exercises a position absent from the store.
			positions = append(positions, struct {
				score int64
				id    string
			}{r.Assessment.OperatingKey, r.Candidate.ID + "!"})
		}
		for _, pos := range positions {
			for _, n := range []int{1, 2, 3, 100} {
				want := []store.Ranked{}
				for _, r := range full {
					if pos.id == "" || r.Assessment.OperatingKey > pos.score || (r.Assessment.OperatingKey == pos.score && r.Candidate.ID < pos.id) {
						want = append(want, r)
						if len(want) == n {
							break
						}
					}
				}
				got, err := f.s.ShortlistReversed(ctx, filter, pos.score, pos.id, n)
				contractOK(t, err)
				contractEqual(t, got, want)
				ids := []string{}
				for _, r := range want {
					ids = append(ids, r.Candidate.ID)
				}
				query := shortlistLast
				args := []any{boundNumber(filter.MaxMinutes), boundNumber(filter.MinRevenue), filter.Class, filter.Cargo, n}
				if pos.id != "" {
					query = shortlistPreceding
					args = []any{boundNumber(filter.MaxMinutes), boundNumber(filter.MinRevenue), filter.Class, filter.Cargo, pos.score, pos.id, n}
				}
				contractEqual(t, statementIDs(t, f, query, args...), ids)
			}
		}
		traversed := []store.Ranked{}
		var score int64
		id := ""
		for {
			got, err := f.s.ShortlistReversed(ctx, filter, score, id, 2)
			contractOK(t, err)
			if len(got) == 0 {
				break
			}
			traversed = append(traversed, got...)
			if len(traversed) > len(full) {
				t.Fatal("reversed walk repeated entries")
			}
			last := got[len(got)-1]
			score, id = last.Assessment.OperatingKey, last.Candidate.ID
		}
		contractEqual(t, traversed, full)
	}
}

// R-HEI8-OLVU R-HGY1-G5D8 R-3116-B5IX
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
		p := contractNewPass(t, f, store.TriggerRebuild)
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

// R-HI5X-TX3X
func TestReversedWindowsRejectInvalidCounts(t *testing.T) {
	f := contractDB(t)
	for _, n := range []int{0, -1, -100} {
		ranked, err := f.s.ShortlistReversed(context.Background(), store.ShortlistFilter{}, 0, "", n)
		contractError(t, err, store.ErrUnreachable)
		if ranked != nil {
			t.Fatal("invalid count returned a shortlist")
		}
		passes, err := f.s.ListPassesReversed(context.Background(), "", "", n)
		contractError(t, err, store.ErrUnreachable)
		if passes != nil {
			t.Fatal("invalid count returned passes")
		}
	}
}

// R-2ZT9-XDS8
func TestReversedStatementParameters(t *testing.T) {
	f := contractDB(t)
	for _, tc := range []struct {
		query windowStatement
		args  []any
	}{
		{shortlistLast, []any{int64(70), int64(11000000), "private", "machinery", 3}},
		{shortlistPreceding, []any{int64(70), int64(11000000), "private", "machinery", int64(9), "id", 3}},
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

// R-3292-OX9M R-33GZ-2P0B
func TestReversedStatementPlans(t *testing.T) {
	for _, populated := range []bool{false, true} {
		f := contractDB(t)
		if populated {
			f = statementFixture(t)
		}
		for _, filter := range reversedFilters() {
			for _, tc := range []struct {
				query          windowStatement
				args           []any
				prefix, suffix string
			}{
				{shortlistLast, []any{boundNumber(filter.MaxMinutes), boundNumber(filter.MinRevenue), filter.Class, filter.Cargo, 3}, "SCAN ", " INDEX assessments_operating"},
				{shortlistPreceding, []any{boundNumber(filter.MaxMinutes), boundNumber(filter.MinRevenue), filter.Class, filter.Cargo, int64(9), "id", 3}, "SEARCH ", " INDEX assessments_operating (operating_key>?)"},
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
