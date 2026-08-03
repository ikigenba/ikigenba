# Phase 3 — The loopback-only, never-self-recorded ingest endpoint

*Realizes design Decision 3 (loopback ingest endpoint). Depends on Phase 02.*

`internal/ingest` gains the receiving half of the suite ingest API, and
`cmd/telemetry`'s `Handlers` hook mounts it.

- `ingest.Mount(rt, store, clock)` registers `POST /ingest` through
  `Router.HandleLoopback` (appkit's `LoopbackOnly` guard — telemetry writes no
  guard of its own). Exclusion from request recording is **not** done in this
  package: it is the `Spec.TelemetryExclude: []string{"/ingest"}` entry the
  composition root already carries from phase 1, which the chassis passes to
  `server.Options.RecordExclude`. This phase must keep the route path and that
  entry in agreement.
- The `Request`/`Response` envelope types from D3, with the two-tier validation:
  a malformed **envelope** (non-JSON, non-object, missing/non-array `records`,
  or over the 8 MiB cap) is a 400/413 storing nothing; a malformed **record**
  inside a well-formed batch is rejected individually, logged at warn with the
  failing field and the record id (never its contents), and counted — the
  response is 202 with `{"stored":N,"rejected":M}` even when `M > 0` or
  `N == 0`.
- A positive `dropped` count is persisted via `Store.NoteDropped`, attributed to
  the batch's service, and `DroppedTotal` is surfaced through the service's
  `health` details so an investigator sees the store is incomplete without
  issuing a query.
- The write is synchronous: the batch is durable before the 202.

Transport tests bind a real `127.0.0.1` listener on an ephemeral port and speak
real HTTP through the registered route; storage assertions read real SQLite.

**Done when:**

- Every id below is covered by a clearly-named, id-tagged test:
  - R-VIUF-3BD6 — a real loopback POST of a well-formed batch returns 202 with
    `rejected: 0` and every record is afterwards readable with its fields and
    verbatim `params` bytes intact.
  - R-VK2B-H33V — a mixed batch stores exactly the well-formed records and
    reports the malformed count, across all four malformed cases D3 names.
  - R-VLA7-UUUK — a malformed envelope answers 400 and stores nothing, and a
    refused request's `dropped` count is not applied.
  - R-VMI4-8ML9 — `X-Forwarded-Proto: https` yields a bare 404 storing nothing,
    while the byte-identical request without it returns 202 and stores.
  - R-VNQ0-MEBY — `dropped` counts accumulate across batches and a zero/absent
    count adds nothing.
  - R-VOXX-062N — `Spec.TelemetryExclude` contains exactly the path this
    package registers (asserted against the route pattern, not a duplicated
    literal), and after three real loopback POSTs the store holds exactly the
    posted records and no record whose `service` is `telemetry` with an `op`
    containing `/ingest`.
  - R-VQ5T-DXTC — an over-cap body answers 413, stores nothing, and is not
    fully buffered before the decision.
- The service writes no loopback guard of its own:
  `grep -rn 'X-Forwarded-Proto' --include='*.go' --exclude='*_test.go' --exclude-dir=project .`
  run from `telemetry/` returns empty (exit 1) — the guard is appkit's.
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`,
  `go test ./...` all exit 0 in `telemetry/`.
