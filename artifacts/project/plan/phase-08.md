# Phase 8 — The nginx location fragment

*Realizes design Decision 8 (nginx tiers). Depends on Phase 7.*

The committed `etc/nginx.conf` fragment with D8's ten tiers — PRM, bearer
`/mcp`, feed shield, session-gated landing + static with `@login_bounce`,
ungated `/f/` and `/u/` (strip-then-mint correlation; `1g` outer body
guard), session-gated `/p/` with `@login_bounce`, catch-all 404, the 429
re-emit — and the `nginx_test.go` content assertions. (Registering
`artifacts` in the `nginx/run` dev loop is an out-of-tree precondition in
the nginx tree, per D8 — not this phase's work.) End state: the fragment
ships verbatim and every tier property D8 names is pinned by an assertion.

**Done when:** the suite is green and each of R-4TNH-HPLP, R-4UVD-VHCE,
R-4W3A-9933, R-4XB6-N0TS, R-4YJ3-0SKH, R-4ZQZ-EKB6, R-50YV-SC1V,
R-526S-63SK is covered by a test tagged with its id.
