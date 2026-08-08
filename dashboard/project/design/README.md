# dashboard — Design (web surface & sign-in)

**Authority: shape and its proof.** This document and the `project/design/`
directory it heads own *how* the dashboard's web surface, sign-in, and
introspection edge are built and
*how each behavior is proven*. The product (`project/product/README.md`) owns the
*why*, *for whom*, and the user-facing promises; design states the **exact,
checkable form** of those promises and never re-declares the why. This design
covers five bodies of work in the dashboard: (1) the **web surface** — splitting
the single hybrid apex page into a login page, a landing/home page, and a new
profile page (D1–D6), a diminished name-origin colophon on the login page (D7),
the shared banner chrome (D10), and a new owner-only **metrics** page that
samples box resource health in memory and graphs the last 24 hours (D11–D16 —
named *metrics* throughout, page and package, so the word *telemetry* belongs
solely to the suite's forensic record service);
and (2) the **identity model** — moving the dashboard's concept of user identity
from email to the OIDC subject pair `(iss, sub)` behind an opaque local handle,
capturing name/picture at login, enforcing the artifact→identity link as a real
foreign key (D23–D24), and emitting the full identity header set from the
introspection endpoints on every allow, fail-closed, sourced from the
identities row (D17–D19, D24); and (3) the
**login-bounce contract** — a dashboard-owned apex nginx primitive
(`@login_bounce`) that redirects a logged-out *navigation* to a session-gated
`/srv/<svc>/` page into `/login` instead of a bare 401, while leaving scripted
`fetch`/XHR with a clean 401 (D20), plus the web-sign-in plumbing that carries a
validated same-site `return_to` from `/login` through the handshake and back
(D21–D22) so the visitor lands where they were headed; and (4) the **second
sign-in method** — "Sign in with GitHub" for active members of the ikigenba
GitHub organization, a permanently separate identity from Google sign-in: a
sibling GitHub provider package (D25), provider-bound handshakes (D26), the
`/login` chooser with per-provider start routes (D27, with the two-CTA login
composition in D7), the GitHub callback and its org-membership federation gate
(D28), and the provider chooser inside the single MCP authorize endpoint (D29);
and (5) the **suite telemetry edge** — the dashboard is the point every gated
`/srv/<svc>/` request passes through, so its introspection endpoints mint the
suite's per-request **correlation id** and return it to nginx (D30), emit an
`edge` telemetry record for every gated auth decision including denials and
rate-limited attempts (D31) while the durable internal audit log stays exactly as
it is (D32), and the dashboard's own apex nginx fragment blanks
client-supplied correlation ids and forwards the original request method (D33);
and (6) **suite-contract conformance** — the dashboard adopts the umbrella
project's two `[proof: per-service]` contracts, the `/opt/<svc>/` install layout
and the portable authored `manifest.env`, citing them by Decision path and stating
only where its local proof lives (D34); and (7) **testing-language conformance** —
the dashboard adopts the suite's testing-language contract, declaring its own
layers, preconditions, and GOWORK mode and proving that declaration in its own
tree (D37).
It is rewritten in place to stay true (stale decisions are removed, not
stacked); construction history lives in git, not here.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior.
- The ids live inline in these Verification lists and nowhere else — there is **no
  separate requirements document**.
- Design's responsibility for ids ends at minting them into this doc. How coverage
  is measured and when the work is "done" are **not** design's concern — downstream
  phases own that.

## Conventions

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
  forwarding) are proven by content-assertion.** `dashboard/etc/nginx.conf` is a
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

## The apex is the exception — it gates its own session in-process

The suite-wide landing-page change cookie-gates each **path-routed** service's
landing page at nginx via the dashboard-owned `/_session-authn` `auth_request`.
**The dashboard is not gated that way.** It is the apex/`DEFAULT=true` app behind
appkit's Apex bypass: it **owns and issues** the `dashboard_session` cookie and
runs **no** `auth_request` in front of its own routes. So every session decision
for the dashboard's own pages is made **in-process**, exactly as the existing
index does — by reading the `dashboard_session` cookie and looking it up in the
web-session store (`a.sessions.Lookup`, surfaced through `(*app).sessionOwner` /
`(*app).requireSession`). The new profile page reuses that same in-process seam;
it does **not** introduce an nginx fragment, and `/_session-authn` (which other
services consume) is untouched by this change.

