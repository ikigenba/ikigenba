package spacecreate

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func TestRunSurfaceAndValidInvocationReachesPreflight(t *testing.T) {
	// R-XOJ7-MRN8
	type runSignature func(context.Context, []string, io.Writer, seam.Deps, string) error
	var run runSignature = Run
	checkoutErr := errors.New("checkout unavailable")
	execCalls := 0
	cloudCalls := 0
	deps := seam.Deps{
		Dir: t.TempDir(),
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			execCalls++
			return seam.Result{}, checkoutErr
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, nil
		},
	}
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"foo.example.com", "--acme-email", "ops@example.com"}, &stdout, deps, "sandbox")
	if !errors.Is(err, checkoutErr) {
		t.Fatalf("Run error = %v, want checkout error", err)
	}
	if execCalls != 1 || cloudCalls != 0 || stdout.Len() != 0 {
		t.Fatalf("Run effects = exec %d, cloud %d, stdout %q", execCalls, cloudCalls, stdout.String())
	}
}

func TestRunRefusesMissingAndExtraDomainBeforeDependencies(t *testing.T) {
	// R-V1FO-SE25
	// R-Y0Q7-GH26
	// R-3M5T-DZ38
	tests := []struct {
		name       string
		args       []string
		want       string
		wantStderr string
	}{
		{
			name: "missing", want: "space create needs <domain>",
			wantStderr: "devctl: space create needs <domain>\n\nsee 'devctl space --help' for usage\n",
		},
		{
			name: "extra", args: []string{"one.example", "two.example", "--acme-email=ops@example.com"},
			want:       "space create takes only <domain>",
			wantStderr: "devctl: space create takes only <domain>\n\nsee 'devctl space --help' for usage\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := assertCreateUsageBeforeDependencies(t, test.args, test.want)
			gotStderr := "devctl: " + err.Error() + "\n\n" + err.Detail() + "\n"
			if gotStderr != test.wantStderr {
				t.Fatalf("rendered stderr = %q, want %q", gotStderr, test.wantStderr)
			}
		})
	}
}

func TestRunHelpIsExactAndDependencyFree(t *testing.T) {
	// R-3OLM-5IKM
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			calls := 0
			deps := dependencyTrap(&calls)
			var stdout bytes.Buffer
			if err := Run(context.Background(), []string{option}, &stdout, deps, ""); err != nil {
				t.Fatalf("Run help: %v", err)
			}
			if stdout.String() != usageText {
				t.Fatalf("stdout = %q, want %q", stdout.String(), usageText)
			}
			if calls != 0 {
				t.Fatalf("dependency calls = %d, want 0", calls)
			}
		})
	}
}

func TestParseCreateAcceptsAcmeEmailFormsAndLastOccurrence(t *testing.T) {
	// R-E511-ZSGV
	tests := []struct {
		args []string
		want invocation
	}{
		{args: []string{"--acme-email", "first@example.com", "foo.example"}, want: invocation{domain: "foo.example", acmeEmail: "first@example.com"}},
		{args: []string{"foo.example", "--acme-email=second@example.com"}, want: invocation{domain: "foo.example", acmeEmail: "second@example.com"}},
		{args: []string{"--acme-email=first@example.com", "foo.example", "--acme-email", "last@example.com"}, want: invocation{domain: "foo.example", acmeEmail: "last@example.com"}},
	}
	for _, test := range tests {
		got, err := parseInvocation(test.args)
		if err != nil {
			t.Fatalf("parseInvocation(%q): %v", test.args, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("parseInvocation(%q) = %#v, want %#v", test.args, got, test.want)
		}
	}
	_ = assertCreateUsageBeforeDependencies(t, []string{"foo.example", "--elastic-ip", "--acme-email=x@y"}, "unknown option '--elastic-ip'")
}

func TestRunUnknownOptionsAndConsumedOptionValue(t *testing.T) {
	// R-E7GU-RBY9
	for _, option := range []string{"--verbose", "-v", "--elastic-ip"} {
		_ = assertCreateUsageBeforeDependencies(t, []string{"foo.example", option, "--acme-email=x@y"}, "unknown option '"+option+"'")
	}

	checkoutErr := errors.New("reached preflight")
	deps := seam.Deps{
		Dir:  t.TempDir(),
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) { return seam.Result{}, checkoutErr },
	}
	err := Run(context.Background(), []string{"foo.example", "--acme-email", "--not-an-option"}, io.Discard, deps, "sandbox")
	if !errors.Is(err, checkoutErr) {
		t.Fatalf("consumed option-like value error = %v, want preflight error", err)
	}
}

func TestRunAcmeEmailFailuresFollowArityAndPrecedeDependencies(t *testing.T) {
	// R-E8OR-53OY
	_ = assertCreateUsageBeforeDependencies(t, []string{"foo.example"}, "space create needs --acme-email <address>")
	_ = assertCreateUsageBeforeDependencies(t, []string{"one.example", "two.example"}, "space create takes only <domain>")
	_ = assertCreateUsageBeforeDependencies(t, []string{"foo.example", "--acme-email"}, "option '--acme-email' requires a value")
	_ = assertCreateUsageBeforeDependencies(t, []string{"foo.example", "--acme-email="}, "option '--acme-email' requires a value")
}

func assertCreateUsageBeforeDependencies(t *testing.T, args []string, message string) *space.UsageError {
	t.Helper()
	calls := 0
	var stdout bytes.Buffer
	err := Run(context.Background(), args, &stdout, dependencyTrap(&calls), "sandbox")
	var usageErr *space.UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("Run error = %T %v, want *space.UsageError", err, err)
	}
	if usageErr.Message != message || usageErr.Help != helpCommand {
		t.Fatalf("usage error = %#v, want message %q and help %q", usageErr, message, helpCommand)
	}
	if usageErr.ExitCode() != 2 || stdout.Len() != 0 || calls != 0 {
		t.Fatalf("effects = exit %d, stdout %q, dependency calls %d", usageErr.ExitCode(), stdout.String(), calls)
	}
	return usageErr
}

func dependencyTrap(calls *int) seam.Deps {
	return seam.Deps{
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			*calls++
			return seam.Result{}, errors.New("unexpected exec")
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			*calls++
			return cloud.Clients{}, errors.New("unexpected cloud")
		},
	}
}
