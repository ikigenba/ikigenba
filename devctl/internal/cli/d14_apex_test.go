package cli

import (
	"context"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type apexHostFixture struct {
	cloud.EC2
	cloud.Route53
	cloud.IAM
	cloud.STS
	clear   bool
	changes int
	remote  [][]string
}

func (*apexHostFixture) CallerAccountID(context.Context) (string, error) {
	return "123456789012", nil
}

func (f *apexHostFixture) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}}, nil
}

func (f *apexHostFixture) ListSpaceAddresses(context.Context, string) ([]cloud.Address, error) {
	return []cloud.Address{{IP: "18.118.7.42", Space: "sbx1.ikigenba.dev"}}, nil
}

func (*apexHostFixture) Zone(context.Context, string) (cloud.Zone, error) {
	return cloud.Zone{ID: "ZROOT", Name: "ikigenba.dev"}, nil
}

func (f *apexHostFixture) FindRecord(context.Context, string, string, string) (cloud.Record, bool, error) {
	if !f.clear {
		return cloud.Record{}, false, nil
	}
	return cloud.Record{Name: "ikigenba.dev", Type: "A", TTL: 60, Values: []string{"18.118.7.42"}}, true, nil
}

func (f *apexHostFixture) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	f.changes++
	return "CHANGE1", nil
}

func (*apexHostFixture) PutRolePolicy(context.Context, string, string, string) error { return nil }

func TestApexHostCommandFailuresThroughCLI(t *testing.T) {
	// R-SNXG-N0VB R-SRL5-SC3E
	for _, tc := range []struct {
		name    string
		clear   bool
		fail    []string
		stderr  string
		stdout  string
		remote  [][]string
		changes int
	}{
		{
			name: "set certificate", fail: []string{"sudo", "opsctl", "cert", "obtain"},
			stderr: "a\n\nb\nc\n",
			stdout: "space: ok (sbx1.ikigenba.dev running, 18.118.7.42)\nrole: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\n",
			remote: [][]string{{"true"}, {"sudo", "opsctl", "config", "set", "host.apex=crm"}, {"sudo", "opsctl", "cert", "obtain"}},
		},
		{
			name: "clear nginx", clear: true, fail: []string{"sudo", "opsctl", "nginx", "apply"},
			stderr:  "a\n\nb\n",
			stdout:  "space: ok (sbx1.ikigenba.dev running, 18.118.7.42)\nrecord: ok (ikigenba.dev deleted)\nrole: ok (sbx1.ikigenba.dev may no longer prove ikigenba.dev)\n",
			remote:  [][]string{{"true"}, {"sudo", "opsctl", "config", "del", "host.apex"}, {"sudo", "opsctl", "cert", "obtain"}, {"sudo", "opsctl", "nginx", "apply"}},
			changes: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &apexHostFixture{clear: tc.clear}
			deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
			gitExec := deps.Exec
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				return cloud.Clients{EC2: f, Route53: f, IAM: f, STS: f}, nil
			}
			deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
				if cmd.Path == "git" {
					return gitExec(ctx, cmd)
				}
				if cmd.Path != "ssh" {
					t.Fatalf("unexpected process: %#v", cmd)
				}
				if len(cmd.Args) != 8 || cmd.Args[6] != "ec2-user@18.118.7.42" {
					t.Fatalf("unexpected SSH target: %#v", cmd.Args)
				}
				logical := decodeApexRemote(t, cmd.Args[7])
				f.remote = append(f.remote, logical)
				if reflect.DeepEqual(logical, tc.fail) {
					return seam.Result{ExitCode: 1, Stderr: []byte(tc.stderr)}, nil
				}
				return seam.Result{}, nil
			}
			args := []string{"apex", "set", "crm.sbx1"}
			if tc.clear {
				args = []string{"apex", "clear"}
			}
			got := invokeWithDeps(deps, args...)
			wantStderr := "devctl: host: ssh ec2-user@18.118.7.42 " + joinApexRemote(tc.fail) + ": exit status 1\n\n"
			for _, line := range []string{"a", "", "b"} {
				wantStderr += "> " + line + "\n"
			}
			if !tc.clear {
				wantStderr += "> c\n"
			}
			assertResult(t, got, 1, tc.stdout, wantStderr)
			if !reflect.DeepEqual(f.remote, tc.remote) || f.changes != tc.changes {
				t.Fatalf("remote=%q changes=%d, want remote=%q changes=%d", f.remote, f.changes, tc.remote, tc.changes)
			}
		})
	}
}

