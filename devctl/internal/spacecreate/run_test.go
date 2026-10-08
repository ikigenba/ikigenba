package spacecreate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

func TestAssumeRolePolicy(t *testing.T) {
	// R-XQZ0-EB4M
	const want = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ec2.amazonaws.com"}}]}`
	if AssumeRolePolicy != want {
		t.Fatalf("AssumeRolePolicy = %q, want %q", AssumeRolePolicy, want)
	}
}

func TestRunSignatureAndGrammar(t *testing.T) {
	// R-UUB7-VYQS R-UVJ4-9QHH R-UWR0-NI86 R-8UTA-21TK R-8W16-FTK9 R-3M5T-DZ38 R-E7GU-RBY9 R-8X92-TLAY
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing operand", nil, "space create needs <space>"},
		{"extra operand", []string{"a", "b", "--acme-email=x@y"}, "space create takes only <space>"},
		{"missing email", []string{"a"}, "space create needs --acme-email <address>"},
		{"missing email value", []string{"a", "--acme-email"}, "option '--acme-email' requires a value"},
		{"missing operand precedes missing email value", []string{"--acme-email"}, "space create needs <space>"},
		{"extra operand precedes missing email value", []string{"a", "b", "--acme-email"}, "space create takes only <space>"},
		{"empty email value", []string{"a", "--acme-email="}, "option '--acme-email' requires a value"},
		{"old elastic option", []string{"a", "--elastic-ip", "--acme-email=x@y"}, "unknown option '--elastic-ip'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			deps := seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				calls++
				return seam.Result{}, errors.New("unexpected")
			}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				calls++
				return cloud.Clients{}, errors.New("unexpected")
			}}
			var stdout bytes.Buffer
			err := Run(context.Background(), tc.args, "test-version", &stdout, deps)
			var usageErr *space.UsageError
			if !errors.As(err, &usageErr) || usageErr.Message != tc.want || usageErr.Help != helpCommand || calls != 0 || stdout.Len() != 0 {
				t.Fatalf("Run(%q) = error %#v, stdout %q, calls %d", tc.args, err, stdout.String(), calls)
			}
		})
	}

	got, err := parseInvocation([]string{"--acme-email=first", "sbx1", "--acme-email", "last"})
	if err != nil || got.operand != "sbx1" || got.acmeEmail != "last" {
		t.Fatalf("last option parse = %#v, %v", got, err)
	}
	got, err = parseInvocation([]string{"sbx1", "--acme-email", "--literal-address"})
	if err != nil || got.acmeEmail != "--literal-address" {
		t.Fatalf("option-like value parse = %#v, %v", got, err)
	}
}

func TestRunHelpIsExactAndDependencyFree(t *testing.T) {
	// R-UXYX-19YV
	//
	for _, option := range []string{"--help", "-h"} {
		calls := 0
		deps := seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) { calls++; return seam.Result{}, nil }, Cloud: func(context.Context, string, string) (cloud.Clients, error) { calls++; return cloud.Clients{}, nil }}
		var stdout bytes.Buffer
		if err := Run(context.Background(), []string{option}, "test-version", &stdout, deps); err != nil || stdout.String() != usageText || calls != 0 {
			t.Fatalf("help %s = %q, %v, calls %d", option, stdout.String(), err, calls)
		}
	}
}

