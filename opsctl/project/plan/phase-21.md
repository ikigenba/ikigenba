# Phase 21 — Declare opsctl's testing facts and prove the declaration

*Realizes design Decision 17 (testing-language contract) — the two cited
per-service ids only.*

opsctl's `AGENTS.md` gains the Tests declaration D17 specifies, and
`internal/opsctl` gains one test file that keeps the declaration honest and
proves the default gate cannot skip.

What gets built:

- **`opsctl/AGENTS.md`** — its **Tests** section rewritten to state, in the
  contract's vocabulary: the default-gate test command
  (`GOWORK=off go test ./...`), the layers present (`hermetic` and `manual`, and
  that there is no composed and no live layer), the environmental preconditions
  beyond the Go toolchain (a real `tar` binary), and the GOWORK mode
  (`GOWORK=off`), with a pointer to `project/opsctl-verification.md` for the
  manual layer. The existing sentence about live-box ids "checked out of loop"
  is replaced, not stacked beside the new text.
- **`opsctl/internal/opsctl/testing_contract_test.go`** — a new package-local
  test file carrying both cited ids. It reads the committed `AGENTS.md` and
  walks the tree's `*_test.go` files from the package directory's tree root;
  both are ordinary hermetic file reads, no build tags, no seams.

**Done when:**

- `R-O1AD-MRKW` — a test reads the committed `opsctl/AGENTS.md` from disk (not a
  fixture, not an embedded copy) and asserts its Tests section declares the
  default-gate test command, the layer names present, the `tar` precondition,
  and `GOWORK=off`; deleting any one of those four facts from `AGENTS.md` fails
  the test.
- `R-O2IA-0JBL` — a test scans every `*_test.go` under the opsctl tree,
  excluding files whose build constraints include `live`, and asserts **zero**
  occurrences of `t.Skip`, `t.Skipf`, `t.SkipNow`; the needle is assembled from
  parts at runtime so the scanning file does not match itself (verified by the
  scan passing while that file is in range).
- The suite is green: `GOWORK=off go build ./...` and `GOWORK=off go test ./...`
  from `opsctl/` both succeed.
- Both ids appear verbatim as tags in `opsctl/internal/opsctl/*_test.go`:
  `grep -rl 'R-O1AD-MRKW' --include='*_test.go' --exclude-dir=project .` and the
  same for `R-O2IA-0JBL` each print exactly 1 path.
