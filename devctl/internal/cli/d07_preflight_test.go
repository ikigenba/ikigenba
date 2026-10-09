package cli

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

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/spacecreate"
)

const testDomain = "sbx1.ikigenba.dev"

const expectedCreateUsage = `Usage: devctl space create <space> --acme-email <address> [--release <sha|tag>]

Create the space and deploy a release to it: build the release, push its
apps' secrets, make the role, launch the instance with an Elastic IP, write
its records, copy and unpack the release on the host, have the release's
opsctl set the ten host keys, restore the space's own backups when the bucket
holds a host backup, run init, and activate the release. Completed steps
remain on failure; use space destroy to clean up.

Options:
  --acme-email <address>  where the CA sends the space's expiry warnings; required
  --release <sha|tag>     the release to deploy; the newest r<N> tag otherwise
`

func TestCreateCLIUsagePrecedesCheckout(t *testing.T) {
	// R-8UTA-21TK R-8X92-TLAY R-UXYX-19YV R-UWR0-NI86
	deps := seam.Deps{Dir: t.TempDir(), EUID: 1, Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("unexpected exec")
		return seam.Result{}, nil
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud")
		return cloud.Clients{}, nil
	}}
	for _, tc := range []struct {
		args           []string
		code           int
		stdout, stderr string
	}{
		{[]string{"space", "create"}, 2, "", "devctl: space create needs <space>\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "create", "sbx1"}, 2, "", "devctl: space create needs --acme-email <address>\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "create", "sbx1", "--acme-email"}, 2, "", "devctl: option '--acme-email' requires a value\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "create", "sbx1", "--acme-email=x", "--release"}, 2, "", "devctl: option '--release' requires a value\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "create", "sbx1", "--acme-email=x", "--release="}, 2, "", "devctl: option '--release' requires a value\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "create", "--help"}, 0, expectedCreateUsage, ""},
		{[]string{"space", "create", "-h"}, 0, expectedCreateUsage, ""},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr, deps); code != tc.code || stdout.String() != tc.stdout || stderr.String() != tc.stderr {
			t.Fatalf("Run(%q) = %d, %q, %q", tc.args, code, stdout.String(), stderr.String())
		}
	}
}

// commandHarness remains shared with the existing secret-safety regression.
type commandHarness struct {
	t             *testing.T
	checkoutRoot  string
	mutations     []string
	createRoleErr error
}

