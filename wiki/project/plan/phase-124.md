# Phase 124 — Prove ask-path origin attribution, and clear eight stale requirement-id tags

*Realizes design Decision 62 (slice: `R-16VU-W6UV`).*

D62's ask half is the one current wiki Verification id with no tagged test.
`R-183R-9YLK` (the job-driven half) is proven by
`TestWorkerThreadsStoredChainThroughExtractCompileAndEmbed` in
`internal/wiki/ingest_pipeline_test.go`, but nothing pins the claim that an ask
driven by a named user attributes **every** prompts call to that user. The
behavior is in fact implemented and incidentally exercised, which is exactly why
it needs its own test: a regression that dropped origin on one of the three call
sites would leave the suite green today.

**The seam.** `internal/ask`'s `func (a *Asker) Ask(ctx context.Context, owner,
question string) (Answer, error)` (`internal/ask/ask.go`) is where the email
becomes an origin — it is the single site that builds
`llm.Attribution{Origin: …}` from `owner`, mapping a non-empty owner to
`"user:" + owner` and an empty one to `"service:wiki"`. Both callers
(`internal/mcp`'s `handleAskCall`, which passes the chassis `Identity`'s
`OwnerEmail`, and `internal/web`'s `?q=` handler, which passes the request's
owner) hand it the raw email and nothing else, so driving `Asker.Ask` directly
exercises the whole origin rule without dragging in chassis identity or session
plumbing. The `Attribution` value is destructured onto the wire as the `origin`
field of both `completeRequest` (`internal/llm/llm.go`) and `embedRequest`
(`internal/llm/embed.go`).

**The substrate must be a real loopback peer.** `internal/llmtest` decodes only
`model`/`system`/`config`/`messages` from a `/complete` body — it discards
`origin` — so it cannot falsify this claim. Stand up a raw
`httptest.NewServer` that decodes every request body and records path plus
`origin` and `name`, exactly as `TestAskThreadsReceivedChainThroughEveryPromptsCall`
(`internal/ask/ask_test.go`) already does; that file's `promptCall` struct,
`migratedDB`, and `savePage` helpers are the fixtures to reuse, and the new test
belongs beside it in the same file.

**One ask makes three calls**, and the "all" in the id is the load-bearing word:

- `POST /complete`, `name` `wiki.ask-subject` — the analysis call. Always fires.
- `POST /embed`, `name` `wiki.embed-query` — one per non-empty sub-query the
  analysis returns.
- `POST /complete`, `name` `wiki.ask-synthesis` — **conditional**: skipped on
  the honest-empty path when the retrieval floor is not cleared or no pages are
  gathered.

So the fixture must seed a page and a vector-cache entry such that the floor is
cleared and the synthesis call genuinely fires. Construct the `Asker` with
`ask.DefaultSubjectCallSite()` and `ask.DefaultSynthesisCallSite()` rather than
the file's local `testExtractSite()`/`testSynthSite()` helpers — the latter leave
`Stage` empty, so both `/complete` bodies come back named `"wiki."` and a name
assertion against them would be meaningless.

End state — `internal/ask/ask_test.go` additionally holds one clearly-named test
tagged `// R-16VU-W6UV`, driving `asker.Ask(ctx, "alice@example.com", …)`, which:

- asserts the captured `(path, name)` multiset equals exactly
  `{("/complete","wiki.ask-subject"), ("/embed","wiki.embed-query"),
  ("/complete","wiki.ask-synthesis")}`, so a silently-skipped synthesis call
  cannot make the test pass vacuously;
- iterates **every** captured call — not `calls[0]`, and not a subset — asserting
  `origin == "user:alice@example.com"` on each, naming the offending path and
  index on failure;
- additionally drives one ask with an empty owner and asserts every captured
  call carries `origin` `service:wiki`, which is the discriminating half: a
  degenerate implementation that hardcoded `"user:" + owner` unconditionally
  would emit the bare `user:` and fail.

