package retrieve

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"wiki/internal/llm"
)

func TestVectorCacheReplaceHydratesEntriesAndDefensivelyCopies(t *testing.T) {
	// R-3WOB-6U4Q
	cache := NewVectorCache()
	entries := []vectorEntry{
		{Scope: "default", SubjectID: "subject-a", Title: "Alpha", Vec: []float32{1, 0}},
		{Scope: "default", SubjectID: "subject-b", Title: "Beta", Vec: []float32{0, 1}},
	}
	cache.Replace(entries)
	entries[0].Vec[0] = 0

	got := cache.nearest("default", []float32{1, 0}, 2)
	if len(got) != 2 {
		t.Fatalf("nearest hits = %+v, want two hydrated entries", got)
	}
	if got[0].PageID != "subject-a" || got[0].Title != "Alpha" || got[0].Score != 1 {
		t.Fatalf("first hit = %+v, want copied Alpha vector with cosine 1", got[0])
	}
}

func TestVectorCacheUpsertReplacesExistingSubject(t *testing.T) {
	// R-3XW7-KLVF
	cache := NewVectorCache()
	cache.Replace([]vectorEntry{
		{Scope: "default", SubjectID: "subject-a", Title: "Old Alpha", Vec: []float32{0, 1}},
		{Scope: "default", SubjectID: "subject-b", Title: "Beta", Vec: []float32{0.5, 0.5}},
	})
	cache.Upsert(vectorEntry{Scope: "default", SubjectID: "subject-a", Title: "New Alpha", Vec: []float32{1, 0}})

	got := cache.nearest("default", []float32{1, 0}, 10)
	if len(got) != 2 {
		t.Fatalf("nearest hits = %+v, want replacement without duplicate", got)
	}
	if got[0].PageID != "subject-a" || got[0].Title != "New Alpha" || got[0].Score != 1 {
		t.Fatalf("first hit = %+v, want updated Alpha as nearest", got[0])
	}
}

func TestVectorCacheRemoveEvictsSubjectFromNearestResults(t *testing.T) {
	cache := NewVectorCache()
	cache.Replace([]vectorEntry{
		{Scope: "default", SubjectID: "subject-loser", Title: "Loser", Vec: []float32{1, 0}},
		{Scope: "default", SubjectID: "subject-winner", Title: "Winner", Vec: []float32{0.5, 0.5}},
	})

	cache.Remove(" subject-loser ")

	got := cache.nearest("default", []float32{1, 0}, 10)
	if len(got) != 1 {
		t.Fatalf("nearest hits = %+v, want one remaining winner hit", got)
	}
	if got[0].PageID != "subject-winner" || got[0].Title != "Winner" {
		t.Fatalf("remaining hit = %+v, want winner after loser removal", got[0])
	}
}

