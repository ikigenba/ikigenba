package apex_test

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
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/apex"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

func TestErrorContracts(t *testing.T) {
	t.Parallel()

	// R-4282-JQL9 R-43FY-XIBY R-44NV-BA2N R-45VR-P1TC
	holder := &apex.HolderStoppedError{Domain: "sbx2.ikigenba.dev", Label: "sbx2", State: cloud.StateStopped}
	assertFields(t, holder, []string{"Domain", "Label", "State"})
	if holder.Error() != "'sbx2.ikigenba.dev' holds the apex and is stopped" || holder.ExitCode() != 1 || holder.Detail() != "run 'devctl space start sbx2' first" {
		t.Fatalf("HolderStoppedError = %q, %d, %q", holder.Error(), holder.ExitCode(), holder.Detail())
	}
	address := &apex.NotSpaceAddressError{Root: "ikigenba.dev", Values: []string{"203.0.113.9"}}
	assertFields(t, address, []string{"Root", "Values"})
	if got := address.Error(); got != "ikigenba.dev points at 203.0.113.9, which is not a space's address" {
		t.Errorf("NotSpaceAddressError = %q", got)
	}
	noApp := &apex.NoApexAppError{Root: "ikigenba.dev", Domain: "sbx1.ikigenba.dev"}
	assertFields(t, noApp, []string{"Root", "Domain"})
	if got := noApp.Error(); got != "ikigenba.dev points at sbx1.ikigenba.dev, whose host.apex is not set" {
		t.Errorf("NoApexAppError = %q", got)
	}
	step := &apex.StepError{Step: "previous", Err: &host.UnreachableError{Address: "18.220.10.5"}}
	assertFields(t, step, []string{"Step", "Err"})
	if step.Error() != "previous: ssh ec2-user@18.220.10.5: connection timed out" || step.ExitCode() != 1 || step.Detail() != "" {
		t.Errorf("StepError = %q, %d, %q", step.Error(), step.ExitCode(), step.Detail())
	}
	if !errors.Is(step, step.Err) {
		t.Error("StepError does not unwrap its Err")
	}
}

func TestGrammarAndHelpAreSideEffectFree(t *testing.T) {
	t.Parallel()

	// R-Q4D0-4OUV R-4ARD-84S4
	const help = "Usage: devctl apex <subcommand> [arguments]\n\nPoint the root domain at one app on one space, say where it points, or take\nit away. The root is an A record at the space's address; the space's host\ncarries the root in its certificate and routes it to the app.\n\nSubcommands:\n  set <app>.<space>   make <app> on <space> answer at the root\n  show                print the app and address the root points at\n  clear               remove the root's record and the holder's apex configuration\n\nRun 'devctl apex <subcommand> --help' for details.\n"
	for _, args := range [][]string{{"--help"}, {"set", "--help"}, {"show", "-h"}, {"bad", "--help"}} {
		var stdout bytes.Buffer
		calls := 0
		err := apex.Run(context.Background(), args, &stdout, seam.Deps{
			Dir:    "/does/not/exist",
			Exec:   func(context.Context, seam.Cmd) (seam.Result, error) { calls++; return seam.Result{}, nil },
			Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) { calls++; return seam.Result{}, nil },
			Cloud:  func(context.Context, string, string) (cloud.Clients, error) { calls++; return cloud.Clients{}, nil },
		})
		if err != nil || stdout.String() != help || calls != 0 {
			t.Errorf("Run(%q) = output %q, err %v, calls %d", args, stdout.String(), err, calls)
		}
	}
	cases := []struct {
		args    []string
		message string
	}{
		{nil, "apex needs <subcommand>"},
		{[]string{"wat"}, "unknown subcommand 'wat'"},
		{[]string{"--wat"}, "unknown option '--wat'"},
		{[]string{"set"}, "apex set needs <app>.<space>"},
		{[]string{"set", "crm.sbx1", "extra"}, "apex set takes only <app>.<space>"},
		{[]string{"show", "sbx1"}, "apex show takes no arguments"},
		{[]string{"clear", "sbx1"}, "apex clear takes no arguments"},
	}
	for _, tc := range cases {
		var stdout bytes.Buffer
		calls := 0
		err := apex.Run(context.Background(), tc.args, &stdout, seam.Deps{
			Exec:  func(context.Context, seam.Cmd) (seam.Result, error) { calls++; return seam.Result{}, nil },
			Cloud: func(context.Context, string, string) (cloud.Clients, error) { calls++; return cloud.Clients{}, nil },
		})
		var usage *space.UsageError
		if !errors.As(err, &usage) || usage.Message != tc.message || usage.Help != "devctl apex --help" || stdout.Len() != 0 || calls != 0 {
			t.Errorf("Run(%q) = output %q, err %#v, calls %d", tc.args, stdout.String(), err, calls)
		}
	}
}

