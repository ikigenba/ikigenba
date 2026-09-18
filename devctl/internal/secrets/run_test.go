package secrets

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	wantUsageText = `Usage: devctl --account <name> secrets <subcommand> <domain> [<app>]

Push the values an app's manifest names from this machine's keyring to the
space's Parameter Store entry, or list which names a space holds. Values are
never printed.

Subcommands:
  push <domain> [<app>]   write /ikigenba/<domain>/<app> for one app, or every app
  list <domain> [<app>]   print the key names held for one app, or every app

Every subcommand needs --account. Run 'devctl secrets <subcommand> --help' for details.
`
	wantPushUsage = `Usage: devctl --account <name> secrets push <domain> [<app>]

Write the space's Parameter Store entry for an app from this machine's keyring:
a SecureString at /ikigenba/<domain>/<app> holding a JSON object whose keys are
the names the app's manifest declares. Each value comes from the environment
variable of that name, or from the login keyring. Values are never printed.

With <app> omitted, every app in the checkout is written, in name order. Every
value is gathered before anything is written, so one missing value leaves every
app's entry as it was.

Arguments:
  <domain>   the space to write the entry in
  <app>      one app of the checkout; omitted, every app
`
	wantListUsage = `Usage: devctl --account <name> secrets list <domain> [<app>]

Print the key names the space's Parameter Store entry holds for an app: the
app, then its names sorted and comma-separated, or - when the entry is empty.
Only names are printed; a value never is.

With <app> omitted, every entry under /ikigenba/<domain>/ is printed, in app
order, and a space that holds none prints nothing.

Arguments:
  <domain>   the space to read the entries of
  <app>      one app the space holds an entry for; omitted, every app
`
)

