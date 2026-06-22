# Phase 34 — Apply the extract/compile output budgets D18 specified (4096 → 16384)

*Realizes design Decision 18 part (d) (the per-stage output-token budgets) and
its verification R-MW86-M158. Depends on Phase 28 (D18 a/b/c: `CallSite.MaxTokens`
applied to `GenSettings`, `sendText` truncation detection, non-retriable
`ErrTruncated`).*

Phase 28 landed D18's truncation **detection** (a/b/c) but shipped the stage
**budgets** (d) at the 4096 adapter default instead of the design's stated
headroom. D18(d) calls for extract and compile each sized "generously above
realistic output, e.g. **16384**"; the code instead carries
`const defaultMaxTokens = 4096` in both stages. The result: a real ingest
(`AGPOZUX2M4ARJKKMENQ7PITPDU`, a Gary Gygax biography section) fails at the
extract stage with the now-legible `response truncated: generated output usage
4096 reached max_tokens 4096` — detection works, but the budget that was supposed
to clear it was never applied. The gap survived "verified green" because the
Phase 28 R-MW86-M158 test asserts only `MaxTokens > 0` (non-zero), which `4096`
satisfies. The companion D18.md/phase-28.md docs were edited to say 16384 in
commit `954cddd`, but that commit touched **no `.go` file** — doc and code
diverged silently.

This phase makes the code match the design and closes the test gap so it cannot
regress to a no-op again.

- **`extract.DefaultCallSite` budget → 16384.** Raise `defaultMaxTokens` in
  `internal/extract/extract.go` from `4096` to `16384` (D18(d): "generously
  above the largest realistic per-document extraction"; the truncating sources
  are ~4.5–5K output tokens).
- **`compile.DefaultCallSite` budget → 16384.** Raise `defaultMaxTokens` in
  `internal/compile/compile.go` from `4096` to `16384` (D18(d): above the
  12,000-char page cap plus structural overhead).
- **Tighten the verification so non-zero is no longer enough.** The
  R-MW86-M158 assertions (`internal/compile/compile_test.go`) change from
  `MaxTokens <= 0` ("want non-zero") to asserting each stage default carries the
  designed budget — i.e. a budget **comfortably above the 4096 adapter default**
  (assert `>= 16384`), so a silent fall-back to the adapter ceiling fails the
  suite. The "configured ceiling reaches the provider" assertions
  (`req.Gen.MaxTokens == site.MaxTokens`) stay as-is — they already prove
  faithful application, they were just proving it about the wrong number.
- **No design change, no new wiring, no migration.** The composition root
  (`cmd/wiki/main.go`) already builds the extractor/compiler from these
  `DefaultCallSite`s (Phases 14/15), so raising the constant is the whole
  behavioural change. D18(d) is unchanged — it already specifies 16384; this
  phase only realizes it. The deferred follow-on (chunking oversized sources so
  no fixed ceiling can truncate) remains deferred: 16384 is headroom, not a
  guarantee, and a pathologically large source still fails **honestly** via the
  Phase 28 path.

**Done when:** R-MW86-M158 is re-covered by its now-tightened test —
`extract.DefaultCallSite` and `compile.DefaultCallSite` each carry a
`MaxTokens >= 16384` and the extractor/compiler the composition root builds run
at that budget (the request-reaches-provider assertions still hold) — so the
production stages can no longer silently run at the 4096 adapter default; the
ask call sites (D18(d), already 16384 inline) are unaffected; and the full suite
is green per design's *Conventions*.
