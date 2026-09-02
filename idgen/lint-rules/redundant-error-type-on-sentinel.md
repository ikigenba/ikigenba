---
description: a sentinel error var annotated with an explicit error type its right-hand side already yields, so the annotation is pure redundancy
severity: warning
include: ["**/*.go"]
---
Flag a package-level error `var` written `var ErrX error = <expr>` where `<expr>` already has static type `error` — `errors.New(...)`, `fmt.Errorf(...)`, or any call or identifier that returns `error`. The explicit `error` restates a type Go infers unaided, adding nothing to the declaration; the idiomatic spelling omits it, `var ErrX = errors.New(...)`, exactly as the standard library writes `io.EOF`, `sql.ErrNoRows`, and `os.ErrNotExist`. A reader who meets the annotated form pauses to ask what the type is doing, because in Go an explicit interface type on a `var` normally signals a deliberate widening — and here it signals none. A `//nolint` silencing golangci-lint or revive's `var-declaration` warning on such a line does not justify the annotation; it entrenches the redundancy and hides it from the one tool that would flag it. Recommend dropping both the `error` annotation and any suppression that guards it.

Do not flag a declaration whose right-hand side has a *concrete* error type — `&parseError{...}`, `SomeErr{}`, a value of `*fs.PathError` — where the explicit `error` deliberately widens the variable's static type from that concrete type to the interface: there the annotation changes the inferred type and is meaningful, keep it. Do not flag local variables or struct fields typed `error` (their type is declared, not inferred from a same-typed initializer), non-error declarations, or any var whose written type genuinely differs from what the right-hand side would infer.

Flagged:

```go
// error is already the static type of errors.New's result; the annotation restates it
var ErrInvalidID error = errors.New("invalid id")

// a nolint that silences revive here entrenches the redundancy rather than earning it
//nolint:revive // "static error type is part of the API contract"
var ErrTimeout error = fmt.Errorf("request timed out")
```

Spared:

```go
// idiomatic sentinel: type omitted, inferred as error
var ErrInvalidID = errors.New("invalid id")

// explicit error widens *parseError to the interface — meaningful, keep it
var ErrParse error = &parseError{code: 42}
```
