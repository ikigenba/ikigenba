---
description: a constant computed from other constants or an external derivation, recorded as a literal whose origin only a comment (or nothing) explains
severity: error
---
Flag a literal constant that is not a free choice but the *result* of something — an exponentiation or product of sibling constants, a conversion between units already named nearby, a value read off a specification or table, or the output of an offline computation — where the code stores only the answer. The tell is a comment doing the work the code should do (`modulus = 56800235584 // 62^6`), or a value that is impossible to check by reading (`inverse = 25576638575`), or two constants that must move together with nothing forcing them to. The comment is not a safeguard: change the base or the digit count and the derived literal keeps compiling, keeps passing the tests that use it consistently, and starts producing wrong results. Prefer expressing the derivation in the language — a computed constant expression, an initializer, or a compile-time/startup assertion tying the literal to its inputs. Where the value genuinely cannot be computed in-language, require provenance a reader can act on: the formula, the source document and section, or the script that produced it.

Do not flag literals that are definitions rather than derivations — a chosen page size, a protocol's own magic number, a tuning threshold — for which there is nothing to derive them from. Do not flag a constant already accompanied by an executable check that ties it to its inputs, nor one written as an expression over its inputs even if the compiler folds it. Do not flag values whose derivation is universally recognizable in context (`secondsPerHour = 3600`, `1 << 20`). Do not flag test fixtures and golden vectors, whose whole purpose is to be an independently-obtained constant; flagging those defeats the test. A single well-placed provenance comment on an uncomputable value is sufficient — the rule is about traceability, not ceremony.

```go
// Flagged: two derived values, one explained only by a comment and one not at all;
// changing base or codeDigits silently invalidates both.
const (
	base       = 62
	codeDigits = 6
	modulus    = 56800235584 // 62^6
	inverse    = 25576638575
)
```

```go
// Spared: the derivation is code, and the value that cannot be folded is asserted.
const (
	base       = 62
	codeDigits = 6
	modulus    = int64(math.Pow(base, codeDigits)) // or an explicit product
	// inverse is the modular inverse of multiplier mod modulus, found with the
	// extended Euclidean algorithm; the init check below is what actually enforces it.
	inverse = 25576638575
)

func init() {
	if multiplyMod(multiplier, inverse) != 1 {
		panic("inverse is not the modular inverse of multiplier")
	}
}
```

```go
// Spared: a chosen value, not a derived one — there is nothing to trace it to.
const maxRetries = 5
```
