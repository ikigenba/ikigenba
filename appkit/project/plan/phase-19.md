# Phase 19 — The telemetry recorder: ring, batching, ingest client, env config

*Realizes design Decision 15 (the `appkit/telemetry` recorder). Depends on
Phase 18.*

**End state.** `appkit/telemetry` gains its runtime half: `Options`, `New`,
`Record`, `Start`, and `Close`. A bounded ring of 4096 records drops the oldest
on overflow and counts the drops; a flush loop wakes every ~1s (or immediately
at 256 buffered records) and POSTs `{"records":[…]}` — at most 256 per batch —
to the ingest URL, adding `"dropped": N` when the counter is non-zero and
clearing that counter only on a 2xx. A failed or non-2xx batch is logged at
debug and discarded, never retried or re-queued. `Record` is safe on a nil
`*Recorder` and never blocks or errors. `Close` makes one final best-effort
flush. `appkit/config` resolves two new suite-wide (unprefixed) env vars onto
`Config`: `TELEMETRY_INGEST_URL` (default `registry.BaseURL("telemetry") +
"/ingest"`) and `TELEMETRY_ENABLED` (default true; `0`/`false`/`no` disable).

**Done when:**
- These Verification ids are covered by clearly-named tests tagged with the id
  verbatim, driven against a **live in-process HTTP sink** (`httptest.NewServer`)
  and a genuinely **closed** TCP port — never a stub that accepts anything:
  - R-1AU5-EUU8 — enqueuing 4096+64 records leaves the newest 4096; the 64
    oldest are gone.
  - R-1C21-SMKX — 1000 records enqueued against a live sink arrive there as
    `{"records":[…]}` bodies, with no batch exceeding 256 records.
  - R-1D9Y-6EBM — the next successful batch after N drops carries
    `"dropped": N`, and the batch after that carries none.
  - R-1EHU-K62B — against a closed listener, 5000 `Record` calls all return in
    well under a second with no error, and a live sink afterwards still
    receives records.
  - R-1FPQ-XXT0 — `Close` delivers a record enqueued immediately before it to a
    live sink, and returns nil.
  - R-1GXN-BPJP — default config yields `http://127.0.0.1:3008/ingest` and
    enabled=true; `TELEMETRY_INGEST_URL` overrides; `TELEMETRY_ENABLED=false`
    performs zero requests against a live sink.
- The suite is green per design's *Conventions* (`go build ./...`, `go vet
  ./...`, `gofmt -l .` empty, `go test ./...`, all from `appkit/`).
