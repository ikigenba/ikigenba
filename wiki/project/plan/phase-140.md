# Phase 140 — Chat call-site defaults move to OpenRouter at medium effort

*Realizes design Decision 19 (per-call-site configuration), the R-A25B-A1V6 slice, plus the trued default-call-site ids of Decisions 6 and 7 (R-9UTW-ZFF0, R-9X9P-QYWE).*

The four production chat `DefaultCallSite()`s — `extract.DefaultCallSite`,
`compile.DefaultCallSite`, `ask.DefaultSubjectCallSite`,
`ask.DefaultSynthesisCallSite` — carry `Provider "openrouter"` and
`Effort "medium"` (model stays `gpt-5.6-luna`, `MaxTokens` unchanged, no
`Temperature`/`Thinking` pins), and the single in-code default model constant's
provider is `openrouter`. The D34 embed site is untouched (direct `openai`).
Existing tests asserting the retired `openai`/`low` defaults are updated in
place under the same id tag.

**Done when:**

- R-A25B-A1V6 — with all per-site knobs unset, every resolved site equals its
  stage default: `Provider "openrouter"`, `Model "gpt-5.6-luna"`,
  `Effort "medium"`, `MaxTokens >= 16384`, nil `Temperature`, nil `Thinking` —
  covered by a tagged test.
- R-9UTW-ZFF0 / R-9X9P-QYWE — `extract.DefaultCallSite()` and
  `compile.DefaultCallSite()` carry the openrouter/medium defaults per their
  trued D6/D7 statements — their existing tagged tests updated in place.
- The suite is green per design's Conventions (`go test ./...` from `wiki/`).
