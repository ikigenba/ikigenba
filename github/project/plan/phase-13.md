# Phase 13 — Issue-execution support verbs

*Realizes design Decision 9 (`pr_create`, `issue_comments`, `label_add`,
`label_remove`). Depends on Phase 12.*

Four client methods on `internal/gh.Client` — `PRCreate` (POST
`/repos/{org}/{repo}/pulls`), `IssueComments` (GET
`…/issues/{n}/comments`), `LabelAdd` (POST `…/issues/{n}/labels`),
`LabelRemove` (DELETE `…/issues/{n}/labels/{label}`, via the new `delete`
helper on the shared `doJSON` path) — reusing the existing `PR`, `Comment`,
and `Label` types and the standard error mapping. Four MCP verbs in
`internal/mcp/tools.go` (bare names, both schemas, `decodeAndValidate`,
`clientResult`/`codeFor`), the `GitHubClient` interface extended by the four
methods, and exactly one `logWrite` line per write verb with no
owner-identifying field in any request body. Offline harness throughout
(RoundTripper/`apiBase` stub; fake client for the tool layer).

**Done when:** R-GJYX-0UGN, R-F70H-NRU9, R-GL6T-EM7C, R-GMEP-SDY1,
R-GNMM-65OQ, R-F88E-1JKY, R-GOUI-JXFF, and R-GQ2E-XP64 are each covered by a
clearly-named test, and the suite is green per design Conventions
(`GOWORK=off go build ./...` and `GOWORK=off go test ./...` clean with no
SKIP, `gofmt -l .` empty, `go vet ./...` clean, from `github/`).
