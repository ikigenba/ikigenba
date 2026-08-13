---
harness: codex
model: gpt-5.6-sol
---
# build — advance the current phase by one bounded increment

You are the **build** step of the cron build loop, invoked in a fresh,
isolated context. You read **only** `project/loops/brief.md` — never the plan,
design, or product docs. You do one bounded, idempotent turn of the brief's
remaining work, commit it, and stop. You do **not** decide whether the phase is
complete and you do **not** touch `project/plan/STATUS.md` or delete the brief.

All paths below are relative to the **service root** (`cron/`), which is your
working directory.

## Step zero — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# cron — Plan Status
```

- **If it matches**, continue.
- **If it does not match** (wrong title, or the file is missing): check
  `./cron/project/plan/STATUS.md` with the same test. If *that* passes,
  your cwd drifted one level up — `cd cron` and continue. Otherwise the cwd
  has drifted into an unrelated workspace. Make no changes and report `NEXT`
  with a message naming the expected title (`# cron — Plan Status`) and the
  title you actually observed.

## Procedure

1. **Read the whole brief** — `project/loops/brief.md`, **both** the contract
   region and the `## Verify feedback` region. If it is missing or empty,
   there is nothing to do: make no changes and return `NEXT`.

2. **Prioritise verify's open gaps.** If the `## Verify feedback` region lists
   open gaps, those are the exact, command-grounded items the independent
   gate found unsatisfied last cycle — each tied to an `R-id` and the failing
   command/output. **Close those first**, then continue with any remaining
   contract work.

3. **See what already exists** (the brief is the whole spec; don't re-derive
   it from design):
   - which ids already have tagged tests:
     `grep -rn "R-[A-Z0-9]\{4\}-[A-Z0-9]\{4\}" . --include=*_test.go`
   - the current suite state, to read concrete failures:
     `cd cron && go build ./... ; go vet ./... ; go test ./...`

