# Phase 37 — The GitHub callback, its federation gate, and the composition-root wiring

*Realizes design Decision 28 (GitHub callback + org gate), plus the deferred
slices of Decision 26 (R-IF9T-9YRB) and Decision 25 (R-IAE7-QVSJ). Depends on
Phase 34, Phase 35, and Phase 36.*

Add `GET /oauth/github/callback` (`handleGitHubCallback`): binding cookie +
consume, refuse non-`github` handshakes (`provider_mismatch`), exchange via
`a.githubProvider`, then the inline federation gates — `EmailVerified`
(`email_not_verified`, 403) and `OrgMembership == "active"` (`org_membership`,
403) — then `ResolveOrCreate` with `iss https://github.com`, numeric-id `sub`,
and the `Name`→`Login` fallback, branching into the existing
`callbackWeb`/`callbackMCP`. Wire the composition root:
`cmd/dashboard/main.go` requires `GITHUB_CLIENT_ID`/`GITHUB_CLIENT_SECRET`/
`GITHUB_ORG` via `requireEnv`, constructs the live `githubidp` provider, and
passes it through `server.Register`.

**Done when:** R-INT3-YCY6, R-IP10-C4OV, R-IQ8W-PWFK, R-IRGT-3O69,
R-ISOP-HFWY, R-ITWL-V7NN, R-IF9T-9YRB, and R-IAE7-QVSJ are each covered by a
clearly-named, verbatim-tagged test (HTTP-level with the githubidp stub and a
real temp DB; the env gate at the main seam), and the suite is green per
design's Conventions.
