# wiki/project — workspace layout

Everything the wiki service needs to be **designed, planned, and built** lives
under `project/`. This file is the only loose file here; everything else is in
one of the folders below. Paths are written relative to the **service root**
(`wiki/`), which is also the directory the `ralph` build loop runs from.

## The folders

| folder | what's in it | owned by |
|---|---|---|
| `product/` | `README.md` — the *why*, for whom, scope, user-facing promises | `/product-mode` (rewritten in place) |
| `research/` | `research.md` — the design-informing research spine; plus free-form `*-research.md` working notes | `research.md`: `/research-mode` (rewritten in place). Other notes: free-form. |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest + sorted `R-id → Decision` map) + `DNN.md` (one per Decision) | `/design-mode` (rewritten in place) |
| `plan/` | `README.md` (spine) + `STATUS.md` (the manifest — the `Next phase` counter plus the only home of each pending phase's `⬜` marker) + `phase-NN.md` (one per **pending** phase) | `/plan-mode` (a work queue: completion is deletion — the build loop removes a finished phase's `STATUS.md` line and `phase-NN.md`; history lives in git) |
| `bugs/` | free-form bug diagnoses / write-ups | free-form (not mode-owned) |
| `requests/` | free-form feature requests | free-form (not mode-owned) |
| `loops/` | the `ralph` build-loop prompts: `gather.md`, `build.md`, `verify.md` (+ the ephemeral `brief.md`) | build-loop infrastructure |

The four **spine documents** (`product/README.md`, `research/research.md`,
`design/README.md`, `plan/README.md`) are each singular and owned by a `/*-mode`
command — that command is the sanctioned way to change them. The `bugs/`,
`requests/`, and extra `research/*-research.md` notes are informal scratch and
are *not* owned by any mode command. Don't add ad-hoc documents to the spine
folders; fold corrections and follow-ons into the existing spine docs via the
mode commands (and append a plan phase) instead.

## The build loop

`ralph` is the autonomous executor. It runs **from this service directory** and
is handed the full paths to the three prompt files — the names and locations are
this project's convention (documented here and in `project/loops/README.md`);
`ralph` itself assumes nothing about them:

```
ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

It cycles the prompts in fresh contexts — `gather → build → verify → …` — on a
two-status contract: each prompt ends with one JSON object whose `status` is
either `NEXT` (advance to the next prompt, wrapping `verify → gather`) or `DONE`
(stop).

- **gather** — the only prompt that reads the big docs. Greps `STATUS.md` for
  the first `⬜` phase; if there is none it returns `DONE` (the sole exit).
  Otherwise it resolves that phase's Decision(s) and writes a tiny, self-contained
  `loops/brief.md`, then returns `NEXT`.
- **build** — reads **only** `loops/brief.md`; builds the package + id-tagged
  tests, runs the suite, commits, leaves the marker untouched. Returns `NEXT`.
- **verify** — the independent gate and only prompt that completes a phase.
  Pass → delete that phase's `- Phase NN …` line from `STATUS.md` (never the
  `Next phase` counter line) and its `phase-NN.md` body file, and commit the
  deletion; gap → leave its `⬜` line untouched. Either way it deletes
  `loops/brief.md`. Returns `NEXT`.

`brief.md` is the ephemeral seam between the prompts — created by `gather`,
deleted by `verify`, never committed (it is gitignored). The loop is human-free
and converges: an incomplete phase simply stays `⬜` and is re-attacked next
cycle; the only stops are `gather`'s `DONE` or a `ralph` budget rail.
