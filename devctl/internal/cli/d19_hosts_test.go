package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/hosts"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

// R-OG7R-VJAK
var _ func(context.Context, []string, io.Writer, seam.Deps) error = hosts.Run

const expectedHostsUsage = `Usage: devctl hosts <subcommand> [arguments]

Make one space the hosts operator, or stop it being one. The hosts operator's
role has the managed policy <root>-hosts-operator, which lets the accounts
service on that space run hosts; at most one space is the hosts operator.

Subcommands:
  set <space>     make <space> the hosts operator
  clear <space>   stop <space> being the hosts operator

Run 'devctl hosts <subcommand> --help' for details.
`

const (
	hostsRoot = "ikigenba.dev"
	hostsUS   = "us.ikigenba.dev"
	hostsARN  = "arn:aws:iam::123456789012:policy/tf/ikigenba.dev-hosts-operator"
	hostsName = "ikigenba.dev-hosts-operator"

	setTagLine    = "tag: ok (us.ikigenba.dev tagged hosts operator)\n"
	setRoleLine   = "role: ok (ikigenba.dev-hosts-operator attached to us.ikigenba.dev)\n"
	setOutput     = setTagLine + setRoleLine + hostsUS + "\n"
	clearRoleLine = "role: ok (ikigenba.dev-hosts-operator detached from us.ikigenba.dev)\n"
	clearTagLine  = "tag: ok (us.ikigenba.dev untagged)\n"
	clearOutput   = clearRoleLine + clearTagLine
)

func TestHostsHelp(t *testing.T) {
	// R-OL3D-EM9C
	for _, args := range [][]string{
		{"--help"}, {"-h"}, {"set", "--help"}, {"clear", "us", "-h"},
		{"set", "us", "--force", "-h"},
	} {
		calls := 0
		deps := noExternalDeps(&calls)
		deps.Dir = t.TempDir()
		assertResult(t, invokeWithDeps(deps, append([]string{"hosts"}, args...)...), 0, expectedHostsUsage, "")
		deps.EUID = 0
		assertResult(t, invokeWithDeps(deps, append([]string{"hosts"}, args...)...), 3, "", "devctl: must not run as root\n")
		if calls != 0 {
			t.Fatalf("hosts %q made %d external calls", args, calls)
		}
	}
}

func TestHostsArgumentGrammar(t *testing.T) {
	// R-OMB9-SE01 R-ONJ6-65QQ
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "hosts needs <subcommand>"},
		{[]string{"show"}, "unknown subcommand 'show'"},
		{[]string{"set"}, "hosts set needs <space>"},
		{[]string{"clear"}, "hosts clear needs <space>"},
		{[]string{"set", "us", "sbx1"}, "hosts set takes only <space>"},
		{[]string{"clear", "us", "sbx1"}, "hosts clear takes only <space>"},
		{[]string{"--force"}, "unknown option '--force'"},
		{[]string{"clear", "us", "--force"}, "unknown option '--force'"},
		{[]string{"set", "-V"}, "unknown option '-V'"},
	} {
		calls := 0
		deps := noExternalDeps(&calls)
		deps.Dir = t.TempDir()
		want := "devctl: " + test.want + "\n\nsee 'devctl hosts --help' for usage\n"
		assertResult(t, invokeWithDeps(deps, append([]string{"hosts"}, test.args...)...), 2, "", want)
		var out bytes.Buffer
		err := hosts.Run(t.Context(), test.args, &out, deps)
		var usage *space.UsageError
		if !errors.As(err, &usage) || usage.Message != test.want || usage.Help != "devctl hosts --help" ||
			out.Len() != 0 || calls != 0 {
			t.Fatalf("hosts %q gave error %#v, stdout %q, and %d external calls", test.args, err, out.String(), calls)
		}
	}
}

