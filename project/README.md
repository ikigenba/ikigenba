# project — workspace layout

Everything this workspace needs to be **designed, planned, and built** lives
under `project/`. This file is the only loose file here; everything else is in
one of the folders below. Paths are written relative to the **repo root**,
which is also the directory the build loop runs from — this workspace governs
the suite mono-repo it sits at the root of.

## The folders

| folder | what's in it | written by |
|---|---|---|
| `product/` | `README.md` — the *why*: problem, users, scope, promises, success criteria | `$seal-spec` (rewritten in place) |
| `research/` | `research.md` — collected external ground truth design references | `$seal-spec` (rewritten in place; optional) |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest + sorted `R-id → Decision` map) + `DNN.md` (one per Decision) | `$seal-spec` (rewritten in place) |
| `plan/` | `README.md` (static rules) + `STATUS.md` (manifest: `Next phase` counter + `⬜` lines) + `phase-NN.md` (one per **pending** phase) | `$seal-spec` (appends); the build loop deletes completed phases |
| `bugs/` | free-form bug diagnoses / write-ups | free-form (not spec-owned) |
| `requests/` | free-form feature requests | free-form (not spec-owned) |
| `loops/` | the generated build-loop prompts + how the installed loop works | a prompt-generator workflow |

Product, research, and design are the **single current statement** — rewritten
in place, never stacked; the plan is a **work queue** of pending phases only —
completed phases are deleted by the build loop, and construction history lives
in git. The `bugs/` and `requests/` folders are informal scratch. Don't add
ad-hoc documents to the spec folders; change the spec through an
`$open-spec` → `$seal-spec` session instead.

For how the installed build loop runs, see `project/loops/README.md` (or the
loop prompt files themselves) — loop mechanics belong to the generator
workflow that installed them, not to this map.