func TestSetRunsPreflightAndOrderedSteps(t *testing.T) {
	// R-4D75-ZO9I R-4FMY-R7QW R-4GUV-4ZHL R-4I2R-IR8A R-VX21-6EX8
	// R-4KIK-AAPO R-4LQG-O2GD R-VY9X-K6NX R-4O69-FLXR
	f := successfulFake()
	deps := testDeps(t, f)
	var stdout bytes.Buffer
	if err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, deps); err != nil {
		t.Fatalf("Run set: %v", err)
	}
	want := "space: ok (sbx1.ikigenba.dev running, 18.118.7.42)\n" +
		"role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\n" +
		"host: ok (host.apex=crm, certificate obtained, nginx applied)\n" +
		"record: ok (ikigenba.dev -> 18.118.7.42, INSYNC)\n" +
		"previous: ok (sbx2.ikigenba.dev cleared)\n" +
		"ikigenba.dev crm.sbx1.ikigenba.dev\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if f.profile != "ikigenba.dev" || f.region != "us-east-2" {
		t.Errorf("cloud connect = %q, %q", f.profile, f.region)
	}
	if len(f.policies) != 2 || f.policies[0].role != "sbx1.ikigenba.dev" || f.policies[1].role != "sbx2.ikigenba.dev" {
		t.Errorf("policies = %#v", f.policies)
	}
	wantTargetPolicy := space.PolicyDocument("ikigenba.dev", "ZONE1", spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"}, true)
	wantPreviousPolicy := space.PolicyDocument("ikigenba.dev", "ZONE1", spaceref.Space{Label: "sbx2", Domain: "sbx2.ikigenba.dev"}, false)
	if f.policies[0].policy != space.PolicyName || f.policies[0].document != wantTargetPolicy || f.policies[1].policy != space.PolicyName || f.policies[1].document != wantPreviousPolicy {
		t.Errorf("policy contents = %#v", f.policies)
	}
	if len(f.changes) != 1 || len(f.changes[0]) != 1 || f.changes[0][0].Action != cloud.ChangeUpsert || !reflect.DeepEqual(f.changes[0][0].Record.Values, []string{"18.118.7.42"}) {
		t.Errorf("changes = %#v", f.changes)
	}
	wantSSH := []string{
		"'true'",
		"'sudo' 'opsctl' 'config' 'set' 'host.apex=crm'",
		"'sudo' 'opsctl' 'cert' 'obtain'",
		"'sudo' 'opsctl' 'nginx' 'apply'",
		"'true'",
		"'sudo' 'opsctl' 'config' 'del' 'host.apex'",
		"'sudo' 'opsctl' 'cert' 'obtain'",
		"'sudo' 'opsctl' 'nginx' 'apply'",
	}
	if !reflect.DeepEqual(f.ssh, wantSSH) {
		t.Errorf("ssh commands = %#v, want %#v", f.ssh, wantSSH)
	}
	if f.streamCalls != 0 || f.ssmCalls != 0 || f.s3Calls != 0 {
		t.Errorf("forbidden calls: stream=%d ssm=%d s3=%d", f.streamCalls, f.ssmCalls, f.s3Calls)
	}
}

func TestSetPreflightRefusesBeforeOutputOrMutation(t *testing.T) {
	// R-4FMY-R7QW
	tests := []struct {
		name string
		edit func(*fake)
		want any
	}{
		{"target stopped", func(f *fake) { f.instances[0].State = cloud.StateStopped }, &space.NotRunningError{}},
		{"target missing", func(f *fake) { f.instances = f.instances[1:] }, &cloud.NoSpaceError{}},
		{"unowned address", func(f *fake) { f.addresses = nil }, &apex.NotSpaceAddressError{}},
		{"holder stopped", func(f *fake) { f.instances[1].State = cloud.StateStopped }, &apex.HolderStoppedError{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := successfulFake()
			tc.edit(f)
			var stdout bytes.Buffer
			err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
			if reflect.TypeOf(err) != reflect.TypeOf(tc.want) || stdout.Len() != 0 || len(f.policies) != 0 || len(f.changes) != 0 || len(f.ssh) != 0 {
				t.Errorf("err=%T %v stdout=%q policies=%d changes=%d ssh=%d", err, err, stdout.String(), len(f.policies), len(f.changes), len(f.ssh))
			}
		})
	}
}

func TestAcceptedInvocationFailureOrderAndPropagation(t *testing.T) {
	// R-4D75-ZO9I R-4FMY-R7QW
	t.Run("root discovery", func(t *testing.T) {
		want := errors.New("git unavailable")
		cloudCalls := 0
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"show"}, &stdout, seam.Deps{
			Dir: t.TempDir(),
			Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				return seam.Result{}, want
			},
			Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				cloudCalls++
				return cloud.Clients{}, nil
			},
		})
		if !errors.Is(err, want) || err.Error() != "git rev-parse --show-toplevel: git unavailable" || stdout.Len() != 0 || cloudCalls != 0 {
			t.Fatalf("err=%v stdout=%q cloud calls=%d", err, stdout.String(), cloudCalls)
		}
	})

	t.Run("operand parse", func(t *testing.T) {
		f := successfulFake()
		deps := testDeps(t, f)
		cloudCalls := 0
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, nil
		}
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1.example.com"}, &stdout, deps)
		var target *spaceref.NotAnAppError
		if !errors.As(err, &target) || target.Operand != "crm.sbx1.example.com" || err.Error() != "'crm.sbx1.example.com' is not an app on a space: <app>.<space>" || stdout.Len() != 0 || cloudCalls != 0 {
			t.Fatalf("err=%#v stdout=%q cloud calls=%d", err, stdout.String(), cloudCalls)
		}
	})

	t.Run("connect", func(t *testing.T) {
		f := successfulFake()
		deps := testDeps(t, f)
		want := errors.New("connect failed")
		calls := 0
		deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
			calls++
			if profile != "ikigenba.dev" || region != "us-east-2" {
				t.Fatalf("Cloud(%q, %q)", profile, region)
			}
			return cloud.Clients{}, want
		}
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"clear"}, &stdout, deps)
		if !errors.Is(err, want) || errors.Unwrap(err) != nil || stdout.Len() != 0 || calls != 1 || len(f.operations) != 0 {
			t.Fatalf("err=%v stdout=%q calls=%d operations=%v", err, stdout.String(), calls, f.operations)
		}
	})

	for _, tc := range []struct {
		name string
		edit func(*fake, error)
	}{
		{"target lookup", func(f *fake, want error) { f.listInstancesErrAt, f.listInstancesErr = 1, want }},
		{"zone", func(f *fake, want error) { f.zoneErr = want }},
		{"record", func(f *fake, want error) { f.findRecordErr = want }},
		{"address lookup", func(f *fake, want error) { f.listAddressesErr = want }},
		{"holder lookup", func(f *fake, want error) { f.listInstancesErrAt, f.listInstancesErr = 2, want }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := successfulFake()
			want := errors.New(tc.name + " failed")
			tc.edit(f, want)
			var stdout bytes.Buffer
			err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
			if !errors.Is(err, want) || errors.Unwrap(err) != nil || stdout.Len() != 0 || len(f.policies) != 0 || len(f.changes) != 0 || len(f.ssh) != 0 {
				t.Fatalf("err=%v stdout=%q policies=%v changes=%v ssh=%v operations=%v", err, stdout.String(), f.policies, f.changes, f.ssh, f.operations)
			}
		})
	}

	t.Run("exact refusal fields", func(t *testing.T) {
		f := successfulFake()
		f.instances[0].State = cloud.StateStopping
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, io.Discard, testDeps(t, f))
		var stopped *space.NotRunningError
		if !errors.As(err, &stopped) || stopped.Domain != "sbx1.ikigenba.dev" || stopped.State != cloud.StateStopping {
			t.Fatalf("err=%#v", err)
		}

		f = successfulFake()
		f.addresses = nil
		err = apex.Run(context.Background(), []string{"set", "crm.sbx1"}, io.Discard, testDeps(t, f))
		var address *apex.NotSpaceAddressError
		if !errors.As(err, &address) || address.Root != "ikigenba.dev" || !reflect.DeepEqual(address.Values, []string{"18.220.10.5"}) {
			t.Fatalf("err=%#v", err)
		}

		f = successfulFake()
		f.instances[1].State = cloud.StateStopped
		err = apex.Run(context.Background(), []string{"set", "crm.sbx1"}, io.Discard, testDeps(t, f))
		var holder *apex.HolderStoppedError
		if !errors.As(err, &holder) || holder.Domain != "sbx2.ikigenba.dev" || holder.Label != "sbx2" || holder.State != cloud.StateStopped {
			t.Fatalf("err=%#v", err)
		}
	})
}

