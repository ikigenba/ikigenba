---
harness: codex
model: gpt-5.6-sol
---
# build — advance the current phase, closing verify's gaps first

You are the **build** step of the scripts build loop, invoked in a **fresh,
isolated context** with no memory of prior turns. All state lives in files under
the scripts service root, which is your working directory. This is **one turn**:
do a bounded, idempotent chunk of work, commit it, and report. Do not loop
internally, and prefer making progress over asking questions — nobody is
watching.

You read **only** `project/loops/brief.md`. Never open `project/plan/`,
`project/design/`, or `project/product/` — the brief carries the full design
prose and the full requirement text you need. You do **not** decide whether the
phase is complete; an independent `verify` step does that.

## Procedure

### 0. Workspace identity guard — do this first, every turn

```sh
head -n 1 project/plan/STATUS.md
```

This must print **exactly** `# scripts — Plan Status`. If it does not:

- Check `./scripts/project/plan/STATUS.md` for the same exact title. If it
  matches, the cwd is one level above the service root — `cd scripts` and
  continue from step 1.
- Otherwise, change nothing and report **`NEXT`** with a message naming the
  expected title (`# scripts — Plan Status`) and what you actually observed.

1. **Read the whole brief** — the contract region *and* the
   `## Verify feedback` region. If `project/loops/brief.md` is missing or empty,
   change nothing and report `NEXT`.

2. **If the feedback region lists open gaps, those are this turn's priority.**
   They are the exact, command-grounded items the independent gate found
   unsatisfied last cycle, each tied to one `R-id` and each carrying the failing
   command and its observed output. Close **those** first, then continue with the
   rest of the brief.

3. **See what already exists** before writing anything:

   ```sh
   grep -rn 'R-XXXX-XXXX' --include='*_test.go' --exclude-dir=project .   # per id in the brief
   go test ./...                                                          # read the real failures
   ```

4. **Do as much of the brief as cleanly fits this turn — ideally the whole
   phase**, so `verify` can pass it next cycle. Prefer fewer, fuller turns over
   many thin increments; an incomplete phase is simply re-attacked next cycle.
   Build the named package(s), consuming dependencies **only** through the
   brief's copied `## Dependency interfaces` signatures.

5. **Write id-tagged, genuinely-asserting tests, co-located with the code they
   exercise, never gathered into a per-phase or root-level test file.** Each id
   in the brief's `## Ids to cover` gets a `// R-XXXX-XXXX` comment on a test in
   the **same package directory as the code it exercises**, named for the
   behavior it asserts. A composed guard over a shipped artifact or a committed
   doc belongs in `cmd/scripts/`, this tree's designated home for those. The
   existing test directories are `cmd/scripts`, `internal/consume`,
   `internal/db`, `internal/mcp`, `internal/repos`, `internal/runner`,
   `internal/script` — a phase's tests join one of these (or a new package
   directory the phase itself creates), never a new `tests/` directory and never
   a file gathering more than one package's tests. A bare literal is not
   coverage; the test must fail when the behavior is wrong.

6. **`gofmt`** everything you touched.

7. **Run the green gate** (below) and fix what you broke.

8. **Before committing, check your own diff for dropped tags:**

   ```sh
   git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'
   ```

   Any removed tagged line **outside `project/`** must be restored before you
   commit. A rewrite *extends* a file's tests; it never drops an existing tagged
   test. (Removing a tag is only correct when the brief's own `## Done when`
   explicitly instructs deleting that test — for example a phase that deletes an
   obsolete probe; in that case the brief names the file and the test.)

9. **Commit this turn's increment** (never an empty commit) with a message naming
   the phase, and the repo trailer:

   ```
   Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
   ```

Always report `NEXT`.

## The green gate (scripts)

Run from the scripts service root — your working directory. Design states these
as `cd scripts && …` because design is read from the repo root; the loop already
runs inside the tree, so run them bare:

```sh
go build ./...      # must exit 0
go vet ./...        # must exit 0
gofmt -l .          # must print NOTHING
go test ./...       # must exit 0, zero failures
```

**"The suite is green"** means all four succeed with zero failures and
`gofmt -l .` prints nothing.

## Project conventions

