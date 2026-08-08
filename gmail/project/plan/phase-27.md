# Phase 27 — Testing-language conformance: declare the tree's testing facts and add the two adopted conformance tests

*Realizes design Decision 25 (testing-language conformance).*

Documentation plus one new test file. No existing gmail test or source file
changes — `internal/gmail/live_test.go` is already conformant (`//go:build live`
with `requiredLiveCredential` hard-failing) and must be left exactly as it is.
Two changes, both in `gmail/`:

1. **Declare the testing facts in `gmail/AGENTS.md`.** Its Tests section replaces
   the single package-checks line with the declarations D25's table states: the
   default-gate command `go test ./...` (and that green also means clean
   `go build ./...`, `go vet ./...`, `gofmt -l .`); the layers present —
   **hermetic**, **composed**, **live**; that there is **no** environmental
   precondition beyond the Go toolchain; the **GOWORK mode** (workspace for the
   default gate, `GOWORK=off` forced by the production build); and the live
   invocation `go test -tags live ./...` with its `GMAIL_CLIENT_ID` /
   `GMAIL_CLIENT_SECRET` / `GMAIL_REFRESH_TOKEN` credentials, run at deploy
   verification. This is what makes D19's R-3NGL-AMPW reachable rather than
   dead.
2. **Add the two conformance tests** in a new hermetic file
   `gmail/cmd/gmail/docs_test.go` (the sibling-service idiom: shipped-file and
   doc-truth guards live in `cmd/<svc>/`, beside the deploy-drift guards), each
   tagged with its adopted id:
   - `R-O1AD-MRKW` — reads `../../AGENTS.md` from disk, isolates its `## Tests`
     section, and asserts that section names the default-gate command, each of
     the three layer names, the no-precondition statement, and the GOWORK mode.
     It must fail if any one of those is missing, and must not pass on a match
     found elsewhere in the file.
   - `R-O2IA-0JBL` — walks the tree for `*_test.go` files, skips any whose
     source carries the `live` build constraint, and asserts zero occurrences of
     `t.Skip`, `t.Skipf`, and `t.SkipNow`. The needle is assembled from parts at
     runtime (e.g. `"t." + "Skip"`) so the scan never matches its own source.
     Report every offending file and line. It passes on landing; its value is
     being re-run on every gate.

**Done when:**

- `R-O1AD-MRKW` is covered by a test that reads the committed `AGENTS.md` and
  fails when a required declaration is absent.
- `R-O2IA-0JBL` is covered by the self-excluding source scan and passes with
  zero findings.
- `cd gmail && grep -rn 't\.Skip' --include='*_test.go' .` prints **nothing**.
- `cd gmail && git diff --name-only -- internal/gmail/live_test.go` is **empty**
  — the already-conformant live file was not touched — and
  `cd gmail && go vet -tags live ./...` succeeds.
- The suite is green: `cd gmail && go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), `go test ./...` all succeed with zero failures.
