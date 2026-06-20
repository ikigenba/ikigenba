//go:build integration

// The standing integration tier's embedding-lane slice (P11) — see "Integration
// testing" in docs/wiki-redesign-plan.md, and P11's phase-owned third-and-final
// checkpoint. It runs the REAL pinned embed model (WIKI_EMBED_MODEL /
// WIKI_EMBED_DIMS) end-to-end and asserts the output is STRUCTURALLY valid: a
// page_vectors row of the configured dims under the current model tag, and a
// hybrid retriever that returns ranked whole-page hits with the vector lane on.
// It never asserts ranking QUALITY (that is Part II's graded sweep) — only that
// the live lane produces a vector of the right shape and fuses without erroring.
//
// Build-tag gated (`-tags=integration`) so it is always in the tree but never in
// the unit gate. With no OPENAI_API_KEY it emits the visible `INTEGRATION
// CHECKPOINT SKIPPED — no keys` line and skips — never passing as if it ran.
package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	embedopenai "agentkit/embed/openai"

	"wiki/internal/config"
	"wiki/internal/db"
	"wiki/internal/page"

	_ "modernc.org/sqlite"
)

func TestEmbeddingLaneIntegration(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Log("INTEGRATION CHECKPOINT SKIPPED — no keys")
		t.Skip("no OPENAI_API_KEY present")
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// A migrated DB with one seeded page so the catch-up worker and retriever have
	// something to embed and rank.
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "wiki.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := page.NewStore(conn)
	mustExec := func(q string, args ...any) {
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	mustExec(`INSERT INTO subjects (id, type, kind, canonical_name, created_by_run) VALUES ('01ACMEINTEGRATIONSUBJECT01','entity','company','Acme Corp','r')`)
	mustExec(`INSERT INTO aliases (type, norm, subject_id) VALUES ('entity',?,?)`, page.Normalize("Acme Corp"), "01ACMEINTEGRATIONSUBJECT01")
	res, err := conn.Exec(`INSERT INTO pages (subject, title, body, version) VALUES (?,?,?,1)`,
		"01ACMEINTEGRATIONSUBJECT01", "Acme Corp", "Dana Lee is the CEO of Acme Corp, a widget manufacturer. [01ARRIVAL]")
	if err != nil {
		t.Fatalf("insert page: %v", err)
	}
	rowid, _ := res.LastInsertId()
	mustExec(`INSERT INTO pages_fts (rowid, title, body) VALUES (?,?,?)`, rowid, "Acme Corp", "Dana Lee is the CEO of Acme Corp, a widget manufacturer. [01ARRIVAL]")

	embedder, err := embedopenai.New(os.Getenv("OPENAI_API_KEY"))
	if err != nil {
		t.Fatalf("embedder construct (checkpoint RED): %v", err)
	}

	// 1) Catch-up: a real embed round-trip writes a page_vectors row.
	catchup := NewCatchup(CatchupOptions{Store: store, Embedder: embedder, Model: cfg.Embed.Model, Dims: cfg.Embed.Dims})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	catchup.sweep(ctx)

	vecs, err := store.LoadVectors(ctx, ModelTag(cfg.Embed.Model, cfg.Embed.Dims))
	if err != nil {
		t.Fatalf("load vectors (checkpoint RED): %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("page_vectors rows = %d, want 1 (checkpoint RED — catch-up did not embed)", len(vecs))
	}
	if got := len(vecs[0].Vector); got != cfg.Embed.Dims {
		t.Fatalf("vector dims = %d, want WIKI_EMBED_DIMS=%d (checkpoint RED)", got, cfg.Embed.Dims)
	}

	// 2) Hybrid retriever with the vector lane ON returns ranked whole-page hits.
	r := New(Options{
		Store:    store,
		Embedder: embedder,
		Model:    cfg.Embed.Model,
		Dims:     cfg.Embed.Dims,
		RRFk:     cfg.Retrieval.RRFk,
		LaneIn:   cfg.Retrieval.LaneIn,
	})
	hits, err := r.Search(ctx, "who runs the widget company", 5, true)
	if err != nil {
		t.Fatalf("hybrid search (checkpoint RED): %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("hybrid search returned no hits (checkpoint RED) — Acme is in the fixture")
	}
	found := false
	for _, h := range hits {
		if h.Subject == "01ACMEINTEGRATIONSUBJECT01" {
			found = true
			if h.Body == "" {
				t.Error("hit has empty body — a hit must be the whole page (checkpoint RED)")
			}
		}
	}
	if !found {
		t.Errorf("Acme subject not in hybrid hits (checkpoint RED): %+v", hits)
	}
}
