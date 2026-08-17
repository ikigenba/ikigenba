# Phase 25 — Tracking tokens: `contact_tokens`, `mint`, and `search` lookup

*Realizes design Decision 25 (tracking tokens).*

Give crm the ability to mint a short token for a contact and resolve it back. A
forward, timestamped migration (`bin/create-migration crm <name>`) adds the
`contact_tokens` table with a live-unique index on `token` and a live index on
`contact_id`. A new `mint` domain verb generates a 6-char lowercase
Crockford-base32 token (alphabet `0-9a-z` minus `i`,`l`,`o`,`u`), retried on the
live-unique clash, inserts one live row, and returns
`{ token, contact_id, campaign }` with `structuredContent` + `outputSchema`
(D19). `search` gains a `token` filter under `type:"contact"` that returns the
single live contact owning that live token. `mint` publishes no event.

**Done when:**

- **R-9QOE-LQTI** — `mint` for a live contact returns a 6-char token from the
  Crockford-base32 alphabet (no `i`/`l`/`o`/`u`) and a matching live
  `contact_tokens` row exists carrying the token and the supplied `campaign`.
- **R-9RWA-ZIK7** — a batch of `mint` calls returns pairwise-distinct tokens, and
  a direct insert of a second live row with an already-live token is rejected by
  `uq_contact_tokens_token_live`.
- **R-9T47-DAAW** — `search {type:"contact", filters:{token:T}}` returns exactly
  the contact owning live token `T`; after `T` is soft-deleted, and for an
  unknown token, it returns no contact.
- **R-9UC3-R21L** — minting twice for one contact with two campaigns yields two
  distinct live tokens, both resolving to that one contact, each carrying its
  campaign label.
- **R-9VK0-4TSA** — `mint` with a `contact_id` naming no live contact returns the
  typed not-found error and creates no `contact_tokens` row.
- **R-9XZS-WD9O** — a `mint` driven through the assembled MCP server (composition
  root, real SQLite) creates the row and returns the token in `structuredContent`,
  and a follow-up `search` token lookup over the same server resolves it back to
  the contact.
- The suite is green: `cd crm && go build ./...`, `cd crm && go vet ./...`,
  `cd crm && gofmt -l .` (no output), and `cd crm && go test ./...` all succeed.
