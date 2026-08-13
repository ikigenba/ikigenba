# scripts — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module scripts` rooted at
  `scripts/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo). The landing
  page itself touches no SQLite, but the module/build facts are unchanged.
- **Build / typecheck command:** `cd scripts && go build ./...` and
  `cd scripts && go vet ./...`. The production build adds
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by
  `bin/ship scripts`).
- **Test command (the default gate):** `cd scripts && go test ./...`.
  **"The suite is green"** means: `cd scripts && go build ./...`,
  `cd scripts && go vet ./...`,
  `cd scripts && gofmt -l .` (no output), and `cd scripts && go test ./...`
  all succeed with zero failures.
- **Testing vocabulary:** the layer names every Decision below uses —
  **hermetic**, **composed**, **live**, **manual** — are the suite's, defined
  once in **`root project/design/D23.md`** and never restated here. scripts has
  a hermetic layer and a composed layer and **no live or manual layer**: its one
  external effect is spawning a local `python3` process, which the contract
  counts as hermetic, and its neighbours are reached only over loopback. It
  therefore declares no `-tags live` invocation and no credentials. scripts's
  adoption of that contract, and its declared testing facts, are **D34**.
- **Environmental preconditions:** **`python3` on `PATH`** — the runtime the
  service execs, and the substrate of every runner, lifecycle, and `suite.py`
  claim below — and **`git` on `PATH`** — the binary that materializes every run
  dir as a pinned checkout (D38) and the substrate of the version-plane run
  claims. Both are hard failures when absent, never skips (D34).
- **GOWORK mode:** workspace — the default gate resolves the replace-siblings
  through the repo-root `go.work`; only the production build forces `GOWORK=off`.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Requirement-id tag location:** `R-XXXX-XXXX` tags live as `// R-XXXX-XXXX`
  comments in `*_test.go` files — the glob the coverage check greps.
- **Module wiring:** `appkit` and `eventplane` are committed in-repo
  replace-siblings (`replace appkit => ../appkit`,
  `replace eventplane => ../eventplane`). The landing page adds **no new
  dependency** — it uses only the standard library (`net/http`, `embed`,
  `html/template` or `text/template`) and the appkit chassis. **D10** adds a third
  such replace-sibling, `registry` (`replace registry => ../registry`, a
  standalone zero-dependency module at the repo root), consumed only at the
  composition root. The matching repo-root `go.work use ./registry` entry is
  **external, repo-root-owned wiring** (like the existing `eventplane` entry) and
  is a precondition, not built here.
- **The chassis owns the server.** scripts is `appkit.Main(scriptsSpec())`, where
  `scriptsSpec()` returns **one fully-formed `appkit.Spec` literal** (no
  post-construction mutation, per **D11**'s composition-root normalization):
  `App:"scripts"`, `Mount:"/srv/scripts/"`, `Port: registry.MustPort("scripts")`
  (== `3003`, resolved by name per **D10**; was a `3003` literal), `MCP:true`,
  `WWW:true` (web surface from `share/www` through the chassis, **D12**),
  `Feed:"/feed"` (event-plane **producer** of the completion kinds
  `succeeded`/`failed`, keys `scripts:succeeded|failed/<script name>`, **D18**),
  the `Consumers` table (six upstream entries — `cron`/`crm`/`ledger`/`dropbox`/
  `prompts`/`repos` (**D39**) — chassis-owned per **D11**, replacing the legacy `Consumes` +
  `Subscriptions` fields and the hand-rolled consumer `Workers`), `Health` (the
  static runtime-contract reporter, feeding both HTTP `/health` and the MCP `health`
  tool, **D13**), and the `Producer` outbox hook. The fixed verbs
  (`serve`/`version`/`manifest`/`migrate`/`schema`), config-from-env, the loopback
  HTTP server + PRM + identity gate, DB open + migration run, the `/feed` mount, the
  `GET /static/` mount, and the `consumer.Run` loops are appkit's. main.go declares
  scripts's identity (the Spec) and wires its domain surface through the Spec hooks.
  The landing route and `POST /mcp` (assembled via `appkit/mcp`, **D13**) are wired
  through the **`Spec.Handlers`** hook (`registerRoutes`).
- **nginx is the sole trust boundary.** scripts runs no token logic. nginx
  introspects every `/srv/scripts/` request against the dashboard and forwards to
  the loopback service. The landing page's gate is therefore an **nginx** concern
  (the `scripts/etc/nginx.conf` fragment), not a Go concern: the Go handler is
  mounted **ungated in-process**, exactly as `POST /mcp` relies on nginx for its
  bearer gate. scripts binds `127.0.0.1` only.
- **Two front doors, two audiences.** Humans in a browser are gated by the
  dashboard login-session cookie (`auth_request /_session-authn`); agents/MCP
  clients are gated by an opaque bearer (`auth_request /_authn`). The landing
  page is the **cookie-gated human** door; the existing `/mcp` is the
  **bearer-gated agent** door, unchanged. The event-plane `/feed` is loopback-only
  and never reachable through nginx — neither door touches it.
