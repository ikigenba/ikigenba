# Phase 144 — Retrieval recall fixes: porter-stemmed FTS index, space-joined keyword routing, caller-limit honor

*Realizes design Decision 31 (keyword lane — stemming slice: R-NC9Q-4F8K) and 33 (fusion — R-NDHM-I6Z9, R-NEPI-VYPY).*

Three defects drop relevant pages before ask ever reads them; this phase removes
all three in the retrieval layer.

- **Porter-stemmed index (D31).** A new timestamped forward migration (via
  `bin/create-migration wiki`) drops and recreates `pages_fts` as the same
  external-content FTS5 table with `tokenize = 'porter unicode61'`, then
  rebuilds it from `pages` (`INSERT INTO pages_fts(pages_fts) VALUES('rebuild');`).
  Committed prior migrations stay untouched. The external-content sync dance and
  `ftsPhrase` are unchanged.
- **Space-joined keyword routing (D33).** `joinAnalyzedTerms` in
  `internal/retrieve/hybrid.go` joins keywords + aliases with plain spaces, so
  `ftsPhrase` — the single sanitizer — never quotes an injected `OR` operator
  into a literal search term.
- **Caller-limit honor (D33).** `FusionConfig.resolve` treats the configured
  `FinalK` as the fallback for an unset limit: an explicit requested limit
  governs the returned count in both directions, clamped to
  `retrieve.LimitCap` (20).

The retired routing behavior R-Q9ZE-LHF5 (OR-joined keyword lane text) is gone
from design; its tagged test is deleted with it.

**Done when:**

- R-NC9Q-4F8K — against a real temp SQLite with migrations applied, a `MATCH`
  for "kings" returns the page whose title/body say only "king" — covered by a
  named test tagged with the id.
- R-NDHM-I6Z9 — spy lanes show the meaning lane receives the sub-query sentence
  and the keyword lane receives the space-joined keywords + aliases with no
  non-term `OR` token — covered by a named test tagged with the id.
- R-NEPI-VYPY — with `FinalK` 8 configured and >8 distinct fused candidates, a
  request with limit 20 returns more than 8 hits and a request above 20 clamps
  to 20 — covered by a named test tagged with the id.
- `grep -rn "R-Q9ZE-LHF5" --exclude-dir=project .` from `wiki/` returns nothing
  (the retired id's test and tag are deleted).
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` empty, `go test ./...` all passing from `wiki/`).
