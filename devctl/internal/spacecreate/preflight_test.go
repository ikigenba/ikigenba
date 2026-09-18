package spacecreate

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestPreflightOpensCheckoutBeforeCloud(t *testing.T) {
	// R-Y4DW-LSA9
	for _, test := range []struct {
		name       string
		execResult seam.Result
		execErr    error
	}{
		{name: "open fails", execErr: errors.New("git unavailable")},
		{name: "apps fail", execResult: seam.Result{Stdout: []byte(filepath.Join(t.TempDir(), "missing") + "\n")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cloudCalls := 0
			deps := seam.Deps{
				Dir: t.TempDir(),
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					return test.execResult, test.execErr
				},
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					cloudCalls++
					return cloud.Clients{}, nil
				},
			}
			if _, err := preflight(context.Background(), deps, "sandbox", "foo.sbx.ikigenba.dev"); err == nil {
				t.Fatal("preflight error = nil, want checkout failure")
			}
			if cloudCalls != 0 {
				t.Fatalf("Cloud calls = %d, want 0", cloudCalls)
			}
		})
	}
}

func TestPreflightCompletesReadOnlyChecksInOrder(t *testing.T) {
	// R-VVFF-8UQ5
	env := newPreflightEnv(t)
	got, err := preflight(context.Background(), env.deps(), "sandbox", "foo.sbx.ikigenba.dev")
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	wantEvents := []string{
		"checkout:open", "cloud:bootstrap", "ssm:get", "cloud:regional",
		"route53:zones", "route53:records", "ec2:spaces", "iam:role",
		"iam:profile", "sts:caller",
	}
	if !reflect.DeepEqual(env.events, wantEvents) {
		t.Fatalf("events = %v, want %v", env.events, wantEvents)
	}
	if len(env.mutations) != 0 {
		t.Fatalf("mutations before preflight completed = %v", env.mutations)
	}
	if got.accountID != "123456789012" || got.zone != env.zone || got.account.Properties.Domain != env.accountDomain {
		t.Fatalf("result = %#v, want gathered account, zone and caller id", got)
	}
}

func TestPreflightAccountDomain(t *testing.T) {
	// R-Y6TP-DBRN
	env := newPreflightEnv(t)
	_, err := preflight(context.Background(), env.deps(), "development", "foo.example.com")
	assertRefusal(t, err, "'foo.example.com' does not end in the account domain 'sbx.ikigenba.dev'")
	if strings.Contains(strings.Join(env.events, ","), "route53:") {
		t.Fatalf("events after domain refusal = %v", env.events)
	}

	equal := newPreflightEnv(t)
	equal.accountDomain = "ikigenba.dev"
	equal.zone = cloud.Zone{ID: "ZROOT", Name: "ikigenba.dev"}
	if _, err := preflight(context.Background(), equal.deps(), "sandbox", "ikigenba.dev"); err != nil {
		t.Fatalf("equal account domain refused: %v", err)
	}
}

func TestPreflightRejectsDelegatedDomains(t *testing.T) {
	// R-Y81L-R3IC
	for _, domain := range []string{"foo.sbx.ikigenba.dev", "sbx.ikigenba.dev"} {
		t.Run(domain, func(t *testing.T) {
			env := newPreflightEnv(t)
			env.zone = cloud.Zone{ID: "ZROOT", Name: "ikigenba.dev"}
			env.records = []cloud.Record{{Name: "sbx.ikigenba.dev", Type: "NS"}}
			_, err := preflight(context.Background(), env.deps(), "sandbox", domain)
			assertRefusal(t, err, "'"+domain+"' is delegated away from this account's zone 'ikigenba.dev'")
			if len(env.mutations) != 0 {
				t.Fatalf("mutations = %v, want none", env.mutations)
			}
		})
	}
}

func TestPreflightRejectsExistingSpace(t *testing.T) {
	// R-Y99I-4V91
	env := newPreflightEnv(t)
	env.instances = []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: "foo.sbx.ikigenba.dev", State: cloud.StateRunning}}
	_, err := preflight(context.Background(), env.deps(), "sandbox", "foo.sbx.ikigenba.dev")
	assertRefusal(t, err, "a space at 'foo.sbx.ikigenba.dev' already exists (i-0c9e94542d98846a8)")
	if len(env.mutations) != 0 {
		t.Fatalf("mutations = %v, want none", env.mutations)
	}
}

