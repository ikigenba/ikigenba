package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/spacecreate"
)

const (
	testAccount = "sbx.ikigenba.dev"
	testDomain  = "foo.sbx.ikigenba.dev"
)

func TestCreateCommandOpensCheckoutAndAppsBeforeCloud(t *testing.T) {
	// R-Y4DW-LSA9
	t.Run("checkout open fails", func(t *testing.T) {
		h := newCommandHarness(t)
		h.execErr = errors.New("git unavailable")
		result := h.invoke(testDomain)
		if result.code != 1 || result.stdout != "" || h.cloudCalls != 0 {
			t.Fatalf("result = %#v, cloud calls = %d, want exit 1, empty stdout, no cloud", result, h.cloudCalls)
		}
	})

	t.Run("checkout apps fails", func(t *testing.T) {
		h := newCommandHarness(t)
		h.checkoutRoot = filepath.Join(t.TempDir(), "missing")
		result := h.invoke(testDomain)
		if result.code != 1 || result.stdout != "" || h.cloudCalls != 0 {
			t.Fatalf("result = %#v, cloud calls = %d, want exit 1, empty stdout, no cloud", result, h.cloudCalls)
		}
		if !reflect.DeepEqual(h.events, []string{"exec:git"}) {
			t.Fatalf("events = %v, want checkout open only", h.events)
		}
	})
}

func TestCreateCommandCompletesReadOnlyPreflightBeforeEffects(t *testing.T) {
	// R-H7YL-8EPO
	wantPrefix := []string{
		"exec:git", "cloud:", "ssm:get", "cloud:us-east-2", "route53:zones",
		"route53:records", "ec2:spaces", "iam:role", "iam:profile", "sts:caller",
	}

	t.Run("ordered barrier", func(t *testing.T) {
		h := newCommandHarness(t)
		h.addApp("web")
		h.putErr = errors.New("stop at first mutation")
		result := h.invoke(testDomain)
		if result.stdout != "" {
			t.Fatalf("stdout = %q, want empty before secrets push succeeds", result.stdout)
		}
		want := append(append([]string{}, wantPrefix...), "ssm:put:web")
		if !reflect.DeepEqual(h.events, want) {
			t.Fatalf("events = %v, want %v", h.events, want)
		}
	})

	for _, stop := range wantPrefix[1:] {
		stop := stop
		t.Run("failure at "+stop, func(t *testing.T) {
			h := newCommandHarness(t)
			h.failAt = stop
			result := h.invoke(testDomain)
			if result.stdout != "" || len(h.mutations) != 0 {
				t.Fatalf("failure at %s produced stdout %q or mutations %v", stop, result.stdout, h.mutations)
			}
			if got := h.events[len(h.events)-1]; got != stop {
				t.Fatalf("last event = %q, want %q; all events %v", got, stop, h.events)
			}
		})
	}
}

func TestCreateCommandAccountDomainRefusalAndEquality(t *testing.T) {
	// R-Y6TP-DBRN
	h := newCommandHarness(t)
	assertCommandResult(t, h.invoke("foo.example.com"), 2, "", "devctl: 'foo.example.com' does not end in the account domain 'sbx.ikigenba.dev'\n")

	equal := newCommandHarness(t)
	equal.accountDomain = "ikigenba.dev"
	equal.zone = cloud.Zone{ID: "ZROOT", Name: "ikigenba.dev"}
	equal.createRoleErr = errors.New("stop after first steps")
	assertCommandResult(t, equal.invoke("ikigenba.dev"), 1,
		"account: ok (ikigenba.dev, us-east-2)\ndomain: ok (zone ikigenba.dev ZROOT)\nsecrets: ok (0 apps)\n",
		"devctl: stop after first steps\n")
}

func TestCreateCommandDelegationRefusals(t *testing.T) {
	// R-Y81L-R3IC
	for _, domain := range []string{"foo.sbx.ikigenba.dev", "sbx.ikigenba.dev"} {
		t.Run(domain, func(t *testing.T) {
			h := newCommandHarness(t)
			h.zone = cloud.Zone{ID: "ZROOT", Name: "ikigenba.dev"}
			h.records = []cloud.Record{{Name: "sbx.ikigenba.dev", Type: "NS"}}
			want := "devctl: '" + domain + "' is delegated away from this account's zone 'ikigenba.dev'\n"
			assertRefusedWithoutEffects(t, h, h.invoke(domain), want)
		})
	}
}

