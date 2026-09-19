package secrets

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	wantUsageText = `Usage: devctl secrets <subcommand> <space> [<app>]

Push the values an app's manifest names from this machine's keyring to the
space's Parameter Store entry, or list which names a space holds. Values are
never printed.

Subcommands:
  push <space> [<app>]   write /<space domain>/<app> for one app, or every app
  list <space> [<app>]   print the key names held for one app, or every app

Run 'devctl secrets <subcommand> --help' for details.
`
	wantPushUsage = `Usage: devctl secrets push <space> [<app>]

Write the space's Parameter Store entry for an app from this machine's keyring:
a SecureString at /<space domain>/<app> holding a JSON object whose keys are
the names the app's manifest declares. Each value comes from the environment
variable of that name, or from the login keyring. Values are never printed.

With <app> omitted, every app in the checkout is written, in name order. Every
value is gathered before anything is written, so one missing value leaves every
app's entry as it was.

Arguments:
  <space>    the space to write the entry in, by label or full domain
  <app>      one app of the checkout; omitted, every app
`
	wantListUsage = `Usage: devctl secrets list <space> [<app>]

Print the key names the space's Parameter Store entry holds for an app: the
app, then its names sorted and comma-separated, or - when the entry is empty.
Only names are printed; a value never is.

With <app> omitted, every entry under /<space domain>/ is printed, in app
order, and a space that holds none prints nothing.

Arguments:
  <space>    the space to read the entries of, by label or full domain
  <app>      one app the space holds an entry for; omitted, every app
`
)

func TestRunSignature(t *testing.T) {
	// R-7GAI-G94Y
	type runSignature func(context.Context, []string, io.Writer, seam.Deps) error
	var run runSignature = Run
	if run == nil {
		t.Fatal("Run is nil")
	}
}

func TestOperationSignatures(t *testing.T) {
	// R-08DN-WK4B
	type pushSignature func(context.Context, seam.Deps, cloud.SSM, string, []checkout.App) ([]Entry, error)
	type listSignature func(context.Context, cloud.SSM, string) ([]Entry, error)
	type namesSignature func(context.Context, cloud.SSM, string, string) ([]string, error)

	var push pushSignature = Push
	var list listSignature = List
	var names namesSignature = Names
	if push == nil || list == nil || names == nil {
		t.Fatal("exported secrets operation is nil")
	}
}

func TestUsageErrorHasExactFieldsAndBehavior(t *testing.T) {
	// R-FVCC-YC0X
	want := []reflect.StructField{
		{Name: "Message", Type: reflect.TypeFor[string]()},
		{Name: "Help", Type: reflect.TypeFor[string]()},
	}
	assertExactFields(t, reflect.TypeFor[UsageError](), want)

	err := &UsageError{Message: "bad option", Help: "devctl secrets --help"}
	if got := err.Error(); got != "bad option" {
		t.Errorf("Error() = %q", got)
	}
	if got := err.Detail(); got != "see 'devctl secrets --help' for usage" {
		t.Errorf("Detail() = %q", got)
	}
	if got := err.ExitCode(); got != 2 {
		t.Errorf("ExitCode() = %d, want 2", got)
	}
}

func TestRunHelp(t *testing.T) {
	// R-0EH5-TETS
	// R-0FP2-76KH
	// R-0GWY-KYB6
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "long top level", args: []string{"--help"}, want: wantUsageText},
		{name: "short top level", args: []string{"-h"}, want: wantUsageText},
		{name: "long push", args: []string{"push", "--help"}, want: wantPushUsage},
		{name: "short push", args: []string{"push", "-h"}, want: wantPushUsage},
		{name: "long list", args: []string{"list", "--help"}, want: wantListUsage},
		{name: "short list", args: []string{"list", "-h"}, want: wantListUsage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := Run(context.Background(), test.args, &stdout, seam.Deps{}); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if got := stdout.String(); got != test.want {
				t.Fatalf("stdout = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSecretsSubcommandGrammar(t *testing.T) {
	// R-0I4U-YQ1V
	tests := []struct {
		args []string
		want invocation
	}{
		{args: []string{"push", "sbx1"}, want: invocation{subcommand: "push", space: "sbx1"}},
		{args: []string{"push", "sbx1", "crm"}, want: invocation{subcommand: "push", space: "sbx1", app: "crm"}},
		{args: []string{"list", "sbx1"}, want: invocation{subcommand: "list", space: "sbx1"}},
		{args: []string{"list", "sbx1", "crm"}, want: invocation{subcommand: "list", space: "sbx1", app: "crm"}},
	}
	for _, test := range tests {
		got, err := parseInvocation(test.args)
		if err != nil {
			t.Errorf("parseInvocation(%q) returned error: %v", test.args, err)
		} else if got != test.want {
			t.Errorf("parseInvocation(%q) = %#v, want %#v", test.args, got, test.want)
		}
	}
	for _, args := range [][]string{
		{"create", "sbx1"},
		{"push", "sbx1", "crm", "extra"},
		{"list", "--verbose", "sbx1"},
	} {
		if _, err := parseInvocation(args); err == nil {
			t.Errorf("parseInvocation(%q) returned nil error", args)
		}
	}
}

func TestSecretsUsageFailures(t *testing.T) {
	// R-0JCR-CHSK
	assertRunUsageError(t, nil, "secrets needs <subcommand>")
	assertRunUsageError(t, []string{"frobnicate"}, "unknown subcommand 'frobnicate'")

	// R-0KKN-Q9J9
	for _, subcommand := range []string{"push", "list"} {
		assertRunUsageError(t, []string{subcommand}, "secrets "+subcommand+" needs <space>")
	}

	// R-0LSK-419Y
	for _, subcommand := range []string{"push", "list"} {
		assertRunUsageError(t, []string{subcommand, "sbx1", "crm", "extra"},
			"secrets "+subcommand+" takes at most <space> and <app>")
	}
}

func TestSecretsRejectsUnknownOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--verbose"},
		{"push", "--verbose"},
		{"push", "sbx1", "--verbose"},
		{"list", "sbx1", "crm", "--verbose"},
		{"push", "--help", "--verbose"},
	} {
		assertRunUsageError(t, args, "unknown option '--verbose'")
	}
}

func assertRunUsageError(t *testing.T, args []string, message string) {
	t.Helper()
	var stdout bytes.Buffer
	err := Run(context.Background(), args, &stdout, seam.Deps{})
	if stdout.Len() != 0 {
		t.Fatalf("Run(%q) stdout = %q, want empty", args, stdout.String())
	}
	var usageError *UsageError
	if !errors.As(err, &usageError) {
		t.Fatalf("Run(%q) error = %T %v, want *UsageError", args, err, err)
	}
	if usageError.Message != message || usageError.Help != helpCommand {
		t.Fatalf("Run(%q) error = %#v, want message %q and help %q", args, usageError, message, helpCommand)
	}
	if usageError.Error() != message || usageError.Detail() != "see 'devctl secrets --help' for usage" || usageError.ExitCode() != 2 {
		t.Fatalf("Run(%q) error methods returned unexpected values", args)
	}
}
