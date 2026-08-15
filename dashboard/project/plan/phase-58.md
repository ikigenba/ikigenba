# Phase 58 — Promote lint tier to `strict`

*Realizes design Decision 44 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`dashboard/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports 13 findings, all internal complexity/style items fixable without
changing any exported signature or seam —

- `funlen`: `registerRoutes` (`cmd/dashboard/main.go`), `handleAuthn` and
  `handleAuthnPAT` (`internal/server/authn.go`), `handleAuthorize`
  (`internal/server/oauth.go`);
- gocritic `unnamedResult`: `internal/githubidp/githubidp.go`,
  `internal/grantevents/grantevents.go`, `internal/metrics/charts.go`,
  `internal/server/grants.go`;
- `gocyclo` (> 15): `verifyIDToken` (`internal/googleidp/googleidp.go`),
  `handleTokenAuthCode` and `handleTokenRefresh` (`internal/server/oauth.go`),
  `newApp` (`internal/server/server.go`);
- `nestif`: the `if !ok` block in `internal/server/authn.go`.

Shorten/extract the long functions, name the returned results, and reduce the
branching so `bin/lint dashboard` is clean at strict; land the marker flip in
the same completion commit.

**Done when:** `cat dashboard/.lint-tier` prints exactly `strict`;
`bin/lint dashboard` (from the repo root) exits 0 reporting tier `strict`; and
the suite is green per CONVENTIONS (`cd dashboard && go build ./...`,
`go vet ./...`, `gofmt -l .` empty, `go test ./...` all succeed).
