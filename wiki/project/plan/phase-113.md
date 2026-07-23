# Phase 113 — Ask call sites: rehoused analysis and synthesis prompts, system/user split, luna defaults everywhere

*Realizes design Decision 36 (analysis) and 9 (synthesis) slices, plus the D19 all-sites default. Depends on Phase 112.*

The analysis instruction preamble moves to `internal/ask/analysis-prompt.txt`, embedded as `DefaultAnalysisInstructions` (the import of `wiki/eval/analysis` goes); the synthesis preamble is lifted out of `synthPrompt` into `internal/ask/synthesis-prompt.txt`, embedded and exported. Both ask sites send their preamble as `System` and only the rendered input as the user message (the question for analysis; the question + retrieved page bodies for synthesis). `ask.DefaultSubjectCallSite()` / `ask.DefaultSynthesisCallSite()` carry `Provider "openai"`, `Model "gpt-5.6-luna"`, `Effort "low"`, `MaxTokens 16384`; the in-code default model constant becomes `gpt-5.6-luna` with provider openai, and `internal/wiki`'s resolution yields the four D19 defaults with all knobs unset. The retired R-GIY9-26PA test goes with its behavior.

**Done when:**
- R-A0XE-WA4H — a captured analysis `/complete` request carries `system` = the embedded `internal/ask/analysis-prompt.txt` and a user message of only the question.
- R-9ZPI-IIDS — a captured synthesis `/complete` request carries `system` = the embedded `internal/ask/synthesis-prompt.txt` and a user message of only the question + retrieved page bodies.
- R-A25B-A1V6 — with all per-site knobs unset, each of the four resolved sites carries openai / gpt-5.6-luna / effort low / MaxTokens ≥ 16384 with nil temperature and nil thinking.
- The retired id R-GIY9-26PA appears in no test file.
- The suite is green (design Conventions).
