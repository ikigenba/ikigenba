# Phase 38 — Testing-language conformance: delete the untagged live probe, harden the live layer, declare the facts

*Realizes design Decision 30 (testing-language conformance).*

One package's tests plus the tree's `AGENTS.md` and one new test file. Four
changes, all in `dropbox/`:

1. **Delete `TestLiveProbe`** from `dropbox/internal/dropbox/client_test.go`,
   together with the `// ---- Live integration probe ... ----` banner comment
   above it and any import (`os`, and `time`/`http` if they become unused) that
   only it needed. Nothing else in `client_test.go` changes; the file must stay
   `gofmt`-clean and must no longer read a `DROPBOX_*` variable.
2. **Make the live helper hard-fail.** In
   `dropbox/internal/dropbox/client_live_test.go`, `newLiveClient` stops calling
   `t.Skip` and instead fails with `t.Fatalf` naming each absent variable — one
   required-credential helper in the shape of gmail's `requiredLiveCredential`,
   used for `DROPBOX_APP_KEY`, `DROPBOX_APP_SECRET`, and
   `DROPBOX_REFRESH_TOKEN`. `DROPBOX_APP_FOLDER_ROOT` stays optional and is read
   as before. The file keeps its `//go:build live` constraint and its three
   existing ids unchanged.
3. **Declare the testing facts in `dropbox/AGENTS.md`.** Its Tests section
   replaces the single `Unit: go test ./...` line with the declarations D30's
   table states: the default-gate command `go test ./...` (and that green also
   means clean `go build ./...`, `go vet ./...`, `gofmt -l .`); the layers
   present — **hermetic**, **composed**, **live**; that there is **no**
   environmental precondition beyond the Go toolchain; the **GOWORK mode**
   (workspace for the default gate, `GOWORK=off` forced by the production
   build); and the live invocation `go test -tags live ./...` with its
   `DROPBOX_APP_KEY` / `DROPBOX_APP_SECRET` / `DROPBOX_REFRESH_TOKEN`
   credentials, run at deploy verification.
4. **Add the two conformance tests** in a new hermetic file
   `dropbox/cmd/dropbox/docs_test.go` (the sibling-service idiom: shipped-file
   and doc-truth guards live in `cmd/<svc>/`), each tagged with its adopted id:
   - `R-O1AD-MRKW` — reads `../../AGENTS.md` from disk, isolates its `## Tests`
     section, and asserts that section names the default-gate command, each of
     the three layer names, the no-precondition statement, and the GOWORK mode.
     It must fail if any one of those is missing, and must not pass on a match
     found elsewhere in the file.
   - `R-O2IA-0JBL` — walks the tree for `*_test.go` files, skips any whose
     source carries the `live` build constraint, and asserts zero occurrences of
     `t.Skip`, `t.Skipf`, and `t.SkipNow`. The needle is assembled from parts at
     runtime (e.g. `"t." + "Skip"`) so the scan never matches its own source.
     Report every offending file and line.

**Done when:**

- `R-O1AD-MRKW` is covered by a test that reads the committed `AGENTS.md` and
  fails when a required declaration is absent (verified by temporarily removing
  one declaration during development, not by a committed negative fixture).
- `R-O2IA-0JBL` is covered by the self-excluding source scan and passes with
  zero findings.
- `cd dropbox && grep -rn 't\.Skip' --include='*_test.go' .` prints **nothing**
  — the tree has no skip left, tagged or untagged.
- `cd dropbox && grep -c 'TestLiveProbe' internal/dropbox/client_test.go`
  returns `0` (exit status 1 from `grep` is the pass).
- `cd dropbox && grep -c 'go:build live' internal/dropbox/client_live_test.go`
  returns `1` and
  `cd dropbox && go vet -tags live ./...` succeeds — the live layer still
  compiles under its tag.
- The suite is green: `cd dropbox && go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), `go test ./...` all succeed with zero failures.
