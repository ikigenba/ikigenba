# project — workspace layout (`bin/`)

Everything this workspace needs to be **designed, planned, and built** lives
under `bin/project/`. This file is the only loose file here; everything else is
in one of the folders below. Paths inside this workspace are written relative
to the **repo root** (the directory the build loop runs from); the codebase
this workspace governs is the repo-root **`bin/`** tree — the off-box operator
scripts and the `bin/bintest` Go test module — and nothing else.

## The folders

| folder | what's in it | written by |
|---|---|---|
| `product/` | `README.md` — the *why*: problem, users, scope, promises, success criteria | `$seal-spec` (rewritten in place) |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest + sorted `R-id → Decision` map) + `DNN.md` (one per Decision) | `$seal-spec` (rewritten in place) |
| `plan/` | `README.md` (static rules) + `STATUS.md` (manifest: `Next phase` counter + `⬜` lines) + `phase-NN.md` (one per **pending** phase) | `$seal-spec` (appends); the build loop deletes completed phases |

There is no `research/` here: this workspace depends on no external ground
truth of its own. The suite-wide contracts `bin/` produces artifacts for — the
version identity, the release-bundle layout, the install tree, the env/manifest
contract — are owned by the **umbrella project** at the repo root
(`project/design/`) and are **cited by path, never restated** here.

Product and design are the **single current statement** — rewritten in place,
never stacked; the plan is a **work queue** of pending phases only — completed
phases are deleted by the build loop, and construction history lives in git.
Don't add ad-hoc documents to the spec folders; change the spec through an
`$open-spec` → `$seal-spec` session instead.
