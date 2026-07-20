# Design — Owner-Id Keying (owner-id)

Services today key everything they store and scope on the caller's email
(`X-Owner-Email`). Email is about to become mutable: the dashboard is gaining a
GitHub alternate login, and GitHub account emails are user-editable. A changed
email must not orphan a user's data or break their access. The dashboard already
mints a stable opaque identifier per human (`identities.id`, keyed by IdP
`(iss, sub)`) and, as of the shipped header contract, forwards it on every
authenticated request as `X-Owner-Id`, alongside `X-Owner-Email`,
`X-Owner-Name`, and `X-Owner-Picture`, on all three auth surfaces, fail-closed.

This design converts the suite to key on that id. It touches appkit and all
thirteen services; it does not touch the dashboard at all.

## Decisions

### 1. appkit `Identity` carries all four owner headers; the gate requires the id

`appkit/server.Identity` becomes:

```go
type Identity struct {
    OwnerID      string // X-Owner-Id: the stable key; all scoping and lookups
    OwnerEmail   string // X-Owner-Email: display only
    OwnerName    string // X-Owner-Name: display only
    OwnerPicture string // X-Owner-Picture: display only
    ClientID     string // X-Client-Id
}
```

`requireIdentityHeaders` refuses (401, same challenge semantics as today) when
`X-Owner-Id` is empty. The other three are passed through as display data and
never gated on. The MCP fallback header read (`identityFromRequest`) follows the
same rule. The health envelope reports `owner_id` alongside the existing
`owner_email` and `client_id`.

This is a **hard flip**: no interim period where appkit accepts
`X-Owner-Email` as an identity. The header is already live on every surface in
production, so the fallback would protect nothing. Consequence: after the appkit
change lands, every service module is red until its own conversion phase lands;
each service phase leaves its module green, and only the mid-plan window is red.

### 2. Owner-scoped rows store both id and email; the email is a write-once snapshot

Every table that stores an owner stores two columns:

- `owner_id` TEXT NOT NULL: the scoping and lookup key. Every query, filter,
  uniqueness constraint, and authorization comparison keys on this and only
  this.
- `owner_email` TEXT NOT NULL: captured from the request headers at row
  creation, never updated afterward, never read for logic. Display only.

There is **no refresh mechanism**: no dashboard resolve endpoint, no local
identity cache, no identity events. If a person's email changes, rows created
before and after the change display different emails, and the UI treats them as
two identities. This is a conscious trade: display may drift, keying never
does, and because both values sit on every row, any future grouping or
re-display fix has all the data it needs in the database already.

### 3. Column names are standardized suite-wide

The columns are named `owner_id` and `owner_email` everywhere. Existing
deviations are renamed in the rebuild: wiki's `owner` and sites' `created_by`.

### 4. Migrations rebuild owner-scoped tables and drop their rows

A service cannot map stored emails to owner ids (only the dashboard knows the
mapping), so converted tables are rebuilt empty by a new migration (created via
`bin/create-migration`, never hand-numbered; committed migrations are never
edited). This is safe during the migration window: only ledger holds live
customer data, and ledger has no owner columns. The rebuild is deterministic on
any database (dev, fresh, or the live box) and does not depend on anyone
remembering to wipe `state/`.

### 5. Display surfaces keep email and gain id

MCP tool results that expose an owner today (for example prompts' prompt/run
records, sites' list) keep `owner_email` and add `owner_id`. Log fields and
health envelopes likewise carry both.

### 6. The event plane's envelope is untouched; one payload converts

Event envelopes (`{id, source, time, kind, subject, payload}`) carry no owner.
One payload does: webhooks' `received` events carry an `owner` field (the
stored owner email), which repos' intake consumer reads to attribute sessions.
Per Decision 2's row rule applied to the payload, the field is replaced by the
pair `owner_id` (the for-whom key consumers act on) + `owner_email` (display
snapshot), both from the stored webhook row; webhooks' phase renames the field
and repos' phase follows it on the consuming side. No other service embeds an
owner in an event payload.

## Inventory — what each module changes

| module | tables rebuilt | code |
|---|---|---|
| appkit | none | Identity + gate flip, MCP fallback, health envelope, tests |
| prompts | `prompts`, `runs` (owner_email columns) | key on owner_id; MCP schemas add owner_id |
| repos | `repos`, `sessions` | key on owner_id |
| scripts | `scripts` | key on owner_id |
| webhooks | `webhooks` | key on owner_id |
| wiki | `jobs` (`owner` renamed), `aliases` (`created_by` renamed) | key on owner_id (`wiki_ingest`/`wiki_jobs` are dead legacy tables from frozen 002, no readers — untouched) |
| sites | `sites` (`created_by` renamed to owner_email, owner_id added) | list keeps showing email |
| dropbox | none | Identity field reads only (health/display) |
| github | none | Identity field reads only (log fields) |
| crm, ledger, notify, cron, gmail | none | tests inject `X-Owner-Id`; display reads follow the new Identity |

Ledger note: ledger is the one service with live production data. Its phase is
code/test only; it ships no migration.

## Execution model

appkit and every service are spec-governed: each module's changes flow through
its own `project/` spec (amend, then run its build loop), never hand edits. The
companion `owner-id-plan.md` orders the phases: appkit first (the hard flip),
then each service in turn, then a suite-level verification pass.

## Out of scope

- The GitHub alternate login itself (next unit of work; this conversion is its
  prerequisite).
- Any dashboard change. The dashboard already stores and forwards owner ids.
- Identity grouping/refresh in displays (consciously deferred, see Decision 2).