func TestCreateCommandExistingSpaceRefusal(t *testing.T) {
	// R-Y99I-4V91
	h := newCommandHarness(t)
	h.instances = []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: testDomain, State: cloud.StateRunning}}
	assertRefusedWithoutEffects(t, h, h.invoke(testDomain),
		"devctl: a space at 'foo.sbx.ikigenba.dev' already exists (i-0c9e94542d98846a8)\n")
}

func TestCreateCommandNestedSpaceMatrix(t *testing.T) {
	// R-3NDP-RQTX
	tests := []struct {
		name, domain string
		spaces       []cloud.Instance
		wantStderr   string
		wantPass     bool
	}{
		{
			name: "unknown app and multiple descendant labels", domain: "unknown.api.foo.sbx.ikigenba.dev",
			spaces:     []cloud.Instance{{ID: "i-parent", Space: testDomain, State: cloud.StateRunning}},
			wantStderr: "devctl: 'unknown.api.foo.sbx.ikigenba.dev' lies under space 'foo.sbx.ikigenba.dev'\n",
		},
		{
			name: "parent after descendant", domain: testDomain,
			spaces:     []cloud.Instance{{ID: "i-child", Space: "deep.api." + testDomain, State: cloud.StateRunning}},
			wantStderr: "devctl: 'foo.sbx.ikigenba.dev' would contain space 'deep.api.foo.sbx.ikigenba.dev'\n",
		},
		{
			name: "first returned conflict", domain: testDomain,
			spaces: []cloud.Instance{
				{ID: "i-a", Space: "a." + testDomain, State: cloud.StateRunning},
				{ID: "i-b", Space: "b." + testDomain, State: cloud.StateRunning},
			},
			wantStderr: "devctl: 'foo.sbx.ikigenba.dev' would contain space 'a.foo.sbx.ikigenba.dev'\n",
		},
		{
			name: "terminated space ignored", domain: "api." + testDomain,
			spaces: []cloud.Instance{
				{ID: "i-dead", Space: testDomain, State: cloud.StateTerminated},
			},
			wantPass: true,
		},
		{
			name: "account domain exception", domain: testAccount,
			spaces: []cloud.Instance{
				{ID: "i-child", Space: testDomain, State: cloud.StateRunning},
			},
			wantPass: true,
		},
		{
			name: "existing account domain exception", domain: testDomain,
			spaces: []cloud.Instance{
				{ID: "i-account", Space: testAccount, State: cloud.StateRunning},
			},
			wantPass: true,
		},
		{
			name: "textual suffix lacks label boundary", domain: "notfoo.sbx.ikigenba.dev",
			spaces: []cloud.Instance{
				{ID: "i-foo", Space: testDomain, State: cloud.StateRunning},
			},
			wantPass: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newCommandHarness(t)
			h.addApp("unrelated")
			h.instances = test.spaces
			if test.wantPass {
				h.putErr = errors.New("passed nesting barrier")
				result := h.invoke(test.domain)
				if result.stderr != "devctl: passed nesting barrier\n" || result.stdout != "" {
					t.Fatalf("result = %#v, want passage to first mutation", result)
				}
				return
			}
			assertRefusedWithoutEffects(t, h, h.invoke(test.domain), test.wantStderr)
		})
	}
}

func TestCreateCommandExistingRoleOrProfileRefusal(t *testing.T) {
	// R-YBPA-WEQF
	for _, test := range []struct {
		name          string
		roleExists    bool
		profileExists bool
	}{
		{name: "role alone", roleExists: true},
		{name: "instance profile alone", profileExists: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newCommandHarness(t)
			h.roleExists = test.roleExists
			h.profileExists = test.profileExists
			assertRefusedWithoutEffects(t, h, h.invoke(testDomain),
				"devctl: a role for 'foo.sbx.ikigenba.dev' already exists (ikigenba-space-foo.sbx.ikigenba.dev)\n")
		})
	}
}

