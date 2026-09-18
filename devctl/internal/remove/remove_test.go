package remove

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const testAccountProperties = `{
  "domain":"sbx.ikigenba.dev",
  "backup_bucket":"backups",
  "launch_template_id":"lt-1",
  "permissions_boundary_arn":"arn:boundary",
  "region":"us-test-1",
  "delete_secrets_on_destroy":false,
  "delete_backups_on_destroy":false,
  "backup_host_files_seconds":1,
  "backup_service_files_seconds":2,
  "backup_service_db_seconds":3,
  "backup_service_wal_seconds":4
}`

type runSignature func(context.Context, []string, io.Writer, seam.Deps, string) error

var _ runSignature = Run

func TestPublicContract(t *testing.T) {
	// R-GS57-TWB1 R-GTD4-7O1Q
	wantString := reflect.TypeFor[string]()
	typeOf := reflect.TypeOf(UsageError{})
	if typeOf.NumField() != 2 ||
		typeOf.Field(0).Name != "Message" || typeOf.Field(0).Type != wantString ||
		typeOf.Field(1).Name != "Help" || typeOf.Field(1).Type != wantString {
		t.Fatalf("UsageError fields = %v, want exactly Message string and Help string", typeOf)
	}
	err := &UsageError{Message: "bad invocation", Help: "devctl remove --help"}
	if err.Error() != "bad invocation" || err.ExitCode() != 2 || err.Detail() != "see 'devctl remove --help' for usage" {
		t.Fatalf("UsageError methods = (%q, %d, %q)", err.Error(), err.ExitCode(), err.Detail())
	}
}

func TestArgumentGrammarStopsBeforeExternalAccess(t *testing.T) {
	// R-GVSW-Z7J4
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{name: "none", message: "remove needs <domain> and <app>"},
		{name: "one", args: []string{"foo.example"}, message: "remove needs <domain> and <app>"},
		{name: "extra", args: []string{"foo.example", "crm", "extra"}, message: "remove takes only <domain> and <app>"},
		{name: "unknown first", args: []string{"--force", "foo.example", "crm"}, message: "unknown option '--force'"},
		{name: "unknown later", args: []string{"foo.example", "-x"}, message: "unknown option '-x'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			external := 0
			deps := seam.Deps{
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					external++
					return cloud.Clients{}, errors.New("unexpected cloud call")
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					external++
					return seam.Result{}, errors.New("unexpected command")
				},
			}
			var stdout bytes.Buffer
			err := Run(context.Background(), test.args, &stdout, deps, "selected")
			var usageErr *UsageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("Run(%q) error = %T %v, want *UsageError", test.args, err, err)
			}
			if usageErr.Message != test.message || usageErr.Help != "devctl remove --help" ||
				usageErr.Detail() != "see 'devctl remove --help' for usage" || usageErr.ExitCode() != 2 {
				t.Errorf("Run(%q) error = %#v, detail %q, code %d", test.args, usageErr, usageErr.Detail(), usageErr.ExitCode())
			}
			if stdout.Len() != 0 || external != 0 {
				t.Errorf("Run(%q) stdout/external = %q/%d, want empty/0", test.args, stdout.String(), external)
			}
		})
	}
}

func TestResolvesSelectedRunningSpaceBeforeSSH(t *testing.T) {
	// R-3TH7-OLJE
	tests := []struct {
		name      string
		instances []cloud.Instance
		want      any
	}{
		{name: "missing", want: (*account.NoSpaceError)(nil)},
		{name: "stopped", instances: []cloud.Instance{{ID: "i-1", Space: "foo.example", State: cloud.StateStopped, Address: "192.0.2.8"}}, want: (*space.NotRunningError)(nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := []string{}
			commands := 0
			deps := removeDeps(test.instances, &profiles, func(context.Context, seam.Cmd) (seam.Result, error) {
				commands++
				return seam.Result{}, errors.New("unexpected SSH")
			})
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"foo.example", "crm"}, &stdout, deps, "Selected Profile")
			switch test.want.(type) {
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
			if !reflect.DeepEqual(profiles, []string{"Selected Profile", "Selected Profile"}) {
				t.Errorf("profiles = %q, want selected profile twice", profiles)
			}
			if stdout.Len() != 0 || commands != 0 {
				t.Errorf("stdout/commands = %q/%d, want empty/0", stdout.String(), commands)
			}
		})
	}
}

