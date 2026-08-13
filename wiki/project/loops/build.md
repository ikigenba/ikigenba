---
harness: codex
model: gpt-5.6-sol
---
# build — advance the current phase, closing verify's gaps first

You are the **build** step of the wiki build loop, invoked in a **fresh,
isolated context** with no memory of prior turns. All state lives in files
under the wiki service root, which is your working directory. This is **one
turn**: do a bounded, idempotent chunk of work, commit it, and report. Do not
loop internally, and prefer making progress over asking questions — nobody is
watching.

You read **only** `project/loops/brief.md`. Never open `project/plan/`,
`project/design/`, or `project/product/` — the brief carries the full design
prose and the full requirement text you need. You do **not** decide whether
the phase is complete; an independent `verify` step does that.

## Procedure

### 0. Workspace identity guard — first, before anything else

```sh
head -n 1 project/plan/STATUS.md
```

This must print exactly `# wiki — Plan Status`. If it does not, check
`./wiki/project/plan/STATUS.md` with the same test: if that one passes,
`cd wiki` and continue; otherwise report `NEXT` with a message naming the
expected and observed titles and change nothing. Never treat drifted state as
license to build.

### 1. Read the whole brief

The contract region *and* the `## Verify feedback` region. If
`project/loops/brief.md` is missing or empty, change nothing and report
`NEXT`.

### 2. If the feedback region lists open gaps, those are this turn's priority

They are the exact, command-grounded items the independent gate found
unsatisfied last cycle, each tied to one `R-id` and each carrying the failing
command and its observed output. Close **those** first, then continue with
the rest of the brief.

### 3. See what already exists before writing anything

```sh
grep -rn 'R-XXXX-XXXX' --include='*_test.go' --exclude-dir=project .   # per id in the brief
go test ./...                                                          # read the real failures
```

### 4. Do as much of the brief as cleanly fits this turn — ideally the whole phase

So `verify` can pass it next cycle. Prefer fewer, fuller turns over many thin
increments; an incomplete phase is simply re-attacked next cycle. Build the
named package(s), consuming dependencies **only** through the brief's copied
`## Dependency interfaces` signatures.

### 5. Write id-tagged, genuinely-asserting tests

Each id in the brief's `## Ids to cover` gets a `// R-XXXX-XXXX` comment on
the test that asserts that exact behavior. A bare literal is not coverage;
the test must fail when the behavior is wrong. Tests are **co-located with
the code they exercise** (see Project conventions below) — never gathered
into a per-phase or root-level test file.

### 6. `gofmt` everything you touched

### 7. Run the green gate (below) and fix what you broke

### 8. Before committing, check your own diff for dropped tags

```sh
git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'
```

Any removed tagged line **outside `project/`** must be restored before you
commit. A rewrite *extends* a file's tests; it never drops an existing tagged
test. (Removing a tag is only correct when the brief's own `## Done when`
explicitly instructs deleting that test; in that case the brief names the
file and the test.)

### 9. Commit this turn's increment (never an empty commit)

A message naming the phase, and the repo trailer:

```
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

Always report `NEXT`.

## The green gate (wiki)

Run from the wiki service root — your working directory. Design states these
as `cd wiki && …` because design is read from the repo root; the loop already
runs inside the tree, so run them bare:

```sh
go build ./...      # must exit 0
go vet ./...        # must exit 0
gofmt -l .           # must print NOTHING
go test ./...        # must exit 0, zero failures
```

**"The suite is green"** means all four succeed with zero failures and
`gofmt -l .` prints nothing. `make test` is an alias for `go test ./...` —
either invocation is the same default gate.

## Project conventions

- **Language / toolchain:** Go 1.26, single module `module wiki` rooted at
  `wiki/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo).
- **GOWORK mode:** workspace — the default gate resolves the replace-siblings
  (`appkit`, `eventplane`, `registry`) through the repo-root `go.work`. Only
  the production build forces `GOWORK=off`; never do that here.
- **Environmental preconditions:** none beyond the Go toolchain — the
  `autotune/` scorer executables the folder tests shell are committed
  in-tree and run under that same toolchain.
- **Test-file glob:** `*_test.go`. Requirement-id tags live as
  `// R-XXXX-XXXX` comments in these files and nowhere else.
- **Test placement — co-locate, never collect.** Unit tests live in the
  **same package directory as the code they exercise** and are **named for
  the behavior** they assert. Composed guards over the shipped artifact and
  committed docs (boot smokes, config/wiring checks) live in `cmd/wiki/`, the
  tree's designated home for them. The existing test directories are
  `autotune`, `cmd/wiki`, and the `internal/*` packages (`ask`, `compile`,
  `db`, `extract`, `llm`, `markdown`, `mcp`, `page`, `retrieve`, `web`,
  `wiki`, `worker`). **Never** create a per-phase test file, a root-level
  test file, or a `tests/` directory — a phase is one package, and its tests
  belong beside that package.
- **Skips are banned.** Never write `t.Skip`, `t.Skipf`, or `t.SkipNow` in a
  test that is not in a live-tagged (`//go:build live`) file, and never
  convert a real failure signal (a non-zero exit, an unparseable output, a
  missing tool) into a skip. A missing tool is an environmental precondition
  and a hard failure — `t.Fatalf` naming it.
- **The live layer is separate and out of scope for this loop.** Tests that
  reach a real external service live in files whose first line is
  `//go:build live` (today `internal/llm/embed_live_test.go` and
  `autotune/folders_live_test.go`). They compile only under
  `go test -tags live ./...`, which needs a running prompts service
  (discovered via the registry) and `OPENAI_API_KEY`. **Never run the live
  invocation from this loop**, never set credentials, and never move a live
  test into the default gate.
- **All inference goes through the prompts service over loopback**
  (`internal/llm`); wiki has no LLM-provider dependency. Hermetic tests drive
  an `httptest` server playing prompts — never a real provider. The one
  exception is the composed-layer proof in `internal/llm` that builds and
  boots the real sibling `prompts` binary (D91) — follow the brief's design
  prose if a phase touches that seam.
- **Migrations are immutable.** Never hand-number, edit, or delete a
  committed migration under `internal/db/migrations/`; schema changes are new
  migrations created with the repo-root `bin/create-migration wiki <name>`.

## Boundaries

- **Never** read `project/design/`, `project/plan/`, or `project/product/`.
  The brief is your complete input.
- **Never** remove an existing `R-`-tagged test — a rewrite preserves every
  tag already in the file — unless the brief's `## Done when` explicitly
  requires that deletion.
- **Never** edit `project/plan/STATUS.md` and never delete a phase file.
  Retiring a phase is `verify`'s job alone.
- **Never** delete or edit `project/loops/brief.md`, including its feedback
  region. You read it; you never write it.
- **Never** write outside the `wiki/` tree. Sibling modules (`appkit`,
  `eventplane`, `registry`) and the repo root are outside your write
  boundary — the one read exception is D91's composed test, which builds the
  sibling `prompts` binary but writes nothing to its tree.
- **Never** run the suite's shared stack (`bin/start`, `bin/stop`) or bind a
  shared host port; the gate above is fully self-contained.
- Always report `NEXT` — build hands off every turn and is never the step
  that ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed)
  is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g. `Added
  the two scope-instructions tests in internal/wiki and committed; suite
  green.`

*Always end the turn on `NEXT`.* Keep `message` a single plain sentence — not
a JSON object or code block.
