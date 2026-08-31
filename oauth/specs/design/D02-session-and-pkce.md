# D02-session-and-pkce

Package `internal/oauth` owns the OAuth protocol in both directions and is a
pure function of its inputs — no I/O of its own, no clock, no ambient
randomness. A `Session` carries the two per-login secrets from the authorize
request through to the token exchange:

```go
package oauth

// Session carries the per-login secrets from authorization through exchange.
type Session struct{ State, CodeVerifier string }

// NewSession draws both secrets from entropy. Entropy is a parameter, never a
// package-level default: a caller-supplied reader is what makes a session
// reproducible under test, and a nil-defaulting field would leave two code
// paths where one will do.
func NewSession(entropy io.Reader) (Session, error)
```

**Entropy is injected, not defaulted.** `crypto/rand.Reader` is wired in at the
composition root (D01), so this package never reaches for process-wide
randomness. That is what lets a deterministic reader produce a byte-for-byte
reproducible session, which in turn is what makes every downstream assertion in
D03 and D04 a fixed-value comparison rather than a shape check.

**The draw.** `NewSession` reads **64 bytes for the code verifier first, then 32
bytes for the state**, each rendered with `base64.RawURLEncoding` — base64url
with no `=` padding. Those byte counts and that order are contract, not
implementation: they are directly observable through the injected reader, and
every fixed-entropy test in this project depends on knowing which region of the
reader's stream becomes which secret. Rendered, they are an 86-character
verifier and a 43-character state.

**The verifier grammar is RFC 7636 §4.1**, quoted verbatim:

```
code-verifier = 43*128unreserved
unreserved    = ALPHA / DIGIT / "-" / "." / "_" / "~"
```

A 64-byte draw yields 86 characters, comfortably inside the 43–128 bound, and
`base64.RawURLEncoding`'s alphabet (`A-Z`, `a-z`, `0-9`, `-`, `_`) is a strict
subset of `unreserved`. Both facts hold for *every* 64-byte input, which is why
the grammar is asserted here as a **property of `NewSession`** over a
PRNG-seeded sample rather than defended by a runtime check.

That is a deliberate departure from the predecessor implementation, which
validated the verifier's length and character class inside `AuthorizeURL`. On
the only path that exists — a `Session` that `NewSession` produced — that guard
can never fire; it is reachable solely by hand-constructing a `Session` literal,
which nothing does. A guard that cannot fail is not a safety net, it is an
unreachable branch that must nonetheless be tested and maintained. Establishing
the grammar as a postcondition of the one constructor is stronger and cheaper,
and it is why `AuthorizeURL` is **infallible** in this design (D03).

**Failure.** Entropy can come up short. Each draw reports which secret it was
drawing when the read failed, so a diagnostic names the value rather than the
offset; both wrap the underlying read error so `errors.Is` still reaches it.

**State and verifier are independent secrets.** The state defends the callback
against a redirect the user's browser did not initiate (D06); the verifier
defends the token exchange against an intercepted authorization code (D04).
They are drawn separately and are never the same value.

## REQUIREMENTS

- R-ISXQ-JU4R: `NewSession` MUST render both `State` and `CodeVerifier` with `base64.RawURLEncoding`, so that for a PRNG-seeded sample of entropy inputs both values contain no `=` padding and consist only of characters in `[A-Za-z0-9\-_]`.
- R-IU5M-XLVG: For a PRNG-seeded sample of entropy inputs, the `CodeVerifier` `NewSession` returns MUST satisfy RFC 7636 §4.1's `code-verifier = 43*128unreserved` grammar — length between 43 and 128 inclusive, and every character drawn from `ALPHA / DIGIT / "-" / "." / "_" / "~"`.
- R-IVDJ-BDM5: `NewSession` MUST draw 64 bytes for the code verifier before drawing 32 bytes for the state, verified with a reader whose first 64 and next 32 bytes are distinguishable, yielding an 86-character `CodeVerifier` and a 43-character `State`.
- R-IWLF-P5CU: Two `NewSession` calls given readers over identical byte sequences MUST return identical `Session` values, and the returned values MUST depend only on those bytes (no ambient randomness).
- R-IXTC-2X3J: When entropy supplies fewer than 64 bytes, `NewSession` MUST return an error that names the code verifier as the value it failed to generate and that wraps the underlying read error.
- R-IZ18-GOU8: When entropy supplies at least 64 but fewer than 96 bytes, `NewSession` MUST return an error that names the state as the value it failed to generate and that wraps the underlying read error.
- R-J1H1-88BM: For a PRNG-seeded sample of entropy inputs, `NewSession` MUST return a `State` and a `CodeVerifier` that are never equal to each other, and two sessions drawn from differing entropy MUST differ in both fields.
