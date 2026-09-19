package remove

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

type runSignature func(context.Context, []string, io.Writer, seam.Deps) error

var _ runSignature = Run

func TestPublicContract(t *testing.T) {
	// R-8AJV-4VEY R-GTD4-7O1Q
	wantString := reflect.TypeFor[string]()
	typeOf := reflect.TypeOf(UsageError{})
	if typeOf.NumField() != 2 || typeOf.Field(0).Name != "Message" || typeOf.Field(0).Type != wantString ||
		typeOf.Field(1).Name != "Help" || typeOf.Field(1).Type != wantString {
		t.Fatalf("UsageError fields = %v, want exactly Message string and Help string", typeOf)
	}
	err := &UsageError{Message: "bad invocation", Help: "devctl remove --help"}
	if err.Error() != "bad invocation" || err.ExitCode() != 2 || err.Detail() != "see 'devctl remove --help' for usage" {
		t.Fatalf("UsageError methods = (%q, %d, %q)", err.Error(), err.ExitCode(), err.Detail())
	}
}

func TestHelpAndGrammarStopBeforeExternalAccess(t *testing.T) {
	// R-HPEE-9HCL R-HRU7-10TZ
	for _, args := range [][]string{{"--help"}, {"sbx1", "crm", "-h"}} {
		var stdout bytes.Buffer
		calls := 0
		if err := Run(context.Background(), args, &stdout, forbiddenDeps(&calls)); err != nil {
			t.Fatalf("Run(%q): %v", args, err)
		}
		if stdout.String() != helpText || calls != 0 {
			t.Fatalf("Run(%q) stdout/calls = %q/%d", args, stdout.String(), calls)
		}
	}

	tests := []struct {
		args []string
		want string
	}{
		{nil, "remove needs <space> and <app>"},
		{[]string{"sbx1"}, "remove needs <space> and <app>"},
		{[]string{"sbx1", "crm", "extra"}, "remove takes only <space> and <app>"},
		{[]string{"--force", "sbx1", "crm"}, "unknown option '--force'"},
	}
	for _, tc := range tests {
		calls := 0
		var stdout bytes.Buffer
		err := Run(context.Background(), tc.args, &stdout, forbiddenDeps(&calls))
		var usageErr *UsageError
		if !errors.As(err, &usageErr) || usageErr.Message != tc.want || usageErr.Help != "devctl remove --help" {
			t.Errorf("Run(%q) = %#v, want usage %q", tc.args, err, tc.want)
		}
		if calls != 0 || stdout.Len() != 0 {
			t.Errorf("Run(%q) calls/stdout = %d/%q", tc.args, calls, stdout.String())
		}
	}
}

func TestResolutionFailuresStopBeforeSSH(t *testing.T) {
	// R-HU9Z-SKBD
	for _, tc := range []struct {
		name      string
		instances []cloud.Instance
		stopped   bool
		want      string
	}{
		{name: "missing", want: "no space at 'gone.ikigenba.dev'"},
		{name: "stopped", instances: []cloud.Instance{{Space: "gone.ikigenba.dev", State: cloud.StateStopped}}, stopped: true, want: "'gone.ikigenba.dev' is stopped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newFixture(t, tc.instances)
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"gone", "crm"}, &stdout, fixture.deps())
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %T %v, want %q", err, err, tc.want)
			}
			if tc.stopped {
				var target *space.NotRunningError
				if !errors.As(err, &target) {
					t.Fatalf("error = %T, want *space.NotRunningError", err)
				}
			} else {
				var target *cloud.NoSpaceError
				if !errors.As(err, &target) {
					t.Fatalf("error = %T, want *cloud.NoSpaceError", err)
				}
			}
			if stdout.Len() != 0 || fixture.sshCalls != 0 {
				t.Fatalf("stdout/ssh = %q/%d", stdout.String(), fixture.sshCalls)
			}
			fixture.assertResolution(t)
		})
	}
}

func TestRemoveUsesOnlyRootLookupAndOneHostCommand(t *testing.T) {
	// R-MTKM-W619 R-HWPS-K3SR R-HXXO-XVJG
	// R-GZGM-4IR7
	fixture := newFixture(t, []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}})
	fixture.sshResult = seam.Result{Stdout: []byte("discarded\n")}
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"sbx1", "crm"}, &stdout, fixture.deps()); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "remove: ok (opsctl uninstalled crm)\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	want := sshCommand(fixture.work, "18.118.7.42", "'sudo' 'opsctl' 'uninstall' 'crm'")
	if !reflect.DeepEqual(fixture.ssh, []seam.Cmd{want}) {
		t.Fatalf("ssh = %#v, want %#v", fixture.ssh, []seam.Cmd{want})
	}
	fixture.assertResolution(t)
}

func TestRemoveReturnsHostFailureWithoutSuccess(t *testing.T) {
	// R-HWPS-K3SR R-HXXO-XVJG R-GZGM-4IR7
	fixture := newFixture(t, []cloud.Instance{{Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}})
	fixture.sshResult = seam.Result{ExitCode: 7, Stderr: []byte("one\ntwo\n")}
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"sbx1", "gmail"}, &stdout, fixture.deps())
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Error() != "remove: ssh ec2-user@18.118.7.42 sudo opsctl uninstall gmail: exit status 7" || commandErr.Detail() != "> one\n> two" {
		t.Fatalf("error = %#v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	fixture.assertResolution(t)
}

func forbiddenDeps(calls *int) seam.Deps {
	return seam.Deps{Dir: "/outside-checkout",
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			(*calls)++
			return cloud.Clients{}, errors.New("unexpected cloud")
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			(*calls)++
			return seam.Result{}, errors.New("unexpected exec")
		},
	}
}

type fixture struct {
	root      string
	work      string
	instances []cloud.Instance
	events    []string
	ssh       []seam.Cmd
	sshCalls  int
	sshResult seam.Result
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

func (f *fixture) deps() seam.Deps {
	return seam.Deps{Dir: f.work,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			f.events = append(f.events, "cloud "+profile+" "+region)
			return cloud.Clients{STS: fixtureSTS{events: &f.events}, EC2: fixtureEC2{events: &f.events, instances: f.instances}}, nil
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path == "git" {
				f.events = append(f.events, "git")
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			}
			f.sshCalls++
			f.ssh = append(f.ssh, command)
			return f.sshResult, nil
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

type fixtureSTS struct {
	cloud.STS
	events *[]string
}

func (f fixtureSTS) CallerAccountID(context.Context) (string, error) {
	*f.events = append(*f.events, "sts")
	return "123", nil
}

type fixtureEC2 struct {
	cloud.EC2
	events    *[]string
	instances []cloud.Instance
}

func (f fixtureEC2) ListSpaceInstances(_ context.Context, domain string) ([]cloud.Instance, error) {
	*f.events = append(*f.events, "list "+domain)
	return append([]cloud.Instance(nil), f.instances...), nil
}

func sshCommand(dir, address, remote string) seam.Cmd {
	return seam.Cmd{Path: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@" + address, remote}, Dir: dir}
}