func TestInvokesOpsctlUninstallAndReportsSuccess(t *testing.T) {
	// R-GZGM-4IR7
	profiles := []string{}
	var commands []seam.Cmd
	deps := removeDeps(runningRemoveInstance(), &profiles, func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command)
		return seam.Result{Stdout: []byte("remote output must be discarded\n")}, nil
	})
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"foo.example", "crm"}, &stdout, deps, "work"); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	wantCommand := seam.Cmd{
		Path: "ssh",
		Args: []string{
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=accept-new",
			"ec2-user@192.0.2.8",
			"'sudo' 'opsctl' 'uninstall' 'crm'",
		},
		Dir: "/work",
	}
	if !reflect.DeepEqual(commands, []seam.Cmd{wantCommand}) {
		t.Errorf("commands = %#v, want %#v", commands, []seam.Cmd{wantCommand})
	}
	if stdout.String() != "remove: ok (opsctl uninstalled crm)\n" {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestHostFailureIsReturnedWithoutSuccess(t *testing.T) {
	// R-GZGM-4IR7
	profiles := []string{}
	deps := removeDeps(runningRemoveInstance(), &profiles, func(context.Context, seam.Cmd) (seam.Result, error) {
		return seam.Result{ExitCode: 7, Stdout: []byte("partial\n"), Stderr: []byte("failed\n")}, nil
	})
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"foo.example", "crm"}, &stdout, deps, "work")
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error = %T %v, want *host.CommandError", err, err)
	}
	if commandErr.Step != "remove" || !reflect.DeepEqual(commandErr.Command, []string{"ssh", "ec2-user@192.0.2.8", "sudo", "opsctl", "uninstall", "crm"}) {
		t.Errorf("host error = %#v", commandErr)
	}
	if stdout.Len() != 0 {
		t.Errorf("failure stdout = %q, want empty", stdout.String())
	}
}

func runningRemoveInstance() []cloud.Instance {
	return []cloud.Instance{{ID: "i-1", Space: "foo.example", State: cloud.StateRunning, Address: "192.0.2.8"}}
}

func removeDeps(instances []cloud.Instance, profiles *[]string, exec seam.Runner) seam.Deps {
	return seam.Deps{
		Dir:  "/work",
		Exec: exec,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			*profiles = append(*profiles, profile)
			if region == "" {
				return cloud.Clients{SSM: removeSSM{}}, nil
			}
			return cloud.Clients{EC2: removeEC2{instances: instances}}, nil
		},
	}
}

type removeSSM struct{}

func (removeSSM) GetParameter(context.Context, string) (string, error) {
	return testAccountProperties, nil
}
func (removeSSM) PutSecureParameter(context.Context, string, string) error {
	panic("unexpected secret mutation")
}
func (removeSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	panic("unexpected secret read")
}
func (removeSSM) DeleteParameter(context.Context, string) error {
	panic("unexpected secret deletion")
}

type removeEC2 struct{ instances []cloud.Instance }

func (f removeEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return append([]cloud.Instance(nil), f.instances...), nil
}
func (removeEC2) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	panic("unexpected instance describe")
}
func (removeEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	panic("unexpected instance creation")
}
func (removeEC2) StartInstance(context.Context, string) error { panic("unexpected instance start") }
func (removeEC2) StopInstance(context.Context, string) error  { panic("unexpected instance stop") }
func (removeEC2) TerminateInstance(context.Context, string) error {
	panic("unexpected instance termination")
}
func (removeEC2) InstanceChecksPassed(context.Context, string) (bool, error) {
	panic("unexpected instance checks")
}
func (removeEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) {
	panic("unexpected address read")
}
func (removeEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	panic("unexpected address allocation")
}
func (removeEC2) AssociateAddress(context.Context, string, string) error {
	panic("unexpected address association")
}
func (removeEC2) DisassociateAddress(context.Context, string) error {
	panic("unexpected address disassociation")
}
func (removeEC2) ReleaseAddress(context.Context, string) error {
	panic("unexpected address release")
}