func TestVectorRetrieverEmbedsQueryAndReturnsCosineTopK(t *testing.T) {
	// R-3Z43-YDM4
	cache := NewVectorCache()
	cache.Replace([]vectorEntry{
		{Scope: "default", SubjectID: "subject-a", Title: "Alpha", Vec: []float32{1, 0}},
		{Scope: "default", SubjectID: "subject-b", Title: "Beta", Vec: []float32{0.8, 0.6}},
		{Scope: "default", SubjectID: "subject-c", Title: "Gamma", Vec: []float32{0, 1}},
	})
	var embedded []string
	retriever := &vectorRetriever{
		cache: cache,
		embed: func(_ context.Context, _ llm.Attribution, text string) ([]float32, error) {
			embedded = append(embedded, text)
			return []float32{1, 0}, nil
		},
	}

	got, err := retriever.Search(context.Background(), "default", "  alpha query  ", SearchLimits{Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !reflect.DeepEqual(embedded, []string{"alpha query"}) {
		t.Fatalf("embedded queries = %#v, want trimmed query", embedded)
	}
	if len(got.Hits) != 2 {
		t.Fatalf("hits = %+v, want top two", got.Hits)
	}
	if got.Hits[0].PageID != "subject-a" || got.Hits[0].Score != 1 {
		t.Fatalf("first hit = %+v, want Alpha with cosine 1", got.Hits[0])
	}
	if got.Hits[1].PageID != "subject-b" || math.Abs(got.Hits[1].Score-0.8) > 0.000001 {
		t.Fatalf("second hit = %+v, want Beta with cosine 0.8", got.Hits[1])
	}
}

func TestVectorRetrieverReturnsEmbedError(t *testing.T) {
	want := errors.New("embed failed")
	retriever := &vectorRetriever{
		cache: NewVectorCache(),
		embed: func(context.Context, llm.Attribution, string) ([]float32, error) {
			return nil, want
		},
	}

	_, err := retriever.Search(context.Background(), "default", "alpha", SearchLimits{Limit: 1})
	if !errors.Is(err, want) {
		t.Fatalf("Search err = %v, want %v", err, want)
	}
}

func TestVectorRetrieverSearchIsScopeBounded(t *testing.T) {
	// R-H79R-15SP
	cache := NewVectorCache()
	cache.Replace([]vectorEntry{{Scope: "s2", SubjectID: "subject-s2", Title: "Only in s2", Vec: []float32{1, 0}}})
	retriever := NewVectorRetriever(func(context.Context, llm.Attribution, string) ([]float32, error) {
		return []float32{1, 0}, nil
	}, cache)

	got, err := retriever.Search(context.Background(), "s1", "semantic match", SearchLimits{})
	if err != nil {
		t.Fatalf("Search s1: %v", err)
	}
	if len(got.Hits) != 0 {
		t.Fatalf("s1 hits = %+v, want no result from semantically matching s2 vector", got.Hits)
	}
}

func TestVectorCacheConcurrentReadsAndWrites(t *testing.T) {
	// R-40C0-C5CT
	cache := NewVectorCache()
	cache.Replace([]vectorEntry{{Scope: "default", SubjectID: "subject-a", Title: "Alpha", Vec: []float32{1, 0}}})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				hits := cache.nearest("default", []float32{1, 0}, 1)
				if len(hits) != 1 {
					t.Errorf("nearest len = %d, want 1", len(hits))
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			cache.Upsert(vectorEntry{Scope: "default", SubjectID: "subject-a", Title: "Alpha", Vec: []float32{1, 0}})
			cache.Upsert(vectorEntry{Scope: "default", SubjectID: "subject-b", Title: "Beta", Vec: []float32{0, 1}})
		}
	}()
	wg.Wait()

	got := cache.nearest("default", []float32{0, 1}, 1)
	if len(got) != 1 || got[0].PageID != "subject-b" || got[0].Score != 1 {
		t.Fatalf("nearest after concurrent upserts = %+v, want Beta with cosine 1", got)
	}
}

func TestVectorCacheRejectsEntriesWithoutScope(t *testing.T) {
	// R-R2MT-ZFEF
	for _, test := range []struct {
		name string
		call func()
	}{
		{name: "upsert", call: func() { NewVectorCache().Upsert(vectorEntry{SubjectID: "subject-a"}) }},
		{name: "replace", call: func() { NewVectorCache().Replace([]vectorEntry{{SubjectID: "subject-a"}}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("cache mutation did not panic for an empty scope")
				}
			}()
			test.call()
		})
	}
}

func TestVectorCacheRemoveScopeEvictsOnlyMatchingEntries(t *testing.T) {
	cache := NewVectorCache()
	cache.Replace([]vectorEntry{
		{Scope: "s1", SubjectID: "subject-s1", Title: "S1", Vec: []float32{1}},
		{Scope: "s2", SubjectID: "subject-s2", Title: "S2", Vec: []float32{1}},
	})

	cache.RemoveScope("s1")

	if got := cache.nearest("s1", []float32{1}, 10); len(got) != 0 {
		t.Fatalf("s1 hits after removal = %+v, want none", got)
	}
	if got := cache.nearest("s2", []float32{1}, 10); len(got) != 1 || got[0].PageID != "subject-s2" {
		t.Fatalf("s2 hits after s1 removal = %+v, want subject-s2", got)
	}
}
