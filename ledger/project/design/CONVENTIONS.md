# ledger — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module ledger` rooted at
  `ledger/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo).
- **Build / typecheck command:** `cd ledger && go build ./...` and
  `cd ledger && go vet ./...`. The production build adds
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by
  `bin/ship ledger`).
- **Test command:** `cd ledger && go test ./...`. **"The suite is green"** means:
  `cd ledger && go build ./...`, `cd ledger && go vet ./...`,
  `cd ledger && gofmt -l .` (no output), and `cd ledger && go test ./...` all
  succeed with zero failures.
- **Test-file glob:** `*_test.go` — requirement-id tags (`R-XXXX-XXXX`) live in
  these files only.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Module wiring:** `appkit`, `eventplane`, and `registry` are committed in-repo
  replace-siblings (`replace appkit => ../appkit`,
  `replace eventplane => ../eventplane`, `replace registry => ../registry`; D9).
  The repo-root `go.work` and the sibling modules themselves are external
  preconditions owned outside `ledger/`.
- **The chassis owns the server.** ledger is `appkit.Main(appkit.Spec{…})`:
  `App:"ledger"`, `Mount:"/srv/ledger/"`, `Port:registry.MustPort("ledger")`
  (== `3101`; D9), `MCP:true`, `WWW:true` (D10), and `Feed:"/feed"` (event-plane
  producer). The fixed verbs (`serve`/`version`/`manifest`/`migrate`/`schema`),
  config-from-env, the loopback HTTP server + PRM + identity gate, the www-site
  load + static mount, the MCP transport with the standard `health`/`reflection`
  tools, and the `/feed` mount are appkit's. main.go declares ledger's identity
  (the Spec) and wires its surface through the Spec hooks; the landing route is
  wired through the existing **`Spec.Handlers`** hook, beside the `POST /mcp`
  mount.
- **nginx is the sole trust boundary.** ledger runs no token logic. nginx
  introspects every `/srv/ledger/` request against the dashboard and forwards to
  the loopback service. The landing page's gate is therefore an **nginx** concern
  (the `ledger/etc/nginx.conf` fragment), not a Go concern: the Go handler is
  mounted **ungated in-process**, exactly as `POST /mcp` relies on nginx for its
  bearer gate. ledger binds `127.0.0.1` only.
- **Two front doors, two audiences.** Humans in a browser are gated by the
  dashboard login-session cookie (`auth_request /_session-authn`); agents/MCP
  clients are gated by an opaque bearer (`auth_request /_authn`). The landing
  page is the **cookie-gated human** door; the existing `/mcp` is the
  **bearer-gated agent** door, unchanged.
