# sites — Product

**Authority: intent.** This document owns *why* sites exists, *for whom*, what is
in and out of scope, and what we **promise** the user — in outcome terms only.
Mechanism (the handler, the in-process static server, the DB schema, the nginx
fragment, the MCP tool shapes, the embedded landing template) and its checkable
proof live in `project/design/README.md`. Where the two touch observable
behavior, product states the *promise* and design states the *exact, checkable
form*; that boundary keeps product, design, and plan from overlapping.

> **History note.** sites originally shipped a **publish/unpublish lifecycle**: a
> site had an editable *working tree*, and "publishing" it created a symlink into
> a per-visibility *served tree* that **nginx served straight off disk** via
> `alias`. That machinery is being removed (it duplicated in the filesystem a
> fact the database already held, purely so nginx — which cannot read the DB —
> could serve it). This product doc states the **current** model; the publish
> lifecycle, the working tree, the served symlinks, and disk-serving by nginx are
> gone. History of the change lives in the plan.

## Problem

A single-box customer wants to host a handful of small static websites — a
marketing page, an internal runbook, a private scratch site — without standing up
separate hosting, and to manage them the same way they manage everything else on
the box: by talking to an agent over MCP. They also, occasionally, open the
service in a browser to see what sites exist and confirm the service is up.

The earlier design solved the hosting need but accreted complexity an agent
added: a create → edit-a-working-tree → **publish-to-a-tier** lifecycle, with the
published content represented as a **symlink tree served off disk by nginx**.
That is more moving parts than the job needs — the database already knows which
sites exist and whether each is public, so re-encoding that as an on-disk symlink
tree for nginx's benefit is redundant state that can drift.

## Purpose

sites is the box's **static-website host**. Each site is a folder of files with
two identities: a human **name** (a free-form label the owner chooses, which is
how the owner tells sites apart in any listing) and a **web address** (its
slug — the name and the address are not the same thing). A site has exactly one
of three **visibilities**: **public** (served to anyone, at an address the
owner chose), **private** (served only to a logged-in dashboard user), or
**unlisted** (served to anyone — but at a long, generated, unguessable address,
so the URL itself is the credential; the owner never chooses an unlisted site's
address). Every site keeps its owner-chosen name whatever its visibility — an
unlisted site is still identified by its name in the owner's list, never by its
random address. That name-plus-address pair and the visibility are the whole of
a site's state. The owner manages sites through the
`ikigenba_sites_*` MCP surface (create, edit files, change visibility, delete);
the **sites process itself serves the site bytes** over its loopback HTTP
server, behind the nginx front door. A human who opens the service root in a
browser gets a **landing page** that shows the service version and lists the
sites that exist.

## Users

- **The owner, through an agent (MCP).** Creates sites, edits their files, sets
  each site's visibility (public, private, or unlisted), and deletes them — all
  as MCP tool calls. This is the primary surface.
- **A logged-in dashboard user, in a browser.** Opens the bare `/srv/sites/` root
  and sees the landing page: the running version and the list of sites (name,
  visibility, who created it, when). The check is coarse — any logged-in
  dashboard user may view it.
- **A visitor to a public site.** Anyone on the internet who opens a public
  site's URL and is served its files. A **private** site's files are served only
  to a logged-in dashboard user.
- **A recipient of an unlisted link.** Someone the owner hands an unlisted
  site's URL — a client, a reviewer — who opens it and is served the files with
  no login. They can reach only what they were linked to; the unguessable name
  is what keeps everyone else out.
- **The operator, confirming a deploy.** Opens the root after a deploy to confirm
  sites is up and which version is live.

## Scope

sites does this and only this:

- **Host static sites in three visibilities.** A site is served according to
  exactly one visibility the owner sets: **public** (served with no
  authentication, at an address the owner chose), **private** (served only to a
  logged-in dashboard user, at an address the owner chose), or **unlisted**
  (served with no authentication, at a long generated address nobody can
  guess — the URL is the credential). There is no "unpublished/draft" state and
  no separate publish step — a site that exists is served.
- **Name every site.** Every site carries a free-form display name the owner
  states at creation and can change later — the label that identifies the site
  in listings, independent of its address and untouched by any visibility
  change.
- **Serve site bytes in-process.** The sites process serves the files for both
  visibilities from its own loopback server; nginx proxies to it and never reads
  the site files off disk itself.