func TestHostsCommandPreflights(t *testing.T) {
	// R-6I8R-BGWE
	for _, subcommand := range []string{"set", "clear"} {
		t.Run(subcommand, func(t *testing.T) {
			for _, test := range []struct {
				operand string
				stderr  string
			}{
				{"crm.us", "devctl: 'crm.us' is not a space: a space is one label under 'ikigenba.dev'\n"},
				{"Us_1", "devctl: 'Us_1' is not a valid label\n"},
			} {
				f := hostsTestFixture{}
				deps := f.deps(t)
				assertResult(t, invokeWithDeps(deps, "hosts", subcommand, test.operand), 2, "", test.stderr)
				f.assertCalls(t, "git rev-parse --show-toplevel")
			}

			calls := 0
			deps := checkoutDeps(t, `{"domain":"example.test","region":"eu-west-1"}`)
			want := errors.New("hosts connection failed")
			deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
				calls++
				if profile != "example.test" || region != "eu-west-1" {
					t.Fatalf("cloud received %q, %q", profile, region)
				}
				return cloud.Clients{}, want
			}
			assertResult(t, invokeWithDeps(deps, "hosts", subcommand, "us"), 1, "", "devctl: hosts connection failed\n")
			if calls != 1 {
				t.Fatalf("cloud calls = %d; want one", calls)
			}
			err := hosts.Run(t.Context(), []string{subcommand, "us"}, io.Discard, deps)
			if !identicalHostsError(err, want) {
				t.Fatalf("cloud error = %v; want identical error", err)
			}

			deps = checkoutDeps(t, `{"domain":"example.test"}`)
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				t.Fatal("cloud reached with an invalid root file")
				return cloud.Clients{}, nil
			}
			assertResult(t, invokeWithDeps(deps, "hosts", subcommand, "Us_1"), 2, "", "devctl: infra/terraform.tfvars.json: missing 'region'\n")
		})
	}
}

func identicalHostsError(got, want error) bool {
	return reflect.ValueOf(got).Equal(reflect.ValueOf(want))
}

func TestHostsErrorShapes(t *testing.T) {
	// R-OHFO-9B19 R-OINK-N2RY
	missing := hosts.NoRoleError(struct{ Domain string }{Domain: "gone.ikigenba.dev"})
	if got := missing.Error(); got != "no role for 'gone.ikigenba.dev'" {
		t.Fatal(got)
	}
	var diagnostic error = &missing
	if _, ok := diagnostic.(interface{ ExitCode() int }); ok {
		t.Fatal("NoRoleError implements ExitCode")
	}
	if _, ok := diagnostic.(interface{ Detail() string }); ok {
		t.Fatal("NoRoleError implements Detail")
	}
	operator := hosts.OperatorError(struct {
		Domain string
		Label  string
	}{Domain: hostsUS, Label: "us"})
	if operator.Error() != "'us.ikigenba.dev' is the hosts operator" ||
		operator.ExitCode() != 1 || operator.Detail() != "run 'devctl hosts clear us' first" {
		t.Fatalf("operator diagnostic = %q, %d, %q", operator.Error(), operator.ExitCode(), operator.Detail())
	}
}

type hostsTestFixture struct {
	cloud.EC2
	cloud.IAM
	calls     []string
	outputs   []string
	out       *bytes.Buffer
	instances []cloud.Instance
	addresses []cloud.Address
	role      bool
	tagged    []string
	attached  bool
	tag       string
	tagExists bool
	denyEC2   bool
	failAt    string
	err       error
}

func (f *hostsTestFixture) record(method string) error {
	f.calls = append(f.calls, method)
	if f.out != nil {
		f.outputs = append(f.outputs, f.out.String())
	}
	if f.failAt == method {
		return f.err
	}
	return nil
}

func (f *hostsTestFixture) assertCalls(t *testing.T, want ...string) {
	t.Helper()
	if !slices.Equal(f.calls, want) {
		t.Fatalf("calls = %q; want %q", f.calls, want)
	}
}

func (f *hostsTestFixture) deps(t *testing.T) seam.Deps {
	t.Helper()
	deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	git := deps.Exec
	deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if err := f.record(cmd.Path + " " + strings.Join(cmd.Args, " ")); err != nil {
			return seam.Result{}, err
		}
		return git(ctx, cmd)
	}
	deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		t.Fatal("unexpected streaming process")
		return seam.Result{}, nil
	}
	deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
		if err := f.record("connect " + profile + " " + region); err != nil {
			return cloud.Clients{}, err
		}
		clients := cloud.Clients{STS: f, EC2: f, IAM: f}
		if f.denyEC2 {
			clients.EC2 = nil
		}
		return clients, nil
	}
	return deps
}

func (f *hostsTestFixture) CallerAccountID(context.Context) (string, error) {
	return "123456789012", f.record("CallerAccountID")
}

func (f *hostsTestFixture) ListSpaceInstances(_ context.Context, root string) ([]cloud.Instance, error) {
	return f.instances, f.record("ListSpaceInstances " + root)
}

