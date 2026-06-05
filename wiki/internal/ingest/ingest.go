package ingest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"agentkit/agent"
	"agentkit/job"
	"agentkit/provider"
	"agentkit/wire"

	"wiki/internal/ids"
	"wiki/internal/jobstore"
	"wiki/internal/search"
	"wiki/internal/store"
)

// ClientFactory builds a provider.Client for the ingest model. main.go supplies
// the real anthropic client factory (which closes over ANTHROPIC_API_KEY); tests
// supply a stub. Returning an error lets ingest fail the job cleanly when no
// client can be built (e.g. a missing key at run time), rather than panicking.
type ClientFactory func() (provider.Client, error)

// Config carries the injected ingest knobs. All come from cmd/wiki/main.go's
// env-reading composition root (PLAN Decision 3: model + cost ceiling are config,
// not hardcoded in the agent). A zero MaxTokens lets the agent loop / backend
// apply their own fallback; a zero TTL means the job is bounded only by Cancel
// and the model.
type Config struct {
	Model     string        // ingest model id (default DefaultModel)
	MaxTokens int           // per-job output-token ceiling (cost knob)
	JobTTL    time.Duration // per-run wall-clock TTL (0 = no deadline)
}

// withDefaults fills unset Config fields with package defaults.
func (c Config) withDefaults() Config {
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultMaxTokens
	}
	return c
}

// Core is the ingest pipeline. It owns the injected collaborators and exposes
// Ingest (the trigger entrypoint reused by wiki_ingest_text and, later,
// wiki_ingest_url) and JobStatus (the owner-scoped status read). It is
// owner-agnostic: every method takes owner+collection as arguments.
//
// The agentkit job.Runner holds a single Store, but wiki's job rows carry a
// per-owner+collection scope (stamped on Insert). So Core constructs a fresh
// owner-scoped job.Runner per Ingest, each over an owner-bound jobstore.Store.
// The runners are cheap (a store handle, a ttl, and an in-flight cancel map);
// the underlying *sql.DB is shared and single-writer, which is what preserves
// single-flight across runners (the partial-unique index is global). Boot-time
// crash recovery is a one-shot SweepRunning over the whole table (not
// owner-scoped), handled by Recover at main.go startup.
type Core struct {
	store     *store.Store
	search    search.Index
	db        *sql.DB
	newClient ClientFactory
	cfg       Config
	now       func() time.Time
	// fetch resolves a URL to extracted markdown + a derived title. It defaults to
	// FetchAndExtract (real HTTP + pure-Go HTML→markdown); tests stub it so
	// IngestURL never touches the network. Reused by wiki_ingest_url.
	fetch FetchFunc
}

// New builds a Core. search may be a *search.BM25Index; db is the service's
// single-writer handle (backing both wiki_jobs and wiki_ingest).
func New(st *store.Store, idx search.Index, db *sql.DB, newClient ClientFactory, cfg Config) *Core {
	return &Core{
		store:     st,
		search:    idx,
		db:        db,
		newClient: newClient,
		cfg:       cfg.withDefaults(),
		now:       func() time.Time { return time.Now().UTC() },
		fetch:     FetchAndExtract,
	}
}

// SetFetch overrides the URL fetch/extract func (tests inject a stub so
// IngestURL stays hermetic). main.go uses the default.
func (c *Core) SetFetch(f FetchFunc) {
	if f != nil {
		c.fetch = f
	}
}

// Recover runs the boot-time crash sweep over the whole wiki_jobs table: every
// row left 'running' by a crash is flipped to 'failed'. main.go calls it once on
// startup before serving. It is not owner-scoped (a crash orphans every owner's
// runs). Returns the number of rows swept.
func (c *Core) Recover(ctx context.Context) (int, error) {
	return jobstore.New(c.db, "", "").SweepRunning(ctx)
}

// Result is what Ingest returns to the MCP verb: the spawned job id plus the
// raw-store outcome (sha256 + whether the bytes were already present).
type Result struct {
	JobID      string
	Sha256     string
	RawRelPath string
	AlreadyHad bool
}