func TestValidateSpacesRejectsOnlyLabelBoundedNesting(t *testing.T) {
	// R-3NDP-RQTX
	tests := []struct {
		name          string
		domain        string
		accountDomain string
		spaces        []account.Space
		want          string
	}{
		{
			name: "unknown app label below space", domain: "unknown.api.foo.sbx.ikigenba.dev", accountDomain: "sbx.ikigenba.dev",
			spaces: []account.Space{{Domain: "foo.sbx.ikigenba.dev"}},
			want:   "'unknown.api.foo.sbx.ikigenba.dev' lies under space 'foo.sbx.ikigenba.dev'",
		},
		{
			name: "create parent after descendant", domain: "foo.sbx.ikigenba.dev", accountDomain: "sbx.ikigenba.dev",
			spaces: []account.Space{{Domain: "deep.api.foo.sbx.ikigenba.dev"}},
			want:   "'foo.sbx.ikigenba.dev' would contain space 'deep.api.foo.sbx.ikigenba.dev'",
		},
		{
			name: "first returned conflict", domain: "foo.sbx.ikigenba.dev", accountDomain: "sbx.ikigenba.dev",
			spaces: []account.Space{{Domain: "a.foo.sbx.ikigenba.dev"}, {Domain: "b.foo.sbx.ikigenba.dev"}},
			want:   "'foo.sbx.ikigenba.dev' would contain space 'a.foo.sbx.ikigenba.dev'",
		},
		{
			name: "account domain may contain spaces", domain: "sbx.ikigenba.dev", accountDomain: "sbx.ikigenba.dev",
			spaces: []account.Space{{Domain: "foo.sbx.ikigenba.dev"}},
		},
		{
			name: "existing account domain may contain creation", domain: "foo.sbx.ikigenba.dev", accountDomain: "sbx.ikigenba.dev",
			spaces: []account.Space{{Domain: "sbx.ikigenba.dev"}},
		},
		{
			name: "textual suffix is not a label", domain: "notfoo.sbx.ikigenba.dev", accountDomain: "sbx.ikigenba.dev",
			spaces: []account.Space{{Domain: "foo.sbx.ikigenba.dev"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSpaces(test.domain, test.accountDomain, test.spaces)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateSpaces: %v, want nil", err)
				}
				return
			}
			assertRefusal(t, err, test.want)
		})
	}
}

func TestPreflightRejectsExistingRoleOrProfile(t *testing.T) {
	// R-YBPA-WEQF
	for _, test := range []struct {
		name          string
		roleExists    bool
		profileExists bool
	}{
		{name: "role", roleExists: true},
		{name: "instance profile", profileExists: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newPreflightEnv(t)
			env.roleExists = test.roleExists
			env.profileExists = test.profileExists
			_, err := preflight(context.Background(), env.deps(), "sandbox", "foo.sbx.ikigenba.dev")
			assertRefusal(t, err, "a role for 'foo.sbx.ikigenba.dev' already exists (ikigenba-space-foo.sbx.ikigenba.dev)")
			wantTail := []string{"iam:role", "iam:profile"}
			if got := env.events[len(env.events)-2:]; !reflect.DeepEqual(got, wantTail) {
				t.Fatalf("last events = %v, want %v", got, wantTail)
			}
		})
	}
}

