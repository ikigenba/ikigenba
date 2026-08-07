# nginx/project — workspace map

Everything the repo-root `nginx/` tree needs to be **designed, planned, and
built** lives under `project/`. This file is a map, not a manual: it says what is
in each folder and who writes it. Paths are written relative to the **tree root**
(`nginx/`), which is also the directory the build loop would run from.

## The folders

| folder | what's in it | written by |
|---|---|---|
| `product/` | `README.md` — the *why*: problem, users, scope, promises, success criteria | `$seal-spec` (rewritten in place) |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest) + `DNN.md` (one per Decision) | `$seal-spec` (rewritten in place) |
| `plan/` | `README.md` (rules) + `STATUS.md` (the `Next phase` counter and the only home of each pending phase's `⬜` marker) + `phase-NN.md` (one per **pending** phase) | `$seal-spec` (appends); the build loop deletes completed phases |
| `loops/` | the generated build-loop prompts and a `README.md` describing the installed loop | a prompt-generator workflow (not installed here) |

There is no `research/` tree: this design depends on no external ground truth of
its own. The registrar and TLS facts the parked front door rests on were
gathered once for the suite and live in the umbrella project's research at the
repo root; the Decisions here cite them rather than re-deriving or restating
them.

`project/**` is **direction-gated**: it is written only inside an
operator-invoked spec move (the `$open-spec` → `$grill-me` → `$seal-spec` arc) or
by the build loop's completion mutations. In any other session it is read-only
reference — a stale or wrong spec is a finding to report, not a license to edit.

## The codebase this governs

`nginx/` — and only `nginx/`. That is the local-dev front door (`nginx.conf`,
`run`, `locations/`) plus the committed parked `default_server` artifacts under
`nginx/parked/`. This spec never names a seam, file, or phase outside that tree:
not a service's `etc/nginx.conf`, not `dashboard/etc/nginx.conf`, not `bin/`, not
`deploy.md`. Where a Decision depends on something owned elsewhere — the suite's
path-routing and identity-header contract, the `/srv/<svc>/` fragment shape, the
operator runbook that installs the parked files — it **cites** the owning
document by path and owns none of it.

## The build loop

No loop prompts are installed. This tree is config, static files, and one shell
script; it is built and changed by hand under an operator instruction and
verified by the structural checks design's *Conventions* names.
