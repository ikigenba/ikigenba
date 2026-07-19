# Phase 38 — Admission gate and the shared provider factory

*Realizes design Decision 31 (admission control; the gate-level slice: R-67FV-VQ6I, R-68NS-9HX7, R-6CBH-ET5A — the runner-level id R-6B3L-11EL lands with Phase 41) and the rewritten Decision 4 (provider factory relocation; structural).*

Build `internal/admit`: the `Gate` with per-provider call semaphores (`PROMPTS_MAX_INFLIGHT_CALLS`, default 8) and the global run semaphore (`PROMPTS_MAX_CONCURRENT_RUNS`, default 8), ctx-aware blocking acquires, both caps in `ManifestExtras`. Relocate the provider factory per the rewritten D4: new leaf package `internal/provider` with `Build` (moved verbatim from `runner`) and the new `BuildEmbedder` (openai/google); `runner` consumes `provider.Build` through its existing injectable seam. No behavior change to runs in this phase.

**Done when:** the suite is green and these ids are covered by tagged tests:

- R-67FV-VQ6I — cap-1 same-provider acquires serialize
- R-68NS-9HX7 — distinct providers never contend
- R-6CBH-ET5A — canceled ctx aborts a blocked acquire without holding a slot

plus the structural check: `go build ./...` compiles with `buildProvider` defined only in `internal/provider` (`grep -rn "func Build(" internal/provider` matches; `grep -n "func buildProvider" internal/runner/runner.go` is empty).
