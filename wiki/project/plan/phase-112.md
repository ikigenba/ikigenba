# Phase 112 — Extract and compile call sites: rehoused prompts, system/user split, gpt-5.6-luna defaults

*Realizes design Decision 6 (extract stage) and 7 (compile stage) slices, plus the D19 defaults for those two sites.*

The extract instruction preamble moves to `internal/extract/prompt.txt`, embedded package-locally as `DefaultPromptInstructions` (the import of `wiki/eval/extract` goes; the `eval/` tree itself is untouched until Phase 115). The compile instruction preamble is lifted out of `renderPrompt` into `internal/compile/prompt.txt`, embedded and exported. Both stages send their preamble as the site's `System` message and only the rendered input as the user message (`extract.Render` and compile's `renderPrompt` stop prepending instructions; compile's tighten retry appends only the corrective note to the rendered input). Both `DefaultCallSite()` constructors carry `Provider "openai"`, `Model "gpt-5.6-luna"`, `Effort "low"`, `MaxTokens 16384`, no temperature, no thinking field; `internal/wiki`'s per-site resolution builds from them. Tests asserting the retired temperature/thinking defaults (the R-W2HP-H0J0, R-4CK8-E688, R-FWOT-NRHN, R-4DS4-RXYX tags) are removed with their behaviors.

**Done when:**
- R-9UTW-ZFF0 — extract's default call site carries openai / gpt-5.6-luna / effort low / MaxTokens ≥ 16384 / retries 2, no temperature or thinking, and the composition root builds from it (bad-then-good extraction re-prompts).
- R-9W1T-D75P — a captured extract `/complete` request carries `system` = the embedded `internal/extract/prompt.txt` and a user message of only the rendered header + source text.
- R-9X9P-QYWE — compile's default call site carries the same openai/luna/low/≥16384 shape with no temperature or thinking, and the composition root builds from it.
- R-9YHM-4QN3 — a captured compile `/complete` request carries `system` = the embedded `internal/compile/prompt.txt` and a user message of only the rendered identity + claims (corrective note only on the tighten retry).
- The retired ids R-W2HP-H0J0, R-4CK8-E688, R-FWOT-NRHN, R-4DS4-RXYX appear in no test file.
- The suite is green (design Conventions).
