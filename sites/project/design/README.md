# sites — Design

**Authority: shape and its proof.** This document and the `project/design/`
directory it heads own *how* sites is built and *how each behavior is proven*.
The product (`project/product/README.md`) owns the *why*, *for whom*, and the
user-facing promises; design states the **exact, checkable form** of those
promises and never re-declares the why. Design *uses* the product's contractual
constants by value (every site carries an owner-chosen display name distinct
from its slug; a site's visibility is exactly one of public, private, or
unlisted; an unlisted site's URL is its credential; a site that exists is
served; sites serves every byte under its mount; the visibility gate is
nginx's; the landing page is session-gated and shows version + site list; the
visual system is Carbon) but does **not** own them. This is the single, current statement of the
architecture — it is rewritten in place to stay true (stale decisions are
removed, not stacked); construction history lives in git, not here.

> **Scope.** This design covers sites' whole current surface: the slug/visibility
> domain (`internal/sites`), the in-process static server (`internal/serve`), the
> confined file tools (`internal/files`), the MCP tool table (`internal/mcp`), the
> embedded landing page (`share/www`), the migration set (`internal/db`), and the
> nginx fragment (`sites/etc/nginx.conf`), and the version-plane client and push
> consumer (`internal/sites`, D32–D38). All of these live under `sites/`;
> nothing outside `sites/` is named or changed. Cross-service facts (the dashboard
> session validator `/_session-authn`, the dashboard apex login-bounce named
> location `@login_bounce`, the dropbox mirror, the **repos loopback surface and
> its `push` events**, the shared `registry`) are fixed external contracts this
> design consumes.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior.
- The ids live inline in these Verification lists and nowhere else — there is
  **no separate requirements document**.
- Design's responsibility for ids ends at minting them into this doc. How
  coverage is measured, what counts as a covered id, and when the work is "done"
  are **not** design's concern — downstream phases own that.

## Conventions

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

## Data model

`sites` is one row per hosted site, keyed by `slug`. The row is: `slug` (PK —
the URL handle: owner-chosen for public/private sites, the generated 30-char
token for unlisted sites, D27), `name` (TEXT NOT NULL — the owner-chosen
free-form display label, validated by `ValidateName`; never touched by a
visibility transition, changed only by `rename`), `visibility` (TEXT,
CHECK-constrained to exactly `public`/`private`/`unlisted` — the retired
`public` boolean is gone), `owner_id` (TEXT NOT NULL — the stable owner key
`X-Owner-Id`, captured at create), `owner_email` (TEXT NOT NULL — a write-once
display snapshot of the creator email), `source_path` (TEXT, nullable —
dropbox-sync provenance, unchanged), `created_at`, `updated_at`. Per the suite
owner-id conversion (`project/design/D17.md` at the repo root) sites stores the stable
`owner_id` beside the display `owner_email`; sites owner-scopes no query (the
`slug` is the global handle and `list`/the landing page show every site). The two
owner columns are displayed in listings and, since the version plane, are the
identity sites asserts on repos' owner-scoped verbs (D32) — the one place sites
reads an owner for logic, and the reason the callerless seeding and consumer
paths have a truthful owner to present. Two
version-plane columns ride along: `repo_sha` (the commit the served copy is
materialized at) and `repo_seeded` (whether the site has been brought into the
plane) — and a `path` column carries the site's **publish root**: the
repository subfolder the site serves (`''` = the repository root; D38). There
is no lifecycle flag. The database is the single source of truth
for which sites exist, what they are called, and their visibility; the on-disk
folder location mirrors it in lockstep. What a site *contains* is the
repository's — the database records only where the copy stands (`repo_sha`). See
D15; the token generator is D27; the plane is D32–D37.

## Filesystem layout

A site's files live **directly** at its served location — there is no working
tree and no symlink indirection. That tree is the **materialized copy** of the
site's repository at the tip of `main` (root `project/design/D24.md`): plain
exported files, never a checkout, so no VCS metadata is ever under a served path
and neither the static server nor the file tools carry a `.git` deny rule. There
are exactly **two** parents (matching the two nginx locations); the three
visibilities map onto them:

- `SITES_ROOT/public/<slug>/**` — a public **or unlisted** site's files
  (unlisted serves through the same ungated tier; its token name is the
  credential).
- `SITES_ROOT/private/<slug>/**` — a private site's files.

`SITES_ROOT` defaults to `/opt/sites/state/www`. `Layout.SiteDir(v, slug)` (with
`Seg(v)` mapping unlisted → `public`) is the single path helper. A visibility
change renames/relocates the directory in lockstep with the row, including the
token rename on transitions into unlisted. See D16.

## The version plane

