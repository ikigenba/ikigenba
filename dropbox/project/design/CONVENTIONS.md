# dropbox — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module dropbox` rooted at
  `dropbox/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo). The landing
  page itself touches no SQLite, but the module/build facts are unchanged.
- **Build / typecheck command:** `cd dropbox && go build ./...` and
  `cd dropbox && go vet ./...`. The production build adds
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by
  `bin/ship dropbox`).
- **Test command (the default gate):** `cd dropbox && go test ./...`.
  **"The suite is green"** means: `cd dropbox && go build ./...`,
  `cd dropbox && go vet ./...`, `cd dropbox && gofmt -l .` (no output), and
  `cd dropbox && go test ./...` all succeed with zero failures.
- **Testing vocabulary:** the layer names every Decision below uses —
  **hermetic**, **composed**, **live**, **manual** — are the suite's, defined
  once in **`root project/design/D23.md`** and never restated here. dropbox has
  a hermetic layer, a composed layer, and a live layer; it has no manual-layer
  check of its own. The default gate above carries the hermetic and composed
  layers only. dropbox's adoption of that contract, and its declared testing
  facts, are **D30**.
- **Live invocation:** `cd dropbox && go test -tags live ./...`. It requires
  `DROPBOX_APP_KEY` and `DROPBOX_APP_SECRET` from the environment (channel 2) and
  `DROPBOX_REFRESH_TOKEN` from the local `state/` file the client reads
  (channel 5, D33); optional `DROPBOX_APP_FOLDER_ROOT` scopes the smoke below the
  app-folder root. It **fails loudly** when any is absent. The operator runs it
  at **deploy verification**, and whenever a change touches the Dropbox write
  client or the uploader. It is never part of the default gate.
- **GOWORK mode:** workspace — the default gate resolves the replace-siblings
  through the repo-root `go.work`; only the production build forces `GOWORK=off`.
- **Environmental preconditions:** none beyond the Go toolchain.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Test-file glob:** `*_test.go` — the requirement-id tags for the coverage
  check live in these files.
- **Module wiring:** `appkit`, `eventplane`, and `registry` are committed in-repo
  replace-siblings (`replace appkit => ../appkit`,
  `replace eventplane => ../eventplane`, `replace registry => ../registry`; the
  `registry` require+replace is added by D9). The repo-root `go.work` and the
  sibling modules themselves are external preconditions owned outside `dropbox/`.
  The web surface adds **no other** new dependency — the page mechanism is the
  chassis (`appkit/web`), the MCP transport is the chassis (`appkit/mcp`).
- **The chassis owns the server.** dropbox is `appkit.Main(appkit.Spec{…})`:
  `App:"dropbox"`, `Mount:"/srv/dropbox/"`, `Port:registry.MustPort("dropbox")`
  (== `3200`; D9), `MCP:true`, `WWW:true` (D11), `Feed:"/feed"` (event-plane
  producer), plus its `Migrations`, `Events`, `ManifestExtras`, a `Health`
  reporter, a `Producer` hook, and a `Workers` hook (the background sync engine).
  The fixed verbs (`serve`/`version`/`migrate`/`schema`),
  config-from-env, the loopback HTTP server + PRM + identity gate, the www-site
  load + static mount, the MCP transport with the standard `health`/`reflection`
  tools, and the `/feed` mount are appkit's. main.go declares dropbox's identity
  (the Spec) and wires its surface through the Spec hooks: the landing route
  (rendered via `rt.WWW()`) and the `POST /mcp` mount (assembled by
  `internal/mcp.NewHandler`) are wired through the **`Spec.Handlers`** hook,
  beside the loopback `GET /content` / `GET /list` byte mounts.
- **nginx is the sole trust boundary.** dropbox runs no token logic. nginx
  introspects every `/srv/dropbox/` request against the dashboard and forwards to
  the loopback service. The landing page's gate is therefore an **nginx** concern
  (the `dropbox/etc/nginx.conf` fragment), not a Go concern: the Go handler is
  mounted **ungated in-process**, exactly as `POST /mcp` relies on nginx for its
  bearer gate. dropbox binds `127.0.0.1` only.
- **Two front doors, two audiences.** Humans in a browser are gated by the
  dashboard login-session cookie (`auth_request /_session-authn`); agents/MCP
  clients are gated by an opaque bearer (`auth_request /_authn`). The landing
  page is the **cookie-gated human** door; the existing `/mcp` is the
  **bearer-gated agent** door, unchanged. (The loopback `/content`/`/list` byte
  routes are a third, private-to-the-box door; since the structured-MCP adoption
  (D23) they are guarded by the shared chassis loopback wrapper
  (`rt.HandleLoopback` / `server.LoopbackOnly`, keyed on `X-Forwarded-Proto`
  only) rather than a per-handler predicate.)