func TestRunSuccessfulCreateUsesCurrentContracts(t *testing.T) {
	// R-V2UI-KCXN R-V5AB-BWF1 R-V6I7-PO5Q R-V7Q4-3FWF R-V8Y0-H7N4
	// R-VA5W-UZDT R-VBDT-8R4I R-VCLP-MIV7 R-VDTM-0ALW R-VF1I-E2CL
	// R-VG9E-RU3A R-VHHB-5LTZ R-VIP7-JDKO R-VJX3-X5BD
	//
	// R-EB4J-WN6C  R-YMOE-CCEO
	f := newCreateFake(t)
	f.objects = []cloud.Object{
		{Key: "sbx1/host/old.tar.zst", Modified: time.Unix(1, 0)},
		{Key: "sbx1/host/new.tar.zst", Modified: time.Unix(2, 0)},
	}
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"sbx1", "--acme-email", "ops@ikigenba.dev"}, "test-version", &stdout, f.deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "account: ok (ikigenba.dev, us-east-2, 295229566359)\n" +
		"domain: ok (zone ikigenba.dev ZONE1)\n" +
		"build: ok (r2, dist/" + createSHA + ".tar.xz)\n" +
		"secrets: ok (1 apps)\n" +
		"role: ok (sbx1.ikigenba.dev)\n" +
		"instance: ok (i-new running, 198.51.100.7)\n" +
		"address: ok (elastic ip 18.118.7.42 associated)\n" +
		"records: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)\n" +
		"host: ok (status checks passed, cloud-init done)\n" +
		"copy: ok (" + createSHA + ".tar.xz -> 18.118.7.42)\n" +
		"unpack: ok (/opt/ikigenba/releases/" + createSHA + ")\n" +
		"opsctl: ok (10 keys set)\n" +
		"restore: ok (host/new.tar.zst, 10 keys set again)\n" +
		"init: ok\n" +
		"restore: ok (opsctl restore crm)\n" +
		"activate output\n" +
		"sbx1.ikigenba.dev 18.118.7.42\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if f.openProfile != "ikigenba.dev" || f.openRegion != "us-east-2" {
		t.Fatalf("cloud open = %q, %q", f.openProfile, f.openRegion)
	}
	if f.findZone != "ZONE1" || f.findDomain != "sbx1.ikigenba.dev" || f.findType != "NS" {
		t.Fatalf("FindRecord arguments = %q, %q, %q", f.findZone, f.findDomain, f.findType)
	}
	if f.allocateRoot != "ikigenba.dev" || f.allocateSpace != "sbx1.ikigenba.dev" {
		t.Fatalf("AllocateAddress arguments = %q, %q", f.allocateRoot, f.allocateSpace)
	}
	wantPrefix := []string{"git", "git", "git", "sts", "zone", "template", "boundary", "ns", "spaces", "role-exists", "profile-exists", "build", "secret", "create-role", "put-policy", "create-profile", "add-role", "launch-ready", "run-instance", "describe", "allocate", "associate", "records", "change-status", "checks"}
	if len(f.events) < len(wantPrefix) || !reflect.DeepEqual(f.events[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("events = %v", f.events)
	}
	launch := cloud.LaunchSpec{LaunchTemplateID: "lt-1", InstanceProfile: "sbx1.ikigenba.dev", Domain: "ikigenba.dev", Space: "sbx1.ikigenba.dev"}
	if f.readySpec != launch || f.runSpec != launch {
		t.Fatalf("launch specs = %#v, %#v", f.readySpec, f.runSpec)
	}
	if f.roleSpec != (cloud.RoleSpec{Name: "sbx1.ikigenba.dev", AssumeRolePolicy: AssumeRolePolicy, PermissionsBoundaryARN: "arn:boundary"}) {
		t.Fatalf("role spec = %#v", f.roleSpec)
	}
	wantPolicy := space.PolicyDocument("ikigenba.dev", "ZONE1", resultSpace(), false)
	if f.policy != wantPolicy || f.policyName != space.PolicyName {
		t.Fatalf("policy = %q, %q", f.policyName, f.policy)
	}
	joined := strings.ReplaceAll(strings.Join(f.remotes, "\n"), release.Opsctl(createSHA), "opsctl")
	if strings.Count(joined, "'opsctl' 'config' 'set'") != 20 || !strings.Contains(joined, "'opsctl' 'config' 'del' 'host.apex'") || !strings.Contains(joined, "'opsctl' 'host' 'restore'") {
		t.Fatalf("remote commands:\n%s", joined)
	}
	if strings.Count(joined, "'opsctl' 'config' 'set' 'aws.region=us-east-2'") != 2 {
		t.Fatalf("aws.region configuration commands:\n%s", joined)
	}
}

