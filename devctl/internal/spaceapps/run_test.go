package spaceapps

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

const properties = `{
  "domain":"example.test",
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

type runContract func(context.Context, []string, io.Writer, seam.Deps, string) error

var _ runContract = Run

func TestPublicContract(t *testing.T) {
	// R-H0OI-IAHW R-H1WE-W28L
	typeOf := reflect.TypeFor[NoAppError]()
	if typeOf.NumField() != 2 || typeOf.Field(0).Name != "App" || typeOf.Field(0).Type != reflect.TypeFor[string]() ||
		typeOf.Field(1).Name != "Domain" || typeOf.Field(1).Type != reflect.TypeFor[string]() {
		t.Fatalf("NoAppError fields = %v, want exactly App string and Domain string", typeOf)
	}
	if got := (&NoAppError{App: "crm", Domain: "foo.example"}).Error(); got != "no app 'crm' on 'foo.example'" {
		t.Fatalf("NoAppError.Error() = %q", got)
	}

	for _, subcommand := range []string{"restart", "logs"} {
		err := Run(context.Background(), []string{subcommand}, io.Discard, seam.Deps{}, "work")
		var usage *space.UsageError
		if !errors.As(err, &usage) {
			t.Errorf("Run(%q) error = %T %v, want *space.UsageError", subcommand, err, err)
		}
	}
}

func TestGrammarRejectsBeforeExternalAccess(t *testing.T) {
	// R-H34B-9TZA R-H4C7-NLPZ
	tests := []struct {
		args    []string
		message string
	}{
		{args: []string{"restart"}, message: "space restart needs <domain> and <app>"},
		{args: []string{"logs", "foo.example"}, message: "space logs needs <domain> and <app>"},
		{args: []string{"restart", "foo.example", "crm", "extra"}, message: "space restart takes only <domain> and <app>"},
		{args: []string{"logs", "foo.example", "crm", "extra"}, message: "space logs takes only <domain> and <app>"},
		{args: []string{"restart", "--follow", "foo.example", "crm"}, message: "unknown option '--follow'"},
		{args: []string{"logs", "--wat", "foo.example", "crm"}, message: "unknown option '--wat'"},
		{args: []string{"logs", "foo.example", "crm", "--since"}, message: "option '--since' requires a value"},
		{args: []string{"logs", "--since", "", "foo.example", "crm"}, message: "option '--since' requires a value"},
		{args: []string{"logs", "--since", "--follow", "foo.example", "crm"}, message: "option '--since' requires a value"},
		{args: []string{"logs", "--since="}, message: "option '--since' requires a value"},
	}
	for _, test := range tests {
		t.Run(test.message+reflect.ValueOf(test.args).String(), func(t *testing.T) {
			external := 0
			deps := seam.Deps{
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					external++
					return cloud.Clients{}, errors.New("unexpected cloud")
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					external++
					return seam.Result{}, errors.New("unexpected exec")
				},
				Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
					external++
					return seam.Result{}, errors.New("unexpected stream")
				},
			}
			var stdout bytes.Buffer
			err := Run(context.Background(), test.args, &stdout, deps, "work")
			var usage *space.UsageError
			if !errors.As(err, &usage) {
				t.Fatalf("Run(%q) error = %T %v, want *space.UsageError", test.args, err, err)
			}
			if usage.Message != test.message || usage.Help != "devctl space --help" || usage.ExitCode() != 2 {
				t.Errorf("usage = %#v, code %d", usage, usage.ExitCode())
			}
			if external != 0 || stdout.Len() != 0 {
				t.Errorf("external/stdout = %d/%q, want 0/empty", external, stdout.String())
			}
		})
	}
}

func TestResolvesRunningSpaceBeforeHostAccess(t *testing.T) {
	// R-H97T-6OOR
	tests := []struct {
		name      string
		instances []cloud.Instance
		want      any
	}{
		{name: "missing", want: (*account.NoSpaceError)(nil)},
		{name: "stopped", instances: []cloud.Instance{{Space: "foo.example", State: cloud.StateStopped}}, want: (*space.NotRunningError)(nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := []string{}
			hostCalls := 0
			deps := operationDeps(test.instances, &profiles)
			deps.Exec = func(context.Context, seam.Cmd) (seam.Result, error) {
				hostCalls++
				return seam.Result{}, errors.New("unexpected host")
			}
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"restart", "foo.example", "crm"}, &stdout, deps, "selected")
			switch test.want.(type) {
			case *account.NoSpaceError:
				var want *account.NoSpaceError
				if !errors.As(err, &want) {
					t.Fatalf("error = %T %v, want *account.NoSpaceError", err, err)
				}
			case *space.NotRunningError:
				var want *space.NotRunningError
				if !errors.As(err, &want) {
					t.Fatalf("error = %T %v, want *space.NotRunningError", err, err)
				}
			}
			if !reflect.DeepEqual(profiles, []string{"selected", "selected"}) || hostCalls != 0 || stdout.Len() != 0 {
				t.Errorf("profiles/host/stdout = %q/%d/%q", profiles, hostCalls, stdout.String())
			}
		})
	}
}

func TestRestartUsesOpsctlAndDecoratesOnlySuccess(t *testing.T) {
	// R-HAFP-KGFG
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failure], func(t *testing.T) {
			profiles := []string{}
			var commands []seam.Cmd
			deps := operationDeps(runningInstance(), &profiles)
			deps.Exec = func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				commands = append(commands, command)
				if failure {
					return seam.Result{ExitCode: 7, Stderr: []byte("restart failed\n")}, nil
				}
				return seam.Result{Stdout: []byte("discard me\n")}, nil
			}
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"restart", "foo.example", "crm"}, &stdout, deps, "work")
			wantCommand := sshCommand("'sudo' 'opsctl' 'restart' 'crm'")
			if !reflect.DeepEqual(commands, []seam.Cmd{wantCommand}) {
				t.Fatalf("commands = %#v, want %#v", commands, []seam.Cmd{wantCommand})
			}
			if failure {
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) || commandErr.Step != "restart" || stdout.Len() != 0 {
					t.Fatalf("error/stdout = %#v/%q", err, stdout.String())
				}
			} else if err != nil || stdout.String() != "restart: ok (opsctl restarted crm)\n" {
				t.Fatalf("error/stdout = %v/%q", err, stdout.String())
			}
		})
	}
}

func TestLogsChecksUnitAndRejectsAbsentApp(t *testing.T) {
	// R-HBNL-Y865
	for _, queryFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "not found", true: "query failure"}[queryFailure], func(t *testing.T) {
			profiles := []string{}
			var commands []seam.Cmd
			streamCalls := 0
			deps := operationDeps(runningInstance(), &profiles)
			deps.Exec = func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				commands = append(commands, command)
				if queryFailure {
					return seam.Result{ExitCode: 5, Stderr: []byte("query failed\n")}, nil
				}
				return seam.Result{Stdout: []byte("not-found\n")}, nil
			}
			deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
				streamCalls++
				return seam.Result{}, nil
			}
			err := Run(context.Background(), []string{"logs", "foo.example", "crm"}, io.Discard, deps, "work")
			wantQuery := sshCommand("'sudo' 'systemctl' 'show' '--property=LoadState' '--value' 'ikigenba-crm.service'")
			if !reflect.DeepEqual(commands, []seam.Cmd{wantQuery}) || streamCalls != 0 {
				t.Fatalf("commands/streams = %#v/%d", commands, streamCalls)
			}
			if queryFailure {
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) {
					t.Fatalf("error = %T %v, want *host.CommandError", err, err)
				}
			} else {
				var noApp *NoAppError
				if !errors.As(err, &noApp) || *noApp != (NoAppError{App: "crm", Domain: "foo.example"}) {
					t.Fatalf("error = %#v, want NoAppError", err)
				}
			}
		})
	}
}

func TestLogsBuildsJournalCommands(t *testing.T) {
	// R-H34B-9TZA R-HCVI-BZWU
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "default", args: []string{"logs", "foo.example", "crm"}, want: "'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '-n' '100' '--no-pager'"},
		{name: "options around operands", args: []string{"logs", "--follow", "foo.example", "--since=-2h", "crm", "--follow"}, want: "'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '--since' '-2h' '-f' '--no-pager'"},
		{name: "last since wins", args: []string{"logs", "--since", "yesterday", "foo.example", "crm", "--since", "-1h"}, want: "'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '--since' '-1h' '--no-pager'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := []string{}
			var streamed seam.Cmd
			deps := operationDeps(runningInstance(), &profiles)
			deps.Exec = loadedUnit
			deps.Stream = func(_ context.Context, command seam.Cmd, _ io.Writer) (seam.Result, error) {
				streamed = command
				return seam.Result{}, nil
			}
			if err := Run(context.Background(), test.args, io.Discard, deps, "work"); err != nil {
				t.Fatal(err)
			}
			if want := sshCommand(test.want); !reflect.DeepEqual(streamed, want) {
				t.Errorf("stream command = %#v, want %#v", streamed, want)
			}
		})
	}
}

func TestLogsStreamsOutputAndReportsFailure(t *testing.T) {
	// R-HE3E-PRNJ
	profiles := []string{}
	deps := operationDeps(runningInstance(), &profiles)
	deps.Exec = loadedUnit
	deps.Stream = func(_ context.Context, _ seam.Cmd, stdout io.Writer) (seam.Result, error) {
		_, _ = io.WriteString(stdout, "first\x00second\n")
		return seam.Result{ExitCode: 9, Stderr: []byte("journal failed\nmore\n")}, nil
	}
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"logs", "foo.example", "crm"}, &stdout, deps, "work")
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Step != "logs" || commandErr.ExitCode() != 1 ||
		commandErr.Detail() != "> journal failed\n> more" {
		t.Fatalf("error = %#v, detail %q", err, commandErr.Detail())
	}
	if stdout.String() != "first\x00second\n" {
		t.Fatalf("streamed stdout = %q", stdout.String())
	}
}

func TestLogsTreatsCancellationAsCleanInterrupt(t *testing.T) {
	// R-HE3E-PRNJ
	ctx, cancel := context.WithCancel(context.Background())
	profiles := []string{}
	deps := operationDeps(runningInstance(), &profiles)
	deps.Exec = loadedUnit
	deps.Stream = func(_ context.Context, _ seam.Cmd, stdout io.Writer) (seam.Result, error) {
		_, _ = io.WriteString(stdout, "delivered\n")
		cancel()
		return seam.Result{}, context.Canceled
	}
	var stdout bytes.Buffer
	if err := Run(ctx, []string{"logs", "foo.example", "crm", "--follow"}, &stdout, deps, "work"); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if stdout.String() != "delivered\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestLogsRejectsInvalidAppBeforeExternalAccess(t *testing.T) {
	// R-HFBB-3JE8
	external := 0
	deps := seam.Deps{Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		external++
		return cloud.Clients{}, errors.New("unexpected cloud")
	}}
	err := Run(context.Background(), []string{"logs", "foo.example", "Bad/App"}, io.Discard, deps, "work")
	var usage *space.UsageError
	if !errors.As(err, &usage) || usage.Message != "'Bad/App' is not a usable app name" || usage.Help != "devctl space --help" {
		t.Fatalf("error = %#v", err)
	}
	if external != 0 {
		t.Fatalf("external calls = %d, want 0", external)
	}
}

func operationDeps(instances []cloud.Instance, profiles *[]string) seam.Deps {
	return seam.Deps{
		Dir: "/work",
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			*profiles = append(*profiles, profile)
			if region == "" {
				return cloud.Clients{SSM: operationSSM{}}, nil
			}
			return cloud.Clients{EC2: operationEC2{instances: instances}}, nil
		},
	}
}

func runningInstance() []cloud.Instance {
	return []cloud.Instance{{ID: "i-1", Space: "foo.example", State: cloud.StateRunning, Address: "192.0.2.8"}}
}

func loadedUnit(context.Context, seam.Cmd) (seam.Result, error) {
	return seam.Result{Stdout: []byte("loaded\n")}, nil
}

func sshCommand(remote string) seam.Cmd {
	return seam.Cmd{
		Path: "ssh",
		Args: []string{
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=accept-new",
			"ec2-user@192.0.2.8",
			remote,
		},
		Dir: "/work",
	}
}

type operationSSM struct{ cloud.SSM }

func (operationSSM) GetParameter(context.Context, string) (string, error) { return properties, nil }

type operationEC2 struct {
	cloud.EC2
	instances []cloud.Instance
}

func (f operationEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return append([]cloud.Instance(nil), f.instances...), nil
}
