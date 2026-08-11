# artifacts/project — workspace map

Everything the artifacts service needs to be **designed, planned, and built**
lives under `project/`. This file is a map, not a manual: it says what is in
each folder and who writes it. Paths are written relative to the **service
root** (`artifacts/`), which is also the directory the build loop runs from.

## The folders

| folder | what's in it | written by |
|---|---|---|
| `product/` | `README.md` — the *why*: problem, users, scope, promises, success criteria | `$seal-spec` (rewritten in place) |
| `research/` | `research.md` — the external ground truth design cites (optional) | `$seal-spec` (rewritten in place) |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest) + `DNN.md` (one per Decision) | `$seal-spec` (rewritten in place) |
| `plan/` | `README.md` (rules) + `STATUS.md` (the `Next phase` counter and the only home of each pending phase's `⬜` marker) + `phase-NN.md` (one per **pending** phase) | `$seal-spec` (appends); the build loop deletes completed phases |
| `loops/` | the generated build-loop prompts and a `README.md` describing the installed loop | a prompt-generator workflow (not yet installed here) |

`project/**` is **direction-gated**: it is written only inside an
operator-invoked spec move (the `$open-spec` → `$grill-me` → `$seal-spec`
arc) or by the build loop's completion mutations. In any other session it is
read-only reference — a stale or wrong spec is a finding to report, not a
license to edit.

## The codebase this governs

`artifacts/` — and only `artifacts/`. The service is a suite mono-repo
subproject; this spec never names a seam, file, or phase outside that tree.
Suite-wide conventions are cited from the umbrella project as
`root project/design/DNN.md`, never restated locally.

## The build loop

No loop prompts are installed yet. When they are, `project/loops/README.md`
describes the installed loop's topology and how it consumes `plan/STATUS.md`;
that document belongs to the generator workflow, not to this map.