func (f *hostsTestFixture) ListSpaceAddresses(_ context.Context, root string) ([]cloud.Address, error) {
	return f.addresses, f.record("ListSpaceAddresses " + root)
}

func (f *hostsTestFixture) RoleExists(_ context.Context, role string) (bool, error) {
	return f.role, f.record("RoleExists " + role)
}

func (f *hostsTestFixture) ManagedPolicy(_ context.Context, policy string) (string, error) {
	return hostsARN, f.record("ManagedPolicy " + policy)
}

func (f *hostsTestFixture) RolesTagged(_ context.Context, key, value string) ([]string, error) {
	return f.tagged, f.record("RolesTagged " + key + " " + value)
}

func (f *hostsTestFixture) TagRole(_ context.Context, role, key, value string) error {
	return f.record("TagRole " + role + " " + key + " " + value)
}

func (f *hostsTestFixture) AttachRolePolicy(_ context.Context, role, arn string) error {
	return f.record("AttachRolePolicy " + role + " " + arn)
}

func (f *hostsTestFixture) AttachedPolicy(_ context.Context, role, name string) (string, bool, error) {
	return hostsARN, f.attached, f.record("AttachedPolicy " + role + " " + name)
}

func (f *hostsTestFixture) DetachRolePolicy(_ context.Context, role, arn string) error {
	return f.record("DetachRolePolicy " + role + " " + arn)
}

func (f *hostsTestFixture) RoleTag(_ context.Context, role, key string) (string, bool, error) {
	return f.tag, f.tagExists, f.record("RoleTag " + role + " " + key)
}

func (f *hostsTestFixture) UntagRole(_ context.Context, role, key string) error {
	return f.record("UntagRole " + role + " " + key)
}

func hostsSetCalls(role string) []string {
	return []string{
		"git rev-parse --show-toplevel", "connect ikigenba.dev us-east-2", "CallerAccountID",
		"ListSpaceInstances " + hostsRoot, "ListSpaceAddresses " + hostsRoot,
		"RoleExists " + role, "ManagedPolicy " + hostsName, "RolesTagged HostsOperator true",
		"TagRole " + role + " HostsOperator true", "AttachRolePolicy " + role + " " + hostsARN,
	}
}

func hostsClearCalls(role string) []string {
	return []string{
		"git rev-parse --show-toplevel", "connect ikigenba.dev us-east-2", "CallerAccountID",
		"RoleExists " + role, "AttachedPolicy " + role + " " + hostsName,
		"DetachRolePolicy " + role + " " + hostsARN, "RoleTag " + role + " HostsOperator",
		"UntagRole " + role + " HostsOperator",
	}
}

