# Phase 142 — Landing and selector redirects pick the tier by scope visibility

*Realizes design Decision 76 (the scoped web surface), the rewritten R-HN4G-06FQ / R-HOCC-DY6F slice. Depends on Phase 141.*

The two emitted redirects in `internal/web` compose their tier from the target
scope's current visibility: the landing 303 (`GET /{$}`) sends the cookie
scope (or the `default` fallback) to `{mount}public/{scope}/` when that scope
is public and `{mount}private/{scope}/` when private; the selector action
(`GET /{tier}/{scope}/select?to=…`) does the same for `to`, regardless of the
caller's tier, still setting the `wiki_scope` cookie. The private tier
continues to serve any known scope for a directly-typed URL; existing tagged
tests for the two ids are updated in place to the visibility-derived
expectations.

**Done when:**

- R-HN4G-06FQ — the landing redirect targets the cookie scope's
  visibility-selected tier (public scope → `{mount}public/…`, private →
  `{mount}private/…`, unknown/absent cookie → the `default` scope by its own
  visibility) — covered by a tagged test against a real registry.
- R-HOCC-DY6F — the selector redirect targets `to`'s visibility-selected tier
  from either caller tier, with the cookie set only by the selector action —
  covered by a tagged test against a real registry.
- The suite is green per design's Conventions (`go test ./...` from `wiki/`).
