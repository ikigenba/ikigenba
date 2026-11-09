package cli

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type registryCLI struct {
	*commandHarness
	instances []cloud.Instance
	addresses []cloud.Address
}

func (f *registryCLI) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return f.instances, nil
}
func (f *registryCLI) ListSpaceAddresses(context.Context, string) ([]cloud.Address, error) {
	return f.addresses, nil
}
func (f *registryCLI) StopInstance(context.Context, string) error {
	f.t.Fatal("stopped ambiguous space")
	return nil
}

func TestStopRefusesDuplicateActiveInstances(t *testing.T) {
	// R-43MH-HP6E
	f := &registryCLI{commandHarness: newCommandHarness(t), instances: []cloud.Instance{
		{ID: "i-0f1e2d3c4b5a69788", Space: testDomain, State: cloud.StateRunning},
		{ID: "i-0c9e94542d98846a8", Space: testDomain, State: cloud.StateStopped},
	}}
	deps := f.deps()
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{EC2: f, STS: f}, nil
	}
	assertResult(t, invokeWithDeps(deps, "space", "stop", "sbx1"), 2, "", "devctl: 'sbx1.ikigenba.dev' has more than one active instance (i-0c9e94542d98846a8, i-0f1e2d3c4b5a69788)\n")
}

func TestCommandsRejectAddressWithoutInstance(t *testing.T) {
	// R-NOWN-L0CJ
	for _, args := range [][]string{
		{"space", "stop", "sbx1"}, {"space", "start", "sbx1"}, {"space", "status", "sbx1"},
		{"deploy", "sbx1", "release-87"}, {"rollback", "sbx1"}, {"restore", "sbx1", "crm"},
		{"apex", "set", "crm.sbx1"}, {"golden", "capture", "sbx1", "demo"}, {"seed", "sbx1", "demo"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := &registryCLI{commandHarness: newCommandHarness(t), addresses: []cloud.Address{{Space: testDomain, IP: "18.118.7.42", AllocationID: "eipalloc-01"}}}
			deps := f.deps()
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				return cloud.Clients{STS: f, EC2: f}, nil
			}
			deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
				t.Fatal("streamed on a space without instance")
				return seam.Result{}, nil
			}
			deps.Exec = func(_ context.Context, c seam.Cmd) (seam.Result, error) {
				if c.Path != "git" || !reflect.DeepEqual(c.Args, []string{"rev-parse", "--show-toplevel"}) {
					t.Fatalf("unexpected command: %#v", c)
				}
				return seam.Result{Stdout: []byte(f.checkoutRoot + "\n")}, nil
			}
			deps = withCreateRelease(deps)
			assertResult(t, invokeWithDeps(deps, args...), 1, "", "devctl: 'sbx1.ikigenba.dev' has no instance\n")
		})
	}
}

func TestReplaceRejectsDuplicateAddressesBeforeMutation(t *testing.T) {
	// R-NGDC-WM5O
	f := &registryCLI{commandHarness: newCommandHarness(t), addresses: []cloud.Address{
		{Space: testDomain, IP: "18.190.33.61", AllocationID: "eipalloc-02"},
		{Space: testDomain, IP: "18.118.7.42", AllocationID: "eipalloc-01"},
	}}
	deps := f.deps()
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{EC2: f, STS: f, Route53: f, IAM: f}, nil
	}
	exec := deps.Exec
	deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path != "git" {
			t.Fatalf("external mutation before duplicate refusal: %#v", c)
		}
		return exec(ctx, c)
	}
	deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		t.Fatal("unexpected streaming")
		return seam.Result{}, nil
	}
	assertResult(t, invokeWithDeps(deps, "space", "replace", "sbx1"), 2, "", "devctl: 'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)\n")
	if len(f.mutations) != 0 {
		t.Fatal(f.mutations)
	}
}

func (f *registryCLI) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	f.t.Fatal("unexpected launch")
	return cloud.Instance{}, nil
}
func (f *registryCLI) TerminateInstance(context.Context, string) error {
	f.t.Fatal("unexpected termination")
	return nil
}
func (f *registryCLI) DisassociateAddress(context.Context, string) error {
	f.t.Fatal("unexpected disassociation")
	return nil
}

