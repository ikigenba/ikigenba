//go:build !race

package store_test

import (
	"context"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-9JC7-61RU R-7FMV-RIF1
func TestSearchTiming(t *testing.T) {
	// Both record data and the store's ID source are deterministic and injected.
	rng := rand.NewChaCha8([32]byte{9, 4, 8, 1, 7, 5})
	words := []string{"Brickwork", "Plumbing", "Hardware", "Landscape", "Freight", "Mechanical", "Fabricators", "Logistics", "Electric", "Delivery", "Concrete", "Builders"}
	name := func() string {
		parts := make([]string, 2+rng.Uint64()%3)
		for i := range parts {
			parts[i] = words[rng.Uint64()%12]
		}
		return strings.Join(parts, " ")
	}
	rows := make([]searchPriorRow, 0, 100001)
	counts := map[string]int{}
	for i := range 100000 {
		n := name()
		names := []string{n}
		if i%3 == 0 {
			names = append(names, name())
		}
		rows = append(rows, searchPriorRow{searchRowID(i), n, store.StatusActive, names})
		for _, n := range names {
			counts[n]++
		}
	}
	const planted = "Zyqvxjklmn"
	rows = append(rows, searchPriorRow{searchRowID(100000), planted, store.StatusActive, []string{planted}})
	s := searchUpgrade(t, rows)
	unique := searchPriorRow{}
	for _, row := range rows[:100000] {
		if counts[row.name] == 1 {
			unique = row
			break
		}
	}
	if unique.id == "" {
		t.Fatal("fixed seed produced no unique display name")
	}
	// Compute the short-query ranking over all names before measuring the call.
	shortCandidates := []store.Candidate{}
	for _, r := range rows {
		if strings.HasPrefix(strings.ToLower(r.name), "br") {
			shortCandidates = append(shortCandidates, store.Candidate{ID: r.id, Name: r.name, Names: r.names, Status: store.StatusActive})
		}
	}
	if len(shortCandidates) < 1000 {
		t.Fatal("fixture lacks 1000 short-prefix display names")
	}
	shortWant := referenceSearch(shortCandidates, "br", store.SearchDefaultLimit)
	cases := []struct {
		name, q string
		want    int
		first   string
		exact   bool
	}{
		{"short", "br", 10, shortWant[0].Candidate.ID, false},
		{"unique", unique.name, 10, unique.id, true},
		{"planted transposition", "Zyqvxkjlmn", 1, rows[100000].id, false},
		{"word transposition", "Brcikwork", 10, "", false},
		{"two substitutions", "Brixkwork Plumxing", 10, "", false},
		{"four words", "Brickwork Plumbing Hardware Landscape", 10, "", false},
		{"absent", "qqqqqqqq", 0, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			got, e := s.Search(context.Background(), tc.q, store.SearchDefaultLimit)
			elapsed := time.Since(start)
			contractOK(t, e)
			if elapsed >= 100*time.Millisecond {
				t.Fatalf("Search(%q) took %s, limit 100ms", tc.q, elapsed)
			}
			t.Logf("Search(%q): %s", tc.q, elapsed)
			// The unique query may have fewer than ten other matches; its first match is required.
			if tc.name != "unique" {
				contractEqual(t, len(got), tc.want)
			} else if len(got) == 0 {
				t.Fatal("unique display name missing")
			}
			if tc.first != "" {
				contractEqual(t, got[0].Candidate.ID, tc.first)
			}
			if tc.exact {
				contractEqual(t, got[0].Matched, tc.q)
				contractEqual(t, got[0].Score, 1.0)
			}
			if tc.name == "short" {
				for i, m := range got {
					contractEqual(t, m.Candidate.ID, shortWant[i].Candidate.ID)
					contractEqual(t, m.Matched, shortWant[i].Matched)
					contractEqual(t, m.Score, shortWant[i].Score)
				}
			}
			if tc.name == "planted transposition" {
				tier, score := referenceName(strings.ToLower(tc.q), planted, true)
				contractEqual(t, tier, 5)
				contractEqual(t, got[0].Matched, planted)
				contractEqual(t, got[0].Score, score)
			}
			// Large recall may truncate; every returned match must still be current,
			// active, individually valid, unique, and ordered by the contract's scores.
			seen := map[string]bool{}
			for i, m := range got {
				current, e := s.GetCandidate(context.Background(), m.Candidate.ID)
				contractOK(t, e)
				contractEqual(t, m.Candidate, current)
				contractEqual(t, current.Status, store.StatusActive)
				if !slices.Contains(append([]string{current.Name}, current.Names...), m.Matched) || seen[current.ID] {
					t.Fatal("invalid or repeated match")
				}
				seen[current.ID] = true
				_, score := referenceName(strings.ToLower(tc.q), m.Matched, m.Matched == current.Name)
				contractEqual(t, m.Score, score)
				if i > 0 && (got[i-1].Score < m.Score || got[i-1].Score == m.Score && got[i-1].Candidate.ID > m.Candidate.ID) {
					t.Fatal("matches out of rank order")
				}
			}
		})
	}
}
