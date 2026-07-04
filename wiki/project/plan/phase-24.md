# Phase 24 — The LLM-call footprint: recorder seam + `llm_calls` store

*Realizes design Decision 13 (the LLM-call footprint). Depends on Phase 3 (D5 the `llm` seam — `JSON[T]`, `Converse`, `sendText`, `CallSite`) and Phase 2 (the data model, stores, and migration runner).*

Give the service a durable, inspectable record of every call it makes to an LLM, so a later phase can list and a later release can diagnose the structured-content failures. Recording happens at the **one seam every provider round-trip funnels through** — `sendText` in `internal/llm` — so extract's parse-retries, compile's tighten loop, and ask's calls are all captured by the same mechanism, each round-trip its own row.

**What gets built (the observable end state):**

- `internal/llm` — a `Recorder` interface and `CallRecord` struct; `Client` gains an injected `Recorder` (nil = no-op). `CallSite` gains a `Stage` field (`"extract"|"compile"|"ask"`). New `WithJobID(ctx, id)` / `JobID(ctx)` context helpers. `sendText` builds **one** `CallRecord` per provider round-trip — capturing `Stage` (from the `CallSite`), `JobID` (from ctx; `""` when unset), `Attempt` (the `JSON[T]` loop's 1-based counter; `1` for a single shot), `Provider`/`Model`, `Params` (the resolved temperature/reasoning), the `{system,user}` `Request` actually sent (the corrective re-prompt on a retry, not the original), the `Response` (`""` on transport error), `Usage` when the provider reports it, `Err`, and the timestamps — and hands it to the recorder. The recorder call uses a **detached context** (`context.WithoutCancel`) so a cancelled call still records.
- `internal/wiki` — `LLMCallStore`, a SQLite-backed `Recorder` over a new `llm_calls` table (new migration via `bin/create-migration wiki`, matching the live TEXT-`RFC3339Nano` / no-`FOREIGN KEY` style, with the `llm_calls_job` and `llm_calls_time` indexes). Records are append-only — no update, no delete.
- The composition root injects the `LLMCallStore` as the `Client`'s recorder, and the worker's per-job `WithJobID` wrapping arrives with the lifecycle work in Phase 25 (extract/compile records carry `JobID==""` until then; ask records always do).

**Done when:**

- R-VNS0-1Z85 — a single successful round-trip produces exactly one record carrying the `CallSite`'s `Stage`/`Model`/`Params`, the `{system,user}` `Request`, the `Response`, and empty `Err` (via a capturing recorder mock).
- R-VOZW-FQYU — a `JSON[T]` call scripted bad-then-good emits two records (`Attempt 1` bad, `Attempt 2` good), the second's `Request` carrying the corrective re-prompt.
- R-VRFP-7AG8 — a provider transport/stream error emits one record with non-empty `Err` and empty `Response`.
- R-VSNL-L26X — a round-trip under `WithJobID(ctx,"J")` records `JobID=="J"`; with no job on the context, `JobID==""`.
- R-VTVH-YTXM — `Record` invoked with an already-cancelled context still persists the row to a real temp SQLite (detached write).
- R-VV3E-CLOB — `LLMCallStore.Record` round-trips every `llm_calls` column against a real migrated temp SQLite, and a nil `Recorder` on the `Client` is a no-op (no panic, no row).
- The suite is green (per design *Conventions*: `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...`, `bin/check-migrations wiki`).
