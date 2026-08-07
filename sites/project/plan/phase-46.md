# Phase 46 — Compose the served-tree root from `IKIGENBA_ROOT`; delete the compiled-in `/opt` default

*Realizes design Decision 30 (composed served-tree root).*

What gets built: `cmd/sites/main.go` resolves the served-tree root once by the
`SITES_ROOT` override → `IKIGENBA_ROOT` composition
(`<root>/sites/state/www`) → `./tmp/www` dev-fallback ladder and injects it;
`internal/sites/layout.go` loses the `DefaultRoot` constant, `NewLayout("")`
becomes an error, and the zero-`Layout` fallback in `root()` is deleted (path
helpers unchanged). A source-scan test in `cmd/sites/main_test.go` guards
sites' non-test Go source against `"/opt` string literals. End state: the
deployed layout is byte-identical (`IKIGENBA_ROOT=/opt` composes the old
path), and no box-path literal remains in sites production code.

**Done when:**
- R-YWR7-Z1GM — resolver ladder test passes: explicit `SITES_ROOT` wins; else
  `IKIGENBA_ROOT`/sites/state/www; else `./tmp/www` — never a compiled-in
  `/opt` path.
- R-YXZ4-CT7B — `NewLayout("")` returns an error and no package code path
  substitutes a compiled-in root for an empty one.
- R-VKB6-SHHV — the source-scan test walks sites non-test `.go` files and
  finds no `"/opt` string literal.
- All three ids appear verbatim as tags in test files under `sites/`, and the
  suite is green per design Conventions (`cd sites && go test ./...`, plus
  build/vet).
