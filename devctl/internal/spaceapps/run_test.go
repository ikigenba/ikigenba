package spaceapps

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/appref"
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

	for _, args := range [][]string{nil, {}, {"unknown"}, {"--unknown"}} {
		err := Run(context.Background(), args, io.Discard, seam.Deps{}, "work")
		var usage *space.UsageError
		if !errors.As(err, &usage) {
			t.Errorf("Run(%q) error = %T %v, want *space.UsageError", args, err, err)
		}
	}
}

func TestRestartAndLogsAcceptBothHelpOptions(t *testing.T) {
	// R-H34B-9TZA
	for _, subcommand := range []string{"restart", "logs"} {
		for _, option := range []string{"-h", "--help"} {
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
			if err := Run(context.Background(), []string{subcommand, option}, io.Discard, deps, ""); err != nil {
				t.Errorf("Run(%q, %q) = %v", subcommand, option, err)
			}
			if external != 0 {
				t.Errorf("Run(%q, %q) external calls = %d, want 0", subcommand, option, external)
			}
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
		{args: []string{"logs", "--since", "--since", "foo.example", "crm"}, message: "option '--since' requires a value"},
		{args: []string{"logs", "--since", "--since=-1h", "foo.example", "crm"}, message: "option '--since' requires a value"},
		{args: []string{"logs", "--since", "--help", "foo.example", "crm"}, message: "option '--since' requires a value"},
		{args: []string{"logs", "--since", "-h", "foo.example", "crm"}, message: "option '--since' requires a value"},
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
		name       string
		subcommand string
		instances  []cloud.Instance
		wantType   any
		wantError  string
	}{
		{name: "restart missing", subcommand: "restart", wantType: (*account.NoSpaceError)(nil), wantError: "no space at 'foo.example'"},
		{name: "restart stopped", subcommand: "restart", instances: []cloud.Instance{{Space: "foo.example", State: cloud.StateStopped}}, wantType: (*space.NotRunningError)(nil), wantError: "'foo.example' is stopped"},
		{name: "logs missing", subcommand: "logs", wantType: (*account.NoSpaceError)(nil), wantError: "no space at 'foo.example'"},
		{name: "logs stopped", subcommand: "logs", instances: []cloud.Instance{{Space: "foo.example", State: cloud.StateStopped}}, wantType: (*space.NotRunningError)(nil), wantError: "'foo.example' is stopped"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var events []string
			mutations := 0
			hostCalls := 0
			deps := resolutionDeps(test.instances, &events, &mutations)
			deps.Exec = func(context.Context, seam.Cmd) (seam.Result, error) {
				hostCalls++
				return seam.Result{}, errors.New("unexpected host")
			}
			deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
				hostCalls++
				return seam.Result{}, errors.New("unexpected stream")
			}
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{test.subcommand, "foo.example", "crm"}, &stdout, deps, "selected")
			if err == nil || err.Error() != test.wantError {
				t.Fatalf("error = %T %v, want %q", err, err, test.wantError)
			}
			switch test.wantType.(type) {
			case *account.NoSpaceError:
				var want *account.NoSpaceError
				if !errors.As(err, &want) || want.Domain != "foo.example" {
					t.Fatalf("error = %T %v, want *account.NoSpaceError", err, err)
				}
			case *space.NotRunningError:
				var want *space.NotRunningError
				if !errors.As(err, &want) || want.Domain != "foo.example" || want.State != cloud.StateStopped {
					t.Fatalf("error = %T %v, want *space.NotRunningError", err, err)
				}
			}
			wantEvents := []string{"cloud selected ", "get /ikigenba/account", "cloud selected us-test-1", "list spaces"}
			if !reflect.DeepEqual(events, wantEvents) || mutations != 0 || hostCalls != 0 || stdout.Len() != 0 {
				t.Errorf("events/mutations/host/stdout = %q/%d/%d/%q", events, mutations, hostCalls, stdout.String())
			}
		})
	}
}

