# Phase 8 — Validation via published provider registries

*Realizes design Decision 3 (Validation — completion). Depends on Phase 1.*

`validateConfig` and `providerModelSupported` in `prompts/internal/prompt/service.go` validate the configured model through the **published** agentkit instead of the local `agentkit/model` package. For each of the four providers, a minimal handle is constructed with an empty API key (`anthropic.New("")`, `openai.New("")`, `google.New("")`, `zai.New("")`) and the model is checked via `provider.Pricing(cfg.Model)` — `!ok` rejects the model with a `*ValidationError` naming it. The validation order is unchanged and fails fast: unknown provider string → unknown model → missing API key env var (via the injected `getenv`).

Removed from `service.go`: the `agentkit/model` import, the hardcoded `zaiModels` allowlist map, and any hardcoded `cfg.Provider` anthropic normalization. The provider env var map (anthropic→`ANTHROPIC_API_KEY`, openai→`OPENAI_API_KEY`, google→`GEMINI_API_KEY`, zai→`ZAI_API_KEY`) is retained exactly.

End state: no `agentkit/model` import in `service.go`; create/update validate every model against the published provider's own `Pricing` registry, so a model the runtime can actually run (e.g. `gemini-3.1-flash-lite`) is accepted, and a model absent from the registry is rejected.

**Done when:** D3's Verification ids are covered by clearly-named tests and the suite is green — R-JVR2-WAUP (unknown provider rejected), R-JWYZ-A2LE (valid provider + unrecognised model rejected), R-JY6V-NUC3 (valid provider + known model + empty key rejected), R-JZES-1M2S (valid provider + known model + key set returns nil).
