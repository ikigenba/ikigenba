# Phase 11 — Adopt `registry` at the composition root

*Realizes design Decision 15 (adopt `registry`; resolve crm's own loopback port
by name at startup). Depends on the existing `cmd/crm/main.go` composition root.
Covers `R-X04D-MBGE`. **Read D15 for the exact call site and rationale.***

crm stops hardcoding its loopback port literal and references itself **by name**
through the shared `registry` library, resolving **once at the composition
root**. This is behavior-preserving: `registry` already carries crm's current
value (`crm=3100`), so the resolved value is byte-identical to the literal it
replaces. crm is a producer and consumes no feeds, so — unlike notify's D9 — there
are **no peer-feed defaults to convert**; the own-port `Port: 3100` is the only
non-test literal that changes.

**External precondition (assume satisfied; do NOT build it here).** The repo-root
`go.work` carries `use ./registry` and the `registry` module exists and is green.
Both are owned outside `crm/` (repo root / the `registry` project). No step in
this phase edits `../go.work`, `../registry/`, or any sibling module — the
executor runs from `crm/` and cannot reach outside it.

**What gets changed (all inside `crm/`):**

- **`crm/go.mod`** — add `require registry v0.0.0` and a committed
  `replace registry => ../registry`, mirroring the existing `appkit` /
  `eventplane` in-repo replace-siblings. This is the only build-graph change.
- **`crm/cmd/crm/main.go`** — import `registry` and replace the appkit `Spec.Port`
  value `3100` → `registry.MustPort("crm")`. Leave every other Spec field (`App`,
  `Mount`, `MCP`, `WWW`, `Feed`, `Migrations`, `Events`, `ManifestExtras`, the
  `Handlers`/`Producer` hooks) exactly as they are — only the port literal moves
  to a `registry` call.
- Touch nothing else. **No schema change — no migration.** Do not edit
  `etc/manifest.env` or `etc/nginx.conf` (phase 12 re-points their *tests* at
  `registry`; the files' literals stay). The `cmd/crm/main_test.go` nginx and
  manifest assertions still carry `127.0.0.1:3100` / `Port: 3100` after this phase
  — phase 12 converts them; do not add the source-scan guard here.

**Done when:** the suite is green — `cd crm && go build ./...`,
`cd crm && go vet ./...`, `cd crm && gofmt -l .` (no output),
`cd crm && go test ./...`, and `bin/check-migrations crm` all succeed with zero
failures — and:

- R-X04D-MBGE — crm's composition-root listen port is `registry.MustPort("crm")`,
  not a `3100` literal. Asserted here if the Spec's port is directly inspectable
  from a test; otherwise delegated to phase 12's manifest drift guard
  (`manifest.Emit` with `registry.MustPort("crm")` byte-matching the committed
  `etc/manifest.env`) and tagged there — note the delegation in the code.
- `crm/go.mod` requires `registry` with a committed
  `replace registry => ../registry`.
- `grep -n "Port:[[:space:]]*3100" crm/cmd/crm/main.go` returns no match (the Spec
  literal is gone), while `grep -n "registry.MustPort" crm/cmd/crm/main.go` finds
  the new call.