func TestRunPreflightRefusalsAndRoleLength(t *testing.T) {
	// R-90WR-YWJ1 R-93CK-QG0F R-94KH-47R4 R-95SD-HZHT
	tests := []struct {
		name   string
		change func(*createFake)
		want   string
	}{
		{"delegated", func(f *createFake) { f.delegated = true }, "'sbx1.ikigenba.dev' is delegated away from 'ikigenba.dev'"},
		{"space exists", func(f *createFake) {
			f.instances = []cloud.Instance{{ID: "i-old", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning}}
		}, "a space at 'sbx1.ikigenba.dev' already exists (i-old)"},
		{"role exists", func(f *createFake) { f.roleExists = true }, "a role for 'sbx1.ikigenba.dev' already exists"},
		{"profile exists", func(f *createFake) { f.profileExists = true }, "a role for 'sbx1.ikigenba.dev' already exists"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newCreateFake(t)
			tc.change(f)
			var out bytes.Buffer
			err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, "test-version", &out, f.deps())
			var refused *RefusedError
			if !errors.As(err, &refused) || err.Error() != tc.want || out.Len() != 0 || contains(f.events, "secret") {
				t.Fatalf("err=%v out=%q events=%v", err, out.String(), f.events)
			}
		})
	}

	f := newCreateFake(t)
	long := strings.Repeat("a", 52)
	var out bytes.Buffer
	err := Run(context.Background(), []string{long, "--acme-email=x"}, "test-version", &out, f.deps())
	if err == nil || !strings.Contains(err.Error(), "role name is at most 64 characters") || f.openProfile != "" {
		t.Fatalf("long role = %v, cloud profile %q", err, f.openProfile)
	}
	boundary := newCreateFake(t)
	boundary.stopAt = "zone"
	_ = Run(context.Background(), []string{strings.Repeat("a", 51), "--acme-email=x"}, "test-version", io.Discard, boundary.deps())
	if boundary.openProfile == "" {
		t.Fatal("64-byte domain did not reach cloud")
	}
}

func TestCreateStopsAtSecretsAndEmptyRestoreSkipsRestoreCommands(t *testing.T) {
	//
	failure := newCreateFake(t)
	failure.stopAt = "secret"
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, "test-version", &stdout, failure.deps()); err == nil || !strings.HasSuffix(stdout.String(), ".tar.xz)\n") || contains(failure.events, "create-role") {
		t.Fatalf("secret failure = %v, stdout %q, events %v", err, stdout.String(), failure.events)
	}

	empty := newCreateFake(t)
	if err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, "test-version", io.Discard, empty.deps()); err != nil {
		t.Fatalf("empty restore create: %v", err)
	}
	joined := strings.ReplaceAll(strings.Join(empty.remotes, "\n"), release.Opsctl(createSHA), "opsctl")
	if strings.Contains(joined, "'opsctl' 'host' 'restore'") || strings.Contains(joined, "'host.apex'") || strings.Count(joined, "'opsctl' 'config' 'set'") != 10 {
		t.Fatalf("empty restore remote commands:\n%s", joined)
	}
}

func TestNewestBackupBreaksModifiedTieByKey(t *testing.T) {
	// R-VDTM-0ALW
	f := newCreateFake(t)
	when := time.Unix(1, 0)
	f.objects = []cloud.Object{{Key: "sbx1/host/a", Modified: when}, {Key: "sbx1/host/z", Modified: when}}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, "build-version", &out, f.deps()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "restore: ok (host/z, 10 keys set again)\n") {
		t.Fatalf("restore = %q", out.String())
	}
}

