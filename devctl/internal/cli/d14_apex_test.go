package cli

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type apexHostFixture struct {
	cloud.EC2
	cloud.Route53
	cloud.IAM
	cloud.STS
	clear     bool
	changes   int
	remote    [][]string
	instances []cloud.Instance
	addresses []cloud.Address
	record    *cloud.Record
	policies  []string
	policyErr error
	changeErr error
}

func (*apexHostFixture) CallerAccountID(context.Context) (string, error) {
	return "123456789012", nil
}

func (f *apexHostFixture) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	if f.instances != nil {
		return f.instances, nil
	}
	return []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}}, nil
}

func (f *apexHostFixture) ListSpaceAddresses(context.Context, string) ([]cloud.Address, error) {
	if f.addresses != nil {
		return f.addresses, nil
	}
	return []cloud.Address{{IP: "18.118.7.42", Space: "sbx1.ikigenba.dev"}}, nil
}

func (*apexHostFixture) Zone(context.Context, string) (cloud.Zone, error) {
	return cloud.Zone{ID: "ZROOT", Name: "ikigenba.dev"}, nil
}

func (f *apexHostFixture) FindRecord(context.Context, string, string, string) (cloud.Record, bool, error) {
	if f.record != nil {
		return *f.record, true, nil
	}
	if !f.clear {
		return cloud.Record{}, false, nil
	}
	return cloud.Record{Name: "ikigenba.dev", Type: "A", TTL: 60, Values: []string{"18.118.7.42"}}, true, nil
}

func (f *apexHostFixture) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	f.changes++
	return "CHANGE1", f.changeErr
}

func (f *apexHostFixture) PutRolePolicy(_ context.Context, role, _, _ string) error {
	f.policies = append(f.policies, role)
	return f.policyErr
}
func (*apexHostFixture) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	return cloud.ChangeInsync, nil
}

