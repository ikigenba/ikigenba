# Phase 48 — `.envrc` conformance + the `bin/push-secrets` tool

*Realizes design Decision 12 (per-app secrets parameters, pushed from
`.envrc`).*

Bring the `.envrc` manifests into strict-parse conformance and build the one
generic seeding tool, exactly as D12 specifies:

- **`wiki/.envrc`**: unroll the `for`-loop into plain
  `export X="$(cat ~/.secrets/X)"` lines for `ANTHROPIC_API_KEY`,
  `OPENAI_API_KEY`, `GEMINI_API_KEY`, and `ZAI_API_KEY`; `IKIGENBA_TOKEN` is
  removed (it is workstation-only). Keep `source_up`.
- **Repo-root `.envrc`**: gains
  `export IKIGENBA_TOKEN="$(cat ~/.secrets/IKIGENBA_TOKEN)"` with a comment
  marking it workstation-only (the local-MCP-to-box token; never pushed —
  the root `.envrc` is not an app and is never parsed).
- **`bin/push-secrets`**: new executable bash script per D12 — strict
  `.envrc` parse, `~/.secrets` resolution with env-var override, flat-JSON
  assembly, temp-file `put-parameter --overwrite` write, `{}` for secret-less
  apps, `--all` over the `VERSION`-carrying app dirs, `--dry-run` printing
  parameter names and key names only, masked output, loud aborts.

No maintained tests (D12 mints no ids; `bin/` tooling is untested by
Conventions). Functional verification — `--dry-run` against every app's
`.envrc`, then a live push — is performed once by the operator/agent
**outside the build loop** after this phase lands.

**Done when** (all deterministic, from the repo root):

- `test -x bin/push-secrets` succeeds and `bash -n bin/push-secrets` exits 0.
- `grep -c 'export [A-Z_]*="\$(cat ~/.secrets/' wiki/.envrc` prints exactly
  `4`, and `grep -q 'IKIGENBA_TOKEN' wiki/.envrc` fails, and
  `grep -q 'for k in' wiki/.envrc` fails.
- `grep -q 'export IKIGENBA_TOKEN="\$(cat ~/.secrets/IKIGENBA_TOKEN)"' .envrc`
  succeeds.
- The suite is green: `go test ./...` from the repo root exits 0.
