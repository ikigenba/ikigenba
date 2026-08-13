# notify — Design Conventions

## Conventions

- **Language / toolchain:** Go **1.26**, single module `module notify` rooted at
  `notify/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo).
- **Build / typecheck command:** `cd notify && go build ./...` and
  `cd notify && go vet ./...`. The production build adds
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by
  `bin/ship notify`).
- **Test command:** `cd notify && go test ./...`. **"The suite is green"** means:
  `cd notify && go build ./...`, `cd notify && go vet ./...`,
  `cd notify && gofmt -l .` (no output), and `cd notify && go test ./...`
  all succeed with zero failures.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Requirement-id tags live in `*_test.go` files** — the test-file glob every
  `R-XXXX-XXXX` id is grepped against to determine realized-ness.
- **Module wiring:** `appkit`, `eventplane`, and `registry` are committed
  in-repo replace-siblings (`replace appkit => ../appkit`,
  `replace eventplane => ../eventplane`, `replace registry => ../registry`).
  The repo-root `go.work` and the sibling modules themselves are external
  preconditions owned outside `notify/`.
- **The chassis owns the server.** notify is `appkit.Main(appkit.Spec{…})`:
  `App:"notify"`, `Mount:"/srv/notify/"`, `Port:registry.MustPort("notify")`
  (== `3201`; D9), `MCP:true`, `WWW:true` (D12), and
  `Consumers:[…crm…, …prompts…]` (D11 — the declared event-plane consumer role;
  notify serves **no** `/feed` of its own). The fixed verbs
  (`serve`/`version`/`manifest`/`migrate`/`schema`), config-from-env, the
  loopback HTTP server + PRM + identity gate, the www-site load + static
  mount, the MCP transport with the standard `health`/`reflection` tools, and
  the consumer loops are appkit's. main.go declares notify's identity (the
  Spec) and wires its surface through the Spec hooks; the landing route is
  wired through **`Spec.Handlers`**, beside the `POST /mcp` mount.
- **nginx is the sole trust boundary.** notify runs no token logic. nginx
  introspects every `/srv/notify/` request against the dashboard and forwards to
  the loopback service. The landing page's gate is therefore an **nginx** concern
  (the `notify/etc/nginx.conf` fragment), not a Go concern: the Go handler is
  mounted **ungated in-process**, exactly as `POST /mcp` relies on nginx for its
  bearer gate. notify binds `127.0.0.1` only.
- **Two front doors, two audiences.** Humans in a browser are gated by the
  dashboard login-session cookie (`auth_request /_session-authn`); agents/MCP
  clients are gated by an opaque bearer (`auth_request /_authn`). The landing
  page is the **cookie-gated human** door; `/mcp` is the **bearer-gated agent**
  door.
- **Cross-module collaborators (outside `notify/`).** The repo-root `bin/start`
  is not Go and not under `notify/`; where a Decision names it (D11's env-name
  migration, D12's `NOTIFY_WWW_PATH` export) it is a boundary-crossing
  collaborator verified by the live `bin/start` smoke, **not** by the notify Go
  suite. Phases that touch it are called out explicitly in the plan.
