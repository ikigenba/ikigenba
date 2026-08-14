# Phase 56 — Source the shell footer version from the chassis runtime version

*Realizes design Decision 9 (shell footer version).*

The shell footer must show the real running version of the deployed binary, not
a hardcoded `v0.0.0+dev`. Today the footer's version comes from a page-local
`debug.ReadBuildInfo().Main.Version` lookup (`internal/server`'s `buildVersion`),
which is empty under the suite's production build (`GOWORK=off -buildvcs=false`,
built as its own main module) and so always collapses to the literal
`v0.0.0+dev`.

Rework the version to flow from the chassis through the composition root, exactly
as every other web-surface service does:

- `internal/server`'s server-construction seam (`server.Options`) gains a
  `Version string` field; the server stores it and every shell page's template
  data carries it. The `buildVersion` / `debug.ReadBuildInfo` page-local source
  is removed — there is no dashboard-local version source or fallback string.
- The composition root (`cmd/dashboard`) sets `Options.Version` from the chassis
  runtime version (`appkit` `Router.Version()`, i.e. `rt.Version()`), the same
  value `/health` and the `version` verb report.
- The shell footer renders that injected value verbatim; an unstamped local
  build shows `dashboard dev`.

The existing footer test (`internal/server/shell_adoption_test.go`) is tightened
so it constructs the server with a **known injected version** and asserts the
footer renders that exact string, replacing the weak `v\d+\.\d+\.\d+` regex that
the `v0.0.0+dev` fallback also satisfied.

**Done when:**
- R-VN4Y-ERZ1 — with the server constructed for a known injected
  `Options.Version`, the logged-in `GET /`, `/install`, `/metrics`, and
  `/profile` each render exactly one `<footer>dashboard <that exact version>`,
  and the logged-out `GET /` renders no `<footer>` — covered by a genuine test
  in `internal/server/*_test.go`.
- `debug.ReadBuildInfo` no longer appears in `dashboard/internal/server`
  (`grep -rn 'ReadBuildInfo' dashboard/internal/server` is empty).
- The dashboard green bar passes: `go build ./...`, `go vet ./...`, a silent
  `gofmt -l .`, and `go test ./...`.
