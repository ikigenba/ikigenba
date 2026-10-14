package store_test

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-ISFX-PG6M: this assignment requires an untyped string constant.
const shortlistBefore windowStatement = store.ShortlistBefore

func statementOffset(t *testing.T, f *contractFixture, args ...any) int64 {
	t.Helper()
	var out int64
	ctx := context.Background()
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, string(shortlistBefore), args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			t.Fatal("offset statement returned no row")
		}
		if err := rows.Scan(&out); err != nil {
			return err
		}
		if rows.Next() {
			t.Fatal("offset statement returned multiple rows")
		}
		return rows.Err()
	}))
	return out
}

// R-ISFX-PG6M R-ITNU-37XB R-IUVQ-GZO0
func TestShortlistBeforeParameters(t *testing.T) {
	f := contractDB(t)
	args := []any{int64(120), int64(10000000), "class", "cargo", int64(9), "id"}
	contractEqual(t, statementOffset(t, f, args...), int64(0))
	offset, err := f.s.ShortlistOffset(context.Background(), store.ShortlistFilter{MaxMinutes: 120, MinRevenue: 10000000, Class: "class", Cargo: "cargo"}, 9, "id")
	contractOK(t, err)
	contractEqual(t, offset, int64(0))
	contractOK(t, f.d.Read(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(context.Background(), string(shortlistBefore), args[:5]...)
		if err == nil {
			_ = rows.Close()
			t.Fatal("offset statement accepted too few parameters")
		}
		return nil
	}))
}

// R-ITNU-37XB R-IUVQ-GZO0 R-IW3M-UREP
func TestShortlistOffsetMatchesFilteredPositions(t *testing.T) {
	f := statementFixture(t)
	ctx := context.Background()
	filters := []store.ShortlistFilter{
		{MaxMinutes: 100}, {MaxMinutes: 20}, {MaxMinutes: 100, MinRevenue: 13000000},
		{MaxMinutes: 100, Class: sources.ClassForHire}, {MaxMinutes: 100, Class: "private"},
		{MaxMinutes: 100, Cargo: "machinery"}, {MaxMinutes: 100, Cargo: "missing"},
		{MaxMinutes: 70, MinRevenue: 11000000, Class: "private", Cargo: "machinery"},
		{MaxMinutes: -1}, {MaxMinutes: 100, Class: "missing"},
	}
	for _, filter := range filters {
		full, err := f.s.Shortlist(ctx, filter, 0, "", 100)
		contractOK(t, err)
		positions := []struct {
			key int64
			id  string
		}{{math.MaxInt64, "start"}, {math.MinInt64, "end"}, {0, "missing"}}
		for _, entry := range full {
			positions = append(positions, struct {
				key int64
				id  string
			}{entry.Assessment.ScoreKey, entry.Candidate.ID})
		}
		for _, p := range positions {
			var want int64
			for _, entry := range full {
				if entry.Assessment.ScoreKey > p.key || (entry.Assessment.ScoreKey == p.key && entry.Candidate.ID <= p.id) {
					want++
				}
			}
			got, err := f.s.ShortlistOffset(ctx, filter, p.key, p.id)
			contractOK(t, err)
			contractEqual(t, got, want)
			contractEqual(t, statementOffset(t, f, filter.MaxMinutes, filter.MinRevenue, filter.Class, filter.Cargo, p.key, p.id), want)
		}
		for _, key := range []int64{math.MinInt64, 0, math.MaxInt64} {
			got, err := f.s.ShortlistOffset(ctx, filter, key, "")
			contractOK(t, err)
			contractEqual(t, got, int64(0))
		}
	}
	before := contractState(t, f)
	offset, err := f.s.ShortlistOffset(ctx, filters[0], math.MinInt64, "end")
	contractOK(t, err)
	f.now = f.now.AddDate(10, 0, 0)
	advanced, err := f.s.ShortlistOffset(ctx, filters[0], math.MinInt64, "end")
	contractOK(t, err)
	contractEqual(t, advanced, offset)
	contractEqual(t, contractState(t, f), before)
}

// R-IXBJ-8J5E
func TestShortlistBeforeSeeksScoreIndex(t *testing.T) {
	for _, populated := range []bool{false, true} {
		var f *contractFixture
		if populated {
			f = statementFixture(t)
		} else {
			f = contractDB(t)
		}
		for _, key := range []int64{math.MinInt64, 0, math.MaxInt64} {
			for _, filter := range []store.ShortlistFilter{{}, {MaxMinutes: 70, MinRevenue: 11000000, Class: "private", Cargo: "machinery"}} {
				details := statementPlan(t, f, shortlistBefore, filter.MaxMinutes, filter.MinRevenue, filter.Class, filter.Cargo, key, "id")
				if len(details) == 0 || !strings.HasPrefix(details[0], "SEARCH ") || !strings.HasSuffix(details[0], " INDEX assessments_score (score_key>?)") {
					t.Fatalf("unexpected leading plan: %v", details)
				}
				for _, detail := range details {
					if strings.HasPrefix(detail, "SCAN ") || strings.Contains(detail, "USE TEMP B-TREE") {
						t.Fatalf("unbounded scan or temporary sort: %v", details)
					}
				}
			}
		}
	}
}