func TestSetFailureBoundariesAndExactCalls(t *testing.T) {
	// R-4GUV-4ZHL R-VX21-6EX8 R-4KIK-AAPO R-4LQG-O2GD R-VY9X-K6NX R-4O69-FLXR
	const spaceLine = "space: ok (sbx1.ikigenba.dev running, 18.118.7.42)\n"
	const roleLine = "role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\n"
	const hostLine = "host: ok (host.apex=crm, certificate obtained, nginx applied)\n"
	const recordLine = "record: ok (ikigenba.dev -> 18.118.7.42, INSYNC)\n"

	t.Run("role", func(t *testing.T) {
		f := successfulFake()
		want := &cloud.Error{Service: "iam", Operation: "PutRolePolicy", Code: "Throttling"}
		f.putPolicyErrAt, f.putPolicyErr = 1, want
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
		wantCall := policyCall{"sbx1.ikigenba.dev", space.PolicyName, space.PolicyDocument("ikigenba.dev", "ZONE1", spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"}, true)}
		if !errors.Is(err, want) || errors.Unwrap(err) != nil || stdout.String() != spaceLine || !reflect.DeepEqual(f.policies, []policyCall{wantCall}) || len(f.ssh) != 0 || len(f.changes) != 0 {
			t.Fatalf("err=%v stdout=%q policies=%#v ssh=%v changes=%v", err, stdout.String(), f.policies, f.ssh, f.changes)
		}
	})

	t.Run("host command", func(t *testing.T) {
		f := successfulFake()
		f.execResult = func(cmd seam.Cmd) (seam.Result, error) {
			if strings.Contains(cmd.Args[len(cmd.Args)-1], "'cert' 'obtain'") {
				return seam.Result{ExitCode: 1, Stderr: []byte("first\nsecond\n")}, nil
			}
			return seam.Result{}, nil
		}
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
		var command *host.CommandError
		wantSSH := []string{"'true'", "'sudo' 'opsctl' 'config' 'set' 'host.apex=crm'", "'sudo' 'opsctl' 'cert' 'obtain'"}
		if !errors.As(err, &command) || command.Step != "host" || command.Status != 1 || command.Stderr != "first\nsecond\n" || stdout.String() != spaceLine+roleLine || !reflect.DeepEqual(f.ssh, wantSSH) || !reflect.DeepEqual(f.sshTargets, []string{"ec2-user@18.118.7.42", "ec2-user@18.118.7.42", "ec2-user@18.118.7.42"}) || len(f.changes) != 0 {
			t.Fatalf("err=%#v stdout=%q ssh=%v targets=%v changes=%v", err, stdout.String(), f.ssh, f.sshTargets, f.changes)
		}
	})

	t.Run("host wait", func(t *testing.T) {
		f := successfulFake()
		f.execResult = func(seam.Cmd) (seam.Result, error) { return seam.Result{ExitCode: 255}, nil }
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
		var step *apex.StepError
		var unreachable *host.UnreachableError
		if !errors.As(err, &step) || step.Step != "host" || !errors.As(err, &unreachable) || unreachable.Address != "18.118.7.42" || stdout.String() != spaceLine+roleLine || len(f.ssh) != host.ProbeAttempts || len(f.changes) != 0 {
			t.Fatalf("err=%#v stdout=%q ssh=%v changes=%v", err, stdout.String(), f.ssh, f.changes)
		}
		for _, command := range f.ssh {
			if command != "'true'" {
				t.Fatalf("command after failed wait: %q", command)
			}
		}
	})

	for _, tc := range []struct {
		name       string
		failure    string
		wantStatus int
	}{
		{"set key", "'config' 'set'", 7},
		{"nginx", "'nginx' 'apply'", 8},
	} {
		t.Run("host "+tc.name+" short circuit", func(t *testing.T) {
			f := successfulFake()
			f.execResult = func(cmd seam.Cmd) (seam.Result, error) {
				if strings.Contains(cmd.Args[len(cmd.Args)-1], tc.failure) {
					return seam.Result{ExitCode: tc.wantStatus}, nil
				}
				return seam.Result{}, nil
			}
			var stdout bytes.Buffer
			err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
			var command *host.CommandError
			if !errors.As(err, &command) || command.Step != "host" || command.Status != tc.wantStatus || stdout.String() != spaceLine+roleLine || len(f.changes) != 0 {
				t.Fatalf("err=%#v stdout=%q ssh=%v changes=%v", err, stdout.String(), f.ssh, f.changes)
			}
			if tc.name == "set key" && len(f.ssh) != 2 {
				t.Fatalf("ssh after set failure = %v", f.ssh)
			}
			if tc.name == "nginx" && len(f.ssh) != 4 {
				t.Fatalf("ssh through nginx failure = %v", f.ssh)
			}
		})
	}

	t.Run("record change", func(t *testing.T) {
		f := successfulFake()
		want := &cloud.Error{Service: "route53", Operation: "ChangeResourceRecordSets", Code: "Throttling"}
		f.changeErr = want
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
		wantRecord := cloud.Record{Name: "ikigenba.dev", Type: "A", TTL: space.RecordTTL, Values: []string{"18.118.7.42"}}
		if !errors.Is(err, want) || errors.Unwrap(err) != nil || stdout.String() != spaceLine+roleLine+hostLine || !reflect.DeepEqual(f.changeZones, []string{"ZONE1"}) || len(f.changes) != 1 || !reflect.DeepEqual(f.changes[0], []cloud.RecordChange{{Action: cloud.ChangeUpsert, Record: wantRecord}}) || f.changeStatusCalls != 0 || len(f.policies) != 1 {
			t.Fatalf("err=%v stdout=%q zones=%v changes=%#v status=%d policies=%v", err, stdout.String(), f.changeZones, f.changes, f.changeStatusCalls, f.policies)
		}
	})

	t.Run("record wait", func(t *testing.T) {
		f := successfulFake()
		want := errors.New("status failed")
		f.changeStatusErr = want
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
		if !errors.Is(err, want) || errors.Unwrap(err) != nil || stdout.String() != spaceLine+roleLine+hostLine || !reflect.DeepEqual(f.changeStatusIDs, []string{"CHANGE1"}) || len(f.policies) != 1 {
			t.Fatalf("err=%v stdout=%q status ids=%v policies=%v", err, stdout.String(), f.changeStatusIDs, f.policies)
		}
	})

	t.Run("previous policy", func(t *testing.T) {
		f := successfulFake()
		want := errors.New("previous policy failed")
		f.putPolicyErrAt, f.putPolicyErr = 2, want
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
		wantPrev := policyCall{"sbx2.ikigenba.dev", space.PolicyName, space.PolicyDocument("ikigenba.dev", "ZONE1", spaceref.Space{Label: "sbx2", Domain: "sbx2.ikigenba.dev"}, false)}
		if !errors.Is(err, want) || errors.Unwrap(err) != nil || stdout.String() != spaceLine+roleLine+hostLine+recordLine || len(f.policies) != 2 || f.policies[1] != wantPrev || len(f.ssh) != 4 {
			t.Fatalf("err=%v stdout=%q policies=%#v ssh=%v", err, stdout.String(), f.policies, f.ssh)
		}
	})

	t.Run("previous timeout", func(t *testing.T) {
		f := successfulFake()
		f.execResult = func(cmd seam.Cmd) (seam.Result, error) {
			if cmd.Args[len(cmd.Args)-2] == "ec2-user@18.220.10.5" {
				return seam.Result{ExitCode: 255}, nil
			}
			return seam.Result{}, nil
		}
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"set", "crm.sbx1"}, &stdout, testDeps(t, f))
		var step *apex.StepError
		var unreachable *host.UnreachableError
		oldCommands := 0
		for i, target := range f.sshTargets {
			if target == "ec2-user@18.220.10.5" {
				oldCommands++
				if f.ssh[i] != "'true'" {
					t.Fatalf("old host command %q", f.ssh[i])
				}
			}
		}
		if !errors.As(err, &step) || step.Step != "previous" || !errors.As(err, &unreachable) || unreachable.Address != "18.220.10.5" || stdout.String() != spaceLine+roleLine+hostLine+recordLine || oldCommands != host.ProbeAttempts {
			t.Fatalf("err=%#v stdout=%q old commands=%d ssh=%v", err, stdout.String(), oldCommands, f.ssh)
		}
	})

	for _, tc := range []struct {
		name string
		edit func(*fake)
	}{
		{"already target", func(f *fake) { f.record.Values = []string{"18.118.7.42"} }},
		{"no record", func(f *fake) { f.recordFound = false }},
	} {
		t.Run(tc.name+" reruns every target step", func(t *testing.T) {
			f := successfulFake()
			tc.edit(f)
			var stdout bytes.Buffer
			if err := apex.Run(context.Background(), []string{"set", "crm.sbx1.ikigenba.dev"}, &stdout, testDeps(t, f)); err != nil {
				t.Fatal(err)
			}
			want := spaceLine + roleLine + hostLine + recordLine + "previous: ok (none)\nikigenba.dev crm.sbx1.ikigenba.dev\n"
			wantSSH := []string{"'true'", "'sudo' 'opsctl' 'config' 'set' 'host.apex=crm'", "'sudo' 'opsctl' 'cert' 'obtain'", "'sudo' 'opsctl' 'nginx' 'apply'"}
			if stdout.String() != want || len(f.policies) != 1 || len(f.changes) != 1 || f.changeStatusCalls != 1 || !reflect.DeepEqual(f.ssh, wantSSH) || f.streamCalls != 0 || f.ssmCalls != 0 || f.s3Calls != 0 {
				t.Fatalf("stdout=%q policies=%v changes=%v status=%d ssh=%v forbidden=%d/%d/%d", stdout.String(), f.policies, f.changes, f.changeStatusCalls, f.ssh, f.streamCalls, f.ssmCalls, f.s3Calls)
			}
		})
	}
}

