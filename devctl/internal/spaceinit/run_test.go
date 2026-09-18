package spaceinit

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const propertiesJSON = `{
  "domain":"sbx.ikigenba.dev",
  "backup_bucket":"backup-bucket",
  "launch_template_id":"lt-123",
  "permissions_boundary_arn":"arn:boundary",
  "region":"us-east-2",
  "delete_secrets_on_destroy":false,
  "delete_backups_on_destroy":false,
  "backup_host_files_seconds":11,
  "backup_service_files_seconds":22,
  "backup_service_db_seconds":33,
  "backup_service_wal_seconds":44
}`

const expectedHelp = `Usage: devctl --account <name> space init <domain> [--opsctl <version>] [--acme-email <address>]

Set the host's nine derived keys again and run opsctl init. Keep its installed
opsctl and email unless an option names a replacement. Cloud resources and
secrets are unchanged.

Options:
  --opsctl <version>      install this release using the host's saved installer
  --acme-email <address>  replace the CA contact address
`

func TestRunSignature(_ *testing.T) {
	// R-GH64-DYMS
	acceptRunSignature(Run)
}

func acceptRunSignature(func(context.Context, []string, io.Writer, seam.Deps, string) error) {}

func TestParseInvocation(t *testing.T) {
	// R-GIE0-RQDH
	wantEmail := "last@example"
	tests := []struct {
		name string
		args []string
		want invocation
	}{
		{name: "exactly one domain", args: []string{"one.example"}, want: invocation{domain: "one.example"}},
		{
			name: "options on both sides and last values win",
			args: []string{"--opsctl", "v1", "one.example", "--acme-email=first@example", "--opsctl=v2", "--acme-email", "last@example"},
			want: invocation{domain: "one.example", opsctl: "v2", acmeEmail: &wantEmail},
		},
		{name: "long help without domain", args: []string{"--help"}, want: invocation{help: true}},
		{name: "short help without domain", args: []string{"-h"}, want: invocation{help: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseInvocation(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseInvocation = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestArgumentFailuresPrecedeDependencies(t *testing.T) {
	// R-GJLX-5I46
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", want: "space init needs <domain>"},
		{name: "extra", args: []string{"one.example", "two.example"}, want: "space init takes only <domain>"},
		{name: "unknown", args: []string{"one.example", "--force"}, want: "unknown option '--force'"},
		{name: "opsctl missing", args: []string{"one.example", "--opsctl"}, want: "option '--opsctl' requires a value"},
		{name: "opsctl separated empty", args: []string{"one.example", "--opsctl", ""}, want: "option '--opsctl' requires a value"},
		{name: "opsctl empty", args: []string{"--opsctl=", "one.example"}, want: "option '--opsctl' requires a value"},
		{name: "email missing", args: []string{"one.example", "--acme-email"}, want: "option '--acme-email' requires a value"},
		{name: "email separated empty", args: []string{"one.example", "--acme-email", ""}, want: "option '--acme-email' requires a value"},
		{name: "email empty", args: []string{"one.example", "--acme-email="}, want: "option '--acme-email' requires a value"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout strings.Builder
			err := Run(context.Background(), test.args, &stdout, noCallDeps(t), "sandbox")
			wantErr := &space.UsageError{Message: test.want, Help: "devctl space --help"}
			if !reflect.DeepEqual(err, wantErr) {
				t.Fatalf("error = %#v, want %#v", err, wantErr)
			}
			if stdout.String() != "" {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestHelpBeforeDependencies(t *testing.T) {
	// R-GM1P-X1LK
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			var stdout strings.Builder
			err := Run(context.Background(), []string{option}, &stdout, noCallDeps(t), "")
			if err != nil {
				t.Fatal(err)
			}
			if stdout.String() != expectedHelp {
				t.Fatalf("help = %q, want %q", stdout.String(), expectedHelp)
			}
		})
	}
}

func noCallDeps(t *testing.T) seam.Deps {
	t.Helper()
	return seam.Deps{
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("cloud dependency called")
			return cloud.Clients{}, nil
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			t.Fatal("exec dependency called")
			return seam.Result{}, nil
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			t.Fatal("stream dependency called")
			return seam.Result{}, nil
		},
	}
}

func TestAccountFailurePrecedesOutputAndDownstreamWork(t *testing.T) {
	// R-GN9M-ATC9
	wantErr := errors.New("account lookup failed")
	fixture := newFixture(runningInstances(), hostedZones())
	fixture.ssmErr = wantErr
	var stdout strings.Builder
	err := Run(context.Background(), []string{"app.sbx.ikigenba.dev"}, &stdout, fixture.deps(t), "sandbox")
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !reflect.DeepEqual(fixture.cloudRegions, []string{""}) {
		t.Fatalf("cloud regions = %v, want only account bootstrap", fixture.cloudRegions)
	}
	if len(fixture.commands) != 0 {
		t.Fatalf("SSH commands = %v, want none", fixture.commands)
	}
}

func TestResolutionCompletesBeforeOutputOrSSH(t *testing.T) {
	// R-GN9M-ATC9
	tests := []struct {
		name      string
		instances []cloud.Instance
		zones     []cloud.Zone
		wantError string
	}{
		{name: "absent", wantError: "no space at 'app.sbx.ikigenba.dev'"},
		{name: "stopped", instances: []cloud.Instance{{ID: "i-one", Space: "app.sbx.ikigenba.dev", State: cloud.StateStopped}}, wantError: "'app.sbx.ikigenba.dev' is stopped"},
		{name: "zone absent", instances: runningInstances(), wantError: "no hosted zone for 'app.sbx.ikigenba.dev'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(test.instances, test.zones)
			var stdout strings.Builder
			err := Run(context.Background(), []string{"app.sbx.ikigenba.dev"}, &stdout, fixture.deps(t), "sandbox")
			if err == nil || err.Error() != test.wantError {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if stdout.String() != "" || len(fixture.commands) != 0 {
				t.Fatalf("stdout = %q, SSH commands = %v; want neither", stdout.String(), fixture.commands)
			}
		})
	}
}

func TestRunKeepsVersionAndEmail(t *testing.T) {
	// R-GOHI-OL2Y
	fixture := newFixture(runningInstances(), hostedZones())
	fixture.results = []seam.Result{{Stdout: []byte("v3.2.1\n")}}
	var stdout strings.Builder
	if err := Run(context.Background(), []string{"app.sbx.ikigenba.dev"}, &stdout, fixture.deps(t), "sandbox"); err != nil {
		t.Fatal(err)
	}
	want := "account: ok (sbx.ikigenba.dev, us-east-2)\n" +
		"domain: ok (zone sbx.ikigenba.dev ZONE1)\n" +
		"instance: ok (i-one running, 192.0.2.10)\n" +
		"opsctl: ok (v3.2.1 kept, 9 keys set)\n" +
		"init: ok\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	assertCommands(t, fixture.commands, false, false)
}

func TestRunUpgradesSetsEmailAndInitializes(t *testing.T) {
	// R-GOHI-OL2Y R-GPPF-2CTN
	fixture := newFixture(runningInstances(), hostedZones())
	fixture.results = make([]seam.Result, 12)
	fixture.results[11].Stdout = []byte("successful remote output must be discarded\n")
	var stdout strings.Builder
	args := []string{"--acme-email=ops@example", "app.sbx.ikigenba.dev", "--opsctl", "v8.7.6"}
	if err := Run(context.Background(), args, &stdout, fixture.deps(t), "sandbox"); err != nil {
		t.Fatal(err)
	}
	wantSuffix := "opsctl: ok (v8.7.6 installed, 10 keys set)\ninit: ok\n"
	if !strings.HasSuffix(stdout.String(), wantSuffix) {
		t.Fatalf("stdout = %q, want suffix %q", stdout.String(), wantSuffix)
	}
	if strings.Contains(stdout.String(), "successful remote output") {
		t.Fatalf("stdout leaked successful remote output: %q", stdout.String())
	}
	assertCommands(t, fixture.commands, true, true)
}

func TestRunStopsAtRemoteFailureWithoutOtherMutation(t *testing.T) {
	// R-GPPF-2CTN R-GQXB-G4KC
	tests := []struct {
		name         string
		args         []string
		failureIndex int
		result       seam.Result
		wantCommands int
		wantStep     string
		wantDetail   string
		wantOpsctlOK bool
		wantInitOK   bool
		retainedKeys int
	}{
		{
			name: "upgrade is first failure",
			args: []string{"app.sbx.ikigenba.dev", "--opsctl=v9"}, failureIndex: 0,
			result:       seam.Result{ExitCode: 11, Stderr: []byte("upgrade failed\nall details\n")},
			wantCommands: 1, wantStep: "opsctl", wantDetail: "> upgrade failed\n> all details",
		},
		{
			name: "version is first failure and stdout is fallback",
			args: []string{"app.sbx.ikigenba.dev"}, failureIndex: 0,
			result:       seam.Result{ExitCode: 12, Stdout: []byte("version failed on stdout\ncomplete output\n")},
			wantCommands: 1, wantStep: "opsctl", wantDetail: "> version failed on stdout\n> complete output",
		},
		{
			name: "configuration stops with earlier keys retained",
			args: []string{"app.sbx.ikigenba.dev", "--opsctl=v9"}, failureIndex: 4,
			result:       seam.Result{ExitCode: 17, Stdout: []byte("ignored stdout\n"), Stderr: []byte("first line\nsecond line\n")},
			wantCommands: 5, wantStep: "opsctl", wantDetail: "> first line\n> second line", retainedKeys: 3,
		},
		{
			name: "init is last attempted step",
			args: []string{"app.sbx.ikigenba.dev"}, failureIndex: 10,
			result:       seam.Result{ExitCode: 18, Stderr: []byte("init failed\ncomplete detail\n")},
			wantCommands: 11, wantStep: "init", wantDetail: "> init failed\n> complete detail", wantOpsctlOK: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(runningInstances(), hostedZones())
			fixture.results = make([]seam.Result, test.failureIndex+1)
			fixture.results[test.failureIndex] = test.result
			var stdout strings.Builder
			err := Run(context.Background(), test.args, &stdout, fixture.deps(t), "sandbox")
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) {
				t.Fatalf("error = %T %v, want *host.CommandError", err, err)
			}
			if commandErr.Step != test.wantStep {
				t.Fatalf("step = %q, want %q", commandErr.Step, test.wantStep)
			}
			if commandErr.Detail() != test.wantDetail {
				t.Fatalf("detail = %q, want %q", commandErr.Detail(), test.wantDetail)
			}
			if len(fixture.commands) != test.wantCommands {
				t.Fatalf("commands = %v; want %d attempts", fixture.commands, test.wantCommands)
			}
			if got := strings.Contains(stdout.String(), "opsctl: ok"); got != test.wantOpsctlOK {
				t.Fatalf("opsctl success reported = %v, want %v; stdout = %q", got, test.wantOpsctlOK, stdout.String())
			}
			if got := strings.Contains(stdout.String(), "init: ok"); got != test.wantInitOK {
				t.Fatalf("init success reported = %v, want %v; stdout = %q", got, test.wantInitOK, stdout.String())
			}
			if test.retainedKeys > 0 {
				configured := 0
				for _, command := range fixture.commands {
					if strings.Contains(command, "'config' 'set'") {
						configured++
					}
				}
				if configured != test.retainedKeys+1 {
					t.Fatalf("config attempts = %d, want %d successful plus failed attempt", configured, test.retainedKeys)
				}
			}
			assertNoForbiddenCommands(t, fixture.commands)
			if fixture.mutations != 0 {
				t.Fatalf("cloud mutations = %d, want zero", fixture.mutations)
			}
		})
	}
}

func assertNoForbiddenCommands(t *testing.T, commands []string) {
	t.Helper()
	for _, command := range commands {
		for _, forbidden := range []string{"checkout", "deploy", "restore", "restart"} {
			if strings.Contains(command, forbidden) {
				t.Fatalf("forbidden %s command attempted: %q", forbidden, command)
			}
		}
	}
}

func runningInstances() []cloud.Instance {
	return []cloud.Instance{{ID: "i-one", Space: "app.sbx.ikigenba.dev", State: cloud.StateRunning, Address: "192.0.2.10"}}
}

func hostedZones() []cloud.Zone {
	return []cloud.Zone{{ID: "ZONE1", Name: "sbx.ikigenba.dev"}}
}

func assertCommands(t *testing.T, commands []string, upgraded, email bool) {
	t.Helper()
	wantCount := 11
	if email {
		wantCount++
	}
	if len(commands) != wantCount {
		t.Fatalf("command count = %d, want %d: %v", len(commands), wantCount, commands)
	}
	if upgraded && !strings.Contains(commands[0], "'/usr/local/share/ikigenba/opsctl-install.sh' 'v8.7.6'") {
		t.Fatalf("upgrade command = %q", commands[0])
	}
	if !upgraded && !strings.Contains(commands[0], "'opsctl' 'version'") {
		t.Fatalf("version command = %q", commands[0])
	}
	joined := strings.Join(commands, "\n")
	for _, key := range []string{"host.name", "dns.provider", "dns.zones", "aws.region", "backup.s3_uri", "backup.host_files_seconds", "backup.service_files_seconds", "backup.service_db_seconds", "backup.service_wal_seconds"} {
		if strings.Count(joined, key+"=") != 1 {
			t.Fatalf("commands set %s incorrectly: %v", key, commands)
		}
	}
	if got := strings.Contains(joined, "acme.email=ops@example"); got != email {
		t.Fatalf("email command present = %v, want %v", got, email)
	}
	if !strings.Contains(commands[len(commands)-1], "'sudo' 'opsctl' 'init'") {
		t.Fatalf("last command = %q, want init", commands[len(commands)-1])
	}
}

type fixture struct {
	instances    []cloud.Instance
	zones        []cloud.Zone
	commands     []string
	results      []seam.Result
	cloudRegions []string
	ssmErr       error
	mutations    int
}

func newFixture(instances []cloud.Instance, zones []cloud.Zone) *fixture {
	return &fixture{instances: instances, zones: zones}
}

func (f *fixture) deps(t *testing.T) seam.Deps {
	t.Helper()
	return seam.Deps{
		Dir: ".",
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			f.cloudRegions = append(f.cloudRegions, region)
			if profile != "sandbox" {
				t.Fatalf("profile = %q", profile)
			}
			if region == "" {
				return cloud.Clients{SSM: fixtureSSM{fixture: f}}, nil
			}
			if region != "us-east-2" {
				t.Fatalf("region = %q", region)
			}
			return cloud.Clients{EC2: fixtureEC2{fixture: f}, Route53: fixtureRoute53{fixture: f}}, nil
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path != "ssh" || len(command.Args) == 0 {
				t.Fatalf("command = %#v", command)
			}
			f.commands = append(f.commands, command.Args[len(command.Args)-1])
			index := len(f.commands) - 1
			if index < len(f.results) {
				return f.results[index], nil
			}
			return seam.Result{}, nil
		},
	}
}

type fixtureSSM struct{ fixture *fixture }

func (f fixtureSSM) GetParameter(context.Context, string) (string, error) {
	if f.fixture.ssmErr != nil {
		return "", f.fixture.ssmErr
	}
	return propertiesJSON, nil
}
func (f fixtureSSM) PutSecureParameter(context.Context, string, string) error {
	f.fixture.mutations++
	return errors.New("unexpected PutSecureParameter")
}
func (fixtureSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, errors.New("unexpected ListParameters")
}
func (f fixtureSSM) DeleteParameter(context.Context, string) error {
	f.fixture.mutations++
	return errors.New("unexpected DeleteParameter")
}

type fixtureEC2 struct{ fixture *fixture }

func (f fixtureEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return f.fixture.instances, nil
}
func (fixtureEC2) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{}, errors.New("unexpected DescribeInstance")
}
func (f fixtureEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	f.fixture.mutations++
	return cloud.Instance{}, errors.New("unexpected RunInstance")
}
func (f fixtureEC2) StartInstance(context.Context, string) error { f.fixture.mutations++; return nil }
func (f fixtureEC2) StopInstance(context.Context, string) error  { f.fixture.mutations++; return nil }
func (f fixtureEC2) TerminateInstance(context.Context, string) error {
	f.fixture.mutations++
	return nil
}
func (fixtureEC2) InstanceChecksPassed(context.Context, string) (bool, error) {
	return false, errors.New("unexpected InstanceChecksPassed")
}
func (fixtureEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) {
	return nil, errors.New("unexpected ListSpaceAddresses")
}
func (f fixtureEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	f.fixture.mutations++
	return cloud.Address{}, nil
}
func (f fixtureEC2) AssociateAddress(context.Context, string, string) error {
	f.fixture.mutations++
	return nil
}
func (f fixtureEC2) DisassociateAddress(context.Context, string) error {
	f.fixture.mutations++
	return nil
}
func (f fixtureEC2) ReleaseAddress(context.Context, string) error {
	f.fixture.mutations++
	return nil
}

type fixtureRoute53 struct{ fixture *fixture }

func (f fixtureRoute53) ListZones(context.Context) ([]cloud.Zone, error) { return f.fixture.zones, nil }
func (fixtureRoute53) ListRecords(context.Context, string) ([]cloud.Record, error) {
	return nil, errors.New("unexpected ListRecords")
}
func (f fixtureRoute53) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	f.fixture.mutations++
	return "", nil
}
func (fixtureRoute53) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	return "", errors.New("unexpected ChangeStatus")
}
