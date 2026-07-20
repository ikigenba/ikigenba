# Phase 35 — Provider-bound handshakes in `oauth_state`

*Realizes design Decision 26 (provider-bound handshakes) — the store + Google
slice: R-IBM4-4NJ8, R-ICU0-IF9X. The GitHub-callback side (R-IF9T-9YRB)
belongs to Phase 37.*

Add the `provider TEXT NOT NULL DEFAULT 'google'` column via
`bin/create-migration dashboard add_oauth_state_provider`; add
`ProviderGoogle`/`ProviderGitHub` constants and the `Handshake.Provider` field
to `internal/oauthstate`; change `CreateWeb` to
`CreateWeb(ctx, provider, returnTo)` and `CreateMCP` to
`CreateMCP(ctx, provider, mcp)`, delete the legacy `Create`, and read
`provider` back in `Consume`. Update the existing callers (`handleLogin`,
`handleAuthorize`) to pass `ProviderGoogle`, and make `handleCallback` refuse a
consumed handshake whose provider is not `google` (`400`,
`provider_mismatch` federation reject). The module ends green with Google-only
behavior externally unchanged.

**Done when:** R-IBM4-4NJ8 and R-ICU0-IF9X are each covered by a clearly-named,
verbatim-tagged test (temp-DB store round-trip; HTTP-level Google-callback
refusal), all pre-existing tests still pass, and the suite is green per
design's Conventions.
