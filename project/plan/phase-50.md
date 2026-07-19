# Phase 50 — push-secrets preserves multi-line secret values

*Realizes design Decision 12 (per-app secrets parameters, pushed from `.envrc`).*

`bin/push-secrets` currently strips **all** whitespace from every resolved
secret value (`tr -d '[:space:]'`, on both the env-var override path and the
`~/.secrets/<NAME>` file path), which flattens a multi-line PEM private key
into a single unparseable line. Rework value handling to D12's amended rule:

- Resolution preserves the value **byte-for-byte except trailing newlines are
  trimmed**, identically on both resolution paths. Empty-after-trim still
  aborts with the existing error message shape.
- JSON assembly carries values containing newlines (and tabs) intact. The
  current line-oriented tab-separated temp-file feed into `jq -Rn` cannot;
  replace it with an assembly that can (for example per-value `jq --arg`
  accumulation, or `--rawfile` from per-value `chmod 600` temp files). The
  chmod-600/trap-cleaned temp-file discipline and never-print-a-value rule are
  unchanged.
- The script's header comment states the new preservation rule instead of
  "stripped of whitespace".
- `sops/seed-secrets.md` gains the multi-line round-trip step in its manual
  verification procedure: after a live push of an app carrying a PEM secret,
  read back that parameter's key shape only — per-key value length and line
  count, never a value — and confirm the multi-line key kept its interior
  newlines.

**Done when:**

- `grep -F "tr -d '[:space:]'" bin/push-secrets` exits non-zero (the stripping
  idiom is gone).
- `bash -n bin/push-secrets` exits 0.
- `bin/push-secrets --dry-run --all` exits 0 and prints a `>> <app>: ` line
  for all fourteen deployable apps (count with
  `bin/push-secrets --dry-run --all | grep -c '^>> '` = 14).
- `grep -c 'line count' sops/seed-secrets.md` reports at least 1 (the
  round-trip step is present).
