# Phase 24 — Recorder-stamped record identity and time

*Realizes design Decision 15 (recorder-stamped id/time) and 16 (caller-facing
record shape without id/time).*

The `appkit/telemetry` recorder becomes the sole writer of a record's wire
identity and timestamp. The caller-facing `Record` struct loses its `ID` and
`Time` fields; `Record()` stamps each accepted entry into an internal wire
envelope with a minted Crockford ULID (`Options.NewID`, default
`logging.NewULID`) and the acceptance instant in UTC (`Options.Now`, default
`time.Now`), so every batch the recorder POSTs is protocol-valid — ingest's
"id must be 26 Crockford base32 characters" validation can no longer reject
chassis traffic. `Options` gains the two injected seams; existing appkit tests
that set `ID:`/`Time:` on records are rewritten against the new shape (no
production code sets either field).

**Done when:**
- The following ids are covered by clearly-named tagged tests and the suite is
  green (`go test ./...` in `appkit/`, plus `GOWORK=off go build ./...`):
  - R-PKUI-T1UT — 1000 records at a live sink: every `id` matches
    `^[0-9A-HJKMNP-TV-Z]{26}$`, all pairwise distinct.
  - R-PNAB-KLC7 — every arriving `time` parses RFC3339Nano with `Z` offset,
    inside the test's captured before/after UTC window.
  - R-POI7-YD2W — pinned `Options.Now` + deterministic `Options.NewID` appear
    verbatim, in enqueue order, at the sink.
- The rewritten behaviors keep their existing tags green: R-1AU5-EUU8,
  R-1C21-SMKX, R-1FPQ-XXT0 (records distinguished by `op`), and R-1I5J-PHAE
  (wire-envelope contract keys; caller-facing `Record` exposes no `id`/`time`
  field, asserted via reflection).
- `grep -rn 'ID:\|Time:' --include='*.go'` over `appkit/` (excluding
  `project/`) finds no site constructing a `telemetry.Record` with an `ID:` or
  `Time:` field — the fields no longer exist to set (a clean compile enforces
  this; the grep is the smoke).
