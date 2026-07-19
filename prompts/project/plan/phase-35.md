# Phase 35 — Runner resolves the catalog route: wire model and pricing on the Conversation

*Realizes design Decision 7 (catalog-resolution slice: wire model + `Conversation.Pricing`). Depends on Phase 34.*

`internal/runner/runner.go`'s `execute` resolves the pinned config's route once at spawn via `catalog.Resolve(cfg.Provider, cfg.Model)`: the Conversation is built with `Model: wireModel` (the route's wire form — vendor-namespaced for openrouter routes) and `Pricing: entry.Pricing` (consumer-owned cost resolution). A pinned config that no longer resolves fails the run loudly with an error naming the provider and model. The injected fake provider in runner tests records the incoming `*agentkit.Request` so the wire model is assertable.

**Done when:**

- `go build ./...` and `go test ./...` are green from `prompts/`, `gofmt -l .` is empty.
- These ids are covered by clearly-named tests tagged verbatim in `*_test.go`:
  - R-1X6X-E3KP — a run pinned to an openrouter-routed catalog model reaches the provider with `Request.Model` equal to the route's wire slug, not the stored catalog name.
  - R-1ZMQ-5N23 — a run with non-zero usage and no provider-reported cost records a `usage_json` cost greater than zero, priced from the model's catalog rates.
- Tests tagged R-K5I9-YGS9, R-K6Q6-C8IY, R-K7Y2-Q09N, R-K95Z-3S0C still pass.
