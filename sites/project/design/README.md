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
> nginx fragment (`sites/etc/nginx.conf`). All of these live under `sites/`;
> nothing outside `sites/` is named or changed. Cross-service facts (the dashboard
> session validator `/_session-authn`, the dashboard apex login-bounce named
> location `@login_bounce`, the dropbox mirror, the shared `registry`) are fixed
> external contracts this design consumes.

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
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Migrations are timestamped and immutable.** Schema lives under
  `sites/internal/db/migrations/`, applied forward-only by the appkit runner and
  keyed per file. A committed migration is **frozen** — a schema change is a
  **new** migration created with `bin/create-migration sites <name>` (which stamps
  a UTC `YYYYMMDDHHMMSS_<slug>.sql` version); never hand-name or edit one. The
  name/slug split adds one new migration that **rebuilds** `sites` with a
  `slug` primary key and a required `name` display-label column, carrying no
  rows across (no production data; D15); every previously committed migration
  stays frozen.
- **Module wiring:** `appkit`, `eventplane`, and `registry` are committed in-repo
  replace-siblings. sites resolves its own port and the dropbox mirror address by
  name through `registry` (D9). No `agentkit` dependency (D10/D11): confined
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
  `/static/` assets), `Migrations:db.FS`. sites is **not** an event-plane producer
  (no `/feed`); its MCP `reflection` reports an empty event graph (D13). The fixed
  verbs, config-from-env, the loopback server + PRM + identity gate, the
  `appkit/mcp` transport, and the `appkit/web` render/static mechanism are
  appkit's. main.go declares sites's identity (the Spec) and wires its surface
  through the `Spec.Handlers` hook: the landing route (`GET /{$}`), the site-serving
  routes (`GET /public/`, `GET /private/`), and the `POST /mcp` mount.
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
owner-id conversion (`docs/owner-id-design.md`) sites stores the stable
`owner_id` beside the display `owner_email`; sites owner-scopes no query (the
`slug` is the global handle and `list`/the landing page show every site), so
neither column is read for logic — they are captured and displayed only. There
is no lifecycle flag. The database is the single source of truth for which
sites exist, what they are called, and their visibility; the on-disk folder
location mirrors it in lockstep (the MCP tools are the only writer). See D15;
the token generator is D27.

## Filesystem layout

A site's files live **directly** at its served location — there is no working
tree and no symlink indirection. There are exactly **two** parents (matching the
two nginx locations); the three visibilities map onto them:

- `SITES_ROOT/public/<slug>/**` — a public **or unlisted** site's files
  (unlisted serves through the same ungated tier; its token name is the
  credential).
- `SITES_ROOT/private/<slug>/**` — a private site's files.

`SITES_ROOT` defaults to `/opt/sites/state/www`. `Layout.SiteDir(v, slug)` (with
`Seg(v)` mapping unlisted → `public`) is the single path helper. A visibility
change renames/relocates the directory in lockstep with the row, including the
token rename on transitions into unlisted. See D16.

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

Testing is part of the architecture. The cross-cutting approach:

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
  landing/PRM/mcp/`@sites_authn_500` locations remain (D4's ids).
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

**Service packages.** `internal/sites` (slug/visibility store + `Layout.SiteDir`),
`internal/serve` (the in-process static server, D17), `internal/files` (confined
filesystem ops, D10), `internal/mcp` (the domain tool table over the `appkit/mcp`
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
