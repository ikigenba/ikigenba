# Phase 31 — Remove the two stray R-4LKF-FB23 tags outside the boot smoke

*Realizes no design Decision — structural tag hygiene. No dependencies.*

`R-4LKF-FB23` is the umbrella opsctl install-layout adoption id
(`project/design/D08.md` at the repo root, `[proof: per-service]`). D9 names
scripts' proof of record: `TestScriptsBootsFromOpsctlLayoutAndServesHealth` in
`cmd/scripts/main_test.go`. Two stray `// R-4LKF-FB23` tag comments survive from
the pre-DNN multi-file spread, inside the bodies of:

- `internal/runner/runner_test.go` `TestSpawnUsesRebuildableRunsDirOutsideState`
  (~line 997)
- `internal/script/service_test.go` `TestServiceReadsRunFilesFromRebuildableRunsDir`
  (~line 566)

Neither test proves the install-layout boot behavior. Delete the two tag comment
lines; both tests themselves stay exactly as they are.

End state: exactly one `R-4LKF-FB23` tag in the scripts tree, on the boot smoke
D9 names.

**Done when:**

- `grep -rn 'R-4LKF-FB23' --include='*_test.go' .` from the scripts root prints
  exactly one line: the tag on `TestScriptsBootsFromOpsctlLayoutAndServesHealth`
  in `cmd/scripts/main_test.go`.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` silent, `go test ./...` all passing).
