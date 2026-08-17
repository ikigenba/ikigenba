# Phase 13 — push-secrets recognizes `rotating` lines and never pushes them

*Realizes design Decision 2 (push-secrets), which realizes the umbrella's
rotating-credential exclusion R-EJ1W-1G19 (`root project/design/D32.md`,
`[proof: bin]`).*

`bin/push-secrets` derives its pushed key set from `secret NAME` lines only, so a
channel-5 `rotating NAME` line (`root project/design/D32.md`) is already excluded
by construction. This phase makes that exclusion explicit and proven: recognize
`rotating NAME` as a known third line form (alongside `secret NAME` and
`config NAME value`) so it is never silently mistaken for anything else, and add
the `bin/bintest` test that pins the exclusion. No live-push behavior changes;
the value fidelity, `--all`, masking, and overwrite-only semantics are untouched.

**Done when** (from the `bin/` tree root, the loop's working directory):
- `go test ./bintest/...` exits 0.
- **R-EJ1W-1G19** covered by a tagged `bin/bintest` test: execing the real
  `push-secrets --dry-run` against a temp-dir app fixture whose `etc/env.list`
  carries a `secret` key, a `rotating` key, and a `config` key (plus a minimal
  `etc/deploy.env`), the printed key set **includes** the `secret` key and
  **excludes** the `rotating` and `config` keys — no keyring, no network.
- `grep -q 'rotating' push-secrets` succeeds — the keyword is recognized in the
  parser (a `project/`-excluded grep against the script).