Leave `TestAskThreadsReceivedChainThroughEveryPromptsCall` exactly as it is — it
is D64's chain claim under `R-XNXS-O9AJ`, and it must not be folded into or
replaced by the new test. Sharing an unexported helper is fine; sharing a tag is
not.

## Eight stale tags to clear

Each of the following ids appears as a tag in a test file but belongs to no
current Decision. Every one has been traced to its minting and its retirement;
the instruction per id is exact and must be followed literally. **Nothing here
is a new behavior** — no id is added to design by this phase.

**Delete only the tag comment line; keep the test and its other tags:**

- `// R-1AJK-1I2Y` at **two** sites — `internal/wiki/ingest_pipeline_test.go`
  (on `TestWorkerThreadsStoredChainThroughExtractCompileAndEmbed`) and
  `internal/wiki/merge_worker_test.go` (on
  `TestMergeWorkerReembedsWinnerAfterCommitAndEvictsLoserVector`). It was D62's
  old `group_id = job.ID` rule, retired when D62 was re-scoped to origin alone
  and D64 took correlation over — D64 explicitly rejects the rule the id stated.
  The current claims are `R-XP5P-2118` and `R-XJ27-56BR`, already tagging the
  work. The merge-worker test in fact now asserts the *opposite* of the retired
  rule, so the tag is actively misleading.
- `// R-1CZC-T1KC` in `internal/mcp/mcp_test.go` (on
  `TestToolsListAdvertisesConfiguredWikiSurface`). It was D63's "no `llm_calls`
  tool advertised" claim; D57's `R-YF06-03HO`, which already tags the same test,
  absorbs it — its exact-membership assertion precludes the retired tool by
  construction.
- `// R-3BBH-H35U` in `internal/mcp/mcp_test.go` (on
  `TestPaginatedListToolsForwardFiltersAndReturnNextCursors`). It was the
  retired `llm_calls` pagination verb. The test body contains no `llm_calls`
  reference at all — it exercises `jobs`/`subjects`/`claims` paging, covered by
  its sibling D16/D57 tags.
- `// R-ZAAY-UUHZ` in `internal/wiki/config_test.go` (on
  `TestNewConfigLayersEmbeddingEnvironmentOverrides`). It was a D34 migration
  widening an `llm_calls` CHECK constraint, retired with the whole wiki-side
  call log. The test is a pure config-override test fully minted by its sibling
  `R-Z932-H2RA`; the tag was mis-attached from the start.
- `// R-4LKF-FB23` in `internal/wiki/wiki_test.go` (on
  `TestLoadVectorCacheEntriesLoadsStoredPageEmbeddings`). This is a **duplicate**
  of a live id, not an orphan: the tag was added as a one-line drive-by by an
  unrelated opsctl phase onto a vector-cache test that has nothing to do with
  booting an install tree. The genuine proof is
  `TestWikiBootsFromOpsctlLayoutAndServesHealth` in `cmd/wiki/main_test.go`,
  which keeps its tag. One id, one behavior, one place — delete the stray.

**Delete the tag and the test function it tags:**

- `// R-BICU-BZG6` and `TestDefaultAnalysisInstructionsMatchPromptFile` in
  `internal/ask/analyze_test.go`. The id was D69's analysis-eval workbench
  requirement that a Go constant stay byte-identical to a separate
  `eval/analysis/prompt.txt`. D69 is retired and the prompt now lives at
  `internal/ask/analysis-prompt.txt` under `//go:embed`, so file/const identity
  is a compiler guarantee; D71 states the tune folder has no mechanical tie. The
  embedded-prompt contract is minted by D36's `R-A0XE-WA4H`, already tagging
  this same file.
- `// R-BJKQ-PR6V` and `TestAnalyzeUsesExportedNormalization` in
  `internal/ask/analyze_test.go`. Also D69: a workbench-faithfulness assertion
  that production `Analyze` used the exported envelope helpers. The workbench is
  gone, and the assertion is now self-referential — `Analyze` calls
  `NormalizeAnalysis`, so it would pass even if that function were a no-op. The
  real behaviors are D36's `R-QCF7-D0WJ` (cap at 4) and `R-QDN3-QSN8` (empty
  fallback).
