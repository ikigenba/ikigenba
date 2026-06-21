# Phase 1 — Config struct and four-provider validation

*Realizes design Decision 2 (Config struct) and Decision 3 (Validation). No prior phase dependency.*

Two changes land together in `prompts/internal/prompt/` because they are tightly coupled: the Config struct defines the fields that validateConfig must check.

**`prompts/go.mod`**: add `require github.com/ikigenba/agentkit v0.1.0`. The existing `replace agentkit => ../agentkit` pair stays — it is not removed until Phase 06. After this change both module paths coexist: old code still imports `agentkit/...` (local), new code imports `github.com/ikigenba/agentkit/...` (published).

**`prompts/internal/prompt/model.go`**: expand `Config` from its current narrow shape to the 16-field struct specified in D2 — `provider` and `model` as required string fields, plus 11 optional generation/retry/tuning fields using basic Go types (pointer-to-float for nullable floats, string for duration values, pointer-to-bool for nullable bool). The `agentkit/model` import is removed; Config carries no agentkit types.

**`prompts/internal/prompt/service.go`**: rewrite `validateConfig` to accept a `getenv func(string) string` parameter and execute the three-step validation from D3 in order — provider membership check, model pricing check via `provider.New("").Pricing(model)`, API key presence check. The hardcoded `cfg.Provider = string(model.ProviderAnthropic)` normalization in `Create` and `Update` is removed. Existing call sites pass `os.Getenv`. The `agentkit/model` import is removed; the function imports the four published provider sub-packages.

The runner is untouched; the build stays green because the local agentkit replace remains in effect.

**Done when:** R-JTBA-4RDB, R-JUJ6-IJ40, R-JVR2-WAUP, R-JWYZ-A2LE, R-JY6V-NUC3, R-JZES-1M2S are each covered by a clearly-named test and `go test ./...` is green.
