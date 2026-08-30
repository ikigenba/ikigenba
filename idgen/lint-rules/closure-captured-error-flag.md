---
description: error state accumulated by mutating a variable captured in a closure instead of being returned by the helper that detects it
severity: warning
---
Flag a local closure that reports failure by assigning to a captured boolean or error variable rather than returning it, especially when the closure is invoked from more than one place in the enclosing function. The failure signal then travels by side effect: a reader who reaches the final check on the flag has to find every call site to learn when it can be set, and the closure cannot be moved, reused, or tested on its own because its result is not in its signature. Recommend giving the helper a result the caller inspects, or unifying the call sites so a single loop can handle the outcome directly.

Do not flag a closure that must match a callback signature with no error result — a `filepath.Walk` or tree-visitor callback, a sort comparator, an HTTP handler — where capturing is the only way to get a value out. Do not flag genuine accumulators whose whole purpose is to aggregate across many calls and be read once, such as a counter or a collected multi-error, and do not flag concurrent patterns using an errgroup or a mutex-protected field. A captured variable holding non-error state is a different question and is out of scope here.

Flagged:

```go
failed := false
handle := func(token string) {
	v, err := parse(token)
	if err != nil {
		report(err)
		failed = true // signal escapes by side effect, from two call sites
		return
	}
	emit(v)
}
for _, t := range args {
	handle(t)
}
for scanner.Scan() {
	handle(scanner.Text())
}
if failed {
	return exitFailure
}
```

Spared:

```go
ok := true
for _, token := range tokens { // one call site, result in the signature
	if !handle(token) {
		ok = false
	}
}
```
