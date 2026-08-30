# llm-lint rule candidates

Harvested by four Opus subagents auditing `idgen/` (which already passes
gofmt, go vet, and golangci-lint with the strict set — everything below is
judgment-level, llm-lint's niche). Themes: error handling, API/package
design, idiom/maintainability, test quality. Convergence = number of agents
that found the same smell independently. Every Tier 1/2 rule fired on real
code; agents verified findings empirically (probe tests, coverage runs,
arithmetic checks).

## Tier 1 — multi-agent convergence, fired on real code

- `unverified-derived-constant` (×3): a constant that must satisfy an
  algebraic relation to its siblings (here `multiplierInverse`) is asserted
  nowhere; the init guard checks only gcd coprimality, which cannot break by
  typo, while the constant that silently corrupts every decode is unguarded.
- `duplicated-validation-across-layers` (×3): the same input grammar
  (`^[A-Za-z0-9]+$`) implemented independently in producer and consumer
  packages under the same name (`validPrefix` — a regexp var in `cli`, a
  hand-rolled func in `idgen`), free to drift.
- `zero-value-as-absent-sentinel` (×3): `previousMillisecond = 0` is a legal
  instant; correctness propped up by a `minted > 0` loop-counter guard —
  two mechanisms encoding one condition. `math.MinInt64` deletes both.
- `undocumented-silent-wraparound-at-range-ceiling` (×3): doc comment states
  the lower clamp only; body wraps every 36⁸ ms (~89.4 y) minting ids that
  decode to wrong times, and `t.Sub()` saturation makes year-2400 and
  year-3000 mint identical ids.
- `discarded-output-write-error` (×2): twelve `_, _ =` writes; `idgen -n
  100000 | head -5` gets EPIPE on every write yet exits 0. `_, _ =`
  satisfies errcheck, which is why this needs a judgment rule.
- `unbounded-retry-on-injected-dependency` (×2): the mint wait loop spins
  forever if the injected Clock never advances; the liveness requirement on
  `Sleep`/`Now` is documented nowhere on the interface, and the suite's own
  `fakeClock` violates it (any `-n 2` test using it would hang).
- `io-error-mapped-to-data-error` (×2): `scanner.Err()` folded into the
  malformed-id bool, so infrastructure failure exits with the code the
  contract reserves for bad input, and truncated output is undetectable.
- `exported-mutable-package-var` (×2): `idgen.Epoch` is an exported var any
  importer can reassign — the one value that would invalidate every id in
  circulation, wearing a constant's clothes.
- `asymmetric-mode-dispatch` / `god-entrypoint` (×2): decode extracted to
  `runDecode`, mint left inline in a 59-line `Run` doing parse + validate +
  loop; the two modes read at different altitudes.
- `repeated-flag-alias-literals` + `dead-flag-metadata` (×2): each
  short/long pair repeats default and usage string; all five description
  strings are unreachable because `Usage` is overridden.
- `unreachable-defensive-guard` (×2): `!ok || n >= modulus` is provably dead
  (prior validation restricts the alphabet; 8 base-36 digits max out one
  below modulus), reads as a real guard, and its error message lies.
- `unquoted-untrusted-token-in-diagnostic` (×2): one diagnostic quotes with
  `strconv.Quote`, its sibling interpolates the token raw — a newline in an
  id forges extra stderr lines, breaking line-counting tests.
- `expected-value-from-code-under-test` (×2, tests): `want :=
  idgen.MintAt(...)` in CLI tests reduces to `MintAt(x) == MintAt(x)`; the
  suite contains the wrong (`version+"\n"`) and right (`"v0.1.0\n"`) version
  of the same assertion three lines apart.
- `test-only-parameterization` (×2): `validateAffineMap(m, mod)` is
  parameterized solely so a test can pass `(6, 9)`; production always feeds
  the two constants, and the test proves the helper panics, not the package.

## Tier 2 — single agent, fired, high value

Error handling:
- `double-reported-error-diagnostic`: unknown flag prints the usage block
  twice — `flag.ContinueOnError` already invokes `Usage` before returning.
- `error-context-restated-by-caller`: `idgen: invalid id broken: invalid
  id: non-canonical format` — caller restates the sentinel's own text.
- `unvalidated-precondition-on-exported-api`: `MintAt` trusts a prefix rule
  it neither documents nor checks; `MintAt("a-b", t)` yields an id its own
  `TimeOf` rejects.
- `closure-captured-error-flag`: decode failure accumulated by mutating a
  captured bool from two call sites instead of returning it.
- `untyped-code-enum`: exit codes as bare ints, interchangeable with counts.

Clarity:
- `unexplained-modular-arithmetic`: `multiplyMod` exists only because
  `ms*multiplier` overflows int64; the `+ modulus` before `%` guards Go's
  signed modulo — none of it stated, all of it invitingly "simplifiable".
- `derived-constant-without-provenance` / `hand-synced-derived-constant`:
  `modulus = 2821109907456 // 36^8` documents its derivation in a comment
  instead of computing/asserting it; inverse constant carries nothing.
- `magic-literal-beside-existing-constant`: the 4-4 split as bare `4`s in
  five places across three files while `bodyDigits` is used once.
- `literal-zone-suffix-without-conversion-guarantee`: layout hardcodes `Z`;
  correct only because `.UTC()` sits in the same expression.
- `same-name-different-kind`: `validPrefix` is a var in one package and a
  func in another for the same concept.

Test quality:
- `vacuous-substring-assertion`: usage-mentions-every-option test checks
  `Contains "-n"` — a substring of `--number`; only the `-V` check is real.
  (Verified: deleting all short spellings still passes 4 of 5.)
- `ineffective-env-mutation-in-test`: `t.Setenv("TZ", ...)` cannot affect
  `time.Local` (resolved once per process); the TZ test is a placebo.
- `self-consistency-only-assertion`: prefix-agnostic decode test asserts
  decodes equal each other, never the independently-known instant.
- `unreachable-property-domain`: 20k random strings ~never form the
  `prefix-4-4` shape; the "adversarial sweep" bails at the first guard
  (coverage-confirmed).
- `implementation-derived-test-domain`: round-trip sweep bounds inputs with
  `% modulus` — the implementation's own constant — so it can never cross
  the wrap it should catch; no test at min/max/max+1/epoch−1ms.
- `call-count-scripted-fake`: backward-clock fake dips on `nowCalls == 2`,
  encoding the subject's internal call sequence; a refactor silently
  relocates the dip.
- `report-generated-counterexample` / `log-prng-seed-on-failure`: sweep
  failures print an iteration index, not the input or seed.
- `prefer-exact-over-inequality-assertion`: `totalSleep() >= 4ms` under a
  deterministic fake that makes the exact value predictable.
- `overclaiming-exhaustive-test-name`: "every non-canonical boundary" table
  that cannot cover a documented error branch.
- `unused-fake-or-table-field`: fake's `reported` field written, never read.
- `helper-asserts-and-returns`: mint helper fatals on stdout shape before
  returning the exit code its callers came to assert.
- `duplicate-test-doubles` / `multiple-invocation-helpers-one-subject` /
  `duplicated-inline-prng`: three fakes for one interface, a dozen
  inconsistent invocation shapes, the same PRNG pasted twice.

## Tier 3 — [no-finding] here, but strong generic rules

`panic-in-library-path`, `os-exit-skips-defers`,
`error-string-matching-for-control-flow`, `owned-resource-unclosed`,
`unowned-stream-closed`, `unchecked-duration-multiply`,
`producer-side-interface`, `single-implementation-interface`,
`time-equality-operator`, `monotonic-time-persisted`,
`implicit-local-timezone`, `init-side-effects`, `silent-input-clamping`,
`getter-prefix`, `package-name-stutter`, `no-commented-out-code`,
`narrates-next-line`, `no-else-after-return`, `confusing-err-shadowing`,
`no-time-now-in-tests`, `require-stderr-and-exit-assertions-in-cli-tests`,
`assert-no-extra-output`, `require-t-helper-in-assertion-helpers`,
`require-t-parallel`, `bigint-where-fixed-width-suffices`.

## Spec-system follow-ups (not lint rules)

1. **Design gap — D2**: R-SSYW-CRAP guards multiplier/modulus coprimality (a
   compile-time fact that cannot break by typo) but nothing guards
   `multiplier·inverse ≡ 1 (mod 36⁸)` — the one-line check that catches the
   fat-fingered constant that would decode every id to garbage. Candidate
   new requirement.
2. **Design gap — D2**: upper-range behavior (36⁸ wrap, Duration
   saturation) is undesigned; ids can silently lie past 2115. Decide:
   error, clamp, or document.
3. **Behavior bug uncovered by any requirement**: unknown flag prints the
   usage block twice (D4's exactly-once requirement covers only --help).
4. **$audit-spec candidates** — tagged tests that don't genuinely prove
   their ids: R-TPW6-OKBG (vacuous Contains), R-TNGD-X0U2 (TZ placebo),
   R-SRQZ-YZK0 (unreachable domain), R-SJ7P-ALD5 (impl-derived domain),
   R-T1I7-15HK (call-ordinal fake), R-T55W-6GPN (Contains admits the
   double-print).
