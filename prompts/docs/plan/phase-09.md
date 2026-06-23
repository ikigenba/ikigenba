# Phase 9 — Finish runner off local agentkit

*Realizes design Decision 7 (Runner — completion). Depends on Phase 4 and Phase 8.*

`prompts/internal/runner/runner.go` carries no local `agentkit/...` imports. The live `execute` path already drives `agentkit.Conversation.Send` (published agentkit); this phase removes the dead legacy code the half-migration left beside it:

- The unused `buildRequest` function — the sole consumer of `agentkit/provider`, `agentkit/tools` (the `legacytools` alias), and `agentkit/model`'s `Resolve`/`DefaultEffort`/`ModelContext` — is deleted.
- The `FramingPrompt` constant (which the published agentkit does not provide) is copied into `prompts/internal/runner/` and used by `buildSystemPrompt`, replacing the `agentkit/agent` import.
- The imports `agentkit/agent`, `agentkit/model`, `agentkit/provider`, and `agentkit/tools` are removed.

The tests that exercised `buildRequest` — `runner_maxtokens_test.go` and `runner_event_test.go` — are retargeted at the live `Conversation` path (system-prompt framing, gen-settings mapping, retry-policy mapping, event/user-message assembly, and the max-output-token default), dropping their `agentkit/model` and `agentkit/provider` imports.

End state: the runner package builds and tests pass with no non-`github.com/ikigenba/agentkit` `agentkit/...` import remaining.

**Done when:** D7's Verification ids remain covered by clearly-named tests and the suite is green — R-K5I9-YGS9 (run uses the injected provider), R-K6Q6-C8IY (output JSONL has a `turn_start` and a `summary` LogRecord), R-K7Y2-Q09N (completed run's `usage_json` is a non-empty object), R-K95Z-3S0C (a 50ms-TTL run against a blocking provider is `failed` with `run TTL exceeded`).
