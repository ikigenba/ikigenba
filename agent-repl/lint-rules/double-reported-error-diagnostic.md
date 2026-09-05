---
description: an error path re-emits a diagnostic the library it called has already printed, duplicating the output
severity: error
---
Flag error paths that print, log, or render a diagnostic which the API they just called has already emitted on the same stream. The classic instance is Go's `flag` package under `ContinueOnError`: a parse failure already writes the message *and* invokes the flag set's `Usage` before returning the error, so a caller that responds by calling `Usage()` again prints the whole usage block twice. The same shape appears wherever a library both reports and returns — a parser that logs before returning, a driver that prints to stderr on its own, a helper documented as "prints the reason". The giveaway in the code is an error that is bound and then dropped without inspection while the handler produces output from nothing but the fact that an error occurred: the author could not have known what the library had already said. Fix by suppressing one of the two — usually by not re-printing, or by silencing the library's output — and by actually using the returned error to decide the wording.

Do not flag a handler that adds genuinely new information (a suggestion, a config path, a remediation hint) alongside output the library produced. Do not flag re-printing to a *different* destination, such as echoing to a log what a library wrote to stderr. Do not flag when the library's output is demonstrably suppressed — `SetOutput(io.Discard)`, a quiet mode, a custom error handler installed before the call. Do not flag paired print-and-return in the same package where only one of the two ever reaches a user.

```go
// Flagged: flag.ContinueOnError already printed the message and the usage block; this prints it again.
if err := flags.Parse(args); err != nil {
	flags.Usage()
	return exitUsage
}
```

```go
// Spared: the library's own reporting is the only reporting, and the error decides the code.
if err := flags.Parse(args); err != nil {
	if errors.Is(err, flag.ErrHelp) {
		return exitSuccess
	}
	return exitUsage
}
```
