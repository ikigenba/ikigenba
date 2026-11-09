package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestCreateApexUsageThroughCLI(t *testing.T) {
	// R-3B9D-MSO3
	for _, args := range [][]string{
		{"sbx1", "--acme-email=x", "--apex", "dummy", "--no-apex"},
		{"sbx1", "--no-apex", "--acme-email=x", "--apex=dummy"},
		{"sbx1", "--acme-email=x", "--apex"}, {"sbx1", "--acme-email=x", "--apex="},
	} {
		calls := 0
		message := "option '--apex' requires a value"
		if len(args) > 3 && (args[1] == "--no-apex" || args[len(args)-1] == "--no-apex") {
			message = "space create takes --apex or --no-apex, not both"
		}
		assertResult(t, invokeWithDeps(noExternalDeps(&calls), append([]string{"space", "create"}, args...)...), 2, "", "devctl: "+message+"\n\nsee 'devctl space --help' for usage\n")
		if calls != 0 {
			t.Fatal(calls)
		}
	}
	t.Run("apex before operand reaches checkout", func(t *testing.T) {
		external := 0
		deps := noExternalDeps(&external)
		deps.Dir = t.TempDir()
		var commands []seam.Cmd
		deps.Exec = func(_ context.Context, c seam.Cmd) (seam.Result, error) {
			commands = append(commands, c)
			return seam.Result{}, errors.New("checkout sentinel")
		}
		assertResult(t, invokeWithDeps(deps, "space", "create", "--apex", "dummy", "sbx1", "--acme-email", "ops@ikigenba.dev"), 1, "", "devctl: git rev-parse --show-toplevel: checkout sentinel\n")
		want := []seam.Cmd{{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: deps.Dir}}
		if !reflect.DeepEqual(commands, want) || external != 0 {
			t.Fatalf("commands %#v, other external calls %d", commands, external)
		}
	})
}

func TestCreateRegistryRefusalsThroughCLI(t *testing.T) {
	// R-3CHA-0KES
	for _, tc := range []struct {
		instances []cloud.Instance
		addresses []cloud.Address
		message   string
	}{
		{instances: []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: testDomain, State: cloud.StateRunning}}, message: "a space at 'sbx1.ikigenba.dev' already exists (i-0c9e94542d98846a8)"},
		{instances: []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: testDomain, State: cloud.StateRunning}}, addresses: []cloud.Address{{Space: testDomain, IP: "18.118.7.42", AllocationID: "eipalloc-01", InstanceID: "i-0c9e94542d98846a8"}}, message: "a space at 'sbx1.ikigenba.dev' already exists (i-0c9e94542d98846a8)"},
		{instances: []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: testDomain, State: cloud.StateTerminated}}, addresses: []cloud.Address{{Space: testDomain, IP: "18.118.7.42", AllocationID: "eipalloc-01"}}, message: "a space at 'sbx1.ikigenba.dev' already exists (elastic ip 18.118.7.42)"},
		{addresses: []cloud.Address{{Space: testDomain, IP: "18.190.33.61"}, {Space: testDomain, IP: "18.118.7.42"}}, message: "'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)"},
	} {
		f := &registryCLI{commandHarness: newCommandHarness(t), instances: tc.instances, addresses: tc.addresses}
		deps := f.deps()
		exec := deps.Exec
		deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
			if c.Path != "git" || c.Args[0] == "worktree" {
				t.Fatalf("effect before registry refusal: %#v", c)
			}
			return exec(ctx, c)
		}
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			return cloud.Clients{STS: f, EC2: f, IAM: f, Route53: f}, nil
		}
		assertResult(t, invokeWithDeps(deps, "space", "create", "sbx1", "--acme-email=x"), 2, "", "devctl: "+tc.message+"\n")
		if len(f.mutations) != 0 {
			t.Fatal(f.mutations)
		}
	}
}