- **Manage sites over MCP.** Create a site at an explicitly stated visibility —
  the caller always says which of the three it wants; there is no silent
  default — edit its files with the file tools, change its visibility, and
  delete it, through the `ikigenba_sites_*` tools. Content edits are
  **immediately live** (there is no working-copy/publish indirection).
- **Generate and rotate unlisted links.** Creating an unlisted site returns its
  long unguessable URL, ready to hand out. Setting an already-unlisted site to
  unlisted again **rotates** the link: a fresh unguessable URL replaces the old
  one, which stops working — that is how the owner revokes a link they no longer
  trust. The site's name is untouched by rotation, so the owner can always tell
  which site a fresh link belongs to. Moving a site out of unlisted requires
  the owner to give it a real address of their choosing.
- **Record who and when.** Each site records the owner who created it and the
  creation time, surfaced by the tools and on the landing page. Because `create`
  is the only way a site comes into being, **every site has a real creator** —
  there are no anonymously-imported sites.
- **Send logged-out visitors to sign in, then back.** For its session-gated
  browser surfaces — the landing root, the landing assets, and the private site
  tier — a logged-out person who navigates there is sent to the dashboard sign-in
  and returned to that page after signing in, instead of seeing a bare refusal.
  This is a front-door behavior sites opts into; scripted (non-navigation) and
  bearer requests are still refused as before, and the public tier and the bearer
  `/mcp` endpoint are deliberately left untouched.
- **Serve a landing page at the bare mount root.** A dynamic, session-gated page
  showing the service version and the list of existing sites (name, visibility,
  creator, created-at), styled with the suite's Carbon design system. The list is
  **browsable in the page**: a fuzzy search box filters by name or address, the
  name / created / creator columns sort (click a header to change direction), results
  **paginate past ten** rows, a single control clears the filter and ordering
  back to the default view, and each row carries a one-click control that copies
  that site's full URL to the clipboard.
- **Describe itself to a connecting agent.** The MCP surface is self-describing:
  its connection instructions name what sites is for in everyday words and point
  at a `guide` tool that returns the site model and worked examples, so an agent
  can discover and drive sites from the connection alone, with no external skill.
- **Import from a Dropbox mirror.** The `sync` tool reconciles a Dropbox-mirrored
  subtree into an **already-created** site's files (the site must be created
  first; `sync` never brings a site into being). It writes directly into the
  site's served folder and leaves the site's visibility unchanged.

It deliberately does **nothing else**. In particular it does not: keep any
publish/unpublish lifecycle, working tree, or served-symlink tree; let nginx
serve site files off disk; offer any "draft" or "offline-but-exists" state; run
any token or session logic itself (nginx is the sole trust boundary); or host
anything but static files.

## Contractual constants

Promised values the design must honor verbatim and never re-declare:

- **A site's name and its address are not the same thing.** Every site carries
  an owner-chosen free-form display name — the label that identifies it in any
  listing — and a separate web address. Public and private sites live at an
  address the owner chose; an unlisted site lives at a generated unguessable
  address the owner never chooses. The name survives every visibility change.
- **A site's visibility is exactly one of public, private, or unlisted** — never
  a spectrum, never a lifecycle. There is no fourth state.
- **An unlisted site's URL is its credential.** It is served with no
  authentication, exactly like a public site; what protects it is that its
  address cannot be guessed. Setting an unlisted site to unlisted again rotates
  the address, and the old URL stops working — the site's name is untouched.
- **A site that exists is served.** There is no publish step and no unpublished
  state: creating a site and putting files in it makes it live; deleting it takes
  it offline. Choosing the visibility at create, and changing it thereafter, are
  the only visibility controls.
- **sites serves every byte under its mount.** For any path under
  `/srv/sites/…`, the bytes come from the sites process — nginx proxies, it does
  not serve site files off disk.
- **The visibility gate is nginx's.** Public and unlisted site paths are
  unauthenticated (an unlisted site is served through the same ungated public
  tier — its protection is its unguessable name, not an auth check); private
  site paths are gated by the dashboard browser session
  (`auth_request /_session-authn`). The sites process runs no token/session logic
  and trusts the front door.
- **The landing page lives at the bare mount root only**, is gated by the
  dashboard browser session (not a bearer token), and shows the running version
  plus the list of sites. A failed session check yields `401` — which the apex
  front door turns into a redirect to the dashboard sign-in for a logged-out
  **browser navigation** (returning the visitor to the page after they sign in);
  a non-navigation request with no session still receives the `401`.
