# Handoff — idgen lint-rule follow-through

Session context for continuing work in the `spec` worktree
(`/mnt/projects/ikigenba/spec`, branch `spec` of github.com/ikigenba/ikigenba).
Delete this file when it stops being useful.

## Where things stand

The `idgen/` sub-project proved out the spec system end to end:

1. `$open-spec` wrote six designs (38 requirements) reproducing the installed
   `idgen` binary; `$seal-spec` sealed them (seal now ends with a commit — a
   skill change made this session); ralph ran all 8 phases green.
2. The generated code passes every gate: gofmt check, `go build ./...`,
   `go test -race ./...`, `golangci-lint run` (strict set), `llm-lint cmd
   internal`. Binary verified byte-identical to the installed idgen on help,
   version, mint, decode.
3. Four Opus subagents then audited the generated code for judgment-level
   smells (things golangci-lint cannot see). Every verified finding became a
   project llm-lint rule: **36 rules in `idgen/lint-rules/`**, wired by
   `idgen/.llm-lint.json` (`"rules": ["lint-rules"]`, ancestor-walk
   discovery). All parse — `llm-lint --list-rules` from `idgen/` shows 38
   enabled (36 local + 2 builtin).
4. **All 36 rules are `severity: warning`** on purpose: they were harvested
   from this code, so they fire on it; warnings print but exit 0 (error
   severity exits 1), so the gate keeps passing. Promotion policy is in
   `idgen/AGENTS.md`: fix a smell → promote its rule to `error`.

## Next session's task: work through the rules

Not yet done — no real `llm-lint cmd internal` run has been made since the
rules landed (only `--list-rules` parse validation). The plan:

1. Run `llm-lint cmd internal` from `idgen/` and see which rules actually
   fire, where, and whether any false-positive.
2. Work through findings: fix real smells in the code. NOTE the project rule:
   contract-level changes go through the spec (`$open-spec` →
   `$seal-spec` → loop), not direct edits; below-contract refactors are
   fair game for direct fixes — judge each finding accordingly.
3. As each smell is fixed, promote its rule to `severity: error`.
4. Rules that prove noisy: tighten their do-not-flag guidance or drop them.
5. Tier-1 rules that behave well are candidates for promotion to og-llm-lint
   builtins (that repo is spec-driven too — changes go through its
   `project/` spec, not direct edits).

## Known spec-level issues (from the audit; see docs/llm-lint-rule-candidates.md, last section)

- **D2 gap**: nothing guards `multiplier·multiplierInverse ≡ 1 (mod 36⁸)` —
  the init coprimality check (R-SSYW-CRAP) can't catch a fat-fingered
  inverse constant. Wants a new requirement.
- **D2 gap**: upper-range behavior undesigned — body wraps at 36⁸ ms
  (~89.4y, ids silently lie past ~2115), `t.Sub` saturates ±292y.
- **Behavior bug, no covering requirement**: unknown flag prints the usage
  block twice (D4's exactly-once req covers only --help).
- **$audit-spec candidates** — tagged tests that don't genuinely prove their
  ids: R-TPW6-OKBG (vacuous Contains), R-TNGD-X0U2 (TZ placebo),
  R-SRQZ-YZK0 (sweep never reaches deep code), R-SJ7P-ALD5 (domain bounded
  by impl constant), R-T1I7-15HK (call-ordinal fake), R-T55W-6GPN
  (Contains admits the double-print).

## Facts worth knowing

- llm-lint rule file format: strict frontmatter (`description`, `severity:
  error|warning`, optional `include`/`exclude` as JSON arrays; id =
  filename stem `[a-z0-9][a-z0-9-]*`); body = LLM prompt with
  Flagged/Spared examples. Omitted `include` → default all-code globs.
- llm-lint exit codes: 0 clean or warnings-only, 1 error findings, 2
  config/usage, 3 operational. Needs provider API key in env (defaults:
  google/gemini-3.7-flash).
- idgen's output shape collides with requirement-id grep (`R-XXXX-XXXX`):
  design prose and test files must never contain id-shaped literals that
  aren't real requirement ids (rule recorded in D2 + idgen/AGENTS.md).
- Rollback point for a clean re-test of the whole loop: commit `607e6f3`
  (sealed spec, no generated code).
- `idgen/specs/loops/run` is an untracked 2-line ralph wrapper the operator
  created; leave it or ask.
- Releases: tag `idgen/vX.Y.Z` on main → `.github/workflows/release-idgen.yml`
  (checks tag == source version in `internal/cli/version.go`, goreleaser
  builds, `gh release create` publishes). Untested until the first real tag.
- Version is source-carried (`v0.1.0`), never ldflags-injected — D6.

## History of this session's commits

`98038e9` seal-spec commits at end · `4a367bf` idgen spec sealed ·
`607e6f3` gitignore fix (rollback point) · 8 ralph phase commits ·
`1934ff9` README · `b083390` installer · `f7b856e` README order ·
`5f7a5ff` gofmt gate · `b8fa9f4` verify cleans stale feedback ·
`86cb867` the 36 lint rules + provenance doc.
