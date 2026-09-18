package cli

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

type d05CodedError struct {
	code   int
	detail string
}

func (err *d05CodedError) Error() string  { return "coded failure" }
func (err *d05CodedError) ExitCode() int  { return err.code }
func (err *d05CodedError) Detail() string { return err.detail }

func TestOperationErrorReportsUnknownErrorDirectly(t *testing.T) {
	// R-CAGK-A9JL
	var stderr bytes.Buffer
	err := fmt.Errorf("outer: %w", errors.New("specific failure"))
	if got := operationError(&stderr, err); got != 1 {
		t.Fatalf("operationError exit code = %d, want 1", got)
	}
	if got, want := stderr.String(), "devctl: outer: specific failure\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestOperationErrorUsesCodedErrorAndDetail(t *testing.T) {
	// R-D4G2-IO81
	for _, test := range []struct {
		name   string
		code   int
		detail string
		want   string
	}{
		{name: "operation", code: 1, want: "devctl: wrapped: coded failure\n"},
		{name: "usage detail", code: 2, detail: "see 'devctl secrets --help' for usage", want: "devctl: wrapped: coded failure\n\nsee 'devctl secrets --help' for usage\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			err := fmt.Errorf("wrapped: %w", &d05CodedError{code: test.code, detail: test.detail})
			if got := operationError(&stderr, err); got != test.code {
				t.Fatalf("operationError exit code = %d, want %d", got, test.code)
			}
			if got := stderr.String(); got != test.want {
				t.Fatalf("stderr = %q, want %q", got, test.want)
			}
		})
	}
}
