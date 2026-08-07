# Phase 36 — Remove the stray R-4LKF-FB23 tag from the mirror-path test

*Realizes no design Decision — structural tag hygiene. No dependencies.*

`R-4LKF-FB23` is the umbrella opsctl install-layout adoption id
(`project/design/D08.md` at the repo root, `[proof: per-service]`). D10 names
dropbox's proof of record: `TestDropboxBootsFromOpsctlLayoutAndServesHealth` in
`cmd/dropbox/main_test.go` (~line 476). A second, stray `// R-4LKF-FB23` tag in
the same file sits on `TestDefaultMirrorPathTracksDurableStateDB` (~line 190) —
a mirror-path-derivation test that proves nothing about the install layout.
Delete that one tag comment line; the test itself stays exactly as it is.

End state: exactly one `R-4LKF-FB23` tag in the dropbox tree, on the boot smoke
D10 names.

**Done when:**

- `grep -rn 'R-4LKF-FB23' --include='*_test.go' .` from the dropbox root prints
  exactly one line: the tag on `TestDropboxBootsFromOpsctlLayoutAndServesHealth`
  in `cmd/dropbox/main_test.go`.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` silent, `go test ./...` all passing).