func TestShowAndClear(t *testing.T) {
	// R-4PE5-TDOG R-4QM2-75F5 R-4RTY-KX5U R-4T1U-YOWJ R-4U9R-CGN8
	// R-4VHN-Q8DX R-VZHT-XYEM R-4XXG-HRVB R-50D9-9BCP
	t.Run("show", func(t *testing.T) {
		f := successfulFake()
		f.getValue = "crm\n"
		var stdout bytes.Buffer
		if err := apex.Run(context.Background(), []string{"show"}, &stdout, testDeps(t, f)); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != "crm.sbx2.ikigenba.dev 18.220.10.5\n" || len(f.policies) != 0 || len(f.changes) != 0 {
			t.Errorf("stdout=%q policies=%d changes=%d", stdout.String(), len(f.policies), len(f.changes))
		}
	})
	t.Run("show unset", func(t *testing.T) {
		f := successfulFake()
		f.getUnset = true
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"show"}, &stdout, testDeps(t, f))
		var target *apex.NoApexAppError
		if !errors.As(err, &target) || stdout.Len() != 0 {
			t.Errorf("err=%v stdout=%q", err, stdout.String())
		}
	})
	t.Run("show absent, unowned, and stopped", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			edit func(*fake)
			want any
		}{
			{"absent", func(f *fake) { f.recordFound = false }, nil},
			{"unowned", func(f *fake) { f.addresses = nil }, &apex.NotSpaceAddressError{}},
			{"stopped", func(f *fake) { f.instances[1].State = cloud.StateStopped }, &space.NotRunningError{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := successfulFake()
				tc.edit(f)
				var stdout bytes.Buffer
				err := apex.Run(context.Background(), []string{"show"}, &stdout, testDeps(t, f))
				if reflect.TypeOf(err) != reflect.TypeOf(tc.want) || stdout.Len() != 0 || len(f.ssh) != 0 || len(f.changes) != 0 || len(f.policies) != 0 {
					t.Errorf("err=%T %v stdout=%q ssh=%d changes=%d policies=%d", err, err, stdout.String(), len(f.ssh), len(f.changes), len(f.policies))
				}
			})
		}
	})
	t.Run("clear", func(t *testing.T) {
		f := successfulFake()
		var stdout bytes.Buffer
		if err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f)); err != nil {
			t.Fatal(err)
		}
		want := "space: ok (sbx2.ikigenba.dev running, 18.220.10.5)\nrecord: ok (ikigenba.dev deleted)\nrole: ok (sbx2.ikigenba.dev may no longer prove ikigenba.dev)\nhost: ok (host.apex removed, certificate obtained, nginx applied)\n"
		if stdout.String() != want || len(f.changes) != 1 || f.changes[0][0].Action != cloud.ChangeDelete || !reflect.DeepEqual(f.changes[0][0].Record, f.record) || f.changeStatusCalls != 0 || len(f.policies) != 1 {
			t.Errorf("stdout=%q changes=%#v status=%d policies=%#v", stdout.String(), f.changes, f.changeStatusCalls, f.policies)
		}
	})
	t.Run("clear unowned record", func(t *testing.T) {
		f := successfulFake()
		f.addresses = nil
		var stdout bytes.Buffer
		if err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f)); err != nil || stdout.String() != "record: ok (ikigenba.dev deleted)\n" || len(f.changes) != 1 || !reflect.DeepEqual(f.changes[0][0].Record, f.record) || len(f.ssh) != 0 || len(f.policies) != 0 {
			t.Errorf("err=%v stdout=%q changes=%#v ssh=%d policies=%d", err, stdout.String(), f.changes, len(f.ssh), len(f.policies))
		}
	})
	t.Run("clear stopped holder preserves record", func(t *testing.T) {
		f := successfulFake()
		f.instances[1].State = cloud.StateStopped
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f))
		var stopped *space.NotRunningError
		if !errors.As(err, &stopped) || stdout.Len() != 0 || len(f.changes) != 0 || len(f.ssh) != 0 || len(f.policies) != 0 {
			t.Errorf("err=%v stdout=%q changes=%d ssh=%d policies=%d", err, stdout.String(), len(f.changes), len(f.ssh), len(f.policies))
		}
	})
	t.Run("clear absent", func(t *testing.T) {
		f := successfulFake()
		f.recordFound = false
		var stdout bytes.Buffer
		if err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f)); err != nil || stdout.String() != "record: ok (already gone)\n" || len(f.changes) != 0 || len(f.ssh) != 0 {
			t.Errorf("err=%v stdout=%q changes=%d ssh=%d", err, stdout.String(), len(f.changes), len(f.ssh))
		}
	})
}

