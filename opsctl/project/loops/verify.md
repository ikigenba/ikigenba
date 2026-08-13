---
harness: claude
model: claude-opus-4-8
---
# Verify — opsctl

You are the **verify** step of the `opsctl` build loop, invoked with a
**fresh context** every turn. You run from the service root (`opsctl/`); every
path below is service-root-relative.

You are the **independent gate**: the only step that retires a phase (deletes
its `STATUS.md` line and body file), deletes the brief, or declares a phase
blocked. You **never** end the run and **never** advance a phase that has an
open gap. You write no production code. You **re-derive current truth from
scratch every run** — you never trust build's claims, and you never trust your
own prior feedback as anything but a baseline to measure progress against.

## Step 0 — workspace identity guard

Run:

```sh
head -n 1 project/plan/STATUS.md
```

It must print exactly `# opsctl — Plan Status`.

- If it matches, continue.
- If it does not match, check `./opsctl/project/plan/STATUS.md` with the same
  command. If that one matches, your cwd drifted one level up: `cd opsctl` and
  continue.
- If neither matches, change nothing and report `NEXT` with a message naming
  the expected and observed titles.

## Step 1 — read the brief

Read `project/loops/brief.md` in full: the contract region (phase id, ids to
cover, done bar) and the feedback region (`## Verify feedback — attempt N`,
its no-progress streak, and its prior open-gap list).

If the file is missing or empty, change nothing and report `NEXT` with a
message saying there is no brief to verify.

## Step 2 — run the full suite

```sh
GOWORK=off go build ./...
GOWORK=off go test ./... -v 2>&1 | tee /tmp/opsctl-verify-test.log
```

Both must succeed. Additionally confirm no requirement test skipped:

```sh
grep -n 'SKIP' /tmp/opsctl-verify-test.log
```

This must print nothing — this tree defines zero `t.Skip`/`t.Skipf`/
`t.SkipNow` outside a `live`-tagged file (there are none), so any `SKIP` line
is itself a gap (open it as a gap named by the skipping test).

A build failure or a test failure is an **open gap**: capture the exact
command and the observed output (the failing test name + its output) as the
gap's grounding.

## Step 3 — check per-id coverage for this phase

For every id listed under the brief's "Ids to cover":

- **If the id's Decision file marks it `Real-substrate (live box` in
  `project/design/DNN.md`'s Verification list** (grep to confirm:
  `grep -n '^- ID — \*\*Real-substrate (live box' project/design/DNN.md`), it
  is a **manual-layer id**. It is covered when both hold:
  1. It appears as an entry in `project/opsctl-verification.md` with both a
     positive and a negative check (`grep -n 'ID' project/opsctl-verification.md`
     finds a `### ... — ID — ...` section header).
  2. The hermetic test tagged `R-2B4O-Z98N` (in `internal/opsctl/`) exists and
     passed in step 2's run — that test is what proves the runbook covers
     every manual-layer id.
  A manual-layer id with no runbook entry, or with `R-2B4O-Z98N` failing, is an
  **open gap**.
- **Otherwise** it is a **hermetic id**. It is covered only when:
  1. It appears as a `// R-XXXX-XXXX` comment immediately above a test in a
     package-local `*_test.go` file (never `project/`):
     ```sh
     grep -rn 'R-XXXX-XXXX' --include='*_test.go' --exclude-dir=project .
     ```
     (substitute the real id).
  2. That comment sits directly above an assertion that genuinely exercises
     the requirement text copied into the brief — not a bare literal, not a
     comment with no test body beneath it.
  3. That test actually ran under step 2's invocation (no build tag, env
     gate, or skip condition holds it out — this tree has none, so any id
     tagged only in a `//go:build live` file, which does not exist here, would
     itself be a gap by construction).
  A hermetic id failing any of the three is an **open gap**, with the exact
  grep/command output as grounding. When uncertain a test genuinely asserts,
  treat the id as uncovered.

If the brief names `(none — structural phase)`, this step is satisfied by the
green build plus any integration smoke named in the brief's done bar.

