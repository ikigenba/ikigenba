package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/rollback"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const expectedRollbackUsage = `Usage: devctl rollback <space>

Have opsctl on the space activate the release it ran before the current one
again: current points at previous, previous is removed, and every app is
restarted. With no previous release opsctl refuses, so a second rollback is
refused until the next deploy. What rollback does on the host is opsctl's.
`

func TestRollbackPublicContract(t *testing.T) {
	// R-X0CS-DA78
	acceptRollbackRun(rollback.Run)
	e := &rollback.UsageError{Message: "message", Help: "help"}
	if e.Error() != "message" || e.Detail() != "see 'help' for usage" || e.ExitCode() != 2 {
		t.Fatal("rollback contract")
	}
	if (&rollback.UsageError{}).Detail() != "" {
		t.Fatal("empty help detail")
	}
}

func acceptRollbackRun(func(context.Context, []string, io.Writer, seam.Deps) error) {}

func TestRollbackUsageBeforeExternalAccess(t *testing.T) {
	// R-X2SL-4TOM R-X40H-ILFB
	for _, test := range []struct {
		args     []string
		code     int
		out, err string
	}{
		{[]string{"rollback"}, 2, "", "devctl: rollback needs <space>\n\nsee 'devctl rollback --help' for usage\n"},
		{[]string{"rollback", "sbx1", "sbx2"}, 2, "", "devctl: rollback takes only <space>\n\nsee 'devctl rollback --help' for usage\n"},
		{[]string{"rollback", "sbx1", "--now"}, 2, "", "devctl: unknown option '--now'\n\nsee 'devctl rollback --help' for usage\n"},
		{[]string{"rollback", "--help"}, 0, expectedRollbackUsage, ""},
		{[]string{"rollback", "-h"}, 0, expectedRollbackUsage, ""},
	} {
		calls := 0
		deps := noExternalDeps(&calls)
		deps.Dir = t.TempDir()
		assertResult(t, invokeWithDeps(deps, test.args...), test.code, test.out, test.err)
		if calls != 0 {
			t.Fatalf("external calls %d", calls)
		}
	}
}

func TestRollbackLookupRefusals(t *testing.T) {
	// R-X58D-WD60
	for _, stopped := range []bool{false, true} {
		h := newD11Harness(t)
		operand := "gone"
		want := "devctl: no space at 'gone.ikigenba.dev'\n"
		if stopped {
			operand = "sbx2"
			h.instances[0].State = cloud.StateStopped
			want = "devctl: 'sbx2.ikigenba.dev' is stopped\n"
		}
		assertResult(t, invokeWithDeps(h.deps(), "rollback", operand), 1, "", want)
		if h.sshCalls != 0 || h.streamCalls != 0 {
			t.Fatal("host reached before running check")
		}
	}
}

func TestRollbackDispatchStreamsOnlyHostOutput(t *testing.T) {
	// R-X1KO-R1XX R-X6GA-A4WP R-X7O6-NWNE
	for _, failed := range []bool{false, true} {
		h := newD11Harness(t)
		deps := h.deps()
		strict := &rollbackCloudFake{t: t, instances: h.instances}
		deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
			if profile != "ikigenba.dev" || region != "us-east-2" {
				t.Fatalf("cloud profile=%s region=%s", profile, region)
			}
			return cloud.Clients{STS: strict, EC2: strict}, nil
		}
		base := deps.Exec
		git, streams := 0, 0
		deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
			if cmd.Path != "git" || !slices.Equal(cmd.Args, []string{"rev-parse", "--show-toplevel"}) {
				t.Fatalf("unexpected Exec %#v", cmd)
			}
			git++
			return base(ctx, cmd)
		}
		deps.Stream = func(_ context.Context, cmd seam.Cmd, out io.Writer) (seam.Result, error) {
			streams++
			if cmd.Path != "ssh" || cmd.Args[len(cmd.Args)-2] != "ec2-user@18.118.7.42" || cmd.Args[len(cmd.Args)-1] != "'sudo' 'opsctl' 'rollback'" {
				t.Fatalf("command %#v", cmd)
			}
			if failed {
				return seam.Result{ExitCode: 1, Stderr: []byte("opsctl: no previous release to roll back to")}, nil
			}
			_, err := io.WriteString(out, "arbitrary\n\nrollback output\n")
			return seam.Result{}, err
		}
		if failed {
			assertResult(t, invokeWithDeps(deps, "rollback", "sbx1"), 1, "", "devctl: rollback: ssh ec2-user@18.118.7.42 sudo opsctl rollback: exit status 1\n\n> opsctl: no previous release to roll back to\n")
		} else {
			assertResult(t, invokeWithDeps(deps, "rollback", "sbx1"), 0, "arbitrary\n\nrollback output\n", "")
		}
		if strict.stsCalls != 1 || strict.ec2Calls != 1 {
			t.Fatalf("sts=%d ec2=%d", strict.stsCalls, strict.ec2Calls)
		}
		if git != 1 || streams != 1 {
			t.Fatalf("git=%d streams=%d", git, streams)
		}
	}
}

func TestRollbackReturnsRootFailureUnchanged(t *testing.T) {
	// R-X58D-WD60
	sentinel := errors.New("git failed")
	deps := noExternalDeps(new(int))
	deps.Exec = func(context.Context, seam.Cmd) (seam.Result, error) { return seam.Result{}, sentinel }
	err := rollback.Run(t.Context(), []string{"sbx1"}, io.Discard, deps)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error %v", err)
	}
}

func TestChangedRootPreflightsThroughCLI(t *testing.T) {
	// R-XBBV-T7VH R-XA3Z-FG4S
	for _, args := range [][]string{{"space", "stop", "sbx1"}, {"apex", "show"}} {
		deps := checkoutDeps(t, `{"domain":"example.test","region":"eu-west-1"}`)
		calls := 0
		deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
			calls++
			if profile != "example.test" || region != "eu-west-1" {
				t.Fatalf("cloud %s %s", profile, region)
			}
			return cloud.Clients{}, errors.New("cloud sentinel")
		}
		assertResult(t, invokeWithDeps(deps, args...), 1, "", "devctl: cloud sentinel\n")
		if calls != 1 {
			t.Fatalf("cloud calls %d", calls)
		}
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"space", "status", "crm.sbx1"}, "devctl: 'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'\n"},
		{[]string{"apex", "set", "crm.sbx1.example.com"}, "devctl: 'crm.sbx1.example.com' is not an app on a space: <app>.<space>\n"},
	} {
		deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("cloud after invalid operand")
			return cloud.Clients{}, nil
		}
		assertResult(t, invokeWithDeps(deps, test.args...), 2, "", test.want)
	}
}

// All other EC2 and STS methods, and all other cloud services, are nil and
// fail immediately if rollback invokes an operation outside its contract.
type rollbackCloudFake struct {
	cloud.EC2
	cloud.STS
	t                  *testing.T
	instances          []cloud.Instance
	stsErr, ec2Err     error
	stsCalls, ec2Calls int
}

func (f *rollbackCloudFake) CallerAccountID(context.Context) (string, error) {
	f.stsCalls++
	return "123", f.stsErr
}
func (f *rollbackCloudFake) ListSpaceInstances(_ context.Context, root string) ([]cloud.Instance, error) {
	if root != "ikigenba.dev" {
		f.t.Fatalf("lookup root=%s", root)
	}
	f.ec2Calls++
	return f.instances, f.ec2Err
}

func TestRollbackReturnsCloudFailuresUnchangedBeforeHost(t *testing.T) {
	// R-X58D-WD60
	for _, step := range []string{"connect", "sts", "lookup"} {
		t.Run(step, func(t *testing.T) {
			sentinel := errors.New(step + " sentinel")
			deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
			strict := &rollbackCloudFake{t: t}
			if step == "sts" {
				strict.stsErr = sentinel
			}
			if step == "lookup" {
				strict.ec2Err = sentinel
			}
			opens := 0
			deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
				opens++
				if profile != "ikigenba.dev" || region != "us-east-2" {
					t.Fatalf("cloud profile=%s region=%s", profile, region)
				}
				if step == "connect" {
					return cloud.Clients{}, sentinel
				}
				return cloud.Clients{STS: strict, EC2: strict}, nil
			}
			exec := deps.Exec
			git := 0
			deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
				if cmd.Path != "git" || !slices.Equal(cmd.Args, []string{"rev-parse", "--show-toplevel"}) {
					t.Fatalf("unexpected exec %#v", cmd)
				}
				git++
				return exec(ctx, cmd)
			}
			deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
				t.Fatal("stream after preflight failure")
				return seam.Result{}, nil
			}
			var stdout bytes.Buffer
			err := rollback.Run(t.Context(), []string{"sbx1"}, &stdout, deps)
			if reflect.ValueOf(err) != reflect.ValueOf(sentinel) || stdout.Len() != 0 || opens != 1 || git != 1 {
				t.Fatalf("err=%v output=%q opens=%d git=%d", err, stdout.String(), opens, git)
			}
			sts, ec2 := 1, 0
			if step == "connect" {
				sts = 0
			}
			if step == "lookup" {
				ec2 = 1
			}
			if strict.stsCalls != sts || strict.ec2Calls != ec2 {
				t.Fatalf("sts=%d ec2=%d", strict.stsCalls, strict.ec2Calls)
			}
		})
	}
}
