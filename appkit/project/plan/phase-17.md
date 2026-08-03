# Phase 17 — Correlation context at the chassis edge, on Crockford ULIDs

*Realizes design Decision 14 (correlation read-or-mint + Crockford ULID fix).*

**Cross-workspace dependency, stated in prose:** this phase cannot build until
the leaf package **`eventplane/correlation`** exists in the sibling `eventplane`
module — its `Header` constant, `New`/`Valid`, and the `With`/`From` context
accessors (see `project/research/research.md`). eventplane's own plan builds it;
appkit only consumes it. There is no earlier **appkit** phase this one depends
on.

**End state.** `appkit/logging` mints Crockford base32 ULIDs and no longer owns
a request-id context key: `NewULID` uses the alphabet
`0123456789ABCDEFGHJKMNPQRSTVWXYZ`, `WithRequestID` is gone, and
`RequestID(ctx)` reads through `correlation.From`. `RequestIDMiddleware` is
replaced by `CorrelationMiddleware(logger, next)`, which trusts a valid inbound
`X-Correlation-Id`, mints a fresh id when the header is absent or malformed,
puts the id on the context via `correlation.WithContext`, echoes it on the response as
both `X-Correlation-Id` and `X-Request-ID`, and logs it as `correlation_id` on
the existing begin/end debug records. `server.New` mounts the new middleware in
place of the old one. `appkit/go.mod` needs no new require — `eventplane` is
already a committed require/replace sibling.

**Done when:**
- These Verification ids are covered by clearly-named tests tagged with the id
  verbatim, driving the real middleware and the real `server.New` mux through
  `httptest`:
  - R-14QN-I04R — a valid inbound `X-Correlation-Id` is used verbatim on the
    context and echoed on the response (no fresh mint per hop).
  - R-15YJ-VRVG — an absent header yields a minted, 26-char, `correlation.Valid`
    id on the context, echoed on the response.
  - R-176G-9JM5 — a malformed inbound id (wrong length; and a 26-char value
    containing `I`/`L`/`O`/`U`) is replaced by a fresh valid id and never
    appears on the context or the response.
  - R-18EC-NBCU — 500 `logging.NewULID()` mints are all 26 chars, use only the
    Crockford alphabet, never contain `I`/`L`/`O`/`U`, and do produce the
    digits `0`/`1`.
  - R-19M9-133J — through the real chain, `X-Correlation-Id` and `X-Request-ID`
    on the response carry the same value, equal to the id the inner handler saw.
- The suite is green per design's *Conventions*: from `appkit/`, `go build
  ./...`, `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all
  succeed with zero failures.
- `grep -rn "RequestIDMiddleware\|WithRequestID" --include='*.go' .` from
  `appkit/` returns **no** matches (the old seam is deleted, not shadowed).
