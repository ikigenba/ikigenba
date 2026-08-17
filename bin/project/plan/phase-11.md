# Phase 11 — bin/start reads each service's etc/env.list from the keyring

*Realizes design Decision 3 (local dev stack).*

Replace `bin/start`'s per-service secret sourcing with the `etc/env.list` read
(`root project/design/D12.md`), keeping the launcher app-agnostic:

- Each per-service launch function replaces `source "$repo/<svc>/.envrc"` with a
  scoped read of `<svc>/etc/env.list` in the same subshell: a `config NAME value`
  line becomes `export NAME=value`, a `secret NAME` line becomes
  `export NAME="$(secret-tool lookup name NAME)"`. The per-service scoped subshell
  is kept, so one service's customer data never leaks into another's environment.
- The `source_up` shim and any repo-root `.envrc` sourcing are removed; `GOFLAGS`
  is supplied by the launch environment as it is today.
- Unchanged: the two checkout-relative exports `bin/start` already sets itself
  (`DASHBOARD_MANIFEST_ROOT`, `REPOS_STATE_DIR`), the `<SVC>_WWW_PATH` exports, the
  registry-derived ports, and the staging half (`stage_manifest_root`, D5).

The orchestrating half of `bin/start` is the deliberately-untested tier
(CONVENTIONS): it mints no ids, and the live stack-up stays product's success
criterion, verified by the operator outside the loop.

**Done when** (from the `bin/` tree root, the loop's working directory):
- `go test ./bintest/...` exits 0 — the D5 `--stage-only` and registry tests
  stay green; the staging half is untouched by this change.
- `grep -q 'etc/env.list' start` and `grep -q 'secret-tool lookup name' start`
  both succeed.
- `grep -nE '\.envrc|source_up' start` prints nothing — no service `.envrc` is
  sourced any longer.
