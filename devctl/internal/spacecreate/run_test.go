package spacecreate

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
	// R-Z2ND-8MJF R-8TLD-OA2V R-8UTA-21TK R-8W16-FTK9 R-3M5T-DZ38 R-E7GU-RBY9 R-8X92-TLAY
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
			err := Run(context.Background(), tc.args, &stdout, deps)
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
	// R-8YGZ-7D1N
	for _, option := range []string{"--help", "-h"} {
		calls := 0
		deps := seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) { calls++; return seam.Result{}, nil }, Cloud: func(context.Context, string, string) (cloud.Clients, error) { calls++; return cloud.Clients{}, nil }}
		var stdout bytes.Buffer
		if err := Run(context.Background(), []string{option}, &stdout, deps); err != nil || stdout.String() != usageText || calls != 0 {
			t.Fatalf("help %s = %q, %v, calls %d", option, stdout.String(), err, calls)
		}
	}
}

func TestRunSuccessfulCreateUsesCurrentContracts(t *testing.T) {
	// R-8ZOV-L4SC R-924O-CO9Q R-9709-VR8I R-VTEC-13P5 R-9GRG-XX62 R-9HZD-BOWR
	// R-EB4J-WN6C R-9J79-PGNG R-YMOE-CCEO R-VUM8-EVFU R-EKVQ-YT3W R-9LN2-H04U R-YSRW-9745 R-9MUY-URVJ
	f := newCreateFake(t)
	f.objects = []cloud.Object{
		{Key: "sbx1/host/old.tar.zst", Modified: time.Unix(1, 0)},
		{Key: "sbx1/host/new.tar.zst", Modified: time.Unix(2, 0)},
	}
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"sbx1", "--acme-email", "ops@ikigenba.dev"}, &stdout, f.deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "account: ok (ikigenba.dev, us-east-2, 295229566359)\n" +
		"domain: ok (zone ikigenba.dev ZONE1)\n" +
		"secrets: ok (1 apps)\n" +
		"role: ok (sbx1.ikigenba.dev)\n" +
		"instance: ok (i-new running, 198.51.100.7)\n" +
		"address: ok (elastic ip 18.118.7.42 associated)\n" +
		"records: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)\n" +
		"host: ok (status checks passed, cloud-init done)\n" +
		"opsctl: ok (v9.8.7 installed, 10 keys set)\n" +
		"restore: ok (host/new.tar.zst, 10 keys set again)\n" +
		"init: ok\n" +
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
	wantPrefix := []string{"git", "sts", "zone", "template", "boundary", "ns", "spaces", "role-exists", "profile-exists", "secret", "create-role", "put-policy", "create-profile", "add-role", "launch-ready", "run-instance", "describe", "allocate", "associate", "records", "change-status", "checks"}
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
	joined := strings.Join(f.remotes, "\n")
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
			err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, &out, f.deps())
			var refused *RefusedError
			if !errors.As(err, &refused) || err.Error() != tc.want || out.Len() != 0 || contains(f.events, "secret") {
				t.Fatalf("err=%v out=%q events=%v", err, out.String(), f.events)
			}
		})
	}

	f := newCreateFake(t)
	long := strings.Repeat("a", 52)
	var out bytes.Buffer
	err := Run(context.Background(), []string{long, "--acme-email=x"}, &out, f.deps())
	if err == nil || !strings.Contains(err.Error(), "role name is at most 64 characters") || f.openProfile != "" {
		t.Fatalf("long role = %v, cloud profile %q", err, f.openProfile)
	}
	boundary := newCreateFake(t)
	boundary.stopAt = "zone"
	_ = Run(context.Background(), []string{strings.Repeat("a", 51), "--acme-email=x"}, io.Discard, boundary.deps())
	if boundary.openProfile == "" {
		t.Fatal("64-byte domain did not reach cloud")
	}
}

func TestCreateStopsAtSecretsAndEmptyRestoreSkipsRestoreCommands(t *testing.T) {
	// R-9709-VR8I R-9LN2-H04U
	failure := newCreateFake(t)
	failure.stopAt = "secret"
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, &stdout, failure.deps()); err == nil || stdout.Len() != 0 || contains(failure.events, "create-role") {
		t.Fatalf("secret failure = %v, stdout %q, events %v", err, stdout.String(), failure.events)
	}

	empty := newCreateFake(t)
	if err := Run(context.Background(), []string{"sbx1", "--acme-email=x"}, io.Discard, empty.deps()); err != nil {
		t.Fatalf("empty restore create: %v", err)
	}
	joined := strings.Join(empty.remotes, "\n")
	if strings.Contains(joined, "'opsctl' 'host' 'restore'") || strings.Contains(joined, "'host.apex'") || strings.Count(joined, "'opsctl' 'config' 'set'") != 10 {
		t.Fatalf("empty restore remote commands:\n%s", joined)
	}
}

func TestNewestBackupBreaksModifiedTieByKey(t *testing.T) {
	// R-9LN2-H04U
	when := time.Unix(1, 0)
	got := newestObject([]cloud.Object{{Key: "sbx1/host/a", Modified: when}, {Key: "sbx1/host/z", Modified: when}})
	if got.Key != "sbx1/host/z" {
		t.Fatalf("newestObject tie = %q, want greater key", got.Key)
	}
}

func resultSpace() spaceref.Space { return spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"} }

type createFake struct {
	t                                    *testing.T
	root                                 string
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
		return errors.New("stop at " + name)
	}
	return nil
}
func (f *createFake) deps() seam.Deps {
	return seam.Deps{Dir: f.root, EUID: 1, Getenv: func(string) string { return "" }, After: immediateAfter, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "git" {
			_ = f.event("git")
			return seam.Result{Stdout: []byte(f.root + "\n")}, nil
		}
		if cmd.Path == "curl" {
			_ = f.event("curl")
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v9.8.7","published_at":"2026-09-17T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://example.test/install.sh"}]}]`)}, nil
		}
		f.events = append(f.events, "ssh")
		f.remotes = append(f.remotes, cmd.Args[len(cmd.Args)-1])
		return seam.Result{}, nil
	}, Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
		f.openProfile, f.openRegion = profile, region
		return cloud.Clients{EC2: f, SSM: f, Route53: f, S3: f, IAM: f, STS: f}, nil
	}}
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
