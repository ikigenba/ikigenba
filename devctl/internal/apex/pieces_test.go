package apex_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/apex"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

var (
	_ func(context.Context, cloud.Route53, cloud.EC2, cloud.Zone, string, string) (cloud.Space, error) = apex.CheckHolder
	_ func(context.Context, io.Writer, cloud.IAM, string, cloud.Zone, spaceref.Space) error            = apex.GrantApex
	_ func(context.Context, io.Writer, seam.Deps, cloud.IAM, string, cloud.Zone, cloud.Space) error    = apex.ClearPrevious
)

func TestCheckHolder(t *testing.T) {
	// R-F2VH-ZFD3 R-EDSB-23CF R-EF07-FV34 R-EG83-TMTT
	for _, name := range []string{"absent", "target", "unowned", "running", "stopped", "address only", "find failure", "lookup failure"} {
		t.Run(name, func(t *testing.T) {
			f := successfulFake()
			sentinel := errors.New("lookup sentinel")
			switch name {
			case "absent":
				f.recordFound = false
			case "target":
				f.record.Values = []string{"18.118.7.42"}
			case "unowned":
				f.record.Values = []string{"203.0.113.9"}
			case "stopped":
				f.instances[1].State = cloud.StateStopped
			case "address only":
				f.instances[1].State = cloud.StateTerminated
			case "find failure":
				f.findRecordErr = sentinel
			case "lookup failure":
				f.listInstancesErrAt, f.listInstancesErr = 1, sentinel
			}
			got, err := apex.CheckHolder(t.Context(), f, f, cloud.Zone{ID: "ZONE1"}, "ikigenba.dev", "sbx1.ikigenba.dev")
			if len(f.recordQueries) != 1 || !reflect.DeepEqual(f.recordQueries[0], [3]string{"ZONE1", "ikigenba.dev", "A"}) || len(f.changes) != 0 || len(f.policies) != 0 || len(f.ssh) != 0 {
				t.Fatalf("calls: %#v", f)
			}
			switch name {
			case "absent", "target":
				if err != nil || got != (cloud.Space{}) || len(f.instanceRoots) != 0 {
					t.Fatalf("got=%#v err=%v calls=%v", got, err, f.operations)
				}
			case "unowned":
				var e *apex.NotSpaceAddressError
				if !errors.As(err, &e) || e.Root != "ikigenba.dev" || !reflect.DeepEqual(e.Values, []string{"203.0.113.9"}) || len(f.instanceRoots) != 0 {
					t.Fatalf("got=%#v err=%#v", got, err)
				}
			case "running":
				if err != nil || got.Domain != "sbx2.ikigenba.dev" || got.ID != "i-2" || got.Address != "18.220.10.5" || got.State != cloud.StateRunning || len(f.instanceRoots) != 1 {
					t.Fatalf("got=%#v err=%v", got, err)
				}
			case "stopped":
				var e *apex.HolderStoppedError
				if !errors.As(err, &e) || e.Domain != "sbx2.ikigenba.dev" || e.Label != "sbx2" || e.State != cloud.StateStopped {
					t.Fatalf("err=%#v", err)
				}
			case "address only":
				var e *cloud.NoInstanceError
				if !errors.As(err, &e) || e.Domain != "sbx2.ikigenba.dev" {
					t.Fatalf("err=%#v", err)
				}
			default:
				if !errors.Is(err, sentinel) {
					t.Fatalf("err=%v", err)
				}
			}
		})
	}
}

func TestGrantApex(t *testing.T) {
	// R-F2VH-ZFD3 R-F7R3-IIBV
	sp := spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"}
	for _, fail := range []bool{false, true} {
		f := successfulFake()
		sentinel := &cloud.Error{Service: "iam", Operation: "PutRolePolicy", Code: "Throttling"}
		if fail {
			f.putPolicyErrAt, f.putPolicyErr = 1, sentinel
		}
		var out bytes.Buffer
		err := apex.GrantApex(t.Context(), &out, f, "ikigenba.dev", cloud.Zone{ID: "ZONE1"}, sp)
		want := []policyCall{{sp.Domain, space.PolicyName, space.PolicyDocument("ikigenba.dev", "ZONE1", sp, true)}}
		if !reflect.DeepEqual(f.policies, want) {
			t.Fatalf("calls=%#v", f.policies)
		}
		if fail {
			if !errors.Is(err, sentinel) || out.Len() != 0 {
				t.Fatalf("err=%v out=%q", err, out.String())
			}
		} else if err != nil || out.String() != "role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\n" {
			t.Fatalf("err=%v out=%q", err, out.String())
		}
	}
}

