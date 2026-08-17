# crm — Product

**Authority: intent.** This document owns *why* crm serves the surfaces it does,
*for whom*, what is in and out of scope, and what we **promise** — in outcome
terms only. Mechanism (handlers, templates, the Carbon tokens, the nginx
fragment, the MCP transport, the tool descriptors, the guide document) and its
checkable proof live in `project/design/README.md`. Where the two touch
observable behavior, product states the *promise* and design states the *exact,
checkable form*; that boundary keeps product, design, and plan from overlapping.

> **Scope note.** This doc covers crm's **ralph-governed** work. Two threads are
> chassis/surface work:
>
> 1. **The web landing page** — the human front door under `/srv/crm/`.
> 2. **Agent-facing MCP self-discovery** — how a connecting agent learns what
>    crm is and how to drive its tools, from the crm connection alone.
>
> The third is the **CRM domain** itself — the entity model, the verbs'
> semantics, validation, the migrations, the outbox producer. That domain
> **used** to sit outside this spec (built from a legacy plan, described only in
> code and `AGENTS.md`); that carve-out is **retired**. The CRM domain is now
> governed here like everything else: it changes only through a Decision and a
> phase. Most of the domain still **predates** this spec and is described
> normatively **only where a Decision touches it** — today, the sales-funnel
> vocabularies (D24) and the tracking-token capability (D25); the remainder
> stands as built until a Decision revises it.

## Problem

**Landing page.** Until recently every ikigenba service except the dashboard
served **only** machine surfaces — the RFC 9728 PRM bootstrap, `/health`, the
bearer-gated `/mcp`, and the loopback `/feed`. A human who opened
`<account>.ikigenba.com/srv/crm/` in a browser got nothing useful: there is no
token in a browser, so the bearer gate refuses them, and there was no
human-facing page behind that mount at all. The services declared "no UI," and
that statement has been deliberately retired: every deployable app now serves its
own HTML pages, beginning with a single landing page.

**Agent discovery.** A human does the actual *work* of crm through an AI agent,
not the landing page. For that agent to use crm well, it must discover — from
crm's own MCP connection alone — what crm is for, when to reach for it, and how to
drive each tool. Today that discovery is uneven. The tool surface is verbose in
one place (a single tool carries a full per-entity field reference that **every**
tool listing pays for in context) and silent in others, and agents have leaned on
an **external, separately-installed skill** to map everyday language ("contacts",
"companies", "pipeline") to crm and to recall field shapes. An agent that has only
crm connected, with no such skill, cannot make efficient use of it. The surface
should describe itself.

**Sales funnel and outreach.** crm's domain vocabulary was inherited from a
marketing-funnel model — contacts moving `subscriber → lead → opportunity →
customer`, deals through `lead → qualified → proposal → negotiation → won → lost`
— that does not match how the operator actually works: cold outreach, where a
person is either someone being pursued or a customer, and a sale moves through
the operator's own plain steps. The mismatched words make the tool harder to
use, not easier. Separately, the operator hands prospects tracked links and has
no way to tell, when a link comes back, which prospect it belonged to: nothing
ties a handed-out code to a contact.

## Purpose

crm is the suite's **sales CRM**: organizations, contacts, deals, tasks, and
interactions, driven by an agent over MCP. It exposes two self-explaining front
doors. The **landing page** is the human front door under `/srv/crm/`: a minimal
Carbon-styled card showing the service name and running version, gated by the
dashboard browser session. The **MCP surface** is the agent front door: it
describes itself so a connecting agent learns what crm is, when to use it, and
how to drive each tool, and can pull a single fuller usage guide **on demand** —
all from the crm connection itself, with **no external skill, plugin, or doc**
required for correct use.

The **CRM domain** behind that surface is governed here too. This spec states the
domain normatively only where a Decision touches it: today, the sales-funnel
vocabularies a contact and a deal move through, and the tracking tokens that tie
a handed-out code back to a contact.

## Users

- **A logged-in dashboard user, in a browser.** Any human authenticated to this
  box's dashboard who navigates to `/srv/crm/` sees the service name and version
  on the Carbon design system. The check is deliberately **coarse**: any
  logged-in dashboard user may view any app's landing page — there is no
  per-resource or per-owner authorization on this page.
