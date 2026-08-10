# Phase 129 — The scoped web routes: tiers, cookie redirect, selector, per-visibility URLs

*Realizes design Decision 76. Depends on Phase 128.*

Rebuilds `internal/web`'s route table around `/{tier}/{scope}/…` (explicit `public`/`private` registrations), the root cookie redirect, the selector endpoint + per-page selector UI, the registry/visibility wall (private scope 404 on the public tier), the per-tier+scope `<base href>`, and the per-call tier+scope page base for MCP citations and every absolute subject link (the D56/D59 composition moved off the startup constant).

**Done when:** the suite is green and these ids are covered by tagged tests:
- R-HKON-8MYC — tier+scope routes dispatch; non-tier first segment 404s.
- R-HLWJ-MEP1 — private tier serves any known scope; public tier 404s private scopes indistinguishably.
- R-HN4G-06FQ — root 303s to the cookie scope, `default` fallback on absent/unknown.
- R-HOCC-DY6F — only the selector action sets the cookie; redirects stay in-tier.
- R-HQS5-5HNT — selector lists all scopes (private tier) / public only (public tier), current preselected.
- R-HS01-J9EI — `<base href>` and absolute links carry the page's tier+scope.
- R-HT7X-X157 — unknown scope in any tier renders the styled 404.
- R-I0JC-7NLD — MCP ask citations pick the tier from current visibility.
