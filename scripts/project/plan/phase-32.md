# Phase 32 — Env-channel conformance: the run TTL in the manifest

*Realizes design Decision 33 (env-channel conformance).*

What gets built: `Spec.ManifestExtras` in `cmd/scripts/main.go` gains
`SCRIPTS_RUN_TTL=30m` after the retention pair, and the committed
`scripts/etc/manifest.env` gains the matching line (the existing drift test
pins the byte agreement); a source-scan test in `cmd/scripts/main_test.go`
guards scripts' non-test Go source against `"/opt` string literals. End state:
the committed manifest is the complete operator surface for scripts' universal
knobs.

**Done when:**
- R-HDCE-C6WU — the resolved no-env run TTL equals the committed
  `scripts/etc/manifest.env` value.
- R-VKB6-SHHV — the source-scan test walks scripts non-test `.go` files and
  finds no `"/opt` string literal.
- Both ids appear verbatim as tags in test files under `scripts/`, and the
  suite is green per design Conventions (`cd scripts && go test ./...`, plus
  build/vet).
