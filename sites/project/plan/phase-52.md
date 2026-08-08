# Phase 52 — Site lifecycle against the plane: create, delete, slug rotation

*Realizes design Decision 36 (site lifecycle against the version plane). Depends on Phase 50.*

`internal/mcp`'s three structural verbs each gain their one plane call, in the
order D36 fixes, each passing the **site's recorded owner** (`owner_id` /
`owner_email` off the row) rather than the acting caller's identity (D32). `create` inserts the row and makes the directory, then calls
`version.Create`, and on failure **removes the row and the directory it just
made** before returning the error — including on the unlisted path, where the
token-collision retry still owns slug selection. `delete` calls `version.Delete`
**first** and removes the row and tree only on success. `set_visibility` calls
`version.Rename` **first**, and only when the slug actually changes: every
transition into unlisted (including unlisted→unlisted rotation) and leaving
unlisted with `new_slug`. `rename` (display name) and a tier-only visibility
change make no plane call.

**Done when:**

- R-FARO-E6HT — a successful `create` produces exactly one `Create` call for the
  slug; with `Create` made to fail, the tool returns an error envelope,
  `Store.Get(slug)` is not-found, and `SiteDir(Public, slug)` does not exist. An
  unlisted `create` produces a `Create` for the generated token.
- R-FBZK-RY8I — a successful `delete` produces exactly one `Delete` call and
  leaves neither row nor directory; with `Delete` made to fail, both the row and
  the directory (files byte-identical) remain.
- R-FD7H-5PZ7 — `set_visibility(slug, "unlisted")` produces one `Rename` from
  the old slug to the returned token; a second rotation renames token→token; and
  `set_visibility(token, "public", new_slug:"launch")` renames token→`launch`.
  No `Create` call appears on any of them.
- R-FEFD-JHPW — a private→public change with the slug kept, an idempotent
  re-assertion of the current visibility, and `rename(slug, "New Label")` each
  produce **zero** repos requests while still performing their local effects.
- The suite is green.
