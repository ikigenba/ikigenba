# appkit/project — workspace map

Everything needed to **design, plan, and build** appkit lives under `project/`,
at the root of the codebase it governs (`appkit/`, which is also the directory
the build loop runs from). This file is a map, not a manual — the shapes of the
documents below are owned by the spec workflow, and the installed build loop is
described in `loops/README.md`.

## The folders

| folder | what's in it | written by |
|---|---|---|
| `product/` | `README.md` — the *why*: problem, users, scope, promises, success criteria | `$seal-spec` (rewritten in place) |
| `research/` | `research.md` — collected external ground truth that design references | `$seal-spec` (rewritten in place; optional) |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest) + `DNN.md` (one per Decision) | `$seal-spec` (rewritten in place) |
| `plan/` | `README.md` (rules) + `STATUS.md` (manifest + `⬜`/`✅` markers) + `phase-NN.md` (one per phase) | `$seal-spec` (append-only) |
| `loops/` | the generated build-loop prompts + `README.md` describing the installed loop | a prompt-generator workflow |
| `bugs/` | free-form bug diagnoses / write-ups | free-form (not spec-owned) |
| `requests/` | free-form feature requests | free-form (not spec-owned) |

Don't add ad-hoc documents to the spec folders; changes to product, research,
design, and plan go through the spec workflow (`$open-spec` → `$seal-spec`).