func newCommandHarness(t *testing.T) *commandHarness {
	h := &commandHarness{t: t, checkoutRoot: t.TempDir()}
	h.write(filepath.Join("infra", "terraform.tfvars.json"), `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	return h
}

func (h *commandHarness) write(relative, value string) {
	h.t.Helper()
	path := filepath.Join(h.checkoutRoot, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		h.t.Fatal(err)
	}
}
func (h *commandHarness) addAppWithSecrets(name string, names ...string) {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "\"" + n + "\""
	}
	h.write(filepath.Join(name, "cmd", name, "main.go"), "package main\n")
	h.write(filepath.Join(name, "etc", "manifest.toml"), "app = \""+name+"\"\nsecrets = ["+strings.Join(quoted, ",")+"]\n")
}
func (h *commandHarness) deps() seam.Deps {
	return seam.Deps{Dir: h.checkoutRoot, EUID: 1, Getenv: func(string) string { return "" }, After: immediateD07After, Now: func() time.Time { return time.Unix(1, 0) }, Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
		if result, handled, err := fakeCreateBuild(h.t, h.checkoutRoot, c); handled {
			return result, err
		}

		if c.Path != "ssh" && c.Path != "scp" {
			h.t.Fatalf("unexpected command %#v", c)
		}
		return seam.Result{}, nil
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{EC2: h, SSM: h, Route53: h, S3: h, IAM: h, STS: h}, nil
	}}
}

func immediateD07After(time.Duration) <-chan time.Time {
	ready := make(chan time.Time)
	close(ready)
	return ready
}

func (h *commandHarness) CallerAccountID(context.Context) (string, error)        { return "123", nil }
func (h *commandHarness) LaunchTemplate(context.Context, string) (string, error) { return "lt", nil }
func (h *commandHarness) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return nil, nil
}
func (h *commandHarness) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (h *commandHarness) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (h *commandHarness) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) {
	return true, nil
}
func (h *commandHarness) StartInstance(context.Context, string) error     { return nil }
func (h *commandHarness) StopInstance(context.Context, string) error      { return nil }
func (h *commandHarness) TerminateInstance(context.Context, string) error { return nil }
func (h *commandHarness) InstanceChecksPassed(context.Context, string) (bool, error) {
	return true, nil
}
func (h *commandHarness) ListSpaceAddresses(context.Context, string) ([]cloud.Address, error) {
	return nil, nil
}
func (h *commandHarness) AllocateAddress(context.Context, string, string) (cloud.Address, error) {
	return cloud.Address{}, nil
}
func (h *commandHarness) AssociateAddress(context.Context, string, string) error { return nil }
func (h *commandHarness) DisassociateAddress(context.Context, string) error      { return nil }
func (h *commandHarness) ReleaseAddress(context.Context, string) error           { return nil }
func (h *commandHarness) GetParameter(context.Context, string) (string, error)   { return "", nil }
func (h *commandHarness) PutSecureParameter(_ context.Context, name, _ string) error {
	h.mutations = append(h.mutations, "ssm:put:"+filepath.Base(name))
	return nil
}
func (h *commandHarness) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, nil
}
func (h *commandHarness) DeleteParameter(context.Context, string) error { return nil }
func (h *commandHarness) Zone(context.Context, string) (cloud.Zone, error) {
	return cloud.Zone{ID: "Z", Name: "ikigenba.dev"}, nil
}
func (h *commandHarness) ListRecords(context.Context, string) ([]cloud.Record, error) {
	return nil, nil
}
func (h *commandHarness) FindRecord(context.Context, string, string, string) (cloud.Record, bool, error) {
	return cloud.Record{}, false, nil
}
func (h *commandHarness) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	return "c", nil
}
func (h *commandHarness) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	return cloud.ChangeInsync, nil
}
func (h *commandHarness) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	return nil, nil
}
func (h *commandHarness) PutObject(context.Context, string, string, io.Reader, int64) error {
	return nil
}
func (h *commandHarness) DeleteObjects(context.Context, string, []string) error { return nil }
func (h *commandHarness) PermissionsBoundary(context.Context, string) (string, error) {
	return "arn", nil
}
func (h *commandHarness) RoleExists(context.Context, string) (bool, error) { return false, nil }
func (h *commandHarness) CreateRole(context.Context, cloud.RoleSpec) error {
	h.mutations = append(h.mutations, "iam:create-role")
	return h.createRoleErr
}
func (h *commandHarness) PutRolePolicy(context.Context, string, string, string) error { return nil }
func (h *commandHarness) DeleteRolePolicy(context.Context, string, string) error      { return nil }
func (h *commandHarness) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	return nil, false, nil
}
func (h *commandHarness) CreateInstanceProfile(context.Context, string) error            { return nil }
func (h *commandHarness) AddRoleToInstanceProfile(context.Context, string, string) error { return nil }
func (h *commandHarness) RemoveRoleFromInstanceProfile(context.Context, string, string) error {
	return nil
}
func (h *commandHarness) DeleteInstanceProfile(context.Context, string) error { return nil }
func (h *commandHarness) DeleteRole(context.Context, string) error            { return nil }

func TestRootBoundOperationProfilesThroughCLI(t *testing.T) {
	//
	for _, args := range [][]string{{"space", "list"}, {"space", "destroy", "sbx1"}, {"space", "stop", "sbx1"}, {"space", "start", "sbx1"}, {"space", "status", "sbx1"}, {"apex", "set", "crm.sbx1"}, {"apex", "show"}, {"apex", "clear"}} {
		h := newCommandHarness(t)
		h.write(filepath.Join("infra", "terraform.tfvars.json"), `{"domain":"example.test","region":"eu-west-1"}`)
		deps := h.deps()
		calls := 0
		deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
			calls++
			if profile != "example.test" || region != "eu-west-1" {
				t.Fatalf("%q cloud = %q, %q", args, profile, region)
			}
			return cloud.Clients{}, errors.New("profile sentinel")
		}
		assertResult(t, invokeWithDeps(deps, args...), 1, "", "devctl: profile sentinel\n")
		if calls != 1 {
			t.Fatalf("%q cloud calls = %d", args, calls)
		}
	}
}

func TestInvalidSpaceAndAppNeverConnectThroughCLI(t *testing.T) {
	// R-S8UF-C8XM
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"space", "create", "golden", "--acme-email", "ops@ikigenba.dev"}, "'golden' is not a usable space label: golden/ holds the golden sets"},
		{[]string{"space", "status", "golden.ikigenba.dev"}, "'golden' is not a usable space label: golden/ holds the golden sets"},
		{[]string{"space", "create", "crm.sbx1", "--acme-email", "ops@ikigenba.dev"}, "'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'"},
		{[]string{"space", "status", "crm.sbx1"}, "'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'"},
		{[]string{"apex", "set", "crm.sbx1.example.com"}, "'crm.sbx1.example.com' is not an app on a space: <app>.<space>"},
	} {
		h := newCommandHarness(t)
		deps := h.deps()
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("unexpected cloud call")
			return cloud.Clients{}, nil
		}
		assertResult(t, invokeWithDeps(deps, tc.args...), 2, "", "devctl: "+tc.want+"\n")
	}
}

func TestCreateConnectsOnceAndStopsOnIdentityFailureThroughCLI(t *testing.T) {
	// R-UZ6T-F1PK
	//
	for _, operand := range []string{"sbx1", "sbx1.ikigenba.dev"} {
		h := newCommandHarness(t)
		deps := h.deps()
		calls := 0
		sts := &preflightSTS{}
		deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
			calls++
			if profile != "ikigenba.dev" || region != "us-east-2" {
				t.Fatalf("cloud = %q, %q", profile, region)
			}
			return cloud.Clients{STS: sts}, nil
		}
		assertResult(t, invokeWithDeps(deps, "space", "create", operand, "--acme-email", "ops@ikigenba.dev"), 1, "", "devctl: sts GetCallerIdentity: identity sentinel\n")
		if calls != 1 || sts.calls != 1 {
			t.Fatalf("cloud calls = %d, sts calls = %d", calls, sts.calls)
		}
	}
}

type preflightSTS struct{ calls int }

func (s *preflightSTS) CallerAccountID(context.Context) (string, error) {
	s.calls++
	return "", &cloud.Error{Service: "sts", Operation: "GetCallerIdentity", Err: errors.New("identity sentinel")}
}

func (*commandHarness) CopyObject(context.Context, string, string, string) error {
	panic("unexpected CopyObject")
}

func TestRootFailuresStopBeforeCloudThroughCLI(t *testing.T) {
	//
	for _, args := range [][]string{{"space", "list"}, {"space", "destroy", "sbx1"}, {"space", "stop", "sbx1"}, {"space", "start", "sbx1"}, {"space", "status", "sbx1"}, {"apex", "set", "crm.sbx1"}, {"apex", "show"}, {"apex", "clear"}} {
		h := newCommandHarness(t)
		h.write(filepath.Join("infra", "terraform.tfvars.json"), `{}`)
		deps := h.deps()
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("unexpected cloud after root error")
			return cloud.Clients{}, nil
		}
		assertResult(t, invokeWithDeps(deps, args...), 2, "", "devctl: infra/terraform.tfvars.json: missing 'domain'\n")
	}
}

const createSHA = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"

// withCreateRelease supplies only the git release queries, preserving other seam behavior.
func withCreateRelease(deps seam.Deps) seam.Deps {
	exec := deps.Exec
	deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path == "git" && c.Args[0] == "tag" {
			return seam.Result{Stdout: []byte("r2\n")}, nil
		}
		if c.Path == "git" && len(c.Args) > 1 && c.Args[1] == "--verify" {
			return seam.Result{Stdout: []byte(createSHA + "\n")}, nil
		}
		return exec(ctx, c)
	}
	return deps
}

func fakeCreateBuild(t *testing.T, root string, c seam.Cmd) (seam.Result, bool, error) {
	t.Helper()
	fixture, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fixture.Close() }()
	relative := func(path string) string {
		t.Helper()
		value, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(value) {
			t.Fatalf("path outside fixture: %s", path)
		}
		return value
	}
	write := func(path, value string) {
		t.Helper()
		if err := fixture.MkdirAll(filepath.Dir(relative(path)), 0750); err != nil {
			t.Fatal(err)
		}
		if err := fixture.WriteFile(relative(path), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if c.Path == "git" {
		if c.Args[0] == "tag" {
			return seam.Result{Stdout: []byte("r2\n")}, true, nil
		}
		if len(c.Args) > 1 && c.Args[1] == "--verify" {
			return seam.Result{Stdout: []byte(createSHA + "\n")}, true, nil
		}
		if c.Args[0] == "worktree" {
			if c.Args[1] == "add" {
				if err := os.MkdirAll(c.Args[3], 0750); err != nil {
					return seam.Result{}, true, err
				}
				entries, err := os.ReadDir(root)
				if err != nil {
					return seam.Result{}, true, err
				}
				for _, entry := range entries {
					name := entry.Name()
					data, err := fixture.ReadFile(filepath.Join(name, "etc", "manifest.toml"))
					if os.IsNotExist(err) {
						continue
					}
					if err != nil {
						return seam.Result{}, true, err
					}
					write(filepath.Join(c.Args[3], name, "etc", "manifest.toml"), string(data))
					write(filepath.Join(c.Args[3], name, "cmd", name, "main.go"), "package main\n")
				}
			}
			return seam.Result{}, true, nil
		}
		return seam.Result{Stdout: []byte(root + "\n")}, true, nil
	}
	if c.Path == "go" {
		write(c.Args[3], "binary")
		return seam.Result{}, true, nil
	}
	if c.Path == "tar" {
		write(c.Args[1], "archive")
		return seam.Result{}, true, nil
	}
	if len(c.Args) == 1 && c.Args[0] == "manifest" {
		data, err := os.ReadFile(filepath.Join(c.Dir, "etc", "manifest.toml"))
		return seam.Result{Stdout: data}, true, err
	}
	if c.Path == "secret-tool" {
		return seam.Result{ExitCode: 1}, true, nil
	}
	return seam.Result{}, false, nil
}

func TestCreateReleaseResolutionThroughCLI(t *testing.T) {
	// R-DA4A-C76V R-UZ6T-F1PK R-UVJ4-9QHH R-UWR0-NI86
	for _, tc := range []struct {
		args                 []string
		tags, rev, sha, want string
	}{
		{nil, "r1\nr2\nr3-rc1\n", "refs/tags/r2^{commit}", createSHA, ""},
		{[]string{"--release", "9e1c7a3"}, "", "9e1c7a3^{commit}", "9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c", ""},
		{[]string{"--release=r3-rc1"}, "", "refs/tags/r3-rc1^{commit}", createSHA, ""},
		{[]string{"--release", "main"}, "", "refs/tags/main^{commit}", "", "devctl: 'main' is not a commit\n"},
		{nil, "r1-rc1\nfeature/x\n", "", "", "devctl: no r<N> release tag in this checkout; name one with --release <sha|tag>\n"},
		{[]string{"--release=first", "--release", "r2"}, "", "refs/tags/r2^{commit}", createSHA, ""},
	} {
		h := newCommandHarness(t)
		h.write("broken/etc/manifest.toml", "invalid [")
		deps := h.deps()
		original := deps.Exec
		tags, resolves, cloudCalls := 0, 0, 0
		deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
			if c.Path == "git" && c.Args[0] == "tag" {
				tags++
				return seam.Result{Stdout: []byte(tc.tags)}, nil
			}
			if c.Path == "git" && len(c.Args) > 1 && c.Args[1] == "--verify" {
				resolves++
				if c.Args[len(c.Args)-1] != tc.rev {
					t.Fatalf("resolve = %v want %s", c.Args, tc.rev)
				}
				if tc.sha == "" {
					return seam.Result{ExitCode: 1}, nil
				}
				return seam.Result{Stdout: []byte(tc.sha + "\n")}, nil
			}
			return original(ctx, c)
		}
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, errors.New("cloud sentinel")
		}
		args := append([]string{"space", "create"}, tc.args...)
		args = append(args, "sbx1", "--acme-email=x")
		got := invokeWithDeps(deps, args...)
		if tc.want == "" {
			assertResult(t, got, 1, "", "devctl: cloud sentinel\n")
			if cloudCalls != 1 {
				t.Fatal("cloud not reached")
			}
		} else {
			assertResult(t, got, 2, "", tc.want)
			if cloudCalls != 0 {
				t.Fatal("unexpected cloud")
			}
		}
		wantTags := 0
		if len(tc.args) == 0 {
			wantTags = 1
		}
		if tags != wantTags {
			t.Fatalf("tag calls %d want %d", tags, wantTags)
		}
		wantResolve := 1
		if tc.rev == "" {
			wantResolve = 0
		}
		if resolves != wantResolve {
			t.Fatalf("resolve calls %d want %d", resolves, wantResolve)
		}
	}
}

func TestCreatePlatformBarrierThroughCLI(t *testing.T) {
	// R-V1MM-6L6Y
	for _, stop := range []string{"zone", "template", "boundary", "ns", "instances", "role", "profile"} {
		t.Run(stop, func(t *testing.T) {
			h := newCommandHarness(t)
			f := &createPlatformFake{commandHarness: h, stop: stop}
			deps := h.deps()
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				return cloud.Clients{STS: f, Route53: f, EC2: f, IAM: f}, nil
			}
			exec := deps.Exec
			deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
				if c.Path != "git" || c.Args[0] == "worktree" {
					t.Fatalf("effect before preflight: %#v", c)
				}
				return exec(ctx, c)
			}
			want := "devctl: stop " + stop + "\n"
			if stop == "zone" {
				want = "devctl: no hosted zone 'ikigenba.dev'\n"
			}
			if stop == "template" {
				want = "devctl: no launch template 'ikigenba.dev'\n"
			}
			if stop == "boundary" {
				want = "devctl: no permissions boundary 'ikigenba.dev'\n"
			}
			assertResult(t, invokeWithDeps(deps, "space", "create", "sbx1", "--acme-email=x"), 1, "", want)
			ordered := []string{"zone", "template", "boundary", "ns", "instances", "role", "profile"}
			for i, name := range f.calls {
				if name != ordered[i] {
					t.Fatalf("calls %v", f.calls)
				}
			}
			if f.calls[len(f.calls)-1] != stop || len(h.mutations) != 0 {
				t.Fatalf("calls %v mutations %v", f.calls, h.mutations)
			}
		})
	}
}

type createPlatformFake struct {
	*commandHarness
	stop  string
	calls []string
}

func (f *createPlatformFake) fail(name string) error {
	f.calls = append(f.calls, name)
	if name == f.stop {
		return errors.New("stop " + name)
	}
	return nil
}
func (f *createPlatformFake) Zone(context.Context, string) (cloud.Zone, error) {
	if err := f.fail("zone"); err != nil {
		return cloud.Zone{}, &cloud.NotFoundError{Kind: "hosted zone", Name: "ikigenba.dev"}
	}
	return cloud.Zone{ID: "Z", Name: "ikigenba.dev"}, nil
}
func (f *createPlatformFake) LaunchTemplate(context.Context, string) (string, error) {
	if err := f.fail("template"); err != nil {
		return "", &cloud.NotFoundError{Kind: "launch template", Name: "ikigenba.dev"}
	}
	return "lt", nil
}
func (f *createPlatformFake) PermissionsBoundary(context.Context, string) (string, error) {
	if err := f.fail("boundary"); err != nil {
		return "", &cloud.NotFoundError{Kind: "permissions boundary", Name: "ikigenba.dev"}
	}
	return "arn", nil
}
func (f *createPlatformFake) FindRecord(context.Context, string, string, string) (cloud.Record, bool, error) {
	return cloud.Record{}, false, f.fail("ns")
}
func (f *createPlatformFake) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return nil, f.fail("instances")
}
func (f *createPlatformFake) RoleExists(context.Context, string) (bool, error) {
	return false, f.fail("role")
}
func (f *createPlatformFake) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	return nil, false, f.fail("profile")
}

var createApps = []string{"auth", "dummy", "events", "mcp", "repos", "scripts", "sites", "telemetry"}

type completeCreateFake struct {
	*commandHarness
	backup                        bool
	prefixes, remotes, parameters []string
	fail                          string
}

func (f *completeCreateFake) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{ID: "i-0c9e94542d98846a8", State: cloud.StateRunning, Address: "3.19.79.227"}, nil
}
func (f *completeCreateFake) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	if f.fail == "launch" {
		return cloud.Instance{}, &cloud.Error{Service: "ec2", Operation: "RunInstances", Code: "InsufficientInstanceCapacity"}
	}
	return cloud.Instance{ID: "i-0c9e94542d98846a8"}, nil
}
func (f *completeCreateFake) AllocateAddress(context.Context, string, string) (cloud.Address, error) {
	return cloud.Address{IP: "18.118.7.42", AllocationID: "eipalloc-1"}, nil
}
func (f *completeCreateFake) AssociateAddress(context.Context, string, string) error { return nil }
func (f *completeCreateFake) ListObjects(_ context.Context, bucket, prefix string) ([]cloud.Object, error) {
	if bucket != "ikigenba.dev" {
		f.t.Fatalf("bucket %s", bucket)
	}
	f.prefixes = append(f.prefixes, prefix)
	if f.backup && prefix != "staging/sites/" {
		return []cloud.Object{{Key: prefix + "old.tar.zst", Modified: time.Unix(1, 0)}, {Key: prefix + "new.tar.zst", Modified: time.Unix(2, 0)}}, nil
	}
	return nil, nil
}
func (f *completeCreateFake) PutSecureParameter(ctx context.Context, name, value string) error {
	f.parameters = append(f.parameters, name)
	return f.commandHarness.PutSecureParameter(ctx, name, value)
}
func (f *completeCreateFake) deps() seam.Deps {
	deps := f.commandHarness.deps()
	exec := deps.Exec
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{STS: f, EC2: f, IAM: f, SSM: f, S3: f, Route53: f}, nil
	}
	deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path == "go" && f.fail == "build" {
			return seam.Result{ExitCode: 1, Stderr: []byte("# github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\ncmd/dashboard/main.go:41:2: undefined: render\n")}, nil
		}
		if c.Path == "git" && c.Args[0] == "worktree" && c.Args[1] == "add" {
			result, err := exec(ctx, c)
			if err != nil {
				return result, err
			}
			// extra exists only in the working checkout, never in the release tree.
			if err := os.RemoveAll(filepath.Join(c.Args[3], "extra")); err != nil {
				return seam.Result{}, err
			}
			return result, nil
		}
		if c.Path == "ssh" {
			remote := c.Args[len(c.Args)-1]
			f.remotes = append(f.remotes, remote)
			if remote == "'mktemp'" {
				return seam.Result{Stdout: []byte("/tmp/upload\n")}, nil
			}
			if strings.Contains(remote, "'test' '-e'") {
				return seam.Result{ExitCode: 1}, nil
			}
			if strings.Contains(remote, "'mktemp' '-d'") {
				return seam.Result{Stdout: []byte("/opt/ikigenba/releases/.unpack.fake\n")}, nil
			}
			if strings.HasSuffix(remote, "'init'") && f.fail == "init" {
				return seam.Result{ExitCode: 2}, nil
			}
			if strings.HasSuffix(remote, "'host' 'restore'") {
				return seam.Result{Stdout: []byte("arbitrary restore output\n")}, nil
			}
		}
		return exec(ctx, c)
	}
	deps.Stream = func(_ context.Context, c seam.Cmd, w io.Writer) (seam.Result, error) {
		f.remotes = append(f.remotes, c.Args[len(c.Args)-1])
		if _, err := io.WriteString(w, "activation output\n"); err != nil {
			return seam.Result{}, err
		}
		if f.fail == "activate" {
			return seam.Result{ExitCode: 1, Stderr: []byte("opsctl: activate failed\n")}, nil
		}
		return seam.Result{}, nil
	}
	return deps
}
func newCompleteCreateFake(t *testing.T) *completeCreateFake {
	h := newCommandHarness(t)
	for _, app := range createApps {
		h.addAppWithSecrets(app)
	}
	h.addAppWithSecrets("extra")
	return &completeCreateFake{commandHarness: h}
}
func createInitialLines() string {
	return "account: ok (ikigenba.dev, us-east-2, 123)\ndomain: ok (zone ikigenba.dev Z)\nbuild: ok (r2, dist/" + createSHA + ".tar.xz)\n"
}

func TestCreateDeploysReleaseThroughCLI(t *testing.T) {
	// R-V2UI-KCXN R-V5AB-BWF1 R-VBDT-8R4I R-VCLP-MIV7 R-VDTM-0ALW R-VG9E-RU3A R-VHHB-5LTZ R-VIP7-JDKO R-VJX3-X5BD
	for _, operand := range []string{"sbx1", "sbx1.ikigenba.dev", "staging"} {
		f := newCompleteCreateFake(t)
		f.backup = operand == "staging"
		label := "sbx1"
		if f.backup {
			label = "staging"
		}
		domain := label + ".ikigenba.dev"
		got := invokeWithDeps(f.deps(), "space", "create", operand, "--acme-email", "ops@ikigenba.dev")
		restore := "restore: ok (no host backup)\n"
		if f.backup {
			restore = "restore: ok (host/new.tar.zst, 10 keys set again)\n"
		}
		want := createInitialLines() + "secrets: ok (8 apps)\nrole: ok (" + domain + ")\ninstance: ok (i-0c9e94542d98846a8 running, 3.19.79.227)\naddress: ok (elastic ip 18.118.7.42 associated)\nrecords: ok (created " + domain + ", *." + domain + " -> 18.118.7.42, INSYNC)\nhost: ok (status checks passed, cloud-init done)\ncopy: ok (" + createSHA + ".tar.xz -> 18.118.7.42)\nunpack: ok (/opt/ikigenba/releases/" + createSHA + ")\nopsctl: ok (10 keys set)\n" + restore + "init: ok\n"
		if f.backup {
			for _, app := range createApps {
				if app == "sites" {
					want += "restore: ok (no backup of sites)\n"
				} else {
					want += "restore: ok (opsctl restore " + app + ")\n"
				}
			}
		}
		want += "activation output\n" + domain + " 18.118.7.42\n"
		assertResult(t, got, 0, want, "")
		if len(f.parameters) != 8 {
			t.Fatalf("parameters %v", f.parameters)
		}
		for i, app := range createApps {
			if f.parameters[i] != "/"+domain+"/"+app {
				t.Fatalf("parameters %v", f.parameters)
			}
		}
		wantPrefix := []string{label + "/host/"}
		if f.backup {
			for _, app := range createApps {
				wantPrefix = append(wantPrefix, label+"/"+app+"/")
			}
		}
		if !reflect.DeepEqual(f.prefixes, wantPrefix) {
			t.Fatalf("prefixes %v want %v", f.prefixes, wantPrefix)
		}
		program := "'/opt/ikigenba/releases/" + createSHA + "/opsctl/bin/opsctl'"
		configCount := 0
		var configVectors []string
		for _, remote := range f.remotes {
			if strings.Contains(remote, "'opsctl'") || strings.Contains(remote, "'curl'") || strings.Contains(remote, "'bash'") {
				t.Fatalf("wrong program %s", remote)
			}
			if strings.Contains(remote, "'config' 'set'") {
				configCount++
				configVectors = append(configVectors, remote)
				if !strings.HasPrefix(remote, "'sudo' "+program+" 'config' 'set'") {
					t.Fatalf("config %s", remote)
				}
			}
		}
		wantCount := 10
		if f.backup {
			wantCount = 20
		}
		if configCount != wantCount {
			t.Fatalf("config count %d", configCount)
		}
		values := []string{"host.name=" + domain, "dns.provider=route53", "dns.zones=ikigenba.dev:Z", "aws.region=us-east-2", "backup.s3_uri=s3://ikigenba.dev/" + label + "/", "backup.host_files_seconds=86400", "backup.service_files_seconds=86400", "backup.service_db_seconds=86400", "backup.service_wal_seconds=300", "acme.email=ops@ikigenba.dev"}
		for i, vector := range configVectors {
			wantVector := "'sudo' " + program + " 'config' 'set' '" + values[i%10] + "'"
			if vector != wantVector {
				t.Fatalf("configuration %d = %s want %s", i, vector, wantVector)
			}
		}
		var operations []string
		for _, remote := range f.remotes {
			if strings.HasPrefix(remote, "'sudo' "+program) {
				operations = append(operations, remote)
			}
		}
		tail := []string{}
		if f.backup {
			tail = append(tail, "'sudo' "+program+" 'host' 'restore'")
			tail = append(tail, configVectors[10:]...)
			tail = append(tail, "'sudo' "+program+" 'config' 'del' 'host.apex'")
		}
		tail = append(tail, "'sudo' "+program+" 'init'")
		if f.backup {
			for _, app := range createApps {
				if app != "sites" {
					tail = append(tail, "'sudo' "+program+" 'restore' '"+app+"'")
				}
			}
		}
		tail = append(tail, "'sudo' "+program+" 'activate' '"+createSHA+"' 'r2'")
		if !reflect.DeepEqual(operations[10:], tail) {
			t.Fatalf("operations after config = %v want %v", operations[10:], tail)
		}

	}
}

func TestCreateFailuresKeepCompletedStepsThroughCLI(t *testing.T) {
	// R-V2UI-KCXN R-V5AB-BWF1 R-V7Q4-3FWF R-VF1I-E2CL R-VHHB-5LTZ R-VIP7-JDKO
	for _, stop := range []string{"build", "secret", "launch", "init", "activate"} {
		t.Run(stop, func(t *testing.T) {
			f := newCompleteCreateFake(t)
			f.fail = stop
			if stop == "build" {
				f.write("dashboard/cmd/dashboard/main.go", "package main\n")
				f.write("dashboard/etc/manifest.toml", "app = \"dashboard\"\n")
				for _, app := range createApps {
					if err := os.RemoveAll(filepath.Join(f.checkoutRoot, app)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if stop == "secret" {
				f.addAppWithSecrets("auth", "GOOGLE_CLIENT_ID")
			}
			got := invokeWithDeps(f.deps(), "space", "create", "sbx1", "--acme-email=ops@ikigenba.dev")
			wantCode := 1
			if stop == "secret" {
				wantCode = 2
			}
			if got.code != wantCode {
				t.Fatalf("result %#v", got)
			}
			if strings.Contains(got.stdout, "sbx1.ikigenba.dev 18.118.7.42") || strings.Contains(got.stderr, "arbitrary restore output") {
				t.Fatalf("result %#v", got)
			}
			switch stop {
			case "build":
				assertResult(t, got, 1, "account: ok (ikigenba.dev, us-east-2, 123)\ndomain: ok (zone ikigenba.dev Z)\n", "devctl: build dashboard: exit status 1\n\n> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\n> cmd/dashboard/main.go:41:2: undefined: render\n")
			case "secret":
				assertResult(t, got, 2, createInitialLines(), "devctl: auth: no value for 'GOOGLE_CLIENT_ID' in the keyring or the environment\n")
				if _, err := os.Stat(filepath.Join(f.checkoutRoot, "dist", createSHA+".tar.xz")); err != nil {
					t.Fatal(err)
				}
			case "launch":
				assertResult(t, got, 1, createInitialLines()+"secrets: ok (8 apps)\nrole: ok (sbx1.ikigenba.dev)\n", "devctl: ec2 RunInstances: InsufficientInstanceCapacity\n")
			case "init":
				if got.stderr != "devctl: init: ssh ec2-user@18.118.7.42 sudo /opt/ikigenba/releases/"+createSHA+"/opsctl/bin/opsctl init: exit status 2\n" || !strings.HasSuffix(got.stdout, "restore: ok (no host backup)\n") {
					t.Fatalf("result %#v", got)
				}
			case "activate":
				if !strings.HasSuffix(got.stdout, "init: ok\nactivation output\n") || got.stderr != "devctl: activate: ssh ec2-user@18.118.7.42 sudo /opt/ikigenba/releases/"+createSHA+"/opsctl/bin/opsctl activate "+createSHA+" r2: exit status 1\n\n> opsctl: activate failed\n" {
					t.Fatalf("result %#v", got)
				}
			}
		})
	}
}

func (f *completeCreateFake) TerminateInstance(context.Context, string) error {
	f.t.Fatal("create undid instance")
	return nil
}
func (f *completeCreateFake) ReleaseAddress(context.Context, string) error {
	f.t.Fatal("create undid address")
	return nil
}
func (f *completeCreateFake) DeleteRole(context.Context, string) error {
	f.t.Fatal("create undid role")
	return nil
}
func (f *completeCreateFake) DeleteRolePolicy(context.Context, string, string) error {
	f.t.Fatal("create undid role policy")
	return nil
}
func (f *completeCreateFake) DeleteParameter(context.Context, string) error {
	f.t.Fatal("create undid parameter")
	return nil
}
func (f *completeCreateFake) PutObject(context.Context, string, string, io.Reader, int64) error {
	f.t.Fatal("create uploaded backup")
	return nil
}
func (f *completeCreateFake) CopyObject(context.Context, string, string, string) error {
	f.t.Fatal("create copied backup")
	return nil
}
func (f *completeCreateFake) DeleteObjects(context.Context, string, []string) error {
	f.t.Fatal("create deleted backup")
	return nil
}

func TestCreateUnlabelledReleaseThroughCLI(t *testing.T) {
	// R-V2UI-KCXN R-DA4A-C76V
	f := newCompleteCreateFake(t)
	deps := f.deps()
	exec := deps.Exec
	const sha = "9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c"
	deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path == "git" && len(c.Args) > 1 && c.Args[1] == "--verify" {
			if c.Args[len(c.Args)-1] != "9e1c7a3^{commit}" {
				t.Fatalf("resolve %v", c.Args)
			}
			return seam.Result{Stdout: []byte(sha + "\n")}, nil
		}
		if c.Path == "git" && c.Args[0] == "tag" {
			t.Fatal("explicit release listed tags")
		}
		return exec(ctx, c)
	}
	got := invokeWithDeps(deps, "space", "create", "--release", "9e1c7a3", "sbx1", "--acme-email=ops@ikigenba.dev")
	if got.code != 0 || got.stderr != "" || !strings.Contains(got.stdout, "build: ok (dist/"+sha+".tar.xz)\n") {
		t.Fatalf("result %#v", got)
	}
	want := "'sudo' '/opt/ikigenba/releases/" + sha + "/opsctl/bin/opsctl' 'activate' '" + sha + "'"
	if f.remotes[len(f.remotes)-1] != want {
		t.Fatalf("activate %s want %s", f.remotes[len(f.remotes)-1], want)
	}
}

type createListingFailure struct {
	*completeCreateFake
	failed   *bool
	prefix   string
	sentinel error
}

func (f *createListingFailure) ListObjects(ctx context.Context, bucket, prefix string) ([]cloud.Object, error) {
	if *f.failed {
		f.t.Fatalf("listing after failure: %s", prefix)
	}
	if prefix == f.prefix {
		*f.failed = true
		f.prefixes = append(f.prefixes, prefix)
		return nil, f.sentinel
	}
	return f.completeCreateFake.ListObjects(ctx, bucket, prefix)
}

func TestCreateHostAndAppFailuresStopByUse(t *testing.T) {
	// R-VBDT-8R4I R-VCLP-MIV7 R-VDTM-0ALW R-VG9E-RU3A
	for _, tc := range []struct {
		name, match, prefix, lastLine, absent string
		configOrdinal                         int
	}{
		{name: "copy", match: "scp", lastLine: "host: ok (status checks passed, cloud-init done)\n", absent: "copy:"},
		{name: "unpack", match: "'tar' '-x'", lastLine: "copy: ok (" + createSHA + ".tar.xz -> 18.118.7.42)\n", absent: "unpack:"},
		{name: "configure", match: "'config' 'set'", configOrdinal: 3, lastLine: "unpack: ok (/opt/ikigenba/releases/" + createSHA + ")\n", absent: "opsctl:"},
		{name: "host listing", prefix: "sbx1/host/", lastLine: "opsctl: ok (10 keys set)\n", absent: "restore:"},
		{name: "host restore", match: "'host' 'restore'", lastLine: "opsctl: ok (10 keys set)\n", absent: "restore:"},
		{name: "configure again", match: "'config' 'set'", configOrdinal: 13, lastLine: "opsctl: ok (10 keys set)\n", absent: "restore:"},
		{name: "delete apex", match: "'config' 'del' 'host.apex'", lastLine: "opsctl: ok (10 keys set)\n", absent: "restore:"},
		{name: "app listing", prefix: "sbx1/dummy/", lastLine: "restore: ok (opsctl restore auth)\n", absent: "restore: ok (opsctl restore dummy)"},
		{name: "app restore", match: "'restore' 'dummy'", lastLine: "restore: ok (opsctl restore auth)\n", absent: "restore: ok (opsctl restore dummy)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCompleteCreateFake(t)
			f.backup = true
			deps := f.deps()
			exec := deps.Exec
			sentinel := errors.New("host-phase sentinel")
			failed := false
			configCount := 0
			listing := &createListingFailure{completeCreateFake: f, failed: &failed, prefix: tc.prefix, sentinel: sentinel}
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				return cloud.Clients{STS: f, EC2: f, IAM: f, SSM: f, S3: listing, Route53: f}, nil
			}
			deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
				remote := ""
				if len(c.Args) > 0 {
					remote = c.Args[len(c.Args)-1]
				}
				if failed {
					if c.Path != "ssh" || !strings.Contains(remote, "'rm'") {
						t.Fatalf("operation after failure %#v", c)
					}
					return exec(ctx, c)
				}
				if c.Path == "ssh" && strings.Contains(remote, "'config' 'set'") {
					configCount++
				}
				shouldFail := tc.match != "" && (c.Path == tc.match || (c.Path == "ssh" && strings.Contains(remote, tc.match)))
				if tc.configOrdinal != 0 {
					shouldFail = shouldFail && configCount == tc.configOrdinal
				}
				if shouldFail {
					failed = true
					return seam.Result{}, sentinel
				}
				return exec(ctx, c)
			}
			deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
				t.Fatal("activation after host failure")
				return seam.Result{}, nil
			}
			var out bytes.Buffer
			err := spacecreate.Run(context.Background(), []string{"sbx1", "--acme-email=ops@ikigenba.dev"}, "phase-version", &out, deps)
			wantError := "ssh: " + sentinel.Error()
			if tc.match == "scp" {
				wantError = "scp: " + sentinel.Error()
			}
			if tc.prefix != "" {
				wantError = sentinel.Error()
			}
			if !failed || !errors.Is(err, sentinel) || err.Error() != wantError {
				t.Fatalf("err %v failed %t", err, failed)
			}
			if !strings.HasSuffix(out.String(), tc.lastLine) || strings.Contains(out.String(), tc.absent) || strings.Contains(out.String(), "sbx1.ikigenba.dev 18.118.7.42") || strings.Contains(out.String(), sentinel.Error()) {
				t.Fatalf("output %q", out.String())
			}
		})
	}
}
