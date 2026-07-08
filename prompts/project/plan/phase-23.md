# Phase 23 — Runner wires deferred suite tools; framing prompt; agentkit `v0.2.0`

*Realizes design Decision 19 (progressive suite-tool discovery) and the
rewritten Decision 7 conversation assembly. Depends on Phase 22 (discovery
groups). **External precondition:** the published
`github.com/ikigenba/agentkit v0.2.0` tag (agentkit's phase 48) — local dev
builds resolve agentkit through the repo-root `go.work` replace and can proceed
before the tag exists, but the phase is not done until the committed `go.mod`
pin builds with `GOWORK=off`.*

Wire Phase 22's groups into the run: sandbox tools stay eager in
`Conversation.Tools`; every suite tool arrives via
`Conversation.DeferredTools`, so the model sees agentkit's generated catalog
(`load_tools`) instead of ~120 front-loaded schemas. Add the framing-prompt
guidance paragraph and bump the dependency.

## Steps

In **`prompts/internal/runner/`**:

- **Change `Runner.discover`'s type** to
  `func(ctx context.Context, owner, promptID string) []agentkit.DeferredToolGroup`
  (default closure still `suite.Discover(...)`; test injections update in
  kind).
- **Rework the conversation assembly** in `execute`:
  `Tools: runtools.All(sandboxRoot)` only; `DeferredTools:` the discover
  result. No other `Conversation` field changes.
- **Extend `framingPrompt`** (`framing_prompt.go`) with one guidance paragraph
  after the sandbox-tools sentence, per D19: it names `load_tools` (catalog in
  its description; load by exact name before calling) and names no individual
  service. `eventPreamble` is unchanged.

In **`prompts/go.mod`**: bump `github.com/ikigenba/agentkit` from `v0.1.1` to
`v0.2.0` (`go mod tidy`; the committed `go.sum` updates with it).

In the **runner tests**: fake `discover` values become groups; add a scripted
fake provider whose `RoundTrip` responses drive the full deferred path
(`load_tools` call → native call of the loaded tool → final text).

## Done when

The suite is green (design *Conventions* commands, from `prompts/`) and:

- **R-9NBD-XAU5** — a clearly-named test asserts (via a recording fake
  provider's first `Request`) that a run's tools contain the sandbox tools and
  no `ikigenba_*` tool definition; that `load_tools` is present when the
  injected groups are non-empty; and that it is absent when they are empty.
- **R-9OJA-B2KU** — a clearly-named test asserts the conversation `System`
  contains the deferred-tools guidance naming `load_tools` and contains no
  `ikigenba_` service enumeration.
- **R-9PR6-OUBJ** — a clearly-named test drives `Runner.execute` with a
  scripted fake provider that calls `load_tools` for a deferred suite tool and
  then calls that tool natively: the suite tool's `Call` reaches the fake peer,
  the run finishes `succeeded`, and `output.jsonl` carries the
  `tool_use`/`tool_result` `LogRecord` lines for both calls.
- `grep -n "agentkit v0.2.0" prompts/go.mod` matches, and the build is green
  with `GOWORK=off` once the published tag exists (the precondition above).