## The four pages and the routes that serve them

| Page | Route | Audience | Gate |
|---|---|---|---|
| **Login** | `GET /` with no/invalid session | logged-out human | none (public) |
| **Landing / home** | `GET /` with a valid session | logged-in owner | session (in-process branch) |
| **Profile** | `GET /profile` | logged-in owner | session (in-process, redirect if absent) |
| **Metrics** | `GET /metrics` (+ `GET /metrics/fragment`) | logged-in owner | session (in-process, redirect if absent) |

`GET /` stays **one** handler that branches on the session (login vs landing) —
its split is by *audience on the same route*, the behavior that exists today.
The profile and metrics pages are each a **new route** with a **new handler**
and its own template; metrics adds a second route (`/metrics/fragment`) that
returns just its charts block for the once-a-minute client poll (D14).

## Testing strategy

Testing is part of the architecture. The cross-cutting approach every Decision's
Verification list assumes:

- **The layer vocabulary is the suite's, not this tree's.** Every test below
  belongs to exactly one layer — **hermetic**, **composed**, **live**, or
  **manual** — as defined by `root project/design/D23.md`, which owns the layer
  definitions, the `//go:build live` mechanism, and the ban on skipping outside
  live-tagged files. D37 records the dashboard's adoption and its declared facts.
  In short: everything below is **hermetic** except the boot smoke in
  `cmd/dashboard/main_test.go`, which is **composed**, and the interactive
  provider sign-in, which is **manual**. The dashboard has **no live layer**.
- **HTTP-level tests against the standalone server (hermetic).** The in-package tests drive
  the real route table via `(*app).routes()` (the `server` package's existing test
  harness — see `internal/server/index_test.go`, `grants_test.go`,
  `login_test.go`), issuing requests with `httptest` and asserting status codes,
  `Location` headers, and rendered HTML. New tests are co-located in
  `internal/server/*_test.go`, `package server`, named for the behavior asserted.
- **Session is a real store on a temp DB; identity is injected.** The web-session
  store runs against a real temp `modernc.org/sqlite` migrated by the appkit
  runner, exactly as the existing server tests construct it. A request is "signed
  in" by minting a session and presenting its cookie; "signed out" by omitting it.
  No live network, no real Google IdP.
- **Render assertions, not screenshot diffs.** "The landing has no PAT form" is
  proven by asserting the rendered `GET /` body omits the PAT-create markup;
  "profile renders the grants block" by asserting the `GET /profile` body contains
  it. Redirect targets (`/profile` vs `/`) are proven by asserting the `Location`
  header on the 3xx response.
- **Doc truth is a hermetic Go test like everything else.** The `AGENTS.md` claim
  (D6) is a fixed-substring check — the stale rules absent, the four-page truth
  present — and it runs as an ordinary test in `cmd/dashboard/docs_test.go`
  reading the committed file from disk, so the id has a tag site and the doc is
  re-checked on every `go test ./...`, not once at authoring time. D37's
  testing-facts declaration and its skip-ban source scan are proven in that same
  file, by the same read-from-disk means.
- **Metric sources are tested hermetically against fixtures; the two that hinge on
  a real OS contract get a real-substrate check (still hermetic).** The `/proc/meminfo` parser runs
  against a fixture reader; the cgroup reader and the `du` walk run against temp
  trees at an injected root (including the absent-path → 0 case); service discovery
  runs against a temp manifest root (asserting the dashboard, which lacks
  `MCP=true`, is excluded). Free-disk reads a **real** `statfs` — a claim that only
  the kernel can falsify — so its id drives the syscall against a real temp path,
  not a stub, and asserts a plausible non-zero total. The collector's lifecycle
  (immediate first sample, per-tick sampling, clean return on `ctx` cancel,
  error→0+log) is tested with fake sources and a short injected interval.
