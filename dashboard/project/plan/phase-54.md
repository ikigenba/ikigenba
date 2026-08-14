# Phase 54 — Add Grok as a third install agent

*Realizes design Decision 40 (install page Grok subsection), Decision 5 (home
excludes `/install/grok`), and Decision 42 (Grok install script).*

The dashboard's install page presents a third stacked peer, Grok, after Codex,
and `GET /install/grok` serves the Grok variant of the one-paste script through
the existing `handleInstall` seam. Home still has no install snippet, including
no `/install/grok`.

**Done when:**
- R-Y4GE-ZIS2 — signed-in `GET /install` renders `<h3>Grok</h3>` after Codex,
  with a snippet containing `/install/grok | bash` and a copy control named
  `Copy Grok install command`
- R-Y5OB-DAIR — the logged-in home body contains no `/install/grok`
- R-Y6W7-R29G — `GET /install/grok` with no session returns `200` `text/plain`
  bash through the registered route table
- R-Y844-4U05 — that script uses the grok remove/add lines with
  `--scope user`, `--transport http`, and
  `--header 'Authorization: Bearer ${IKIGENBA_TOKEN}'` for each MCP service,
  carries the missing-token guard, says `Restart Grok`, and contains neither
  `claude mcp` nor `codex mcp` nor `--bearer-token-env-var`
- `cd dashboard && go build ./...` && `go vet ./...` && `gofmt -l .` (no
  output) && `go test ./...` all succeed
