---
description: one identifier bound to different kinds of declaration for the same concept in different places
severity: warning
---
Flag one identifier used for what is conceptually a single idea but bound to different kinds of declaration in different places — a variable in one package and a function in another, a type here and a plain field there, a constant in one file and a method elsewhere. A reader who has met one form mispredicts how the other is used, and navigation misleads: a search lands on both, go-to-definition picks one, and neither reveals that the other exists. In practice the collision is usually the symptom rather than the disease, since two spellings of one concept normally mean the concept was implemented twice; say so, and recommend naming the two forms apart or, better, collapsing them into one definition the other side calls.

Do not flag conventional pairings the language or its idiom requires: a type and its `NewType` constructor, an interface and a type deliberately named to match, a field and the accessor method that exposes it under a related name. Do not flag common short names reused for unrelated things in separate scopes (`i`, `err`, `buf`, `ok`, `out`), a test fixture named after the thing it stands in for, or two genuinely unrelated concepts that happen to share an obvious English word.

Flagged:

```go
// package cli
var validSlug = regexp.MustCompile(`^[a-z0-9-]+$`) // a var

// package store
func validSlug(s string) bool { ... } // a func — same name, same concept
```

Spared:

```go
type Encoder struct{ ... }

func NewEncoder(w io.Writer) *Encoder { ... } // conventional type/constructor pairing
```
