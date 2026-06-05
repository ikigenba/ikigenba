# Search backend — Task 0.2 de-risking spike + `internal/search` interface

> Status: **design note** (durable deliverable of Task 0.2). The spike code that
> produced the evidence below was throwaway and has been deleted; it was never
> committed to the wiki service. Companion to `PLAN.md` Task 0.2 / Task 3.2 and
> `GOALS.md` (Query). `GOALS.md` + `PLAN.md` win on conflict.

## Confirmed: YES — modernc.org/sqlite does FTS5 + bm25() under CGO_ENABLED=0

`modernc.org/sqlite` (v1.50.1, the version already pinned by `ledger/go.mod`)
compiles an **FTS5** virtual table and the **`bm25()`** ranking function into its
pure-Go SQLite, and they work with **CGO off**. No C toolchain, no build tag, no
extension load is needed — FTS5 and bm25 are baked into the standard
`modernc.org/sqlite` amalgamation.

### How the spike was run (offline, no network fetch)

`modernc.org/sqlite v1.50.1` is already a dependency of the `ledger` module, so
it is in the module cache. The spike ran *inside* the ledger module to reuse that
dependency with zero network access:

1. Read `ledger/internal/db/db.go` for the exact open path: driver name
   `"sqlite"`, DSN
   `file:<path>?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)`,
   `SetMaxOpenConns(1)`.
2. Dropped a throwaway `ledger/internal/db/fts5spike_test.go` (package `db`) that
   opened a temp-file DB via the existing `db.Open(...)`, created
   `CREATE VIRTUAL TABLE docs USING fts5(title, body);`, inserted 4 markdown
   blobs with differing frequencies of the term "otter", and ran the bm25 query.
3. Ran from the repo root with CGO explicitly off and verbose:
   `CGO_ENABLED=0 go test ./ledger/internal/db/ -run FTS5 -v -count=1`.
4. Deleted the spike file; confirmed `git status` shows ledger unchanged (only
   `wiki/` added).

### Captured spike output (evidence)

```
=== RUN   TestFTS5BM25Spike
    fts5spike_test.go:92: FTS5 bm25() ranked results for MATCH 'otter' (ascending = most relevant first):
    fts5spike_test.go:94:   [0] score=-0.000002  title="Otters everywhere"
    fts5spike_test.go:94:   [1] score=-0.000001  title="Wildlife notes"
    fts5spike_test.go:94:   [2] score=-0.000001  title="A passing mention"
    fts5spike_test.go:113: SPIKE OK: modernc.org/sqlite FTS5 + bm25() works under this build.
--- PASS: TestFTS5BM25Spike (0.00s)
PASS
ok  	ledger/internal/db	0.007s
```

The "Beavers only" page (no "otter" term) is correctly excluded; the
highest-term-frequency page ranks first; scores ascend (more relevant = more
negative). It worked first try — no fallback needed.

### Cross-compile / pure-Go proof

The deploy target is `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`. Building the spike
test binary for that exact target succeeded and produced a **statically linked**
ELF with **no dynamic dependencies** (`ldd` → "not a dynamic executable"). So
FTS5 + bm25 survive the suite's deterministic cross-compile, not just the host
`go test`.

## Required build flags / tags / imports (so Task 3.2 reproduces it)

There are **none beyond the suite's existing convention**. Specifically:

- **Driver registration:** the blank import `_ "modernc.org/sqlite"` — already
  how `ledger/internal/db/db.go` does it. Open with driver name `"sqlite"`.
- **No build tag is required.** FTS5 and bm25 are compiled into
  `modernc.org/sqlite` by default at v1.50.1. (Do NOT use the `mattn/go-sqlite3`
  `sqlite_fts5` tag — that's the CGO driver and is the wrong, CGO path.)
- **No `EnableFTS5` flag, no extension `.Load()`, no `sqlite3_*` C calls.**
- **Pragmas:** reuse the suite's DSN pragmas (WAL, foreign_keys, busy_timeout).
  Single connection (`SetMaxOpenConns(1)`) matches the suite's single-writer
  discipline and avoids FTS5 write contention.
- **Version floor:** pin `modernc.org/sqlite >= v1.50.1` (whatever the suite is
  already on). wiki's `go.mod` will require it directly rather than borrowing
  ledger's.

## Chosen `internal/search` interface

Borrowed from qmd's `internal/store` schema/query SHAPE (FTS5 table fed from a
content table via triggers; `SELECT ..., bm25(tbl) AS rank ... ORDER BY rank`),
but **reimplemented** — qmd's store is an internal package and its vector path
needs CGO (`asg017/sqlite-vec` + a llama.cpp embedder), which is exactly why we
reimplement BM25-only on modernc. Key shape changes from qmd for the wiki:

