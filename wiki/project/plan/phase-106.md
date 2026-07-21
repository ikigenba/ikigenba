# Phase 106 — Free-form step config: the `-c` spec is the whole call, absent knobs are never sent

*Realizes design Decision 68 (free-form config resolution), slice R-XGGI-JK9U, and Decision 66 (optional generation knobs), slice R-XHOE-XC0J.*

The workbench stops inheriting production pins. `internal/eval`'s config loader makes the `eval` block's `temperature`, `thinking`, `max_tokens`, and `max_parse_retries` optional (pointer/absent-aware; retries default 2), and `cmd/eval-extract` omits any absent knob from the agentkit call entirely — a config stating only `provider` and `model` produces calls carrying no temperature, no reasoning setting, no max_tokens. `cmd/autotune`'s config resolution is rewritten: the `eval` block of the workspace config is composed verbatim from the operator's `-c` set (`provider`/`model` required by name, `auth` defaulting to `key`, nothing else added), with `embedding`/`weights` still copied verbatim from the committed file and `embedding.*`/`weights.*`/unknown keys still rejected by name. The retired pins-plus-overrides behavior's id `R-F2EG-TLJL` is deleted from design; its tagged test is removed (its still-valid assertions — embedding/weights copying, rejection-by-name — fold into the R-XGGI-JK9U test). The committed `eval/extract/config.json` is unchanged.

**Done when:**
- R-XGGI-JK9U — verbatim `-c`→`eval` composition, required/defaulted/rejected keys, embedding/weights byte-copy — covered by a tagged test.
- R-XHOE-XC0J — present knobs sent with their values, absent knobs omitted from calls (capturing fake provider), retries default 2, loader accepts a provider+model-only `eval` block — covered by a tagged test.
- `grep -rn "R-F2EG-TLJL" --include='*_test.go' .` from `wiki/` returns nothing (the retired id's tag is gone).
- The suite is green per design Conventions.
