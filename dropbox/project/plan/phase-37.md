# Phase 37 — Env-channel conformance: sync tuning knobs in the manifest

*Realizes design Decision 29 (env-channel conformance).*

What gets built: `Spec.ManifestExtras` in `cmd/dropbox/main.go` gains
`DROPBOX_LONGPOLL_TIMEOUT=480` and `DROPBOX_MAX_ENTRY_RETRIES=5` after the
retention pair, and the committed `dropbox/etc/manifest.env` gains the matching
lines (the existing drift test pins the byte agreement); a source-scan test in
`cmd/dropbox/main_test.go` guards dropbox's non-test Go source against `"/opt`
string literals. End state: the committed manifest is the complete operator
surface for dropbox's universal knobs.

**Done when:**
- R-M0AZ-C8H7 — resolved no-env knob defaults equal the committed
  `dropbox/etc/manifest.env` values.
- R-VKB6-SHHV — the source-scan test walks dropbox non-test `.go` files and
  finds no `"/opt` string literal.
- Both ids appear verbatim as tags in test files under `dropbox/`, and the
  suite is green per design Conventions (`cd dropbox && go test ./...`, plus
  build/vet).
