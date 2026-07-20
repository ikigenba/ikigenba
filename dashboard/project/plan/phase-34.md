# Phase 34 — The `internal/githubidp` provider package

*Realizes design Decision 25 (GitHub identity provider) — the package slice:
R-I4AP-U132, R-I5IM-7STR, R-I6QI-LKKG, R-I7YE-ZCB5, R-I96B-D41U. The
composition-root env gate (R-IAE7-QVSJ) belongs to Phase 37.*

Build `dashboard/internal/githubidp`: `Credentials`, `Identity`, the `Provider`
interface, the live implementation (authorize-URL builder; code exchange
against the JSON-mode token endpoint with its 200-with-`error` contract; the
`/user`, `/user/emails`, and org-membership reads with 404-means-none; one
retry on 5xx; injectable `webBase`/`apiBase` roots; token confined to
`ExchangeCode` and discarded), and the `NewStub()` test double — mirroring
`internal/googleidp`'s layout. No server wiring in this phase; the package
stands alone and green.

**Done when:** R-I4AP-U132, R-I5IM-7STR, R-I6QI-LKKG, R-I7YE-ZCB5, and
R-I96B-D41U are each covered by a clearly-named test in
`internal/githubidp/*_test.go` (httptest fakes per design's testing strategy),
tagged verbatim, and the suite is green (`go build ./...`, `go vet ./...`,
`gofmt -l .` empty, `go test ./...` in `dashboard/`).