- **Whole pages, not fragments/chunks.** qmd's `SearchResult` carries chunk
  snippets/offsets; wiki indexes and returns **whole markdown pages** (GOALS:
  "qmd ranks and returns *whole curated pages* (+ the index), not raw
  fragments"). No chunking, no snippet/offset machinery.
- **Owner + collection scoped.** Every method takes `owner` and `collection`
  (collection defaulted to `"default"` per Decision 4); the FTS table is keyed
  by `(owner, collection)` so results never cross an owner's boundary.
- **The collection's `index.md` page is always part of the result set** (GOALS:
  "+ the index"), surfaced as a dedicated field rather than relying on it to rank
  in via bm25 — index-first navigation is a hard contract, not luck.
- **An interface, so a later vector/hybrid backend is an additive swap** (GOALS
  "Later"; the qmd vector/CGO cost is paid then, deliberately, behind this same
  interface).

```go
// Package search is the wiki's ranked page-retrieval backend. The Phase-1
// implementation is BM25 over modernc.org/sqlite FTS5 (pure-Go, CGO_ENABLED=0).
// It is an interface so a later vector/hybrid backend is an additive swap.
package search

import "context"

// Page is a whole markdown page handed to the indexer. Path is the page's
// relative path within the owner+collection tree (e.g. "concepts/otters.md")
// and is the stable identity used for upsert/delete.
type Page struct {
	Path  string // relative path within the collection tree; unique per (owner, collection)
	Title string // page title (frontmatter title, or first H1, or derived from Path)
	Body  string // the full markdown body (whole page, not a fragment)
}

// Result is one ranked whole-page hit.
type Result struct {
	Path  string  // relative path within the collection tree
	Title string  // page title
	Body  string  // the full markdown page body
	Score float64 // raw SQLite bm25() score: LOWER (more negative) == MORE relevant
}

// Results is a ranked search response. Hits are ordered best-first
// (ascending bm25 score). Index is the collection's index.md page, always
// included so the caller can navigate index-first; it is nil only if the
// collection has no index page yet.
type Results struct {
	Hits  []Result // whole pages, best-first
	Index *Result  // the collection's index.md (navigation entry point), or nil
}

// Index is the search backend: it maintains a BM25-ranked index of whole
// markdown pages per (owner, collection) and answers ranked page queries.
//
// Implementations are owner-scoped: an owner+collection's pages never appear in
// another owner's results. The Phase-1 implementation is FTS5/bm25() on
// modernc.org/sqlite; vector/hybrid is a future implementation of this same
// interface.
type Index interface {
	// IndexPage upserts a single whole page into the (owner, collection) index,
	// keyed by page.Path. Re-indexing the same path replaces it (re-ingest safe).
	IndexPage(ctx context.Context, owner, collection string, page Page) error

	// IndexPages bulk-upserts pages for a collection (e.g. a full reindex after
	// an ingest integration pass). Implementations should do this in one
	// transaction.
	IndexPages(ctx context.Context, owner, collection string, pages []Page) error

	// RemovePage drops a page from the index by its relative path. Idempotent:
	// removing an absent path is not an error.
	RemovePage(ctx context.Context, owner, collection, path string) error

	// Search runs a BM25 query over the (owner, collection) index and returns
	// ranked whole pages plus the collection's index page. limit caps Hits
	// (limit <= 0 selects an implementation default).
	Search(ctx context.Context, owner, collection, query string, limit int) (Results, error)

	// Close releases the backend's resources (e.g. the SQLite handle).
	Close() error
}
```

Notes on the interface for Task 3.2:

