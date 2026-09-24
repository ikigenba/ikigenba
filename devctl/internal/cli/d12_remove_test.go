package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantRemoveUsage = `Usage: devctl remove <space> <app>

Have opsctl on the space take <app> off it: stop and remove its socket and
service, remove its binary and configuration, and stop routing its name. Its
state/ is kept on the host and its secrets are kept in the account, so a later
deploy of <app> lands over its data. What remove does on the host is opsctl's.
`

func TestRemoveHelpAndSyntaxThroughCLI(t *testing.T) {
	// R-JZUS-6SJQ R-HRU7-10TZ
	for _, args := range [][]string{{"remove", "--help"}, {"remove", "-h"}, {"remove", "sbx1", "crm", "--help"}} {
		calls := 0
		deps := seam.Deps{EUID: 1, Dir: t.TempDir(),
			Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				calls++
				return cloud.Clients{}, errors.New("unexpected")
			},
			Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				calls++
				return seam.Result{}, errors.New("unexpected")
			},
		}
		assertResult(t, invokeWithDeps(deps, args...), 0, wantRemoveUsage, "")
		if calls != 0 {
			t.Fatalf("external calls = %d", calls)
		}
	}
	assertResult(t, invokeWithDeps(seam.Deps{EUID: 1, Dir: "/outside"}, "remove", "sbx1"), 2, "", "devctl: remove needs <space> and <app>\n\nsee 'devctl remove --help' for usage\n")
}

func TestCLIDispatchesRemoveAndMapsResolution(t *testing.T) {
	// R-HT23-ESKO R-HU9Z-SKBD R-MTKM-W619
	missing := newD12Fixture(t, nil)
	assertResult(t, invokeWithDeps(missing.deps(), "remove", "gone", "crm"), 1, "", "devctl: no space at 'gone.ikigenba.dev'\n")
	if len(missing.ssh) != 0 {
		t.Fatalf("missing-space ssh = %#v", missing.ssh)
	}

	stopped := newD12Fixture(t, []cloud.Instance{{Space: "sbx2.ikigenba.dev", State: cloud.StateStopped}})
	assertResult(t, invokeWithDeps(stopped.deps(), "remove", "sbx2", "crm"), 1, "", "devctl: 'sbx2.ikigenba.dev' is stopped\n")
	if len(stopped.ssh) != 0 {
		t.Fatalf("stopped-space ssh = %#v", stopped.ssh)
	}
}

func TestCLIRemoveSuccessAndHostFailure(t *testing.T) {
	// R-HWPS-K3SR R-JXEZ-F92C
	instances := []cloud.Instance{{Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}}
	success := newD12Fixture(t, instances)
	success.result = seam.Result{Stdout: []byte("removed\n"), Stderr: []byte("notice\n")}
	assertResult(t, invokeWithDeps(success.deps(), "remove", "sbx1", "crm"), 0, "remove: ok (opsctl uninstalled crm)\n", "")
	want := d12SSH(success.work, "'sudo' 'opsctl' 'uninstall' 'crm'")
	if !reflect.DeepEqual(success.ssh, []seam.Cmd{want}) {
		t.Fatalf("ssh = %#v", success.ssh)
	}

	failure := newD12Fixture(t, instances)
	failure.result = seam.Result{ExitCode: 1, Stdout: []byte("not installed\nmore output\n"), Stderr: []byte("uninstall failed\nmore error\n")}
	assertResult(t, invokeWithDeps(failure.deps(), "remove", "sbx1", "gmail"), 1, "", "devctl: remove: ssh ec2-user@18.118.7.42 sudo opsctl uninstall gmail: exit status 1\n\n> not installed\n> more output\n> uninstall failed\n> more error\n")
}

type d12Fixture struct {
	root, work string
	instances  []cloud.Instance
	ssh        []seam.Cmd
	result     seam.Result
}

func newD12Fixture(t *testing.T, instances []cloud.Instance) *d12Fixture {
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
	return &d12Fixture{root: root, work: work, instances: instances}
}

func (f *d12Fixture) deps() seam.Deps {
	return seam.Deps{EUID: 1, Dir: f.work,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			return cloud.Clients{STS: d12STS{}, EC2: &d12EC2{instances: f.instances}}, nil
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path == "git" {
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			}
			f.ssh = append(f.ssh, command)
			return f.result, nil
		},
	}
}

type d12STS struct{ cloud.STS }

func (d12STS) CallerAccountID(context.Context) (string, error) { return "123", nil }

type d12EC2 struct {
	cloud.EC2
	instances []cloud.Instance
}

func (f *d12EC2) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return append([]cloud.Instance(nil), f.instances...), nil
}
func d12SSH(dir, remote string) seam.Cmd {
	return seam.Cmd{Path: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@18.118.7.42", remote}, Dir: dir}
}
