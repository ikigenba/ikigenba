# Phase 122 — Prompts calls on the chassis client, carrying the chain id

*Realizes design Decision 64 (instrumented client + chain id) and the `R-KIH2-R4UC` slice of Decision 65 (the sweep's one-root-per-cycle rule, proven on the wire). Depends on Phase 121.*

**Cross-workspace dependency.** Needs the new `appkit`: the shared instrumented
outbound HTTP client, the correlation middleware, and the context accessor.
Phase 121 must land first — this phase asserts a worker call carries the job
row's stored correlation id, and that column arrives in 120.

**What gets built.**

- `internal/llm` — `Client` stops constructing `&http.Client{}`; `New` takes an
  `*http.Client` instead. `cmd/wiki/main.go` obtains the chassis-provided
  instrumented outbound client from the `Router` inside `Spec.Handlers` and
  hands it over, requesting no client-level timeout. Nothing else about
  `/complete` or `/embed` changes: same request shapes, same status taxonomy,
  same reliance on per-call context deadlines.
- `internal/ask` — `Asker.Ask` stops calling `ids.New()` for correlation and
  reads the chain id from `ctx` into `Attribution.GroupID`, threading the one
  value through the whole fan-out. `internal/ids` keeps `New()` for entity ids.
- `internal/wiki` — the worker threads the job's chain id (Phase 121) onto the
  context its stages run under, so both the outbound header and `group_id`
  carry it; the catch-up sweep's per-cycle root (also Phase 121) reaches its
  `wiki.embed-page` calls the same way.
- `internal/wiki` — D35's `R-6YVT-TFOD` test is updated in place: the
  after-commit embed is still attributed to its job, but the value it asserts
  is now the job's chain id (stored or rooted), never `job.ID`. Same id, same
  behavior, corrected expression — D35 was rewritten to match.
- The two D62 correlation behaviors this seal deleted from design — "one ask
  mints exactly one fresh correlation id" and "every worker call carries
  `group_id` equal to the job's id" — no longer exist, so their tagged tests are
  deleted rather than left asserting a rule design no longer states. They are
  replaced by `R-XNXS-O9AJ` and `R-XP5P-2118`. D62's two remaining origin
  behaviors and their tests stay untouched.

**Done when:**

- `R-XLHZ-WPT5` — driven through the real `internal/llm` client against the
  httptest prompts server, every `/complete` and `/embed` request issued under a
  context carrying chain id `X` arrives with header `X-Correlation-Id: X`; the
  same calls under a bare context arrive with that header absent or empty.
- `R-XMPW-AHJU` — a source-scan test over the wiki tree asserts zero
  `http.Client{` constructions in non-test `.go` files (`*_test.go` excluded).
- `R-XNXS-O9AJ` — one ask driven under chain id `X` posts `group_id` exactly `X`
  on `wiki.embed-query`, every `wiki.ask-subject`, and `wiki.ask-synthesis`; a
  second ask under chain id `Y` posts `Y`; neither posts a `group_id` absent
  from its context.
- `R-XP5P-2118` — a worker-processed job whose row stores correlation id `X`
  while `job.ID` is a different ULID posts `group_id` `X` — not `job.ID` — on
  its extract, compile, and embed-page calls, captured at the httptest prompts.
- `R-KIH2-R4UC` — one drain cycle of the catch-up embedding sweep over several
  stale pages issues every `wiki.embed-page` call of that cycle under a single
  correlation id (one distinct id across the cycle, not one per page), and a
  second drain cycle uses a different id — captured at the httptest prompts.
- `R-6YVT-TFOD` stays green with its updated assertion: the after-commit embed
  carries its job's chain id, not `job.ID`.
- `grep -rn 'ids\.New()' internal/ask` from `wiki/` returns no matches (exit
  status 1): the ask path no longer mints a correlation id of its own.
- The suite is green per design's *Conventions*: `go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
  zero failures.
