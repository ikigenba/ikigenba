# D01-layout-and-run-seam

auth is one Go binary that serves the platform's auth service. This document
fixes the skeleton the rest of the auth design hangs on: the module path, the
package set and what each package owns, the one-way import direction, and the
run seam through which the program touches the outside world. It designs no
endpoint, no config validation, no OAuth flow, and no persistence behavior —
only where those things live and how they are wired and tested. The later
design documents attach their contracts to the packages this document names:
`internal/server` owns the HTTP handlers and router, `internal/store` owns
persistence (D04), `internal/google` owns the Google/OIDC client (D05), and
`internal/idcodec` owns the opaque-id and secret encoding (D04).

The platform's apps share a run seam so their gates run offline and
deterministically. A `cli` package exposes `Run` plus a `Process` value that
carries everything the program would otherwise read from the ambient world:
the command-line arguments, an environment lookup, and the output streams.
`main` in `cmd/auth` is thin wiring — it builds a real `Process` from the OS
and calls `Run`, mapping the result to the process exit status. A test builds a
`Process` backed by buffers and fakes and drives `Run` without touching the
real environment. auth needs more injected than a plain app, because it reads
the clock, mints random ids and secrets, talks to Google, and opens a database;
so `Process` also carries a clock, a randomness source, the Google OIDC issuer
location (so a loopback fake stands in for Google offline), and the database
source (so a temporary or in-memory database stands in for `state/auth.db`).

The version is a value, not a build artifact: `internal/version` holds it as a
plain source-level `var` of shape `v<semver>`, so a developer's build and a
deployed binary report the same string and nothing is injected at link time.

## REQUIREMENTS

- R-3FKW-RNJY: The Go module path MUST be `github.com/ikigenba/ikigenba/auth`.
- R-3GST-5FAN: Package `main` in directory `cmd/auth` (file `cmd/auth/main.go`) MUST own `func main()` and MUST contain only wiring — building a `cli.Process` and calling `cli.Run` — with no other logic.
- R-3I0P-J71C: Package `internal/cli` MUST own the run seam: it owns the `Process` type and the `Run` function and nothing that belongs to another package's concern.
- R-3J8L-WYS1: Package `internal/server` MUST own the auth service's HTTP router and all of its HTTP handlers; every exported HTTP-handling name in the design lives here.
- R-3KGI-AQIQ: Package `internal/store` MUST own persistence: the domain entity types, the store handle, and every operation that reads or writes the database.
- R-3LOE-OI9F: Package `internal/google` MUST own the Google/OIDC client: every exported name that starts a Google sign-in, exchanges a code, or verifies an ID token lives here.
- R-3MWB-2A04: Package `internal/idcodec` MUST own opaque-id and secret encoding: the Crockford base32 encoder, the id and secret minters, and the secret hash.
- R-3O47-G1QT: Package `internal/version` MUST own the release version value and export it as `Version`.
- R-3QK0-7L87: `internal/cli` MUST export `type Process struct` with exactly these fields: `Args []string`, `Getenv func(string) string`, `Stdout io.Writer`, `Stderr io.Writer`, `Now func() time.Time`, `Rand io.Reader`, `OIDCIssuer string`, and `DBSource string`.
- R-3RRW-LCYW: `internal/cli` MUST export `func Run(p Process) int`.
- R-3SZS-Z4PL: `internal/version` MUST export `var Version string`, initialized in source to a string literal matching the shape `v<semver>` (a leading `v` followed by a semantic version), and its value MUST NOT depend on linker flags — a plain build with no `-ldflags` MUST yield that same literal value.
- R-3U7P-CWGA: The internal packages MUST form a one-way import graph in which `cmd/auth` imports `internal/cli`; `internal/cli` may import `internal/server`, `internal/store`, `internal/google`, `internal/idcodec`, and `internal/version`; `internal/server` may import `internal/store`, `internal/google`, `internal/idcodec`, and `internal/version`; `internal/store` and `internal/google` may import `internal/idcodec` and `internal/version`; `internal/idcodec` and `internal/version` MUST import no other `internal/*` package; and no `internal/*` package MUST import `internal/cli`.
- R-3VFL-QO6Z: `main` MUST construct a `Process` whose `Args` is the process's command-line arguments, `Getenv` looks up the process environment, `Stdout` and `Stderr` are the process's standard output and error streams, `Now` is the system wall clock, `Rand` is a cryptographically secure random source, `OIDCIssuer` is the production Google OIDC issuer location, and `DBSource` is the database path `state/auth.db`; and MUST then call `cli.Run` with that `Process`.
- R-3WNI-4FXO: `main` MUST terminate the process with the exact integer that `cli.Run` returns as the process exit status.
- R-3XVE-I7OD: `cli.Run` MUST return `0` on success, `1` when the server fails, and `2` on a usage error.
- R-3Z3A-VZF2: `cli.Run` MUST handle `SIGINT` and `SIGTERM` identically: either signal MUST initiate the same shutdown of a running server and MUST cause `Run` to return `0`.
- R-40B7-9R5R: Given a `Process` whose `Stdout` and `Stderr` are in-memory buffers, `Args` an explicit slice, `Getenv` a fake lookup, `Now` a fixed clock, `Rand` a deterministic reader, `OIDCIssuer` a loopback URL, and `DBSource` a temporary database, `cli.Run` MUST take every input and produce every output through that `Process` — reading arguments only from `Args`, environment only through `Getenv`, the current time only through `Now`, and randomness only through `Rand` — and MUST NOT read the real process arguments, the real process environment, the real wall clock, or a global random source.
- R-YNFB-36HN: A running `auth` server MUST be self-contained in its executable: it MUST serve its HTML, JavaScript, and CSS from assets embedded in the binary, MUST depend on no shared library at run time, and MUST open no file at run time other than its SQLite database at `state/auth.db`.
