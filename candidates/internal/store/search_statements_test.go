package store_test

import (
	"database/sql"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// Assigning to a distinct string type requires SearchShort to be untyped.
const searchShort windowStatement = store.SearchShort

func searchShortFixture(t *testing.T) (*contractFixture, []string) {
	t.Helper()
	f := contractDB(t)
	ids := []string{}
	for i, name := range []string{"Ma", "Ma Co", "Mb", "Ébène", "Ma\U0010ffff", "Ma\x00Co", "M\x00a", "M[ Co", "M* Co", "M? Co", "M% Co", "M_ Co", "İstanbul", "Σigma", "Zed"} {
		var alternates []string
		if name == "Ma Co" || name == "Zed" {
			alternates = []string{"Ma Alternate", name}
		}
		ids = append(ids, searchAttach(t, f, "short-"+strconv.Itoa(i), name, alternates...))
	}
	merged := searchAttach(t, f, "short-merged", "Ma Merged")
	ids = append(ids, merged)
	_, _, err := f.s.Decide(t.Context(), store.Ruling{Kind: store.KindMerge, Candidate: ids[len(ids)-2], Other: merged, Why: "same business", UserID: "owner"})
	contractOK(t, err)
	c, err := f.s.GetCandidate(t.Context(), merged)
	contractOK(t, err)
	contractEqual(t, c.Status, store.StatusMerged)
	contractEqual(t, strings.HasPrefix(strings.ToLower(strings.Join(c.Names, "\n")), "ma"), true)
	_, _, err = f.s.Decide(t.Context(), store.Ruling{Kind: store.KindExclude, Candidate: ids[0], Why: "out of scope", UserID: "owner"})
	contractOK(t, err)
	return f, ids
}

// R-3WUE-4KWA
func TestSearchShortStatementParameters(t *testing.T) {
	f := contractDB(t)
	contractEqual(t, statementIDs(t, f, searchShort, "ma", "ma\xff"), []string{})
	for _, args := range [][]any{nil, {"ma"}} {
		contractOK(t, f.d.Read(t.Context(), func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(t.Context(), store.SearchShort, args...)
			if err == nil {
				_ = rows.Close()
				t.Fatal("short statement accepted too few parameters")
			}
			return nil
		}))
	}
}

// R-3Y2A-ICMZ R-7I2O-J1WF
func TestSearchShortStatementMatchesCurrentNames(t *testing.T) {
	f, ids := searchShortFixture(t)
	check := func() {
		cs := searchCandidates(t, f.s, ids)
		for _, q := range []string{"m", "ma", "mb", "éb", "m[", "m*", "m?", "m%", "m_", "i", "σ", "zz"} {
			want := []string{}
			for _, c := range cs {
				if c.Status == store.StatusActive && strings.HasPrefix(strings.ToLower(strings.Join(c.Names, "\n")), q) {
					want = append(want, c.ID)
				}
			}
			got := statementIDs(t, f, searchShort, q, q+"\xff")
			slices.Sort(got)
			slices.Sort(want)
			contractEqual(t, got, want)
			for _, limit := range []int{1, 2, store.SearchMaxLimit} {
				assertSearchRanking(t, f.s, cs, " \t"+strings.ToUpper(q)+"\x00", limit)
			}
		}
	}
	check()
	_, _, err := f.s.AddOwnerRecord(t.Context(), store.OwnerRecord{Candidate: ids[1], Note: "rename", UserID: "owner", Set: store.OwnerValues{Name: ownPtr("Na Co")}})
	contractOK(t, err)
	check()
}

// R-40I3-9W4D
func TestSearchShortStatementSeeksNameRange(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(strconv.FormatBool(populated), func(t *testing.T) {
			var f *contractFixture
			if populated {
				f, _ = searchShortFixture(t)
			} else {
				f = contractDB(t)
			}
			for _, bounds := range [][2]string{{"ma", "ma\xff"}, {"éb", "éb\xff"}, {"", ""}, {"z", "a"}, {"\x00", "\xff"}} {
				details := statementPlan(t, f, searchShort, bounds[0], bounds[1])
				if len(details) == 0 || !strings.HasPrefix(details[0], "SEARCH ") || !strings.HasSuffix(details[0], " INDEX candidates_names_lower (names_lower>? AND names_lower<?)") {
					t.Fatalf("unexpected leading plan: %v", details)
				}
				for _, detail := range details {
					if strings.HasPrefix(detail, "SCAN ") || strings.Contains(detail, "candidates_window") || strings.Contains(detail, "USE TEMP B-TREE") {
						t.Fatalf("unbounded scan or temporary sort: %v", details)
					}
				}
			}
		})
	}
}
