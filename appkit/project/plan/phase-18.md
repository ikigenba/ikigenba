# Phase 18 — The telemetry record shape, digests, and the param encoder

*Realizes design Decision 16 (record shape + capture-under-threshold encoder).*

**End state.** A new package `appkit/telemetry` exists carrying the pure half of
the capability and nothing else: the `Kind` constants, the `Actor`, `Outcome`,
and `Record` types with the contract's exact JSON tags and omit-empty rules
(`time` marshalling RFC3339Nano in UTC), the `Digest(b []byte) (int, string)`
helper over `crypto/sha256`, and `EncodeParams(raw json.RawMessage, sensitive
[]string) map[string]any` implementing, in order: sensitive-name elision, the
1024-byte per-value cap, and the 8192-byte per-record budget by eliding the
largest remaining value repeatedly (ties broken by key name ascending). Elision
replaces a value in place with `{"$elided":{"bytes":N,"sha256":"<hex>"}}`. The
package has no HTTP, no goroutines, and no dependency beyond the standard
library at this point.

**Done when:**
- These Verification ids are covered by clearly-named tests tagged with the id
  verbatim, asserting against real `encoding/json` and real `crypto/sha256`:
  - R-1I5J-PHAE — the marshalled `Record` carries exactly the contract keys and
    no others; empty optional fields are omitted; `time` is RFC3339Nano UTC.
  - R-1JDG-3913 — a 2000-byte value is elided with the exact `bytes` and the
    independently computed digest; a 1024-byte value is kept literally.
  - R-1LT8-USIH — with values of ~5000/~4000/~200/~100 bytes the encoded map
    ends ≤ 8192 bytes with the two largest elided and the two smallest verbatim.
  - R-1N15-8K96 — a 3-byte param declared sensitive is elided while an
    undeclared sibling of the same size is kept.
  - R-1O91-MBZV — `Digest` returns the exact length and a 64-char lowercase-hex
    SHA-256 of a fixed known input, and every emitted `sha256` matches
    `^[0-9a-f]{64}$`.
- The suite is green per design's *Conventions* (`go build ./...`, `go vet
  ./...`, `gofmt -l .` empty, `go test ./...`, all from `appkit/`).
