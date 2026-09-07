package store_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/dory/internal/store"
)

func TestSearchUsesFTS5QuerySemantics(t *testing.T) {
	// R-IO5V-66C6
	path := filepath.Join(t.TempDir(), "session.sqlite")
	now := func() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) }
	s, err := store.Create(path, "id", "root", now)
	if err != nil {
		t.Fatal(err)
	}
	ids := addTexts(t, s,
		"cobalt headers together",
		"cobalt alone",
		"headers alone",
		"run make verify today",
		"run make fast verify today",
		"errors happen",
		"mistakes happen",
	)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(path, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	assertSearchIDs(t, s, "cobalt AND headers", store.Filter{}, []int64{ids[0]})
	assertSearchIDs(t, s, `"make verify"`, store.Filter{}, []int64{ids[3]})
	assertSearchIDs(t, s, "error*", store.Filter{}, []int64{ids[5]})
	if _, err := s.Search(`"`, store.Filter{}); err == nil {
		t.Fatal("Search accepted an invalid FTS5 query")
	}
}

func TestSearchOrdersByRankThenNewestID(t *testing.T) {
	// R-IPDR-JY2V
	s := createSearchStore(t)
	ids := addTexts(t, s,
		"signal with many unrelated filler words that dilute the match",
		"signal signal signal signal signal signal signal signal",
		"signal signal signal",
		"signal signal signal",
	)
	assertSearchIDs(t, s, "signal", store.Filter{}, []int64{ids[1], ids[3], ids[2], ids[0]})
}

func TestSearchAppliesAllFilters(t *testing.T) {
	// R-IQLN-XPTK
	s := createSearchStore(t)
	ids := []int64{
		addSearchEntry(t, s, "3", store.KindNote, "needle"),
		addSearchEntry(t, s, "3.1", store.KindPrompt, "needle"),
		addSearchEntry(t, s, "3.1.2", store.KindReport, "needle"),
		addSearchEntry(t, s, "31", store.KindTranscript, "needle"),
		addSearchEntry(t, s, "4", store.KindNote, "needle"),
	}
	assertSearchIDs(t, s, "needle", store.Filter{}, []int64{ids[4], ids[3], ids[2], ids[1], ids[0]})
	assertSearchIDs(t, s, "needle", store.Filter{Kinds: []store.Kind{store.KindNote, store.KindReport}}, []int64{ids[4], ids[2], ids[0]})
	assertSearchIDs(t, s, "needle", store.Filter{Exclude: []store.Kind{store.KindPrompt, store.KindTranscript}}, []int64{ids[4], ids[2], ids[0]})
	assertSearchIDs(t, s, "needle", store.Filter{Address: "3"}, []int64{ids[2], ids[1], ids[0]})
	assertSearchIDs(t, s, "needle", store.Filter{
		Kinds:   []store.Kind{store.KindNote, store.KindPrompt, store.KindTranscript},
		Exclude: []store.Kind{store.KindPrompt},
		Address: "3",
	}, []int64{ids[0]})
}

func TestSearchPaginatesWithCompleteMetadata(t *testing.T) {
	// R-IRTK-BHK9
	s := createSearchStore(t)
	ids := make([]int64, 43)
	for i := range ids {
		ids[i] = addSearchEntry(t, s, "root", store.KindNote, "pageable")
	}

	first, err := s.Search("pageable", store.Filter{Page: 0})
	if err != nil {
		t.Fatal(err)
	}
	assertPage(t, first, 1, 3, 43, reversed(ids[23:]))
	second, err := s.Search("pageable", store.Filter{Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertPage(t, second, 2, 3, 43, reversed(ids[3:23]))
	last, err := s.Search("pageable", store.Filter{Page: 3})
	if err != nil {
		t.Fatal(err)
	}
	assertPage(t, last, 3, 3, 43, reversed(ids[:3]))
	past, err := s.Search("pageable", store.Filter{Page: 4})
	if err != nil {
		t.Fatal(err)
	}
	assertPage(t, past, 4, 3, 43, nil)
	veryLargePage := int(^uint(0) >> 1)
	farPast, err := s.Search("pageable", store.Filter{Page: veryLargePage})
	if err != nil {
		t.Fatal(err)
	}
	assertPage(t, farPast, veryLargePage, 3, 43, nil)
	missing, err := s.Search("absent", store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	assertPage(t, missing, 1, 0, 0, nil)
}

func TestSearchBuildsOneLineRuneLimitedPreviews(t *testing.T) {
	// R-IT1G-P9AY
	s := createSearchStore(t)
	shortID := addSearchEntry(t, s, "root", store.KindNote, "marker alpha\r\nbeta\rgamma\ndelta")
	longText := "marker " + strings.Repeat("界", 152) + "🙂discarded\nline"
	longID := addSearchEntry(t, s, "root", store.KindNote, longText)

	page, err := s.Search("marker", store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got := hitByID(t, page.Hits, shortID).Preview; got != "marker alpha beta gamma delta" {
		t.Fatalf("short preview = %q", got)
	}
	wantLong := "marker " + strings.Repeat("界", 152) + "🙂"
	if got := hitByID(t, page.Hits, longID).Preview; got != wantLong || len([]rune(got)) != store.PreviewRunes {
		t.Fatalf("long preview has %d runes and value %q", len([]rune(got)), got)
	}
}

func createSearchStore(t *testing.T) *store.Store {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) }
	s, err := store.Create(filepath.Join(t.TempDir(), "session.sqlite"), "id", "root", now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func addTexts(t *testing.T, s *store.Store, texts ...string) []int64 {
	t.Helper()
	ids := make([]int64, len(texts))
	for i, text := range texts {
		ids[i] = addSearchEntry(t, s, "root", store.KindNote, text)
	}
	return ids
}

func addSearchEntry(t *testing.T, s *store.Store, address string, kind store.Kind, text string) int64 {
	t.Helper()
	id, err := s.Add(address, kind, text, nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertSearchIDs(t *testing.T, s *store.Store, query string, filter store.Filter, want []int64) {
	t.Helper()
	page, err := s.Search(query, filter)
	if err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(page.Hits); !slices.Equal(got, want) {
		t.Fatalf("Search(%q, %#v) ids = %v, want %v", query, filter, got, want)
	}
}

func assertPage(t *testing.T, page store.Page, number, pages, total int, wantIDs []int64) {
	t.Helper()
	if page.Page != number || page.Pages != pages || page.Total != total {
		t.Fatalf("page metadata = (%d, %d, %d), want (%d, %d, %d)", page.Page, page.Pages, page.Total, number, pages, total)
	}
	if len(page.Hits) > store.PageSize {
		t.Fatalf("page contains %d hits, maximum is %d", len(page.Hits), store.PageSize)
	}
	if got := hitIDs(page.Hits); !slices.Equal(got, wantIDs) {
		t.Fatalf("page ids = %v, want %v", got, wantIDs)
	}
}

func hitIDs(hits []store.Hit) []int64 {
	ids := make([]int64, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ID
	}
	return ids
}

func reversed(ids []int64) []int64 {
	result := slices.Clone(ids)
	slices.Reverse(result)
	return result
}

func hitByID(t *testing.T, hits []store.Hit, id int64) store.Hit {
	t.Helper()
	for _, hit := range hits {
		if hit.ID == id {
			return hit
		}
	}
	t.Fatalf("hit %d not found in %v", id, hitIDs(hits))
	return store.Hit{}
}
