// Package index is wiki's hybrid retrieval primitive (design §9.3): one retriever
// over two lanes — the lexical BM25 lane (FTS5, the page store) and a vector lane
// (brute-force cosine over page_vectors) — fused with Reciprocal Rank Fusion. It
// slots behind the read side's Retriever interface (P10) WITHOUT changing the
// search contract: a hit is still the whole page, rank order only, no scores.
//
// The vector lane is INDEPENDENTLY SWITCHABLE per call site (design §9.3, the
// config switches): the search verb, resolution's candidates, and lint-sweep each
// decide whether they fuse the vector lane in. The lane defaults OFF everywhere
// (FTS5-first) and turns on per site only when measurement (Part II) shows lift.
//
// Two degradation paths keep the read side alive without a key or a network:
//   - construction-time: no embedder (absent OPENAI_API_KEY) → the retriever is
//     lexical-only, exactly the page-store lane (the ANTHROPIC_API_KEY pattern).
//   - read-time: a configured vector lane whose query-embed call fails → that one
//     read falls back to lexical-only rather than erroring (design §9.3).
package index

import (
	"context"
	"math"
	"sort"

	"agentkit/embed"

	"wiki/internal/page"
)

// store is the page-store surface the retriever depends on, narrowed for testing:
// the lexical lane (SearchPages), the vector corpus (LoadVectors), and the
// id→whole-page resolve (WholePagesByIDs).
type store interface {
	SearchPages(ctx context.Context, query string, limit int) ([]page.WholePage, error)
	LoadVectors(ctx context.Context, model string) ([]page.PageVector, error)
	WholePagesByIDs(ctx context.Context, ids []string) ([]page.WholePage, error)
}

// Options configures the hybrid retriever (design §9.3). Embedder may be nil (no
// key → lexical-only). Model/Dims pin the embed call; RRFk and LaneIn tune the
// fuse (eval-harness knobs).
type Options struct {
	Store    store
	Embedder embed.Embedder
	Model    string // WIKI_EMBED_MODEL (+ Dims forms the page_vectors model tag)
	Dims     int    // WIKI_EMBED_DIMS
	RRFk     int    // WIKI_RRF_K (default 60)
	LaneIn   int    // per-lane top-N into the fuse (default 50)
}

// Retriever is the hybrid two-lane retriever. It is constructed once at the
// composition root and shared by the call sites (each passes its own per-site
// vector-lane switch to Search).
type Retriever struct {
	store    store
	embedder embed.Embedder
	modelTag string // the page_vectors model column value: "<model>@<dims>"
	model    string
	dims     int
	rrfK     int
	laneIn   int
}