func TestCreateRunSecretsBarrierUsesCheckoutApps(t *testing.T) {
	// R-YFD0-1PYI
	t.Run("ordered apps and unchanged push error", func(t *testing.T) {
		h := newCommandHarness(t)
		h.addApp("web")
		h.addApp("crm")
		pushErr := errors.New("put failed")
		h.putErrAt = "web"
		h.putErr = pushErr
		var stdout bytes.Buffer
		err := spacecreate.Run(context.Background(), []string{testDomain, "--acme-email", "admin@example.com"}, &stdout, h.deps(), "sandbox")
		if reflect.ValueOf(err).Pointer() != reflect.ValueOf(pushErr).Pointer() {
			t.Fatalf("error = %T %v, want identical error %p", err, err, pushErr)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout.String())
		}
		if !reflect.DeepEqual(h.mutations, []string{"ssm:put:crm", "ssm:put:web"}) {
			t.Fatalf("secret app order = %v, want checkout order [crm web]", h.mutations)
		}
	})

	t.Run("successful report", func(t *testing.T) {
		h := newCommandHarness(t)
		for _, app := range []string{"web", "crm", "mail"} {
			h.addApp(app)
		}
		h.createRoleErr = errors.New("stop after report")
		assertCommandResult(t, h.invoke(testDomain), 1,
			"account: ok (sbx.ikigenba.dev, us-east-2)\n"+
				"domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)\n"+
				"secrets: ok (3 apps)\n",
			"devctl: stop after report\n")
	})

	t.Run("missing secret command diagnostic", func(t *testing.T) {
		h := newCommandHarness(t)
		h.addAppWithSecrets("crm", "CRM_API_KEY")
		result := h.invoke(testDomain)
		assertCommandResult(t, result, 2, "", "devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment\n")
		if len(h.mutations) != 0 {
			t.Fatalf("mutations = %v, want no PutSecureParameter", h.mutations)
		}
	})
}

type commandResult struct {
	code           int
	stdout, stderr string
}

type commandHarness struct {
	t             *testing.T
	checkoutRoot  string
	events        []string
	mutations     []string
	cloudCalls    int
	execErr       error
	failAt        string
	accountDomain string
	zone          cloud.Zone
	records       []cloud.Record
	instances     []cloud.Instance
	roleExists    bool
	profileExists bool
	putErrAt      string
	putErr        error
	createRoleErr error
}

func newCommandHarness(t *testing.T) *commandHarness {
	t.Helper()
	return &commandHarness{
		t:             t,
		checkoutRoot:  t.TempDir(),
		accountDomain: testAccount,
		zone:          cloud.Zone{ID: "Z02587302QXWONVKW632", Name: testAccount},
	}
}

func (h *commandHarness) addApp(name string) { h.addAppWithSecrets(name) }

func (h *commandHarness) addAppWithSecrets(name string, secrets ...string) {
	h.t.Helper()
	dir := filepath.Join(h.checkoutRoot, name)
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o750); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		h.t.Fatal(err)
	}
	manifest := "app = \"" + name + "\"\n"
	if len(secrets) != 0 {
		quoted := make([]string, len(secrets))
		for index, secret := range secrets {
			quoted[index] = "\"" + secret + "\""
		}
		manifest += "secrets = [" + strings.Join(quoted, ", ") + "]\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "manifest.toml"), []byte(manifest), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *commandHarness) invoke(domain string) commandResult {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(),
		[]string{"--account", "sandbox", "space", "create", domain, "--acme-email", "admin@example.com"},
		strings.NewReader(""), &stdout, &stderr, h.deps())
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func (h *commandHarness) deps() seam.Deps {
	return seam.Deps{
		Dir:  h.checkoutRoot,
		EUID: 1,
		Getenv: func(string) string {
			return ""
		},
		Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			if cmd.Path == "git" {
				h.events = append(h.events, "exec:git")
				if h.execErr != nil {
					return seam.Result{}, h.execErr
				}
				return seam.Result{Stdout: []byte(h.checkoutRoot + "\n")}, nil
			}
			h.events = append(h.events, "exec:"+cmd.Path)
			return seam.Result{ExitCode: 1}, nil
		},
		Cloud: func(_ context.Context, _ string, region string) (cloud.Clients, error) {
			h.cloudCalls++
			if err := h.boundary("cloud:" + region); err != nil {
				return cloud.Clients{}, err
			}
			if region == "" {
				return cloud.Clients{SSM: harnessSSM{h: h}}, nil
			}
			return h.clients(), nil
		},
	}
}

