---
description: hand-rolled modular or overflow-avoiding arithmetic with no comment stating the bound it protects
severity: warning
---
Flag arithmetic that is written the long way specifically to stay inside a numeric range, where nothing in the code says which range or why. The recognizable shapes are a hand-rolled multiply-by-doubling or add-in-a-loop where a single `*` would read naturally; a redundant-looking `x % m` applied to a value the reader can already see is smaller than `m`; adding the modulus before taking a remainder (`(a + m - b) % m`) to dodge a language's signed-remainder rule; splitting a value into high and low halves before combining; and casts to a wider type mid-expression. Each of these is correct and each is *invitingly* simplifiable — the next reader sees ceremony around an operation they know, deletes it, and the test suite may only catch the regression stochastically because small inputs still round-trip. The comment that earns its keep names the concrete bound: which product would overflow, at what magnitude, and therefore what the workaround preserves. Flag the function or expression once, not every operation inside it.

Do not flag ordinary modular arithmetic whose purpose is visible from the domain — wrapping a ring buffer index, bucketing a hash, computing a checksum — where the modulus is a named constant and no overflow question arises. Do not flag arithmetic that already carries a comment, doc comment, or named intermediate variable stating the bound, even informally; the rule wants the rationale recorded somewhere, not in a particular place. Do not flag a direct transcription of a named published algorithm where the citation is present, and do not flag arbitrary-precision or checked-arithmetic library calls, which state their own intent. Do not flag a language whose integers do not overflow.

```go
// Flagged: the doubling loop exists only because value*factor would overflow,
// and the `+ modulus` only because % keeps the dividend's sign — neither is stated.
func multiplyMod(value, factor int64) int64 {
	value %= modulus
	var product int64
	for factor > 0 {
		if factor&1 != 0 {
			product = (product + value) % modulus
		}
		value = (value * 2) % modulus
		factor >>= 1
	}
	return product
}

difference := (n + modulus - offset) % modulus
```

```go
// Spared: the bound is named, so the workaround cannot be "simplified" by accident.
// multiplyMod multiplies mod modulus by doubling: a direct value*factor would
// reach ~2.4e21 and overflow int64, while each doubling peaks at 2*(modulus-1).
func multiplyMod(value, factor int64) int64 {
	...
}

// Adding modulus keeps the dividend non-negative; Go's % takes the dividend's sign.
difference := (n + modulus - offset) % modulus
```

```go
// Spared: plain modular arithmetic with a self-evident domain and no overflow question.
slot := hash % uint64(len(buckets))
```
