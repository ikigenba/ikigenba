# Phase 34 — Promote lint tier to `strict`

*Realizes design Decision 22 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`appkit/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports nine findings, all internal complexity/style items resolvable without
changing any exported signature or seam —

- gocritic `unnamedResult` in `httpclient/httpclient.go`, `manifest/manifest.go`,
  `telemetry/recorder.go`, `telemetry/root.go` (two), and `telemetry/telemetry.go`
  (name the returned results);
- `gocyclo` (complexity 23 > 15) on `runServe` in `verbs.go` (reduce branching by
  extraction);
- `nestif` (complexity 5) in `mcp/mcp.go` and `server/server.go` (flatten the
  nested blocks).

Fix them so `bin/lint appkit` is clean at strict, and land the marker flip in
the same completion commit.

**Done when:** `cat appkit/.lint-tier` prints exactly `strict`;
`bin/lint appkit` (from the repo root) exits 0 reporting tier `strict`; and the
suite is green per CONVENTIONS (`cd appkit && go build ./...`, `go vet ./...`,
`gofmt -l .` empty, `go test ./...` all succeed).