func TestShowExactHostReadAndFailureBoundaries(t *testing.T) {
	// R-4QM2-75F5 R-4RTY-KX5U
	t.Run("set", func(t *testing.T) {
		f := successfulFake()
		f.getValue = "crm\r\n"
		var stdout bytes.Buffer
		if err := apex.Run(context.Background(), []string{"show"}, &stdout, testDeps(t, f)); err != nil {
			t.Fatal(err)
		}
		wantSSH := []string{"'sudo' 'opsctl' 'config' 'get' 'host.apex'"}
		if stdout.String() != "crm.sbx2.ikigenba.dev 18.220.10.5\n" || !reflect.DeepEqual(f.ssh, wantSSH) || !reflect.DeepEqual(f.sshTargets, []string{"ec2-user@18.220.10.5"}) || len(f.policies) != 0 || len(f.changes) != 0 || f.streamCalls != 0 || f.ssmCalls != 0 || f.s3Calls != 0 {
			t.Fatalf("stdout=%q ssh=%v targets=%v policies=%v changes=%v forbidden=%d/%d/%d", stdout.String(), f.ssh, f.sshTargets, f.policies, f.changes, f.streamCalls, f.ssmCalls, f.s3Calls)
		}
	})

	t.Run("unset", func(t *testing.T) {
		f := successfulFake()
		f.getUnset = true
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"show"}, &stdout, testDeps(t, f))
		var unset *apex.NoApexAppError
		if !errors.As(err, &unset) || unset.Root != "ikigenba.dev" || unset.Domain != "sbx2.ikigenba.dev" || stdout.Len() != 0 || len(f.ssh) != 1 {
			t.Fatalf("err=%#v stdout=%q ssh=%v", err, stdout.String(), f.ssh)
		}
	})

	t.Run("get error", func(t *testing.T) {
		f := successfulFake()
		f.execResult = func(seam.Cmd) (seam.Result, error) {
			return seam.Result{ExitCode: 2, Stderr: []byte("broken\n")}, nil
		}
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"show"}, &stdout, testDeps(t, f))
		var command *host.CommandError
		if !errors.As(err, &command) || command.Step != "show" || command.Status != 2 || !reflect.DeepEqual(command.Command, []string{"ssh", "ec2-user@18.220.10.5", "sudo", "opsctl", "config", "get", "host.apex"}) || command.Stderr != "broken\n" || stdout.Len() != 0 || len(f.ssh) != 1 {
			t.Fatalf("err=%#v stdout=%q ssh=%v", err, stdout.String(), f.ssh)
		}
	})

	for _, tc := range []struct {
		name string
		edit func(*fake)
	}{
		{"absent", func(f *fake) { f.recordFound = false }},
		{"running", func(f *fake) { f.getValue = "crm\n" }},
	} {
		t.Run(tc.name+" forbids mutations", func(t *testing.T) {
			f := successfulFake()
			tc.edit(f)
			var stdout bytes.Buffer
			if err := apex.Run(context.Background(), []string{"show"}, &stdout, testDeps(t, f)); err != nil {
				t.Fatal(err)
			}
			if len(f.changes) != 0 || len(f.policies) != 0 || f.changeStatusCalls != 0 || f.streamCalls != 0 || f.ssmCalls != 0 || f.s3Calls != 0 {
				t.Fatalf("changes=%v policies=%v status=%d forbidden=%d/%d/%d", f.changes, f.policies, f.changeStatusCalls, f.streamCalls, f.ssmCalls, f.s3Calls)
			}
		})
	}
}

