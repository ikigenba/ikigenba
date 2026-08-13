# sites — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module sites` rooted at
  `sites/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo).
- **Build / typecheck command:** `cd sites && go build ./...` and
  `cd sites && go vet ./...`. The production build adds
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by
  `bin/ship sites`).
- **Test command:** `cd sites && go test ./...`. **The test-file glob where
  requirement-id tags live is `*_test.go`.** **"The suite is green"** means:
  `cd sites && go build ./...`, `cd sites && go vet ./...`, `cd sites && gofmt -l .`
  (no output), and `cd sites && go test ./...` all succeed with zero failures.
  **Green includes the browser wiring test (D23) and therefore hard-requires a
  `google-chrome` binary on `PATH`** of the box running the suite (present:
  `/usr/bin/google-chrome`). No Chrome → the suite is red, never skipped. The
  harness may retry the browser *launch* once; scenario assertions are never
  retried.
- **Test layers.** The suite's testing vocabulary — the hermetic / composed /
  live / manual layers, what each may touch, the single `//go:build live`
  mechanism, and the ban on `t.Skip` outside live-tagged files — is the contract
  `root project/design/D23.md`, cited and not restated here. (The `root` prefix
  matters: this tree has its own local D23.) sites' own layer facts — hermetic
  plus composed in the default gate, no live layer, no manual runbook, and the
  `google-chrome` and `go`-on-`PATH` preconditions — are recorded in D31.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Migrations are timestamped and immutable.** Schema lives under
  `sites/internal/db/migrations/`, applied forward-only by the appkit runner and
  keyed per file. A committed migration is **frozen** — a schema change is a
  **new** migration created with `bin/create-migration sites <name>` (which stamps
  a UTC `YYYYMMDDHHMMSS_<slug>.sql` version); never hand-name or edit one. Every
  site is **live customer data**, so every new migration is **additive**: the
  version plane's two columns arrive by `ALTER TABLE … ADD COLUMN` with defaults
  and carry every existing row forward untouched (D15). Every previously
  committed migration stays frozen.
- **Module wiring:** `appkit`, `eventplane`, and `registry` are committed in-repo
  replace-siblings. sites resolves its own port, the dropbox mirror address, and
  the repos base address by name through `registry` (D9/D32). No `agentkit`
  dependency (D10/D11): confined
  file-tool logic lives in the native `internal/files` package. Two **test-only**
  dependencies (pure Go, no cgo, imported only from `*_test.go`, linked into no
  shipped binary — enforced mechanically by D23's import-graph id):
  `github.com/dop251/goja` (an ES engine: the landing page's client JavaScript
  `share/www/static/landing.js`, D22, is written as pure functions and exercised
  by loading the real shipped file into goja from a Go test) and
  `github.com/chromedp/chromedp` (drives the headless Chrome for D23's single
  browser wiring test over the DevTools protocol — no node/npm toolchain; see
  `project/research/research.md`).
- **The chassis owns the server.** sites is `appkit.Main(appkit.Spec{…})`:
  `App:"sites"`, `Mount:"/srv/sites/"`, `Port:registry.MustPort("sites")` (== 3004),
  `MCP:true`, `WWW:true` (chassis loads/serves the `share/www` landing template and
  `/static/` assets), `Migrations:db.FS`, and one `Consumers` entry for the
  `repos` upstream (D35). sites is **not** an event-plane producer (no `/feed`)
  but **is** a consumer; its MCP `reflection` reports an empty `publishes` and
  the one repos subscription (D13). The fixed
  verbs, config-from-env, the loopback server + PRM + identity gate, the
  `appkit/mcp` transport, and the `appkit/web` render/static mechanism are
  appkit's. main.go declares sites's identity (the Spec) and wires its surface
  through the `Spec.Handlers` hook: the landing route (`GET /{$}`), the site-serving
  routes (`GET /public/`, `GET /private/`), and the `POST /mcp` mount. The hook is
  also where the two injected outbound clients (dropbox mirror, D28; version
  plane, D32) and the background seeding pass (D37) are constructed.
- **The chassis also owns correlation and telemetry.** Reading-or-minting the
  `X-Correlation-Id` on every inbound request, recording inbound MCP and plain
  HTTP traffic, emitting `lifecycle` records at boot and graceful shutdown, and
  the **instrumented outbound HTTP client the Router hands out** (`rt.HTTPClient(…)`)
  are all appkit's, proven by appkit's ids and never re-proven here. sites' own
  obligations are exactly two: **every** outbound client it holds — the dropbox
  mirror (D28) and the version-plane client (D32) — is that Router-provided
  client, injected at the composition root and reached by the live request
  context; and its nginx fragment hands the process a trustworthy id
  (D29). Since sites is not an event-plane producer, the
  `eventplane` `Append` change that carries `correlation_id` cannot reach its
  source; adopting the new libraries is a recompile.
- **nginx is the sole trust boundary.** sites runs no token/session logic and
  binds `127.0.0.1` only. Every `/srv/sites/` request is gated (or not) at nginx,
  which forwards to the loopback service. **nginx serves no site bytes off disk** —
  it `proxy_pass`es both the public and private site paths to the sites process
  (there is no `alias`); this is the core change from the earlier disk-served
  design. The site-serving Go routes are therefore mounted **ungated in-process**,
  exactly as `POST /mcp` relies on nginx for its bearer gate.
- **Two front doors, two audiences.** Humans in a browser are gated by the
  dashboard login-session cookie (`auth_request /_session-authn`); agents/MCP
  clients by an opaque bearer (`auth_request /_authn`). The landing page and the
  **private** site tier are cookie-gated; the **public** site tier is
  unauthenticated (and serves **unlisted** sites too — their protection is the
  generated unguessable name, D16/D27, not an auth check); the `/mcp` endpoint
  is bearer-gated.
