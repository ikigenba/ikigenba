# Phase 6 — Composition root & chassis boot

*Realizes design Decision 1 (Service skeleton, seams & composition root). Depends on Phases 1–5.*

Wire everything built so far into a single deployable binary. With the inner
packages (`internal/db`, the `webhooks` domain package, `internal/mcp`) all in
place, write `cmd/webhooks/main.go` as the composition root: one
`appkit.Main(appkit.Spec{…})` call declaring `App: "webhooks"`,
`Mount: "/srv/webhooks/"`, `Port: 3006`, `MCP: true`, `Feed: "/feed"`,
`Migrations: db.FS`, `Events: webhooks.Events`, the `ManifestExtras`
(`OUTBOX_RETENTION_DAYS=7`, `OUTBOX_RETENTION_MAX_ROWS=1000000`), a `Handlers` hook
that builds the `Service` and mounts the two routes —
`rt.RequireIdentity(mcp.NewHandler(...))` on `POST /mcp` and the **bare**
`NewIngressHandler(...)` on `/in/` — and a `Producer` hook that injects the
outbox into the `Service`. The service is **producer-only** (no `Spec.Consumes`,
no `Spec.Workers`) and uses appkit's default backup/restore (no overrides).

Add the committed `etc/manifest.env` (and `etc/deploy.env`) plus the `Makefile`.
`etc/manifest.env` must declare `APP=webhooks`, `MOUNT=/srv/webhooks/`,
`PORT=3006`, `MCP=true`, `FEED=/feed`, and the manifest extras, such that the
real binary's `manifest` verb byte-equals the committed file.

End state: `cd webhooks && go build ./... && go vet ./... && go test ./...` green
— `go build ./...` now covers `cmd/webhooks` too.

**Done when:** design D1's Verification ids are each covered by a genuine test
against the real binary / a real temp-file DB, and the suite is green —
- R-IC14-FKIK — the built binary's `manifest` verb output **byte-equals** the
  committed `etc/manifest.env`, and that file declares `APP`/`MOUNT`/`PORT`/`MCP`/
  `FEED` as above;
- R-ID90-TC99 — `serve` against a clean empty temp-file SQLite applies all
  migrations and the loopback/`httptest` server answers `/health` with a `200`
  health envelope reporting service `webhooks`.