func TestClearExactSequenceAndFailureBoundaries(t *testing.T) {
	// R-4T1U-YOWJ R-4U9R-CGN8 R-VZHT-XYEM R-4XXG-HRVB R-50D9-9BCP
	const spaceLine = "space: ok (sbx2.ikigenba.dev running, 18.220.10.5)\n"
	const recordLine = "record: ok (ikigenba.dev deleted)\n"
	const roleLine = "role: ok (sbx2.ikigenba.dev may no longer prove ikigenba.dev)\n"

	t.Run("success", func(t *testing.T) {
		f := successfulFake()
		var stdout bytes.Buffer
		if err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f)); err != nil {
			t.Fatal(err)
		}
		wantPolicy := policyCall{"sbx2.ikigenba.dev", space.PolicyName, space.PolicyDocument("ikigenba.dev", "ZONE1", spaceref.Space{Label: "sbx2", Domain: "sbx2.ikigenba.dev"}, false)}
		wantSSH := []string{"'true'", "'sudo' 'opsctl' 'config' 'del' 'host.apex'", "'sudo' 'opsctl' 'cert' 'obtain'", "'sudo' 'opsctl' 'nginx' 'apply'"}
		wantOutput := spaceLine + recordLine + roleLine + "host: ok (host.apex removed, certificate obtained, nginx applied)\n"
		if stdout.String() != wantOutput || !reflect.DeepEqual(f.changeZones, []string{"ZONE1"}) || len(f.changes) != 1 || !reflect.DeepEqual(f.changes[0], []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: f.record}}) || !reflect.DeepEqual(f.policies, []policyCall{wantPolicy}) || !reflect.DeepEqual(f.ssh, wantSSH) || !reflect.DeepEqual(f.sshTargets, []string{"ec2-user@18.220.10.5", "ec2-user@18.220.10.5", "ec2-user@18.220.10.5", "ec2-user@18.220.10.5"}) || f.changeStatusCalls != 0 || f.streamCalls != 0 || f.ssmCalls != 0 || f.s3Calls != 0 {
			t.Fatalf("stdout=%q zones=%v changes=%#v policies=%#v ssh=%v targets=%v forbidden=%d/%d/%d/%d", stdout.String(), f.changeZones, f.changes, f.policies, f.ssh, f.sshTargets, f.changeStatusCalls, f.streamCalls, f.ssmCalls, f.s3Calls)
		}
	})

	t.Run("absent", func(t *testing.T) {
		f := successfulFake()
		f.recordFound = false
		var stdout bytes.Buffer
		if err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f)); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != "record: ok (already gone)\n" || !reflect.DeepEqual(f.zoneRoots, []string{"ikigenba.dev"}) || !reflect.DeepEqual(f.recordQueries, [][3]string{{"ZONE1", "ikigenba.dev", "A"}}) || len(f.instanceRoots) != 0 || len(f.addressRoots) != 0 || len(f.changes) != 0 || len(f.policies) != 0 || len(f.ssh) != 0 {
			t.Fatalf("stdout=%q zones=%v queries=%v instances=%v addresses=%v changes=%v policies=%v ssh=%v", stdout.String(), f.zoneRoots, f.recordQueries, f.instanceRoots, f.addressRoots, f.changes, f.policies, f.ssh)
		}
	})

	t.Run("unowned", func(t *testing.T) {
		f := successfulFake()
		f.addresses = nil
		var stdout bytes.Buffer
		if err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f)); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != recordLine || !reflect.DeepEqual(f.changeZones, []string{"ZONE1"}) || len(f.changes) != 1 || !reflect.DeepEqual(f.changes[0], []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: f.record}}) || len(f.instanceRoots) != 0 || len(f.policies) != 0 || len(f.ssh) != 0 {
			t.Fatalf("stdout=%q zones=%v changes=%#v instances=%v policies=%v ssh=%v", stdout.String(), f.changeZones, f.changes, f.instanceRoots, f.policies, f.ssh)
		}
	})

	t.Run("record failure", func(t *testing.T) {
		f := successfulFake()
		want := errors.New("delete failed")
		f.changeErr = want
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f))
		if !errors.Is(err, want) || errors.Unwrap(err) != nil || stdout.String() != spaceLine || len(f.changes) != 1 || len(f.policies) != 0 || len(f.ssh) != 0 {
			t.Fatalf("err=%v stdout=%q changes=%v policies=%v ssh=%v", err, stdout.String(), f.changes, f.policies, f.ssh)
		}
	})

	t.Run("role failure", func(t *testing.T) {
		f := successfulFake()
		want := errors.New("policy failed")
		f.putPolicyErrAt, f.putPolicyErr = 1, want
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f))
		if !errors.Is(err, want) || errors.Unwrap(err) != nil || stdout.String() != spaceLine+recordLine || len(f.changes) != 1 || len(f.policies) != 1 || len(f.ssh) != 0 {
			t.Fatalf("err=%v stdout=%q changes=%v policies=%v ssh=%v", err, stdout.String(), f.changes, f.policies, f.ssh)
		}
	})

	t.Run("host nginx failure", func(t *testing.T) {
		f := successfulFake()
		f.execResult = func(cmd seam.Cmd) (seam.Result, error) {
			if strings.Contains(cmd.Args[len(cmd.Args)-1], "'nginx' 'apply'") {
				return seam.Result{ExitCode: 1, Stderr: []byte("nginx one\nnginx two\n")}, nil
			}
			return seam.Result{}, nil
		}
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f))
		var command *host.CommandError
		wantSSH := []string{"'true'", "'sudo' 'opsctl' 'config' 'del' 'host.apex'", "'sudo' 'opsctl' 'cert' 'obtain'", "'sudo' 'opsctl' 'nginx' 'apply'"}
		if !errors.As(err, &command) || command.Step != "host" || command.Status != 1 || command.Stderr != "nginx one\nnginx two\n" || command.Error() != "host: ssh ec2-user@18.220.10.5 sudo opsctl nginx apply: exit status 1" || command.Detail() != "> nginx one\n> nginx two" || stdout.String() != spaceLine+recordLine+roleLine || !reflect.DeepEqual(f.ssh, wantSSH) {
			t.Fatalf("err=%#v stdout=%q ssh=%v", err, stdout.String(), f.ssh)
		}
	})

	for _, tc := range []struct {
		name     string
		failure  string
		wantSSH  int
		wantCode int
	}{
		{"delete key", "'config' 'del'", 2, 4},
		{"certificate", "'cert' 'obtain'", 3, 5},
	} {
		t.Run("host "+tc.name+" short circuit", func(t *testing.T) {
			f := successfulFake()
			f.execResult = func(cmd seam.Cmd) (seam.Result, error) {
				if strings.Contains(cmd.Args[len(cmd.Args)-1], tc.failure) {
					return seam.Result{ExitCode: tc.wantCode}, nil
				}
				return seam.Result{}, nil
			}
			var stdout bytes.Buffer
			err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f))
			var command *host.CommandError
			if !errors.As(err, &command) || command.Step != "host" || command.Status != tc.wantCode || stdout.String() != spaceLine+recordLine+roleLine || len(f.ssh) != tc.wantSSH {
				t.Fatalf("err=%#v stdout=%q ssh=%v", err, stdout.String(), f.ssh)
			}
		})
	}

	t.Run("host wait failure", func(t *testing.T) {
		f := successfulFake()
		f.execResult = func(seam.Cmd) (seam.Result, error) { return seam.Result{ExitCode: 255}, nil }
		var stdout bytes.Buffer
		err := apex.Run(context.Background(), []string{"clear"}, &stdout, testDeps(t, f))
		var step *apex.StepError
		var unreachable *host.UnreachableError
		if !errors.As(err, &step) || step.Step != "host" || !errors.As(err, &unreachable) || unreachable.Address != "18.220.10.5" || stdout.String() != spaceLine+recordLine+roleLine || len(f.ssh) != host.ProbeAttempts {
			t.Fatalf("err=%#v stdout=%q ssh=%v", err, stdout.String(), f.ssh)
		}
		for _, command := range f.ssh {
			if command != "'true'" {
				t.Fatalf("command after failed wait: %q", command)
			}
		}
	})
}

