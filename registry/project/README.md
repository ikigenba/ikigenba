# registry/project — workspace layout

Everything the `registry` module needs to be **designed, planned, and built**
lives under `project/`. This file is the only loose file here; everything else is
in one of the folders below. Paths are written relative to the **module root**
(`registry/`), which is also the directory the `ralph` build loop runs from.

> **Status: active.** The `project/` spine is populated and the build-loop
> prompts under `loops/` exist, so there is work for `ralph` to run. There is no
> code in `registry/` yet — the loop creates it. The spec is extended the usual
> way: evolve `product/` and `design/` in place with the `/*-mode` commands, then
> **append** a plan phase (`plan/phase-NN.md` + a `plan/STATUS.md` line). The
> prompts are committed and used as-is — appending a phase does **not** regenerate
> them.

## The folders

| folder | what's in it | owned by |
|---|---|---|
| `product/` | `product.md` — the *why*, for whom, scope, promises | `/product-mode` (rewritten in place) |
| `research/` | design-informing research notes (none yet) | free-form / `/research-mode` |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest + sorted `R-id → Decision` map) + `DNN.md` (one per Decision) | `/design-mode` (rewritten in place) |
| `plan/` | `README.md` (rules) + `STATUS.md` (the manifest — `Next phase` counter + only home of each pending phase's `⬜` marker) + `phase-NN.md` (one per **pending** phase; completion deletes it) | `/plan-mode` (work queue) |
| `bugs/` | free-form bug diagnoses | free-form (not mode-owned) |
| `requests/` | free-form feature requests | free-form (not mode-owned) |
| `loops/` | the `ralph` build-loop prompts: `gather.md`, `build.md`, `verify.md` (+ the ephemeral `brief.md`) and `run` | build-loop infrastructure |

## The build loop

`ralph` is the autonomous executor. It runs **from the module root** (`registry/`)
and is handed the full paths to the three prompt files:

```
ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

(or just `./project/loops/run`). It cycles the prompts in fresh contexts —
`gather → build → verify → …` — on a two-status contract: each prompt ends with
one JSON object whose `status` is `NEXT` (advance, wrapping `verify → gather`) or
`DONE` (stop).

- **gather** — the only prompt that reads the big docs. Greps `STATUS.md` for the
  first `⬜` phase; if none, the queue is empty and it returns `DONE` (the sole
  exit). Otherwise it resolves that phase's Decision(s) and writes a tiny,
  self-contained `loops/brief.md`, then returns `NEXT`. It preserves an in-flight
  brief rather than regenerating it.
- **build** — reads **only** `loops/brief.md`; builds the package + id-tagged
  tests, runs the suite, commits, touches no `STATUS.md` line. Returns `NEXT`.
- **verify** — the independent gate and only prompt that edits `STATUS.md`. Pass →
  delete that phase's `- Phase NN …` line from `STATUS.md` and `rm` its
  `phase-NN.md`, commit the deletion, and delete `brief.md`; gap → leave the
  `⬜` line as is and overwrite the brief's feedback region with the open gaps
  (the brief persists). Returns `NEXT`.

`brief.md` is the ephemeral seam between the prompts — created by `gather`, deleted
by `verify` on a pass, never committed (it is gitignored via the repo-root
`*/project/loops/brief.md` rule). The loop is human-free and converges: an
incomplete phase simply stays `⬜` and is re-attacked next cycle; the only stops
are `gather`'s `DONE` or a `ralph` budget rail.