func TestCreateAbsentApexAppThroughCLI(t *testing.T) {
	// R-26O1-7ZW4
	for _, unlabelled := range []bool{false, true} {
		f := newCompleteCreateFake(t)
		deps := f.deps()
		args := []string{"space", "create", "sbx1", "--acme-email=x", "--apex=crm"}
		sha, name := createSHA, "release-87"
		if unlabelled {
			sha = "9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c"
			name = "dist/" + sha + ".tar.xz"
			args = append(args, "--release=9e1c7a3")
			exec := deps.Exec
			deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
				if c.Path == "git" && len(c.Args) > 1 && c.Args[1] == "--verify" {
					return seam.Result{Stdout: []byte(sha + "\n")}, nil
				}
				return exec(ctx, c)
			}
		}
		got := invokeWithDeps(deps, args...)
		buildName := "release-87, dist/" + sha + ".tar.xz"
		if unlabelled {
			buildName = name
		}
		assertResult(t, got, 1, "account: ok (ikigenba.dev, us-east-2, 123)\ndomain: ok (zone ikigenba.dev Z)\nbuild: ok ("+buildName+")\n", "devctl: 'crm' is not an app in "+name+"\n")
		if len(f.mutations) != 0 || len(f.parameters) != 0 || len(f.commands) != 0 {
			t.Fatal(f.mutations, f.parameters, f.commands)
		}
		if _, err := os.Stat(filepath.Join(f.checkoutRoot, "dist", sha+".tar.xz")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCreateResolverFailureThroughCLI(t *testing.T) {
	// R-3L0K-OYLN
	f := newCompleteCreateFake(t)
	deps := f.deps()
	deps.LookupHost = func(domain string) ([]string, error) {
		if domain != testDomain {
			t.Fatal(domain)
		}
		return []string{"203.0.113.9"}, nil
	}
	got := invokeWithDeps(deps, "space", "create", "sbx1", "--acme-email=x")
	want := createInitialLines() + "secrets: ok (8 apps)\nrole: ok (sbx1.ikigenba.dev)\ninstance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)\naddress: ok (elastic ip 18.118.7.42 associated)\nrecords: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)\n"
	assertResult(t, got, 1, want, "devctl: resolve: sbx1.ikigenba.dev does not resolve to 18.118.7.42\n")
	if len(f.commands) != 0 {
		t.Fatal(f.commands)
	}
}

func TestCreateKnownHostsFailuresThroughCLI(t *testing.T) {
	// R-ONQA-V0EN R-9JPE-20LM R-OOY7-8S5C
	for _, failure := range []string{"absent", "stat", "ssh-keygen", "present-label", "present-domain"} {
		t.Run(failure, func(t *testing.T) {
			f := newCompleteCreateFake(t)
			deps := f.deps()
			getenv := deps.Getenv
			deps.Getenv = func(key string) string {
				if key == "HOME" {
					return "/home/dev"
				}
				return getenv(key)
			}
			var paths []string
			deps.Stat = func(path string) (fs.FileInfo, error) {
				paths = append(paths, path)
				switch failure {
				case "absent":
					return nil, fmt.Errorf("injected filesystem error: %w", fs.ErrNotExist)
				case "stat":
					return nil, fmt.Errorf("injected filesystem error: %w", fs.ErrPermission)
				default:
					return nil, nil
				}
			}
			exec := deps.Exec
			keygens := 0
			failed := false
			deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
				if failed {
					t.Fatalf("command after known_hosts failure %#v", c)
				}
				if c.Path == "ssh-keygen" {
					keygens++
					if !reflect.DeepEqual(c.Args, []string{"-R", testDomain}) || c.Dir != deps.Dir || len(c.Env) != 0 {
						t.Fatal(c)
					}
					if strings.HasPrefix(failure, "present-") {
						return seam.Result{Stdout: []byte("arbitrary-keygen-out"), Stderr: []byte("arbitrary-keygen-err")}, nil
					}
					failed = true
					return seam.Result{ExitCode: 255, Stderr: []byte("hostkeys_foreach failed for /home/dev/.ssh/known_hosts: Permission denied")}, nil
				}
				return exec(ctx, c)
			}
			operand := "sbx1"
			if failure == "present-domain" {
				operand = testDomain
			}
			got := invokeWithDeps(deps, "space", "create", operand, "--acme-email=x")
			if !reflect.DeepEqual(paths, []string{"/home/dev/.ssh/known_hosts"}) {
				t.Fatal(paths)
			}
			if failure == "absent" || strings.HasPrefix(failure, "present-") {
				wantKeygens := 1
				if failure == "absent" {
					wantKeygens = 0
				}
				if got.code != 0 || got.stderr != "" || keygens != wantKeygens || strings.Contains(got.stdout, "arbitrary-keygen-") || !strings.Contains(got.stdout, "restore: ok (no host backup)\nknown_hosts: ok (removed sbx1.ikigenba.dev)\ninit: ok\n") {
					t.Fatal(got, keygens)
				}
				return
			}
			if got.code != 1 || !strings.HasSuffix(got.stdout, "restore: ok (no host backup)\n") || strings.Contains(strings.Join(f.remotes, " "), "'init'") {
				t.Fatal(got)
			}
			if failure == "ssh-keygen" {
				assertResult(t, got, 1, got.stdout, "devctl: known_hosts: ssh-keygen -R sbx1.ikigenba.dev: exit status 255\n\n> hostkeys_foreach failed for /home/dev/.ssh/known_hosts: Permission denied\n")
			} else if keygens != 0 || !strings.HasPrefix(got.stderr, "devctl: ") || !strings.Contains(got.stderr, "/home/dev/.ssh/known_hosts") {
				t.Fatal(got, keygens)
			}
		})
	}
}

type createApexCLI struct {
	*completeCreateFake
	state    cloud.InstanceState
	policies int
}

func (f *createApexCLI) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return []cloud.Instance{{ID: "i-previous", Space: "sbx2.ikigenba.dev", State: f.state, Address: "18.220.10.5"}}, nil
}
func (f *createApexCLI) ListSpaceAddresses(context.Context, string) ([]cloud.Address, error) {
	return []cloud.Address{{IP: "18.220.10.5", Space: "sbx2.ikigenba.dev", AllocationID: "eipalloc-previous", InstanceID: "i-previous"}}, nil
}
func (f *createApexCLI) FindRecord(_ context.Context, _ string, name, kind string) (cloud.Record, bool, error) {
	if name == "ikigenba.dev" && kind == "A" {
		return cloud.Record{Name: name, Type: kind, TTL: 60, Values: []string{"18.220.10.5"}}, true, nil
	}
	return cloud.Record{}, false, nil
}
func (f *createApexCLI) PutRolePolicy(context.Context, string, string, string) error {
	f.policies++
	return nil
}
func TestCreateApexPreviousHolderThroughCLI(t *testing.T) {
	// R-NW4G-ELAI R-NYK9-64RW
	for _, state := range []cloud.InstanceState{cloud.StateStopped, cloud.StateRunning} {
		t.Run(string(state), func(t *testing.T) {
			f := &createApexCLI{completeCreateFake: newCompleteCreateFake(t), state: state}
			deps := f.deps()
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				return cloud.Clients{STS: f, EC2: f, IAM: f, Route53: f, SSM: f, S3: f}, nil
			}
			got := invokeWithDeps(deps, "space", "create", "sbx1", "--acme-email=x", "--apex=dummy")
			if state == cloud.StateStopped {
				if got.code != 1 || !strings.HasSuffix(got.stdout, "known_hosts: ok (removed sbx1.ikigenba.dev)\napex: ok (host.apex=dummy)\n") || f.policies != 1 {
					t.Fatal(got, f.policies)
				}
				assertResult(t, got, 1, got.stdout, "devctl: 'sbx2.ikigenba.dev' holds the apex and is stopped\n\nrun 'devctl space start sbx2' first\n")
				for _, remote := range f.remotes {
					if strings.HasSuffix(remote, "'init'") {
						t.Fatal(remote)
					}
				}
			} else {
				if got.code != 0 || got.stderr != "" || !strings.HasSuffix(got.stdout, "activation output\nrecord: ok (ikigenba.dev -> 18.118.7.42, INSYNC)\nprevious: ok (sbx2.ikigenba.dev cleared)\nsbx1.ikigenba.dev 18.118.7.42\n") {
					t.Fatal(got)
				}
				if !strings.Contains(got.stdout, "apex: ok (host.apex=dummy)\nrole: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\ninit: ok\n") {
					t.Fatal(got)
				}
				previous := false
				for _, cmd := range f.commands {
					if cmd.Path == "ssh" && cmd.Args[len(cmd.Args)-2] == "ec2-user@sbx2.ikigenba.dev" && cmd.Args[len(cmd.Args)-1] == "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'opsctl' 'config' 'del' 'host.apex'" {
						previous = true
					}
				}
				if !previous {
					t.Fatal("previous holder not cleared")
				}
			}
		})
	}
}
