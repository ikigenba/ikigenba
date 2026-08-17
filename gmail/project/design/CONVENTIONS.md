# gmail — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module gmail` rooted at
  `gmail/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo). The landing page
  itself touches no SQLite, but the module/build facts are unchanged.
- **Build / typecheck command:** `cd gmail && go build ./...` and
  `cd gmail && go vet ./...`. The production build adds
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by
  `bin/ship gmail`).
- **Test command (the default gate):** `cd gmail && go test ./...`.
  **"The suite is green"** means: `cd gmail && go build ./...`,
  `cd gmail && go vet ./...`, `cd gmail && gofmt -l .`
  (no output), and `cd gmail && go test ./...` all
  succeed with zero failures.
- **Testing vocabulary:** the layer names every Decision below uses —
  **hermetic**, **composed**, **live**, **manual** — are the suite's, defined
  once in **`root project/design/D23.md`** and never restated here. gmail has a
  hermetic layer, a composed layer, and a live layer; it has no manual-layer
  check of its own. The default gate above carries the hermetic and composed
  layers only. gmail's adoption of that contract, and its declared testing
  facts, are **D25**.
- **Live invocation:** `cd gmail && go test -tags live ./...`. It requires
  `GMAIL_CLIENT_ID` and `GMAIL_CLIENT_SECRET` from the environment (channel 2)
  and `GMAIL_REFRESH_TOKEN` from the local `state/` file the client reads
  (channel 5, D28) — the same credentials, from the same places, the deployed
  service reads — and **fails loudly** when any is absent. It compiles only under that tag: today it is
  `internal/gmail/live_test.go`, the attachment round-trip against the real
  Gmail API that D19's R-3NGL-AMPW owns. The operator runs it at **deploy
  verification**, and whenever a change touches the Gmail client, the
  attachment/multipart path, or the OAuth token exchange. This invocation is
  what makes the tagged id reachable rather than dead; it is never part of the
  default gate.
- **GOWORK mode:** workspace — the default gate resolves the replace-siblings
  through the repo-root `go.work`; only the production build forces `GOWORK=off`.
- **Environmental preconditions:** none beyond the Go toolchain.
- **Test-file glob:** `*_test.go` — requirement-id tags (`// R-XXXX-XXXX`) live
  only in files matching this glob.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Module wiring:** `appkit`, `eventplane`, and `registry` are committed in-repo
  replace-siblings (`replace appkit => ../appkit`,
  `replace eventplane => ../eventplane`, `replace registry => ../registry` — the
  last added by D11). The web surface adds **no new third-party dependency** — the
  template loading, rendering, and static serving are the appkit chassis's
  (`appkit/web`, `Spec.WWW`); the service ships only the on-disk `share/www` tree.
- **The chassis owns the server.** gmail is `appkit.Main(appkit.Spec{…})`:
  `App:"gmail"`, `Mount:"/srv/gmail/"`, `Port:registry.MustPort("gmail")` (== 3202,
  D11), `MCP:true`, `WWW:true` (D9), `Feed:"/feed"` (event-plane producer). The
  fixed verbs (`serve`/`version`/`manifest`/`migrate`/`schema`), config-from-env,
  the loopback HTTP server + PRM + identity gate, the `/feed` mount, **and the
  www loader + `GET /static/` mount** are appkit's. The Spec is declared inline at
  the composition root (`cmd/gmail/main.go` as `gmailSpec()`, D13) and wires
  gmail's surface through the Spec hooks — `Handlers` (the landing render through
  `rt.WWW()` + the `POST /mcp` mount), `Producer` (the outbox sink), and
  `Workers` (the poll daemon). The landing route is wired through the
  **`Spec.Handlers`** hook, beside the `POST /mcp` mount; the `Producer`/`Workers`
  connector wiring is untouched.
- **nginx is the sole trust boundary.** gmail runs no token logic. nginx
  introspects every `/srv/gmail/` request against the dashboard and forwards to the
  loopback service. The landing page's gate is therefore an **nginx** concern
  (the `gmail/etc/nginx.conf` fragment), not a Go concern: the Go handler is
  mounted **ungated in-process**, exactly as `POST /mcp` relies on nginx for its
  bearer gate. gmail binds `127.0.0.1` only.
- **Two front doors, two audiences.** Humans in a browser are gated by the
  dashboard login-session cookie (`auth_request /_session-authn`); agents/MCP
  clients are gated by an opaque bearer (`auth_request /_authn`). The landing
  page is the **cookie-gated human** door; the existing `/mcp` is the
  **bearer-gated agent** door, unchanged.
