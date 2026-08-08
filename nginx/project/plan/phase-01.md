# Phase 01 — `nginx/AGENTS.md`: the tree's committed testing declaration

*Realizes design Decision 4 (structural adoption of the testing-language
contract). Depends on no pending phase.*

`nginx/` gains the tree doc it has never had. This is a **structural** phase: no
code, no module, no test file — the tree deliberately has none, and this phase
does not change that.

What gets built:

- **`nginx/AGENTS.md`** — a new committed tree doc with two parts. A preamble:
  what `nginx/` is (the local-dev front door on `:8080` mirroring prod
  `/srv/<svc>/` routing via `./run`, plus the committed `parked/`
  `default_server` files for live non-apex domains), that it is spec-governed by
  `nginx/project/` so its config and `run` are not hand-edited, and that it is
  not versioned. Then a **Tests** section carrying the declaration D4 fixes:
  - the check commands — `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t`
    and `bash -n nginx/run`, run from the repo root — and that this tree has no
    test command because it has no test suite;
  - **layers: manual only** — no hermetic, no composed, no live layer, and no
    `//go:build live` file;
  - where the manual checks live: D3's live-box checklist in the repo-root
    `deploy.md`, and the suite-level bring-the-stack-up health check;
  - preconditions: an `nginx` binary on `PATH`; no Go toolchain;
  - GOWORK mode: not applicable — no Go module here, and the repo-root `go.work`
    must not name this tree.

**Done when:** `nginx/AGENTS.md` exists and carries the declared strings.
Checked from the repo root with exact match counts, `project/`-excluded so no
phrase in this spec can satisfy them — each of the following prints `1`:

- `grep -c -F 'layers: manual only' nginx/AGENTS.md`
- `grep -c -F 'nginx -p nginx -c nginx.conf -t' nginx/AGENTS.md`
- `grep -c -F 'bash -n nginx/run' nginx/AGENTS.md`
- `grep -c -F 'GOWORK' nginx/AGENTS.md` (the not-applicable declaration)
- `grep -rc -F 'layers: manual only' --exclude-dir=project nginx/ | grep -c ':1$'`
  — confirming the string lives in the tree outside `project/`, in exactly one
  file.

And the tree stays green as D1's Conventions define it: `bash -n nginx/run`
exits 0 and `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t` exits 0
(this phase changes no config, so both must still pass unchanged).