func TestPushSecretsAndReportUsesSecretsBarrier(t *testing.T) {
	// R-YFD0-1PYI
	t.Run("success", func(t *testing.T) {
		env := newPreflightEnv(t)
		acct := env.account()
		result := preflightResult{
			account: acct,
			zone:    cloud.Zone{ID: "Z02587302QXWONVKW632", Name: "sbx.ikigenba.dev"},
			apps: []checkout.App{
				{Name: "crm", Manifest: checkout.Manifest{App: "crm"}},
				{Name: "mail", Manifest: checkout.Manifest{App: "mail"}},
				{Name: "web", Manifest: checkout.Manifest{App: "web"}},
			},
		}
		var stdout bytes.Buffer
		entries, err := pushSecretsAndReport(context.Background(), env.deps(), "foo.sbx.ikigenba.dev", result, &stdout)
		if err != nil {
			t.Fatalf("pushSecretsAndReport: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("entries = %d, want 3", len(entries))
		}
		wantMutations := []string{
			"ssm:put:/ikigenba/foo.sbx.ikigenba.dev/crm",
			"ssm:put:/ikigenba/foo.sbx.ikigenba.dev/mail",
			"ssm:put:/ikigenba/foo.sbx.ikigenba.dev/web",
		}
		if !reflect.DeepEqual(env.mutations, wantMutations) {
			t.Fatalf("secret writes = %v, want ordered %v", env.mutations, wantMutations)
		}
		want := "account: ok (sbx.ikigenba.dev, us-east-2)\n" +
			"domain: ok (zone sbx.ikigenba.dev Z02587302QXWONVKW632)\n" +
			"secrets: ok (3 apps)\n"
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	})

	t.Run("missing secret", func(t *testing.T) {
		env := newPreflightEnv(t)
		result := preflightResult{
			account: env.account(), zone: env.zone,
			apps: []checkout.App{{Name: "crm", Manifest: checkout.Manifest{App: "crm", Secrets: []string{"CRM_API_KEY"}}}},
		}
		deps := env.deps()
		deps.Getenv = func(string) string { return "" }
		deps.Exec = func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			if cmd.Path != "secret-tool" {
				t.Fatalf("Exec path = %q, want secret-tool", cmd.Path)
			}
			return seam.Result{ExitCode: 1}, nil
		}
		var stdout bytes.Buffer
		_, err := pushSecretsAndReport(context.Background(), deps, "foo.sbx.ikigenba.dev", result, &stdout)
		if got, want := err.Error(), "crm: no value for 'CRM_API_KEY' in the keyring or the environment"; got != want {
			t.Fatalf("error = %q, want %q", got, want)
		}
		if stdout.Len() != 0 || len(env.mutations) != 0 {
			t.Fatalf("failure produced stdout %q or mutations %v", stdout.String(), env.mutations)
		}
	})

	t.Run("push error unchanged", func(t *testing.T) {
		env := newPreflightEnv(t)
		pushErr := errors.New("put failed")
		env.putErr = pushErr
		result := preflightResult{
			account: env.account(), zone: env.zone,
			apps: []checkout.App{{Name: "crm", Manifest: checkout.Manifest{App: "crm"}}},
		}
		var stdout bytes.Buffer
		_, err := pushSecretsAndReport(context.Background(), env.deps(), "foo.sbx.ikigenba.dev", result, &stdout)
		if !errors.Is(err, pushErr) {
			t.Fatalf("error = %v, want unchanged %v", err, pushErr)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout.String())
		}
	})
}

func assertRefusal(t *testing.T, err error, want string) {
	t.Helper()
	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("error = %T %v, want *RefusedError", err, err)
	}
	if refused.Message != want {
		t.Fatalf("message = %q, want %q", refused.Message, want)
	}
}

type preflightEnv struct {
	t             *testing.T
	root          string
	events        []string
	mutations     []string
	accountDomain string
	zone          cloud.Zone
	records       []cloud.Record
	instances     []cloud.Instance
	roleExists    bool
	profileExists bool
	putErr        error
}

func newPreflightEnv(t *testing.T) *preflightEnv {
	t.Helper()
	return &preflightEnv{
		t:             t,
		root:          t.TempDir(),
		accountDomain: "sbx.ikigenba.dev",
		zone:          cloud.Zone{ID: "Z02587302QXWONVKW632", Name: "sbx.ikigenba.dev"},
	}
}

func (e *preflightEnv) deps() seam.Deps {
	cloudCalls := 0
	return seam.Deps{
		Dir: e.root,
		Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			e.events = append(e.events, "checkout:open")
			if cmd.Path != "git" {
				e.t.Fatalf("checkout command = %#v", cmd)
			}
			return seam.Result{Stdout: []byte(e.root + "\n")}, nil
		},
		Cloud: func(_ context.Context, _, _ string) (cloud.Clients, error) {
			cloudCalls++
			if cloudCalls == 1 {
				e.events = append(e.events, "cloud:bootstrap")
				return cloud.Clients{SSM: fakeSSM{env: e}}, nil
			}
			e.events = append(e.events, "cloud:regional")
			return e.clients(), nil
		},
	}
}

func (e *preflightEnv) account() *account.Account {
	return &account.Account{
		Profile: "sandbox",
		Properties: account.Properties{
			Domain: e.accountDomain, Region: "us-east-2", BackupBucket: "backups",
			LaunchTemplateID: "lt-123", PermissionsBoundaryARN: "arn:boundary",
		},
		Clients: e.clients(),
	}
}

func (e *preflightEnv) clients() cloud.Clients {
	return cloud.Clients{
		EC2: fakeEC2{env: e}, SSM: fakeSSM{env: e}, Route53: fakeRoute53{env: e},
		S3: fakeS3{}, IAM: fakeIAM{env: e}, STS: fakeSTS{env: e},
	}
}

func (e *preflightEnv) propertiesJSON() string {
	return `{"domain":"` + e.accountDomain + `","backup_bucket":"backups","launch_template_id":"lt-123","permissions_boundary_arn":"arn:boundary","region":"us-east-2","delete_secrets_on_destroy":false,"delete_backups_on_destroy":false,"backup_host_files_seconds":0,"backup_service_files_seconds":0,"backup_service_db_seconds":0,"backup_service_wal_seconds":0}`
}