type policyCall struct{ role, policy, document string }

type fake struct {
	root               string
	profile            string
	region             string
	instances          []cloud.Instance
	addresses          []cloud.Address
	record             cloud.Record
	recordFound        bool
	getValue           string
	getUnset           bool
	ssh                []string
	sshTargets         []string
	policies           []policyCall
	changes            [][]cloud.RecordChange
	changeZones        []string
	changeStatusIDs    []string
	changeStatusCalls  int
	zoneRoots          []string
	recordQueries      [][3]string
	instanceRoots      []string
	addressRoots       []string
	operations         []string
	putPolicyErrAt     int
	putPolicyErr       error
	changeErr          error
	changeStatusErr    error
	zoneErr            error
	findRecordErr      error
	listInstancesErrAt int
	listInstancesErr   error
	listAddressesErr   error
	execResult         func(seam.Cmd) (seam.Result, error)
	streamCalls        int
	ssmCalls           int
	s3Calls            int
}

func successfulFake() *fake {
	return &fake{
		instances: []cloud.Instance{
			{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"},
			{ID: "i-2", Space: "sbx2.ikigenba.dev", State: cloud.StateRunning, Address: "18.220.10.5"},
		},
		addresses:   []cloud.Address{{IP: "18.118.7.42", Space: "sbx1.ikigenba.dev"}, {IP: "18.220.10.5", Space: "sbx2.ikigenba.dev"}},
		record:      cloud.Record{Name: "ikigenba.dev", Type: "A", TTL: 60, Values: []string{"18.220.10.5"}},
		recordFound: true,
	}
}

func testDeps(t *testing.T, f *fake) seam.Deps {
	t.Helper()
	f.root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(f.root, "infra"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return seam.Deps{
		Dir: f.root,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			f.profile, f.region = profile, region
			return cloud.Clients{EC2: f, Route53: f, IAM: f, STS: f, SSM: f, S3: f}, nil
		},
		Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			if cmd.Path == "git" {
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			}
			if cmd.Path != "ssh" {
				return seam.Result{}, errors.New("unexpected command: " + cmd.Path)
			}
			remote := cmd.Args[len(cmd.Args)-1]
			f.ssh = append(f.ssh, remote)
			f.sshTargets = append(f.sshTargets, cmd.Args[len(cmd.Args)-2])
			f.operations = append(f.operations, "ssh:"+cmd.Args[len(cmd.Args)-2]+":"+remote)
			if f.execResult != nil {
				return f.execResult(cmd)
			}
			if strings.Contains(remote, "'config' 'get'") {
				if f.getUnset {
					return seam.Result{ExitCode: 1}, nil
				}
				return seam.Result{Stdout: []byte(f.getValue)}, nil
			}
			return seam.Result{}, nil
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			f.streamCalls++
			return seam.Result{}, nil
		},
		After: func(_ time.Duration) <-chan time.Time { ch := make(chan time.Time); close(ch); return ch },
	}
}

func assertFields(t *testing.T, pointer any, names []string) {
	t.Helper()
	typeOf := reflect.TypeOf(pointer).Elem()
	if typeOf.NumField() != len(names) {
		t.Fatalf("%s has %d fields, want %d", typeOf, typeOf.NumField(), len(names))
	}
	for i, name := range names {
		if typeOf.Field(i).Name != name {
			t.Errorf("field %d = %s, want %s", i, typeOf.Field(i).Name, name)
		}
	}
}

