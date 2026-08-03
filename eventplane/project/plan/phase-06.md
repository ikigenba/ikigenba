# Phase 6 — The `correlation` leaf package

*Realizes design Decision 6 (correlation primitives).*

A new package `eventplane/correlation` exists at `eventplane/correlation/`,
exporting `Header`, `New`, `Valid`, `WithContext`, `FromContext` and `Ensure`
exactly as D6 declares them. It is stdlib-only: it imports no third-party
package and none of `outbox`, `consumer` or `routing`, so everything downstream
of this module can take it as a dependency without pulling in the event plane.
The minter produces Crockford base32 (`0123456789ABCDEFGHJKMNPQRSTVWXYZ`), not
RFC 4648; the context key is an unexported zero-size struct type, so the value
is reachable only through the accessors. Nothing outside the new package
changes in this phase.

**Done when:**

- `go test ./...` and `go vet ./...` from `eventplane/` both exit 0, and
  `gofmt -l .` prints nothing.
- These behaviors are covered by clearly-named tests in
  `eventplane/correlation/*_test.go`, each citing its id:
  - R-UBWK-3IAS — 26 characters, all from the Crockford alphabet; across 1000
    mints no `I`/`L`/`O`/`U` appears and each of `0`, `1`, `8`, `9` appears at
    least once.
  - R-UD4G-HA1H — 1000 mints are distinct; across 20 pairs separated by a ≥2ms
    pause, the later id sorts strictly greater as a string.
  - R-UECC-V1S6 — `Valid` accepts minted ids and rejects `""`, 25- and
    27-character ids, a lowercase spelling, and a 26-character string
    containing `I`/`L`/`O`/`U`.
  - R-UFK9-8TIV — `FromContext` of a bare context is `""`; a minted id round
    trips verbatim; `WithContext` with a malformed id leaves `FromContext` at
    `""`.
  - R-UGS5-ML9K — `Ensure` returns an existing id unchanged and never
    re-mints; on a bare context it returns a `Valid` id matching the returned
    context, and two bare contexts get two different ids.
  - R-UI02-0D09 — `Header == "X-Correlation-Id"` byte-for-byte.
- The package is genuinely a stdlib-only leaf:
  `go list -deps eventplane/correlation | grep -vE '^(internal/|[a-z0-9]+(/|$))'`
  run from `eventplane/` prints nothing (every dependency is a stdlib path;
  no `eventplane/…` and no third-party module appears).
- `git diff --name-only` for this phase touches only `eventplane/correlation/`
  (plus `project/`), and `eventplane/go.mod` gains no `require` line:
  `git diff -- go.mod | grep -c '^+.*require'` is `0`.
