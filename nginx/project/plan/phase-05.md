# Phase 5 — Add `artifacts` to `run`'s fragment list

*Realizes design Decision 2 (`run`: fragment regeneration) — its literal
service-list rule; no requirement ids (repo-root shell tooling, the
deliberately-untested tier).*

End state: `nginx/run`'s `for svc in …` list names all fifteen path-routed
services, `artifacts` included, so a `run` copies `artifacts/etc/nginx.conf`
into `locations/artifacts.conf` and the dev front door routes `/srv/artifacts/`.
artifacts joined the suite's deployable services and `bin/start` now launches it
and lists it in its own port-offset fragment loop; `nginx/run` was the one
fragment list that still omitted it, so the two dev front doors disagreed —
exactly the drift D2's "must agree with `bin/start`" clause guards against, and
the reason `/srv/artifacts/` returns 404 on the offset-0 path a netns instance
uses. The fix adds the one name; artifacts already ships its own
`etc/nginx.conf` fragment, so no port and no path is written here.

**Done when:**

- `bash -n run` exits 0.
- `run`'s `for svc in …` list contains `artifacts` alongside the other
  fourteen: this prints `1`

  ```sh
  grep -cE 'for svc in .*\bartifacts\b' run
  ```

- The list still names every other service (no name dropped): each of
  `crm cron dropbox gmail github ledger notify prompts repos scripts sites
  telemetry webhooks wiki` appears in the `for svc in …` line.
- `artifacts/etc/nginx.conf` exists (the fragment the copy loop reads), so a
  `run` produces `locations/artifacts.conf` byte-identical to it.