type destroyAddressCLI struct {
	*registryCLI
	releases []string
	changes  []cloud.RecordChange
}

func (f *destroyAddressCLI) ReleaseAddress(_ context.Context, id string) error {
	f.releases = append(f.releases, id)
	return nil
}
func (f *destroyAddressCLI) RoleExists(context.Context, string) (bool, error) { return true, nil }
func (f *destroyAddressCLI) ListRecords(context.Context, string) ([]cloud.Record, error) {
	return []cloud.Record{{Name: testDomain, Type: "A", TTL: 60, Values: []string{"18.118.7.42"}}, {Name: "*." + testDomain, Type: "A", TTL: 60, Values: []string{"18.118.7.42"}}}, nil
}
func (f *destroyAddressCLI) ChangeRecords(_ context.Context, _ string, changes []cloud.RecordChange) (string, error) {
	f.changes = append(f.changes, changes...)
	return "change", nil
}

func TestDestroyAddressWithoutInstanceThroughCLI(t *testing.T) {
	// R-CMVS-Y1U6
	f := &destroyAddressCLI{registryCLI: &registryCLI{commandHarness: newCommandHarness(t), instances: []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: testDomain, State: cloud.StateTerminated}}, addresses: []cloud.Address{{Space: testDomain, IP: "18.118.7.42", AllocationID: "eipalloc-01"}}}}
	deps := f.deps()
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{EC2: f, STS: f, Route53: f, IAM: f, SSM: f, S3: f}, nil
	}
	exec := deps.Exec
	deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path != "git" {
			t.Fatalf("unexpected host access: %#v", c)
		}
		return exec(ctx, c)
	}
	deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		t.Fatal("unexpected streaming")
		return seam.Result{}, nil
	}
	want := "retire: ok (already gone)\ninstance: ok (already gone)\naddress: ok (elastic ip 18.118.7.42 released)\nrecords: ok (deleted sbx1.ikigenba.dev, *.sbx1.ikigenba.dev)\nsecrets: ok (kept)\nbackups: ok (kept)\nrole: ok (sbx1.ikigenba.dev deleted)\n"
	assertResult(t, invokeWithDeps(deps, "space", "destroy", "sbx1"), 0, want, "")
	if !reflect.DeepEqual(f.releases, []string{"eipalloc-01"}) || len(f.changes) != 2 {
		t.Fatalf("releases %v changes %#v", f.releases, f.changes)
	}
	for _, change := range f.changes {
		if change.Action != cloud.ChangeDelete {
			t.Fatal(change)
		}
	}
}

func TestDestroyRetireFailureThroughCLI(t *testing.T) {
	// R-CGSB-174P
	f := &registryCLI{commandHarness: newCommandHarness(t), instances: []cloud.Instance{{ID: "i-0a1b2c3d4e5f60718", Space: "staging.ikigenba.dev", State: cloud.StateRunning}}}
	deps := f.deps()
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{STS: f, EC2: f, Route53: f}, nil
	}
	exec := deps.Exec
	sshCalls := 0
	deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path == "git" {
			return exec(ctx, c)
		}
		sshCalls++
		if c.Path != "ssh" || c.Args[len(c.Args)-2] != "ec2-user@staging.ikigenba.dev" || c.Args[len(c.Args)-1] != "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'opsctl' 'retire'" {
			t.Fatalf("unexpected retire command %#v", c)
		}
		return seam.Result{ExitCode: 1}, nil
	}
	deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		t.Fatal("unexpected streaming")
		return seam.Result{}, nil
	}
	assertResult(t, invokeWithDeps(deps, "space", "destroy", "staging"), 1, "", "devctl: retire: ssh ec2-user@staging.ikigenba.dev sudo systemd-run --quiet --wait --pipe --collect opsctl retire: exit status 1\n")
	if sshCalls != 1 {
		t.Fatalf("ssh calls %d", sshCalls)
	}
}
