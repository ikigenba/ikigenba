# Phase 3 — Every WWW service's launch function exports its dev asset path

*Realizes design Decision 3 (the local dev stack) — its WWW-path membership
rule; no requirement ids (bash orchestration, the deliberately-untested tier).*

End state: `bin/start` satisfies D3's mechanical membership rule — every
service directory carrying a `share/www` has a matching
`<SVC>_WWW_PATH="$repo/<svc>/share/www"` export in its launch function. The
one current violation is telemetry (`launch_telemetry` sets only
`TELEMETRY_DB_PATH`, so the chassis's fail-loud WWW load kills the service at
dev boot); the fix adds its export in the same shape as every other WWW
service's line. No other launch function changes.

**Done when:**

- This check prints nothing (run from the repo root; it lists every service
  with a `share/www` whose export is missing from `bin/start`):

  ```sh
  for d in */share/www; do s=${d%%/*}; \
    u=$(echo "$s" | tr '[:lower:]' '[:upper:]'); \
    grep -q "${u}_WWW_PATH=\"\$repo/$s/share/www\"" bin/start || echo "$s"; \
  done
  ```

- `grep -c 'TELEMETRY_WWW_PATH' bin/start` prints a number ≥ 1.
- `bin/bintest` exits 0 (the staged-layout gate is untouched and stays green).
