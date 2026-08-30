# D2-id-format

Package `internal/idgen` owns the id format grammar in both directions and is a
pure function of its inputs — no I/O, no clock. These constants are **frozen**:
ids minted by earlier idgen builds are already embedded throughout this
repository's specs and tests, and every one of them must remain decodable
forever.

An id is `<prefix>-XXXX-XXXX`. The 8-character body encodes the number of
milliseconds from a 2026 UTC epoch to the minting instant, passed through a
reversible affine bijection so consecutive milliseconds land far apart:

```
n = (ms · 0x9E3779B1 + 0xC0FFEE) mod 36⁸
```

`n` is rendered in uppercase base-36 (digits `0-9A-Z`), zero-padded to 8
characters, and split 4-4 with a dash. Decoding inverts the map with the
modular inverse of the multiplier. Decode grammar:
`^[A-Za-z0-9]+-([0-9A-Z]{4})-([0-9A-Z]{4})$` — the prefix is accepted and
ignored, since the instant lives entirely in the body.

Public API:

```go
package idgen

// Epoch is the zero point: 2026-01-01 00:00:00 UTC, constructed with
// time.Date(..., time.UTC).
var Epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// ErrInvalidID wraps the error TimeOf returns for a malformed id.
var ErrInvalidID = errors.New("invalid id")

// ValidPrefix reports whether prefix is a well-formed id prefix: a non-empty
// run of letters and digits, matching the prefix portion of the decode
// grammar. It is the single authority on the prefix grammar; callers that
// validate a prefix ask it rather than re-deriving the character class.
func ValidPrefix(prefix string) bool

// MintAt returns "<prefix>-XXXX-XXXX" for the given instant. Instants before
// Epoch are clamped to Epoch. The caller guarantees prefix satisfies
// ValidPrefix (cli validates at the flag boundary; D5); MintAt does not
// re-validate.
func MintAt(prefix string, t time.Time) string

// TimeOf inverts the body of any "<prefix>-XXXX-XXXX" id to the instant it
// was minted from, at millisecond precision, in UTC. Ids of any prefix
// decode. Returns an error wrapping ErrInvalidID when id is not canonical.
func TimeOf(id string) (time.Time, error)
```

The prefix is a parameter, not baked in: the prefix string is supplied by the
caller, and the decision to reject a bad one lives at the flag boundary in
`cli` (D5). But `idgen` owns the id format grammar in both directions, and the
prefix is part of that grammar, so `idgen` owns what a well-formed prefix *is*
and exports it as `ValidPrefix`. `cli` chooses when to validate and what to
report; it does not carry its own copy of the character class. One encoding of
the rule, in the package that owns the format — widening it (say, to allow
`_`) is then a single edit that `MintAt`, `TimeOf`, and the flag boundary all
follow together.

**Spec-system note.** idgen's output shares the exact shape of this
repository's requirement ids, and the spec system's gap greps design and test
files for that shape. Neither design prose nor test files may contain an
id-shaped literal (`R-` + 4 + 4 uppercase base-36 characters) that is not a
genuine requirement id — golden vectors are quoted as prefix and body
separately, and tests must join them at runtime rather than embedding the
full id as one literal.

Reference vectors, derived independently of any implementation (verified
against the shipping binary):

- ms 0 (the epoch itself) → body `0007-J3LA`
- 2026-03-15T12:00:00.000Z (ms 6350400000) → body `OBCA-0VLA`

## REQUIREMENTS

- R-SJ7P-ALD5: `TimeOf(MintAt(p, t))` MUST return `t` truncated to the millisecond, verified by an ordinary deterministic test (not a Go fuzz target) sweeping a large (hundreds+) PRNG-seeded sample of `ms ∈ [0, 36⁸)` across several valid prefixes.
- R-SKFL-OD3U: `MintAt("R", Epoch)` MUST return the id with prefix `R` and the independently derived golden body `0007-J3LA`, pinning the affine offset constant.
- R-SLNI-24UJ: `MintAt("R", ...)` of the literal absolute instant 2026-03-15T12:00:00.000Z (written as a civil time, not as an offset from the `Epoch` symbol) MUST return the id with prefix `R` and the independently derived golden body `OBCA-0VLA`, pinning the affine multiplier, the 4-4 split, and the 2026 epoch together.
- R-SMVE-FWL8: `MintAt` MUST zero-pad the body to exactly 8 characters for small millisecond values.
- R-SO3A-TOBX: `MintAt` MUST clamp instants before `Epoch` to `Epoch` (they encode as ms 0).
- R-SPB7-7G2M: `TimeOf` MUST decode ids with differing prefixes (e.g. `R`, `S`, `SPEC`) but the same body to the same instant.
- R-SQJ3-L7TB: `TimeOf` MUST return an error wrapping `ErrInvalidID` for every input that does not match the decode grammar `^[A-Za-z0-9]+-([0-9A-Z]{4})-([0-9A-Z]{4})$`.
- R-SRQZ-YZK0: `TimeOf` MUST never panic, verified by an ordinary deterministic test (not a Go fuzz target) sweeping a large PRNG-seeded sample of arbitrary and adversarial strings, each of which yields either an `ErrInvalidID`-wrapping error or a valid time.
- R-5ZQU-BTSZ: Package `idgen` MUST export `ValidPrefix(prefix string) bool`, returning true for exactly those strings that are non-empty and composed only of characters in `[A-Za-z0-9]`.
- R-60YQ-PLJO: `TimeOf`'s acceptance of the prefix portion of an id MUST agree with `ValidPrefix` — for a PRNG-seeded sample of candidate prefixes spanning valid runs, empty strings, and strings bearing separator, punctuation, and non-ASCII characters, an otherwise-canonical id built from a candidate MUST decode if and only if `ValidPrefix` accepts that candidate.
- R-SSYW-CRAP: Package `idgen` MUST panic at init if the affine multiplier and 36⁸ are not coprime (fail-loud: a non-invertible map would make every existing id irrecoverable).