// ModelTag composes the page_vectors model-column value from a model id and dims
// ("text-embedding-3-large@1024"). The catch-up embedder stamps the same tag, so
// a model OR dims change makes prior rows read-invalid (cross-config cosine is
// garbage — design §9.3).
func ModelTag(model string, dims int) string {
	return model + "@" + itoa(dims)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// New builds the hybrid retriever. A nil Embedder (absent key) yields a
// lexical-only retriever — every Search ignores the vector switch and serves the
// BM25 lane (the construction-time degradation path).
func New(opts Options) *Retriever {
	rrfK := opts.RRFk
	if rrfK <= 0 {
		rrfK = 60
	}
	laneIn := opts.LaneIn
	if laneIn <= 0 {
		laneIn = 50
	}
	return &Retriever{
		store:    opts.Store,
		embedder: opts.Embedder,
		modelTag: ModelTag(opts.Model, opts.Dims),
		model:    opts.Model,
		dims:     opts.Dims,
		rrfK:     rrfK,
		laneIn:   laneIn,
	}
}

// Search returns up to limit whole-page hits for the query, best-first. When
// useVector is false (or no embedder is wired) it is the pure lexical lane. When
// true it fuses the BM25 lane with the brute-force-cosine vector lane via RRF.
// A vector-lane failure (embed error / no current-model vectors) degrades to the
// lexical result rather than erroring (design §9.3 read-side degradation).
func (r *Retriever) Search(ctx context.Context, query string, limit int, useVector bool) ([]page.WholePage, error) {
	if limit <= 0 {
		return nil, nil
	}

	// The lexical lane always runs (it is both a standalone result and an RRF input).
	lexical, err := r.store.SearchPages(ctx, query, r.laneIn)
	if err != nil {
		return nil, err
	}

	if !useVector || r.embedder == nil {
		return capPages(lexical, limit), nil
	}

	vector, verr := r.vectorLane(ctx, query)
	if verr != nil || len(vector) == 0 {
		// Read-time degradation: a failed/empty vector lane serves lexical-only.
		return capPages(lexical, limit), nil
	}

	fused := rrfFuse(r.rrfK, lexical, vector)
	return capPages(fused, limit), nil
}

// vectorLane embeds the query, scans the current-model page vectors with
// brute-force cosine, and returns the top-LaneIn subjects resolved to whole
// pages, best-first. An embed failure or an empty/absent corpus yields an error
// (treated as a read-time degradation by the caller).
func (r *Retriever) vectorLane(ctx context.Context, query string) ([]page.WholePage, error) {
	res, err := r.embedder.Embed(ctx, r.model, r.dims, []string{query})
	if err != nil {
		return nil, err
	}
	if len(res.Vectors) == 0 {
		return nil, nil
	}
	q := res.Vectors[0]

	corpus, err := r.store.LoadVectors(ctx, r.modelTag)
	if err != nil {
		return nil, err
	}
	if len(corpus) == 0 {
		return nil, nil
	}

	type scored struct {
		subject string
		score   float64
	}
	ranked := make([]scored, 0, len(corpus))
	for _, pv := range corpus {
		ranked = append(ranked, scored{subject: pv.Subject, score: cosine(q, pv.Vector)})
	}
	// Best-first; ties broken by subject id for determinism.
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].subject < ranked[j].subject
	})
	if len(ranked) > r.laneIn {
		ranked = ranked[:r.laneIn]
	}
	ids := make([]string, len(ranked))
	for i, s := range ranked {
		ids[i] = s.subject
	}
	return r.store.WholePagesByIDs(ctx, ids)
}

// cosine is the cosine similarity of two equal-length vectors. Mismatched
// lengths (a stale cross-dims row that slipped past the model-tag guard) score 0
// rather than panicking — defense in depth.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// rrfFuse merges two ranked whole-page lanes by Reciprocal Rank Fusion: each
// lane contributes 1/(k+rank) (rank 0-based) to a subject's score, summed across
// lanes, then sorted descending. RRF needs only rank position, never a per-lane
// score — which is exactly what both lanes provide (slice order). Ties break by
// subject id for determinism. The whole-page payload is taken from whichever lane
// first carried the subject (both carry the same page).
func rrfFuse(k int, lanes ...[]page.WholePage) []page.WholePage {
	type acc struct {
		page  page.WholePage
		score float64
	}
	scores := map[string]*acc{}
	var order []string
	for _, lane := range lanes {
		for rank, wp := range lane {
			a, ok := scores[wp.Subject]
			if !ok {
				a = &acc{page: wp}
				scores[wp.Subject] = a
				order = append(order, wp.Subject)
			}
			a.score += 1.0 / float64(k+rank)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		si, sj := scores[order[i]].score, scores[order[j]].score
		if si != sj {
			return si > sj
		}
		return order[i] < order[j]
	})
	out := make([]page.WholePage, 0, len(order))
	for _, id := range order {
		out = append(out, scores[id].page)
	}
	return out
}

// capPages truncates a result list to limit.
func capPages(pages []page.WholePage, limit int) []page.WholePage {
	if limit > 0 && len(pages) > limit {
		return pages[:limit]
	}
	return pages
}
