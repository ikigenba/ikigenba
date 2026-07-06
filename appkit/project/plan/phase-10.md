# Phase 10 — Chassis-owned consumer loops (`Spec.Consumers`)

*Realizes design Decision 10 (the declared consumer table). Depends on Phase 09
(the reflection tool the subscriptions derivation feeds) and Phase 05's
`appkit/config` resolution idiom; the `eventplane/consumer` engine and the
`registry` module are fixed external contracts consumed as-is.*

Observable end state:

- The root `appkit` package declares `type Consumer` (`Source`,
  `Subscriptions`, `Handler func(*Router) consumer.Handler`) and `Spec` gains
  `Consumers []Consumer`; `appkit/go.mod` carries `require registry v0.0.0`
  and `replace registry => ../registry`.
- `appkit/config` resolves, per app/source pair, the feed URL
  (`<APP>_<SRC>_FEED_URL`, default `registry.BaseURL(src)+"/feed"`) and the
  first-subscription choice (`<APP>_<SRC>_FROM`, default `"tail"`), pure over
  injected `getenv`.
- The manifest verb emits `CONSUMES=` from the table's sources; the reflection
  tool reports the concatenated per-entry subscriptions; a Spec mixing
  `Consumers` with legacy `Consumes` or `Subscriptions` fails startup loudly.
- Serve launches one `eventplane/consumer.Run` loop per entry on the serve
  context with the `Workers` lifecycle semantics, `ConsumerID = Spec.App`,
  handler factories invoked after `Handlers`/`Producer` with the built Router.
- Specs that don't set `Consumers` (including every existing appkit test)
  behave exactly as before.

**Done when:** the suite is green — `cd appkit && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
zero failures, and `GOWORK=off go build ./...` succeeds from `appkit/` (the
isolated build proving the committed registry replace) — and:

- R-4199-A0U9, R-42H5-NSKY, R-44WY-FC2C, R-464U-T3T1, R-47CR-6VJQ,
  R-48KN-KNAF, and R-49SJ-YF14 (D10) are covered by clearly-named tests, with
  R-49SJ-YF14 driven against a real `httptest` SSE feed and a real
  `t.TempDir()` SQLite database;
- the pre-existing appkit test suite passes with no assertion changes (the
  D10 additivity proof).
