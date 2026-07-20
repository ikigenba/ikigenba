# Phase 36 — The `/login` chooser, the provider start routes, and the two-CTA login composition

*Realizes design Decision 27 (login chooser + start routes) and the amended
Decision 7 CTA block (R-ILDB-6TGS, R-IML7-KL7H). Depends on Phase 34 and
Phase 35.*

Split "start signing in" from "start signing in with X": `GET /login` renders
the sign-in composition as a chooser (no handshake, no cookie; validated
`return_to` threaded into both anchor hrefs), `GET /login/google` takes over
the old `/login` body (mint web handshake with `ProviderGoogle` + validated
`return_to`, binding cookie, 302 to Google), and `GET /login/github` mirrors it
with `ProviderGitHub` and `githubidp.AuthorizeURL` against
`/oauth/github/callback`. The logged-out `index.html` CTA block becomes the two
stacked provider anchors (`/login/google`, `/login/github`, identical classes,
Google first) bracketed by the same two rules; one template serves both routes.
`(*app)` gains the `githubProvider` field this phase wires for `AuthorizeURL`
(constructed with the stub in tests; live construction lands in Phase 37).
Tests tagged with the retired ids R-JEZ4-2107 and R-3JAM-JLZ6 are updated to
the new composition and re-tagged R-ILDB-6TGS / R-IML7-KL7H.

**Done when:** R-IGHP-NQI0, R-IHPM-1I8P, R-IIXI-F9ZE, R-IK5E-T1Q3,
R-ILDB-6TGS, and R-IML7-KL7H are each covered by a clearly-named,
verbatim-tagged test in `internal/server/*_test.go`, the tags R-JEZ4-2107 and
R-3JAM-JLZ6 no longer appear anywhere in the codebase, and the suite is green
per design's Conventions.
