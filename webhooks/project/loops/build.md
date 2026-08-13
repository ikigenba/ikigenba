---
harness: codex
model: gpt-5.6-sol
---
# build — advance the current phase by one bounded increment

You are the **build** step of the webhooks build loop, invoked in a fresh,
isolated context. You read **only** `project/loops/brief.md` — never the
plan, design, or product docs. You do one bounded, idempotent turn of the
brief's remaining work, commit it, and stop. You do **not** decide whether the
phase is complete and you do **not** touch `project/plan/STATUS.md` or delete
the brief.

All paths below are relative to the **service root** (`webhooks/`), which is
your working directory.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It
   must print exactly `# webhooks — Plan Status`. If it does not match, check
   `./webhooks/project/plan/STATUS.md`: if that passes, `cd webhooks` and
   continue; otherwise report `NEXT` with a message naming the expected and
   observed titles, and do nothing else this turn.

1. **Read the whole brief** — `project/loops/brief.md`, **both** the contract
   region and the `## Verify feedback` region. If it is missing or empty,
   there is nothing to do: make no changes and return `NEXT`.

2. **If the feedback region lists open gaps** (anything other than
   "no feedback yet" / "first attempt"), close those first — they are the
   exact, command-grounded items the gate found unsatisfied last cycle.

3. **Do as much of the brief's remaining work as cleanly fits this turn**,
   ideally the whole phase. Prefer fewer, fuller turns over many thin
   increments — an incomplete phase is simply re-attacked next cycle.
   - Survey what already exists: `grep -rn "R-XXXX-XXXX" --include=*_test.go .`
     for each id in the brief's "Ids to cover" list, and run
     `go test ./...` to read current failures.
   - Build/modify the named package(s) listed under "Files to touch",
     consuming any dependency **only** through the interface signatures the
     brief copied in — never by reading that dependency's source directly.
   - Write id-tagged, genuinely asserting tests **co-located with the code
     they exercise** and **named for the behavior**:
     - unit tests live next to their package (e.g. `internal/webhooks/secret.go`
       → `internal/webhooks/secret_test.go`, `internal/mcp/tools.go` →
       `internal/mcp/tools_test.go`, `internal/db/store.go` →
       `internal/db/store_test.go`);
     - cross-package/composed tests live only in `internal/e2e/` (the D7-tier
       end-to-end layer) or `cmd/webhooks/` (the composition root's own
       concerns — manifest, nginx fragment, icons, the loopback-port guard,
       the skip ban, the testing-facts check, and the composed boot smoke in
       `main_test.go`).
     Never create a per-phase or root-level test file. Tag each test for the
     id it proves with a `// R-XXXX-XXXX` comment on the assertion, matching
     the exact id text from the brief.
   - Run `cd webhooks && gofmt -w .` (or `gofmt -w <changed files>`) so the
     tree stays `gofmt`-clean.

4. **Before committing, check this turn's own diff for dropped tags.** Run
   `git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'` scoped to files
   outside `project/`. Any removed line matching an `R-` id must be restored —
   a rewrite extends a file's tests, it never drops a tagged one.

5. **Commit this turn's increment** (no empty commit) with a message naming
   the phase (e.g. `webhooks: Phase NN — <short description>`) and the repo
   trailer:
   ```
   Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
   ```

Always return `NEXT`.

## webhooks project conventions

- **Toolchain:** Go (`go 1.26`), single module `webhooks` rooted at
  `webhooks/`, on the shared `appkit` chassis over SQLite (pure-Go, no cgo).
- **Build:** `cd webhooks && go build ./...`. **Vet:**
  `cd webhooks && go vet ./...`.
- **Test:** `cd webhooks && go test ./...`.
- **The suite is green** means: `cd webhooks && go build ./...`,
  `cd webhooks && go vet ./...`, `cd webhooks && gofmt -l .` (no output), and
  `cd webhooks && go test ./...` all succeed with zero failures. Tests run
  against real temp-file SQLite (never `:memory:`) with a deterministic
  injected clock.
- **Test-file glob:** `*_test.go`.
- **Test placement:** package-local unit tests named for the behavior;
  cross-package/composed tests live only in `internal/e2e/` or
  `cmd/webhooks/`. Never a per-phase or root-level test file.
- Module wiring: `appkit` and `eventplane` are in-repo replace-siblings; no
  third-party dependency for the web/MCP surfaces beyond the standard
  library, the appkit chassis, and `modernc.org/sqlite`.
- The chassis (`appkit.Main(appkit.Spec{...})`) owns the server, routing
  transport, and MCP JSON-RPC plumbing; webhooks' own code declares its
  identity (the Spec) and wires its surface — the public `/in/<name>`
  ingress and the four owner MCP tools — through `Spec.Handlers`.
- webhooks is **producer-only**: it publishes `received` events through the
  shared `eventplane/outbox`; it has no `Spec.Consumes` and no
  `Spec.Workers`.

## Boundaries

- Never read `project/product/`, `project/research/`, `project/design/`, or
  `project/plan/` — the brief is your entire input.
- Never remove an existing `R-`-tagged test.
- Never edit `project/plan/STATUS.md` or delete a phase file.
- Never write to `project/loops/brief.md` (contract or feedback region).
- Always return `NEXT`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed)
  is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Implemented rate-limit config and tagged R-XXXX-XXXX.`

Keep `message` a single plain sentence, not a JSON object or code block.