func TestApexHostCommandFailuresThroughCLI(t *testing.T) {
	// R-FJY3-C7QT R-FL5Z-PZHI
	for _, tc := range []struct {
		name      string
		clear     bool
		fail      []string
		stderr    string
		stdout    string
		remote    [][]string
		instances []cloud.Instance
		addresses []cloud.Address
		record    *cloud.Record
		policies  []string
		policyErr error
		changeErr error
		changes   int
	}{
		{
			name: "set certificate", fail: []string{"sudo", "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "opsctl", "cert", "obtain"},
			stderr: "a\n\nb\nc\n",
			stdout: "space: ok (sbx1.ikigenba.dev running, 18.118.7.42)\nrole: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\n",
			remote: [][]string{{"true"}, {"sudo", "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "opsctl", "config", "set", "host.apex=crm"}, {"sudo", "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "opsctl", "cert", "obtain"}},
		},
		{
			name: "clear nginx", clear: true, fail: []string{"sudo", "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "opsctl", "nginx", "apply"},
			stderr:  "a\n\nb\n",
			stdout:  "space: ok (sbx1.ikigenba.dev running, 18.118.7.42)\nrecord: ok (ikigenba.dev deleted)\nrole: ok (sbx1.ikigenba.dev may no longer prove ikigenba.dev)\n",
			remote:  [][]string{{"true"}, {"sudo", "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "opsctl", "config", "del", "host.apex"}, {"sudo", "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "opsctl", "cert", "obtain"}, {"sudo", "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "opsctl", "nginx", "apply"}},
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
				if len(cmd.Args) != 8 || cmd.Args[6] != "ec2-user@sbx1.ikigenba.dev" {
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
			wantStderr := "devctl: host: ssh ec2-user@sbx1.ikigenba.dev " + joinApexRemote(tc.fail) + ": exit status 1\n\n"
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
	// R-ST4K-APZN R-EINW-L6B7
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

func apexCommandDeps(t *testing.T, f *apexHostFixture, reply func(seam.Cmd) seam.Result) seam.Deps {
	t.Helper()
	deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	gitExec := deps.Exec
	deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
		if profile != "ikigenba.dev" || region != "us-east-2" {
			t.Fatalf("cloud %q %q", profile, region)
		}
		return cloud.Clients{EC2: f, Route53: f, IAM: f, STS: f}, nil
	}
	deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "git" {
			return gitExec(ctx, cmd)
		}
		if cmd.Path != "ssh" {
			t.Fatalf("unexpected command %#v", cmd)
		}
		f.remote = append(f.remote, decodeApexRemote(t, cmd.Args[len(cmd.Args)-1]))
		if reply != nil {
			return reply(cmd), nil
		}
		return seam.Result{}, nil
	}
	deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		t.Fatal("unexpected stream")
		return seam.Result{}, nil
	}
	deps.After = func(time.Duration) <-chan time.Time { ch := make(chan time.Time); close(ch); return ch }
	return deps
}

func TestApexHostPreflightThroughCLI(t *testing.T) {
	// R-FF2H-T4S1 R-EHG0-7EKI R-EL3P-CPSL R-ENJI-499Z
	for _, command := range []string{"set", "show", "clear"} {
		for _, state := range []string{"absent", "unowned", "stopped", "address only"} {
			t.Run(command+" "+state, func(t *testing.T) {
				f := &apexHostFixture{clear: true}
				args := []string{"apex", command}
				if command == "set" {
					args = append(args, "crm.sbx1")
				}
				want := ""
				code := 1
				switch state {
				case "absent":
					if command == "set" {
						args[2] = "crm.gone"
						want = "devctl: no space at 'gone.ikigenba.dev'\n"
					} else {
						f.clear = false
						code = 0
						if command == "clear" {
							want = "record: ok (already gone)\n"
						}
					}
				case "unowned":
					f.record = &cloud.Record{Name: "ikigenba.dev", Type: "A", Values: []string{"203.0.113.9"}}
					want = "devctl: ikigenba.dev points at 203.0.113.9, which is not a space's address\n"
					if command == "clear" {
						code = 0
						want = "record: ok (ikigenba.dev deleted)\n"
					}
				case "stopped":
					f.instances = []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateStopped}}
					want = "devctl: 'sbx1.ikigenba.dev' is stopped\n"
				case "address only":
					f.instances = []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateTerminated}}
					want = "devctl: 'sbx1.ikigenba.dev' has no instance\n"
				}
				got := invokeWithDeps(apexCommandDeps(t, f, nil), args...)
				if code == 0 {
					assertResult(t, got, code, want, "")
				} else {
					assertResult(t, got, code, "", want)
				}
				wantChanges := 0
				if command == "clear" && state == "unowned" {
					wantChanges = 1
				}
				if len(f.remote) != 0 || len(f.policies) != 0 || f.changes != wantChanges {
					t.Fatalf("host=%v policies=%v changes=%d", f.remote, f.policies, f.changes)
				}
			})
		}
	}
	// A stopped previous holder is refused before mutations on the running target.
	f := &apexHostFixture{record: &cloud.Record{Name: "ikigenba.dev", Type: "A", Values: []string{"18.220.10.5"}}, instances: []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning}, {ID: "i-2", Space: "sbx2.ikigenba.dev", State: cloud.StateStopped}}, addresses: []cloud.Address{{IP: "18.118.7.42", Space: "sbx1.ikigenba.dev"}, {IP: "18.220.10.5", Space: "sbx2.ikigenba.dev"}}}
	assertResult(t, invokeWithDeps(apexCommandDeps(t, f, nil), "apex", "set", "crm.sbx1"), 1, "", "devctl: 'sbx2.ikigenba.dev' holds the apex and is stopped\n\nrun 'devctl space start sbx2' first\n")
	if len(f.remote) != 0 || len(f.policies) != 0 || f.changes != 0 {
		t.Fatal("mutated before previous holder check")
	}
}

