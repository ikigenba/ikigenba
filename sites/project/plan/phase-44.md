# Phase 44 — Clear fourteen stale requirement-id tags left behind by three design rewrites

*Realizes design Decision — (no new behavior; tag hygiene only).*

Fourteen `R-XXXX-XXXX` tags sit in sites's test files that belong to **no current
Decision**. Every one was minted by an earlier design generation and retired when
that Decision was rewritten in place; the rewrite took the id out of design but
left the comment in the code. The result is fourteen untethered tags that look
like coverage and are not.

**Nothing in this phase is a new behavior, and no test's assertions change.**
Each retired id's claim is proven today by a *current* id, and every affected
test stays exactly as it is — only the dead tag comment line is removed. Where
the current id happens to tag a different test than the retired one did, that is
noted per entry below, with the surviving proof named.

## The three rewrites, and what each retired

- **`3f0d424d`** — the unlisted-visibility rework (two-value `public` boolean →
  three-value `visibility` enum).
- **`835a3928`** — the landing-control wiring fix (D6/D22 rewrite, D23 added).
- **`956e1c00`** — the name/slug split (site display name separated from the URL
  slug).

## The fourteen tags

**Delete only the tag comment line. Keep the test function, its body, its
assertions, and every other tag on it.**

### Group A — the retired id and its successor sit on the *same* test

The rewrite renamed the id and updated the claim in place; the build tagged the
test with the new id and never removed the old one. Both comment lines are
adjacent today.

- `// R-HK3X-22SM` in `cmd/sites/landing_visibility_test.go` — the line
  immediately above `// R-ZI4K-DR4L`, D19's current row-render claim, on the same
  test. `956e1c00` rewrote the id when the row identity became the display name.
- `// R-HLBT-FUJB` in `cmd/sites/landing_visibility_test.go` — immediately above
  `// R-ZKKD-5ALZ`, D19's current data-island claim, same test. Same rewrite; the
  island gained `name` beside `slug`.
- `// R-HMJP-TMA0` in `cmd/sites/landing_visibility_test.go` — immediately above
  `// R-ZLS9-J2CO`, D19's current unlisted-anchor claim, same test. Same rewrite.
- `// R-WMWB-7EWX` in `cmd/sites/main_test.go` — immediately above
  `// R-ZJCG-RIVA`, D19's current name-links-to-slug-URL claim, same test. Same
  rewrite.
- `// R-83NK-DUW1` in `cmd/sites/main_test.go` — immediately above
  `// R-ZGWN-ZZDW`, D6's current control-layout claim, same test. `956e1c00`
  rewrote it when the Slug header became the Name header.
- `// R-0UUY-N97T` in `internal/mcp/tools_test.go` — in `TestToolsList`, a few
  lines below `// R-Z8DD-BL71`. The retired id pinned an **exactly-fifteen** tool
  count; `956e1c00` replaced it with D13's **exactly-sixteen** partition claim
  when `rename` joined the domain set. The tag now sits on the count assertion
  that `R-Z8DD-BL71` owns.

### Group B — the successor id proves the claim on a *different* test

The rewrite minted a stronger successor and the build wrote a **new** test for
it, leaving the older, weaker test in place with its now-dead tag. The older
tests still pass and still assert true things; they are simply no longer the
proof of record. Keep them.

- `// R-RAW6-IUN5` in `cmd/sites/main_test.go`
  (`TestWWWLandingRendersExistingSites`). Two generations back: `3f0d424d` turned
  it into `R-HK3X-22SM` (three visibilities), `956e1c00` turned that into
  D19's current **`R-ZI4K-DR4L`**, tagged on the stronger three-visibility render
  test in `cmd/sites/landing_visibility_test.go`.
- `// R-IDOL-PV70` in `cmd/sites/main_test.go`
  (`TestLandingHandlerRendersJSONIslandFromSiteRows`). Same two-generation chain
  for the data island: → `R-HLBT-FUJB` → D19's current **`R-ZKKD-5ALZ`**, tagged
  in `cmd/sites/landing_visibility_test.go`.
- `// R-IG4E-HEOE` in `cmd/sites/main_test.go`
  (`TestWWWLandingRendersProgressiveControlMarkup`). `835a3928` retired it into
  `R-83NK-DUW1`, now D6's **`R-ZGWN-ZZDW`**, which asserts the same
  `data-sort-key` hooks on Name/Creator/Created with none on Visibility, plus the
  document-order layout the retired id did not pin.
- `// R-IHCA-V6F3` in `cmd/sites/main_test.go`, same test. `835a3928` retired the
  hidden-until-JS claim into `R-83NK-DUW1` → **`R-ZGWN-ZZDW`**; the empty-island
  assertion the tag actually sits on is pinned by D19's **`R-ZKKD-5ALZ`**
  ("rendering with an **empty** slice emits an island parsing to `[]`").
- `// R-554R-3MBC` in `internal/mcp/tools_test.go`
  (`TestCreateHonorsRequestedVisibility`). Two generations: `3f0d424d` → the
  visibility-enum `R-HACP-ZWV2`, `956e1c00` → D20's current **`R-ZQNV-25BG`**,
  tagged on `TestCreateSeparatesNameAndSlugForPublicAndPrivate` in
  `internal/mcp/identity_lifecycle_test.go`, which asserts the row, the stored
  name, the directory, and the `url` per visibility and adds the
  missing/invalid-slug rejections.
