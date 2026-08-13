# cron — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module cron` rooted at
  `cron/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo). The landing page
  itself touches no SQLite, but the module/build facts are unchanged.
- **Build / typecheck command:** `cd cron && go build ./...` and
  `cd cron && go vet ./...`. The production build adds
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by
  `bin/ship cron`).
- **Test command:** `cd cron && go test ./...`. **"The suite is green"** means:
  `cd cron && go build ./...`, `cd cron && go vet ./...`, `cd cron && gofmt -l .`
  (no output), and `cd cron && go test ./...` all succeed with zero failures.
  Requirement-id tags live in Go test files matched by the glob `*_test.go`.
- **Testing vocabulary:** the layer names and the rule fixing each layer by what
  a test may touch are the suite contract's, adopted by D17 and cited at
  `root project/design/D23.md`; this design restates none of them and uses no
  other testing vocabulary. cron has two layers — **composed**: the boot smokes
  in `cmd/cron/main_test.go`, which build the real binary and run `serve` over a
  loopback port; **hermetic**: everything else. There is **no live layer** and no
  tree-local manual layer. Environmental preconditions beyond the Go toolchain:
  **none**. GOWORK mode: workspace for development, `GOWORK=off` for the
  production build.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Module wiring:** `appkit`, `eventplane`, and `registry` are committed in-repo
  replace-siblings (`replace appkit => ../appkit`,
  `replace eventplane => ../eventplane`, `replace registry => ../registry` — the
  last added by D11). The web surface adds **no third-party dependency** — the
  landing page renders through the chassis `appkit/web` site, the MCP surface
  assembles through `appkit/mcp`, and the port resolves through `registry`; the
  service code uses only the standard library plus the appkit/eventplane/registry
  siblings.
- **The chassis owns the server.** cron is `appkit.Main(cronSpec())`, where
  `cronSpec()` is a `func cronSpec() appkit.Spec` declared **inline in
  `cmd/cron/main.go`** (D8 — there is no `internal/cronapp` package):
  `App:"cron"`, `Mount:"/srv/cron/"`, `Port: registry.MustPort("cron")` (D11),
  `MCP:true`, `WWW:true` (D9), `Feed:"/feed"` (event-plane producer). The fixed
  verbs (`serve`/`version`/`manifest`/`migrate`/`schema`), config-from-env, the
  loopback HTTP server + PRM + identity gate, the `Spec.WWW` site load + auto
  `GET /static/` mount, and the `/feed` producer mount are appkit's. `main.go`
  declares cron's identity (the Spec) and wires its surface through the Spec hooks
  (the crontab `Store`, the assembled `appkit/mcp` `POST /mcp` handler, the LIVE
  `Publishes` provider, and the tick `Producer`/`Workers`). The landing route is
  wired through the **`Spec.Handlers`** hook, rendering `share/www/landing.html`
  through `rt.WWW()`, beside the `POST /mcp` mount.
- **nginx is the sole trust boundary.** cron runs no token logic. nginx
  introspects every `/srv/cron/` request against the dashboard and forwards to the
  loopback service. The landing page's gate is therefore an **nginx** concern
  (the `cron/etc/nginx.conf` fragment), not a Go concern: the Go handler is mounted
  **ungated in-process**, exactly as `POST /mcp` relies on nginx for its bearer
  gate. cron binds `127.0.0.1` only.
- **Two front doors, two audiences.** Humans in a browser are gated by the
  dashboard login-session cookie (`auth_request /_session-authn`); agents/MCP
  clients are gated by an opaque bearer (`auth_request /_authn`). The landing
  page is the **cookie-gated human** door; the existing `/mcp` is the
  **bearer-gated agent** door, unchanged.
