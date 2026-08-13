# dashboard — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module dashboard` rooted
  at `dashboard/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo). `appkit`
  and `eventplane` are committed in-repo replace-siblings (`replace appkit =>
  ../appkit`, `replace eventplane => ../eventplane`).
- **Schema.** The web-surface work (D1–D16) touches **no** schema: it is a pure
  HTTP-routing + template + view change under `dashboard/internal/server/` and
  `dashboard/ui/`, plus one in-memory package `dashboard/internal/metrics/`
  whose history lives only in RAM (ring buffers) and is never persisted. The
  **telemetry edge** work (D30–D33) touches no schema either: it is a change to
  the three introspection handlers, a one-method recorder seam, and the apex
  nginx fragment. The
  **identity model** work (D17–D19, D23–D24) *does* add schema: forward-only
  migrations — a new `identities` table (D17), an `owner_id TEXT` column on
  the four auth-artifact carrier tables (`web_sessions`, `oauth_authcodes`,
  `oauth_chains`, `personal_tokens`) (D18), the purge + `NOT NULL` rebuild of
  those carriers (D23), and a rebuild-with-copy declaring
  `owner_id … REFERENCES identities(id)` on all four (D24). Each is created
  with `bin/create-migration dashboard <name>` (timestamped, immutable) and
  applied by the appkit runner; committed migrations are never edited. The
  **login-bounce** work (D20–D22) adds one more such migration — a nullable
  `return_to TEXT` column on `oauth_state` (D21), mirroring how
  `005_oauth_state_mcp.sql` added the MCP columns. The **GitHub sign-in** work
  (D25–D29) adds exactly one migration — a
  `provider TEXT NOT NULL DEFAULT 'google'` column on `oauth_state` (D26) — and
  touches no other schema: GitHub identities are ordinary `identities` rows
  under the existing `(iss, sub)` key.
- **The apex nginx `server` block is the dashboard's, and its changes (D20's
  login-bounce primitive, D33's correlation-id blanking and original-method
  forwarding, D39's http-level `variables_hash_max_size`) are proven by
  content-assertion.** `dashboard/etc/nginx.conf` is a
  server-block fragment `opsctl init-box` installs; lines added to it are
  verified by a **hermetic** Go test that **reads the file from disk** and asserts
  its content (the same pattern the sibling `sites` service uses for its fragment)
  — nginx itself is never run by the gate, so the live 302/401 routing and the
  live header handling are **manual-layer** checks at deploy time. The **dev front door**
  `nginx/nginx.conf` (repo root) carries a mirror of the login-bounce primitive
  for local testing, but it lives **outside this `project/` tree** and is
  maintained as suite infrastructure, never by this spec or its build loop.
- **Suite seams consumed as external fact.** Two shared seams land in sibling
  workspaces before this work builds and are consumed here, never re-implemented:
  the **correlation minter** (`eventplane/correlation` — a leaf package producing
  a bare 26-character Crockford-base32 ULID) and appkit's **telemetry recorder**
  (ring-buffered, batched, fire-and-forget `POST` to the telemetry service's
  `/ingest`, best-effort by contract). The recorder is **obtained from the
  `*appkit.Router`**, never constructed here — a Router-level accessor alongside
  `rt.HTTPClient(...)`, so the process has exactly one recorder and one
  dropped-record count. Most instrumentation arrives free through the chassis;
  the dashboard's `edge` records (D31) are the direct-emitter exception, because
  only the handler knows the decision. Their exact Go signatures are those
  workspaces' decisions; the dashboard depends on each through a one-method
  interface it owns (D30, D31) and satisfies at the composition root, so a
  signature change is a one-line adapter change and the dashboard's tests pin the
  *observable* contract (the emitted header format, the delivered JSON record)
  rather than a foreign symbol.
- **The suite's correlation header is `X-Correlation-Id`,** a bare 26-character
  Crockford-base32 ULID. The record shape, digest algorithm, capture thresholds,
  and ingest endpoint are the suite protocol's (see `project/research/research.md`
  for the parts the dashboard uses); this design owns only what the dashboard
  mints, returns, and records.
- **The metrics collector runs on the appkit `Workers` seam.** `appkit.Spec.Workers`
  is `[]func(ctx context.Context) error`; each worker runs on the serve context and
  a `ctx` cancel (SIGTERM/shutdown) unwinds it. `cmd/dashboard/main.go` follows the
  established capture idiom (`var rt *appkit.Router`; the `Handlers` hook sets it
  and constructs shared collaborators; the `Workers` closure captures it, running
  strictly after Handlers so `rt.Logger()` is live) — the same pattern
  `notify`/`dropbox`/`prompts` use for their consumer loops. The one metrics
  `Store` instance is constructed once at that composition root and shared between
  the collector worker (writer) and the `server` handlers (reader).
- **Linux metric sources are read through injected roots.** The collector reads
  `MemAvailable`/`MemTotal` from `/proc/meminfo`, free/total from `statfs` of the
  `/opt` filesystem, per-service memory from the cgroup-v2
  `<cgroupRoot>/system.slice/<svc>.service/memory.current`, and per-service disk
  from a directory walk of `/opt/<svc>`. Each path/root is a config field defaulted
  to the production value, so tests point them at fixtures/temp trees. On the box a
  missing per-service cgroup file or `/opt/<svc>` dir is a normal "unavailable"
  reading recorded as **0**; an *unexpected* read error is recorded as **0** and
  logged.
- **Build / typecheck command:** `cd dashboard && go build ./...` and
  `go vet ./...`. The production build adds `CGO_ENABLED=0 GOOS=linux GOARCH=amd64
  GOWORK=off` (driven by `bin/ship`).
- **Test command:** `cd dashboard && go test ./...`. **"The suite is green"**
  means: `cd dashboard && go build ./...`, `go vet ./...`, `gofmt -l .` (no
  output), and `go test ./...` all succeed with zero failures. Requirement-id
  tags live in Go test files matched by the glob `*_test.go`.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Server package:** the dashboard's whole apex route table is registered in
  `dashboard/internal/server/routes.go` (`(*app).register`), built over `*app`
  (fields in `server.go`). Templates are parsed once at startup via
  `template.ParseFS(ui.Files, …)` in `server.go`; a broken template fails startup,
  not a request. Static assets and the Carbon `tokens.css`/fonts are already
  embedded under `dashboard/ui/static/` and served at `/static/`.
