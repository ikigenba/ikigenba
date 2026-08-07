# Phase 18 — Clear one unfilled placeholder and twelve stale requirement-id tags

*Realizes design Decision — (no Verification ids; tag hygiene only).*

A tag-hygiene pass over `internal/opsctl/*_test.go`. Nothing about opsctl's
behavior changes: **no test function is added, deleted, renamed, or has any
assertion altered.** The only edits are the removal of thirteen `//` comment
lines carrying requirement ids that name nothing in this tree's current design.
Each was traced to its minting or retirement commit before being listed here.

## The unfilled placeholder

`internal/opsctl/backup_test.go`, inside `TestRestoreRecreatesCacheAsEmptyDirectory`:
a literal `// R-XXXX-XXXX` line sits directly below the legitimate
`// R-WP3M-PO1V` tag. It was added as the sole one-line change of commit
`6ee56188` ("opsctl phase 01: tag restore cache verify gap") — the build loop
emitted the template placeholder from the phase document instead of a minted id.
It marks no behavior of its own: every assertion in that test (the cache
directory exists, is a directory, and is empty after a restore that seeded it
with stale content) is exactly the falsifiable content of D1's `R-WP3M-PO1V`,
and D1's sibling behavior — the recreated `cache/` being chowned to
`<app>:<app>` rather than root — already has its own id and its own test
(`TestRestoreChownsCacheToServiceUser`). No id is minted for it; the line is
deleted.

## The retired ids

Two tags name ids this project's design retired. In both cases the successor id
minted in the same commit already tags the same test, so only the stale line
goes and the test keeps its correct tag:

- `internal/opsctl/setup_test.go` — `// R-AT2M-15HI`, sitting above
  `// R-QFXB-VARQ`. Commit `7411ac79` rewrote D7 in place to retire the
  `working/` segment from the served-tree model, retiring `R-AT2M-15HI` and
  minting `R-QFXB-VARQ` for the no-`working` structure. Tag line only.
- `internal/opsctl/initbox_test.go` — `// R-WHC0-I9HL` in
  `TestInitBoxInstallsBaselineCommandLineToolsOnDefaultAndSkipCertPaths`,
  sitting above `// R-JQGB-RYA2`. Commit `415b9fa1` rewrote D10 in place to
  widen the box baseline past the three-package set, retiring `R-WHC0-I9HL` and
  minting `R-JQGB-RYA2`. Tag line only.

## The foreign per-service contract id

Ten tags in this tree carry the umbrella's per-service boot/health contract id
`R-4LKF-FB23` (repo-root `project/design/D08.md`, marked `[proof: per-service]`).
Per that marker, and per the *Conventions* entry "Suite-contract proofs carried
here" in `project/design/README.md`, a per-service id is proven in the adopting
**service's** own tree; opsctl is tooling and is never such an adopter. Every one
of the eight services involved already carries its own tagged proof for that id
in its own `cmd/<svc>/main_test.go`, so removing these tags removes no proof of
the contract anywhere. All ten tag lines are deleted; **all ten tests stay
exactly as they are**, including the eight that really do boot a built service
binary and poll its health endpoint — they remain valuable opsctl layout tests,
they simply are not the contract's designated proof site:

- `internal/opsctl/deploy_test.go` — seven tags, one each above
  `TestNotifySetupDeployBootsHealthWithStateAndCachePaths`,
  `TestDropboxSetupDeployBootsHealthWithStateCacheAndMirrorPaths`,
  `TestPromptsSetupDeployBootsHealthWithStateSandboxesAndCacheRuns`,
  `TestWikiSetupDeployBootsHealthWithStateAndCachePaths`,
  `TestCronSetupDeployBootsHealthWithStateAndCachePaths`,
  `TestGmailSetupDeployBootsHealthWithStateAndCachePaths`, and
  `TestSitesSetupDeployBootsHealthWithStateWWWPaths`.
- `internal/opsctl/setup_test.go` — one tag above
  `TestWebhooksSetupDeployBootsHealthWithStateCacheAndLibexecPaths`.
- `internal/opsctl/provision_test.go` — two tags, inside `TestSetup_WWWTree` and
  inside `TestWWWDirsFor_OnlySites`. These two are drive-by mistags on top of
  everything above: neither test boots anything or reaches a health endpoint
  (`TestWWWDirsFor_OnlySites` is a pure path-derivation assertion), so neither
  could ever have proven the contract.

## Done when

All checks run from `opsctl/`. The `--exclude-dir=project` on every grep is
load-bearing: this phase file and `project/design/README.md` mention these ids
and paths in prose, and a spec mention is not a tag.

- No unfilled placeholder remains in the code:
  `grep -rn 'R-XXXX-XXXX' --include='*.go' --exclude-dir=project .` prints
  nothing (exit 1).
- No retired or foreign id remains as a tag:
  `grep -rnE 'R-AT2M-15HI|R-WHC0-I9HL|R-4LKF-FB23' --include='*.go' --exclude-dir=project .`
  prints nothing (exit 1).
- The successor tags survive, one apiece:
  `grep -rc 'R-QFXB-VARQ' --include='*.go' --exclude-dir=project .` and
  `grep -rc 'R-JQGB-RYA2' --include='*.go' --exclude-dir=project .` each total
  exactly 1, and `grep -rc 'R-WP3M-PO1V' --include='*.go' --exclude-dir=project .`
  totals exactly 1.
- No test was lost with a tag — all thirteen host functions still exist:
  `grep -rhoE 'func (TestRestoreRecreatesCacheAsEmptyDirectory|TestInitBoxInstallsBaselineCommandLineToolsOnDefaultAndSkipCertPaths|TestSetup_WWWTree|TestWWWDirsFor_OnlySites|TestNotifySetupDeployBootsHealthWithStateAndCachePaths|TestDropboxSetupDeployBootsHealthWithStateCacheAndMirrorPaths|TestPromptsSetupDeployBootsHealthWithStateSandboxesAndCacheRuns|TestWikiSetupDeployBootsHealthWithStateAndCachePaths|TestCronSetupDeployBootsHealthWithStateAndCachePaths|TestGmailSetupDeployBootsHealthWithStateAndCachePaths|TestSitesSetupDeployBootsHealthWithStateWWWPaths|TestWebhooksSetupDeployBootsHealthWithStateCacheAndLibexecPaths|TestRestoreChownsCacheToServiceUser)\(' --include='*_test.go' . | sort -u | wc -l`
  prints `13`.
- The tree-local coverage difference is unchanged — exactly the eight
  real-substrate ids tracked in `project/opsctl-verification.md` and nothing
  else. Running the coverage `comm` from `project/plan/README.md`:

  ```sh
  comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
  ```

  prints exactly **eight** lines (`| wc -l` = 8), and piping that output through
  `comm -3 - <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/opsctl-verification.md | sort -u)`
  yields no line in the left column — i.e. every remaining uncovered id is one
  of the operator-run real-substrate ids that document already tracks. (Those
  ids are deliberately **not** written out here: this file is one of the
  `comm`'s covering inputs, so naming them would make the check pass
  vacuously.)
- The suite is green per design's *Conventions*: `GOWORK=off go build ./...`
  succeeds and `GOWORK=off go test ./...` passes with no failures.
