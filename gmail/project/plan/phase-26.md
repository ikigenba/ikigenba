# Phase 26 — Env-channel conformance: the poll interval in the manifest

*Realizes design Decision 24 (env-channel conformance).*

What gets built: `Spec.ManifestExtras` in `cmd/gmail/main.go` gains
`GMAIL_POLL_INTERVAL=60s` after the retention pair, and the committed
`gmail/etc/manifest.env` gains the matching line (the existing drift test pins
the byte agreement); a source-scan test in `cmd/gmail/main_test.go` guards
gmail's non-test Go source against `"/opt` string literals. End state: the
committed manifest is the complete operator surface for gmail's universal
knobs.

**Done when:**
- R-JWR1-BQ0J — the resolved no-env poll interval equals the committed
  `gmail/etc/manifest.env` value.
- R-VKB6-SHHV — the source-scan test walks gmail non-test `.go` files and
  finds no `"/opt` string literal.
- Both ids appear verbatim as tags in test files under `gmail/`, and the suite
  is green per design Conventions (`cd gmail && go test ./...`, plus
  build/vet).
