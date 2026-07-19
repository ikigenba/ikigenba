# Phase 49 — Retire the shared-blob era

*Realizes design Decision 12 (per-app secrets parameters, pushed from
`.envrc`). Depends on Phase 48.*

Remove every trace of the shared-blob seeding path from the repo, leaving
`bin/push-secrets` as the only procedure:

- **Delete the seven per-service scripts**: `dashboard/bin/secrets`,
  `dropbox/bin/secrets`, `github/bin/secrets`, `gmail/bin/secrets`,
  `notify/bin/secrets`, `prompts/bin/secrets`, `wiki/bin/secrets`. (This is
  operator-sanctioned for the spec-governed services: the deletion is part of
  this sealed suite-level spec.)
- **Rewrite `sops/seed-secrets.md`** around the per-app model: one parameter
  per app at `/ikigenba/<ACCOUNT>/app-config/<app>`, the app's `.envrc` as
  the source of truth, `bin/push-secrets` as the only procedure,
  seed-before-first-deploy for every app (secret-less apps included), and
  names-only verification (`get-parameters-by-path` for the inventory,
  `--with-decryption … | jq -r keys` per app — never print a value). The
  shared-blob read-modify-write instructions and the
  `prompts/bin/secrets` reference-implementation pointer are gone.
- **Sweep remaining references**: any mention of a per-service `bin/secrets`
  script or the shared-blob read-modify-write ritual in service `AGENTS.md`
  files, comments, or docs is updated to point at `bin/push-secrets`.

**Done when** (all deterministic, from the repo root):

- `ls dashboard/bin/secrets dropbox/bin/secrets github/bin/secrets
  gmail/bin/secrets notify/bin/secrets prompts/bin/secrets wiki/bin/secrets`
  exits non-zero with all seven paths absent
  (`find . -maxdepth 3 -path ./project -prune -o -name secrets -path '*/bin/*' -print`
  prints nothing).
- `grep -q 'bin/push-secrets' sops/seed-secrets.md` succeeds and
  `grep -qi 'read-modify-write' sops/seed-secrets.md` fails.
- `grep -rn 'bin/secrets' --exclude-dir=project --exclude-dir=.git .` prints
  no line naming a per-service script (exact match count 0 for the pattern
  `[a-z]*/bin/secrets`).
- The suite is green: `go test ./...` from the repo root exits 0.