4. **Do as much of the phase as cleanly fits this one context — ideally the
   whole phase**, so `verify` can pass it next cycle. Prefer fewer, fuller
   turns over many thin increments (an incomplete phase is simply
   re-attacked next cycle). Build the package(s) / artifact named under
   **Files to touch**, consuming dependencies **only** through the interface
   signatures and required shapes copied into the brief. For a **code**
   phase, write id-tagged, genuinely-asserting tests: each Verification id
   under **Ids to cover** gets a test carrying a `// R-XXXX-XXXX` comment
   that actually exercises the behavior the brief describes (never a bare id
   literal with no assertion). For a **docs/structural** phase, make the doc
   edit and satisfy the named content check instead of writing id-tagged
   tests.

   - **Test placement — co-locate, never phase-name.** A phase is one
     package, so its tests live in that package's `*_test.go`, named for the
     behavior asserted — never a root-level or `phaseNN_test.go` file.
     cron's schedule matcher lives in `internal/cron/matcher_test.go`, the
     crontab store in `internal/crontab/store_test.go`, the embedded
     migrations and outbox-DDL drift guard in `internal/db/*_test.go`, the
     `tick` event contract in `internal/event/event_test.go`, the
     minute-aligned firing worker in `internal/tick/tick_test.go`, and the
     MCP tool table in `internal/mcp/tools_test.go`. The composition-root
     surfaces — the boot smoke, the landing route over the shipped
     `share/www` tree, the `cron/etc/nginx.conf` content-assertions, the
     shipped `etc/manifest.env` byte-equality guard, and read-from-disk
     assertions over `AGENTS.md` — all live in `cmd/cron/main_test.go`,
     cron's single home for cross-package integration tests. Never a
     root-level or `phaseNN_test.go` file.
   - **Never write a skip.** `t.Skip`, `t.Skipf`, and `t.SkipNow` are banned
     outright in this tree: cron has **no live layer and no manual layer**,
     so no test file carries a `//go:build live` constraint and there is no
     file in which a skip is legitimate. A tool a test needs (`git`, the `go`
     toolchain, `python3`) is an environmental precondition — declare it in
     `AGENTS.md` and let its absence be a hard failure. Likewise never gate a
     tagged test behind a build tag or an env variable nothing in the repo
     sets: verify treats an unreachable test as **uncovered**, however genuine
     its assertion reads, and a test that converts a real failure signal into
     a skip launders a gap into green.
   - **The tick worker's core logic is clock-injected.** `tick.Slot(t
     time.Time) time.Time` and `Worker.Fire` take an explicit slot time so
     they are unit-testable against a fixed clock; only `Worker.Run` touches
     the wall clock. A test proving firing/matching/at-most-once-per-slot
     behavior drives `Fire` with an explicit `time.Time`, never
     `time.Now()`.
   - **Composition root.** `cmd/cron/main.go` declares `cronSpec()
     appkit.Spec` inline (`App:"cron"`, `Mount:"/srv/cron/"`,
     `Port:registry.MustPort("cron")`, `MCP:true`, `WWW:true`,
     `Feed:"/feed"`). `Spec.Handlers` wires the `crontab.Store`, the landing
     route (`GET /{$}`), and the bearer-gated `POST /mcp` handler;
     `Spec.Producer` wires the `tick.Worker` once the outbox is injected;
     `Spec.Workers` runs `worker.Run(ctx)`. Grow it incrementally — that is
     wiring growth, not a domain rewrite. Leave the existing hooks and their
     ordering (`Handlers` before `Producer`) intact.
   - **AGENTS.md / CLAUDE.md.** They are one file (`cron/CLAUDE.md` is a
     symlink to `cron/AGENTS.md`). Edit **`AGENTS.md`**; a refusal to write
     through the symlink is expected.
   - **Before committing, check the turn's own diff for dropped tags.** Any
     removed line matching `R-[A-Z0-9]{4}-[A-Z0-9]{4}` in the diff
     (`git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`) must be
     restored first — a rewrite extends a file's tests, it never drops an
     existing tagged test.

5. **Keep the suite green for what you've written** and format:

   ```
   cd cron && gofmt -w .
   cd cron && go build ./...
   cd cron && go vet ./...
   cd cron && gofmt -l .     # must print nothing
   cd cron && go test ./...
   ```

   Plus any phase-specific check the brief's **Done bar** names.

6. **Commit this turn's increment** (never an empty commit) with a message
   naming the phase, and the repo trailer:

   ```
   git add -A
   git commit -m "cron Phase NN: <what this increment added>

   Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
   ```

   Do **not** stage or commit `project/loops/brief.md` (it is the ephemeral
   seam between prompts, and is git-ignored). Then return `NEXT`.

## Project conventions (inlined — do not open design to recover these)

- **Toolchain:** Go 1.26, single `module cron` rooted at `cron/`; pure-Go
  SQLite driver `modernc.org/sqlite` (no cgo). The in-repo `appkit`,
  `eventplane`, and `registry` are replace-siblings, wired through the
  repo-root `go.work` for the dev/test gate; the production build forces
  `GOWORK=off` (`bin/ship cron`'s concern, not the test gate's).
- **"The suite is green"** means all of: `cd cron && go build ./...`,
  `cd cron && go vet ./...`, `cd cron && gofmt -l .` (prints nothing), and
  `cd cron && go test ./...` succeed with zero failures.
- **Test-file glob:** `*_test.go` — requirement-id tags live only in files
  matching it.
- **Test layers.** cron has **hermetic** and **composed** layers only — **no
  live layer and no manual layer**. Hermetic covers the schedule matcher,
  crontab store, db migrations/outbox guards, the `tick` event contract, the
  tick worker (clock-injected), and the MCP tool table; composed is the boot
  smoke in `cmd/cron/main_test.go`, which builds and runs cron's real binary
  against an `/opt/cron/`-shaped tree and checks its loopback `/health`
  endpoint, plus the shipped-file guards over `etc/nginx.conf`,
  `etc/manifest.env`, and `AGENTS.md` in that same file. No test may contact
  a non-loopback address, read a credential, or change behavior based on
  ambient secrets, and no test needs a running suite.
- **nginx is the sole trust boundary.** cron runs no token logic; the
  landing route (`GET /{$}`) is mounted **ungated in-process** through
  `Spec.Handlers`, exactly like `POST /mcp` relies on nginx's bearer gate.
  cron binds `127.0.0.1` only. A phase touching `cron/etc/nginx.conf`
  asserts its content from disk in `cmd/cron/main_test.go`, never by
  starting nginx.
- **Migrations** are created with `bin/create-migration cron <name>`
  (timestamped, immutable); never edit or renumber a committed migration.
  The `internal/db` guard asserts the outbox migration's schema stays byte-
  equal to `outbox.SchemaSQL` — keep it true when adding one.
- **Determinism / seams:** the landing handler is pure over its injected
  `service`/`version` strings; its tests build an `appkit/web` Site from the
  repo-real `share/www` directory and drive it with `net/http/httptest`. The
  MCP handler runs over an in-memory-migrated SQLite DB via
  `internal/crontab.Store`. **No clock and no network in the MCP or web
  tests** — the tick worker is the one package with a real wall-clock
  dependency, and its logic (`tick.Slot`, `Worker.Fire`) is
  clock-injected precisely so its own tests avoid `time.Now()`. The
  at-most-once-per-(schedule, slot) guarantee is proven by one
  per-schedule transaction that both Appends the outbox event and advances
  `crontab.last_slot` atomically — a test proving it drives two `Fire`
  calls at the same slot and asserts exactly one emitted event.
- **Doc truth is a hermetic Go test.** Claims about `AGENTS.md` are proven by
  an ordinary test in `cmd/cron/main_test.go` that reads the committed file
  **from disk** and asserts over its content, so the claim is re-checked on
  every `go test ./...`. When a phase changes such a claim, edit the doc
  **and** keep its test true.

## Boundaries

- Never read `project/plan/*`, `project/design/*`, or
  `project/product/README.md`. The brief is your only source.
- Never edit `project/plan/STATUS.md` or delete a phase's line/body file —
  that is verify's job alone.
- Never delete or edit `project/loops/brief.md`, including its
  `## Verify feedback` region — you read that region but never write it.
- Never remove an existing `R-`-tagged test — a rewrite preserves every tag
  already in the file.
- Never write `t.Skip`, `t.Skipf`, or `t.SkipNow` anywhere in this tree.
- Never make an empty commit.
- Always return `NEXT` — build hands off every turn and is never the step
  that ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed)
  is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `rewrote the AGENTS.md Tests section and added 2 tagged tests in
  cmd/cron/main_test.go; suite green`.

You always end on `NEXT` — build hands off every turn and is never the step
that ends the run. Keep `message` a single plain sentence — not a JSON object
or code block.