func TestApexSetCloudFailuresAndPreviousThroughCLI(t *testing.T) {
	// R-FIQ6-YG04 R-FNLS-HIYW R-FOTO-VAPL R-FHIA-KO9F R-EJVS-YY1W
	const first = "space: ok (sbx1.ikigenba.dev running, 18.118.7.42)\n"
	const role = "role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\n"
	const host = "host: ok (host.apex=crm, certificate obtained, nginx applied)\n"
	const record = "record: ok (ikigenba.dev -> 18.118.7.42, INSYNC)\n"
	for _, stage := range []string{"role", "record", "previous", "same holder"} {
		t.Run(stage, func(t *testing.T) {
			f := &apexHostFixture{}
			var reply func(seam.Cmd) seam.Result
			switch stage {
			case "role":
				f.policyErr = &cloud.Error{Service: "iam", Operation: "PutRolePolicy", Code: "Throttling"}
			case "record":
				f.changeErr = &cloud.Error{Service: "route53", Operation: "ChangeResourceRecordSets", Code: "Throttling"}
			case "previous":
				f.record = &cloud.Record{Name: "ikigenba.dev", Type: "A", Values: []string{"18.220.10.5"}}
				f.instances = []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning}, {ID: "i-2", Space: "sbx2.ikigenba.dev", State: cloud.StateRunning}}
				f.addresses = []cloud.Address{{IP: "18.118.7.42", Space: "sbx1.ikigenba.dev"}, {IP: "18.220.10.5", Space: "sbx2.ikigenba.dev"}}
				reply = func(cmd seam.Cmd) seam.Result {
					if cmd.Args[len(cmd.Args)-2] == "ec2-user@sbx2.ikigenba.dev" {
						return seam.Result{ExitCode: 255}
					}
					return seam.Result{}
				}
			case "same holder":
				f.clear = true
			}
			got := invokeWithDeps(apexCommandDeps(t, f, reply), "apex", "set", "crm.sbx1")
			switch stage {
			case "role":
				assertResult(t, got, 1, first, "devctl: iam PutRolePolicy: Throttling\n")
				if len(f.remote) != 0 {
					t.Fatal(f.remote)
				}
			case "record":
				assertResult(t, got, 1, first+role+host, "devctl: route53 ChangeResourceRecordSets: Throttling\n")
			case "previous":
				assertResult(t, got, 1, first+role+host+record, "devctl: previous: ssh ec2-user@sbx2.ikigenba.dev: connection timed out\n")
			case "same holder":
				assertResult(t, got, 0, first+role+host+record+"previous: ok (none)\nikigenba.dev crm.sbx1.ikigenba.dev\n", "")
				if !reflect.DeepEqual(f.policies, []string{"sbx1.ikigenba.dev"}) {
					t.Fatal(f.policies)
				}
			}
		})
	}
}

func TestApexShowHostReadThroughCLI(t *testing.T) {
	// R-FMDW-3R87 R-EMBL-QHJA
	for _, unset := range []bool{false, true} {
		f := &apexHostFixture{clear: true}
		deps := apexCommandDeps(t, f, func(cmd seam.Cmd) seam.Result {
			if cmd.Args[len(cmd.Args)-2] != "ec2-user@sbx1.ikigenba.dev" {
				t.Fatal(cmd.Args)
			}
			if unset {
				return seam.Result{ExitCode: 1}
			}
			return seam.Result{Stdout: []byte("crm\n")}
		})
		got := invokeWithDeps(deps, "apex", "show")
		if unset {
			assertResult(t, got, 1, "", "devctl: ikigenba.dev points at sbx1.ikigenba.dev, whose host.apex is not set\n")
		} else {
			assertResult(t, got, 0, "crm.sbx1.ikigenba.dev 18.118.7.42\n", "")
		}
		if !reflect.DeepEqual(f.remote, [][]string{{"sudo", "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "opsctl", "config", "get", "host.apex"}}) || f.changes != 0 || len(f.policies) != 0 {
			t.Fatalf("remote=%v changes=%d policies=%v", f.remote, f.changes, f.policies)
		}
	}
}

func TestApexRootFileProfileAndErrors(t *testing.T) {
	// R-EINW-L6B7
	for _, args := range [][]string{{"apex", "set", "crm.sbx1"}, {"apex", "show"}, {"apex", "clear"}} {
		deps := checkoutDeps(t, `{"domain":"example.test","region":"eu-west-1"}`)
		calls := 0
		cause := errors.New("connect sentinel")
		deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
			calls++
			if profile != "example.test" || region != "eu-west-1" {
				t.Fatal(profile, region)
			}
			return cloud.Clients{}, cause
		}
		assertResult(t, invokeWithDeps(deps, args...), 1, "", "devctl: connect sentinel\n")
		if calls != 1 {
			t.Fatal(calls)
		}
		deps = checkoutDeps(t, `{"domain":"example.test"}`)
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("cloud before root-file check")
			return cloud.Clients{}, nil
		}
		got := invokeWithDeps(deps, args...)
		if got.code != 2 || got.stdout != "" || !strings.HasPrefix(got.stderr, "devctl: ") {
			t.Fatal(got)
		}
	}
}
