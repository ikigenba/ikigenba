package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

const usageText = `usage: dory [-c key=value ...] [-resume UUID] [-V] [-h] < prompt

Role configuration (-c <role>.<key>=<value>):
  supervisor.provider, supervisor.model, supervisor.wire
  supervisor.auth, supervisor.auth_file, supervisor.base_url, supervisor.max_context
  worker.provider, worker.model, worker.wire
  worker.auth, worker.auth_file, worker.base_url, worker.max_context
Other role-scoped names are passed through as agent settings.
`

// R-HCLS-94F3
func TestDepsHasExactShape(t *testing.T) {
	type expectedField struct {
		name   string
		typeOf reflect.Type
	}
	expected := []expectedField{
		{name: "Home", typeOf: reflect.TypeOf("")},
		{name: "Getenv", typeOf: reflect.TypeOf((func(string) string)(nil))},
		{name: "Now", typeOf: reflect.TypeOf((func() time.Time)(nil))},
		{name: "SessionID", typeOf: reflect.TypeOf("")},
		{name: "Root", typeOf: reflect.TypeOf("")},
		{name: "Interrupts", typeOf: reflect.TypeOf((<-chan struct{})(nil))},
	}

	actual := reflect.TypeOf(Deps{})
	if actual.NumField() != len(expected) {
		t.Fatalf("Deps has %d fields, want exactly %d", actual.NumField(), len(expected))
	}
	for index, want := range expected {
		field := actual.Field(index)
		if field.Name != want.name || field.Type != want.typeOf {
			t.Errorf("Deps field %d = %s %v, want %s %v", index, field.Name, field.Type, want.name, want.typeOf)
		}
	}
}

// R-VU1F-SXFM
func TestRunReturnsExitCodeInProcess(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"-h"}, failOnRead{t}, &stdout, &stderr, Deps{})

	if code != int(exitSuccess) {
		t.Errorf("Run exit code = %d, want %d", code, exitSuccess)
	}
	if stdout.String() != usageText {
		t.Errorf("stdout = %q, want exact usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// R-HG9H-EFN6
// R-HSGH-8524
func TestHelpAndVersionAreImmediateSuccesses(t *testing.T) {
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
		t.Fatalf("version = %q, want semantic version", version)
	}

	tests := []struct {
		name       string
		argument   string
		wantStdout string
	}{
		{name: "short help", argument: "-h", wantStdout: usageText},
		{name: "long help", argument: "--help", wantStdout: usageText},
		{name: "short version", argument: "-V", wantStdout: "v0.1.0\n"},
		{name: "long version", argument: "--version", wantStdout: "v0.1.0\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir() + "/absent-home"
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := Run(
				context.Background(),
				[]string{test.argument},
				failOnRead{t},
				&stdout,
				&stderr,
				Deps{Home: home},
			)

			if code != int(exitSuccess) {
				t.Errorf("Run exit code = %d, want %d", code, exitSuccess)
			}
			if stdout.String() != test.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), test.wantStdout)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			assertPathAbsent(t, home)
		})
	}
}

// R-HR8K-UDBF
func TestUnknownFlagIsOneUsageError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"--unknown"}, failOnRead{t}, &stdout, &stderr, Deps{})

	if code != int(exitUsage) {
		t.Errorf("Run exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), usageText) {
		t.Errorf("stderr does not contain usage: %q", stderr.String())
	}
	if count := strings.Count(stdout.String()+stderr.String(), "unknown"); count != 1 {
		t.Errorf("offending flag occurs %d times across output, want exactly once: stdout=%q stderr=%q", count, stdout.String(), stderr.String())
	}
}

