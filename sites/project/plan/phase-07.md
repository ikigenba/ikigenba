# Phase 07 — Adopt the shared `registry`: resolve sites's own port and the dropbox mirror address by name

*Realizes design Decision 9. Touches `cmd/sites/main.go` (two call-site swaps +
one import) and `sites/go.mod` (add `require registry` + committed
`replace registry => ../registry`). Adds tests under `cmd/sites/`. No schema
change, no new domain logic — a behavior-preserving swap of two loopback port
literals for `registry` lookups at the composition root.*

> **External preconditions (owned outside `sites/`, assumed satisfied — do NOT
> touch them here):** the repo-root `go.work use ./registry` entry and the
> `registry` module itself (module `registry` at the repo root, with `MustPort`,
> `Port`, `BaseURL` per its D03 and the `sites → 3004` / `dropbox → 3200` rows per
> its D02). This phase runs from `sites/` and only imports the library and wires
> `sites/go.mod`. A change to `../go.work`, `../registry/`, `../bin/`, nginx, or any
> sibling module is out of scope by construction.

Today sites hardcodes two loopback port literals in its composition root
(`cmd/sites/main.go`): its **own** port (`Port: 3004` in `sitesSpec()`) and the
**dropbox** peer address (`"http://127.0.0.1:3200"`, the `DROPBOX_BASE_URL`
fallback default that wires `sites.NewMirrorClient`). Both numbers are duplicated
across the suite and drift silently — a renumber is invisible to the compiler and
only surfaces at deploy. This phase makes the shared `registry` table the single
source of truth for both, resolving each **by name at startup** (in `main`,
which runs once), so no bare loopback port literal remains in sites's production
Go source. The registry table pins `sites → 3004` and `dropbox → 3200`, i.e. the
exact values sites holds today, so this is **behavior-preserving** — sites does
not renumber and no runtime behavior changes.

In **`sites/go.mod`**:
- Add `require registry v0.0.0` to the require block (alongside `appkit`,
  `agentkit`).
- Add a committed `replace registry => ../registry` beside the existing
  `appkit`/`agentkit`/`eventplane` replaces, so the `GOWORK=off` production build
  resolves the leaf library deterministically.

In **`cmd/sites/main.go`**:
- Import `registry`.
- **Own port:** in `sitesSpec()`, `Port: 3004` → `Port: registry.MustPort("sites")`.
  `MustPort` is the strict composition-root form (panics on an unknown name, which
  is a programming error); it returns `3004`, so the Spec value is unchanged.
  appkit's existing `SITES_PORT` env override still layers over this default —
  registry supplies the literal's replacement, not a new override tier.
- **Dropbox mirror default:** in the `Spec.Handlers` closure,
  ```go
  base := config.EnvOr(os.Getenv, "DROPBOX_BASE_URL", "http://127.0.0.1:3200")
  ```
  →
  ```go
  base := config.EnvOr(os.Getenv, "DROPBOX_BASE_URL", registry.BaseURL("dropbox"))
  ```
  `registry.BaseURL("dropbox")` composes exactly `"http://127.0.0.1:3200"` (no
  path — `NewMirrorClient` derives `<base>/list` and `<base>/content` as before).
  The `DROPBOX_BASE_URL` env override still takes precedence when set — this is
  appkit's `config.EnvOr` layering, unchanged; only the hardcoded default is
  replaced.
- Leave the `config`, `os`, and `strings` imports and every other wiring line
  untouched.

> **Do NOT convert the nginx fragment.** `etc/nginx.conf` carries literal
> `127.0.0.1:3004` in its `proxy_pass` lines, and `internal/web/nginx_test.go`
> asserts them. nginx reads that config directly and cannot call a Go library, so
> `registry` cannot supply its address — the fragment literal (and the test that
> mirrors it) is the one deliberate exception to "no `30xx` literal remains" (D9
> **Boundary**). Leave both `etc/nginx.conf` and `internal/web/nginx_test.go`
> exactly as they are; the guardrail below is scoped to production Go and excludes
> `*_test.go`.

**Done when:** the suite is green (per design *Conventions*: `cd sites && go build
./...`, `cd sites && go vet ./...`, `cd sites && gofmt -l .` prints nothing,
`cd sites && go test ./...`, and `bin/check-migrations sites` all succeed with zero
failures) and these ids are covered by clearly-named tests in `cmd/sites/`:

- **R-7K2P-QN4D** — `sitesSpec().Port` equals `registry.MustPort("sites")` (which
  is `3004`): the Spec's port is resolved from the registry by name, not a literal.
  A `Port:` set to a bare int, or resolving to any other value, fails it.
  *(in-package test in `cmd/sites`, no network)*
- **R-7L9F-XW3H** — a walk of every `*.go` under the module **excluding
  `*_test.go`** finds no `127.0.0.1:30` substring and no standalone `3004` token:
  no hardcoded loopback port literal remains in production Go. Reintroducing
  `"http://127.0.0.1:3200"` or a literal `Port: 3004` in production Go fails it.
  The scan walks the module root (`../..` from `cmd/sites`, the same relative-path
  device `internal/web/nginx_test.go` uses to read `../../etc/nginx.conf`), and
  deliberately skips `*_test.go` and non-Go files — so `etc/nginx.conf` and
  `nginx_test.go`'s legitimate `127.0.0.1:3004` assertions are out of scope.
  *(source-tree scan test in `cmd/sites`)*
- **R-7M4C-BV8J** — `registry.BaseURL("dropbox")` composes exactly
  `"http://127.0.0.1:3200"`: the default sites now feeds the `DROPBOX_BASE_URL`
  fallback resolves, via the registry, to the exact address the old literal held
  (behavior-preserving). A different host, port, or a trailing path fails it.
  *(in-package test in `cmd/sites`)*
- **R-7N6R-TZ2Q** — a content assertion over `../../go.mod` finds both a
  `require registry` line and a `replace registry => ../registry` directive, so the
  `GOWORK=off` prod build resolves the leaf library deterministically. A missing
  `require` or `replace` fails it. *(go.mod content-assertion test in `cmd/sites`)*

Note the pre-existing `cmd/sites` tests (`main_test.go`) and the nginx-fragment
tests (`internal/web/nginx_test.go`) must stay green unchanged — this phase adds
no behavior, so `SITES_PORT`-driven startup and the nginx `127.0.0.1:3004`
assertions are unaffected.
