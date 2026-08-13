# Phase 6 — Join `artifacts` to the local dev stack

*Realizes design Decision 3 (the local dev stack) — its "how a new service
joins" rule; no requirement ids (bash orchestration, the deliberately-untested
tier).*

End state: `bin/start` and `bin/stop` carry `artifacts` exactly as they carry
every other deployable service, so bringing the suite up locally builds,
launches, front-door-routes, and tears down artifacts alongside the rest.
artifacts is a deployable suite service (committed `VERSION`, `etc/manifest.env`
+ `etc/nginx.conf`, registry row `{"artifacts", 3009, Core}`, a `go.work`
member) that was simply never added to the stack; D3's list is "the suite's
deployable services", so its absence is the gap this phase closes. artifacts
holds no secret (no `.envrc`) and consumes no feed, so its launcher is the plain
`DB_PATH` + `WWW_PATH` shape (like `launch_crm`), not the secret-sourcing or
feed-wiring shapes.

The additions to `bin/start`, each mirroring the shape already used for the
peer services:

- `SERVICES` gains `artifacts` (the build list).
- a new `launch_artifacts` function exports
  `ARTIFACTS_DB_PATH="$RUN_DIR/artifacts.db"` and
  `ARTIFACTS_WWW_PATH="$repo/artifacts/share/www"`, then execs the built binary
  with `serve --port "$(port 3009)"` — the port resolved, never a second
  literal.
- a `run_bg artifacts launch_artifacts` line (before `nginx`).
- `launch_nginx`'s offset-path fragment loop gains `artifacts`, so the netns
  front door proxies `/srv/artifacts/` from `artifacts/etc/nginx.conf`.
- the `PORTS` map, the readiness `for name` loop, and the closing `/srv/…`
  summary line each gain `artifacts`.

`bin/stop`'s reverse-order kill list gains `artifacts` in a position consistent
with its start order.

The bare offset-0 front door (`nginx/run`) hardcodes the same fragment list in
the `nginx` workspace and is **out of this tree's scope**; routing artifacts
there is a separate `nginx`-spec change and is not required for this phase.

**Done when:**

- `bin/start`'s `SERVICES=(...)` line contains `artifacts`.
- `bin/start` defines `launch_artifacts`, and both of these fixed-string checks
  match (the `$repo` literal is not a regex anchor):

  ```sh
  grep -qF 'ARTIFACTS_WWW_PATH="$repo/artifacts/share/www"' bin/start
  grep -qF 'ARTIFACTS_DB_PATH="$RUN_DIR/artifacts.db"' bin/start
  ```

- `bin/start` contains `run_bg artifacts launch_artifacts`, its `launch_nginx`
  fragment `for svc in …` line lists `artifacts`, its `PORTS` map has an
  `[artifacts]` entry, its readiness `for name in …` line lists `artifacts`,
  and its closing summary line contains `/srv/artifacts/`.
- `bin/stop`'s `for name in …` kill list contains `artifacts`.
- `bin/bintest` exits 0 (`go test ./bintest/...` from `bin/`; the layout-reader
  gate is untouched and stays green).
