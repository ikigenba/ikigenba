---
description: setup or invocation helpers that assert beyond their stated job, making a test's real claims invisible at the call site
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag helpers that both drive the code under test and quietly assert on its behavior, so that every caller makes claims its body never states. A helper named for running a command that also requires the output to be non-empty, newline-terminated, and free of blank lines turns an unrelated test — one about whether a collaborator was consulted, say — into an output-format test as well. The consequences are that a reader can no longer tell from a test body what that test guarantees, that a formatting change fails a dozen tests with no obvious connection to formatting, and that removing the hidden assertion silently weakens every caller at once.

The signal is a helper whose name promises setup or invocation (`run…`, `setup…`, `make…`, `build…`) while its body contains failure calls about the result's shape or value. Whether it marks itself as a helper for line-number attribution is a separate concern; correct attribution does not make an invisible assertion visible.

Do not flag helpers whose *purpose* is assertion and whose name says so (`assertValidID`, `requireSuccess`, `mustDecode`) — those are the recommended alternative, and callers opt into them explicitly. Do not flag a helper that fails on an error from its own machinery: a fixture that cannot build, a temp file that cannot be created, an unmarshal of test data that does not parse. That is precondition checking, not a claim about the behavior under test. Do not flag a helper that returns values and errors for the caller to assert on. The fix is to split invocation from assertion, letting each test call the named assertion helpers it actually intends.

```go
// Flagged: every caller silently also asserts output formatting, so a test
// named for sleep behavior fails when line termination changes.
func runMint(t *testing.T, args []string, clock Clock) ([]string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, bytes.NewReader(nil), &stdout, &stderr, clock)
	out := stdout.String()
	if out == "" || !strings.HasSuffix(out, "\n") {
		t.Fatalf("stdout = %q, want newline-terminated id lines", out)
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n"), stderr.String(), code
}
```

```go
// Spared: invocation returns everything; assertion is opt-in and named.
func runMint(args []string, clock Clock) (stdout, stderr string, code int) {
	var out, errBuf bytes.Buffer
	code = Run(args, bytes.NewReader(nil), &out, &errBuf, clock)
	return out.String(), errBuf.String(), code
}

func assertIDLines(t *testing.T, stdout string, want int) []string {
	t.Helper()
	// ...
}

// Spared: precondition on the helper's own fixture machinery.
func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return data
}
```