func TestRunSignature(t *testing.T) {
	// R-FP8V-1HBG
	type runSignature func(context.Context, []string, io.Writer, seam.Deps, string) error
	var run runSignature = Run
	if run == nil {
		t.Fatal("Run is nil")
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
			if err := Run(context.Background(), test.args, &stdout, seam.Deps{}, ""); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if got := stdout.String(); got != test.want {
				t.Fatalf("stdout = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSecretsSubcommandGrammar(t *testing.T) {
	// R-G2NR-8YH3
	tests := []struct {
		args []string
		want invocation
	}{
		{args: []string{"push", "example.test"}, want: invocation{subcommand: "push", domain: "example.test"}},
		{args: []string{"push", "example.test", "crm"}, want: invocation{subcommand: "push", domain: "example.test", app: "crm"}},
		{args: []string{"list", "example.test"}, want: invocation{subcommand: "list", domain: "example.test"}},
		{args: []string{"list", "example.test", "crm"}, want: invocation{subcommand: "list", domain: "example.test", app: "crm"}},
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
		{"create", "example.test"},
		{"push", "example.test", "crm", "extra"},
		{"list", "--verbose", "example.test"},
	} {
		if _, err := parseInvocation(args); err == nil {
			t.Errorf("parseInvocation(%q) returned nil error", args)
		}
	}
}

func TestSecretsNeedsSubcommand(t *testing.T) {
	assertRunUsageError(t, nil, "secrets needs <subcommand>")
}

func TestSecretsRejectsUnknownSubcommand(t *testing.T) {
	assertRunUsageError(t, []string{"frobnicate"}, "unknown subcommand 'frobnicate'")
}

func TestSecretsSubcommandsNeedDomain(t *testing.T) {
	for _, subcommand := range []string{"push", "list"} {
		assertRunUsageError(t, []string{subcommand}, "secrets "+subcommand+" needs <domain>")
	}
}

func TestSecretsSubcommandsRejectExtraOperands(t *testing.T) {
	for _, subcommand := range []string{"push", "list"} {
		assertRunUsageError(t, []string{subcommand, "example.test", "crm", "extra"},
			"secrets "+subcommand+" takes at most <domain> and <app>")
	}
}

func TestSecretsRejectsUnknownOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--verbose"},
		{"push", "--verbose"},
		{"push", "example.test", "--verbose"},
		{"list", "example.test", "crm", "--verbose"},
		{"push", "--help", "--verbose"},
	} {
		assertRunUsageError(t, args, "unknown option '--verbose'")
	}
}

type resolutionSSM struct {
	cloud.SSM
	value      string
	parameters []cloud.Parameter
	writes     int
}

func (ssm *resolutionSSM) GetParameter(context.Context, string) (string, error) {
	return ssm.value, nil
}

func (ssm *resolutionSSM) PutSecureParameter(context.Context, string, string) error {
	ssm.writes++
	return nil
}

func (ssm *resolutionSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return ssm.parameters, nil
}

type resolutionEC2 struct {
	cloud.EC2
	err   error
	calls int
}

func (ec2 *resolutionEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	ec2.calls++
	return nil, ec2.err
}

func TestPushResolvesSpaceBeforeReadingOrWritingSecrets(t *testing.T) {
	// R-D209-R4QN
	root := t.TempDir()
	appDir := filepath.Join(root, "crm")
	if err := os.MkdirAll(filepath.Join(appDir, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "etc", "manifest.toml"), []byte("app = 'crm'\nsecrets = ['CRM_TOKEN']\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	spaceErr := errors.New("space lookup failed")
	bootstrap := &resolutionSSM{value: testAccountProperties}
	regional := &resolutionSSM{}
	cloudCalls := 0
	lookups := 0
	deps := seam.Deps{
		Dir: root,
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
		Getenv: func(string) string {
			lookups++
			return "must-not-be-read"
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			if cloudCalls == 1 {
				return cloud.Clients{SSM: bootstrap}, nil
			}
			return cloud.Clients{SSM: regional, EC2: &resolutionEC2{err: spaceErr}}, nil
		},
	}

	err := Run(context.Background(), []string{"push", "foo.sbx.ikigenba.dev", "crm"}, io.Discard, deps, "work")
	if err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(spaceErr).Pointer() {
		t.Fatalf("Run error = %T %v, want original space lookup error", err, err)
	}
	if lookups != 0 {
		t.Fatalf("keyring lookups = %d, want 0 before space resolution", lookups)
	}
	if regional.writes != 0 {
		t.Fatalf("parameter writes = %d, want 0 before space resolution", regional.writes)
	}
}

func TestListReadsRetainedParametersWithoutResolvingAnInstance(t *testing.T) {
	// R-D209-R4QN
	const domain = "destroyed.sbx.ikigenba.dev"
	bootstrap := &resolutionSSM{value: testAccountProperties}
	regional := &resolutionSSM{parameters: []cloud.Parameter{{
		Name:  Parameter(domain, "crm"),
		Value: `{"CRM_TOKEN":"retained-secret"}`,
	}}}
	ec2 := &resolutionEC2{err: errors.New("instance lookup must not occur")}
	cloudCalls := 0
	deps := seam.Deps{Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		cloudCalls++
		if cloudCalls == 1 {
			return cloud.Clients{SSM: bootstrap}, nil
		}
		return cloud.Clients{SSM: regional, EC2: ec2}, nil
	}}

	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"list", domain}, &stdout, deps, "work"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := stdout.String(), "crm CRM_TOKEN\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if ec2.calls != 0 {
		t.Fatalf("instance lookups = %d, want 0", ec2.calls)
	}
}

const testAccountProperties = `{
  "domain":"example.test",
  "backup_bucket":"backups",
  "launch_template_id":"lt-1",
  "permissions_boundary_arn":"arn:test",
  "region":"us-test-1",
  "delete_secrets_on_destroy":true,
  "delete_backups_on_destroy":false,
  "backup_host_files_seconds":1,
  "backup_service_files_seconds":2,
  "backup_service_db_seconds":3,
  "backup_service_wal_seconds":4
}`

func assertRunUsageError(t *testing.T, args []string, message string) {
	t.Helper()
	var stdout bytes.Buffer
	err := Run(context.Background(), args, &stdout, seam.Deps{}, "profile")
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