- **Identity is tested against a real temp DB; Google is injected.** The
  `internal/identity` store runs against a real temp `modernc.org/sqlite`
  migrated by the appkit runner — the substrate that actually enforces the
  `UNIQUE (iss, sub)` upsert and the schema (D17). The `ids.New()` handle source
  is injected so a test can assert the handle's provenance. The claim decode
  (D18 part A) drives `googleidp`'s existing token-construction test seam
  (`internal/googleidp/googleidp_test.go`) with payloads that include and omit
  `name`/`picture`; no live Google — the id_token is crafted, as the existing
  googleidp tests already do. Stamping (D18 part B) and header emission (D19) are
  HTTP-level `server`-package tests: a request is driven through the callback /
  introspection routes, then the persisted `owner_id` is read back from the temp
  DB (stamping) or the introspection **response headers** are asserted directly
  (emission) — including that existing headers are unchanged, that a Unicode /
  CR-LF attribute emits ASCII+injection-safe and round-trips on decode, and that
  an empty/absent identity never turns an allow into a deny.
- **GitHub is tested at two seams, no live network.** `internal/githubidp`'s
  live impl runs against `httptest` fakes of the two GitHub bases (the token
  endpoint's 200-with-`error` contract, `/user`, `/user/emails`, the
  membership endpoint's 404-means-none contract) through the injectable
  `webBase`/`apiBase` roots (D25); the `server`-package tests drive the GitHub
  routes/callback with the `githubidp.NewStub()` double, exactly the pattern
  the Google pair uses. Both seams are **hermetic**. An interactive login is a
  browser handshake automation cannot drive even with credentials, so the real
  GitHub contract is captured in `project/research/research.md` and exercised at
  deploy time as a **manual-layer** check — the same precedent as Google.
- **The telemetry edge is tested against the real handler and a real in-process
  sink — hermetic throughout, despite reaching a socket (loopback only).** The
  minting and edge-record claims (D30, D31, D32) are driven through the **real**
  introspection handlers — `httptest` requests against `(*app).routes()`, the
  same harness the rest of the `server` package uses, with a real temp DB behind
  them — because the seam under test *is* an HTTP handler and its response
  headers. Nothing about the decision path is stubbed. Anything that hinges on a
  record actually being **delivered** is asserted against a **live in-process
  HTTP sink**: an `httptest.Server` standing in for the telemetry service's
  `POST /ingest`, wired as the recorder's real ingest target, flushed before the
  assertion, with the test reading the decoded JSON the sink received. A stub
  recorder would accept whatever it was handed and prove nothing left the
  process, so it is not the substrate for those ids; the unreachable-sink case is
  driven against a **closed** listener, which is the only way to falsify the
  best-effort promise. Both remain hermetic: the sink is `httptest` on loopback,
  never a real telemetry service. The nginx-fragment claims (D33) are file-content
  assertions per the Conventions above.
- **Chart rendering is pure and tested hermetically on geometry, not pixels.** The SVG
  builders are pure functions of a `Store` snapshot; tests assert computed
  coordinates and structure (hero y-axis mapped `0 → total capacity`; stacked band
  height at each x equals the sum of service values; the long tail folds into one
  "Other" band; a legend names every band; each visible band gets a distinct
  palette color) and the `humanBytes` binary-unit formatter, plus that the served
  `app.js` wires the 60s metrics poll. The categorical band palette was validated
  colorblind-safe with the dataviz validator when authored and is committed as a
  fixed ordered set; the suite asserts the render *uses* it (distinct color per
  band + legend), not that CI re-runs the validator.

## Layout

The design is split for addressability so a build phase reads only the one
Decision it realizes:

- `project/design/README.md` — this spine: static cross-cutting facts only, no
  per-Decision detail.
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded:
  `D01.md`, `D02.md`, …; referenced in prose and the plan as `D<N>`).
- `project/design/INDEX.md` — the manifest: each Decision → its file, plus a
  sorted `R-id → Decision/file` reverse map. It is the grep target for resolving
  an id.

Design is **rewritten in place**, not append-only (construction history lives
in git): a changed Decision is rewritten in its `DNN.md` and `INDEX.md` is
regenerated.
Existing `R-XXXX-XXXX` ids are stable handles — never renumbered; a newly added
behavior gets a freshly minted id, and a removed behavior's id is deleted with it.
