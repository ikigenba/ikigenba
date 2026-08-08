# Phase 33 — Testing-language conformance: make `python3` a hard precondition and declare the tree's testing facts

*Realizes design Decision 34 (testing-language conformance).*

One test helper plus the tree's `AGENTS.md` and one new test file. Three changes,
all in `scripts/`:

1. **`requirePython` hard-fails.** In `scripts/internal/runner/runner_test.go`
   (~line 138), the helper stops calling `t.Skip` when
   `exec.LookPath("python3")` fails and instead calls `t.Fatalf` naming
   `python3` and stating it is a declared environmental precondition of the
   default gate. Its call sites are unchanged, and no assertion in the file
   changes. The file must stay `gofmt`-clean.
2. **Declare the testing facts in `scripts/AGENTS.md`.** Its Tests section
   replaces the current two bullets with the declarations D34's table states:
   the default-gate command `go test ./...` (and that green also means clean
   `go build ./...`, `go vet ./...`, `gofmt -l .`); the layers present —
   **hermetic** and **composed**, and explicitly **no live and no manual**
   layer; the environmental precondition **`python3` on `PATH`**; and the
   **GOWORK mode** (workspace for the default gate, `GOWORK=off` forced by the
   production build). The existing note that `suite.py` is tested through a real
   `python3` probe harness is kept and now reads as the precondition's reason.
3. **Add the two conformance tests** in a new hermetic file
   `scripts/cmd/scripts/docs_test.go` (the sibling-service idiom: shipped-file
   and doc-truth guards live in `cmd/<svc>/`), each tagged with its adopted id:
   - `R-O1AD-MRKW` — reads `../../AGENTS.md` from disk, isolates its `## Tests`
     section, and asserts that section names the default-gate command, the layer
     names **hermetic** and **composed**, the `python3` precondition, and the
     GOWORK mode — and asserts it does **not** claim a live or manual layer. It
     must fail if any required declaration is missing, and must not pass on a
     match found elsewhere in the file.
   - `R-O2IA-0JBL` — walks the tree for `*_test.go` files, skips any whose
     source carries the `live` build constraint, and asserts zero occurrences of
     `t.Skip`, `t.Skipf`, and `t.SkipNow`. The needle is assembled from parts at
     runtime (e.g. `"t." + "Skip"`) so the scan never matches its own source.
     Report every offending file and line.

**Done when:**

- `R-O1AD-MRKW` is covered by a test that reads the committed `AGENTS.md` and
  fails when a required declaration is absent.
- `R-O2IA-0JBL` is covered by the self-excluding source scan and passes with
  zero findings.
- `cd scripts && grep -rn 't\.Skip' --include='*_test.go' .` prints **nothing**.
- `cd scripts && grep -c 'python3 not on PATH' internal/runner/runner_test.go`
  returns `0` (exit status 1 from `grep` is the pass) — the skip message is
  gone.
- The suite is green: `cd scripts && go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), `go test ./...` all succeed with zero failures on a
  machine that has `python3` on `PATH`.