// Ingest is the async ingest core. It:
//  1. persists content to the immutable raw/ store (sha256-keyed, idempotent) and
//     records provenance in wiki_ingest;
//  2. pre-creates the page tree (the agent's write tool does not mkdir);
//  3. spawns an async agentkit integration job (the agent files/updates pages),
//     gated single-flight per (owner, collection);
//  4. on job success, re-indexes the collection so new/updated pages are
//     searchable (done inside the job, after agent.Run succeeds).
//
// It returns the job id immediately; the integration runs in the background.
//
// Re-ingest policy: identical bytes are a safe no-op on disk (immutable raw) and
// in wiki_ingest (INSERT OR IGNORE). We STILL spawn the integration job on a
// re-ingest — re-running the integration pass over the same raw doc is safe (the
// agent reads index first and updates rather than duplicates), and a prior failed
// integration is the common reason identical bytes are re-submitted. AlreadyHad
// is surfaced so the caller can tell.
func (c *Core) Ingest(ctx context.Context, owner, collection string, content []byte, meta store.RawMeta) (Result, error) {
	if collection == "" {
		collection = store.DefaultCollection
	}

	// 1. Immutable raw write (sha256-keyed, idempotent).
	raw, err := c.store.WriteRaw(owner, collection, content, meta)
	if err != nil {
		return Result{}, fmt.Errorf("ingest: write raw: %w", err)
	}

	// Record provenance in the queryable ledger. INSERT OR IGNORE: a re-ingest of
	// identical bytes (same owner/collection/sha256) is a no-op here, matching the
	// immutable raw store.
	if err := c.recordIngest(ctx, owner, collection, raw, meta); err != nil {
		return Result{}, fmt.Errorf("ingest: record provenance: %w", err)
	}

	// 2. Pre-create the page tree so the agent's confined write tool (which does
	// NOT create parent dirs) can write into sources/concepts/entities/events/….
	root, err := c.store.EnsureLayout(owner, collection)
	if err != nil {
		return Result{}, fmt.Errorf("ingest: ensure layout: %w", err)
	}

	// 3. Build the integration job and spawn it (single-flight per owner+collection).
	// A per-call owner-scoped runner over an owner-bound jobstore: the shared
	// single-writer DB + the global partial-unique running index enforce
	// single-flight across runners.
	jobID := ids.NewULID()
	runner := job.New(jobstore.New(c.db, owner, collection), c.cfg.JobTTL)
	j := &integrationJob{
		core:        c,
		owner:       owner,
		collection:  collection,
		sandboxRoot: root,
		raw:         raw,
		meta:        meta,
	}

	rec := job.Record{
		ID:        jobID,
		FlightKey: FlightKey(owner, collection),
		StartedAt: c.now(),
	}
	if _, err := runner.Spawn(rec, j); err != nil {
		// Single-flight rejection (ErrFlightInUse) or any Insert error: no job was
		// launched. Surface the raw outcome so the caller still learns the bytes
		// were persisted, with an empty job id.
		return Result{JobID: "", Sha256: raw.Sha256, RawRelPath: raw.RelPath, AlreadyHad: raw.AlreadyHad}, err
	}

	return Result{JobID: jobID, Sha256: raw.Sha256, RawRelPath: raw.RelPath, AlreadyHad: raw.AlreadyHad}, nil
}

// IngestURL is the wiki_ingest_url trigger. It differs from Ingest only in how the
// bytes are produced: the service fetches the URL server-side and extracts
// HTML→markdown (pure-Go), then feeds the SAME async ingest core with the
// extracted markdown as content and source defaulted to the URL. There is no
// path-based ingest verb (GOALS): a URL is fetched by the service, never a local
// path.
//
// meta.Source defaults to rawURL when the caller did not supply one; meta.Title
// defaults to the extracted <title> (or a URL-derived title) when the caller did
// not supply one. The caller's explicit title/source/tags always win.
func (c *Core) IngestURL(ctx context.Context, owner, collection, rawURL string, meta store.RawMeta) (Result, error) {
	markdown, title, err := c.fetch(ctx, rawURL)
	if err != nil {
		return Result{}, fmt.Errorf("ingest url: fetch+extract: %w", err)
	}
	if len(markdown) == 0 {
		return Result{}, fmt.Errorf("ingest url: %s produced no content", rawURL)
	}
	if meta.Source == "" {
		meta.Source = rawURL
	}
	if meta.Title == "" {
		meta.Title = title
	}
	return c.Ingest(ctx, owner, collection, markdown, meta)
}

// FlightKey serializes WRITE passes over one (owner, collection) wiki. Two
// ingests into the same wiki would have their integration agents racing on
// index.md / log.md and the same touched pages, so we run them one at a time.
// Per-sha256 would let two passes into one collection clobber each other; per
// owner+collection is the right granularity (the wiki is the shared mutable
// surface).
//
// The lint pass mutates the SAME surface (consolidates/merges/flags pages,
// rewrites index.md, appends log.md), so it MUST share this key: a lint while an
// ingest runs (or vice-versa) is rejected single-flight (job.ErrFlightInUse).
// That is why this is exported — internal/lint reuses the exact same string so
// only one write-pass runs per collection. The "ingest" literal prefix is kept
// for backward-compatibility with already-running rows; it names the shared
// write-pass family, not the ingest verb specifically.
func FlightKey(owner, collection string) string {
	return "ingest\x00" + owner + "\x00" + collection
}

