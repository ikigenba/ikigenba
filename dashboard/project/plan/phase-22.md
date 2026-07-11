# Phase 22 — stamp the identity handle onto every auth artifact

*Realizes design Decision 18 (capture at login), the stamping slice — ids
R-VQY2-GZ3F, R-VS5Y-UQU4, R-VTDV-8IKT. Depends on Phase 20 (the `identity.Store`)
and Phase 21 (the decoded `Iss`/`Name`/`Picture` on `googleidp.Identity`).*

Threads the durable handle from login into every auth artifact the introspection
paths read. A forward-only migration (`bin/create-migration dashboard
add_owner_id_to_auth_artifacts`) adds a nullable `owner_id TEXT` column to
`web_sessions`, `oauth_authcodes`, `oauth_chains`, and `personal_tokens`. `*app`
gains an `identity *identity.Store`; `handleCallback` calls
`a.identity.ResolveOrCreate` on the verified claims and passes the handle into
both completion paths. Creation seams gain an `ownerID` parameter alongside the
existing `ownerEmail` (which is written exactly as today):
`session.SessionStore.Create`, the `oauth` authcode issue,
`oauth.TokenStore.IssueChainAndTokens` (copying `owner_id` from the consumed
authcode), and `pat.Store.Create` (handle taken from the creating owner's
session). No header output changes here — that is Phase 23.

**Done when:** the suite is green (per design *Conventions*, including
`bin/check-migrations dashboard`) and each id below is covered by a clearly-named
test that reads the persisted `owner_id` back from a real temp DB after driving
the relevant flow, and confirms `owner_email` is unchanged:

- R-VQY2-GZ3F — completing a web login stores the resolved handle on the new
  `web_sessions.owner_id`.
- R-VS5Y-UQU4 — the OAuth path stores the handle on `oauth_authcodes.owner_id`,
  and exchanging that authcode carries the same handle onto `oauth_chains.owner_id`.
- R-VTDV-8IKT — creating a PAT stores the creating session's handle on
  `personal_tokens.owner_id` (no re-resolution).