type fakeSSM struct {
	env *preflightEnv
}

func (f fakeSSM) GetParameter(context.Context, string) (string, error) {
	f.env.events = append(f.env.events, "ssm:get")
	return f.env.propertiesJSON(), nil
}
func (f fakeSSM) PutSecureParameter(_ context.Context, name, _ string) error {
	f.env.mutations = append(f.env.mutations, "ssm:put:"+name)
	return f.env.putErr
}
func (fakeSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) { return nil, nil }
func (fakeSSM) DeleteParameter(context.Context, string) error                     { return nil }

type fakeRoute53 struct{ env *preflightEnv }

func (f fakeRoute53) ListZones(context.Context) ([]cloud.Zone, error) {
	f.env.events = append(f.env.events, "route53:zones")
	return []cloud.Zone{f.env.zone}, nil
}
func (f fakeRoute53) ListRecords(context.Context, string) ([]cloud.Record, error) {
	f.env.events = append(f.env.events, "route53:records")
	return f.env.records, nil
}
func (f fakeRoute53) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	f.env.mutations = append(f.env.mutations, "route53:change")
	return "", nil
}
func (fakeRoute53) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	return cloud.ChangeInsync, nil
}

type fakeEC2 struct{ env *preflightEnv }

func (f fakeEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	f.env.events = append(f.env.events, "ec2:spaces")
	return f.env.instances, nil
}
func (fakeEC2) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (f fakeEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	f.env.mutations = append(f.env.mutations, "ec2:run")
	return cloud.Instance{}, nil
}
func (fakeEC2) StartInstance(context.Context, string) error { return nil }
func (fakeEC2) StopInstance(context.Context, string) error  { return nil }
func (fakeEC2) TerminateInstance(context.Context, string) error {
	return nil
}
func (fakeEC2) InstanceChecksPassed(context.Context, string) (bool, error)  { return true, nil }
func (fakeEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) { return nil, nil }
func (f fakeEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	f.env.mutations = append(f.env.mutations, "ec2:allocate")
	return cloud.Address{}, nil
}
func (f fakeEC2) AssociateAddress(context.Context, string, string) error {
	f.env.mutations = append(f.env.mutations, "ec2:associate")
	return nil
}
func (fakeEC2) DisassociateAddress(context.Context, string) error { return nil }
func (fakeEC2) ReleaseAddress(context.Context, string) error      { return nil }

type fakeIAM struct{ env *preflightEnv }

func (f fakeIAM) RoleExists(context.Context, string) (bool, error) {
	f.env.events = append(f.env.events, "iam:role")
	return f.env.roleExists, nil
}
func (f fakeIAM) CreateRole(context.Context, cloud.RoleSpec) error {
	f.env.mutations = append(f.env.mutations, "iam:create-role")
	return nil
}
func (f fakeIAM) PutRolePolicy(context.Context, string, string, string) error {
	f.env.mutations = append(f.env.mutations, "iam:put-policy")
	return nil
}
func (fakeIAM) DeleteRolePolicy(context.Context, string, string) error { return nil }
func (f fakeIAM) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	f.env.events = append(f.env.events, "iam:profile")
	return nil, f.env.profileExists, nil
}
func (f fakeIAM) CreateInstanceProfile(context.Context, string) error {
	f.env.mutations = append(f.env.mutations, "iam:create-profile")
	return nil
}
func (f fakeIAM) AddRoleToInstanceProfile(context.Context, string, string) error {
	f.env.mutations = append(f.env.mutations, "iam:add-role")
	return nil
}
func (fakeIAM) RemoveRoleFromInstanceProfile(context.Context, string, string) error { return nil }
func (fakeIAM) DeleteInstanceProfile(context.Context, string) error                 { return nil }
func (fakeIAM) DeleteRole(context.Context, string) error                            { return nil }

type fakeSTS struct{ env *preflightEnv }

func (f fakeSTS) CallerAccountID(context.Context) (string, error) {
	f.env.events = append(f.env.events, "sts:caller")
	return "123456789012", nil
}

type fakeS3 struct{}

func (fakeS3) ListObjects(context.Context, string, string) ([]cloud.Object, error) { return nil, nil }
func (fakeS3) PutObject(context.Context, string, string, io.Reader, int64) error   { return nil }
func (fakeS3) DeleteObjects(context.Context, string, []string) error               { return nil }
