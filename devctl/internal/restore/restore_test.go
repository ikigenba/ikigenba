package restore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

var _ func(context.Context, []string, io.Writer, seam.Deps, string) error = Run

const accountProperties = `{
  "domain":"sbx.ikigenba.dev",
  "backup_bucket":"backups",
  "launch_template_id":"lt-1",
  "permissions_boundary_arn":"arn:boundary",
  "region":"us-east-2",
  "delete_secrets_on_destroy":false,
  "delete_backups_on_destroy":false,
  "backup_host_files_seconds":1,
  "backup_service_files_seconds":2,
  "backup_service_db_seconds":3,
  "backup_service_wal_seconds":4
}`

const expectedRestoreUsage = `Usage: devctl --account <name> restore <domain> <app> [--at <timestamp>]

Have opsctl on <domain> put <app> back from <domain>'s own backups. The app's
etc/ and state/ come from the newest tarball, and its database, when it
declares one, from litestream. <app>'s unit is stopped for the restore and
started again after it.

Options:
  --at <timestamp>   restore the app as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself.
`

func TestRestorePublicContract(t *testing.T) {
	// R-FLGQ-FXCQ R-FMOM-TP3F
	err := &UsageError{Message: "bad invocation", Help: "devctl restore --help"}
	if err.Error() != "bad invocation" || err.ExitCode() != 2 || err.Detail() != "see 'devctl restore --help' for usage" {
		t.Fatalf("UsageError methods = (%q, %d, %q)", err.Error(), err.ExitCode(), err.Detail())
	}
	if got := (&UsageError{Message: "bad timestamp"}).Detail(); got != "" {
		t.Fatalf("empty-help Detail = %q, want empty", got)
	}
	value := reflect.TypeOf(UsageError{})
	wantType := reflect.TypeFor[string]()
	if value.NumField() != 2 ||
		value.Field(0).Name != "Message" || value.Field(0).Type != wantType ||
		value.Field(1).Name != "Help" || value.Field(1).Type != wantType {
		t.Fatalf("UsageError fields = %v, want exactly Message string and Help string", value)
	}
}

func TestRestoreHelpPrecedesExternalAccess(t *testing.T) {
	// R-FHIL-ORT8
	for _, args := range [][]string{
		{"--help"},
		{"-h"},
		{"foo.sbx.ikigenba.dev", "crm", "--from", "elsewhere", "--help"},
	} {
		access := 0
		deps := seam.Deps{
			Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				access++
				return cloud.Clients{}, nil
			},
			Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				access++
				return seam.Result{}, nil
			},
		}
		var stdout bytes.Buffer
		if err := Run(context.Background(), args, &stdout, deps, "work"); err != nil {
			t.Fatalf("Run(%q): %v", args, err)
		}
		if stdout.String() != expectedRestoreUsage {
			t.Errorf("Run(%q) stdout = %q, want %q", args, stdout.String(), expectedRestoreUsage)
		}
		if access != 0 {
			t.Errorf("Run(%q) external accesses = %d, want 0", args, access)
		}
	}
}

func TestRestoreArgumentGrammar(t *testing.T) {
	// R-FP4F-L8KT R-FQCB-Z0BI
	tests := []struct {
		name    string
		args    []string
		message string
		help    string
	}{
		{name: "none", message: "restore needs <domain> and <app>", help: "devctl restore --help"},
		{name: "one", args: []string{"domain"}, message: "restore needs <domain> and <app>", help: "devctl restore --help"},
		{name: "extra", args: []string{"domain", "app", "extra"}, message: "restore takes only <domain> and <app>", help: "devctl restore --help"},
		{name: "from", args: []string{"domain", "app", "--from", "other"}, message: "unknown option '--from'", help: "devctl restore --help"},
		{name: "from-account", args: []string{"--from-account=other", "domain", "app"}, message: "unknown option '--from-account=other'", help: "devctl restore --help"},
		{name: "at missing", args: []string{"domain", "app", "--at"}, message: "option '--at' requires a value", help: "devctl restore --help"},
		{name: "at empty", args: []string{"--at=", "domain", "app"}, message: "option '--at' requires a value", help: "devctl restore --help"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access := 0
			deps := seam.Deps{
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					access++
					return cloud.Clients{}, nil
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					access++
					return seam.Result{}, nil
				},
			}
			err := Run(context.Background(), test.args, io.Discard, deps, "work")
			var usageErr *UsageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("Run(%q) error = %T %v, want *UsageError", test.args, err, err)
			}
			if usageErr.Message != test.message || usageErr.Help != test.help {
				t.Errorf("Run(%q) error = %#v, want message %q help %q", test.args, usageErr, test.message, test.help)
			}
			if access != 0 {
				t.Errorf("Run(%q) external accesses = %d, want 0", test.args, access)
			}
		})
	}
}

