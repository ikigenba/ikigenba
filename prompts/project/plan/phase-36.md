# Phase 36 — MCP surface: model-only required schema and a catalog-generated describe inventory

*Realizes design Decision 9. Depends on Phase 34.*

`internal/mcp/tools.go`: `configSchema()`'s required array shrinks to `["model"]` (provider stays present as an optional property). The `describe` tool renders its model inventory at call time from `catalog.ListByProvider` over the five providers — chat entries only — listing each model's catalog name, default provider, alternate routes, context size, and reasoning vocabulary rendered from its `ReasoningSpec`; the surrounding prose covers provider optionality/override and per-model reasoning validation. The test asserting provider-in-required (tagged R-KF9H-0MPT — an id design no longer mints) is removed with the behavior.

**Done when:**

- `go build ./...` and `go test ./...` are green from `prompts/`, `gofmt -l .` is empty.
- `grep -rn 'R-KF9H-0MPT' --include='*_test.go' .` from `prompts/` returns no matches.
- These ids are covered by clearly-named tests tagged verbatim in `*_test.go`:
  - R-20UM-JESS — the `config` schema's `required` array in both `create` and `update` descriptors is exactly `["model"]`.
  - R-222I-X6JH — the `describe` text contains every chat model name the catalog carries (asserted by iterating the catalog package, not a hard-coded list), including at least one openrouter-routed model.
- The test tagged R-KE1K-MUZ4 still passes.