func (f *hostsTestFixture) invoke(t *testing.T, subcommand, operand string) runResult {
	t.Helper()
	f.calls = nil
	f.outputs = nil
	var stdout, stderr bytes.Buffer
	f.out = &stdout
	deps := f.deps(t)
	code := Run(t.Context(), []string{"hosts", subcommand, operand}, nil, &stdout, &stderr, deps)
	return runResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func (f *hostsTestFixture) invokeDirect(t *testing.T, subcommand, operand string) (string, error) {
	t.Helper()
	f.calls = nil
	f.outputs = nil
	var out bytes.Buffer
	f.out = &out
	err := hosts.Run(t.Context(), []string{subcommand, operand}, &out, f.deps(t))
	return out.String(), err
}

func (f *hostsTestFixture) assertOutputAtCalls(t *testing.T, firstLineAt int, firstLine string) {
	t.Helper()
	for i, output := range f.outputs {
		want := ""
		if i >= firstLineAt {
			want = firstLine
		}
		if output != want {
			t.Errorf("stdout at %q = %q; want %q", f.calls[i], output, want)
		}
	}
}

func TestHostsSetAcceptsExistingSpaceInAnyState(t *testing.T) {
	// R-OG7R-VJAK R-OJVH-0UIN R-OPYY-XP84 R-OUUK-GS6W R-HD9L-YLGY
	// R-HEHI-CD7N R-MSRG-CO4Z R-OZQ5-ZV5O R-MTZC-QFVO
	for _, test := range []struct {
		name      string
		operand   string
		state     cloud.InstanceState
		addresses []cloud.Address
		tagged    []string
		attached  bool
	}{
		{name: "running label", operand: "us", state: cloud.StateRunning},
		{name: "running domain", operand: hostsUS, state: cloud.StateRunning},
		{name: "stopped", operand: "us", state: cloud.StateStopped},
		{
			name: "address only", operand: "us", state: cloud.StateTerminated,
			addresses: []cloud.Address{{AllocationID: "eipalloc-us", IP: "18.118.7.42", Space: hostsUS}},
		},
		{name: "already set", operand: "us", state: cloud.StateRunning, tagged: []string{hostsUS}, attached: true},
		{
			name: "unrelated roles", operand: "us", state: cloud.StateRunning,
			tagged: []string{hostsUS, "some-other-role", "crm.us.ikigenba.dev"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := hostsTestFixture{
				instances: []cloud.Instance{{ID: "i-us", Space: hostsUS, State: test.state}},
				addresses: test.addresses, role: true, tagged: test.tagged, attached: test.attached,
			}
			assertResult(t, f.invoke(t, "set", test.operand), 0, setOutput, "")
			wantCalls := hostsSetCalls(hostsUS)
			f.assertCalls(t, wantCalls...)
			f.assertOutputAtCalls(t, len(wantCalls)-1, setTagLine)

			// Compare the public package entry point with the CLI dispatch.
			out, err := f.invokeDirect(t, "set", test.operand)
			if err != nil || out != setOutput {
				t.Fatalf("hosts.Run = %v, stdout = %q; want nil and %q", err, out, setOutput)
			}
			f.assertCalls(t, wantCalls...)
		})
	}
}

func TestHostsSetRefusesBeforeMutation(t *testing.T) {
	// R-OPYY-XP84 R-OR6V-BGYT R-OSER-P8PI R-HC1P-KTQ9 R-HD9L-YLGY
	for _, test := range []struct {
		name, operand string
		exists, role  bool
		tagged        []string
		failAt        string
		err           error
		wantCalls     int
		stderr        string
		operator      string
	}{
		{
			name: "no space", operand: "gone", wantCalls: 5,
			stderr: "devctl: no space at 'gone.ikigenba.dev'\n",
		},
		{
			name: "no role", operand: "us", exists: true, wantCalls: 6,
			stderr: "devctl: no role for 'us.ikigenba.dev'\n",
		},
		{
			name: "no managed policy", operand: "us", exists: true, role: true,
			failAt: "ManagedPolicy " + hostsName, err: &cloud.NotFoundError{Kind: "managed policy", Name: hostsName},
			wantCalls: 7, stderr: "devctl: no managed policy 'ikigenba.dev-hosts-operator'\n",
		},
		{
			name: "another operator", operand: "sbx1", exists: true, role: true,
			tagged: []string{hostsUS}, wantCalls: 8, operator: "us",
			stderr: "devctl: 'us.ikigenba.dev' is the hosts operator\n\nrun 'devctl hosts clear us' first\n",
		},
		{
			name: "first operator in byte order", operand: "sbx1", exists: true, role: true,
			tagged: []string{hostsUS, "prod.ikigenba.dev", "sbx2.ikigenba.dev"}, wantCalls: 8, operator: "prod",
			stderr: "devctl: 'prod.ikigenba.dev' is the hosts operator\n\nrun 'devctl hosts clear prod' first\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			domain := test.operand + "." + hostsRoot
			state := cloud.StateTerminated
			if test.exists {
				state = cloud.StateRunning
			}
			f := hostsTestFixture{
				instances: []cloud.Instance{{ID: "i-space", Space: domain, State: state}},
				role:      test.role, tagged: test.tagged, failAt: test.failAt, err: test.err,
			}
			assertResult(t, f.invoke(t, "set", test.operand), 1, "", test.stderr)
			f.assertCalls(t, hostsSetCalls(domain)[:test.wantCalls]...)
			f.assertOutputAtCalls(t, test.wantCalls, "")
			if test.operator != "" {
				out, err := f.invokeDirect(t, "set", test.operand)
				var operator *hosts.OperatorError
				if !errors.As(err, &operator) || !identicalHostsError(err, operator) ||
					operator.Domain != test.operator+"."+hostsRoot || operator.Label != test.operator || out != "" {
					t.Fatalf("hosts.Run returned %#v with stdout %q", err, out)
				}
			}
			if test.exists && !test.role {
				f.assertNoRole(t, "set", test.operand, domain)
			}
		})
	}
}

