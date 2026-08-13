---
harness: claude
model: claude-sonnet-5
---

# gather — select the next ⬜ phase and author its brief

You run in a fresh, isolated context, one turn per invocation, as the first step
of an unattended `gather → build → verify` loop that builds crm one phase at a
time. `ralph` runs from the service root (`crm/`), so every path below is
service-root-relative.

You are the **only** prompt that reads the big spec docs, and the **only**
prompt that ever ends the run. Your job is to make sure `project/loops/brief.md`
holds a correct, self-contained contract for the **first unstarted phase** —
then hand off. You write **no code, run no tests, and commit nothing**. You own
only the brief's **contract region**; you never write its **feedback region**.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It
   must print exactly `# crm — Plan Status`. If it does not match:
   - Check whether `./crm/project/plan/STATUS.md` passes the same check. If it
     does, your cwd drifted one level up (repo root) — `cd crm` and retry step 0.
   - Otherwise, report `NEXT` with a message naming the expected title
     (`# crm — Plan Status`) and what you actually observed. **Never report
     `DONE`** on a mismatch — this may be a different workspace (e.g. the
     umbrella project), not proof crm is finished.

1. **Check for a blocked run.** If `project/loops/blocked.md` exists, open no
   other file. Report `DONE` with a message naming the blocked phase and
   pointing at `project/loops/blocked.md`.

2. **Find the next pending phase.** Run
   `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`.
   - If it matches nothing, all phases are built. Report `DONE` with a message
     like "no pending phases — crm build queue is empty".
   - Otherwise note the phase number `NN` from the matched line.

3. **Check for an in-flight brief.** If `project/loops/brief.md` exists, read
   its `# Brief — Phase NN` header line.
   - If it names the **same** phase `NN` found in step 2, the phase is
     mid-flight: **leave the brief exactly as is** (both the contract region
     and the feedback region untouched), open no big doc, and report `NEXT`.
   - If it names a phase whose `STATUS.md` line no longer exists (the phase
     completed and was deleted), or the brief is missing/empty, continue to
     step 4 to author a fresh brief for phase `NN`.

4. **Author a fresh brief for phase `NN`.**
   - Read only `project/plan/phase-NN.md` (the phase body).
   - Resolve the phase's realized Decision(s) via `project/design/INDEX.md`'s
     `## Decisions` section, then read only those `project/design/DNN.md`
     files.
   - Determine the ids to cover: **only** the ids the phase body/`Done when`
     lists — never a Decision's whole Verification list if the phase covers
     just a slice of it.
   - Copy each realized Decision's **full design prose** verbatim (the
     `## Decision.` and `## Rejected.` sections) into the brief, **omitting**
     its `## Verification.` list.
   - Copy each covered id's **full requirement text** verbatim from its
     Decision's Verification list, one id per line, in the exact form
     `R-XXXX-XXXX — <full requirement text>`.
   - If the phase is structural (owns no ids — `realizes —` on its `STATUS.md`
     line), write `(none — structural phase)` in place of an id list.
   - Extract the **public interface signatures** of any package(s) this phase's
     code depends on (exported funcs/types/interfaces it must consume) from
     those packages' current source — not their tests.
   - Write `project/loops/brief.md` to the schema below with an **empty**
     feedback region.
   - Report `NEXT`.

## crm project conventions (for the brief you author)

- **Toolchain:** Go 1.26, single module `crm` rooted at `crm/`.
- **Build/typecheck:** `cd crm && go build ./...` and `cd crm && go vet ./...`.
- **Test command:** `cd crm && go test ./...`.
- **The suite is green** means all of: `cd crm && go build ./...`,
  `cd crm && go vet ./...`, `cd crm && gofmt -l .` (no output), and
  `cd crm && go test ./...` succeed with zero failures.
- **Test-file glob:** `*_test.go` — where `R-XXXX-XXXX` tags live.
- **Test placement:** unit tests are co-located with the code they exercise,
  package-local (e.g. `internal/crm/contact.go` → `internal/crm/contact_test.go`,
  `internal/mcp/tools.go` → `internal/mcp/tools_test.go`), named for the
  behavior under test. `cmd/crm/` carries the composition-root and integration
  tests (e.g. `cmd/crm/docs_test.go`, `cmd/crm/loopback_guard_test.go`,
  `cmd/crm/main_test.go`) — that is the single home for cross-package
  integration tests. Never write a per-phase or root-level test file.

## The `project/loops/brief.md` schema

```
# Brief — Phase NN

## Contract (gather-owned — do not edit outside gather)

**Objective:** <one line from the phase body>

**Realizes:** D<n> (<title>)[, D<m> (<title>)]

### Design prose

<full ## Decision. and ## Rejected. sections copied verbatim from each
realized DNN.md, Verification list omitted>

### Ids to cover

R-XXXX-XXXX — <full requirement text copied verbatim>
R-YYYY-YYYY — <full requirement text copied verbatim>
<or: (none — structural phase)>

### Files to touch

<paths named/implied by the phase body>

### Dependency interface signatures

<exported funcs/types/interfaces from packages this phase consumes>

### Done when

<the phase body's deterministic Done-when bar, copied verbatim>

## Verify feedback — attempt 0

(no feedback yet — first attempt)
```

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: tells `ralph` to stop the loop. It carries no other
  meaning; say *why* in the message.
- `message` — one short, plain sentence describing what happened, e.g.
  `Authored brief for Phase 21 (D9 self-serve fonts).`

Report `DONE` only when there is no pending `⬜` phase in `project/plan/STATUS.md`
or when `project/loops/blocked.md` exists; otherwise report `NEXT`. Keep
`message` a single plain sentence, not a JSON object or code block.
