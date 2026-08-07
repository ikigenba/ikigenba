# Phase 24 — Env-channel conformance: session-engine knobs in the manifest; org routed as customer data

*Realizes design Decision 15 (env-channel conformance).*

What gets built: the `Spec` in `cmd/repos/spec.go` gains its four
`ManifestExtras` (`REPOS_PROVIDER=anthropic`, `REPOS_MODEL=claude-opus-4-8`,
`REPOS_SESSION_TTL=30m`, `REPOS_MAX_SESSIONS=2`) and the committed
`repos/etc/manifest.env` gains the matching lines after `CONSUMES=webhooks`
(the existing Spec-derived drift test pins the byte agreement); `repos/.envrc`
gains the strict customer-data line
`export REPOS_GITHUB_ORG="$(cat ~/.secrets/REPOS_GITHUB_ORG)"`; and a
source-scan test in `cmd/repos/main_test.go` guards repos' non-test Go source
against `"/opt` string literals. End state: the committed manifest is the
complete operator surface for repos' universal knobs, and the org rides the
app-config parameter.

**Done when:**
- R-L9EG-DDWC — resolved no-env knob defaults equal the committed
  `repos/etc/manifest.env` values.
- R-VKB6-SHHV — the source-scan test walks repos non-test `.go` files and
  finds no `"/opt` string literal.
- `grep -Fx 'export REPOS_GITHUB_ORG="$(cat ~/.secrets/REPOS_GITHUB_ORG)"' repos/.envrc`
  matches exactly one line.
- Both ids appear verbatim as tags in test files under `repos/`, and the suite
  is green per design Conventions (`cd repos && go test ./...`, plus
  build/vet).