func TestOperationsResolveTargetWithoutCheckoutOrCloudMutation(t *testing.T) {
	// R-H97T-6OOR
	for _, subcommand := range []string{"restart", "logs"} {
		t.Run(subcommand, func(t *testing.T) {
			var events []string
			mutations := 0
			var execCommands []seam.Cmd
			var streamCommands []seam.Cmd
			deps := resolutionDeps(runningInstance(), &events, &mutations)
			deps.Getenv = func(string) string {
				t.Fatal("operation consulted process environment for a checkout")
				return ""
			}
			deps.Exec = func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				execCommands = append(execCommands, command)
				if subcommand == "logs" {
					return seam.Result{Stdout: []byte("loaded\n")}, nil
				}
				return seam.Result{}, nil
			}
			deps.Stream = func(_ context.Context, command seam.Cmd, _ io.Writer) (seam.Result, error) {
				streamCommands = append(streamCommands, command)
				return seam.Result{}, nil
			}
			if err := Run(context.Background(), []string{subcommand, "foo.example", "crm"}, io.Discard, deps, "selected"); err != nil {
				t.Fatal(err)
			}
			wantEvents := []string{"cloud selected ", "get /ikigenba/account", "cloud selected us-test-1", "list spaces"}
			if !reflect.DeepEqual(events, wantEvents) || mutations != 0 {
				t.Fatalf("events/mutations = %q/%d, want target resolution and no mutation", events, mutations)
			}
			for _, command := range append(append([]seam.Cmd(nil), execCommands...), streamCommands...) {
				if command.Path != "ssh" || command.Dir != "/checkout-must-not-be-read" {
					t.Fatalf("external command = %#v, want only SSH without checkout discovery", command)
				}
			}
			if subcommand == "restart" && (len(execCommands) != 1 || len(streamCommands) != 0) {
				t.Fatalf("restart exec/stream counts = %d/%d", len(execCommands), len(streamCommands))
			}
			if subcommand == "logs" && (len(execCommands) != 1 || len(streamCommands) != 1) {
				t.Fatalf("logs exec/stream counts = %d/%d", len(execCommands), len(streamCommands))
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
		{name: "options before after and repeated follow", args: []string{"logs", "--follow", "foo.example", "--since=-2h", "crm", "--follow"}, want: "'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '--since' '-2h' '-f' '--no-pager'"},
		{name: "last since wins and dash value is unchanged", args: []string{"logs", "--since", "yesterday", "foo.example", "crm", "--since", "-1h"}, want: "'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '--since' '-1h' '--no-pager'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := []string{}
			var commands []seam.Cmd
			var streams []seam.Cmd
			deps := operationDeps(runningInstance(), &profiles)
			deps.Exec = func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				commands = append(commands, command)
				return seam.Result{Stdout: []byte("loaded\n")}, nil
			}
			deps.Stream = func(_ context.Context, command seam.Cmd, _ io.Writer) (seam.Result, error) {
				streams = append(streams, command)
				return seam.Result{}, nil
			}
			if err := Run(context.Background(), test.args, io.Discard, deps, "work"); err != nil {
				t.Fatal(err)
			}
			wantQuery := sshCommand("'sudo' 'systemctl' 'show' '--property=LoadState' '--value' 'ikigenba-crm.service'")
			if !reflect.DeepEqual(commands, []seam.Cmd{wantQuery}) {
				t.Errorf("exec commands = %#v, want only unit query %#v", commands, wantQuery)
			}
			if want := sshCommand(test.want); !reflect.DeepEqual(streams, []seam.Cmd{want}) {
				t.Errorf("stream commands = %#v, want only %#v", streams, want)
			}
			for _, command := range append(append([]seam.Cmd(nil), commands...), streams...) {
				if strings.Contains(strings.Join(command.Args, " "), "opsctl") {
					t.Errorf("logs invoked opsctl: %#v", command)
				}
			}
		})
	}
}

func TestLogsStreamsOutputAndReportsFailure(t *testing.T) {
	// R-HE3E-PRNJ
	profiles := []string{}
	deps := operationDeps(runningInstance(), &profiles)
	deps.Exec = loadedUnit
	written := make(chan struct{})
	release := make(chan struct{})
	deps.Stream = func(_ context.Context, command seam.Cmd, stdout io.Writer) (seam.Result, error) {
		if want := sshCommand("'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '-n' '100' '--no-pager'"); !reflect.DeepEqual(command, want) {
			t.Errorf("stream command = %#v, want %#v", command, want)
		}
		_, _ = io.WriteString(stdout, "first\x00second\n")
		close(written)
		<-release
		return seam.Result{ExitCode: 9, Stderr: []byte("journal failed\nmore\n")}, nil
	}
	var stdout bytes.Buffer
	errResult := make(chan error, 1)
	go func() {
		errResult <- Run(context.Background(), []string{"logs", "foo.example", "crm"}, &stdout, deps, "work")
	}()
	<-written
	if stdout.String() != "first\x00second\n" {
		t.Fatalf("stdout before Stream returns = %q", stdout.String())
	}
	select {
	case err := <-errResult:
		t.Fatalf("Run returned before stream release: %v", err)
	default:
	}
	close(release)
	err := <-errResult
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Step != "logs" || commandErr.ExitCode() != 1 ||
		commandErr.Error() != "logs: ssh ec2-user@192.0.2.8 sudo journalctl -u ikigenba-crm.service -n 100 --no-pager: exit status 9" ||
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
	apps := []string{"", "Bad/App", "host", "crm-", "crm_api", "crm.api", strings.Repeat("a", 64)}
	for _, app := range apps {
		if appref.ValidName(app) {
			t.Fatalf("test case %q unexpectedly valid", app)
		}
		t.Run(app, func(t *testing.T) {
			cloudCalls, execCalls, streamCalls := 0, 0, 0
			deps := seam.Deps{
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					cloudCalls++
					return cloud.Clients{}, errors.New("unexpected cloud")
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					execCalls++
					return seam.Result{}, errors.New("unexpected exec")
				},
				Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
					streamCalls++
					return seam.Result{}, errors.New("unexpected stream")
				},
			}
			err := Run(context.Background(), []string{"logs", "foo.example", app}, io.Discard, deps, "work")
			var usage *space.UsageError
			if !errors.As(err, &usage) || usage.Message != "'"+app+"' is not a usable app name" ||
				usage.Help != "devctl space --help" || usage.ExitCode() != 2 {
				t.Fatalf("error = %#v", err)
			}
			if cloudCalls != 0 || execCalls != 0 || streamCalls != 0 {
				t.Fatalf("cloud/exec/stream calls = %d/%d/%d, want zero", cloudCalls, execCalls, streamCalls)
			}
		})
	}
}

