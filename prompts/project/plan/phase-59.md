# Phase 59 — Env-channel conformance: composed inventory root, manifest-surfaced knobs, Spec-derived drift oracle

*Realizes design Decision 49 (env-channel conformance).*

What gets built: the run-spawn inventory root at `cmd/prompts/main.go` resolves
by the override → `IKIGENBA_ROOT` → `.` ladder (the `"/opt"` literal goes);
`Spec.ManifestExtras` gains `PROMPTS_RUN_TTL=30m` and the committed
`prompts/etc/manifest.env` gains the three missing knob lines
(`PROMPTS_MAX_INFLIGHT_CALLS=8`, `PROMPTS_MAX_CONCURRENT_RUNS=8`,
`PROMPTS_RUN_TTL=30m`) in Spec order; the composition root exposes a
`promptsSpec()` builder and the existing R-8IAN-FB87 drift test emits from the
builder's fields instead of a hand-built `manifest.Fields` literal (closing the
already-live Spec/manifest divergence); and a source-scan test guards prompts'
non-test Go source against `"/opt` string literals. End state: no box-path
literal in prompts production code, and the committed manifest is the complete
operator surface for prompts' universal knobs.

**Done when:**
- R-M51H-QWOL — resolver ladder test passes: explicit `PROMPTS_MANIFEST_ROOT`
  wins; else `IKIGENBA_ROOT`; else `.` — never `/opt`.
- R-M69E-4OFA — resolved no-env knob defaults equal the committed
  `prompts/etc/manifest.env` values.
- R-VKB6-SHHV — the source-scan test walks prompts non-test `.go` files and
  finds no `"/opt` string literal.
- The R-8IAN-FB87-tagged test emits from `promptsSpec()`'s fields (no
  hand-built `manifest.Fields` literal remains in it) and passes against the
  updated committed manifest.
- All three newly-assigned ids appear verbatim as tags in test files under
  `prompts/`, and the suite is green per design Conventions
  (`cd prompts && go test ./...`, plus build/vet).
