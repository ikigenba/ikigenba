# Phase 10 — push-secrets sources its key set from etc/env.list and the keyring

*Realizes design Decision 2 (push-secrets).*

Rework `bin/push-secrets` so an app's pushed key set and secret values come from
the customer-data contract (`root project/design/D12.md`) instead of the retired
`.envrc` + `~/.secrets` mechanism:

- `parse_secret_names` reads `<app>/etc/env.list` and emits the `NAME` of every
  `secret NAME` line, ignoring `config NAME value` lines, comments, and blanks;
  it no longer opens `<app>/.envrc`.
- `resolve_secret_file` resolves each name from a same-named environment
  variable when set, else from the operator's login keyring via
  `secret-tool lookup name <NAME>`, else aborts loudly; the `~/.secrets/<NAME>`
  file path is gone.
- Everything else is unchanged: flat-JSON assembly through a `chmod 600` temp
  file, `--all` / `--dry-run`, masked output, overwrite-only write, and value
  fidelity (byte-for-byte except a trimmed trailing newline) on both the
  environment-override and keyring paths. An app whose `env.list` has no
  `secret` lines, or no `env.list` at all, still pushes `{}`.

`bin/push-secrets` is the deliberately-untested tier (CONVENTIONS): it mints no
ids, and the live push against SSM stays an out-of-gate manual check.

**Done when** (from the `bin/` tree root, the loop's working directory):
- `go test ./bintest/...` exits 0 — the green gate stays green (the D5
  manifest-reader tests do not touch this script).
- `grep -q 'etc/env.list' push-secrets` and
  `grep -q 'secret-tool lookup name' push-secrets` both succeed.
- `grep -nE '\.envrc|\.secrets' push-secrets` prints nothing — the old
  mechanism is fully removed.
