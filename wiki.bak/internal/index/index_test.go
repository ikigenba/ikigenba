package index

import (
	"context"
	"errors"
	"math"
	"testing"

	"agentkit/embed"

	"wiki/internal/page"
)

// fakeStore is an in-memory store stub implementing the index store + catchup
// store surfaces for offline, deterministic tests.
type fakeStore struct {
	lexical    []page.WholePage // returned by SearchPages, in rank order
	lexicalErr error
	vectors    []page.PageVector // returned by LoadVectors (already current-model)
	vectorsErr error
	pages      map[string]page.WholePage // id → whole page (WholePagesByIDs)

	// catch-up side
	work      []page.VectorWork
	workErr   error
	upserted  map[string]page.PageVector
	upsertVer map[string]int
}

func (f *fakeStore) SearchPages(ctx context.Context, query string, limit int) ([]page.WholePage, error) {
	if f.lexicalErr != nil {
		return nil, f.lexicalErr
	}
	out := f.lexical
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) LoadVectors(ctx context.Context, model string) ([]page.PageVector, error) {
	return f.vectors, f.vectorsErr
}

func (f *fakeStore) WholePagesByIDs(ctx context.Context, ids []string) ([]page.WholePage, error) {
	var out []page.WholePage
	for _, id := range ids {
		if wp, ok := f.pages[id]; ok {
			out = append(out, wp)
		}
	}
	return out, nil
}

func (f *fakeStore) VectorWorkList(ctx context.Context, model string, limit int) ([]page.VectorWork, error) {
	if f.workErr != nil {
		return nil, f.workErr
	}
	out := f.work
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) UpsertVector(ctx context.Context, subject string, embeddedVersion int, model string, vector []float32) error {
	if f.upserted == nil {
		f.upserted = map[string]page.PageVector{}
		f.upsertVer = map[string]int{}
	}
	f.upserted[subject] = page.PageVector{Subject: subject, Vector: vector}
	f.upsertVer[subject] = embeddedVersion
	return nil
}

// fakeEmbedder returns a pinned vector per text via a lookup, or a fixed default.
type fakeEmbedder struct {
	byText map[string][]float32
	def    []float32
	err    error
	calls  int
}

func (e *fakeEmbedder) Embed(ctx context.Context, model string, dims int, texts []string) (embed.Result, error) {
	e.calls++
	if e.err != nil {
		return embed.Result{}, e.err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := e.byText[t]; ok {
			out[i] = v
		} else {
			out[i] = e.def
		}
	}
	return embed.Result{Vectors: out, InputTokens: len(texts)}, nil
}

func wp(id string) page.WholePage { return page.WholePage{Subject: id, Body: "b-" + id} }

func TestCosine(t *testing.T) {
	if got := cosine([]float32{1, 0}, []float32{1, 0}); math.Abs(got-1) > 1e-9 {
		t.Fatalf("identical vectors cosine = %v, want 1", got)
	}
	if got := cosine([]float32{1, 0}, []float32{0, 1}); math.Abs(got) > 1e-9 {
		t.Fatalf("orthogonal cosine = %v, want 0", got)
	}
	if got := cosine([]float32{1, 0}, []float32{-1, 0}); math.Abs(got+1) > 1e-9 {
		t.Fatalf("opposite cosine = %v, want -1", got)
	}
	// Length mismatch / zero vector must not panic and scores 0.
	if got := cosine([]float32{1, 2, 3}, []float32{1, 2}); got != 0 {
		t.Fatalf("mismatched length cosine = %v, want 0", got)
	}
	if got := cosine([]float32{0, 0}, []float32{1, 1}); got != 0 {
		t.Fatalf("zero vector cosine = %v, want 0", got)
	}
}

