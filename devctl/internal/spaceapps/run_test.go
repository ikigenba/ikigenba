package spaceapps

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

type runContract func(context.Context, []string, io.Writer, seam.Deps) error

var _ runContract = Run

func TestPublicContract(t *testing.T) {
	// R-0O97-MDUA R-H1WE-W28L
	typeOf := reflect.TypeFor[NoAppError]()
	if typeOf.NumField() != 2 || typeOf.Field(0).Name != "App" || typeOf.Field(1).Name != "Domain" {
		t.Fatalf("NoAppError fields = %v", typeOf)
	}
	if got := (&NoAppError{App: "crm", Domain: "sbx1.ikigenba.dev"}).Error(); got != "no app 'crm' on 'sbx1.ikigenba.dev'" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestHelpAndSyntaxBeforeExternalAccess(t *testing.T) {
	// R-0PH4-05KZ R-0QP0-DXBO R-0RWW-RP2D R-H34B-9TZA
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"restart", "--help"}, restartHelp}, {[]string{"restart", "sbx1", "crm", "-h"}, restartHelp},
		{[]string{"logs", "--help"}, logsHelp}, {[]string{"logs", "sbx1", "crm", "-h"}, logsHelp},
	} {
		calls := 0
		var stdout bytes.Buffer
		if err := Run(context.Background(), tc.args, &stdout, forbiddenDeps(&calls)); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != tc.want || calls != 0 {
			t.Fatalf("Run(%q) stdout/calls = %q/%d", tc.args, stdout.String(), calls)
		}
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"restart"}, "space restart needs <space> and <app>"},
		{[]string{"logs", "sbx1"}, "space logs needs <space> and <app>"},
		{[]string{"restart", "sbx1", "crm", "extra"}, "space restart takes only <space> and <app>"},
		{[]string{"logs", "--wat", "sbx1", "crm"}, "unknown option '--wat'"},
		{[]string{"logs", "sbx1", "crm", "--since"}, "option '--since' requires a value"},
		{[]string{"logs", "--since", "--follow", "sbx1", "crm"}, "option '--since' requires a value"},
	} {
		calls := 0
		var stdout bytes.Buffer
		err := Run(context.Background(), tc.args, &stdout, forbiddenDeps(&calls))
		var usage *space.UsageError
		if !errors.As(err, &usage) || usage.Message != tc.want || usage.Help != "devctl space --help" {
			t.Errorf("Run(%q) = %#v", tc.args, err)
		}
		if calls != 0 || stdout.Len() != 0 {
			t.Errorf("calls/stdout = %d/%q", calls, stdout.String())
		}
	}
}

func TestResolutionFailuresStopBeforeHostAccess(t *testing.T) {
	// R-0T4T-5GT2
	for _, subcommand := range []string{"restart", "logs"} {
		for _, tc := range []struct {
			name      string
			instances []cloud.Instance
			want      string
		}{
			{name: "missing", want: "no space at 'gone.ikigenba.dev'"},
			{name: "stopped", instances: []cloud.Instance{{Space: "gone.ikigenba.dev", State: cloud.StateStopped}}, want: "'gone.ikigenba.dev' is stopped"},
		} {
			t.Run(subcommand+"/"+tc.name, func(t *testing.T) {
				f := newFixture(t, tc.instances)
				var stdout bytes.Buffer
				err := Run(context.Background(), []string{subcommand, "gone", "crm"}, &stdout, f.deps())
				if err == nil || err.Error() != tc.want {
					t.Fatalf("error = %T %v", err, err)
				}
				if stdout.Len() != 0 || len(f.execs) != 0 || len(f.streams) != 0 {
					t.Fatalf("unexpected host/output: %q %#v %#v", stdout.String(), f.execs, f.streams)
				}
				f.assertResolution(t)
			})
		}
	}
}