func (f *fake) CallerAccountID(context.Context) (string, error) { return "123456789012", nil }
func (f *fake) ListSpaceInstances(_ context.Context, root string) ([]cloud.Instance, error) {
	f.instanceRoots = append(f.instanceRoots, root)
	f.operations = append(f.operations, "instances")
	if f.listInstancesErrAt == len(f.instanceRoots) {
		return nil, f.listInstancesErr
	}
	return append([]cloud.Instance(nil), f.instances...), nil
}
func (f *fake) ListSpaceAddresses(_ context.Context, root string) ([]cloud.Address, error) {
	f.addressRoots = append(f.addressRoots, root)
	f.operations = append(f.operations, "addresses")
	if f.listAddressesErr != nil {
		return nil, f.listAddressesErr
	}
	return append([]cloud.Address(nil), f.addresses...), nil
}

func (f *fake) Zone(_ context.Context, root string) (cloud.Zone, error) {
	f.zoneRoots = append(f.zoneRoots, root)
	f.operations = append(f.operations, "zone")
	if f.zoneErr != nil {
		return cloud.Zone{}, f.zoneErr
	}
	return cloud.Zone{ID: "ZONE1", Name: "ikigenba.dev"}, nil
}

func (f *fake) FindRecord(_ context.Context, zone, name, recordType string) (cloud.Record, bool, error) {
	f.recordQueries = append(f.recordQueries, [3]string{zone, name, recordType})
	f.operations = append(f.operations, "find-record")
	if f.findRecordErr != nil {
		return cloud.Record{}, false, f.findRecordErr
	}
	return f.record, f.recordFound, nil
}
func (f *fake) ChangeRecords(_ context.Context, zone string, changes []cloud.RecordChange) (string, error) {
	f.operations = append(f.operations, "change-records")
	f.changeZones = append(f.changeZones, zone)
	f.changes = append(f.changes, append([]cloud.RecordChange(nil), changes...))
	if f.changeErr != nil {
		return "", f.changeErr
	}
	return "CHANGE1", nil
}
func (f *fake) ChangeStatus(_ context.Context, changeID string) (cloud.ChangeStatus, error) {
	f.operations = append(f.operations, "change-status")
	f.changeStatusIDs = append(f.changeStatusIDs, changeID)
	f.changeStatusCalls++
	if f.changeStatusErr != nil {
		return "", f.changeStatusErr
	}
	return cloud.ChangeInsync, nil
}
func (f *fake) PutRolePolicy(_ context.Context, role, policy, document string) error {
	f.operations = append(f.operations, "put-policy:"+role)
	f.policies = append(f.policies, policyCall{role, policy, document})
	if f.putPolicyErrAt == len(f.policies) {
		return f.putPolicyErr
	}
	return nil
}

func (*fake) LaunchTemplate(context.Context, string) (string, error) { panic("unexpected EC2 call") }
func (*fake) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	panic("unexpected EC2 call")
}
func (*fake) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	panic("unexpected EC2 call")
}
func (*fake) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) {
	panic("unexpected EC2 call")
}
func (*fake) StartInstance(context.Context, string) error     { panic("unexpected EC2 call") }
func (*fake) StopInstance(context.Context, string) error      { panic("unexpected EC2 call") }
func (*fake) TerminateInstance(context.Context, string) error { panic("unexpected EC2 call") }
func (*fake) InstanceChecksPassed(context.Context, string) (bool, error) {
	panic("unexpected EC2 call")
}
func (*fake) AllocateAddress(context.Context, string, string) (cloud.Address, error) {
	panic("unexpected EC2 call")
}
func (*fake) AssociateAddress(context.Context, string, string) error { panic("unexpected EC2 call") }
func (*fake) DisassociateAddress(context.Context, string) error      { panic("unexpected EC2 call") }
func (*fake) ReleaseAddress(context.Context, string) error           { panic("unexpected EC2 call") }
func (*fake) ListRecords(context.Context, string) ([]cloud.Record, error) {
	panic("unexpected Route53 call")
}
func (*fake) PermissionsBoundary(context.Context, string) (string, error) {
	panic("unexpected IAM call")
}
func (*fake) RoleExists(context.Context, string) (bool, error)       { panic("unexpected IAM call") }
func (*fake) CreateRole(context.Context, cloud.RoleSpec) error       { panic("unexpected IAM call") }
func (*fake) DeleteRolePolicy(context.Context, string, string) error { panic("unexpected IAM call") }
func (*fake) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	panic("unexpected IAM call")
}
func (*fake) CreateInstanceProfile(context.Context, string) error { panic("unexpected IAM call") }
func (*fake) AddRoleToInstanceProfile(context.Context, string, string) error {
	panic("unexpected IAM call")
}
func (*fake) RemoveRoleFromInstanceProfile(context.Context, string, string) error {
	panic("unexpected IAM call")
}
func (*fake) DeleteInstanceProfile(context.Context, string) error { panic("unexpected IAM call") }
func (*fake) DeleteRole(context.Context, string) error            { panic("unexpected IAM call") }
func (f *fake) GetParameter(context.Context, string) (string, error) {
	f.ssmCalls++
	panic("unexpected SSM call")
}
func (f *fake) PutSecureParameter(context.Context, string, string) error {
	f.ssmCalls++
	panic("unexpected SSM call")
}
func (f *fake) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	f.ssmCalls++
	panic("unexpected SSM call")
}
func (f *fake) DeleteParameter(context.Context, string) error {
	f.ssmCalls++
	panic("unexpected SSM call")
}
func (f *fake) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	f.s3Calls++
	panic("unexpected S3 call")
}
func (f *fake) PutObject(context.Context, string, string, io.Reader, int64) error {
	f.s3Calls++
	panic("unexpected S3 call")
}
func (f *fake) DeleteObjects(context.Context, string, []string) error {
	f.s3Calls++
	panic("unexpected S3 call")
}

var _ cloud.EC2 = (*fake)(nil)
var _ cloud.Route53 = (*fake)(nil)
var _ cloud.IAM = (*fake)(nil)
var _ cloud.STS = (*fake)(nil)
var _ cloud.SSM = (*fake)(nil)
var _ cloud.S3 = (*fake)(nil)
