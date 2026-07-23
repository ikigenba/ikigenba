# Phase 119 — Restore D19 requirement-id tag coverage

*Realizes design Decision 19 (per-call-site configuration) — the R-GGIG-AN7W slice, plus re-anchoring three drifted sibling tags. No pending-phase dependencies.*

The behaviors D19 mints are all still asserted by the suite, but the id tags drifted during an earlier rebuild: `R-GGIG-AN7W` no longer appears in any test, and three sibling ids tag tests other than the one asserting their behavior. Re-anchor each id as a comment tag inside the test that asserts exactly its designed behavior:

- `R-GGIG-AN7W` → the test setting `EXTRACT_MODEL` and `COMPILE_MODEL` to different values and asserting each resolved site carries its own model (`internal/wiki/config_test.go`, the per-call-site overrides test).
- `R-GHQC-OEYL` → that same overrides test's per-site non-model-knob assertions (a reasoning/temperature/max-tokens override reaching only its own site), not the ask defaults test in `internal/ask/ask_test.go`.
- `R-GK65-FYFZ` → the malformed-environment test asserting startup errors for bad `_REASONING`/`_TEMPERATURE`/`_MAX_TOKENS` values (`internal/wiki/config_test.go`), not the overrides test.
- `R-GLE1-TQ6O` → the test asserting the distinct `ask-subject`/`ask-synthesis` stage labels on the ask default call sites (`internal/ask/ask_test.go`), not the malformed-environment test.

Tag moves only; test logic changes only if a behavior turns out genuinely unasserted, in which case the assertion is added to the correct test.

**Done when:**
- Each of `R-GGIG-AN7W`, `R-GHQC-OEYL`, `R-GK65-FYFZ`, `R-GLE1-TQ6O` appears exactly once across `*_test.go` files (excluding `project/`), in the test asserting its designed behavior.
- The design coverage check from `project/plan/README.md` (the `comm -23` design-only difference, ignoring the literal placeholder `R-XXXX-XXXX`) returns empty.
- The suite is green (design Conventions).