// recordIngest writes one provenance row to wiki_ingest, idempotent on
// (owner, collection, sha256) via INSERT OR IGNORE — so re-ingesting identical
// bytes does not duplicate the ledger.
func (c *Core) recordIngest(ctx context.Context, owner, collection string, raw store.RawDoc, meta store.RawMeta) error {
	tagsJSON, err := json.Marshal(nonNilTags(meta.Tags))
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	_, err = c.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO wiki_ingest
		   (id, owner, collection, sha256, title, source, tags, raw_path, ingested_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ids.NewULID(), owner, collection, raw.Sha256,
		meta.Title, meta.Source, string(tagsJSON), raw.RelPath, raw.IngestedAt,
	)
	if err != nil {
		return err
	}
	return nil
}

func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

// integrationJob is the unit of work the runner spawns: it runs the real agent
// loop (freeform, sch=nil) against the ingest client, confined to the owner+
// collection root, then on success re-indexes the collection. It implements
// agentkit/job.Job.
type integrationJob struct {
	core        *Core
	owner       string
	collection  string
	sandboxRoot string
	raw         store.RawDoc
	meta        store.RawMeta

	// stream captures the agent's wire output so usage can be extracted (mirrors
	// the agentkit e2e template's captureUsage).
	stream bytes.Buffer
}

// Run executes the integration pass. It builds the provider client, runs the
// agent loop over a fresh wire session confined to sandboxRoot, and on success
// re-indexes the collection. A reindex failure fails the job (the pages landed
// but are not searchable — surfaced, not silent). Usage is captured from the
// wire stream regardless of outcome.
func (j *integrationJob) Run(ctx context.Context) (string, error) {
	client, err := j.core.newClient()
	if err != nil {
		return "", fmt.Errorf("ingest job: build client: %w", err)
	}

	req := provider.Request{
		Model:        j.core.cfg.Model,
		MaxTokens:    j.core.cfg.MaxTokens,
		SystemPrompt: systemPrompt(),
		Messages: []provider.Message{{
			Role: provider.RoleUser,
			Blocks: []provider.Block{provider.TextBlock{
				Text: userMessage(j.raw.RelPath, j.raw.Sha256, j.meta.Title, j.meta.Source, j.meta.Tags),
			}},
		}},
		Tools: integrationToolset(),
	}

	sess := wire.NewSession(&j.stream)
	if err := agent.Run(ctx, client, sess, req, nil /* freeform */, j.sandboxRoot, nil /* no tracer */); err != nil {
		return captureUsage(j.stream.Bytes()), fmt.Errorf("ingest job: agent run: %w", err)
	}

	// On success, re-index so the new/updated pages are searchable. This runs only
	// on the success path (a failed agent.Run returns above).
	if err := search.ReindexCollection(ctx, j.core.search, storeAdapter{j.core.store}, j.owner, j.collection); err != nil {
		return captureUsage(j.stream.Bytes()), fmt.Errorf("ingest job: reindex: %w", err)
	}

	return captureUsage(j.stream.Bytes()), nil
}

// storeAdapter wraps *store.Store to satisfy search.PageSource (store.PageEntry →
// search.PageEntry). Identical to search_test's adapter; lives here so the ingest
// core can call ReindexCollection with the real store.
type storeAdapter struct{ s *store.Store }

func (a storeAdapter) WalkPages(owner, collection string) ([]search.PageEntry, error) {
	entries, err := a.s.WalkPages(owner, collection)
	if err != nil {
		return nil, err
	}
	out := make([]search.PageEntry, len(entries))
	for i, e := range entries {
		out[i] = search.PageEntry{RelPath: e.RelPath}
	}
	return out, nil
}

func (a storeAdapter) ReadPage(owner, collection, relPath string) ([]byte, error) {
	return a.s.ReadPage(owner, collection, relPath)
}

// captureUsage extracts the accounting blob from the last result event in the
// wire stream — the best-effort scan agentkit's e2e template uses and ralph's
// runner does, reused here to populate Record.UsageJSON.
func captureUsage(streamed []byte) string {
	var out json.RawMessage
	for _, line := range bytes.Split(streamed, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev struct {
			Type  string          `json:"type"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal(line, &ev); err != nil || ev.Type != "result" {
			continue
		}
		if len(ev.Usage) > 0 && !bytes.Equal(bytes.TrimSpace(ev.Usage), []byte("null")) {
			out = ev.Usage
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(map[string]json.RawMessage{"usage": out})
	return string(b)
}
