# Phase 34 — Adopt agentkit v0.6.0: module bump, typed credentials, catalog-backed validation

*Realizes design Decision 1 (module dependency), 3 (validation), and 4 (provider factory), with Decision 2's provider-optional/base-url updates.*

Bump `prompts/go.mod` to `github.com/ikigenba/agentkit v0.6.0` and bring the module compiling and validating against the v0.6.0 surface:

- `internal/runner/runner.go`: `buildProvider` moves to the typed-credential constructors (`anthropic.APIKey(...)` etc.), gains the `openrouter` case (`OPENROUTER_API_KEY`, `openrouter.New(openrouter.APIKey(key), ...)`), and applies `WithBaseURL` uniformly for any provider when `base_url` is set.
- `internal/prompt/service.go`: `validateConfig` is rewritten per D3 — signature `(Config, getenv) (Config, error)`; catalog membership (chat entries only), provider derivation/route check via `catalog.Resolve`, reasoning-control check via `entry.Reasoning.Accepts`, five-provider env-var map. `providerRegistry` and `applyProviderDefault` are removed; `Create`, `Update`, and `Import` store the validated (provider-filled) config.
- `internal/prompt/model.go`: `Config.Provider` gains `omitempty`; comments true up to the catalog-name model contract (struct shape otherwise unchanged).
- Existing tests tagged R-JVR2-WAUP, R-JWYZ-A2LE, R-JY6V-NUC3, R-JZES-1M2S are updated to the catalog world (same behaviors, catalog-backed mechanism); every model literal used in tests must be a catalog name.

**Done when:**

- `go build ./...` and `go test ./...` are green from `prompts/` (design Conventions), `gofmt -l .` is empty.
- `grep -c 'agentkit v0.6.0' go.mod` returns 1.
- These ids are covered by clearly-named tests tagged verbatim in `*_test.go`:
  - R-1ONM-PPDU — empty provider derives and stores the model's catalog default provider.
  - R-1PVJ-3H4J — explicit provider without a catalog route for the model is rejected.
  - R-1R3F-H8V8 — an enum reasoning level outside the model's `Levels` is rejected.
  - R-1SBB-V0LX — a `thinking_budget` outside the model's range and sentinels is rejected.
  - R-1TJ8-8SCM — `thinking: false` on a `CanDisable: false` model is rejected.
  - R-1UR4-MK3B — a reasoning control the model's spec accepts validates to nil error.
  - R-1VZ1-0BU0 — openrouter-resolved model with empty `OPENROUTER_API_KEY` is rejected naming the var.
- Tests tagged R-JVR2-WAUP, R-JWYZ-A2LE, R-JY6V-NUC3, R-JZES-1M2S, R-JTBA-4RDB, R-JUJ6-IJ40 still pass.
