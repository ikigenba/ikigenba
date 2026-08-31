---
description: a wait or retry loop with no cap, deadline, or failure path, whose termination depends on undocumented behavior of an injected dependency
severity: error
---
Flag loops that spin until an injected collaborator — a clock, a poller, a queue reader, a lock or token source — changes what it returns, with no iteration cap, no deadline, and no path that gives up and reports failure. The distinguishing property is that termination is not a property of this code at all: it is an unwritten contract on the interface, and nothing in the interface's documentation or type promises it. Substituting a plausible implementation (a frozen clock, a no-op sleep, a source that keeps returning the same value) hangs the program silently, with no output and no diagnostic. A hang is worse than an error here because it is undiagnosable from outside the process and cannot be scripted around. Flag the loop even when every implementation shipped today happens to behave, and flag it especially when the codebase's own test doubles include one that would hang.

Do not flag loops whose exit condition is driven by data this code already holds — iterating a slice, draining a scanner until `Scan` returns false, a `for range ch` over a channel the code closes. Do not flag an event loop, server accept loop, or supervisor that is deliberately infinite for the process's lifetime. Do not flag a loop bounded by a `context.Context`, a deadline check, an attempt counter, or a `select` with a timeout, however small the bound. Do not flag a spin whose collaborator is a concrete local value the same function advances.

```go
// Flagged: nothing bounds this; a Clock whose Sleep does not advance Now hangs forever.
for current.Before(deadline) {
	clock.Sleep(time.Millisecond)
	current = clock.Now()
}
```

```go
// Spared: bounded attempts, and a real failure to report when the bound is hit.
for attempt := 0; ; attempt++ {
	if current = clock.Now(); current.After(previous) {
		break
	}
	if attempt >= maxStalledPolls {
		return fmt.Errorf("clock did not advance past %s after %d polls", previous, attempt)
	}
	clock.Sleep(time.Millisecond)
}
```