func TestRestartUsesRootLookupAndOpsctl(t *testing.T) {
	// R-0UCP-J8JR R-HAFP-KGFG
	f := runningFixture(t)
	f.execResults = []seam.Result{{Stdout: []byte("discard me\n")}}
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"restart", "sbx1", "crm"}, &stdout, f.deps()); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "restart: ok (opsctl restarted crm)\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	want := f.ssh("'sudo' 'opsctl' 'restart' 'crm'")
	if !reflect.DeepEqual(f.execs, []seam.Cmd{want}) || len(f.streams) != 0 {
		t.Fatalf("exec/stream = %#v/%#v", f.execs, f.streams)
	}
	f.assertResolution(t)
}

func TestRestartFailureHasNoSuccessLine(t *testing.T) {
	// R-HAFP-KGFG
	f := runningFixture(t)
	f.execResults = []seam.Result{{ExitCode: 6, Stderr: []byte("failed\n")}}
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"restart", "sbx1", "crm"}, &stdout, f.deps())
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Step != "restart" || stdout.Len() != 0 {
		t.Fatalf("error/stdout = %#v/%q", err, stdout.String())
	}
}

func TestLogsChecksUnitBeforeJournal(t *testing.T) {
	// R-HBNL-Y865
	for _, tc := range []struct {
		name   string
		result seam.Result
		noApp  bool
	}{
		{name: "absent", result: seam.Result{Stdout: []byte("not-found\n")}, noApp: true},
		{name: "query failure", result: seam.Result{ExitCode: 5, Stderr: []byte("failed\n")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := runningFixture(t)
			f.execResults = []seam.Result{tc.result}
			err := Run(context.Background(), []string{"logs", "sbx1", "crm"}, io.Discard, f.deps())
			if tc.noApp {
				var target *NoAppError
				if !errors.As(err, &target) || *target != (NoAppError{App: "crm", Domain: "sbx1.ikigenba.dev"}) {
					t.Fatalf("error = %#v", err)
				}
			} else {
				var target *host.CommandError
				if !errors.As(err, &target) {
					t.Fatalf("error = %#v", err)
				}
			}
			if len(f.streams) != 0 {
				t.Fatalf("streams = %#v", f.streams)
			}
		})
	}
}

func TestLogsOptionsAndStreaming(t *testing.T) {
	// R-H34B-9TZA R-HCVI-BZWU R-HE3E-PRNJ R-0UCP-J8JR
	for _, tc := range []struct {
		name    string
		args    []string
		journal string
	}{
		{name: "default", args: []string{"logs", "sbx1", "crm"}, journal: "'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '-n' '100' '--no-pager'"},
		{name: "combined", args: []string{"logs", "--follow", "sbx1", "--since=-2h", "crm", "--follow"}, journal: "'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '--since' '-2h' '-f' '--no-pager'"},
		{name: "last since", args: []string{"logs", "--since", "yesterday", "sbx1", "crm", "--since", "-1h"}, journal: "'sudo' 'journalctl' '-u' 'ikigenba-crm.service' '--since' '-1h' '--no-pager'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := runningFixture(t)
			f.execResults = []seam.Result{{Stdout: []byte("loaded\n")}}
			f.streamText = "first\x00second\n"
			var stdout bytes.Buffer
			if err := Run(context.Background(), tc.args, &stdout, f.deps()); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != f.streamText {
				t.Fatalf("stdout = %q", stdout.String())
			}
			wantQuery := f.ssh("'sudo' 'systemctl' 'show' '--property=LoadState' '--value' 'ikigenba-crm.service'")
			wantJournal := f.ssh(tc.journal)
			if !reflect.DeepEqual(f.execs, []seam.Cmd{wantQuery}) || !reflect.DeepEqual(f.streams, []seam.Cmd{wantJournal}) {
				t.Fatalf("commands = %#v/%#v", f.execs, f.streams)
			}
			if strings.Contains(tc.journal, "opsctl") {
				t.Fatal("journal command used opsctl")
			}
			f.assertResolution(t)
		})
	}
}

func TestLogsFailurePreservesStreamAndDiagnostic(t *testing.T) {
	// R-HE3E-PRNJ
	f := runningFixture(t)
	f.execResults = []seam.Result{{Stdout: []byte("loaded\n")}}
	f.streamText = "delivered\n"
	f.streamResult = seam.Result{ExitCode: 9, Stderr: []byte("bad\nmore\n")}
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"logs", "sbx1", "crm"}, &stdout, f.deps())
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Detail() != "> bad\n> more" || stdout.String() != "delivered\n" {
		t.Fatalf("error/stdout = %#v/%q", err, stdout.String())
	}
}