func resultSpace() spaceref.Space { return spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"} }

type createFake struct {
	t                                    *testing.T
	commands                             []seam.Cmd
	root                                 string
	stopErr                              error
	events, remotes                      []string
	openProfile, openRegion, stopAt      string
	findZone, findDomain, findType       string
	allocateRoot, allocateSpace          string
	delegated, roleExists, profileExists bool
	instances                            []cloud.Instance
	objects                              []cloud.Object
	readySpec, runSpec                   cloud.LaunchSpec
	roleSpec                             cloud.RoleSpec
	policyName, policy                   string
}

func newCreateFake(t *testing.T) *createFake {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "infra", "terraform.tfvars.json"), `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	mustWrite(t, filepath.Join(root, "crm", "cmd", "crm", "main.go"), "package main\n")
	mustWrite(t, filepath.Join(root, "crm", "etc", "manifest.toml"), "app = \"crm\"\n")
	return &createFake{t: t, root: root}
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (f *createFake) event(name string) error {
	f.events = append(f.events, name)
	if f.stopAt == name {
		if f.stopErr != nil {
			return f.stopErr
		}
		return errors.New("stop at " + name)
	}
	return nil
}

const createSHA = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"

func (f *createFake) deps() seam.Deps {
	return seam.Deps{Dir: f.root, EUID: 1, Getenv: func(string) string { return "" }, Now: func() time.Time { return time.Unix(1, 0) }, After: immediateAfter, Exec: f.exec,
		Stream: func(_ context.Context, c seam.Cmd, w io.Writer) (seam.Result, error) {
			f.commands = append(f.commands, c)
			f.remotes = append(f.remotes, c.Args[len(c.Args)-1])
			_, err := io.WriteString(w, "activate output\n")
			return seam.Result{}, err
		},
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			f.openProfile, f.openRegion = profile, region
			return cloud.Clients{EC2: f, SSM: f, Route53: f, S3: f, IAM: f, STS: f}, nil
		}}
}
func (f *createFake) exec(_ context.Context, c seam.Cmd) (seam.Result, error) {
	f.commands = append(f.commands, c)
	if c.Path == "git" {
		if c.Args[0] == "worktree" {
			if c.Args[1] == "add" {
				if err := f.event("build"); err != nil {
					return seam.Result{}, err
				}
				dir := c.Args[3]
				mustWrite(f.t, filepath.Join(dir, "crm", "cmd", "crm", "main.go"), "package main\n")
				mustWrite(f.t, filepath.Join(dir, "crm", "etc", "manifest.toml"), "app = \"crm\"\n")
			}
			return seam.Result{}, nil
		}
		_ = f.event("git")
		if c.Args[0] == "tag" {
			return seam.Result{Stdout: []byte("r1\nr2\nr3-rc1\n")}, nil
		}
		if len(c.Args) > 1 && c.Args[1] == "--verify" {
			return seam.Result{Stdout: []byte(createSHA + "\n")}, nil
		}
		return seam.Result{Stdout: []byte(f.root + "\n")}, nil
	}
	if c.Path == "go" {
		mustWrite(f.t, c.Args[3], "binary")
		return seam.Result{}, nil
	}
	if c.Path == "tar" {
		mustWrite(f.t, c.Args[1], "archive")
		return seam.Result{}, nil
	}
	if len(c.Args) == 1 && c.Args[0] == "manifest" {
		data, err := os.ReadFile(filepath.Join(c.Dir, "etc", "manifest.toml"))
		return seam.Result{Stdout: data}, err
	}
	if c.Path != "ssh" && c.Path != "scp" {
		f.t.Fatalf("unexpected command %#v", c)
	}
	f.remotes = append(f.remotes, c.Args[len(c.Args)-1])
	remote := c.Args[len(c.Args)-1]
	if remote == "'mktemp'" {
		return seam.Result{Stdout: []byte("/tmp/upload\n")}, nil
	}
	if strings.Contains(remote, "'test' '-e'") {
		return seam.Result{ExitCode: 1}, nil
	}
	if strings.Contains(remote, "'mktemp' '-d'") {
		return seam.Result{Stdout: []byte("/opt/ikigenba/releases/.unpack.fake\n")}, nil
	}
	return seam.Result{}, nil
}

func immediateAfter(time.Duration) <-chan time.Time {
	ready := make(chan time.Time)
	close(ready)
	return ready
}

func (f *createFake) CallerAccountID(context.Context) (string, error) {
	if err := f.event("sts"); err != nil {
		return "", err
	}
	return "295229566359", nil
}
func (f *createFake) LaunchTemplate(context.Context, string) (string, error) {
	if err := f.event("template"); err != nil {
		return "", err
	}
	return "lt-1", nil
}
func (f *createFake) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	if err := f.event("spaces"); err != nil {
		return nil, err
	}
	return f.instances, nil
}
func (f *createFake) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	if err := f.event("describe"); err != nil {
		return cloud.Instance{}, err
	}
	return cloud.Instance{ID: "i-new", State: cloud.StateRunning, Address: "198.51.100.7"}, nil
}
func (f *createFake) RunInstance(_ context.Context, s cloud.LaunchSpec) (cloud.Instance, error) {
	f.runSpec = s
	if err := f.event("run-instance"); err != nil {
		return cloud.Instance{}, err
	}
	return cloud.Instance{ID: "i-new", Address: "198.51.100.6"}, nil
}
func (f *createFake) LaunchReady(_ context.Context, s cloud.LaunchSpec) (bool, error) {
	f.readySpec = s
	if err := f.event("launch-ready"); err != nil {
		return false, err
	}
	return true, nil
}
func (f *createFake) StartInstance(context.Context, string) error     { return nil }
func (f *createFake) StopInstance(context.Context, string) error      { return nil }
func (f *createFake) TerminateInstance(context.Context, string) error { return nil }
func (f *createFake) InstanceChecksPassed(context.Context, string) (bool, error) {
	if err := f.event("checks"); err != nil {
		return false, err
	}
	return true, nil
}
func (f *createFake) ListSpaceAddresses(context.Context, string) ([]cloud.Address, error) {
	return nil, nil
}
func (f *createFake) AllocateAddress(_ context.Context, root, spaceDomain string) (cloud.Address, error) {
	f.allocateRoot, f.allocateSpace = root, spaceDomain
	if err := f.event("allocate"); err != nil {
		return cloud.Address{}, err
	}
	return cloud.Address{AllocationID: "eipalloc-1", IP: "18.118.7.42"}, nil
}
func (f *createFake) AssociateAddress(context.Context, string, string) error {
	return f.event("associate")
}
func (f *createFake) DisassociateAddress(context.Context, string) error    { return nil }
func (f *createFake) ReleaseAddress(context.Context, string) error         { return nil }
func (f *createFake) GetParameter(context.Context, string) (string, error) { return "", nil }
func (f *createFake) PutSecureParameter(context.Context, string, string) error {
	return f.event("secret")
}
func (f *createFake) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, nil
}
func (f *createFake) DeleteParameter(context.Context, string) error { return nil }
func (f *createFake) Zone(context.Context, string) (cloud.Zone, error) {
	if err := f.event("zone"); err != nil {
		return cloud.Zone{}, err
	}
	return cloud.Zone{ID: "ZONE1", Name: "ikigenba.dev"}, nil
}
func (f *createFake) ListRecords(context.Context, string) ([]cloud.Record, error) { return nil, nil }
func (f *createFake) FindRecord(_ context.Context, zone, domain, recordType string) (cloud.Record, bool, error) {
	f.findZone, f.findDomain, f.findType = zone, domain, recordType
	if err := f.event("ns"); err != nil {
		return cloud.Record{}, false, err
	}
	return cloud.Record{}, f.delegated, nil
}
func (f *createFake) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	if err := f.event("records"); err != nil {
		return "", err
	}
	return "change-1", nil
}
func (f *createFake) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	if err := f.event("change-status"); err != nil {
		return "", err
	}
	return cloud.ChangeInsync, nil
}
func (f *createFake) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	f.events = append(f.events, "list-objects")
	return f.objects, nil
}
func (f *createFake) PutObject(context.Context, string, string, io.Reader, int64) error { return nil }
func (f *createFake) CopyObject(context.Context, string, string, string) error          { return nil }
func (f *createFake) DeleteObjects(context.Context, string, []string) error             { return nil }
func (f *createFake) PermissionsBoundary(context.Context, string) (string, error) {
	if err := f.event("boundary"); err != nil {
		return "", err
	}
	return "arn:boundary", nil
}
func (f *createFake) RoleExists(context.Context, string) (bool, error) {
	if err := f.event("role-exists"); err != nil {
		return false, err
	}
	return f.roleExists, nil
}
func (f *createFake) CreateRole(_ context.Context, s cloud.RoleSpec) error {
	f.roleSpec = s
	return f.event("create-role")
}
func (f *createFake) PutRolePolicy(_ context.Context, _, name, doc string) error {
	f.policyName, f.policy = name, doc
	return f.event("put-policy")
}
func (f *createFake) DeleteRolePolicy(context.Context, string, string) error { return nil }
func (f *createFake) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	if err := f.event("profile-exists"); err != nil {
		return nil, false, err
	}
	return nil, f.profileExists, nil
}
func (f *createFake) CreateInstanceProfile(context.Context, string) error {
	return f.event("create-profile")
}
func (f *createFake) AddRoleToInstanceProfile(context.Context, string, string) error {
	return f.event("add-role")
}
func (f *createFake) RemoveRoleFromInstanceProfile(context.Context, string, string) error { return nil }
func (f *createFake) DeleteInstanceProfile(context.Context, string) error                 { return nil }
func (f *createFake) DeleteRole(context.Context, string) error                            { return nil }

func TestCreateCheckoutFailuresPrecedeConnection(t *testing.T) {
	//
	t.Run("checkout", func(t *testing.T) {
		want := errors.New("git sentinel")
		deps := seam.Deps{Dir: t.TempDir(), Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{}, want
		}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("cloud after checkout failure")
			return cloud.Clients{}, nil
		}}
		var out bytes.Buffer
		err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, "test-version", &out, deps)
		if !errors.Is(err, want) || out.Len() != 0 {
			t.Fatalf("err=%v out=%q", err, out.String())
		}
	})
	t.Run("release before root", func(t *testing.T) {
		f := newCreateFake(t)
		mustWrite(t, filepath.Join(f.root, "crm", "etc", "manifest.toml"), "invalid toml [")
		mustWrite(t, filepath.Join(f.root, "infra", "terraform.tfvars.json"), "invalid json")
		var out bytes.Buffer
		err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, "test-version", &out, f.deps())
		var manifest *checkout.RootFileError
		if !errors.As(err, &manifest) || out.Len() != 0 || f.openProfile != "" {
			t.Fatalf("err=%v out=%q profile=%q", err, out.String(), f.openProfile)
		}
	})
	t.Run("root", func(t *testing.T) {
		f := newCreateFake(t)
		mustWrite(t, filepath.Join(f.root, "infra", "terraform.tfvars.json"), "invalid json")
		var out bytes.Buffer
		err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, "test-version", &out, f.deps())
		var root *checkout.RootFileError
		if !errors.As(err, &root) || out.Len() != 0 || f.openProfile != "" {
			t.Fatalf("err=%v out=%q profile=%q", err, out.String(), f.openProfile)
		}
	})
}

func TestCreatePropagatesCloudFailuresAndStops(t *testing.T) {
	// R-V1MM-6L6Y R-V6I7-PO5Q R-V7Q4-3FWF R-V8Y0-H7N4 R-VA5W-UZDT R-VIP7-JDKO
	for _, stage := range []string{"zone", "template", "boundary", "ns", "spaces", "role-exists", "profile-exists", "create-role", "put-policy", "create-profile", "add-role", "launch-ready", "run-instance", "allocate", "associate", "records", "checks"} {
		t.Run(stage, func(t *testing.T) {
			f := newCreateFake(t)
			f.stopAt = stage
			f.stopErr = errors.New("sentinel")
			var out bytes.Buffer
			err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, "build-version", &out, f.deps())
			if !errors.Is(err, f.stopErr) {
				t.Fatalf("error %v want unchanged %v", err, f.stopErr)
			}
			if f.events[len(f.events)-1] != stage {
				t.Fatalf("later events %v", f.events)
			}
			steps := map[string]string{"launch-ready": "role:", "run-instance": "instance:", "allocate": "address:", "associate": "address:", "records": "records:", "checks": "host:"}
			if step := steps[stage]; step != "" && strings.Contains(out.String(), step) {
				t.Fatalf("failed step output %q", out.String())
			}
			if strings.Contains(out.String(), "sbx1.ikigenba.dev 18.118.7.42") {
				t.Fatalf("final output on failure %q", out.String())
			}
		})
	}
}

func TestCreateForwardsVersionIntoBuiltRelease(t *testing.T) {
	// R-V2UI-KCXN
	f := newCreateFake(t)
	deps := f.deps()
	exec := deps.Exec
	seen := false
	const version = "version-forwarding-sentinel"
	deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path == "tar" {
			fixture, err := os.OpenRoot(c.Args[3])
			if err != nil {
				t.Fatal(err)
			}
			data, err := fixture.ReadFile(createSHA + "/release.json")
			closeErr := fixture.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			var metadata struct{ SHA, Devctl string }
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.Devctl != version || metadata.SHA != createSHA {
				t.Fatalf("metadata %#v", metadata)
			}
			seen = true
		}
		return exec(ctx, c)
	}
	if err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, version, io.Discard, deps); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("archive was not built")
	}
}

func TestCreateReleaseQueryErrorsPropagate(t *testing.T) {
	// R-V0EP-STG9
	for _, query := range []string{"tag", "resolve"} {
		t.Run(query, func(t *testing.T) {
			f := newCreateFake(t)
			deps := f.deps()
			exec := deps.Exec
			sentinel := errors.New("git release sentinel")
			cloudCalls := 0
			queries := 0
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				cloudCalls++
				t.Fatal("cloud after resolution failure")
				return cloud.Clients{}, nil
			}
			deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
				if c.Path != "git" {
					t.Fatalf("unexpected command %#v", c)
				}
				if query == "tag" && c.Args[0] == "tag" || query == "resolve" && len(c.Args) > 1 && c.Args[1] == "--verify" {
					queries++
					return seam.Result{}, sentinel
				}
				return exec(ctx, c)
			}
			args := []string{"sbx1", "--acme-email=x"}
			if query == "resolve" {
				args = append(args, "--release=r2")
			}
			var out bytes.Buffer
			err := Run(context.Background(), args, "version", &out, deps)
			want := "git rev-parse: " + sentinel.Error()
			if query == "tag" {
				want = "git tag --list --no-column: " + sentinel.Error()
			}
			if !errors.Is(err, sentinel) || err.Error() != want || queries != 1 || cloudCalls != 0 || out.Len() != 0 {
				t.Fatalf("error %v queries %d cloud %d output %q", err, queries, cloudCalls, out.String())
			}
		})
	}
}
