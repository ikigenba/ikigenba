# CRM Redesign Plan — agent-first sales CRM over a polymorphic MCP surface

Status: **approved design, decisions resolved, not yet built.** Build is driven
by **sequential** subagents (no parallelism) against this plan. Scope is confined
to the `crm/` folder.

> This revision folds in the design-review decisions (2026-06-03). The earlier
> draft's "ranked search," 5-way parallel fan-out, and open §10 questions have
> all been resolved; see the **Resolved decisions** table in §11.

## 1. Goal & guiding decision

Turn the current placeholder (an address book of contacts/emails/phones exposed
as 12 fine-grained MCP tools) into a real **sales CRM** for a relationship- and
newsletter-driven funnel, exposed through a **small, fixed set of polymorphic MCP
tools** that does **not** grow as we add entities.

The governing decision, which everything below serves:

> **Tool count is a function of *verbs*, not *entities*.** Adding a new
> capability later (products, quotes, support tickets, …) adds a new entity
> *type* and new *fields* — it does **not** add new tools. The verb surface is
> fixed at six.

This is the answer to the project's stated fear of tool sprawl. When reviewing
any future change, the test is: *did this add a tool?* If yes, justify why a new
verb (not a new type) was unavoidable.

Two ruling decisions from the design discussion:

- **Fully polymorphic `save`** — one upsert tool over all mutable entities, type
  as a parameter, looser schema, server-side typed validation. (Chosen over a
  typed `save`-per-entity.)
- **Entirely greenfield** — nothing consumes the existing CRM; it was a
  placeholder. We replace the domain and tool layers outright; no transition
  shims, no back-compat.

## 2. The fixed tool surface (6 tools)

Down from 12, while *adding* organizations, deals, interactions, tasks,
lifecycle stages, and tags.

| Tool | Shape | Role |
|---|---|---|
| `crm_search` | `(query, type?, filters?, limit?, after_id?)` | **Filtered, recency-ordered** summaries across all entities or scoped to one `type`. The agent's first move on almost any request. Also serves as the list/paginate verb (e.g. timeline deep-reads, newsletter audience pulls). Substring (`LIKE`) match, ordered `updated_at DESC`. *(True relevance ranking via FTS5 is a documented escape hatch, not v1.)* |
| `crm_get` | `(id)` | One entity as a rich "card": a contact returns **with** its org, open deals, recent interactions, and open tasks already attached — one call, full context. Type is resolved from the id by indexed probe (no `type` arg). |
| `crm_save` | `(type, id?, fields, force?)` | Create (no `id`) or update (`id`) **any** mutable entity. Upsert. On create, returns a `duplicate` error with `existing_id` unless `force:true`. Deal stage, contact lifecycle, task completion, tagging — all just `fields`. |
| `crm_delete` | `(type, id)` | **Shallow** soft-delete of any entity (incl. `interaction`). |
| `crm_log` | `(subject_id, kind, body, occurred_at?)` | Append an interaction to the timeline (`note`/`call`/`email`/`meeting`). Append-only; the most frequent write in a CRM and the verb that makes this a CRM rather than an address book. |
| `crm_whoami` | `()` | Unchanged. The end-to-end auth proof. |

Deliberately **not** in the initial surface (documented escape hatches, add only
when justified):

- `crm_link` / `crm_unlink` — reserved for true many-to-many graph edges. The one
  many-to-many we have now (deal ↔ contact participants with a role) is carried
  as a `fields.contacts: [{id, role}]` array on `crm_save(type:"deal")`. Introduce
  `crm_link` only when many-to-many relationships multiply (e.g. contact↔contact,
  deal↔product).
- A separate `crm_timeline` — folded into `crm_get` (recent N inline) +
  `crm_search(type:"interaction", filters:{subject_id})` for deep history.

## 3. Entity model (standard sales-CRM vocabulary)

Five entities. All share `id` (ULID), `created_at`, `updated_at`, `deleted_at`
(soft delete, the existing convention).

### organization
The company/account. `name` (required), `domain` (website/email domain — drives
dedup and newsletter targeting). Future: industry, size.

### contact
A person. Carries the existing rich identity model plus CRM funnel fields:
- `given_name`, `family_name`, `display_name` (derived if absent — keep existing
  derivation: supplied → "given family" → primary email).
- `emails[]` inline, one `primary` (keep existing multi-email model, normalization,
  uniqueness-per-contact rules).
- `phones[]` inline E.164, one `primary` (keep existing model + `phonenumbers`
  validation).
