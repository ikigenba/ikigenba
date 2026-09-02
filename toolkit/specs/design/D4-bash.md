# D4-bash

`Bash` runs a shell command and returns what it printed. It is the one toolkit
tool that is **not confined** to its root: the root is only the directory every
command starts in, and the tool's description tells the model so. Confinement of
a shell is not something a tool can promise — a command can `cd` anywhere — and
pretending otherwise would mislead both the consumer and the model.

```go
type bashInput struct {
	Command string `json:"command"           jsonschema:"required,minLength=1,description=..."`
	Timeout *int   `json:"timeout,omitempty" jsonschema:"minimum=1,maximum=600000,description=..."`
}
```

The harness tool's `description` parameter (a human-readable label for a
permission prompt) and `run_in_background` are absent: the first is display data
the tool would only discard, and the second is harness state. Both belong to a
consumer that wants them, not to the tool.

Each call runs `bash -c <command>` from the root with the process's environment.
The command's stdout and stderr share one pipe, so the result reads like a
terminal session, interleaved in the order written. A **nonzero exit is a normal
result**, not a `Call` error — the output is what the model needs to diagnose the
failure, and the exit status is appended as a marker line. Only a timeout or a
cancelled context is an error, and even then the text carries whatever the
command printed before it was killed.

```
go: cannot find main module
[exit code 1]
```

The command runs in its **own process group**, and a timeout or cancellation
kills the whole group. A tool that killed only the shell would leave a `sleep`
or a server the command backgrounded running after the turn ended. `timeout` is
in milliseconds like the harness tool, defaulting to 120,000 and capped by the
schema at 600,000. Nothing persists between calls: a `cd` in one command does
not move the next. The constructor checks that `bash` is on `PATH` so a missing
shell is a construction-time error, not a per-call surprise.

## REQUIREMENTS

- R-E1LB-JW4F: The `Bash` tool's schema MUST declare exactly the properties `command` (string, required, `minLength` 1) and `timeout` (integer, `minimum` 1, `maximum` 600000, optional).
- R-E2T7-XNV4: `Bash` MUST return an error at construction when no `bash` executable is found on `PATH`.
- R-E414-BFLT: `Bash` MUST run `command` as `bash -c <command>` with the root as the working directory and the process's environment, and every call MUST start in the root regardless of any directory change made by an earlier call.
- R-E590-P7CI: `Bash` MUST capture the command's stdout and stderr through one shared pipe so the result interleaves them in the order written.
- R-E6GX-2Z37: When the command exits with status 0, `Bash` MUST return the captured output unchanged and a nil error.
- R-E7OT-GQTW: When the command exits with a nonzero status N, `Bash` MUST return the captured output followed by a newline and the line `[exit code N]`, with a nil error.
- R-E8WP-UIKL: `Bash` MUST treat an absent `timeout` as 120000 milliseconds, and when the command runs longer than `timeout` milliseconds it MUST return an error whose text begins with `command timed out after N ms` (N the effective timeout) and contains the output captured before the kill.
- R-EBCI-M21Z: When `ctx` is cancelled while the command runs, `Bash` MUST return an error that wraps `ctx.Err()`.
- R-ECKE-ZTSO: `Bash` MUST run the command in its own process group, and on timeout or cancellation MUST kill every process in that group so no descendant of the command survives the call.
