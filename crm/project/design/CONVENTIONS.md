# crm — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module crm` rooted at
  `crm/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo).
- **Build / typecheck command:** `cd crm && go build ./...` and
  `cd crm && go vet ./...`. The production build adds
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by
  `bin/ship crm`).
- **Test command:** `cd crm && go test ./...`. **"The suite is green"** means:
  `cd crm && go build ./...`, `cd crm && go vet ./...`, `cd crm && gofmt -l .`
  (no output), and `cd crm && go test ./...` all succeed with zero failures.
- **Test-file glob:** `*_test.go` is where `R-XXXX-XXXX` requirement-id tags
  live.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Module wiring:** `appkit`, `eventplane`, and `registry` are committed in-repo
  replace-siblings (`replace appkit => ../appkit`,
  `replace eventplane => ../eventplane`, `replace registry => ../registry`). The
  web and MCP surfaces add **no third-party dependency** — they use the standard
  library and the appkit chassis (`appkit/web`, `appkit/mcp`). Since D15 crm
  resolves its own loopback port by name through `registry`
  (`registry.MustPort("crm")`) instead of a bare literal.
- **The chassis owns the server — and, since D12/D13, the web mechanism and the
  MCP transport.** crm is `appkit.Main(appkit.Spec{…})`: `App:"crm"`,
  `Mount:"/srv/crm/"`, `Port:registry.MustPort("crm")` (== `3100`; D15),
  `MCP:true`, `Feed:"/feed"` (event-plane producer), `WWW:true` (web assets from
  the on-disk www root). The fixed verbs
  (`serve`/`version`/`manifest`/`migrate`/`schema`), config-from-env, the
  loopback HTTP server + PRM + identity gate, the `/feed` mount, the
  **auto-mounted `GET /static/`**, and the JSON-RPC `/mcp` plumbing with the
  standard `health`/`reflection` tools are appkit's. main.go declares crm's
  identity (the Spec) and wires its surface through the Spec hooks: the landing
  route and the gated `/mcp` mount live in **`Spec.Handlers`**.
- **Web assets are release artifacts on disk, not embedded.** crm's page
  template and static assets live in the source tree at `crm/share/www/`
  (`landing.html` + `static/tokens.css` + `static/fonts/*.woff2`), shipped by
  `bin/ship` into the versioned `share/<version>` tier and read on the box at
  `share/current/www` (appkit D5–D7 own the resolution and mechanism). The only
  `//go:embed` surfaces left in crm are the migrations (`internal/db`) and the
  MCP guide document (`internal/mcp/guide.md`) — agent-facing content, not web
  assets.
- **nginx is the sole trust boundary.** crm runs no token logic. nginx
  introspects every `/srv/crm/` request against the dashboard and forwards to the
  loopback service. The landing page's gate is therefore an **nginx** concern
  (the `crm/etc/nginx.conf` fragment), not a Go concern: the Go handlers are
  mounted **ungated in-process**, exactly as `POST /mcp` relies on nginx for its
  bearer gate. crm binds `127.0.0.1` only.
- **Two front doors, two audiences.** Humans in a browser are gated by the
  dashboard login-session cookie (`auth_request /_session-authn`); agents/MCP
  clients are gated by an opaque bearer (`auth_request /_authn`). The landing
  page is the **cookie-gated human** door; `/mcp` is the **bearer-gated agent**
  door.
