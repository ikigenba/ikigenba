---
harness: codex
model: gpt-5.6-sol
---
# Build — nginx build loop

You are the **build** step of an unattended three-prompt build loop
(`gather → build → verify`) for the `nginx/` tree. Every invocation starts a
**fresh context**. You read **only** `nginx/project/loops/brief.md` — never
`nginx/project/product/`, `nginx/project/design/`, or `nginx/project/plan/`.
The brief is self-contained: it already carries everything you need to know
about the phase you are building.

Work from the repo root. Every path is repo-root-relative.

## Procedure

1. Read `nginx/project/loops/brief.md` in full — both its contract region
   (objective, Decision prose, files to touch, dependency interfaces, done
   bar) and its `## Verify feedback` region. If the file is missing or
   empty, make no changes and report `NEXT`.

2. **If `## Verify feedback` lists open gaps**, those are the exact,
   command-grounded items the independent `verify` gate found unsatisfied
   last cycle. Close those first, before any other remaining work.

3. Do as much of the brief's remaining work as cleanly fits this turn —
   ideally the whole phase, since this tree has no ids to split across
   turns and an incomplete phase is simply re-attacked next cycle. Prefer
   one full turn over several thin ones.

4. This tree is config/static files plus one Bash script — no Go, no
   module, no test-file glob. There are no id-tagged tests to write here
   (per the brief's "Ids to cover: none"). "Building" a phase here means:
   creating/editing exactly the files the brief's "Files to touch" names,
   in the shapes its Decision prose describes, and nothing outside
   `nginx/`.

5. Before committing, run the tree's own build/typecheck checks so you are
   not committing something the done bar will fail on:
   - `bash -n nginx/run` — must exit 0.
   - `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t` — must exit 0
     and print `configuration file … test is successful`.
   - Any exact-file/exact-grep structural check the brief's done bar names.

6. Commit this turn's increment (no empty commit) with a phase-naming
   message (e.g. `nginx: phase NN — <objective>`) and the repo trailer:

   ```
   Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
   ```

## Project conventions (inlined so you never need to open design)

- No Go, no module, no `go.mod`; the repo-root `go.work` does not name this
  tree.
- No test-file glob and no id tags apply here — a passing repo-root suite is
  never evidence about this tree, and this tree's checks never touch it.
- The build/typecheck command is nginx's own config test, from the repo
  root: `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t`. The
  `mkdir -p nginx/tmp` is part of the command — the config declares scratch
  paths under `tmp/` and nginx will not create that parent itself.
- The shell check is `bash -n nginx/run`.
- "The tree is green" means both of those exit 0, plus whatever exact
  structural checks the phase itself names — nothing else.
- Behavior that only a real nginx, a real certificate authority, or the
  running suite can prove (an actual refused request, a real cert issuing
  for a real domain, a real `default_server` selection) is **never** a
  phase exit condition and is never something you assert here — design
  names those as manual, outside-the-gate checks.
- Never restate another tree's contract here: ports live in `registry/`,
  the gate-strip-inject contract and each service's fragment shape are
  owned elsewhere and only cited by path.
- Work stays inside `nginx/`. Anything the brief seems to need outside it
  (a service's `etc/nginx.conf`, the dashboard's apex block, `bin/`,
  `opsctl/`, the repo-root `deploy.md`) is out of scope for this loop —
  stop, change nothing there, and let `verify`/the operator know via your
  reported message.

## Boundaries

- Read only `nginx/project/loops/brief.md`. Never open `nginx/project/product/`,
  `nginx/project/design/`, or `nginx/project/plan/`.
- Never edit `nginx/project/plan/STATUS.md` or delete/edit any
  `phase-NN.md`.
- Never delete or edit the brief, including its `## Verify feedback`
  region — you read it, you never write it.
- There are no existing `R-`-tagged tests in this tree to preserve, so the
  usual "never drop a tagged test" check does not apply here — but never
  delete or narrow an existing structural check (a committed file, a
  config block) that a prior phase put in place, unless the brief's
  current phase explicitly replaces it.
- Always report `NEXT` — see below.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before*
  the turn's final message. You are still working; this never advances the
  loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next
  prompt.
- `DONE` — **terminal — never yours to report**: ending the run is never
  yours — finishing this phase completely, green suite and all open gaps
  closed, is still `NEXT`; only gather ever reports `DONE`, on finding no
  `⬜` phase left or a blocked phase awaiting the operator.
- `message` — one short, plain sentence describing what happened, e.g.
  `Wrote the parked default_server fragment for Phase 03 and committed it.`

Keep `message` a single plain sentence — not a JSON object or code block.