- **The operator, confirming a deploy.** Opens the mount root after a deploy or
  rollback to confirm crm is up and which version is live — a browser-visible
  liveness signal that complements the machine `/health` and `version` checks.
- **A connecting AI agent (and, through it, the account owner).** An agent that
  has crm's MCP server registered. It routes work to crm, drives the tools, and
  when it needs field shapes or examples, retrieves crm's usage guide. Its whole
  understanding of crm comes from crm's own MCP surface.

The landing page is **not** for agents; agents use the bearer-gated `/mcp`
endpoint. The discovery surface is **not** for humans in a browser; it is what an
agent reads over MCP.

## Scope

**Landing page.** crm serves one page at the mount root: a `GET` of the bare
`/srv/crm/` root returns an HTML page showing the service name (`crm`) and the
running version, styled with the **Carbon** design system, serving its **own**
`tokens.css` and fonts, gated by the dashboard session cookie (an
unauthenticated browser gets `401`). It does nothing else in v1 — no per-resource
authorization, no domain data on the page, no interactive control, and it shares
no handler with any other service.

**Agent self-discovery.** The crm MCP surface describes itself so a connecting
agent can use it with no external skill:

- **Orient from the connection.** A concise service overview names crm's domain
  in the words users actually use (companies, people, deals/pipeline, tasks,
  notes) and states the normal working flow, so an agent can tell what crm is for
  and route to it.
- **Lean per-tool descriptions.** Each tool tells the agent *when to use it* and
  *what it returns*, without carrying bulk reference material that every listing
  must pay for.
- **A guide on demand.** An agent can retrieve a single crm usage guide covering
  the field shapes for each entity and **basic and advanced** worked examples —
  only when it wants it, not in every listing.

The discovery thread deliberately does **nothing else**: it does **not** change
what any crm tool *does*, does **not** require any external skill/plugin/doc for
correct use, and does **not** alter the landing page or the machine endpoints
(`/mcp` transport, PRM well-known, `/health`, `/feed`). Behavior changes come
only from the domain thread below.

**Sales funnel and tracking (CRM domain).** Two domain behaviors are in scope,
each stated where a Decision governs it:

- **The sales-funnel vocabularies.** A contact is a **prospect** or a
  **customer** — the two lifecycle values, default `prospect`. A deal moves
  through the operator's own steps — **contacted → interested → proposal → won →
  lost**, default `contacted` — and its read-only `status` (`open` / `won` /
  `lost`) is derived from the stage. The change replaces the legacy vocabularies
  and **preserves every existing contact and deal** (and their children) by
  remapping old values to new ones; no row is lost.
- **Tracking tokens.** crm can **mint** a short unique token for a contact,
  labeled with a campaign, and **resolve** a token back to the contact that owns
  it. A contact may have many tokens (one per campaign). crm mints and resolves
  only; it does **not** host the links, receive the hits, or send notifications —
  those live in other services.

Everything else about the domain — the full entity model, the other verbs and
their semantics, the outbox event surface — is **unchanged** by this work and
stands as already built.

## Contractual constants

Promised values the design must honor verbatim and never re-declare:

- **The landing page lives at the mount root only** — reachable at
  `<account>.ikigenba.com/srv/crm/`; the service answers it at its exact root
  path `/` and nowhere else. It never shadows `/mcp`, `/health`, `/feed`, or the
  PRM well-known.
- **The landing page is gated by the dashboard browser session, not a bearer
  token** — `auth_request /_session-authn`, never `/_authn`; a failed session
  check yields `401`. The gate is **coarse**: any logged-in dashboard user may
  view it.
- **v1 landing content is exactly: service name + running version** — from what
  the chassis already exposes (`rt.Service()` / `rt.Version()`); no new data
  source.
- **Each app owns its own landing page** — no shared landing handler; crm's page
  code, template, and assets live under `crm/`.
- **The visual system is Carbon** — the suite tokens/fonts; crm ships and serves
  its own copy.
- **The MCP surface is self-sufficient** — a connecting agent can discover and
  correctly use crm from the crm MCP connection alone, with **no external skill**.
- **Discovery describes; it does not change behavior** — the entity model, the
  verb set, their semantics, validation, and the event surface are unchanged by
  the discovery work. The guide is **read-only**, and it adds **no per-entity
  tool** (the tool count stays a function of verbs, not entities).

