# Phase 107 — Scorer keys ride `EVAL_` aliases: harness-safe key delivery into loop turns

*Realizes design Decision 66 (env alias precedence), slice R-1JMV-JW9B, and Decision 68 (alias injection), slice R-1KUR-XO00.*

`cmd/eval-extract`'s every provider-key lookup (chat and embedding) prefers an `EVAL_`-prefixed alias (`EVAL_OPENAI_API_KEY` over `OPENAI_API_KEY`, and likewise for the other providers), and a missing key names both accepted variables. `cmd/autotune` injects the aliases into the ralph child environment — `EVAL_OPENAI_API_KEY` always (the embedding key), plus the chat provider's alias when the run's spec is `auth=key` — sourced from its own environment (canonical or alias), never adding the canonical names to the child, and exiting non-zero before any child when the embedding key is unavailable. This is what lets a codex-harness improver keep subscription billing (no `OPENAI_API_KEY` in its environment) while the scorer inside its turns still embeds.

**Done when:**
- R-1JMV-JW9B — alias wins over canonical, works with canonical unset, absence names both — covered by a tagged test.
- R-1KUR-XO00 — recorded child env carries the right aliases and no driver-added canonical names; `auth=sub` adds no chat alias; missing embedding key fails at startup by name — covered by a tagged test.
- The suite is green per design Conventions.
