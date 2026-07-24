# Phase 47 — OpenAI subscription auth: the `auth` config key end to end

*Realizes design Decision 38 (subscription auth), plus the `auth` slices of Decisions 2 (Config struct), 3 (Validation), and 29 (`/complete`).*

`prompt.Config` gains the `auth` field (`""`/`"key"`/`"sub"`, stored verbatim; D2). `internal/provider` gains `ResolveAuthPath`, the `SubAuth` lazy-singleton store handle over `agentkit/openai/subscription`, and `NewBuilder(sub *SubAuth)` returning the existing factory seam type, dispatching `auth:"sub"` to `openai.New(openai.Subscription(store))` and everything else to the unchanged key path (D38). `validateConfig`/`prompt.ValidateConfig` take the `subAuthAvailable func() bool` probe and enforce the auth vocabulary, the openai-only and no-`base_url` constraints, file presence for `"sub"`, and the key-check skip under `"sub"` (D3). The composition root (`cmd/prompts/main.go`) constructs one `SubAuth` from `ResolveAuthPath(os.Getenv)` and injects the builder into the runner and both inference executors' completion path, and the probe into validation; the `/complete` config vocabulary carries `auth` through to the factory (D29). The MCP tool schema names `auth` among the config keys. `prompts/.envrc` gains `export PROMPTS_OPENAI_AUTH_PATH="$HOME/.secrets/openai-auth.json"`. `BuildEmbedder` and `/embed` are untouched.

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim in `*_test.go` files:
  - R-JTBA-4RDB (rewritten: every optional key including `auth` round-trips) — D2
  - R-SVPV-O479, R-SWXS-1VXY, R-SY5O-FNON, R-SZDK-TFFC, R-T0LH-7761 — D3
  - R-T1TD-KYWQ — D29
  - R-T319-YQNF, R-T496-CIE4, R-T6OZ-41VI, R-T7WV-HTM7 — D38
- `go build ./...`, `go test ./...` green from `prompts/`; `gofmt -l .` emits nothing.
- `grep -n 'PROMPTS_OPENAI_AUTH_PATH' .envrc` returns exactly one match.