func TestClearPrevious(t *testing.T) {
	// R-F2VH-ZFD3 R-FA6W-A1T9 R-FCMP-1LAN
	f := successfulFake()
	deps := testDeps(t, f)
	var out bytes.Buffer
	if err := apex.ClearPrevious(t.Context(), &out, deps, f, "ikigenba.dev", cloud.Zone{ID: "ZONE1"}, cloud.Space{}); err != nil || out.String() != "previous: ok (none)\n" || len(f.policies) != 0 || len(f.ssh) != 0 || f.streamCalls != 0 {
		t.Fatalf("err=%v out=%q calls=%#v", err, out.String(), f)
	}
	for _, stage := range []string{"", "policy", "wait", "config", "cert", "nginx"} {
		t.Run(stage, func(t *testing.T) {
			f := successfulFake()
			deps := testDeps(t, f)
			cause := errors.New("policy sentinel")
			if stage == "policy" {
				f.putPolicyErrAt, f.putPolicyErr = 1, cause
			}
			f.execResult = func(cmd seam.Cmd) (seam.Result, error) {
				if stage == "wait" {
					return seam.Result{ExitCode: 255}, nil
				}
				remote := cmd.Args[len(cmd.Args)-1]
				if stage != "" && strings.Contains(remote, "'"+stage+"'") {
					return seam.Result{ExitCode: 7}, nil
				}
				return seam.Result{}, nil
			}
			var out bytes.Buffer
			err := apex.ClearPrevious(t.Context(), &out, deps, f, "ikigenba.dev", cloud.Zone{ID: "ZONE1"}, cloud.Space{Domain: "sbx2.ikigenba.dev", Address: "18.220.10.5"})
			prev := spaceref.Space{Label: "sbx2", Domain: "sbx2.ikigenba.dev"}
			if !reflect.DeepEqual(f.policies, []policyCall{{prev.Domain, space.PolicyName, space.PolicyDocument("ikigenba.dev", "ZONE1", prev, false)}}) {
				t.Fatalf("policies=%#v", f.policies)
			}
			if stage == "" {
				want := []string{"'true'", "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'opsctl' 'config' 'del' 'host.apex'", "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'opsctl' 'cert' 'obtain'", "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'opsctl' 'nginx' 'apply'"}
				if err != nil || out.String() != "previous: ok (sbx2.ikigenba.dev cleared)\n" || !reflect.DeepEqual(f.ssh, want) {
					t.Fatalf("err=%v out=%q ssh=%v", err, out.String(), f.ssh)
				}
			} else {
				if err == nil || out.Len() != 0 {
					t.Fatalf("err=%v out=%q", err, out.String())
				}
				if stage == "policy" && (!errors.Is(err, cause) || len(f.ssh) != 0) {
					t.Fatalf("err=%v ssh=%v", err, f.ssh)
				}
				if stage == "wait" {
					var e *apex.StepError
					var unreachable *host.UnreachableError
					if !errors.As(err, &e) || e.Step != "previous" || !errors.As(err, &unreachable) || unreachable.Domain != "sbx2.ikigenba.dev" {
						t.Fatalf("err=%#v", err)
					}
				} else if stage != "policy" {
					var e *host.CommandError
					if !errors.As(err, &e) || e.Step != "previous" || len(f.ssh) != map[string]int{"config": 2, "cert": 3, "nginx": 4}[stage] {
						t.Fatalf("err=%v ssh=%v", err, f.ssh)
					}
				}
			}
			for _, target := range f.sshTargets {
				if target != "ec2-user@sbx2.ikigenba.dev" {
					t.Fatal(target)
				}
			}
		})
	}
}