- `Search`'s signature satisfies the Task 0.2 requirement —
  `Search(owner, collection, query)` returning ranked whole-page results with
  scores plus the collection's index page — with `ctx` and `limit` added as the
  obvious operational params. If Task 3.2 prefers the exact 3-arg shape from the
  brief, drop `limit` and apply a fixed internal cap; the `ctx` should stay.
- The reindex hook the ingest core needs (PLAN Task 4.1: "on success, re-index
  via internal/search") is `IndexPages` (whole-collection) or repeated
  `IndexPage` calls — both upsert by path, so re-ingest never duplicates rows.

## Implementation shape for Task 3.2 (the real `internal/search`)

Mirror qmd's trigger-fed FTS5 pattern, scoped per (owner, collection):

```sql
-- content table holds the durable whole-page rows; the FTS5 table is the index.
CREATE TABLE IF NOT EXISTS pages (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  owner      TEXT NOT NULL,
  collection TEXT NOT NULL,
  path       TEXT NOT NULL,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,
  UNIQUE(owner, collection, path)
);

CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
  title, body,
  tokenize = 'porter unicode61'  -- stemming + unicode folding, as qmd uses
);

-- AFTER INSERT/UPDATE/DELETE triggers keep pages_fts.rowid == pages.id in sync
-- (identical pattern to qmd's documents_ai/ad/au triggers).
```

Query (the proven shape):

```sql
SELECT p.path, p.title, p.body, bm25(pages_fts) AS score
  FROM pages_fts
  JOIN pages p ON p.id = pages_fts.rowid
 WHERE pages_fts MATCH ?
   AND p.owner = ? AND p.collection = ?
 ORDER BY score
 LIMIT ?;
```

The index page is fetched separately (a direct `SELECT ... WHERE path =
'index.md'`) and attached to `Results.Index`, independent of whether it matched
the query.

A single shared `*sql.DB` opened the same way as `ledger/internal/db/db.go`
(driver `"sqlite"`, WAL, `SetMaxOpenConns(1)`) is the right handle. The search
index lives at `<data-root>/<owner>/<collection>/.search/index.sqlite` per the
PLAN Task 3.1 layout — it can be its own SQLite file separate from `wiki.db`, or
a shared one; per-collection files keep owner isolation trivially physical.

### Gotchas that will affect Task 3.2

- **bm25() score sign/order.** SQLite's `bm25()` returns a value where **more
  relevant == more negative (smaller)**. `ORDER BY bm25(tbl)` ascending is
  best-first — do NOT add `DESC`. `Result.Score` carries the raw value; if the
  MCP surface ever wants "higher = better", negate at the edge, not in storage.
- **FTS5 query syntax is user-facing.** A raw user query goes through the FTS5
  query grammar, where bare punctuation/operators (`-`, `"`, `*`, `NEAR`, `OR`,
  `:`) have special meaning and can error. qmd defends by stripping quotes and
  wrapping the whole thing in double quotes (`"<query>"`) to force a phrase/term
  match. Task 3.2 should sanitize similarly (strip/escape `"`, decide phrase vs.
  term semantics) so a stray character doesn't return a SQL error to the agent.
- **Empty / no-match queries** return zero hits (not an error). `Search` should
  still return `Results.Index` populated so the agent always has the navigation
  entry point.
- **Tokenizer choice matters for recall.** Use `tokenize = 'porter unicode61'`
  (qmd's choice): porter stemming so "otters"/"otter" match, unicode61 for
  case/diacritic folding. This is a *pragma-equivalent* table option set at
  `CREATE VIRTUAL TABLE` time and is frozen for that table — pick it up front
  (it's baked into the 002/003 migration shape, and an FTS5 table cannot have its
  tokenizer altered later without a rebuild).
- **Reindex == upsert by path.** Because raw docs are immutable but wiki *pages*
  are rewritten by each integration pass, the index must upsert by
  `(owner, collection, path)` and the FTS triggers must delete+reinsert on UPDATE
  (qmd's `documents_au` does exactly this). Idempotent re-ingest depends on it.

## Fallback (NOT needed)

No fallback is required. modernc.org/sqlite does FTS5 + bm25() under
`CGO_ENABLED=0`, proven above. The shell-out-to-qmd path (rejected by Decision 1
in `PLAN.md`, and only to be documented if the spike failed) is **not** taken and
not documented here, because the spike succeeded.
