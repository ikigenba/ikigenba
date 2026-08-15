# Phase 173 — The `internal/match` package

*Realizes design Decision 97 (the match stage — package slice: R-7FBT-OXWC, R-7K7F-80V4) and Decision 19's match site wiring. Depends on Phase 171.*

Builds `internal/match`: the embedded `prompt.txt` (rulings-vs-observations framing, refutes/contradicts definitions, the no-edge-when-unsure default, output schema + worked example), `DefaultCallSite()` (Stage `match`, openrouter, `gpt-5.6-luna`, effort medium, MaxTokens 16384, MaxParseRetries 2), the `Judgment`/`Result` types, the render half (subject identity + numbered corrections/claims lists with `new`/`existing` markers, no ids), the parse/validate half (indices in range; empty judgments valid), and the mechanical new-pair filter (old pairs, same-job and non-backward correction→correction edges dropped). `Config.CallSites.Match` and the `MATCH_*` knobs resolve through the D19 resolver, and the composition root constructs the matcher from the default site.

**Done when:** the suite is green and each id is covered by a genuine tagged test:

- R-7FBT-OXWC — `DefaultCallSite` pins + composition-root construction + `MATCH_*` knob resolution.
- R-7K7F-80V4 — the mechanical filter stages only valid new-pair edges.
