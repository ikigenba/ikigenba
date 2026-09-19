package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type d05CodedError struct {
	code   int
	detail string
}

func (err *d05CodedError) Error() string  { return "coded failure" }
func (err *d05CodedError) ExitCode() int  { return err.code }
func (err *d05CodedError) Detail() string { return err.detail }

func TestOperationErrorReportsUnknownErrorDirectly(t *testing.T) {
	// R-0D99-FN33
	root := t.TempDir()
	writeD05CLIRootFile(t, root)
	err := fmt.Errorf("outer: %w", errors.New("specific failure"))
	deps := d05CLIDeps(root, func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{}, err
	})
	assertResult(t, invokeWithDeps(deps, "secrets", "list", "sbx1"), 1, "", "devctl: outer: specific failure\n")
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
