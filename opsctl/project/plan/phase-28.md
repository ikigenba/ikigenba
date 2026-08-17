# Phase 28 — Decouple opsctl's tests from sibling services: delete the service-boot family, guard against its return

*Realizes design Decision 17 (testing-language contract — the hermetic layer
builds and boots no sibling service).*

opsctl's hermetic suite currently compiles and boots eight real sibling service
binaries, which couples its gate to those trees' boot contracts (the channel-5
`state/` change to gmail/dropbox reddened opsctl though nothing opsctl owns
changed) and breaks D17's hermetic contract (a compiled, executed service binary
is neither a temp-dir file nor a faked seam). Remove that coupling and lock it
out:

- **Delete the eight service-boot integration tests and their fixtures** from
  `internal/opsctl/deploy_test.go` (notify, dropbox, prompts, wiki, cron, gmail,
  sites) and `internal/opsctl/setup_test.go` (webhooks): each
  `Test<Svc>SetupDeployBoots…` function, each `<svc>BootSystem` type and its
  methods, and each `build<Svc>Artifact` helper — plus any helper (e.g. a
  shared binary-bundling helper) left unused once they are gone. These tests
  carry **no** requirement-id tags and are the only tests that build or boot a
  real sibling: deploy/setup orchestration stays proven by the generic
  fake-`System` tests that own the D15/D12/D7/D8 ids (`TestDeploy…`,
  `TestSetup…`, `TestStage…`), and rotating-credential delivery stays proven by
  the seed-state tests (D19). Both families are untouched.
- **Add the guard test** realizing R-E639-DSH5: a hermetic source scan over
  opsctl's `*_test.go` files asserting none builds a sibling service, with the
  needle assembled from parts so the scanning file never matches itself.

No production (`internal/opsctl/*.go` non-test) code changes; this phase edits
only test files.

**Done when** (from `opsctl/`, the loop's working directory):
- The suite is green: `GOWORK=off go build ./...` and `GOWORK=off go test ./...`
  both succeed with zero failures.
- **R-E639-DSH5** is covered by a tagged source-scan test that scans opsctl's
  `*_test.go` files and fails if any builds a sibling service — zero
  three-parent path escapes (`../../..`) resolving a sibling tree and zero
  `go build ./cmd/<name>` invocations of one — its needle assembled from parts
  so it never matches itself.
- `grep -rnE 'func build[A-Za-z]+Artifact|BootSystem' internal/opsctl` prints
  nothing: the eight builders and boot-system types are gone.
