package index

import (
	"context"
	"log/slog"
	"time"

	"agentkit/embed"

	"wiki/internal/page"
)

// The async catch-up embedder (design §9.3): an in-process goroutine that keeps
// page_vectors current WITHOUT ever touching the integration commit. It wakes on
// a nudge (a run completed — the same contentless-doorbell pattern the worker
// pool uses) or a periodic tick, runs the work-list query (vector missing /
// behind version / wrong model), and batches embed API calls to refresh the
// stale rows. Stale vectors serve until replaced, so a slow or failed catch-up
// degrades retrieval recall but never correctness or liveness.
//
// First deploy and a model/dims change are the SAME code path: a new WIKI_EMBED_*
// config makes every row wrong-model, so the next sweep re-embeds the whole
// corpus. No special migration.

// catchupStore is the page-store surface the catch-up worker needs.
type catchupStore interface {
	VectorWorkList(ctx context.Context, model string, limit int) ([]page.VectorWork, error)
	UpsertVector(ctx context.Context, subject string, embeddedVersion int, model string, vector []float32) error
}

// CatchupOptions configures the embedder goroutine. Embedder may be nil (absent
// key) — then the worker is a no-op that never starts an embed call.
type CatchupOptions struct {
	Store    catchupStore
	Embedder embed.Embedder
	Model    string // WIKI_EMBED_MODEL
	Dims     int    // WIKI_EMBED_DIMS
	Logger   *slog.Logger

	// BatchSize is the number of pages embedded per HTTP request (the caller owns
	// chunking to the provider's array limit — embed lib hides no fan-out). 0 → 64.
	BatchSize int
	// Interval is the periodic fallback sweep cadence (the nudge is an optimization,
	// not the truth — a missed nudge is caught by the tick). 0 → 5 minutes.
	Interval time.Duration
}

// Catchup is the async catch-up embedder. Construct once; Run launches its loop
// on a context; Nudge wakes it after a run completes.
type Catchup struct {
	store     catchupStore
	embedder  embed.Embedder
	modelTag  string
	model     string
	dims      int
	log       *slog.Logger
	batchSize int
	interval  time.Duration

	nudge chan struct{}
}

// NewCatchup builds the catch-up embedder. A nil Embedder makes Run a no-op.
func NewCatchup(opts CatchupOptions) *Catchup {
	bs := opts.BatchSize
	if bs <= 0 {
		bs = 64
	}
	iv := opts.Interval
	if iv <= 0 {
		iv = 5 * time.Minute
	}
	return &Catchup{
		store:     opts.Store,
		embedder:  opts.Embedder,
		modelTag:  ModelTag(opts.Model, opts.Dims),
		model:     opts.Model,
		dims:      opts.Dims,
		log:       opts.Logger,
		batchSize: bs,
		interval:  iv,
		// Buffered depth 1: a nudge while a sweep is mid-flight coalesces into one
		// pending wake (a missed nudge loses nothing — the next sweep re-scans).
		nudge: make(chan struct{}, 1),
	}
}

// Nudge wakes the catch-up worker (contentless doorbell). Safe to call from any
// goroutine, including the worker pool's run-completion path. A nudge that finds
// the channel full is dropped — the pending wake already covers it.
func (c *Catchup) Nudge() {
	if c == nil {
		return
	}
	select {
	case c.nudge <- struct{}{}:
	default:
	}
}

// Run drives the catch-up loop until ctx is cancelled. With no embedder wired
// (absent key) it returns immediately — the vector lane is simply never populated
// and the retriever stays lexical-only. It runs one sweep on boot (first deploy
// populates from empty), then sweeps on each nudge or interval tick.
func (c *Catchup) Run(ctx context.Context) error {
	if c.embedder == nil {
		if c.log != nil {
			c.log.Info("wiki embed catch-up disabled (no embedder; lexical-only)")
		}
		return nil
	}

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.sweep(ctx) // boot sweep

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.nudge:
			c.sweep(ctx)
		case <-ticker.C:
			c.sweep(ctx)
		}
	}
}

// sweep runs one full catch-up pass: pull the whole work list, embed it in
// batches, upsert each batch. A per-batch error is logged and the sweep moves on
// (best-effort: a stale row serves until the next pass replaces it). It loops
// until the work list is empty (a single nudge drains all pending work).
func (c *Catchup) sweep(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		work, err := c.store.VectorWorkList(ctx, c.modelTag, c.batchSize)
		if err != nil {
			if c.log != nil {
				c.log.Error("wiki embed catch-up: work list", slog.String("err", err.Error()))
			}
			return
		}
		if len(work) == 0 {
			return // corpus is current
		}
		if err := c.embedBatch(ctx, work); err != nil {
			if c.log != nil {
				c.log.Error("wiki embed catch-up: batch", slog.String("err", err.Error()))
			}
			return // back off; the next nudge/tick retries
		}
		if len(work) < c.batchSize {
			return // last (partial) batch drained the work list
		}
	}
}

// embedBatch embeds one batch of pages in a single API call and upserts each
// resulting vector under the current model tag, stamping the page version the
// text was read at.
func (c *Catchup) embedBatch(ctx context.Context, work []page.VectorWork) error {
	texts := make([]string, len(work))
	for i, w := range work {
		texts[i] = w.Text
	}
	res, err := c.embedder.Embed(ctx, c.model, c.dims, texts)
	if err != nil {
		return err
	}
	for i, w := range work {
		if i >= len(res.Vectors) {
			break // provider returned fewer vectors than inputs — skip the tail
		}
		if err := c.store.UpsertVector(ctx, w.Subject, w.Version, c.modelTag, res.Vectors[i]); err != nil {
			return err
		}
	}
	return nil
}