- `org_id` — FK to organization ("works at"). 1:1, set via save field.
- `title` — job title.
- `lifecycle` — funnel position: `subscriber | lead | opportunity | customer`
  (default `lead`). This is "moving prospects toward our offerings."
- `tags[]` — labels (e.g. `newsletter`). The monthly-newsletter **audience is a
  tag**. Stored in a join table, set via `fields.tags`, queried via search filter.

### deal (opportunity)
An active pursuit of a sale.
- `name`, `org_id` (FK), `stage` (`lead | qualified | proposal | negotiation |
  won | lost`), `amount_cents` (INTEGER), `currency` (default `USD`),
  `close_date` (expected, RFC3339 date).
- `contacts: [{id, role}]` participants (many-to-many; roles free-text, e.g.
  `champion`, `decision_maker`).
- Derived `status` (`open | won | lost`) from stage. **Derived-only** — never set
  directly; `crm_save` rejects a client-supplied `status` (`validation` error).

### interaction (timeline entry)
The dated history. Created via `crm_log`, not `save`. **Append-only** — no
edit-in-place; corrections are delete-and-relog via `crm_delete`.
- `kind` (`note | call | email | meeting`), `body`, `occurred_at` (default now).
- Subject ref: at least one of `contact_id` / `org_id` / `deal_id`.

### task (follow-up)
- `title`, `status` (`open | done`), `due_at?`, `done_at?`.
- Optional subject ref (`contact_id` / `org_id` / `deal_id`).
- Completion is `crm_save(type:"task", id, fields:{status:"done"})`.
- **No owner/assignee** — single-tenant ("the box owner"); "who acted" lives in
  the per-service audit store, not a domain column.

**Decided defaults:** money as integer cents + currency; lifecycle vocabulary
above; deal stage vocabulary above; tags are contact-only for now (org-level
segments are future). Vocabularies are enforced as `CHECK` constraints and
documented in the `crm_save` tool description; greenfield-cheap to revise.

## 4. Polymorphic `save` contract

`inputSchema` is intentionally loose; the **tool description** carries the
per-type field shapes, and the server validates per type with corrective,
typed error messages (the agent self-corrects from them).

```jsonc
{
  "type": "object",
  "required": ["type"],
  "properties": {
    "type":   { "type": "string", "enum": ["organization","contact","deal","task"] },
    "id":     { "type": "string", "description": "omit to create; provide to update" },
    "fields": { "type": "object", "description": "entity-specific; see description" },
    "force":  { "type": "boolean", "description": "override a duplicate match on create" }
  }
}
```

`interaction` is **deliberately absent** from the enum — it is created via
`crm_log`, never `crm_save`.

### Decode / normalize / validate — at the dispatcher seam

The single `crm_save` handler is **not** type-aware beyond routing. The honest
type-switch lives in **one** place — `service.go` — which decodes the loose
`fields` map into a **typed `<Type>Input` struct**, applies normalization (email
lowercase, phone→E.164, `display_name` derivation, label validation), and
validates. **Entities never see `map[string]any`** — their `Save` methods take
clean typed inputs. This preserves "validate at boundaries, trust internal code":
the dispatcher seam is the boundary.

### Create / update semantics

- **Create** (no `id`): insert. Run a dedup probe first — **exact only**:
  - contact by **normalized primary email**,
  - organization by exact **`domain`**, falling back to exact **`name`** when the
    org carries no domain.

  On a match, return `{"error":{"code":"duplicate","existing_id":"…","message":"…"}}`
  instead of creating, unless `force:true`. The agent then chooses update vs force.
  **No fuzzy matching** in the probe — fuzzy belongs in `crm_search`, where ranked
  results are judged, never in a probe that hard-errors and blocks a legit write.
- **Update** (`id`): partial — only provided `fields` change. **Pointer /
  "provided" semantics** as in the current contacts service: a field absent from
  `fields` is untouched.

### Set-valued fields — declarative replace

`fields.tags` (contact) and `fields.contacts` (deal participants) are
**declarative**: the array is the *complete desired set*. The server diffs against
the current live set and reconciles. **Critically:**

- **omitted** (`nil`) = untouched,
- **present-but-empty** (`[]`) = clear all.

Modeled as `*[]T` at the decode boundary (mirroring the existing
pointer-means-provided convention) so a contact edit that doesn't restate `tags`
never silently un-subscribes the newsletter. For tags, the diff *is* what emits
`contact.tagged` / `contact.untagged` (§6).

