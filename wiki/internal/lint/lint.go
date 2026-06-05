package lint

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
	"wiki/internal/ingest"
	"wiki/internal/jobstore"
	"wiki/internal/search"
	"wiki/internal/store"
)

// ClientFactory builds a provider.Client for the lint model. main.go supplies the
// real anthropic client factory (which closes over ANTHROPIC_API_KEY); tests
// supply a stub. Returning an error lets lint fail the job cleanly when no client
// can be built (e.g. a missing key at run time), rather than panicking. Identical
// in shape to ingest.ClientFactory.
type ClientFactory func() (provider.Client, error)

// Config carries the injected lint knobs (model + cost ceiling + TTL), read at
// cmd/wiki/main.go's composition root (PLAN Decision 3: model + cost ceiling are
// config, not hardcoded). A zero MaxTokens applies the package default; a zero
// TTL means the job is bounded only by Cancel and the model.
type Config struct {
	Model     string        // lint model id (default DefaultModel)
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

// Linter is the wiki's maintenance pass. It owns the injected collaborators and
// exposes Lint (the manual trigger entrypoint) and JobStatus (the owner-scoped
// status read, identical in contract to ingest's). It is owner-agnostic: every
// method takes owner+collection.
//
// Like ingest.Core, Linter constructs a fresh owner-scoped job.Runner per Lint,
// each over an owner-bound jobstore.Store; the shared single-writer *sql.DB + the
// global partial-unique running index enforce single-flight ACROSS runners — and
// across packages, since lint reuses ingest's flight key. Crash recovery is the
// ingest core's one-shot boot sweep over the whole table (lint shares the
// wiki_jobs table), so Linter does not need its own Recover.
type Linter struct {
	store     *store.Store
	search    search.Index
	db        *sql.DB
	newClient ClientFactory
	cfg       Config
	now       func() time.Time
}

// New builds a Linter. It takes the same collaborators as ingest.New (the
// filesystem store, the BM25 index, the shared single-writer DB, a client
// factory, and config), so main.go constructs it from the same dependency graph.
func New(st *store.Store, idx search.Index, db *sql.DB, newClient ClientFactory, cfg Config) *Linter {
	return &Linter{
		store:     st,
		search:    idx,
		db:        db,
		newClient: newClient,
		cfg:       cfg.withDefaults(),
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// Result is what Lint returns: the spawned job id. Poll it with JobStatus (the
// same wiki_job_status path ingest uses — lint rows live in the same wiki_jobs
// table).
type Result struct {
	JobID string
}

// Lint is the manual lint trigger. It spawns an async agentkit job that runs the
// lint agent (read+write+glob+grep) over the existing owner+collection page tree
// and, on success, re-indexes the collection so any consolidated/merged pages
// stay searchable. It returns the job id immediately; the pass runs in the
// background.
//
// Single-flight: the job uses ingest.FlightKey(owner, collection) — the SAME key
// ingest uses — so a lint while an ingest is running (or a second lint, or an
// ingest while a lint runs) is rejected with job.ErrFlightInUse. Only one
// write-pass touches a given wiki at a time. On rejection, no job is launched and
// an empty job id is returned alongside the error.
func (l *Linter) Lint(ctx context.Context, owner, collection string) (Result, error) {
	if collection == "" {
		collection = store.DefaultCollection
	}

	// Ensure the collection root + page dirs exist so the confined agent loop has a
	// sandbox to run in (idempotent; a lint over a never-ingested wiki just finds an
	// empty tree and logs that there was nothing to do).
	root, err := l.store.EnsureLayout(owner, collection)
	if err != nil {
		return Result{}, fmt.Errorf("lint: ensure layout: %w", err)
	}

	jobID := ids.NewULID()
	runner := job.New(jobstore.New(l.db, owner, collection), l.cfg.JobTTL)
	j := &lintJob{
		linter:      l,
		owner:       owner,
		collection:  collection,
		sandboxRoot: root,
	}

	rec := job.Record{
		ID:        jobID,
		FlightKey: ingest.FlightKey(owner, collection), // shared write-pass key.
		StartedAt: l.now(),
	}
	if _, err := runner.Spawn(rec, j); err != nil {
		// Single-flight rejection (ErrFlightInUse) or any Insert error: no job was
		// launched. Return an empty job id with the error so the caller learns it.
		return Result{}, err
	}

	return Result{JobID: jobID}, nil
}

// JobStatus reads the lint job's owner-scoped status, reusing ingest's Status
// projection and the same wiki_jobs table the wiki_job_status verb already reads.
// A missing or foreign-owned id returns ingest.ErrJobNotFound. Lint jobs are thus
// observable through the exact same status path as ingest jobs.
func (l *Linter) JobStatus(ctx context.Context, owner, collection, jobID string) (ingest.Status, error) {
	// Delegate to a throwaway ingest.Core over the same db: the status read is
	// owner-scoped over wiki_jobs and carries no ingest-specific state, so a lint
	// job id reads identically. (ingest.Core.JobStatus only touches the db + the
	// jobstore.)
	return ingest.New(l.store, l.search, l.db, l.ingestClientShim(), ingest.Config{}).
		JobStatus(ctx, owner, collection, jobID)
}

// ingestClientShim adapts lint's ClientFactory to ingest.ClientFactory so the
// throwaway ingest.Core used solely for JobStatus is constructible. It is never
// invoked (JobStatus runs no agent), so a nil newClient is tolerated.
func (l *Linter) ingestClientShim() ingest.ClientFactory {
	if l.newClient == nil {
		return func() (provider.Client, error) { return nil, fmt.Errorf("lint: no client") }
	}
	nc := l.newClient
	return func() (provider.Client, error) { return nc() }
}

// lintJob is the unit of work the runner spawns: it runs the real agent loop
// (freeform, sch=nil) with the lint toolset + lint prompt, confined to the owner+
// collection root, then on success re-indexes the collection. It implements
// agentkit/job.Job — the SAME interface the ingest integrationJob implements.
type lintJob struct {
	linter      *Linter
	owner       string
	collection  string
	sandboxRoot string

	// stream captures the agent's wire output so usage can be extracted (mirrors
	// ingest's integrationJob.stream).
	stream bytes.Buffer
}

// Run executes the lint pass. It builds the provider client, runs the agent loop
// over a fresh wire session confined to sandboxRoot with the lint toolset/prompt,
// and on success re-indexes the collection. A reindex failure fails the job (the
// merges landed but are not searchable — surfaced, not silent). Usage is captured
// from the wire stream regardless of outcome.
func (j *lintJob) Run(ctx context.Context) (string, error) {
	client, err := j.linter.newClient()
	if err != nil {
		return "", fmt.Errorf("lint job: build client: %w", err)
	}

	req := provider.Request{
		Model:        j.linter.cfg.Model,
		MaxTokens:    j.linter.cfg.MaxTokens,
		SystemPrompt: systemPrompt(),
		Messages: []provider.Message{{
			Role: provider.RoleUser,
			Blocks: []provider.Block{provider.TextBlock{
				Text: userMessage(j.owner, j.collection),
			}},
		}},
		Tools: lintToolset(),
	}

	sess := wire.NewSession(&j.stream)
	if err := agent.Run(ctx, client, sess, req, nil /* freeform */, j.sandboxRoot, nil /* no tracer */); err != nil {
		return captureUsage(j.stream.Bytes()), fmt.Errorf("lint job: agent run: %w", err)
	}

	// On success, re-index so consolidated/merged/flagged pages stay searchable.
	if err := search.ReindexCollection(ctx, j.linter.search, storeAdapter{j.linter.store}, j.owner, j.collection); err != nil {
		return captureUsage(j.stream.Bytes()), fmt.Errorf("lint job: reindex: %w", err)
	}

	return captureUsage(j.stream.Bytes()), nil
}

// storeAdapter wraps *store.Store to satisfy search.PageSource (store.PageEntry →
// search.PageEntry). Mirrors ingest's adapter; lives here so the lint job can call
// ReindexCollection with the real store.
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
// wire stream — the same best-effort scan ingest's core uses, reused here to
// populate Record.UsageJSON.
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
