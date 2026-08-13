# Phase 5 — `launch_github` exports its dev asset path

*Realizes design Decision 3 (the local dev stack) — its WWW-path membership
rule; no requirement ids (bash orchestration, the deliberately-untested tier).*

End state: `bin/start` satisfies D3's mechanical membership rule — every
service directory carrying a `share/www` has a matching
`<SVC>_WWW_PATH="$repo/<svc>/share/www"` export in its launch function. The one
current violation is github: it moved onto the disk-served `share/www` shape
(chassis Spec now declares `WWW`), but `launch_github` sets only
`GITHUB_DB_PATH`, so the chassis's fail-loud WWW load kills the service at dev
boot (`load www ./share/www: no such file or directory`). The fix adds
`GITHUB_WWW_PATH="$repo/github/share/www"` in `launch_github`, in the same shape
as every other WWW service's line. No other launch function changes.

**Done when:**

- This check prints nothing (run from the repo root; it lists every service
  with a `share/www` whose export is missing from `bin/start`):

  ```sh
  for d in */share/www; do s=${d%%/*}; \
    u=$(echo "$s" | tr '[:lower:]' '[:upper:]'); \
    grep -qF "${u}_WWW_PATH=\"\$repo/$s/share/www\"" bin/start || echo "$s"; \
  done
  ```

  (`-F` matters: `$repo` is a literal here, not a regex anchor, so the check
  must be a fixed-string match or every service reads as a false violation.)

- `grep -c 'GITHUB_WWW_PATH' bin/start` prints a number ≥ 1.
- `bin/bintest` exits 0 (the staged-layout gate is untouched and stays green).
