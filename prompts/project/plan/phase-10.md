# Phase 10 — Pin v0.1.1, purge local agentkit dependency, add guard

*Realizes design Decision 1 (Module dependency — completion, repinned to v0.1.1). Depends on Phase 7, Phase 8, and Phase 9.*

With every local `agentkit/...` usage migrated (Phases 7–9), `prompts/go.mod` is cut over to depend solely on the published package:

- `require github.com/ikigenba/agentkit` is bumped from `v0.1.0` to **`v0.1.1`**.
- `require agentkit v0.0.0` and `replace agentkit => ../agentkit` are removed entirely.
- The `appkit` and `eventplane` replace directives are untouched.
- `prompts/go.sum` is reconciled for the new pin.

All edits stay within `prompts/`: the repo-root `go.work` and `go.work.sum` are not touched (the workspace already maps `github.com/ikigenba/agentkit` to the local published tree, so workspace builds resolve v0.1.1 without a sum change).

A guard test under `prompts/` asserts the invariant going forward: no Go file in the `prompts` module imports a non-`github.com/ikigenba/agentkit` `agentkit/...` path (i.e. the deprecated local module is gone for good).

End state: `prompts` depends only on `github.com/ikigenba/agentkit v0.1.1`; the deprecated local `agentkit` module is referenced nowhere in `prompts/` (source, tests, or `go.mod`).

**Done when:** D1 is structurally complete — `cd prompts && GOWORK=off go build ./... && go test ./...` is green (and the workspace build is green too), and the import-guard test passes, proving zero references to the local `./agentkit`.
