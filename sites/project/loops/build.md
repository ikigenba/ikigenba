---
harness: codex
model: gpt-5.6-sol
---

# build — advance the current phase by one bounded increment

You run in a fresh, isolated context, one turn per invocation, as the middle step
of an unattended `gather → build → verify` loop. `ralph` runs from the service
root (`sites/`), so every path below is service-root-relative.

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
   - **Never drop an existing tagged test.** Before committing, check this turn's
     own diff for dropped tags: any removed line matching `R-[A-Z0-9]{4}-[A-Z0-9]{4}`
     outside `project/` (`git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`)
     must be restored first. A rewrite extends a file's tests; it never drops one
     already there.
5. **Run the full green suite** (all must pass, from `sites/`):

   ```
   cd sites && go build ./...
   cd sites && go vet ./...
   cd sites && gofmt -l .            # must print nothing
   cd sites && go test ./...
   ```

   Green **includes** the D23 headless-Chrome browser-wiring test and therefore
   requires a `google-chrome` binary on `PATH`; no Chrome makes the suite **red**,
   never skipped. One browser-*launch* retry is allowed; scenario assertions are
   never retried.
6. **Commit this turn's increment** — a non-empty commit with a phase-naming
   message and the repo's `Co-Authored-By` trailer. `project/loops/brief.md` is
   gitignored, so `git add -A` will not stage it — good; leave it untouched.
7. Report **`NEXT`**.

## Project conventions (sites)

- **Module / toolchain:** Go 1.26, single `module sites` rooted at `sites/`;
  pure-Go SQLite `modernc.org/sqlite` (no cgo); `appkit`, `eventplane`, and
  `registry` are committed in-repo replace-siblings. No `agentkit` dependency.
- **"The suite is green"** = the four commands in step 5 all succeed with zero
  failures (`gofmt -l .` prints nothing), **and** the D23 browser test runs (no
  skip) against `google-chrome` on `PATH`.
- **Test placement — co-located, behavior-named, never gathered.** Package-local
  unit tests live in the **same package as the code they exercise**, in
  `*_test.go` files named for the behavior asserted — e.g.
  `internal/sites/*_test.go` (domain store, layout, sync, token), `internal/serve/*_test.go`
  (static server), `internal/files/*_test.go` (confined filesystem ops),
  `internal/mcp/*_test.go` (tool table), `internal/db/*_test.go` (migration load
  guard). The few **cross-package integration tests** — the landing render over
  the repo-real `share/www` tree, the goja-driven `landing.js` logic tests, the
  single chromedp browser-wiring test, and the nginx-fragment content assertion —
  live in `cmd/sites/*_test.go` (see the existing `main_test.go`,
  `landing_logic_test.go`, `landing_controls_test.go`, `landing_copy_test.go`,
  `landing_visibility_test.go`). **Never** create a per-phase or root-level test
  file, and never gather multiple packages' tests into one file. A phase is
  (almost always) one package; its tests live with it.
- **Real substrate where a claim needs it.** The domain store's tests run
  against a real migrated `modernc.org/sqlite` (via `appkit/db`), asserting the
  schema itself (`pragma table_info`, a rejected CHECK-violating INSERT) rather
  than a mock. The static server is tested with `net/http/httptest` over a real
  temp `SITES_ROOT` tree. The landing page's client JavaScript is proven in two
  tiers: `github.com/dop251/goja` evaluates the real shipped
  `share/www/static/landing.js` for the pure logic (filter/sort/paginate/reduce),
  and a single `github.com/chromedp/chromedp` headless-Chrome session proves the
  DOM wiring end to end — both are test-only dependencies, imported only from
  `*_test.go`, linked into no shipped binary.
- **Migrations** are created with `bin/create-migration sites <name>`
  (timestamped, immutable); never edit or renumber a committed migration. A
  schema change is always a **new** migration; no production data may be
  dropped.
- **Determinism seam:** handlers take their inputs explicitly (name/version
  strings, the site slice, `SITES_ROOT`) — no clock, no network in the pure
  paths. The landing page's client logic is written as pure functions behind a
  `document` guard; the DOM controller (`initController`) is the impure shell.
- **The nginx fragment** (`sites/etc/nginx.conf`) is proven by a Go test that
  reads the file from disk and asserts its content (locations, `proxy_pass`
  targets, `auth_request` directives, correlation-header lines) — nginx itself
  is never run by the suite.
- **Doc-truth work** (if a brief's done bar is a text/grep check on `AGENTS.md`
  or similar rather than a Go test) is satisfied by editing the doc, not by
  adding a test.

## Boundaries

- Never read `project/design/*`, `project/plan/*`, or `project/product/*` — the
  brief is your only input. If it seems insufficient, do what it does support and
  report `NEXT`; gather will re-author it if the phase resets.
- Never edit `project/plan/STATUS.md` or delete a phase's line/body file — that is
  verify's sole right.
- Never delete or edit `project/loops/brief.md`, including its `## Verify feedback`
  region — you **read** the feedback but never write it.
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
  `Built internal/serve's confined static handler + 4 tagged tests; suite green.`

Always report **`NEXT`** — you hand off every turn. Keep `message` a single plain
sentence — not a JSON object or code block.
