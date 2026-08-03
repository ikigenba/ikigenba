# Phase 52 — The normative telemetry contract docs

*Realizes design Decision 14 (the telemetry contract docs). No pending-phase
dependencies; this is the first suite-level unit of the telemetry rollout and
the ground truth every other workspace's build reads.*

Two committed documents, produced/amended exactly as D14 states them:

- **`docs/telemetry-protocol.md`** (new): the record allowlist and field set,
  the seven kinds with their `op` idioms, capture-under-threshold (1024-byte
  per-value cap, 8192-byte per-record budget, the `$elided` representation,
  SHA-256 lowercase hex), the correlation rules, the loopback ingest API with
  its best-effort semantics, the recursion boundary, lifecycle records, and the
  scope exclusions.
- **`docs/correlation-ids.md`** (amended in place, staying the single current
  statement): Crockford base32 confirmed as the sole alphabet;
  `X-Correlation-Id` named as the HTTP transport; the event-payload-field
  convention superseded by the first-class envelope field; the edge
  strip-and-mint rule added.

**Done when** (deterministic; all greps repo-root, `project/` excluded by
path):

- `grep -c 'X-Correlation-Id' docs/telemetry-protocol.md` ≥ 1, and the doc
  contains the exact strings `"$elided"`, `1024`, `8192`, `/ingest`,
  `lifecycle`, and each of the seven kind names `edge`, `request`, `outbound`,
  `publish`, `consume`, `root`.
- `grep -c 'Crockford' docs/correlation-ids.md` ≥ 1 and
  `grep -c 'X-Correlation-Id' docs/correlation-ids.md` ≥ 1 and
  `grep -ci 'supersed' docs/correlation-ids.md` ≥ 1.
- The suite is green per design Conventions (`go test ./...` from the repo root
  exits 0 — docs-only change, nothing may break).