func TestOtherPrePromptUsageErrorsDoNotReadStdin(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		expectedDiagnostic string
	}{
		{
			name:               "malformed config",
			args:               []string{"-c", "malformed"},
			expectedDiagnostic: `invalid value "malformed" for flag -c: malformed configuration "malformed"`,
		},
		{
			name:               "validation",
			args:               []string{"-c", "bogus.key=value"},
			expectedDiagnostic: "bogus.key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := Run(context.Background(), test.args, failOnRead{t}, &stdout, &stderr, Deps{})

			if code != int(exitUsage) {
				t.Errorf("Run exit code = %d, want %d", code, exitUsage)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), usageText) {
				t.Errorf("stderr does not contain usage: %q", stderr.String())
			}
			if count := strings.Count(stdout.String()+stderr.String(), test.expectedDiagnostic); count != 1 {
				t.Errorf("diagnostic %q occurs %d times, want exactly once: stdout=%q stderr=%q", test.expectedDiagnostic, count, stdout.String(), stderr.String())
			}
		})
	}
}

// R-HTOD-LWST
func TestPromptIsReadAfterValidationAndShapedBeforeExecution(t *testing.T) {
	t.Run("reads through EOF and retains preflight values", func(t *testing.T) {
		reader := &readAllSpy{input: []byte(" \tleading\ninternal\r\nend \t\r\n\u2003")}

		result, failure := preflight(nil, reader)

		if failure != nil {
			t.Fatalf("preflight failure = %v", failure.err)
		}
		if reader.eofReads != 1 {
			t.Errorf("stdin EOF reads = %d, want exactly 1", reader.eofReads)
		}
		if reader.remaining() != 0 {
			t.Errorf("stdin has %d unread bytes, want none", reader.remaining())
		}
		const wantPrompt = " \tleading\ninternal\r\nend"
		if result.prompt != wantPrompt {
			t.Errorf("trimmed prompt = %q, want %q", result.prompt, wantPrompt)
		}
		if result.action != preflightExecute {
			t.Errorf("preflight action = %d, want execute", result.action)
		}
		if result.options.Supervisor.Model == "" || result.options.Worker.Model == "" {
			t.Errorf("validated options were not retained: %+v", result.options)
		}
	})

	t.Run("validation precedes stdin", func(t *testing.T) {
		_, failure := preflight([]string{"-c", "invalid=value"}, failOnRead{t})
		if failure == nil || !failure.usage {
			t.Fatalf("preflight failure = %#v, want usage failure", failure)
		}
	})

	t.Run("empty prompt is usage error without filesystem access", func(t *testing.T) {
		home := t.TempDir() + "/absent-home"
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		code := Run(
			context.Background(),
			nil,
			strings.NewReader(" \t\r\n\u2003"),
			&stdout,
			&stderr,
			Deps{Home: home},
		)

		if code != int(exitUsage) {
			t.Errorf("Run exit code = %d, want %d", code, exitUsage)
		}
		if stdout.Len() != 0 {
			t.Errorf("stdout = %q, want empty", stdout.String())
		}
		if stderr.Len() == 0 || !strings.Contains(stderr.String(), "prompt is empty") {
			t.Errorf("stderr = %q, want nonempty prompt diagnostic", stderr.String())
		}
		assertPathAbsent(t, home)
	})
}

func TestPromptReadFailureIsOperational(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), nil, errorReader{}, &stdout, &stderr, Deps{})

	if code != int(exitFailure) {
		t.Errorf("Run exit code = %d, want %d", code, exitFailure)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "read prompt") || strings.Contains(stderr.String(), usageText) {
		t.Errorf("stderr = %q, want operational read error without usage", stderr.String())
	}
}

type failOnRead struct {
	t *testing.T
}

func (reader failOnRead) Read([]byte) (int, error) {
	reader.t.Helper()
	reader.t.Fatal("stdin was read")
	return 0, errors.New("unreachable")
}

type readAllSpy struct {
	input    []byte
	offset   int
	eofReads int
}

func (reader *readAllSpy) Read(destination []byte) (int, error) {
	if reader.offset == len(reader.input) {
		reader.eofReads++
		return 0, io.EOF
	}

	end := min(reader.offset+3, len(reader.input))
	copied := copy(destination, reader.input[reader.offset:end])
	reader.offset += copied
	return copied, nil
}

func (reader *readAllSpy) remaining() int {
	return len(reader.input) - reader.offset
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("injected read failure")
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Stat(%q) error = %v, want path absent", path, err)
	}
}