func TestLogsCancellationIsCleanAndInvalidAppNeverReachesSSH(t *testing.T) {
	// R-HE3E-PRNJ R-HFBB-3JE8
	ctx, cancel := context.WithCancel(context.Background())
	f := runningFixture(t)
	f.execResults = []seam.Result{{Stdout: []byte("loaded\n")}}
	f.streamErr = context.Canceled
	cancel()
	if err := Run(ctx, []string{"logs", "sbx1", "crm", "--follow"}, io.Discard, f.deps()); err != nil {
		t.Fatalf("cancelled logs = %v", err)
	}

	for _, app := range []string{"host", "Bad/App", "crm-", "crm.api", strings.Repeat("a", 64)} {
		calls := 0
		err := Run(context.Background(), []string{"logs", "sbx1", app}, io.Discard, forbiddenDeps(&calls))
		var usage *space.UsageError
		if !errors.As(err, &usage) || usage.Message != "'"+app+"' is not a usable app name" || calls != 0 {
			t.Fatalf("app %q error/calls = %#v/%d", app, err, calls)
		}
	}
}

func forbiddenDeps(calls *int) seam.Deps {
	return seam.Deps{Dir: "/outside",
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			(*calls)++
			return cloud.Clients{}, errors.New("unexpected cloud")
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			(*calls)++
			return seam.Result{}, errors.New("unexpected exec")
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			(*calls)++
			return seam.Result{}, errors.New("unexpected stream")
		},
	}
}

type fixture struct {
	root, work     string
	instances      []cloud.Instance
	events         []string
	execs, streams []seam.Cmd
	execResults    []seam.Result
	streamResult   seam.Result
	streamText     string
	streamErr      error
}

func newFixture(t *testing.T, instances []cloud.Instance) *fixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, checkout.RootFilePath), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	return &fixture{root: root, work: work, instances: instances}
}

func runningFixture(t *testing.T) *fixture {
	return newFixture(t, []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "192.0.2.8"}})
}

func (f *fixture) deps() seam.Deps {
	return seam.Deps{Dir: f.work,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			f.events = append(f.events, "cloud "+profile+" "+region)
			return cloud.Clients{STS: fakeSTS{events: &f.events}, EC2: fakeEC2{events: &f.events, instances: f.instances}}, nil
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path == "git" {
				f.events = append(f.events, "git")
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			}
			f.execs = append(f.execs, command)
			if len(f.execResults) == 0 {
				return seam.Result{}, nil
			}
			result := f.execResults[0]
			f.execResults = f.execResults[1:]
			return result, nil
		},
		Stream: func(_ context.Context, command seam.Cmd, stdout io.Writer) (seam.Result, error) {
			f.streams = append(f.streams, command)
			_, _ = io.WriteString(stdout, f.streamText)
			return f.streamResult, f.streamErr
		},
	}
}

func (f *fixture) assertResolution(t *testing.T) {
	t.Helper()
	want := []string{"git", "cloud ikigenba.dev us-east-2", "sts", "list ikigenba.dev"}
	if !reflect.DeepEqual(f.events, want) {
		t.Fatalf("events = %#v, want %#v", f.events, want)
	}
}
func (f *fixture) ssh(remote string) seam.Cmd {
	return seam.Cmd{Path: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@192.0.2.8", remote}, Dir: f.work}
}

type fakeSTS struct {
	cloud.STS
	events *[]string
}

func (f fakeSTS) CallerAccountID(context.Context) (string, error) {
	*f.events = append(*f.events, "sts")
	return "123", nil
}

type fakeEC2 struct {
	cloud.EC2
	events    *[]string
	instances []cloud.Instance
}

func (f fakeEC2) ListSpaceInstances(_ context.Context, domain string) ([]cloud.Instance, error) {
	*f.events = append(*f.events, "list "+domain)
	return append([]cloud.Instance(nil), f.instances...), nil
}
