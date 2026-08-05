# Phase 33 — Work-gate the uploader's root chain

*Realizes design Decision 26 (correlation chains), slice R-NF5W-7DOI, with
R-TDZG-5R4G's uploader half restated to match.*

The uploader ticks every second and today opens a `dropbox:upload/drain` root
chain at the top of every pass, before checking the queue — ~86,400 pure-noise
root records/day on an idle box. D26 §3 now specifies the work-gated shape:
the pass first reads the due rows (a local SQLite query producing no telemetry
records) and returns without touching the recorder when none are due; only a
pass with at least one due row opens the root chain, exactly once, and drains
on that context. The sync engine's per-cycle root is deliberately unchanged.

What gets built:

- `internal/dropbox/uploader.go` — `drainUploads` reorders to query-then-gate:
  the due-rows read happens before, and without, any `StartRoot` call; the root
  opens only when the pass has due rows, and the drain of every row in that
  pass runs on the one root context.
- The tagged test for R-TDZG-5R4G's uploader half is updated so the drain pass
  it drives has due work (its claim now applies only to working passes).

**Done when:**

- R-NF5W-7DOI is covered by a genuinely-asserting tagged test (no skip, no
  build tag, no env gate): multiple consecutive idle passes (empty queue, and
  a queue whose only rows are retry-gated into the future) observe zero
  root-start invocations and zero telemetry records; one pass with two due
  rows observes exactly one.
- The R-TDZG-5R4G tagged tests still pass with the uploader half driven
  through a pass that has due work.
- The suite is green per design Conventions: from `dropbox/`,
  `go build ./...`, `go vet ./...`, and `go test ./...` all exit 0.
