# Phase 38 — The provider chooser inside `GET /oauth/authorize`

*Realizes design Decision 29 (MCP authorize chooser). Depends on Phase 35 and
Phase 37.*

Make the single MCP authorize endpoint two-mode on the optional `provider`
query parameter: absent → after all existing request validation, render the
stateless chooser page (signin-wall styling, two anchors that are the same
authorize URL with all original parameters preserved plus
`provider=google`/`provider=github`; no handshake, no cookie);
`provider=google` → today's exact mint-and-303 behavior; `provider=github` →
the mirror through `githubidp.AuthorizeURL` and `/oauth/github/callback`; any
other value → `400 invalid_request`. Validation stays ahead of the choice.

**Done when:** R-IWCE-MR51, R-IXKB-0IVQ, R-IYS7-EAMF, and R-J003-S2D4 are each
covered by a clearly-named, verbatim-tagged test in
`internal/server/*_test.go`, and the suite is green per design's Conventions.
