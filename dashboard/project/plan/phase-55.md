# Phase 55 — Antigravity install-script agent + install-page subsection

*Realizes design Decision 43 (Antigravity install script) and 40 (install-page
Antigravity subsection).*

Add Antigravity as the fourth agent on the connect-your-agent surface.

- **Script route.** `handleInstall` gains the literal `agent` value `agy` at
  `GET /install/agy`, public and `text/plain; charset=utf-8`, serving a
  distinct builder (not the shared `installScript` skeleton) that produces the
  `agy plugin install` script of D43: shebang + `set -euo pipefail`, the
  `${IKIGENBA_TOKEN}` guard pointing at the request scheme/host,
  `Installing N MCP`, a temp plugin dir holding `plugin.json`
  (`{ "name": "ikigenba", "description": "Ikigenba MCP tools" }`) and an
  `mcp_config.json` with one `"ikigenba_<svc>"` `serverUrl`/`Authorization`
  block per `MCP=true` inventory service (`mcpLocalName` / `mcpResourceURL`
  helpers, `${IKIGENBA_TOKEN}` left literal), a single `agy plugin install`, the
  per-service 🟢/🔴 loop over the one result, `${ok} of N successfully
  installed.`, and `Restart agy`. A failed `inventory.Read` keeps
  `handleInstall`'s HTTP-500-no-body contract.
- **Install page.** The `/install` page (D40) grows a fourth stacked subsection
  after Grok: `<h3>Antigravity`, the muted `Adds each service to Antigravity's
  MCP configuration.` line, a snippet box holding
  `curl -fsSL {{.Scheme}}://{{.Host}}/install/agy | bash` with a flush Copy
  control whose accessible name is `Copy Antigravity install command`.
  `Antigravity` appears only as page text; the route and script read `agy`.

**Done when:**
- R-QFNA-KYOH — `GET /install/agy` with no session, through the registered route
  table, returns `200`, `Content-Type` `text/plain`, body starting
  `#!/usr/bin/env bash`.
- R-QGV6-YQF6 — that body against a dashboard + crm + ledger (`MCP=true`)
  fixture requested as `https://int.ikigenba.com` contains
  `echo "Installing 2 MCP"`, `"name": "ikigenba"`,
  `"description": "Ikigenba MCP tools"`, the crm and ledger `serverUrl` /
  `"Authorization": "Bearer ${IKIGENBA_TOKEN}"` blocks, `agy plugin install`,
  `🟢 ikigenba_crm`, `🟢 ikigenba_ledger`,
  `if [ -z "${IKIGENBA_TOKEN:-}" ]; then`,
  `echo "${ok} of 2 successfully installed."`, `Restart agy`; and does not
  contain `claude mcp`, `codex mcp`, `grok mcp`, or `--bearer-token-env-var`.
- R-QEFE-76XS — `GET /install` (live session) renders, after the Grok
  subsection, an `<h3>` reading `Antigravity` above a snippet containing
  `/install/agy | bash` with a copy control named `Copy Antigravity install
  command`, Grok strictly before Antigravity.
- The suite is green (`cd dashboard && go build ./...`, `go vet ./...`,
  `go test ./...` all clean).
