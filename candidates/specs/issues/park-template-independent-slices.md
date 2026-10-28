# Park template hides independently populated rows

Filed during the user-invoked `build-spec` run on `wip-funnel`, starting at `12a2f3d9df953cbe1f15174be772c585236d6356`. The pages verifier reported the conflict and a fresh read-only verifier independently reproduced it.

## Requirements and friction

R-J3KN-8MJW in D08 requires direct execution of the park template returned by `pages.Load` with each slice empty or nonempty and visibility of the supplied place and candidate strings. R-QIO8-DIU7 declares the `ParkData` slices. The requirement does not restrict those independent template states to data produced by the store or the route described by R-1O8P-IM4N.

The committed template suppresses its places and found-candidates tables when `Walks` is empty, even when `Places` or `FoundCandidates` has rows. Source changes cannot correct direct template execution without changing the read-only asset or violating the rule that markup lives exclusively in assets. R-J3KN-8MJW remains unproved; the incomplete coverage tag is removed.

## Evidence

`candidates/assets/park.html` line 231 gates both tables on the walk-history slice. The newly written `TestWalkTemplateStates` initially checked row visibility only when its walk-history bit was set, so that passing test did not prove the required independent combinations.

A test-only overlay constructed `ParkData` with empty `Walks` and distinct supplied values in `Places` and `FoundCandidates`, then executed the park template through `pages.Load`. Both the pages verifier and a fresh blocker verifier ran:

```text
go test -overlay /tmp/scratch.DngTfX ./internal/pages -run '^TestVerifierJ3KNIndependentSlices$' -count=1
```

The command exited 1: `probe-place-id`, `probe-place-name`, `probe-place-walk`, `probe-found-id` and `probe-found-name` were absent from the body with empty walk history. The same data with a nonempty `Walks` control passed. The overlay source was `/tmp/scratch.YR8eV3`; both scratch files were outside the checkout, and no asset was edited.

## Suggested resolution

Outside this build run, correct the asset to display populated places and found candidates independently of walk history, or revise the requirement to describe the intended valid combinations. The build cannot edit assets or the design.