- `// R-HACP-ZWV2` in `internal/mcp/tools_test.go`
  (`TestCreateNamedVisibilityAndConditionalName`) — the middle generation of that
  same chain, superseded by **`R-ZQNV-25BG`** (create shape) together with D20's
  **`R-ZO82-ALU2`** (the name-required-at-every-visibility half), both tagged in
  `internal/mcp/identity_lifecycle_test.go`.
- `// R-RGZO-FPCM` in `internal/mcp/tools_test.go`
  (`TestSetVisibilityMovesBetweenPublicAndPrivate`). `3f0d424d` retired the
  boolean-flip claim; D20's current **`R-ZVJG-L8A8`** covers it on
  `TestSetVisibilityNamedMatrixAndIdempotence`, the very next function in the
  same file, which adds the name-preservation and idempotence halves.
- `// R-RJFH-78U0` in `internal/mcp/tools_test.go` (`TestDelete`). `956e1c00`
  retired it into D20's current **`R-00F2-4B90`** (same claim, keyed by slug),
  tagged on `TestDeleteRemovesSlugRowAndDirectory` in
  `internal/mcp/identity_lifecycle_test.go`.

## What this phase must not do

- **Delete no test function and no assertion.** Every one of the fourteen tests
  keeps its body. This phase removes fourteen comment lines and nothing else.
- **Add no tag and mint no id.** If a test looks under-covered, that is a finding
  to report, not a licence to invent an id.
- **Touch no non-test source.** No file outside `*_test.go` changes.
- **Leave `// R-IEWI-3MXP` alone.** It is a *current* D19 id and stays exactly
  where it is, even though its tag and its Decision text do not line up — that
  mismatch is reported to the operator separately and is explicitly out of scope
  here.

## Done when

- **All fourteen tags are gone.** From `sites/`, this prints nothing:

  ```
  grep -rn --include='*_test.go' --exclude-dir=project \
    -e R-HK3X-22SM -e R-HLBT-FUJB -e R-HMJP-TMA0 -e R-WMWB-7EWX -e R-83NK-DUW1 \
    -e R-0UUY-N97T -e R-RAW6-IUN5 -e R-IDOL-PV70 -e R-IG4E-HEOE -e R-IHCA-V6F3 \
    -e R-554R-3MBC -e R-HACP-ZWV2 -e R-RGZO-FPCM -e R-RJFH-78U0 .
  ```

- **No test function was deleted.** From `sites/`, each of these fourteen names
  still resolves to exactly one `func` declaration:

  ```
  grep -rc --include='*_test.go' --exclude-dir=project \
    -e 'func TestWWWLandingRendersExistingSites' \
    -e 'func TestLandingHandlerRendersJSONIslandFromSiteRows' \
    -e 'func TestWWWLandingRendersProgressiveControlMarkup' \
    -e 'func TestLandingHandlerLinksNamesToSlugVisibilityURLs' \
    -e 'func TestToolsList' \
    -e 'func TestCreateHonorsRequestedVisibility' \
    -e 'func TestCreateNamedVisibilityAndConditionalName' \
    -e 'func TestSetVisibilityMovesBetweenPublicAndPrivate' \
    -e 'func TestDelete(' . | grep -v ':0$'
  ```

  and `go test ./...` reports **no** reduction in the number of tests run versus
  the pre-phase baseline (capture `go test ./... -v 2>&1 | grep -c '^=== RUN'`
  before and after; the two counts must be equal).

- **The surviving successor ids are untouched.** From `sites/`, each of these
  prints exactly one hit, in the file named:

  ```
  grep -rn --include='*_test.go' --exclude-dir=project \
    -e R-ZI4K-DR4L -e R-ZKKD-5ALZ -e R-ZLS9-J2CO -e R-ZJCG-RIVA -e R-ZGWN-ZZDW \
    -e R-Z8DD-BL71 -e R-ZQNV-25BG -e R-ZO82-ALU2 -e R-ZVJG-L8A8 -e R-00F2-4B90 \
    -e R-IEWI-3MXP .
  ```

- **No orphan tags remain at all.** From `sites/`, the code-only difference is
  empty — every id tagged in a test file is a current design id:

  ```
  comm -13 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u)
  ```

- **The three adopted suite-contract ids each keep exactly one tag**, in
  `cmd/sites/main_test.go` and nowhere else. From `sites/`, this names that one
  file three times with count `1` and nothing else:

  ```
  for id in R-4LKF-FB23 R-8DF1-W89F R-8IAN-FB87; do
    grep -rc --include='*_test.go' --exclude-dir=project "$id" . | grep -v ':0$'
  done
  ```

- **Coverage is total.** From `sites/`, the design-only difference is empty:

  ```
  comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
  ```

- **The suite is green** per design's Conventions: `go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed from
  `sites/` with zero failures — including D23's browser wiring test, which
  hard-requires `google-chrome` on `PATH` and is never skipped.
