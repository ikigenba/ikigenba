# Phase 41 — Overridable extract prompt for the eval (`-prompt-file`)

*Realizes design Decision 24 (the overridable extract prompt). Depends on Phase 04 (the `internal/extract` stage + `renderPrompt`), Phase 35 (the `extract.New(client, site)` call-site construction at the composition root and eval), and Phase 38/40 (the `cmd/eval-extract` binary and the `Scorecard`).*

`extract` gains an opt-in instruction-prompt override that only the eval uses; the live service is untouched and runs the baked-in prompt as before. The observable end state:

- `internal/extract` exports `DefaultPromptInstructions` (the current baked-in instruction preamble) and an `Option`/`WithPromptInstructions(string)`; `New` becomes `New(c, site, opts ...Option)` so every existing call still compiles. An `Extractor` with no option renders `DefaultPromptInstructions`; one built with `WithPromptInstructions(alt)` renders `alt` in its place. In **both** cases `renderPrompt` still appends the document header and source text, so the document is always injected by the harness — the override owns only the instructions (and thus the JSON-shape contract).
- The composition root (`cmd/wiki`) still calls `extract.New(client, site)` with no option → production extract behavior is byte-identical to today.
- `cmd/eval-extract` gains `-prompt-file <path>`: absent → the default (production) prompt; present → its contents become `WithPromptInstructions(...)`. An unreadable path or empty/blank contents is a **startup error** (non-zero exit) before any dataset load or LLM call, matching the missing-API-key rule.
- `eval.Scorecard` gains a `Prompt` field stamping which instruction prompt produced the run (`"default"` or the `-prompt-file` path); `WriteHuman` prints it in the header block beside model/config/judge and `WriteJSON` carries it and round-trips. (The exact prompt text of each call remains traceable via the existing `-record` JSONL sink.)

**Done when:**
- R-ODAP-34N6 (D24) — a clearly-named test asserts the no-override path renders `DefaultPromptInstructions`: a capturing mock behind `extract.New(client, site)` confirms the request carries the production preamble plus the case header + source text.
- R-OEIL-GWDV (D24) — a test asserts `WithPromptInstructions(alt)` swaps the instructions: the captured request contains `alt` and the source text and does **not** contain the default preamble.
- R-OFQH-UO4K (D24) — a test asserts `eval-extract -prompt-file` fails loudly (non-zero exit, before any LLM call) on an unreadable path and on empty/blank contents, and runs the default with no `-prompt-file`.
- R-OGYE-8FV9 (D24) — a test asserts `Scorecard.Prompt` is stamped (`"default"` vs the override path) and that `WriteHuman` emits it and `WriteJSON` carries it and round-trips into an equal `Scorecard`.
- The suite is green per design *Conventions*.
