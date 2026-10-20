package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// Assigning each constant to a distinct string type also requires it to be untyped.
type windowStatement string

const (
	candidatesFirst windowStatement = store.CandidatesFirst
	candidatesAfter windowStatement = store.CandidatesAfter
	startedFirst    windowStatement = store.StartedFirst
	startedAfter    windowStatement = store.StartedAfter
)

func statementFixture(t *testing.T) *contractFixture {
	t.Helper()
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 20, "55073": 70}))
	base := f.now
	ids := []string{}
	for i := range 16 {
		f.now = base.Add(time.Duration(i/3) * time.Hour)
		name := fmt.Sprintf("Business %02d", i)
		o := contractObservation(name, name, base)
		if i%2 == 0 {
			o.Reading.Names = append(o.Reading.Names, "Selected alias")
			o.Reading.Zip = "55073"
			o.Reading.Phone = "218-555-0199"
			o.Reading.PhoneKey = sources.PhoneKey(o.Reading.Phone)
		}
		if i%3 == 0 {
			o.Reading.Class = "private"
			o.Reading.Cargo = []string{"machinery", "grain"}
		}
		high := int64(10000000 + (i/3)*1000000)
		o.Reading.Estimate.High = &high
		o.Reading.Estimate.Low = high / 2
		ids = append(ids, contractAttach(t, f, "run", o)[0])
		p := contractNewPass(t, f, name)
		_, err := f.s.FinishPass(ctx, p.ID, store.Counts{})
		contractOK(t, err)
	}
	_, err := f.s.JudgeAll(ctx)
	contractOK(t, err)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same business", UserID: "owner"})
	contractOK(t, err)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[2], Why: "out of scope", UserID: "owner"})
	contractOK(t, err)
	return f
}

func statementIDs(t *testing.T, f *contractFixture, query windowStatement, args ...any) []string {
	t.Helper()
	out := []string{}
	ctx := context.Background()
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, string(query), args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		columns, err := rows.Columns()
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			values := make([]any, len(columns))
			values[0] = &id
			for i := 1; i < len(values); i++ {
				values[i] = new(any)
			}
			if err := rows.Scan(values...); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	}))
	return out
}

func statementPlan(t *testing.T, f *contractFixture, query windowStatement, args ...any) []string {
	t.Helper()
	ctx := context.Background()
	details := []string{}
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "EXPLAIN QUERY PLAN "+string(query), args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				return err
			}
			details = append(details, detail)
		}
		return rows.Err()
	}))
	return details
}

// R-VANB-FF5W R-XWJK-VR9D
func TestWindowStatementsMatchStoreWindows(t *testing.T) {
	f := statementFixture(t)
	ctx := context.Background()
	for _, filter := range []store.CandidateFilter{
		{}, {Name: "SELECTED"}, {Name: "Business 03"}, {Name: "missing"},
		{Zip: "55073"}, {Phone: "(218) 555-0199"}, {Phone: "320.555.0101"},
		{Name: "BUSINESS", Zip: "55073", Phone: "(218) 555-0199"},
	} {
		full, err := f.s.ListCandidates(ctx, filter, "", "", 100)
		contractOK(t, err)
		positions := [][2]string{{"", "missing"}, {"9999", "start"}}
		for _, c := range full {
			positions = append(positions, [2]string{c.Created.Format(time.RFC3339), c.ID})
		}
		for _, n := range []int{1, 3, 100} {
			cs, e := f.s.ListCandidates(ctx, filter, "", "", n)
			contractOK(t, e)
			want := []string{}
			for _, c := range cs {
				want = append(want, c.ID)
			}
			contractEqual(t, statementIDs(t, f, candidatesFirst, strings.ToLower(filter.Name), filter.Zip, sources.PhoneKey(filter.Phone), n), want)
			for _, p := range positions {
				cs, e = f.s.ListCandidates(ctx, filter, p[0], p[1], n)
				contractOK(t, e)
				want = []string{}
				for _, c := range cs {
					want = append(want, c.ID)
				}
				contractEqual(t, statementIDs(t, f, candidatesAfter, strings.ToLower(filter.Name), filter.Zip, sources.PhoneKey(filter.Phone), p[0], p[1], n), want)
			}
		}
	}
	full, err := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, err)
	positions := [][2]string{{"", "missing"}, {"9999", "start"}}
	for _, p := range full {
		positions = append(positions, [2]string{p.Started.Format(time.RFC3339), p.ID})
	}
	for _, n := range []int{1, 3, 100} {
		ps, e := f.s.ListPasses(ctx, "", "", n)
		contractOK(t, e)
		want := []string{}
		for _, p := range ps {
			want = append(want, p.ID)
		}
		contractEqual(t, statementIDs(t, f, startedFirst, n), want)
		for _, pos := range positions {
			ps, e = f.s.ListPasses(ctx, pos[0], pos[1], n)
			contractOK(t, e)
			want = []string{}
			for _, p := range ps {
				want = append(want, p.ID)
			}
			contractEqual(t, statementIDs(t, f, startedAfter, pos[0], pos[1], n), want)
		}
	}
}

// R-VANB-FF5W
func TestWindowStatementParameterCounts(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	for _, tc := range []struct {
		query windowStatement
		args  []any
	}{
		{candidatesFirst, []any{"name", "zip", "phone", 3}},
		{candidatesAfter, []any{"name", "zip", "phone", "created", "id", 3}},
		{startedFirst, []any{3}}, {startedAfter, []any{"started", "id", 3}},
	} {
		statementIDs(t, f, tc.query, tc.args...)
		contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, string(tc.query), tc.args[:len(tc.args)-1]...)
			if err == nil {
				_ = rows.Close()
				t.Fatal("statement accepted too few parameters")
			}
			return nil
		}))
	}
}

// R-RXX9-K4R5 R-U8UF-KJ6D
func TestWindowStatementsSeekAndAvoidTemporarySorting(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprint(populated), func(t *testing.T) {
			var f *contractFixture
			if populated {
				f = statementFixture(t)
			} else {
				f = contractDB(t)
			}
			for _, filtered := range []bool{false, true} {
				name, zip, phone := "", "", ""
				if filtered {
					name, zip, phone = "selected", "55073", "2185550199"
				}
				for _, tc := range []struct {
					query          windowStatement
					args           []any
					prefix, suffix string
				}{
					{candidatesFirst, []any{name, zip, phone, 3}, "SEARCH ", " INDEX candidates_window (status=?)"},
					{candidatesAfter, []any{name, zip, phone, "created", "id", 3}, "SEARCH ", " INDEX candidates_window (status=? AND (created,id)<(?,?))"},
					{startedFirst, []any{3}, "SCAN ", " INDEX passes_window"},
					{startedAfter, []any{"started", "id", 3}, "SEARCH ", " INDEX passes_window ((started,id)<(?,?))"},
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
		})
	}
}
