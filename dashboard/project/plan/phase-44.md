# Phase 44 — Env-channel conformance: composed manifest root, manifest-surfaced authn knobs, no box-path literals

*Realizes design Decision 36 (env-channel conformance).*

What gets built: the dashboard's manifest/inventory root and metrics disk path
are composed once at the composition root (`cmd/dashboard/main.go`) by the
override → `IKIGENBA_ROOT` → `.` ladder and passed to every consumer; the
internal re-defaults to `"/opt"` are deleted (`registerRoutes`'s re-derivation,
`internal/server/server.go`'s empty-root fallback, `internal/metrics`'
`withDefaults` arms for `ManifestRoot`/`DiskPath`, and `readers.readDiskFree`'s
empty-path arm), with empty-root construction failing loudly instead. The `Spec`
gains the two authn `ManifestExtras` and `dashboard/etc/manifest.env` gains the
matching `DASHBOARD_AUTHN_RATE_LIMIT=120` and `DASHBOARD_AUTHN_RATE_WINDOW=10s`
lines (the existing byte-equality drift test pins them). A source-scan test in
`cmd/dashboard/main_test.go` guards the tree against any `"/opt` string literal
in non-test Go source. End state: no `"/opt` literal anywhere in dashboard
production code, and the committed manifest is the complete operator surface
for the dashboard's universal knobs.

**Done when:**
- R-GBZ2-DNKQ — resolver ladder test passes: explicit `DASHBOARD_MANIFEST_ROOT`
  wins; else `IKIGENBA_ROOT`; else `.` — never `/opt`.
- R-GD6Y-RFBF — `server.New` errors on empty `Options.ManifestRoot`; metrics
  collector construction errors on empty `ManifestRoot` or `DiskPath`.
- R-GEEV-5724 — resolved no-env authn defaults equal the committed
  `dashboard/etc/manifest.env` values.
- R-VKB6-SHHV — the source-scan test walks dashboard non-test `.go` files and
  finds no `"/opt` string literal.
- All four ids appear verbatim as tags in test files under `dashboard/`, and
  the suite is green per design Conventions (`cd dashboard && go test ./...`,
  plus build/vet).
