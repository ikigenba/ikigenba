---
harness: claude
model: claude-sonnet-5
---

# gather — select the next ⬜ phase and author its brief

You run in a fresh, isolated context, one turn per invocation, as the first step
of an unattended `gather → build → verify` loop that builds the dashboard one
phase at a time. `ralph` runs from the service root (`dashboard/`), so every path
below is service-root-relative.

You are the **only** prompt that reads the big spec docs, and the **only**
prompt that ever ends the run. Your job is to make sure `project/loops/brief.md`
holds a correct, self-contained contract for the **first unstarted phase** —
or, when there is none (or the loop is blocked), to report `DONE`.

## Step 0 — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# dashboard — Plan Status (web surface & sign-in)
```

- If the file is missing or the line differs, do **not** proceed and never
  report `DONE`. Check whether `./dashboard/project/plan/STATUS.md` passes the
  same check: if it does, your cwd drifted one level up (likely the repo
  root's own `project/`) — `cd dashboard` and retry step 0 in this same turn.
  Otherwise report `NEXT` with a message naming the expected title
  (`# dashboard — Plan Status (web surface & sign-in)`) and what you actually
  saw, so the drift is visible instead of silently ending or misdirecting the
  run.

## Step 1 — check for a block

If `project/loops/blocked.md` exists, open no other file. Report `DONE` with a
message naming the blocked phase and pointing at `project/loops/blocked.md`.

## Step 2 — find the next pending phase

Run:

```
grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
```

- **No match** — there is no pending work. Report `DONE` with a message like
  "no pending phases — dashboard/project/plan/STATUS.md carries no ⬜ line".
- **Match** — note the phase number `NN`.

## Step 3 — is there already a brief for this phase?

Check whether `project/loops/brief.md` exists. If it does, read only its first
line, the `# Brief — Phase NN` header.

- **Same phase number as step 2** — the phase is already mid-flight. Leave the
  brief exactly as it is (both the contract region and the verify-feedback
  region untouched), open no other file, and report `NEXT`.
- **No brief, or the brief names a phase number with no corresponding
  `STATUS.md` line** (that phase completed and was deleted) — continue to
  step 4 and author a fresh brief.

## Step 4 — author the brief

Read **only** what this phase needs:

1. `project/plan/phase-NN.md` — the one phase body file named in step 2.
2. The `DNN.md` file(s) named on its `*Realizes design Decision …*` line —
   resolve via `project/design/INDEX.md`'s `## Decisions` table if the phase
   body doesn't spell out the file path directly.
3. The public interface signatures of any package(s) the phase depends on
   (read just enough of the dependency's `.go` files to copy exported
   signatures — never its internals).

From these, write `project/loops/brief.md` with this exact schema:

```
# Brief — Phase NN

## Contract (gather-owned — verify never writes here)

### Objective
<one line: what this phase builds, from phase-NN.md>

### Realizes
D<n> — <title> (project/design/D<NN>.md)
[D<m> — <title> (project/design/D<MM>.md)]

### Design prose (verbatim, Verification list omitted)
<full "Decision." and "Rejected." prose of each realized Decision, copied
verbatim from its DNN.md. Never copy the Verification list itself — only the
ids below name what this phase owns.>

### Ids to cover
R-XXXX-XXXX — <full requirement text copied verbatim from the Verification list>
R-XXXX-XXXX — <full requirement text copied verbatim from the Verification list>
[... one line per id the phase body/Done-when names — never the Decision's
full id set, only this phase's slice. If the phase is structural and owns no
ids, write exactly: (none — structural phase)]

### Files to touch
<paths named or implied by phase-NN.md>

### Dependency interfaces
<exported signatures of packages this phase consumes, copied verbatim — never
full implementations>

### Done bar
- `cd dashboard && go build ./...` and `go vet ./...` succeed
- `gofmt -l .` (run from `dashboard/`) prints nothing
- `cd dashboard && go test ./...` is all green, and no `R-XXXX-XXXX`-tagged
  test reports SKIP
- Every id listed above is covered by a genuinely asserting `// R-XXXX-XXXX`
  tagged test, co-located with the code it exercises in the package under
  test (a `*_test.go` file next to the source, named for the behavior) —
  never gathered into a per-phase or root-level test file. Cross-package
  integration/composed checks belong in `cmd/dashboard/main_test.go`, the
  suite's one named composed-test home; there is no separate root-level
  test file otherwise.
- A structural phase (no ids) is proven instead by: green build/vet/gofmt and
  the exact check phase-NN.md's own "Done when" names.

## Verify feedback — attempt 0

(empty — no attempts yet)
```

Report `NEXT`.

## Boundaries

- Read only: the one phase file, its realized Decision file(s), and the
  interface signatures of packages it depends on. Never open any other
  `phase-*.md`, any other `D*.md`, `CONVENTIONS.md`, `product/README.md`, or
  `research/research.md`.
- Never build, run tests, or commit.
- Never write the brief's `## Verify feedback` region — that is verify's
  region alone. On a no-op for an in-flight phase, leave the entire brief
  untouched.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next
  prompt.
- `DONE` — **terminal**: tells `ralph` to stop the loop. It carries no other
  meaning; say *why* in the message, e.g. `no pending phases remain` or
  `blocked on Phase 12 — see project/loops/blocked.md`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Authored brief for Phase 54 (D42).`

Report `DONE` only in step 1 (a block exists) or step 2 (no pending phase);
every other path ends on `NEXT`. Keep `message` a single plain sentence, not a
JSON object or code block.
