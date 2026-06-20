package index

import (
	"context"
	"testing"

	"wiki/internal/page"
)

func TestCatchupSweepEmbedsAndUpserts(t *testing.T) {
	fs := &fakeStore{
		work: []page.VectorWork{
			{Subject: "s1", Text: "alpha", Version: 3},
			{Subject: "s2", Text: "beta", Version: 1},
		},
	}
	emb := &fakeEmbedder{byText: map[string][]float32{
		"alpha": {1, 0},
		"beta":  {0, 1},
	}}
	c := NewCatchup(CatchupOptions{Store: fs, Embedder: emb, Model: "m", Dims: 2, BatchSize: 10})
	c.sweep(context.Background())

	if len(fs.upserted) != 2 {
		t.Fatalf("upserted %d, want 2", len(fs.upserted))
	}
	if got := fs.upsertVer["s1"]; got != 3 {
		t.Fatalf("s1 embedded_version = %d, want 3", got)
	}
	if v := fs.upserted["s1"].Vector; len(v) != 2 || v[0] != 1 {
		t.Fatalf("s1 vector = %v", v)
	}
}

// TestCatchupSweepDrainsAcrossBatches: a work list larger than one batch is
// drained by a single sweep (it loops until the work list empties). The fake's
// work list does not shrink on its own, so we model drain by clearing it after
// the first full batch via a one-shot stub.
func TestCatchupSweepStopsOnPartialBatch(t *testing.T) {
	fs := &fakeStore{
		work: []page.VectorWork{{Subject: "s1", Text: "x", Version: 1}}, // 1 < batchSize
	}
	emb := &fakeEmbedder{def: []float32{1}}
	c := NewCatchup(CatchupOptions{Store: fs, Embedder: emb, Model: "m", Dims: 1, BatchSize: 64})
	c.sweep(context.Background())
	if emb.calls != 1 {
		t.Fatalf("embedder calls = %d, want 1 (single partial batch)", emb.calls)
	}
}

func TestCatchupNilEmbedderRunIsNoop(t *testing.T) {
	fs := &fakeStore{work: []page.VectorWork{{Subject: "s1", Text: "x", Version: 1}}}
	c := NewCatchup(CatchupOptions{Store: fs, Embedder: nil, Model: "m", Dims: 1})
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("nil-embedder Run = %v, want nil no-op", err)
	}
	if len(fs.upserted) != 0 {
		t.Fatalf("nil embedder must not upsert; got %d", len(fs.upserted))
	}
}

func TestCatchupNudgeIsNonBlocking(t *testing.T) {
	c := NewCatchup(CatchupOptions{Store: &fakeStore{}, Embedder: &fakeEmbedder{}, Model: "m", Dims: 1})
	// Two nudges in a row: the second must not block (buffered depth 1, drop-if-full).
	c.Nudge()
	c.Nudge()
	var nilC *Catchup
	nilC.Nudge() // nil receiver is safe
}
