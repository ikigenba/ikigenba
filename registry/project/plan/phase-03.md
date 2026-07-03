# Phase 3 — The resolution API (Port / MustPort / BaseURL)

*Realizes design Decision 3 (the resolution API), ids `R-B642-6EO8`,
`R-B7BY-K6EX`, `R-B8JU-XY5M`, `R-B9RR-BPWB`. Depends on Phase 2 (the `Services`
table exists).*

## What gets built

Package `registry`, in `registry/registry.go` (extending the file from Phase 2):

- `const loopbackHost = "127.0.0.1"` — the one place the host is written.
- An O(1) name→port index built once from `Services` (a `map[string]int` in an
  `init`, or a package-level value derived from the slice); `Services` stays the
  source of truth.
- `func Port(name string) (port int, ok bool)` — checked lookup; `ok == false` and
  `port == 0` for an unknown name.
- `func MustPort(name string) int` — returns the port or **panics** on an unknown
  name.
- `func BaseURL(name string) string` — returns `"http://127.0.0.1:<port>"` for a
  known name (via `MustPort` + `loopbackHost`), no trailing path; panics on
  unknown. No `FeedURL`/route helpers — callers append their own path.

Tests in `registry/registry_test.go` (package-local, named for the behavior),
using `recover` to assert the panics.

Observable end state: callers resolve any known service by name to its port and to
a loopback base URL, `dashboard` → `3000` / `http://127.0.0.1:3000`, and unknown
names fail loudly (checked miss on `Port`, panic on `MustPort`/`BaseURL`).

## Done when

All of the following hold on identical repo state, from the module root
(`registry/`):

- `GOWORK=off go build ./...` exits 0.
- `GOWORK=off go test ./...` exits 0 with no failures and no `SKIP`.
- Each id is covered by a genuinely-asserting, package-local
  `registry/*_test.go` test tagged with a `// R-XXXX-XXXX` comment line and named
  for the behavior; `grep -rE 'R-B642-6EO8|R-B7BY-K6EX|R-B8JU-XY5M|R-B9RR-BPWB' registry --include='*_test.go'`
  returns ≥ 4 matching lines, asserting:
  - `R-B642-6EO8` — `Port` returns the exact registered port and `ok == true` for a
    known name (e.g. `Port("crm")` → `3100, true`).
  - `R-B7BY-K6EX` — `Port` returns `ok == false` and `port == 0` for an unknown
    name, without panicking.
  - `R-B8JU-XY5M` — `MustPort` panics on an unknown name (asserted via `recover`)
    and returns the correct port for a known one.
  - `R-B9RR-BPWB` — `BaseURL("crm")` equals `"http://127.0.0.1:3100"` exactly (no
    trailing path/slash; correct host and port).
