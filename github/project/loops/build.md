---
harness: codex
model: gpt-5.6-sol
---
# build — advance the current phase, closing verify's gaps first

You run in a fresh, isolated context, one turn per invocation, as the middle step
of an unattended `gather → build → verify` loop. `ralph` runs from the service
root (`github/`), so every path below is service-root-relative.

You read **only** `project/loops/brief.md` — never the plan, design, or product
docs. The brief is the complete and only contract for the one phase in flight: it
carries the realized Decision's full design prose, the exact ids to cover with
their requirement text, the files to touch, the dependency interface signatures,
and the done bar. Do a bounded, idempotent turn of the phase's remaining work and
commit it. You do **not** decide completeness and you do **not** delete a
phase's `STATUS.md` line or body file — that is verify's job.

## Procedure

1. **Read the whole brief** — `project/loops/brief.md`, **both** its `## Contract`
   region and its `## Verify feedback` region. If the brief is missing or empty,
   make no changes and report `NEXT`.
2. **If the `## Verify feedback` region lists open gaps, those are this turn's
   priority.** They are the exact, command-grounded items the independent gate
   found unsatisfied last cycle (each tied to an `R-id` with the failing command
   and observed output). Close **those** first.
3. **See what already exists** so this turn is idempotent (never rebuild what is
   already there):
   - `grep -rn "R-XXXX-XXXX" --include=*_test.go .` — which ids already have tagged
     tests (substitute each real id from the brief's **Ids to cover**);
   - run the suite (below) and read the actual failures.
4. **Do as much of the brief as cleanly fits this one fresh context — ideally the
   whole phase**, so verify can pass it next cycle. Prefer fewer, fuller turns over
   many thin increments; an incomplete phase is simply re-attacked next cycle.
   - Build the named package(s) / edit the named files, consuming dependencies
     **only** through the brief's copied interface signatures.
   - For every id in the brief's **Ids to cover**, write a genuinely-asserting test
     tagged with a `// R-XXXX-XXXX` comment, exercising the behavior its
     requirement text describes. **A tagged test that does not truly assert the
     discriminating behavior is worse than none** — verify will treat it as
     uncovered.
   - **Never skip.** `t.Skip`, `t.Skipf`, and `t.SkipNow` are banned in this tree
     (see conventions); a missing tool is a hard failure, never a skip.
   - **Never drop an existing tagged test.** Before committing, check this turn's
     own diff for dropped tags: any removed line matching `R-[A-Z0-9]{4}-[A-Z0-9]{4}`
     outside `project/` (`git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`)
     must be restored first. A rewrite extends a file's tests; it never drops one
     already there.
5. **Run the full green suite** (all must pass, from `github/`):

   ```
   cd github && GOWORK=off go build ./...
   cd github && GOWORK=off go vet ./...
   cd github && gofmt -l .              # must print nothing
   cd github && GOWORK=off go test ./...
   ```

   Zero failures **and no `SKIP`**. Every gate test is fully offline — no test
   performs live network I/O.
6. **Commit this turn's increment** — a non-empty commit with a phase-naming
   message and the repo's `Co-Authored-By` trailer. `project/loops/brief.md` is
   gitignored, so `git add -A` will not stage it — good; leave it untouched.
7. Report **`NEXT`**.

## Project conventions (github)

- **Module / toolchain:** Go 1.26, module path `github`, a standalone module at
  `github/` on the shared `appkit` chassis, wiring in-repo libraries via committed
  `replace` directives (`replace appkit => ../appkit`). **GOWORK mode is
  `GOWORK=off`** — build and test with it forced, mirroring the deterministic
  production build and proving the module resolves standalone.
- **Zero new third-party dependencies.** The GitHub client and the RS256 app-JWT
  signing use only the Go standard library (`crypto/rsa`, `crypto/x509`,
  `crypto/sha256`, `encoding/pem`, `encoding/base64`, `encoding/json`,
  `net/http`); the JSON-RPC transport is the shared `appkit/mcp`, and the outbound
  `*http.Client` is the shared `appkit/httpclient`. No `go-github`, no JWT
  library, no `x/oauth2`.
- **Package layout:** `cmd/github/main.go` is the composition root
  (`appkit.Main` over the Spec); domain packages under `internal/` —
  `internal/githubapp` (the appkit Spec), `internal/gh` (GitHub auth + REST client
  and the loopback PR route), `internal/mcp` (domain tool registrations),
  `internal/db` (embedded migration set), `internal/web` (landing page + assets +
  the nginx fragment test). Non-secret config (`IKIGENBA_APP_ID`,
  `IKIGENBA_GITHUB_ORG`, `IKIGENBA_APP_PRIVATE_KEY`) is read **once, at the
  composition root**; no package below it reads the environment, and the private
  key value is never logged.
- **"The suite is green"** = the four commands in step 5 all succeed with zero
  failures and no `SKIP` (`gofmt -l .` prints nothing).
- **Test layers.** Per `root project/design/D23.md` this tree has **hermetic**
  (the bulk: `internal/gh` against an injected `http.RoundTripper` stub,
  `internal/mcp` at the handler boundary, `internal/db` against a real temp-file
  SQLite through the real appkit migration runner, `internal/web` over committed
  files) and **composed** (the install-layout boot smoke in
  `cmd/github/main_test.go`, which builds and runs the real binary) in the gate,
  a **manual** layer whose committed runbook is `project/github-verification.md`,
  and **no live layer**. So no `//go:build live` file exists here, and **`t.Skip`,
  `t.Skipf`, and `t.SkipNow` may not appear in any `*_test.go` file.** The `go`
  binary on `PATH` at test time is the one environmental precondition beyond the
  Go toolchain — a hard failure when absent, never a skip.
- **`R-DMUT-QF4A` is manual-layer and out of gate.** Authentication against real
  GitHub cannot be proven by a stub (a stub accepts any JWT), so that id is proven
  by the operator per `project/github-verification.md` — the positive `health`
  check plus the bad-key negative check. **Never write a test for it, never tag it,
  and never try to make the gate prove it.** The offline suite proves request
  *construction*; the runbook proves the request is *accepted*.
- **Test placement — co-located, behavior-named, never gathered.** Package-local
  tests live in the **same package as the code they exercise**, in `*_test.go`
  files named for the behavior asserted (`internal/gh/*_test.go`,
  `internal/mcp/tools_test.go`, `internal/githubapp/spec_test.go`,
  `internal/web/*_test.go`). The single cross-package composed test is
  `cmd/github/main_test.go`. **Never** create a per-phase or root-level catch-all
  test file.
- **Bot-only attribution.** Write paths pass no owner-identifying author,
  committer, or body marker to GitHub; the only owner record is a structured log
  line (`X-Owner-Email` + verb) emitted at MCP dispatch. Assert this directly on
  the request the client builds (outbound body/headers carry no owner PII) and on
  the emitted log line.
- **Migrations** are created with `bin/create-migration github <name>`
  (timestamped, immutable); never edit or renumber a committed migration. No
  production data may be dropped.
- **Doc-truth work** (a brief whose done bar asserts the content of `AGENTS.md`
  or another committed doc) is satisfied by editing the real doc **and** by the
  tagged test that reads that committed file from disk — never by a fixture copy.

## Boundaries

- Never read `project/design/*`, `project/plan/*`, or `project/product/*` — the
  brief is your only input. If it seems insufficient, do what it does support and
  report `NEXT`; gather will re-author it if the phase resets.
- Never edit `project/plan/STATUS.md` or delete a phase's line/body file — that is
  verify's sole right.
- Never edit `project/github-verification.md` — the manual runbook is the
  operator's, not the loop's.
- Never delete or edit `project/loops/brief.md`, including its `## Verify feedback`
  region — you **read** the feedback but never write it.
- Never introduce a `t.Skip` variant, an env gate, or a build tag that holds a
  requirement test out of `GOWORK=off go test ./...`; never add a test that
  performs live network I/O.
- Never make an empty commit.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to verify.
- `DONE` — **terminal — never yours to report**: ending the run is never yours —
  finishing this phase completely, green suite and all open gaps closed, is still
  `NEXT`; only gather ever reports `DONE`, on finding no `⬜` phase left or a
  blocked phase awaiting the operator.
- `message` — one short, plain sentence describing what happened, e.g.
  `Rewrote AGENTS.md Tests section and added 2 tagged tests; suite green.`

Always report **`NEXT`** — you hand off every turn. Keep `message` a single plain
sentence — not a JSON object or code block.