- **The visual system is Carbon.** `design/carbon.md` + `design/tokens.css` +
  `design/example.html` are the source of truth; sites embeds its own copy of the
  tokens and fonts.

## What we promise (user-facing behavior)

- **Creating a site and adding files makes it live** — with no separate publish
  step. The owner creates a site at a **stated visibility** — public, private,
  or unlisted; the call always says which — writes files into it, and the site
  is served at its URL immediately. Every site is created **with a name** the
  owner states. For public and private the owner also chooses the address; for
  unlisted the service generates the unguessable address and returns the URL
  ready to share.
- **A site is always identifiable by its name.** Whatever its visibility, a
  site keeps its owner-chosen name — in a long list of sites the owner can tell
  each one apart by name, unlisted ones included. The name can be changed at
  any time without affecting the site's address, visibility, or files.
- **An unlisted link can be handed out, and later revoked.** Creating an
  unlisted site (or setting an existing site unlisted) yields a long random URL
  anyone can open with no login — the owner shares it with exactly the people
  they choose. Setting the site unlisted again rotates the URL: the returned
  link is fresh and the old one stops serving, so a leaked link is one call from
  dead — and the site's name stays put through every rotation. Taking a site
  out of unlisted requires the owner to supply a real address for it.
- **An agent can learn sites from the connection alone** — on connecting, the
  instructions say what sites is for in the words a user actually uses, and a
  `guide` tool returns the site model, the rules, and worked examples (including
  creating a public page in one call and importing from Dropbox). No external
  skill or doc is needed to route work to sites or to drive its tools.
- **Public and unlisted sites are served to anyone; private sites only to a
  logged-in user.** Opening a public or unlisted site's path returns its files
  with no login (an unlisted site differs only in that its path cannot be
  guessed); opening a private site's path without a dashboard session is
  refused — a logged-out browser navigating there is sent to the dashboard
  sign-in and returned after signing in, and a non-navigation request receives
  `401`.
- **Changing a site's visibility changes who can reach it** — one tool call,
  and the site's URL and access change accordingly; it is never reachable under
  more than one visibility at once.
- **Deleting a site takes it offline** — its files stop being served and its
  record is gone. There is no lingering "unpublished" state; delete is how a site
  goes away.
- **The landing page lists the sites that exist** — a logged-in human opening the
  bare `/srv/sites/` sees the running version and a row per site showing its
  name, its visibility (public, private, or unlisted), who created it, and when.
  Each name is a link that opens that site; an unlisted site's row reads as its
  name, not as a wall of random characters.
- **A logged-out browser is sent to sign in, then returned.** A person who
  navigates to the landing page (or to a private site) without a dashboard
  session is taken to the dashboard sign-in and, on signing in, returned to the
  page they were headed for — rather than shown a bare refusal. A request that is
  not a browser navigation (a scripted `fetch`, a bearer API call) is still
  refused with `401`.
- **Agents are unaffected in how they connect** — the bearer-gated `/mcp`, the
  PRM well-known, and `/health` behave as before. The *tools* evolve: `create`
  states the visibility explicitly and always takes a name, a site's display
  name can be changed after the fact, `sync` requires the site to already
  exist, and the self-description tool is `guide` (returning worked examples)
  rather than `describe`.
- **The version on the page is the version actually running** — so the operator
  can confirm a deploy in a browser.
- **The landing list is browsable in place.** A logged-in user can **type in a
  search box to fuzzily filter the sites by name or address** (partial,
  out-of-order letters still match), **sort by name, created-at, or creator by
  clicking a column header** (clicking again reverses the direction), and **page through the
  results ten at a time** once there are more than ten. A single **Clear** action
  returns to the default view (no filter, newest-first, first page). Filtering,
  sorting, and paging happen **instantly in the browser** with no page reload;
  the default view is newest-first. This is a convenience for the human viewer —
  it changes nothing an agent sees over MCP.
- **Copying a site's URL from the listing is one click.** Each row carries a copy
  control that places that site's full URL — the same address its name links to —
  onto the clipboard, ready to paste, so the viewer never has to hand-select the
  link. Like the other in-browser conveniences it is a viewer aid only, present
  when JavaScript is on, and changes nothing an agent sees over MCP.

## Success criteria (outcomes)

Each is a result the owner, viewer, or operator can confirm against the running
service:

- As the owner I create a site, write an `index.html` into it, and immediately
  fetch its URL and get that page — with no publish step.
