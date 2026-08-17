# Phase 24 — Sales-funnel vocabularies

*Realizes design Decision 24 (sales-funnel vocabularies).*

Replace the contact-lifecycle and deal-stage closed vocabularies with the
operator's own, preserving all existing data. A forward, timestamped migration
(created with `bin/create-migration crm <name>`) rebuilds `contacts` and `deals`
with the new `CHECK` sets and defaults, copies every live and soft-deleted row
across with foreign-key integrity so no child row is lost, and remaps each legacy
value per D24's table. The domain validation backing `save` and the `guide` field
catalog move to the new vocabularies; the derived `dealStatus` mapping and the
`search` `status:"open"` filter keep their shape over the new stage set.

- `contacts.lifecycle` → `prospect | customer`, default `prospect`
  (`subscriber`/`lead`/`opportunity` → `prospect`, `customer` → `customer`).
- `deals.stage` → `contacted | interested | proposal | won | lost`, default
  `contacted` (`lead`→`contacted`, `qualified`→`interested`,
  `negotiation`→`proposal`, `proposal`/`won`/`lost` unchanged).

**Done when:**

- **R-9LST-2NUQ** — after the migration, `contacts.lifecycle` accepts `prospect`
  and `customer`, rejects any other value with the closed-vocabulary error, and a
  `save` omitting `lifecycle` stores `prospect`.
- **R-9N0P-GFLF** — after the migration, `deals.stage` accepts exactly the five
  new values, rejects any other with the closed-vocabulary error, and a `save`
  omitting `stage` stores `contacted`.
- **R-9O8L-U7C4** — `dealStatus` derives `won`/`lost` from those stages and
  `open` from `contacted`/`interested`/`proposal`, and `search {type:deal,
  filters:{status:"open"}}` returns exactly the non-`won`/`lost` deals.
- **R-9PGI-7Z2T** — the migration over a database seeded with the full legacy
  spread of lifecycle and stage values (with child emails/phones/tags and deal
  participants) preserves every table's row count and every child row and remaps
  every value per D24's table.
- The suite is green: `cd crm && go build ./...`, `cd crm && go vet ./...`,
  `cd crm && gofmt -l .` (no output), and `cd crm && go test ./...` all succeed.