`crm_get` returns the rich card (§ below); `crm_search` returns trimmed summaries
(id, type, display label, a few key fields — enough to disambiguate).

### `crm_get` card shape

- Takes `(id)` only. Type is resolved by **probe-by-id** — up to five indexed
  point lookups, one per table; no separate type-registry table (premature at
  this scale).
- The card is **per-type** (each entity's `Get` hook composes its own): a contact
  attaches {org, open deals, recent interactions, open tasks}; a deal attaches
  {org, participant contacts+roles, recent interactions, open tasks}; an org
  attaches {contacts, open deals, recent interactions}.
- **Recent interactions: N = 20**, newest-first. Deep history is
  `crm_search(type:"interaction", filters:{subject_id})`.
- **"open"** = deal `status='open'` / task `status='open'`.
- Every join filters `deleted_at IS NULL` (see §7 delete rule).

### Error envelope (uniform, closed vocabulary)

Every tool error uses one shape:

```jsonc
{"error":{"code":"<code>","message":"<corrective>","field":"<optional>","existing_id":"<duplicate only>"}}
```

Closed code vocabulary:

- `validation` — bad/missing field (incl. a rejected derived `status`); `field`
  names it; `message` states the **fix** ("email must be RFC5322; got 'bob@'"),
  not just the defect. The loose-schema self-correction bet depends on corrective
  messages.
- `not_found` — id doesn't resolve, or resolves to a soft-deleted row.
- `duplicate` — dedup probe hit; carries `existing_id`; the only code meaning
  "retry with `force:true` or update".
- `conflict` — invariant violation (uniqueness race, no-primary).

**Mapping rule:** entities return typed **sentinels** (`ErrValidation`,
`ErrNotFound`, `ErrConflict`, plus a duplicate carrier), wrapping with `%w` +
context as the current code does in `mapUniqueErr`. The **dispatcher** does the
single sentinel→envelope translation (`errors.Is` switch). Entities never write
wire JSON.

## 5. Data model / migrations

Greenfield the domain schema. The dev DB (`tmp/crm.db`) is deleted and recreated;
no production data exists. **One SQLite file holds the entire service** — all
domain tables *and* the outbox — which is what makes the atomic-outbox tx (§6)
legal. Single-writer is correct and deliberate at ≤100 users.

- **Keep** `001_schema_migrations.sql` and `003_outbox.sql` (library-owned, must
  stay **byte-identical** to `outbox.SchemaSQL` — `migrations_outbox_test.go`
  asserts this; do **not** touch `003`).
- **Replace** `002_contacts.sql` → `002_crm.sql` with the full schema:
  `organizations`, `contacts`, `contact_emails`, `contact_phones`, `contact_tags`,
  `deals`, `deal_contacts`, `interactions`, `tasks`. Carry over the existing
  partial-unique-index discipline (one live primary email/phone per contact;
  live-uniqueness). Add lookup indexes (org_id, stage, subject refs,
  `contact_tags(tag)`, `tasks(status) WHERE open`) and soft-delete partial indexes
  (`WHERE deleted_at IS NULL`). Enforce vocabularies via `CHECK`. Confirm/keep the
  `PRAGMA journal_mode=WAL` setup from the current `db.go`.

(Rewriting `002` rather than appending `004` is chosen because the repo is
never-deployed greenfield — cleaner than carrying a drop-the-contacts migration.)

## 6. Events (event-plane producer)

Keep the atomic-outbox pattern: the event is appended **on the same `*sql.Tx` as
the domain write**, `Ring()` after commit. The single-file DB and the
service-owns-the-tx architecture (§8) are what make this atomic.

**Build first-wave only in code** (needed for the newsletter funnel + the
`notify` consumer):
- `contact.created`, `contact.updated` (carry lifecycle so consumers can react to
  funnel moves).
- `contact.tagged` / `contact.untagged` — segment membership, derived from the
  §4 tag diff. The **newsletter audience changes** flow here; `notify` consumes
  these to manage the send list. **CRM owns the audience; CRM does not send.**

**Second wave is documented intent only — not written until a consumer exists**
(`deal.stage_changed`, `deal.won`, `deal.lost`, `interaction.logged`). No dormant
payload structs in `events.go`; the outbox/Ring plumbing is generic, so adding one
later is a localized additive change. (This is the "reject speculative futures"
principle applied — the plan documents them; the code does not pre-build them.)

The existing `contact.created` payload in `internal/contacts/events.go` is a
reasonable template (snapshot with `primary`, not `is_primary`); redo it for the
new contact shape.

## 7. Keep / replace / delete (exact)

**Keep untouched (platform scaffolding):**
- `internal/db/db.go` (migration runner, WAL pragma), `internal/ids`,
  `internal/logging`.
- `internal/server/*` — routing, PRM well-known, `requireIdentityHeaders`,
  whoami, security headers, graceful shutdown. The service is **MCP-only**: the
  mux mounts exactly `/.well-known/oauth-protected-resource` (unauth), `/whoami`,
  `/mcp`, `/feed`. **There are no REST `/contacts` routes** and none are added.
- `internal/mcp/mcp.go` — the JSON-RPC transport (`initialize`, `tools/list`,
  `tools/call`, result/error helpers). Only the injected service type changes.
- `bin/*`, `etc/*`, `cmd/crm/main.go` (rewire service construction only),
  eventplane wiring, `go.mod`/deps (`phonenumbers`, `ulid`, sqlite).
- Migrations `001`, `003`. Tests `db_test.go`, `server_test.go`,
  `migrations_outbox_test.go`.

**Replace:**
- `internal/db/migrations/002_contacts.sql` → `002_crm.sql` (§5).
- `internal/mcp/tools.go` → the 6-tool surface + polymorphic dispatch (§2, §4).

**Delete / supersede:**
- `internal/contacts/*` → new `internal/crm/` domain package (§8).
- `internal/contacts/events.go` → `internal/crm/events.go` (§6).
- `contacts_test.go` / `events_test.go` die with the package; rewritten as
  `internal/crm/*_test.go` (§9).

**Update docs last:** `crm/CLAUDE.md` "Keep / port", "Changes from the reference",
and the tool notes now describe the new surface.

## 8. Package architecture

A single `internal/crm/` package, **one file per entity**, plus shared
integration files:

```
internal/crm/
  types.go         shared structs, typed <Type>Input, error sentinels
  store.go         DB handle wrapper, tx helpers, shared scanning, newTestStore
  organization.go  org struct + store CRUD + Save/Get/Search/Delete hooks
  contact.go       contact (+emails/phones/tags) struct + store + hooks
  deal.go          deal (+participants) struct + store + hooks
  interaction.go   interaction struct + store + Log
  task.go          task struct + store + hooks
  search.go        cross-entity search + per-type summaries
  service.go       the dispatcher: typed decode/normalize, type routing, owns the
                   tx, dedup probe, event emission, sentinel→envelope. The seam.
  events.go        first-wave event payloads + builders
```

### The frozen entity contract (tx-passed; service owns the tx)

Forced by the atomic-outbox requirement (§6) over a single DB file (§5): the
dispatcher opens **one** `*sql.Tx` and runs decode → entity write → dedup probe →
outbox append → commit, atomically. Entities are pure SQL against a passed tx and
never own a transaction (the existing `Repo(tx)` / `Service(tx-owner)` split).
Phase 0 freezes a compiling interface; because the build is **sequential**, it may
*evolve* if a later entity reveals a need (cheap — revise the earlier entity), but
it starts concrete:

```go
type entity interface {
    Save(tx *sql.Tx, id string, in <Type>Input, now time.Time) (Summary, error)
    Get(tx *sql.Tx, id string) (Card, error)
    Search(tx *sql.Tx, p SearchParams) ([]Summary, error)
    Delete(tx *sql.Tx, id string, at time.Time) error
}
```

`internal/mcp/tools.go` holds the 6 descriptors and dispatches into `crm.Service`
(`Save/Get/Search/Delete/Log`).

### Cross-entity delete rule (Phase 0, shared)

**`Delete` is shallow:** it soft-deletes only its own row + owned children
(contact → emails/phones/tags). It does **not** cascade to or block on other
entities. Dangling FKs (a contact whose org was deleted; a `deal_contacts` row
whose contact was deleted) are **tolerated** — every read path filters
`deleted_at IS NULL` on every joined entity, so orphans are simply hidden from
cards/search, never enforced at delete time. Soft-delete keeps the FK intact, so
undelete restores the relationship.

## 9. Build phases (all sequential — no parallelism)

Dependency-ordered, **one agent at a time**, each gated before the next starts.

1. **Phase 0 — Foundation.** Write `002_crm.sql`; delete `internal/contacts/`;
   scaffold `internal/crm/` with `types.go` (typed inputs, sentinels), `store.go`
   (tx helpers, shared scanning, **`newTestStore(t)`** — fresh migrated SQLite,
   `:memory:` with temp-file fallback if WAL/migrations complain), `service.go`
   (dispatcher stubs, the frozen `entity` interface, the shared delete rule), and
   rewire `cmd/crm/main.go` + `internal/mcp/mcp.go` to the new service so the tree
   **compiles** (empty behavior is fine). Gate: `go build ./...` green.

2. **Phase 1 — Entities (sequential, in this order):**
   **organization → contact → deal → task → interaction.** Each agent reads the
   prior committed entity and conforms to the established pattern. One file each:
   struct, store CRUD against §5 schema, the typed `Save/Get/Search/Delete` (or
   `Log`) hooks. Gate per entity: package compiles; that entity's `*_test.go`
   round-trips its CRUD against a real DB via `newTestStore`.

3. **Phase 2 — Integration.** Flesh out `service.go` (typed decode/normalize, type
   routing, upsert, exact dedup probe, tx ownership, sentinel→envelope),
   `search.go` (cross-entity filtered/recency search + summaries). Gate:
   `service_test.go` round-trips each type through Save/Get/Search/Delete/Log,
   incl. the duplicate/`force` path and the soft-delete-orphan-filtering rule.

4. **Phase 3 — Tool surface.** `internal/mcp/tools.go`: 6 descriptors with per-type
   field docs in the descriptions, polymorphic dispatch, typed error translation,
   dedup/`force` handling. Gate: `tools_test.go` — `tools/list` shows exactly 6;
   `tools/call` exercises each verb + the error envelope.

5. **Phase 4 — First-wave events + newsletter segment.** Wire
   `contact.created/updated` + `contact.tagged/untagged` (from the tag diff)
   through the atomic outbox. Gate: a tagged contact emits a segment event on
   `/feed`.

6. **Phase 5 — Docs.** Update `crm/CLAUDE.md` to the new surface; set this plan's
   status to "built."

Verification throughout: `go build ./...`, `go test ./...`, then drive `/mcp`
directly over loopback (services trust injected `X-Owner-Email`/`X-Client-Id`),
and an end-to-end event check (tag a contact → observe the segment event on
`/feed`). Tests are stdlib table-driven, no new assertion deps; the per-phase
gates are the real acceptance criteria.

## 10. Open questions

**None.** All design questions from the prior draft were resolved in the
2026-06-03 review; see §11.

## 11. Resolved decisions (2026-06-03 review)

| Topic | Decision |
|---|---|
| Vocabularies | lifecycle / deal-stage / interaction-kind accepted as written; deal `status` is **derived-only**, `save` rejects a client `status`. |
| Dedup | **exact-only** — contact by primary email, org by domain (exact-name fallback when no domain); fuzzy stays in `crm_search`. |
| Owner/assignee | **none** (single-tenant); keep task `due_at`/`done_at`; "who acted" in the audit store. |
| Interaction edits | **append-only**; corrections are delete-and-relog via `crm_delete`. |
| Search | **filtered + recency-ordered** (`LIKE`, `updated_at DESC`); FTS5 is a documented escape hatch. |
| Architecture | **tx-passed entities**, service owns the one `*sql.Tx` (atomic outbox over a single DB file); entities take **typed `<Type>Input`**, never `map[string]any`. |
| Decode | **typed decode at the dispatcher seam** (`service.go`); the one honest type-switch. |
| Set fields | `tags` / deal `contacts` are **declarative replace**; **omitted = untouched, present-empty = clear** (`*[]T`); events derive from the diff. |
| Delete | **shallow** soft-delete (self + owned children); read paths filter `deleted_at IS NULL`; orphans tolerated, never enforced/cascaded. |
| Events | **first-wave only** in code; second-wave documented intent until a consumer exists. |
| `crm_get` | probe-by-id type resolution; per-type card; recent interactions **N=20**; "open" = `status='open'`. |
| Errors | uniform envelope, closed 4-code vocab (`validation/not_found/duplicate/conflict`); entities return sentinels, dispatcher renders; messages must be **corrective**. |
| Tests | real-SQLite via frozen `newTestStore(t)` (`:memory:`, temp-file fallback); layered phase gates; stdlib, no new deps. |
| Surface | confirmed **MCP-only** — no REST `/contacts` routes exist or are added. |
| Orchestration | **all subagents sequential** (no parallelism); entity order **org → contact → deal → task → interaction**; interface frozen-but-evolvable. |
