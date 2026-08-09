# Phase 3 — Silence the two nginx warnings in the front-door configs

*Realizes design Decision 1 (dev front door — the `http`-context
`variables_hash_max_size`) and 3 (parked `default_server` — no `server_name`).*

Two edits to committed configuration, no new files:

- `nginx/nginx.conf` gains `variables_hash_max_size 2048;` in its `http`
  context (D1 states where and why: the suite's ~150 `auth_request_set`
  variables overflow nginx's default 1024 table, so every start warns).
- `nginx/parked/nginx.conf` loses its two `server_name _;` lines, one per
  server block (D3 states why: selection is purely by `default_server`, and the
  `_` name collides with the stock package `nginx.conf` on the live box,
  warning on every reload). Nothing else in the file changes.

The observable end state is a warning-free configuration test: with the
per-service fragments present in `locations/` (regenerated exactly as `run`
does — copy each sibling `<svc>/etc/nginx.conf` into `locations/<svc>.conf`),
`nginx -t` against the dev prefix reports success and emits no `[warn]` line.
The parked half cannot be exercised locally (the colliding block exists only on
the live box); its proof here is the file's content, and the live proof is the
runbook re-run in the repo-root `deploy.md`, which is operator work outside
this phase.

**Done when:**

- `grep -c 'variables_hash_max_size 2048;' nginx.conf` prints `1`, and the
  directive sits inside the `http` block (above the `server` block).
- `grep -c 'server_name' parked/nginx.conf` prints `0`.
- `grep -c 'default_server' parked/nginx.conf` prints `4` (both listens in both
  blocks keep the flag — the selection mechanism is untouched).
- From the tree root, after populating `locations/` with the sibling fragments
  as `run` does: `mkdir -p tmp && nginx -p . -c nginx.conf -t` exits 0 **and**
  its output contains no `[warn]` line
  (`nginx -p . -c nginx.conf -t 2>&1 | grep -c '\[warn\]'` prints `0`).
- `bash -n run` exits 0 (the tree's green bar; `run` itself is not edited).