Site content is git-backed: repos holds one bare repository per site, keyed
`sites/<slug>`, and everything sites serves is a copy of that repository's `main`
(root `project/design/D24.md`, cited and not restated). sites' half is seven
Decisions: the injected `VersionClient` seam (D32); the write path — commit
first, then apply to the copy in the same tool call (D33); `sync` as one batch
commit (D34); the `repos:push` consumer that re-materializes a site when `main`
moves under it (D35); the create/delete/slug-rotation lifecycle (D36); the
additive, re-runnable seeding pass that brings pre-plane sites in (D37); and
the **publish root** — a per-site `path` selecting the repository subfolder
that is served, with `set_path` as its mutator (D38). Serving,
visibility, and nginx routing are unchanged by all of it.

## In-process static serving

`internal/serve` is a sites-owned `http.Handler` that serves the two site trees
from `SITES_ROOT` over the loopback server, mounted at `GET /public/` and
`GET /private/`. It serves real files (no symlink layer), maps a directory to its
`index.html`, returns `404` (never a listing, never `403`) for a directory with
no index or a missing path, confines every path under the site dir via
`internal/files.ConfinePath` (an escape is `404`), and 301-redirects a directory
request that lacks a trailing slash. It is distinct from the chassis `/static/`
mount (which serves the service's *own* Carbon UI assets from `share/www`). See
D17.

## Testing strategy

Testing is part of the architecture. Every approach below is **hermetic** in the
sense of `root project/design/D23.md` — temp-dir filesystems, real SQLite through
the real migration runner, `httptest`, in-process JS evaluation, committed-file
reads, and local subprocesses (`go list`, a headless browser on loopback) — with
the single exception of the **composed** boot smoke in `cmd/sites/main_test.go`,
which builds and runs sites' own binary. The layer names are the contract's; D31
records which layers this tree has. The cross-cutting approach:

- **The static server is tested over a temp `SITES_ROOT` with
  `net/http/httptest`.** Tests build a real directory tree under a `t.TempDir()`
  root, construct the `internal/serve` handler over it, and drive it with
  `httptest` requests, asserting status, body, `Content-Type`, the index.html
  mapping, the missing-index `404`, the traversal `404`, and the trailing-slash
  redirect. No network, no running suite.
- **The domain store is tested over a real migrated SQLite DB.** `internal/sites`
  tests open an in-memory/temp DB via `appkit/db`, run the migration set, and
  assert `Create` persists slug, name, `owner_id` + `owner_email` (distinct
  even for a shared email) and stores each of the three visibility values
  verbatim, `SetVisibility` updates the visibility (and the slug, when a
  re-slug rides along — never the name) in one step, `Rename` changes only the
  display name, and the final schema has the `slug` PK, the NOT NULL `name`,
  the CHECK-constrained `visibility` column, and lacks
  `public`/`created_by`/`tier`/`published`/`published_at` (via
  `pragma table_info`, with the CHECK proven by a rejected INSERT). The
  migration assertions run against the **real** SQLite the runner uses, not a
  fake — the substrate that actually enforces the column set.
- **The MCP tool table is tested at the handler boundary.** Tests inject the
  Identity headers (`X-Owner-Id` plus `X-Owner-Email`) and assert the tool set
  contains no `publish`/`unpublish`, that `create` requires the stated
  visibility and a valid display name at every visibility, and enforces the
  slug invariant (slug required for public/private, forbidden for unlisted —
  where the generated token comes from the injectable source), that `create`
  records the request Identity's `owner_id` and `owner_email` (the stable id
  captured even when two callers share an email), that `set_visibility`
  realizes the full transition matrix (re-slug-into-unlisted rotation,
  `new_slug` required when leaving unlisted, the name untouched everywhere)
  with the folder moved and the returned `url` reflecting the new state, that
  `rename` changes only the display name, and that the file
  tools/`sync`/`delete` operate on `SiteDir(site.Visibility, slug)`.
- **The landing surface is tested over the repo-real `share/www` tree.** Tests
  load the shipped tree with `appkit/web.Load`, render `landing.html` with a fixed
  version and a fixed slice of sites, and assert the version card plus one row per
  site (the display name as the linked row identity, the verbatim visibility
  label, creator, created-at — the slug travels only in the data island), and
  that an empty slice still renders. The same substrate proves the D22 additions structurally: the JSON
  data island's shape and URL-parity (D19), and the control layout — filter bar
  above the table, pager below it, hidden-until-JS with a stylesheet that makes
  `hidden` actually hide, sort hooks and `aria-sort` affordance CSS (D6).