- As the owner I create a site **public in one call**, and a request with no
  dashboard session is immediately served its page; a site I create private is
  refused to that same session-less request (a browser navigation is sent to
  sign-in; a scripted/bearer request gets `401`). A `create` that does not state
  a visibility is refused — nothing is silently defaulted.
- As the owner I create an **unlisted** site by stating its name but no
  address, get back a URL whose final address segment is long and obviously
  random, and a request with no dashboard session is served its page at that
  URL — while nobody who lacks the URL can find it by guessing. In my site
  list that site appears under the name I gave it, not under the random
  address.
- As the owner I set an unlisted site to unlisted again and get back a **new**
  URL; the new URL serves the site, the **old URL stops serving it**, and the
  site's name in my list is unchanged — I can still tell which site it is.
- As the owner I take a site out of unlisted by giving it a real address of my
  choosing; a call that omits the new address is refused.
- As the owner I rename a site and its listing shows the new name while its
  URL, visibility, and files are exactly as before; a create or rename with a
  blank name is refused.
- As an agent I call `sync` for a site that does not exist yet and get a clear
  "not found — create it first" refusal, not a silently-created site.
- As an agent connecting to sites for the first time I can tell what it is for,
  and by calling `guide` I get worked examples that let me create and publish a
  site without any external instructions.
- A site I set **public** is served to a request with no dashboard session; a site
  I set **private** is refused to a request with no session (a browser navigation
  is sent to sign-in, a scripted/bearer request gets `401`) and serves its files to
  a request with a valid session.
- I change a site's visibility with one tool call and its reachability changes
  accordingly; it is never served under more than one visibility at once.
- I delete a site and its URL stops serving its files.
- As a logged-in dashboard user I open `<account>.ikigenba.com/srv/sites/` and see
  a Carbon-styled page showing the running version and a row for each site with
  its name, visibility (public, private, or unlisted), creator, and creation
  time; clicking a name opens that site. An unlisted site's row shows its name,
  not its random address.
- As a person with no dashboard session navigating to `/srv/sites/` (or to a
  private site's URL) in a browser, I am sent to the dashboard sign-in and, after
  signing in, returned to the page I was headed for. A session-less request that
  is not a browser navigation (a scripted `fetch`, a bearer call) still gets
  `401`.
- As a logged-in user on the landing page I type part of a site's name or
  address into the search box — including letters that are non-adjacent — and
  the list narrows to just the matching sites as I type, with no page reload.
- As a logged-in user I click the Name, Created, or Creator column header and the
  list reorders by that column; clicking the same header again reverses the
  direction. The list opens sorted newest-first by default.
- As a logged-in user with more than ten sites (or more than ten matches after
  filtering) I see a Prev/Next pager with a "Page X of Y" readout and can page
  through ten at a time; with ten or fewer, no pager appears.
- As a logged-in user I click **Clear** and the search box empties, the ordering
  returns to newest-first, and I am back on the first page.
- As a logged-in user I click a row's **copy** control and that site's full URL —
  the same address its name links to — is on my clipboard, ready to paste, with a
  brief "Copied" confirmation.
- As a logged-in user with JavaScript disabled I still see the complete list of
  sites (unfiltered, the search / sort / pager / copy controls absent) — the page
  degrades to the plain listing rather than showing broken or dead controls; the
  name links still open each site so its URL can be copied by hand.
- Every path served under `/srv/sites/…` is served by the sites process — nginx
  holds no `alias` and reads no site files off disk.
- An MCP client still discovers the AS via the PRM well-known and calls the
  bearer-gated `/mcp`; `/health` still responds.

## Addendum — registry adoption (internal address resolution)

A separate, cross-cutting change (beyond the hosting model) removes the loopback
**port literals** sites hardcodes in its Go composition root. sites resolves both
its own port and the dropbox mirror address **by name** through the shared,
authoritative `registry` table, asked once at startup. This is purely internal
and **behavior-preserving**; the existing `SITES_PORT` / `DROPBOX_BASE_URL` env
overrides still take precedence. The promise it adds is operational:

- **sites's loopback addresses cannot silently drift.** The port sites answers on
  and the dropbox address it fetches through come from the one authoritative
  registry, not a literal that can fall out of step with the rest of the suite.

The nginx front-door fragment (`etc/nginx.conf`) keeps its literal port — nginx
reads that config directly and cannot consult a Go library.
