# project — workspace layout

Everything this workspace needs lives under `project/`. This file is the only
loose file here; everything else is in one of the folders below. Paths are
written relative to the **repo root**, which is also the directory the build loop
runs from.

This is the suite's **umbrella project**. What it governs is not a codebase but
the suite's **shared contracts** — the agreements more than one tree must meet at
(the install tree, the release bundle, version identity, the durable/disposable
boundary, the event epoch, per-service adoption, the env and verb-set contract,
the secrets parameter, the telemetry protocol). It **builds no code of its own**:
every contracted behavior is implemented and proven in the tree that owns the
implementation, named on the behavior by a `[proof: …]` marker. Each subproject's
own `project/` (`appkit/`, `eventplane/`, `opsctl/`, `bin/`, `nginx/`, and every
service) **cites** these Decisions by path and restates none of them.

## The folders

| folder | what's in it | written by |
|---|---|---|
| `product/` | `README.md` — the *why*: problem, users, scope, promises, success criteria | `$seal-spec` (rewritten in place) |
| `research/` | `research.md` — collected external ground truth design references | `$seal-spec` (rewritten in place; optional) |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest + sorted `R-id → Decision` map, with proof-location markers) + `DNN.md` (one contract per Decision) | `$seal-spec` (rewritten in place) |
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
