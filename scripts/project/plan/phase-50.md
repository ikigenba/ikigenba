# Phase 50 — `webhooks` trigger source

*Realizes design Decision 44 (webhooks as trigger source).*

Add `webhooks` as the seventh consumed event source, exactly as the six existing
sources are wired — no new machinery. `internal/script/trigger.go` gains the
`"webhooks": {"received"}` entry in `knownFamilies`; `cmd/scripts/main.go` adds
`scriptsConsumerEntry("webhooks")` to the assembled `Spec.Consumers`. Validation,
the routing matcher, and `consume.Handler` are untouched; the known-source set
becomes seven and unknown-source rejection names all seven.

**Done when:**

- **R-IM5H-6CKN** — `webhooks` validates as an ordinary source:
  `webhooks:received/mg-dev-track` and `webhooks:received/**` are accepted;
  `webhooks:nosuchkind/**` is rejected with `ErrValidation`; an unknown source
  (`github:push/**`) is rejected with an error naming all seven known sources,
  `webhooks` present and `scripts` absent.
- **R-INDD-K4BC** — `scriptsSpec()` declares seven consumer entries whose sources
  are exactly `cron`, `crm`, `ledger`, `dropbox`, `prompts`, `repos`, `webhooks`,
  each with a single subscription whose `Filter` is exactly `"**"`; and
  `ScriptsForEvent(ctx, "webhooks", "webhooks:received/mg-dev-track")` over real
  SQLite returns the script holding `webhooks:received/**` and not one holding a
  different source's filter.
- The suite is green: `cd scripts && go build ./...`, `cd scripts && go vet ./...`,
  `cd scripts && gofmt -l .` (no output), and `cd scripts && go test ./...` all
  succeed.
