---
description: tests mutating process environment that the code under test reads only through a once-initialized global, making the assertion vacuous
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag tests that set an environment variable in-process and then assert on behavior that the environment variable cannot actually influence, because the runtime resolved it once at first use and cached the result for the life of the process. The canonical case is setting `TZ` inside a Go test and asserting on timezone-dependent output: `time.Local` is initialized lazily behind a `sync.Once`, so any earlier use of local time anywhere in the test binary — including in another test file — freezes the zone, and the mutation is a no-op. The test then passes whether or not the code respects the setting, and specifically still passes under the regression it was written to catch, because the host's ambient value usually matches the expected one. Equivalents exist in other runtimes: locale and encoding globals, cached process-wide config singletons, and connection pools built at import time.

Judge this by asking whether the code under test reads the variable *on every call* or through a value captured once. Do not flag env mutation that the code genuinely re-reads per call, nor mutation whose effect is asserted by a subprocess launched with that environment. Do not flag env mutation used only to steer test infrastructure — pointing a client at a fake server, disabling color, selecting a fixture directory — where the value is read at the moment the test uses it. Do not flag it merely because the assertion currently passes; flag it when a plausible regression in the code under test would leave it passing. The fix is either to run the behavior in a subprocess with the variable set in the child's environment, or to inject the dependency (pass the location, clock, or locale explicitly) rather than reaching through a process global.

```go
// Flagged: time.Local is resolved once per process, so this Setenv changes
// nothing. If the formatter regressed from .UTC() to .Local(), the output would
// still be UTC on a UTC host and this test would still pass.
func TestOutputIgnoresTZ(t *testing.T) {
	t.Setenv("TZ", "America/Chicago")
	out, _, _ := run([]string{"--decode", id})
	if !strings.HasSuffix(out, "Z\n") {
		t.Errorf("stdout = %q, want UTC output", out)
	}
}
```

```go
// Spared: the environment is applied to a child process, where it takes effect
// at that process's own initialization.
func TestOutputIgnoresTZ(t *testing.T) {
	cmd := exec.Command(binary, "--decode", id)
	cmd.Env = append(os.Environ(), "TZ=America/Chicago")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "2026-06-07T08:09:10.765Z\n"; string(out) != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// Spared: the variable steers test infrastructure and is read where it is used.
func TestClientHitsFakeServer(t *testing.T) {
	srv := httptest.NewServer(fakeHandler())
	defer srv.Close()
	t.Setenv("SERVICE_URL", srv.URL)
	// ...
}
```