func TestHostsClearRemovesAttachmentBeforeTag(t *testing.T) {
	// R-P3DV-56DR R-P4LR-IY4G R-MWF5-HZD2 R-P71K-AHLU R-MXN1-VR3R
	for _, test := range []struct {
		name, operand, value string
		instances            []cloud.Instance
	}{
		{name: "role without space", operand: "us", value: "true"},
		{
			name: "domain with space", operand: hostsUS, value: "true",
			instances: []cloud.Instance{{ID: "i-us", Space: hostsUS, State: cloud.StateRunning}},
		},
		{name: "tag with other value", operand: "us", value: "false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := hostsTestFixture{
				role: true, attached: true, tagExists: true, tag: test.value, instances: test.instances,
				denyEC2: len(test.instances) == 0,
			}
			assertResult(t, f.invoke(t, "clear", test.operand), 0, clearOutput, "")
			f.assertCalls(t, hostsClearCalls(hostsUS)...)
			f.assertOutputAtCalls(t, 6, clearRoleLine)
		})
	}

	f := hostsTestFixture{role: true, denyEC2: true}
	assertResult(t, f.invoke(t, "clear", "sbx1"), 0, "role: ok (already detached)\ntag: ok (already gone)\n", "")
	want := hostsClearCalls("sbx1.ikigenba.dev")
	f.assertCalls(t, append(want[:5], want[6])...)
	f.assertOutputAtCalls(t, 5, "role: ok (already detached)\n")

	f = hostsTestFixture{denyEC2: true}
	assertResult(t, f.invoke(t, "clear", "gone"), 1, "", "devctl: no role for 'gone.ikigenba.dev'\n")
	f.assertCalls(t, hostsClearCalls("gone.ikigenba.dev")[:4]...)
	f.assertNoRole(t, "clear", "gone", "gone.ikigenba.dev")
}

func (f *hostsTestFixture) assertNoRole(t *testing.T, subcommand, operand, domain string) {
	t.Helper()
	out, err := f.invokeDirect(t, subcommand, operand)
	var missing *hosts.NoRoleError
	if !errors.As(err, &missing) || !identicalHostsError(err, missing) || missing.Domain != domain || out != "" {
		t.Fatalf("hosts.Run returned %#v with stdout %q; want NoRoleError for %q", err, out, domain)
	}
}

func TestHostsReturnsFailedCallUnchangedAndStops(t *testing.T) {
	// R-6I8R-BGWE R-OPYY-XP84 R-OR6V-BGYT R-OSER-P8PI R-HC1P-KTQ9 R-HEHI-CD7N
	// R-MSRG-CO4Z R-OZQ5-ZV5O R-P3DV-56DR R-P4LR-IY4G R-MWF5-HZD2 R-P71K-AHLU
	for _, test := range []struct {
		subcommand string
		calls      []string
		steps      []int
		completed  func(int) string
	}{
		{
			subcommand: "set", calls: hostsSetCalls(hostsUS), steps: []int{2, 3, 4, 5, 6, 7, 8, 9},
			completed: func(step int) string {
				if step == 9 {
					return setTagLine
				}
				return ""
			},
		},
		{
			subcommand: "clear", calls: hostsClearCalls(hostsUS), steps: []int{2, 3, 4, 5, 6, 7},
			completed: func(step int) string {
				if step >= 6 {
					return clearRoleLine
				}
				return ""
			},
		},
	} {
		for _, step := range test.steps {
			t.Run(test.subcommand+"/"+test.calls[step], func(t *testing.T) {
				name, _, _ := strings.Cut(test.calls[step], " ")
				// Use provider failures at mutation seams and unique errors at all other seams.
				wantErr := errors.New("failed at " + name)
				if name == "AttachRolePolicy" || name == "DetachRolePolicy" {
					wantErr = &cloud.Error{Service: "iam", Operation: name, Code: "Throttling"}
				}
				f := hostsTestFixture{
					role: true, attached: true, tagExists: true, tag: "true",
					instances: []cloud.Instance{{ID: "i-us", Space: hostsUS, State: cloud.StateRunning}},
					failAt:    test.calls[step], err: wantErr,
				}
				out, err := f.invokeDirect(t, test.subcommand, "us")
				if !identicalHostsError(err, wantErr) || out != test.completed(step) {
					t.Fatalf("hosts.Run = %v, stdout %q; want same error %v and stdout %q",
						err, out, wantErr, test.completed(step))
				}
				f.assertCalls(t, test.calls[:step+1]...)

				got := f.invoke(t, test.subcommand, "us")
				assertResult(t, got, 1, test.completed(step), "devctl: "+wantErr.Error()+"\n")
				f.assertCalls(t, test.calls[:step+1]...)
			})
		}
	}
}