func TestRestoreTimestampValidationAndSpelling(t *testing.T) {
	// R-FRK8-CS27
	access := 0
	badDeps := seam.Deps{Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		access++
		return cloud.Clients{}, nil
	}}
	err := Run(context.Background(), []string{"domain", "app", "--at", "yesterday"}, io.Discard, badDeps, "work")
	var usageErr *UsageError
	if !errors.As(err, &usageErr) || usageErr.Message != "--at takes an RFC 3339 timestamp" || usageErr.Help != "" {
		t.Fatalf("invalid timestamp error = %#v, want timestamp UsageError without help", err)
	}
	if access != 0 {
		t.Fatalf("invalid timestamp external accesses = %d, want 0", access)
	}

	wantAt := "2026-09-17T18:33:54.123456789-05:00"
	for _, args := range [][]string{
		{"--at", "2000-01-01T00:00:00Z", "foo.sbx.ikigenba.dev", "app", "--at=" + wantAt},
		{"foo.sbx.ikigenba.dev", "--at=" + wantAt, "app"},
	} {
		commands := runSuccessfulRestore(t, args)
		if got := commands[0].Args[len(commands[0].Args)-1]; !strings.Contains(got, "'--at' '"+wantAt+"'") {
			t.Errorf("Run(%q) remote command = %q, want original timestamp %q", args, got, wantAt)
		}
	}
}

func TestRestoreResolvesOnlyRunningSelectedSpace(t *testing.T) {
	// R-FU01-4BJL
	tests := []struct {
		name      string
		instances []cloud.Instance
		wantType  any
	}{
		{name: "absent", wantType: (*account.NoSpaceError)(nil)},
		{name: "stopped", instances: []cloud.Instance{{ID: "i-1", Space: "foo.sbx.ikigenba.dev", State: cloud.StateStopped, Address: "192.0.2.10"}}, wantType: (*space.NotRunningError)(nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			commands := 0
			profiles := make([]string, 0, 2)
			deps := restoreDeps(test.instances, &profiles, func(context.Context, seam.Cmd) (seam.Result, error) {
				commands++
				return seam.Result{}, nil
			})
			err := Run(context.Background(), []string{"foo.sbx.ikigenba.dev", "crm"}, &stdout, deps, "Selected Profile")
			switch test.wantType.(type) {
			case *account.NoSpaceError:
				var wanted *account.NoSpaceError
				if !errors.As(err, &wanted) {
					t.Fatalf("error = %T %v, want *account.NoSpaceError", err, err)
				}
			case *space.NotRunningError:
				var wanted *space.NotRunningError
				if !errors.As(err, &wanted) {
					t.Fatalf("error = %T %v, want *space.NotRunningError", err, err)
				}
			}
			if stdout.Len() != 0 || commands != 0 {
				t.Errorf("stdout = %q, SSH commands = %d; want empty and 0", stdout.String(), commands)
			}
			if !reflect.DeepEqual(profiles, []string{"Selected Profile", "Selected Profile"}) {
				t.Errorf("cloud profiles = %q, want selected profile only", profiles)
			}
		})
	}
}

func TestRestoreInvokesOpsctlAndReportsSuccess(t *testing.T) {
	// R-FV7X-I3AA
	withoutAt := runSuccessfulRestore(t, []string{"foo.sbx.ikigenba.dev", "crm"})
	wantWithoutAt := "'sudo' 'opsctl' 'restore' 'crm'"
	if got := withoutAt[0].Args[len(withoutAt[0].Args)-1]; got != wantWithoutAt {
		t.Errorf("remote command without --at = %q, want %q", got, wantWithoutAt)
	}

	wantAt := "2026-09-17T18:33:54.25-05:00"
	commands := runSuccessfulRestore(t, []string{"foo.sbx.ikigenba.dev", "crm", "--at", wantAt})
	want := seam.Cmd{
		Path: "ssh",
		Args: []string{
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=accept-new",
			"ec2-user@192.0.2.10",
			"'sudo' 'opsctl' 'restore' 'crm' '--at' '" + wantAt + "'",
		},
		Dir: "/work",
	}
	if !reflect.DeepEqual(commands, []seam.Cmd{want}) {
		t.Fatalf("commands = %#v, want %#v", commands, []seam.Cmd{want})
	}

	var stdout bytes.Buffer
	profiles := []string{}
	deps := restoreDeps(runningInstance(), &profiles, func(context.Context, seam.Cmd) (seam.Result, error) {
		return seam.Result{ExitCode: 7, Stdout: []byte("partial\n"), Stderr: []byte("failed\n")}, nil
	})
	err := Run(context.Background(), []string{"foo.sbx.ikigenba.dev", "unknown-local-app"}, &stdout, deps, "work")
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("failure error = %T %v, want *host.CommandError", err, err)
	}
	if commandErr.Step != "restore" || !reflect.DeepEqual(commandErr.Command, []string{"ssh", "ec2-user@192.0.2.10", "sudo", "opsctl", "restore", "unknown-local-app"}) {
		t.Errorf("failure = %#v, want unchanged restore host error", commandErr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failure stdout = %q, want empty", stdout.String())
	}
}