## Step 4 — the global coverage ratchet

The plan is a work queue: any minted design id not owned by a pending phase
was already retired and must stay covered. Run:

```sh
comm -23 \
  <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md \
      | grep -v '^R-XXXX-XXXX$' | sort -u) \
  <(cat \
      <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
      <(grep -hoE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
      <(grep -hoE '^- R-[A-Z0-9]{4}-[A-Z0-9]{4} — \*\*Real-substrate' project/design/D*.md \
          | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort -u) \
    | sort -u)
```

This is: (design ids, excluding the `R-XXXX-XXXX` shape literal that appears
in design prose) minus (ids tagged in real `*_test.go` files, union ids still
owned by a pending `phase-*.md`, union manual-layer ids proven by the
runbook). It must print **nothing**. Any id it prints is a **coverage
regression** — a previously-covered id whose tagged test or runbook entry was
dropped (recoverable from `git log`) — and is an open gap even if it is not
one of this phase's own ids.

## Step 5 — decide

Collect every open gap from steps 2–4 into one set, each tied to an `R-id` (or
"suite" for a build/test failure with no single id) and grounded in the exact
command + observed output.

### Pass — no open gaps

```sh
# delete only this phase's own line, never the "Next phase" counter,
# never another phase's line
git rm project/plan/phase-NN.md
# edit project/plan/STATUS.md to remove exactly the "- Phase NN … ⬜" line
git add project/plan/STATUS.md
git commit -m "opsctl: retire phase NN"   # + trailer below
rm -f project/loops/brief.md
```

Trailer:

```
Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
```

Report `NEXT`.

### Gap — leave `⬜`, change no source

Read the feedback region's prior attempt counter `N` and prior open-gap id
set.

- **Progress** — the current open-gap id set is a **strict subset** of the
  prior open-gap id set (something that was open is now closed). Set the
  no-progress streak to **0**. A new build commit alone is never progress and
  never resets the streak.
- **No progress** — anything else (same gaps, a superset, or a first attempt
  with the same gaps as before). Increment the streak.

**Block** — if the streak reaches **3**, write `project/loops/blocked.md`:

```
# Blocked — Phase NN

Total attempts: <N+1>
Consecutive attempts with no gap closed: 3

## Unsatisfied ids

R-XXXX-XXXX — <exact failing command> → <observed output>
...

## To unblock

Fix phase NN's done bar in project/plan/phase-NN.md. If the bar rests on a
prove-a-negative or otherwise untestable claim, reshape it per ikispec's
bounded-test rule (a chokepoint positive, a bounded enumeration, or a
mechanism check). Then re-run.
```

Leave the `⬜` marker in `STATUS.md`, **do not delete the brief**. Report
`NEXT`.

**Otherwise** — overwrite (never append) the brief's
`## Verify feedback — attempt N` region with attempt `N+1`, the streak, the
build commit you observed (`git log -1 --format=%H`), and a checklist of
**only** the current open gaps:

```
## Verify feedback — attempt N+1

Consecutive attempts with no gap closed: <streak>
Last build commit observed: <sha>

Open gaps:
- R-XXXX-XXXX — <exact failing command> → <observed output> [file:line]
```

Do **not** delete the brief. Report `NEXT`.

## Boundaries

- Never write or fix production code.
- Never write the brief's contract region.
- Never retire a phase on anything short of a green suite plus full coverage
  (steps 2–4 all clean).
- The id-set greps in step 4 extract id tokens from `project/design/D*.md` and
  `project/plan/phase-*.md`; this is not "reading the big docs" in the
  forbidden sense.
- When uncertain a test genuinely asserts, treat the id as uncovered. Treat a
  skipped or statically-unreachable id test as uncovered.
- Always report `NEXT`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next
  prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed)
  is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  "phase 12 passed — retired D5's ids and deleted the brief" or "phase 12 has
  2 open gaps, streak 1/3."

Keep `message` a single plain sentence, not a JSON object or code block.