## What we promise (user-facing behavior)

**Landing page.**

- **A logged-in human who opens `/srv/crm/` sees a real page** — the crm service
  name and running version, on the suite's design system, not a raw proxy error
  or blank page.
- **A browser that is not logged in is refused** — `401`, because the page is
  gated by the dashboard session cookie.
- **The page looks like the rest of the suite** — same fonts, palette, single
  blue signal color, and spacing grid; it loads its **own** assets, not the
  dashboard's.
- **The version on the page is the version actually running** — the operator can
  confirm a deploy or rollback in a browser.

**Agent self-discovery.**

- **An agent with only crm connected can find and use it without any external
  skill** — it routes work about companies, people, deals/pipeline, tasks, and
  notes to crm from crm's own overview, and knows the normal flow.
- **Each tool tells the agent when to use it and what it returns**, concisely,
  without every listing carrying a full field reference.
- **An agent can ask crm for a usage guide** and get field shapes per entity plus
  **basic and advanced** worked examples, on demand.
- **The discovery work changed no tool's behavior** — every crm tool still
  behaves as it did; only how the surface describes itself changed, plus the
  added read-only guide. (Tool *behavior* changes come from the domain thread —
  the funnel vocabularies and the new `mint` verb — never from discovery.)

**Agents are unaffected across both threads** — the bearer-gated `/mcp`
transport, the PRM well-known, `/health`, and the loopback `/feed` behave exactly
as before; the landing page shadows none of them.

**Sales funnel and tracking.**

- **A contact is a prospect or a customer** — those are the two funnel stages; a
  new contact starts as a `prospect`, and any other lifecycle value is rejected.
- **A deal moves through the operator's own steps** — `contacted`, `interested`,
  `proposal`, then `won` or `lost`; its read-only status (`open` / `won` /
  `lost`) follows from the stage, and any other stage value is rejected.
- **The funnel change loses no data** — every contact and deal that existed
  before survives, its old value remapped to the new vocabulary and its child
  records intact.
- **crm can mint a tracking token for a contact and resolve it back** — a short,
  unique, campaign-labeled code handed out for one contact, and later a link hit
  carrying that code is resolved to exactly the contact it belongs to. A contact
  can hold many tokens, one per campaign. crm only mints and resolves; it does
  not host the links or receive the hits.

## Success criteria (outcomes)

Each is a result a viewer, operator, or connecting agent can confirm against the
running service:

- As a logged-in dashboard user I open `<account>.ikigenba.com/srv/crm/` and see
  a Carbon-styled page showing the service name `crm` and the running version.
- As a browser with no dashboard session I open `/srv/crm/` and am refused with
  `401`, not shown the page.
- The version shown on the page matches the version the deployed binary reports.
- The page loads its own `tokens.css` and fonts, and its fonts and colors match
  the suite design system.
- With **only crm connected and no external skill installed**, an agent asked to
  work with contacts / companies / deals routes to crm and completes a basic
  create → find → log flow.
- An agent retrieves crm's usage guide and, using only it, correctly constructs a
  `save` for each entity type — including the set-valued-field and
  derived-deal-status gotchas.
- crm's everyday tool listing is materially leaner than before (the bulk per-type
  field reference is no longer carried in every listing) while each tool still
  conveys when to use it.
- Every existing crm tool call still produces the same result it did before the
  discovery work — the discovery changes altered no behavior.
- Saving a contact with no lifecycle stores it as a `prospect`; saving it as a
  `customer` works; saving it with any other lifecycle value is rejected.
- A deal's stage is one of `contacted`, `interested`, `proposal`, `won`, `lost`
  (default `contacted`); a `won` or `lost` deal reads that status and the rest
  read `open`; any other stage value is rejected.
- After the funnel-vocabulary change, every contact and deal that existed before
  still exists, each carrying a value legal under the new vocabulary, and no
  child record was lost.
- Minting a tracking token for a contact returns a short unique code; minting
  again for a different campaign returns a different code; resolving a token
  returns exactly the contact it belongs to, and an unknown or removed token
  resolves to nothing.
- An MCP client still discovers the AS via the PRM well-known and calls the
  bearer-gated `/mcp` exactly as before; opening `/srv/crm/feed` from nginx still
  returns `404` and `/health` still responds.