func (h *commandHarness) clients() cloud.Clients {
	return cloud.Clients{
		EC2: harnessEC2{h: h}, SSM: harnessSSM{h: h}, Route53: harnessRoute53{h: h},
		S3: harnessS3{}, IAM: harnessIAM{h: h}, STS: harnessSTS{h: h},
	}
}

type commandStop struct{ name string }

func (e commandStop) Error() string { return "stop at " + e.name }

func (h *commandHarness) boundary(name string) error {
	h.events = append(h.events, name)
	if h.failAt == name {
		return commandStop{name: name}
	}
	return nil
}

func (h *commandHarness) propertiesJSON() string {
	return `{"domain":"` + h.accountDomain + `","backup_bucket":"backups","launch_template_id":"lt-123","permissions_boundary_arn":"arn:boundary","region":"us-east-2","delete_secrets_on_destroy":false,"delete_backups_on_destroy":false,"backup_host_files_seconds":0,"backup_service_files_seconds":0,"backup_service_db_seconds":0,"backup_service_wal_seconds":0}`
}

type harnessSSM struct {
	cloud.SSM
	h *commandHarness
}

func (f harnessSSM) GetParameter(context.Context, string) (string, error) {
	if err := f.h.boundary("ssm:get"); err != nil {
		return "", err
	}
	return f.h.propertiesJSON(), nil
}

func (f harnessSSM) PutSecureParameter(_ context.Context, name, _ string) error {
	app := filepath.Base(name)
	f.h.events = append(f.h.events, "ssm:put:"+app)
	f.h.mutations = append(f.h.mutations, "ssm:put:"+app)
	if f.h.putErr != nil && (f.h.putErrAt == "" || f.h.putErrAt == app) {
		return f.h.putErr
	}
	return nil
}

type harnessRoute53 struct {
	cloud.Route53
	h *commandHarness
}

func (f harnessRoute53) ListZones(context.Context) ([]cloud.Zone, error) {
	if err := f.h.boundary("route53:zones"); err != nil {
		return nil, err
	}
	return []cloud.Zone{f.h.zone}, nil
}

func (f harnessRoute53) ListRecords(context.Context, string) ([]cloud.Record, error) {
	if err := f.h.boundary("route53:records"); err != nil {
		return nil, err
	}
	return f.h.records, nil
}

type harnessEC2 struct {
	cloud.EC2
	h *commandHarness
}

func (f harnessEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	if err := f.h.boundary("ec2:spaces"); err != nil {
		return nil, err
	}
	return f.h.instances, nil
}

func (f harnessEC2) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) {
	f.h.mutations = append(f.h.mutations, "ec2:launch-ready")
	return true, nil
}

type harnessIAM struct {
	cloud.IAM
	h *commandHarness
}

func (f harnessIAM) RoleExists(context.Context, string) (bool, error) {
	if err := f.h.boundary("iam:role"); err != nil {
		return false, err
	}
	return f.h.roleExists, nil
}

func (f harnessIAM) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	if err := f.h.boundary("iam:profile"); err != nil {
		return nil, false, err
	}
	return nil, f.h.profileExists, nil
}

func (f harnessIAM) CreateRole(context.Context, cloud.RoleSpec) error {
	f.h.mutations = append(f.h.mutations, "iam:create-role")
	return f.h.createRoleErr
}

type harnessSTS struct {
	cloud.STS
	h *commandHarness
}

func (f harnessSTS) CallerAccountID(context.Context) (string, error) {
	if err := f.h.boundary("sts:caller"); err != nil {
		return "", err
	}
	return "123456789012", nil
}

type harnessS3 struct{ cloud.S3 }

func assertCommandResult(t *testing.T, got commandResult, code int, stdout, stderr string) {
	t.Helper()
	if got.code != code || got.stdout != stdout || got.stderr != stderr {
		t.Fatalf("result = %#v, want code %d, stdout %q, stderr %q", got, code, stdout, stderr)
	}
}

func assertRefusedWithoutEffects(t *testing.T, h *commandHarness, got commandResult, stderr string) {
	t.Helper()
	assertCommandResult(t, got, 2, "", stderr)
	if len(h.mutations) != 0 {
		t.Fatalf("mutations = %v, want none", h.mutations)
	}
}