- **The landing page's client JavaScript is tested in two tiers, each covering
  the other's blind spot.** **goja owns the logic (broad, cheap):** a Go test
  reads `share/www/static/landing.js`, evaluates it in `github.com/dop251/goja`
  (which has no `document`, so only the pure definitions run and the DOM
  controller stays inert), and calls the exposed
  `SitesLanding.{filterSites,sortRows,paginate,nextSort,defaultState,reduce,computeView}`
  against fixed inputs — proving fuzzy-filter semantics, sort order and the
  toggle rule, pagination arithmetic, the state reducer, and the view-model
  derivations against the code that actually ships (D22). **A single headless
  browser proves the wiring (narrow, minimal — D23):** one chromedp-driven
  Chrome session loads a seeded, auth-free `httptest` render of the real landing
  page and touches each interactive control exactly once — boot/unhide, type a
  fuzzy query, click a sort header, Clear, page Next/Prev — proving
  `initController` connects the goja-tested logic to a live DOM. Logic
  boundaries are never re-proven in the browser; wiring is never "proven" by a
  structural assert or a DOM mock.
- **The nginx fragment is proven by content assertion.** A test reads
  `sites/etc/nginx.conf` and asserts the public tier `proxy_pass`es to
  `…/public/` with no `auth_request`, the private tier gates with
  `auth_request /_session-authn` and `proxy_pass`es to `…/private/`, neither
  contains `alias` nor references the on-disk state path, and the pre-existing
  landing/PRM/mcp/`@sites_authn_500` locations remain (D4's ids). D29's
  correlation-header lines extend that same test under their own ids, scoped to
  what a content read genuinely shows — the directives present, on the right
  locations, in the right form. Whether a real minted id arrives is not
  claimable here; it needs a live nginx plus dashboard introspection, outside
  `sites/`.
- **Outbound HTTP is proven at the injected client, not by re-asserting the
  chassis.** The instrumented client comes off the Router (`rt.HTTPClient(…)`)
  and is injected into the mirror client at the composition root, so sites' tests
  supply a `*http.Client` whose `Transport` is a recording `RoundTripper` and
  assert the two things that are sites': every request goes through the injected
  client, and the live request context reaches it (the transport reads the
  correlation id off `req.Context()`). Setting the header and emitting the
  `outbound` record are appkit's behaviors with appkit's ids, never re-proven
  here — and no test needs to stand up a Router.
- **The version plane is tested against a contract-conforming, recording
  `httptest` repos server — and the honest limit of that is stated on the ids.**
  A fake accepts whatever it is handed, so no sites test claims that repos (or
  real git) accepts sites' requests; root `project/design/D24.md` assigns
  custody, the git door, and commit acceptance to **repos**' own tree, and
  building or running repos from sites' suite would breach the scope boundary.
  What the fake *can* falsify is everything that is sites': that the call is made
  at all, exactly once, with exactly the right change set and bytes; that the
  local copy is written only after the commit succeeds and not at all when it
  fails; that a push event re-materializes, is ignored on a non-`main` branch,
  and is skipped when the sha is sites' own; and that a malformed export is
  refused whole. Each id names the wrong implementation it kills. One id
  (R-ER9A-9UMP) is deliberately **composed** rather than hermetic, because the
  claim it makes — that the real composition root wires a real instrumented
  client — is exactly the claim a hermetic test constructing its own client
  cannot make.
- **Determinism.** Handlers take their inputs explicitly (name/version strings,
  the site slice, the `SITES_ROOT`), so output is determined by inputs — no clock,
  no network.

## Layout

The design is split for addressability so a build phase reads only the one
Decision it realizes:

- `project/design/README.md` — this spine: static cross-cutting facts only.
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded;
  referenced in prose and the plan as `D<N>`).
- `project/design/INDEX.md` — the manifest: each Decision → its file, plus a
  sorted `R-id → Decision/file` reverse map; the grep target for resolving an id.

**Service packages.** `internal/sites` (slug/visibility store + `Layout.SiteDir`,
plus the version-plane client, the push materializer, and the seeding pass —
D32/D35/D37), `internal/serve` (the in-process static server, D17),
`internal/files` (confined filesystem ops, D10), `internal/mcp` (the domain tool
table over the `appkit/mcp`
transport, D13/D20), `internal/db` (the embedded migration set + load guard). The
landing page and Carbon assets live on disk in `sites/share/www/` served by the
chassis, including the landing page's client script `share/www/static/landing.js`
(D22, filter/sort/paginate). There is **no** working tree, no served-symlink
tree, and no `internal/web` package.

Design is **rewritten in place**, not append-only (construction history lives in
git): a changed Decision is rewritten in its `DNN.md` and `INDEX.md` is
regenerated; a new Decision adds a `DNN.md` and an INDEX entry. Existing
`R-XXXX-XXXX` ids are stable handles — never renumbered; a newly added behavior
gets a freshly minted id, and a removed behavior's id is deleted with it.
