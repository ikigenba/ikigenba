# Phase 20 — Remove the stray R-4LKF-FB23 tag from the cursor-reconstruction test

*Realizes no design Decision — structural tag hygiene. No dependencies.*

`R-4LKF-FB23` is the umbrella opsctl install-layout adoption id
(`project/design/D08.md` at the repo root, `[proof: per-service]`). D10 names
notify's proof of record: `TestNotifyBootsFromOpsctlLayoutAndServesHealth` in
`cmd/notify/main_test.go`. A second, stray `// R-4LKF-FB23` tag sits on
`TestNotifyConsumerReconstructsCursorAfterProducerCacheRemint` in
`internal/push/push_test.go` (~line 361) — a cursor-reconstruction test that
proves nothing about the install layout. Delete that one tag comment line;
the test itself stays exactly as it is.

End state: exactly one `R-4LKF-FB23` tag in the notify tree, on the boot smoke
D10 names.

**Done when:**

- `grep -rn 'R-4LKF-FB23' --include='*_test.go' .` from the notify root prints
  exactly one line: the tag on `TestNotifyBootsFromOpsctlLayoutAndServesHealth`
  in `cmd/notify/main_test.go`.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` silent, `go test ./...` all passing).