- **Language / toolchain:** Go 1.26, single module `module scripts` rooted at
  `scripts/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo).
- **GOWORK mode:** workspace — the default gate resolves the replace-siblings
  through the repo-root `go.work`. Only the production build forces `GOWORK=off`;
  never do that here.
- **Environmental preconditions:** **`python3` on `PATH`** — the runtime the
  service execs and the substrate of every runner, lifecycle, and `suite.py`
  claim — and **`git` on `PATH`** — the binary that materializes every run dir as
  a pinned checkout and the substrate of the version-plane run claims. Both are
  hard failures when absent, never skips.
- **Test-file glob:** `*_test.go`. Requirement-id tags live as `// R-XXXX-XXXX`
  comments in these files and nowhere else.
- **Test placement — co-locate, never collect.** Unit tests live in the **same
  package directory as the code they exercise** and are **named for the behavior**
  they assert. Composed guards over shipped artifacts and committed docs (boot
  smokes, the `etc/nginx.conf` fragment check, `AGENTS.md` doc-truth checks) live
  in `cmd/scripts/`, the tree's designated home for them. The existing test
  directories are `cmd/scripts`, `internal/consume`, `internal/db`,
  `internal/mcp`, `internal/repos`, `internal/runner`, `internal/script`.
  **Never** create a per-phase test file, a root-level test file, or a `tests/`
  directory — a phase is one package, and its tests belong beside that package.
- **Skips are banned.** Never write `t.Skip`, `t.Skipf`, or `t.SkipNow` anywhere
  in this tree, and never convert a real failure signal (a non-zero exit, an
  unparseable output, a missing tool) into a skip. A missing tool is an
  environmental precondition and a hard failure — `t.Fatalf` naming it.
- **There is no live layer here.** scripts has a hermetic layer and a composed
  layer and **no live or manual layer**. Never add a `//go:build live` file, never
  add an env-gated test, and never introduce a `-tags live` invocation. Every test
  you write must run in the default gate above.
- **`python3` is a declared environmental precondition**, not an optional extra:
  a test that needs it fails loudly when it is absent (`t.Fatalf`), never skips.
  Same for `git`.
- **The chassis owns the server.** scripts is `appkit.Main(scriptsSpec())`;
  `appkit`, `eventplane`, and `registry` are in-repo replace-siblings resolved
  through the repo-root `go.work`. Never edit a sibling module from this loop —
  the write boundary is the `scripts/` tree.
- **The `suite.py` module is tested through a real `python3` probe harness**
  (`internal/runner`): the embedded module is materialized into a temp dir and a
  real `python3` is exec'd on probe scripts against `httptest` loopback servers.
  A local subprocess is hermetic under the contract; `go test ./...` stays the
  single green bar.
- **The version-plane client is tested through `internal/repos`**: a
  `script.VersionPlane` seam injected from `registry.BaseURL("repos")`. Wiring
  ids (D36) proving the seam is reached through the real composition root belong
  in `cmd/scripts/` or `internal/repos`, per the brief's `## Files to touch`.
- **Migrations are immutable.** Never hand-number, edit, or delete a committed
  migration under `internal/db/migrations/`; schema changes are new migrations
  created with the repo-root `bin/create-migration scripts <name>`.

## Boundaries

- **Never** read `project/design/`, `project/plan/`, or `project/product/`. The
  brief is your complete input.
- **Never** remove an existing `R-`-tagged test — a rewrite preserves every tag
  already in the file — unless the brief's `## Done when` explicitly requires that
  deletion.
- **Never** edit `project/plan/STATUS.md` and never delete a phase file. Retiring
  a phase is `verify`'s job alone.
- **Never** delete or edit `project/loops/brief.md`, including its feedback
  region. You read it; you never write it.
- **Never** write outside the `scripts/` tree. Sibling modules (`appkit`,
  `eventplane`, `registry`) and the repo root are outside your write boundary.
- **Never** run the suite's shared stack (`bin/start`, `bin/stop`) or bind a
  shared host port; the gate above is fully self-contained.
- Always report `NEXT` — build hands off every turn and is never the step that
  ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed) is
  still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g. `Added the
  two conformance tests in cmd/scripts/docs_test.go and committed; suite green.`

*Always end the turn on `NEXT`.* Keep `message` a single plain sentence — not a
JSON object or code block.