func TestLogsFormsUnitFromValidatedApp(t *testing.T) {
	// R-HFBB-3JE8
	const app = "crm-api"
	if !appref.ValidName(app) {
		t.Fatal("valid app fixture rejected")
	}
	profiles := []string{}
	var commands []seam.Cmd
	var streams []seam.Cmd
	deps := operationDeps(runningInstance(), &profiles)
	deps.Exec = func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command)
		return seam.Result{Stdout: []byte("loaded\n")}, nil
	}
	deps.Stream = func(_ context.Context, command seam.Cmd, _ io.Writer) (seam.Result, error) {
		streams = append(streams, command)
		return seam.Result{}, nil
	}
	if err := Run(context.Background(), []string{"logs", "foo.example", app}, io.Discard, deps, "work"); err != nil {
		t.Fatal(err)
	}
	wantUnit := "ikigenba-" + app + ".service"
	wantQuery := sshCommand("'sudo' 'systemctl' 'show' '--property=LoadState' '--value' '" + wantUnit + "'")
	wantJournal := sshCommand("'sudo' 'journalctl' '-u' '" + wantUnit + "' '-n' '100' '--no-pager'")
	if !reflect.DeepEqual(commands, []seam.Cmd{wantQuery}) || !reflect.DeepEqual(streams, []seam.Cmd{wantJournal}) {
		t.Fatalf("commands/streams = %#v/%#v, want validated unit %q", commands, streams, wantUnit)
	}
}

func resolutionDeps(instances []cloud.Instance, events *[]string, mutations *int) seam.Deps {
	ssm := guardedSSM{events: events, mutations: mutations}
	ec2 := guardedEC2{events: events, mutations: mutations, instances: instances}
	return seam.Deps{
		Dir: "/checkout-must-not-be-read",
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			*events = append(*events, "cloud "+profile+" "+region)
			if region == "" {
				return cloud.Clients{SSM: ssm}, nil
			}
			return cloud.Clients{EC2: ec2}, nil
		},
	}
}

type guardedSSM struct {
	cloud.SSM
	events    *[]string
	mutations *int
}

func (f guardedSSM) GetParameter(_ context.Context, name string) (string, error) {
	*f.events = append(*f.events, "get "+name)
	return properties, nil
}

func (f guardedSSM) PutSecureParameter(context.Context, string, string) error {
	*f.mutations++
	return errors.New("unexpected parameter mutation")
}

func (f guardedSSM) DeleteParameter(context.Context, string) error {
	*f.mutations++
	return errors.New("unexpected parameter mutation")
}

type guardedEC2 struct {
	cloud.EC2
	events    *[]string
	mutations *int
	instances []cloud.Instance
}

func (f guardedEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	*f.events = append(*f.events, "list spaces")
	return append([]cloud.Instance(nil), f.instances...), nil
}

func (f guardedEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	*f.mutations++
	return cloud.Instance{}, errors.New("unexpected cloud mutation")
}

func (f guardedEC2) StartInstance(context.Context, string) error {
	*f.mutations++
	return errors.New("unexpected cloud mutation")
}

func (f guardedEC2) StopInstance(context.Context, string) error {
	*f.mutations++
	return errors.New("unexpected cloud mutation")
}

func (f guardedEC2) TerminateInstance(context.Context, string) error {
	*f.mutations++
	return errors.New("unexpected cloud mutation")
}

func (f guardedEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	*f.mutations++
	return cloud.Address{}, errors.New("unexpected cloud mutation")
}

func (f guardedEC2) AssociateAddress(context.Context, string, string) error {
	*f.mutations++
	return errors.New("unexpected cloud mutation")
}

func (f guardedEC2) DisassociateAddress(context.Context, string) error {
	*f.mutations++
	return errors.New("unexpected cloud mutation")
}

func (f guardedEC2) ReleaseAddress(context.Context, string) error {
	*f.mutations++
	return errors.New("unexpected cloud mutation")
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