func decodeApexRemote(t *testing.T, command string) []string {
	t.Helper()
	// All arguments in this contract are simple words; decode the SSH single-quote framing.
	var args []string
	for command != "" {
		if command[0] != '\'' {
			t.Fatalf("remote command %q", command)
		}
		end := 1
		for end < len(command) && command[end] != '\'' {
			end++
		}
		if end == len(command) {
			t.Fatalf("remote command %q", command)
		}
		args = append(args, command[1:end])
		command = command[end+1:]
		if command != "" {
			if command[0] != ' ' {
				t.Fatalf("remote command %q", command)
			}
			command = command[1:]
		}
	}
	return args
}

func joinApexRemote(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		result += arg
	}
	return result
}

func TestApexDispatchAndHelpAreExactAndDependencyFree(t *testing.T) {
	// R-48BK-GLAQ R-49JG-UD1F
	for _, args := range [][]string{
		{"apex", "--help"},
		{"apex", "-h"},
		{"apex", "set", "--help"},
		{"apex", "show", "-h"},
		{"apex", "clear", "ignored", "--help"},
	} {
		t.Run(args[1], func(t *testing.T) {
			calls := 0
			deps := noExternalDeps(&calls)
			deps.Dir = t.TempDir()
			assertResult(t, invokeWithDeps(deps, args...), 0, expectedApexUsage, "")
			if calls != 0 {
				t.Fatalf("Run(%q) made %d external calls, want none", args, calls)
			}
		})
	}

	calls := 0
	deps := noExternalDeps(&calls)
	deps.EUID = 0
	assertResult(t, invokeWithDeps(deps, "apex", "--help"), 3, "", "devctl: must not run as root\n")
	if calls != 0 {
		t.Fatalf("superuser refusal made %d external calls, want none", calls)
	}
}

func TestApexArgumentFailuresAreExactAndDependencyFree(t *testing.T) {
	// R-4BZ9-LWIT
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing subcommand", args: []string{"apex"}, want: "apex needs <subcommand>"},
		{name: "unknown subcommand", args: []string{"apex", "frobnicate"}, want: "unknown subcommand 'frobnicate'"},
		{name: "set missing operand", args: []string{"apex", "set"}, want: "apex set needs <app>.<space>"},
		{name: "set extra operand", args: []string{"apex", "set", "crm.sbx1", "extra"}, want: "apex set takes only <app>.<space>"},
		{name: "show operand", args: []string{"apex", "show", "sbx1"}, want: "apex show takes no arguments"},
		{name: "clear operand", args: []string{"apex", "clear", "sbx1"}, want: "apex clear takes no arguments"},
		{name: "unknown option", args: []string{"apex", "show", "--verbose"}, want: "unknown option '--verbose'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			deps := noExternalDeps(&calls)
			deps.Dir = t.TempDir()
			wantStderr := "devctl: " + test.want + "\n\nsee 'devctl apex --help' for usage\n"
			assertResult(t, invokeWithDeps(deps, test.args...), 2, "", wantStderr)
			if calls != 0 {
				t.Fatalf("Run(%q) made %d external calls, want none", test.args, calls)
			}
		})
	}
}

func TestApexOperandParsingPrecedesCloud(t *testing.T) {
	// R-ST4K-APZN
	for _, operand := range []string{"sbx1", "crm.sbx1.example.com"} {
		deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
		cloudCalls := 0
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, nil
		}
		result := invokeWithDeps(deps, "apex", "set", operand)
		want := "devctl: '" + operand + "' is not an app on a space: <app>.<space>\n"
		if result.code != 2 || result.stdout != "" || result.stderr != want || cloudCalls != 0 {
			t.Fatalf("apex set %q = %#v, cloud calls %d", operand, result, cloudCalls)
		}
	}
}