- `// R-KY7O-D7JB` and `TestProductionEnvelopeUsesExportedRenderAndValidate` in
  `internal/extract/extract_test.go`. D66's equivalent faithfulness guarantee for
  the deleted `cmd/eval-extract`. Both halves are redundant against current ids:
  `Validate` type-rejection is `R-VYU0-BPAX` and the user-turn-only-rendered-input
  contract is `R-9W1T-D75P`, both tagging tests in this same file.

Removing the two functions from `internal/ask/analyze_test.go` orphans that
file's `os` and `reflect` imports — drop both from its import block or the
package will not compile. `strings` stays; it is still used. No import breakage
in `internal/extract/extract_test.go`.

**One id is deliberately left alone.** `R-1BRG-F9TN` tags
`TestMigrationsRetireLLMCallSchemaWithoutChangingHistory` in
`internal/db/db_test.go`. Unlike the eight above, the behavior it proves is
**current**: D63 still states in prose that the forward migration dropping the
retired call-log table stands and that committed migrations stay frozen, and no
current id mints it. That is a design omission, not a stale tag — **do not
delete the tag and do not delete the test**, and do not invent a Decision or an
id to cover it. It is reported to the operator separately.

No non-test source changes are expected anywhere in this phase. If the ask path
turns out not to attribute a call the way D62 requires, that is a spec-or-code
finding to report — do not weaken the assertion to make it pass.

**Done when:**

- `R-16VU-W6UV` — a clearly-named test in `internal/ask/ask_test.go` drives
  `Asker.Ask` with owner `alice@example.com` against an `httptest` prompts peer,
  asserts the exact three-call `(path, name)` multiset, and asserts
  `origin == "user:alice@example.com"` on **every** captured call, plus the
  empty-owner `service:wiki` counterpart; the id appears verbatim as a
  `// R-16VU-W6UV` tag on that test.
- **Perturb-and-see-it-fail.** Temporarily change `internal/ask/ask.go` so the
  attribution passed to the synthesis call alone is
  `llm.Attribution{Origin: "service:wiki"}` (leaving the analysis and embed
  calls correct) and confirm the new test **fails**; then revert the
  perturbation and confirm it passes. A test that survives that edit is only
  checking the first call and does not satisfy the id's "all".
- **Deleted tags are gone.** From `wiki/`, this prints nothing:

  ```
  grep -rn --include='*_test.go' --exclude-dir=project \
    -e R-1AJK-1I2Y -e R-1CZC-T1KC -e R-3BBH-H35U -e R-ZAAY-UUHZ \
    -e R-BICU-BZG6 -e R-BJKQ-PR6V -e R-KY7O-D7JB .
  ```

- **Deleted test functions are gone.** From `wiki/`, this prints nothing:

  ```
  grep -rn --include='*_test.go' --exclude-dir=project \
    -e 'func TestDefaultAnalysisInstructionsMatchPromptFile' \
    -e 'func TestAnalyzeUsesExportedNormalization' \
    -e 'func TestProductionEnvelopeUsesExportedRenderAndValidate' .
  ```

- **The live id keeps exactly one tag.** From `wiki/`,
  `grep -rc --include='*_test.go' --exclude-dir=project R-4LKF-FB23 . | grep -v ':0$'`
  names `./cmd/wiki/main_test.go` and nothing else, with count `1`.
- **The preserved id is untouched.** From `wiki/`,
  `grep -rn --include='*_test.go' --exclude-dir=project R-1BRG-F9TN .` still
  prints its one hit in `internal/db/db_test.go`.
- **Anti-vacuous count.** From `wiki/`, this prints `1` — the new id is tagged in
  real test code, not merely quoted in this phase file:

  ```
  grep -rhoE 'R-16VU-W6UV' --include='*_test.go' --exclude-dir=project . | wc -l
  ```

- **Coverage is total.** From `wiki/`, the design-only difference is empty:

  ```
  comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
  ```

- **The suite is green** per design's Conventions: `go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed from
  `wiki/` with zero failures.
