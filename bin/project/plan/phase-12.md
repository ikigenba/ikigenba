# Phase 12 — bin/start fails loudly when a declared secret is unresolved

*Realizes design Decision 3 (the local dev stack).*

`bin/start`'s `load_service_env` resolves each `secret NAME` with a bare
`value="$(secret-tool lookup name "$name")"` under `set -euo pipefail`, so an
unresolved secret (no keyring entry, or an empty value) kills the launcher
subshell before the binary is exec'd — the service surfaces a generic "exited"
against an empty log, indistinguishable from a real crash. This phase makes that
failure explicit (D3): when a `secret` line does not resolve, the launcher
writes a diagnostic naming the service and the unresolved secret to that
service's log, then exits non-zero, so the `✗ <svc> exited — see tmp/<svc>.log`
pointer the start-up wait already prints leads to the cause. The failure stays
scoped to the one service — the rest of the stack still comes up — because each
launcher runs in its own scoped subshell. `config` lines and successful secret
resolution are unchanged, as is the operator's ordinary invocation.

The orchestrating half of `bin/start`, this secret resolution included, is the
deliberately-untested tier (CONVENTIONS): it mints no ids, and the live stack-up
stays product's success criterion, verified by the operator outside the loop —
here, standing the stack up with a service whose `secret` line has no keyring
entry.

**Done when** (from the `bin/` tree root, the loop's working directory):
- `go test ./bintest/...` exits 0 — the D5 `--stage-only` and registry tests
  stay green; the staging half is untouched by this change.
- `grep -q 'secret-tool lookup name' start` succeeds — the keyring resolution is
  retained.
- `grep -qE 'not resolved|not found|missing|unresolved' start` succeeds — the
  unresolved-secret diagnostic branch is present (a `project/`-excluded grep
  against the script, matching no text in this tree today).
- Out of gate (untested tier per D3 / CONVENTIONS, a real keyring required):
  standing the stack up with a service whose `secret` line has no keyring entry
  aborts that one service with a diagnostic naming the service and the secret,
  and brings the rest of the stack up.