func TestRestoreReturnsSudoErrorUnchanged(t *testing.T) {
	// R-FV7X-I3AA
	wantErr := &sudoSentinelError{}
	remote := &recordingSudoer{err: wantErr}
	var stdout bytes.Buffer
	command := []string{"opsctl", "restore", "crm", "--at", "2026-09-17T18:33:54-05:00"}
	err := restoreOnHost(context.Background(), &stdout, remote, command)
	var gotErr *sudoSentinelError
	if !errors.As(err, &gotErr) || gotErr != wantErr || errors.Unwrap(err) != nil {
		t.Fatalf("error = %#v, want identical Sudo error %#v", err, wantErr)
	}
	if remote.step != "restore" || !reflect.DeepEqual(remote.args, command) {
		t.Fatalf("Sudo call = (%q, %#v), want (%q, %#v)", remote.step, remote.args, "restore", command)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failure stdout = %q, want empty", stdout.String())
	}
}

type sudoSentinelError struct{}

func (*sudoSentinelError) Error() string { return "sudo failed" }

type recordingSudoer struct {
	step string
	args []string
	err  error
}

func (remote *recordingSudoer) Sudo(_ context.Context, step string, args ...string) (host.Output, error) {
	remote.step = step
	remote.args = append([]string(nil), args...)
	return host.Output{}, remote.err
}

func runSuccessfulRestore(t *testing.T, args []string) []seam.Cmd {
	t.Helper()
	var commands []seam.Cmd
	var stdout bytes.Buffer
	profiles := []string{}
	deps := restoreDeps(runningInstance(), &profiles, func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		commands = append(commands, cmd)
		return seam.Result{Stdout: []byte("secret remote output\n")}, nil
	})
	if err := Run(context.Background(), args, &stdout, deps, "work"); err != nil {
		t.Fatalf("Run(%q): %v", args, err)
	}
	wantApp := argsApp(args)
	wantAt := ""
	for i, arg := range args {
		if arg == "--at" && i+1 < len(args) {
			wantAt = args[i+1]
		}
		if strings.HasPrefix(arg, "--at=") {
			wantAt = strings.TrimPrefix(arg, "--at=")
		}
	}
	detail := "opsctl restore " + wantApp
	if wantAt != "" {
		detail += " --at " + wantAt
	}
	if got, want := stdout.String(), "restore: ok ("+detail+")\n"; got != want {
		t.Errorf("Run(%q) stdout = %q, want %q", args, got, want)
	}
	return commands
}

func argsApp(args []string) string {
	operands := make([]string, 0, 2)
	for i := 0; i < len(args); i++ {
		if args[i] == "--at" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--at=") {
			continue
		}
		operands = append(operands, args[i])
	}
	return operands[1]
}

func runningInstance() []cloud.Instance {
	return []cloud.Instance{{ID: "i-1", Space: "foo.sbx.ikigenba.dev", State: cloud.StateRunning, Address: "192.0.2.10"}}
}

func restoreDeps(instances []cloud.Instance, profiles *[]string, exec seam.Runner) seam.Deps {
	return seam.Deps{
		Dir:  "/work",
		Exec: exec,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			*profiles = append(*profiles, profile)
			if region == "" {
				return cloud.Clients{SSM: fakeSSM{}}, nil
			}
			return cloud.Clients{EC2: fakeEC2{instances: instances}}, nil
		},
	}
}

type fakeSSM struct{}

func (fakeSSM) GetParameter(context.Context, string) (string, error) { return accountProperties, nil }
func (fakeSSM) PutSecureParameter(context.Context, string, string) error {
	panic("unexpected SSM write")
}
func (fakeSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	panic("unexpected SSM list")
}
func (fakeSSM) DeleteParameter(context.Context, string) error { panic("unexpected SSM delete") }

type fakeEC2 struct{ instances []cloud.Instance }

func (f fakeEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return append([]cloud.Instance(nil), f.instances...), nil
}
func (fakeEC2) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	panic("unexpected EC2 describe")
}
func (fakeEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	panic("unexpected EC2 run")
}
func (fakeEC2) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) {
	panic("unexpected EC2 launch readiness probe")
}
func (fakeEC2) StartInstance(context.Context, string) error     { panic("unexpected EC2 start") }
func (fakeEC2) StopInstance(context.Context, string) error      { panic("unexpected EC2 stop") }
func (fakeEC2) TerminateInstance(context.Context, string) error { panic("unexpected EC2 terminate") }
func (fakeEC2) InstanceChecksPassed(context.Context, string) (bool, error) {
	panic("unexpected EC2 checks")
}
func (fakeEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) {
	panic("unexpected EC2 addresses")
}
func (fakeEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	panic("unexpected EC2 allocate")
}
func (fakeEC2) AssociateAddress(context.Context, string, string) error {
	panic("unexpected EC2 associate")
}
func (fakeEC2) DisassociateAddress(context.Context, string) error {
	panic("unexpected EC2 disassociate")
}
func (fakeEC2) ReleaseAddress(context.Context, string) error { panic("unexpected EC2 release") }