func TestRRFFuse(t *testing.T) {
	// Lane A: [x, y]; Lane B: [y, z]. y appears in both at decent ranks → top.
	laneA := []page.WholePage{wp("x"), wp("y")}
	laneB := []page.WholePage{wp("y"), wp("z")}
	out := rrfFuse(60, laneA, laneB)
	if len(out) != 3 {
		t.Fatalf("fused len = %d, want 3", len(out))
	}
	if out[0].Subject != "y" {
		t.Fatalf("top hit = %q, want y (appears in both lanes)", out[0].Subject)
	}
	// x (rank0 in A) and z (rank1 in B): x has the higher single-lane score.
	if out[1].Subject != "x" {
		t.Fatalf("second hit = %q, want x", out[1].Subject)
	}
}

func TestSearchLexicalOnlyWhenVectorOff(t *testing.T) {
	fs := &fakeStore{lexical: []page.WholePage{wp("a"), wp("b")}}
	r := New(Options{Store: fs, Embedder: &fakeEmbedder{}, Model: "m", Dims: 3})
	got, err := r.Search(context.Background(), "q", 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Subject != "a" {
		t.Fatalf("lexical-only result = %v", got)
	}
}

func TestSearchNoEmbedderIsLexicalOnly(t *testing.T) {
	fs := &fakeStore{lexical: []page.WholePage{wp("a")}}
	r := New(Options{Store: fs, Embedder: nil, Model: "m", Dims: 3})
	// Even with useVector=true, a nil embedder serves lexical-only.
	got, err := r.Search(context.Background(), "q", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Subject != "a" {
		t.Fatalf("nil-embedder result = %v", got)
	}
}

func TestSearchHybridFuses(t *testing.T) {
	// Lexical ranks [a, b]. Vector corpus makes c the closest to the query, then a.
	fs := &fakeStore{
		lexical: []page.WholePage{wp("a"), wp("b")},
		vectors: []page.PageVector{
			{Subject: "a", Vector: []float32{0.2, 1}},
			{Subject: "b", Vector: []float32{1, 0}},
			{Subject: "c", Vector: []float32{0, 1}},
		},
		pages: map[string]page.WholePage{"a": wp("a"), "b": wp("b"), "c": wp("c")},
	}
	emb := &fakeEmbedder{def: []float32{0, 1}} // query points at c, then a
	r := New(Options{Store: fs, Embedder: emb, Model: "m", Dims: 2})
	got, err := r.Search(context.Background(), "q", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	// c must now appear (vector-only hit) and a (in both lanes) should rank high.
	subs := map[string]bool{}
	for _, h := range got {
		subs[h.Subject] = true
	}
	if !subs["c"] {
		t.Fatalf("hybrid result missing vector-only hit c: %v", got)
	}
	if emb.calls != 1 {
		t.Fatalf("embedder called %d times, want 1", emb.calls)
	}
}

func TestSearchVectorFailureDegradesToLexical(t *testing.T) {
	fs := &fakeStore{lexical: []page.WholePage{wp("a")}, vectors: []page.PageVector{{Subject: "a", Vector: []float32{1}}}}
	emb := &fakeEmbedder{err: errors.New("embed boom")}
	r := New(Options{Store: fs, Embedder: emb, Model: "m", Dims: 1})
	got, err := r.Search(context.Background(), "q", 5, true)
	if err != nil {
		t.Fatalf("vector failure must degrade, not error: %v", err)
	}
	if len(got) != 1 || got[0].Subject != "a" {
		t.Fatalf("degraded result = %v", got)
	}
}

func TestSearchEmptyVectorCorpusDegradesToLexical(t *testing.T) {
	fs := &fakeStore{lexical: []page.WholePage{wp("a")}, vectors: nil}
	emb := &fakeEmbedder{def: []float32{1}}
	r := New(Options{Store: fs, Embedder: emb, Model: "m", Dims: 1})
	got, err := r.Search(context.Background(), "q", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("empty-corpus result = %v", got)
	}
}

func TestModelTag(t *testing.T) {
	if got := ModelTag("text-embedding-3-large", 1024); got != "text-embedding-3-large@1024" {
		t.Fatalf("ModelTag = %q", got)
	}
}
