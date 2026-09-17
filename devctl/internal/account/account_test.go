package account

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type fakeSSM struct {
	value string
	err   error
	names []string
}

func (f *fakeSSM) GetParameter(_ context.Context, name string) (string, error) {
	f.names = append(f.names, name)
	return f.value, f.err
}

func (*fakeSSM) PutSecureParameter(context.Context, string, string) error { return nil }
func (*fakeSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, nil
}
func (*fakeSSM) DeleteParameter(context.Context, string) error { return nil }

type openCall struct {
	profile string
	region  string
}

func validPropertiesJSON() string {
	return `{
		"domain":"example.test",
		"backup_bucket":"backups",
		"launch_template_id":"lt-123",
		"permissions_boundary_arn":"arn:boundary",
		"region":"us-test-1",
		"delete_secrets_on_destroy":true,
		"delete_backups_on_destroy":false,
		"backup_host_files_seconds":1,
		"backup_service_files_seconds":2,
		"backup_service_db_seconds":3,
		"backup_service_wal_seconds":4
	}`
}

func TestExportedDataShapes(t *testing.T) {
	// R-Z5LL-ZM39
	if PropertiesParameter != "/ikigenba/account" {
		t.Fatalf("PropertiesParameter = %q", PropertiesParameter)
	}

	// R-Z81E-R5KN
	assertFields(t, reflect.TypeOf(Space{}), []fieldSpec{
		{"Domain", reflect.TypeFor[string]()},
		{"ID", reflect.TypeFor[string]()},
		{"State", reflect.TypeFor[cloud.InstanceState]()},
		{"Address", reflect.TypeFor[string]()},
	})

	// R-C9YK-YEOO
	assertFields(t, reflect.TypeOf(Properties{}), []fieldSpec{
		{"Domain", reflect.TypeFor[string]()},
		{"BackupBucket", reflect.TypeFor[string]()},
		{"LaunchTemplateID", reflect.TypeFor[string]()},
		{"PermissionsBoundaryARN", reflect.TypeFor[string]()},
		{"Region", reflect.TypeFor[string]()},
		{"DeleteSecretsOnDestroy", reflect.TypeFor[bool]()},
		{"DeleteBackupsOnDestroy", reflect.TypeFor[bool]()},
		{"BackupHostFilesSeconds", reflect.TypeFor[int]()},
		{"BackupServiceFilesSeconds", reflect.TypeFor[int]()},
		{"BackupServiceDBSeconds", reflect.TypeFor[int]()},
		{"BackupServiceWALSeconds", reflect.TypeFor[int]()},
	})
}

type fieldSpec struct {
	name string
	typ  reflect.Type
}

func assertFields(t *testing.T, typ reflect.Type, want []fieldSpec) {
	t.Helper()
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typ.Name(), typ.NumField(), len(want))
	}
	for i, field := range want {
		got := typ.Field(i)
		if got.Name != field.name || got.Type != field.typ {
			t.Fatalf("%s field %d = %s %s, want %s %s", typ.Name(), i, got.Name, got.Type, field.name, field.typ)
		}
	}
}

func TestMissingErrors(t *testing.T) {
	// R-Z99B-4XBC
	assertFields(t, reflect.TypeOf(NoSpaceError{}), []fieldSpec{{"Domain", reflect.TypeFor[string]()}})
	assertFields(t, reflect.TypeOf(NoZoneError{}), []fieldSpec{{"Domain", reflect.TypeFor[string]()}})
	if got := (&NoSpaceError{Domain: "app.example"}).Error(); got != "no space at 'app.example'" {
		t.Fatalf("NoSpaceError = %q", got)
	}
	if got := (&NoZoneError{Domain: "app.example"}).Error(); got != "no hosted zone for 'app.example'" {
		t.Fatalf("NoZoneError = %q", got)
	}
}

func TestOpenUsesBootstrapThenConfiguredRegion(t *testing.T) {
	// R-ZAH7-IP21
	ssm := &fakeSSM{value: validPropertiesJSON()}
	regional := cloud.Clients{SSM: &fakeSSM{}}
	var calls []openCall
	deps := seam.Deps{Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
		calls = append(calls, openCall{profile, region})
		if len(calls) == 1 {
			return cloud.Clients{SSM: ssm}, nil
		}
		return regional, nil
	}}

	account, err := Open(context.Background(), deps, "  Mixed Profile  ")
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []openCall{{"  Mixed Profile  ", ""}, {"  Mixed Profile  ", "us-test-1"}}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if !reflect.DeepEqual(ssm.names, []string{PropertiesParameter}) {
		t.Fatalf("parameter names = %#v", ssm.names)
	}
	if account.Profile != "  Mixed Profile  " || !reflect.DeepEqual(account.Clients, regional) {
		t.Fatalf("account = %#v", account)
	}
	wantProperties := Properties{
		Domain: "example.test", BackupBucket: "backups", LaunchTemplateID: "lt-123",
		PermissionsBoundaryARN: "arn:boundary", Region: "us-test-1",
		DeleteSecretsOnDestroy: true, DeleteBackupsOnDestroy: false,
		BackupHostFilesSeconds: 1, BackupServiceFilesSeconds: 2,
		BackupServiceDBSeconds: 3, BackupServiceWALSeconds: 4,
	}
	if !reflect.DeepEqual(account.Properties, wantProperties) {
		t.Fatalf("properties = %#v, want %#v", account.Properties, wantProperties)
	}
}

func TestOpenStopsAfterReadOrDecodeFailure(t *testing.T) {
	// R-ZAH7-IP21
	readErr := errors.New("read failed")
	for _, tc := range []struct {
		name string
		ssm  *fakeSSM
	}{
		{"read", &fakeSSM{err: readErr}},
		{"decode", &fakeSSM{value: `[]`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			deps := seam.Deps{Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				calls++
				return cloud.Clients{SSM: tc.ssm}, nil
			}}
			account, err := Open(context.Background(), deps, "profile")
			if err == nil || account != nil {
				t.Fatalf("Open = %#v, %v", account, err)
			}
			if calls != 1 {
				t.Fatalf("cloud calls = %d, want 1", calls)
			}
		})
	}
}

func TestOpenRequiresTypedProperties(t *testing.T) {
	// R-ZBP3-WGSQ
	invalid := []string{`[]`, `null`}
	keys := []string{
		"domain", "backup_bucket", "launch_template_id", "permissions_boundary_arn", "region",
		"delete_secrets_on_destroy", "delete_backups_on_destroy", "backup_host_files_seconds",
		"backup_service_files_seconds", "backup_service_db_seconds", "backup_service_wal_seconds",
	}
	for _, key := range keys {
		invalid = append(invalid,
			strings.Replace(validPropertiesJSON(), `"`+key+`"`, `"missing_`+key+`"`, 1),
			strings.Replace(validPropertiesJSON(), `"`+key+`":`+valueForKey(key), `"`+key+`":`+wrongValueForKey(key), 1),
		)
	}

	for _, value := range invalid {
		account, err := openValue(t, value)
		if account != nil || err == nil || !strings.HasPrefix(err.Error(), PropertiesParameter+": ") {
			t.Fatalf("Open(%s) = %#v, %v", value, account, err)
		}
	}

	withExtra := strings.TrimSuffix(validPropertiesJSON(), "\n\t}") + `, "future_key":{"anything":true}}`
	if account, err := openValue(t, withExtra); err != nil || account == nil {
		t.Fatalf("Open with extra key = %#v, %v", account, err)
	}
}

func wrongValueForKey(key string) string {
	if key == "delete_secrets_on_destroy" || key == "delete_backups_on_destroy" || strings.HasSuffix(key, "_seconds") {
		return `"wrong type"`
	}
	return "false"
}

func valueForKey(key string) string {
	if key == "delete_secrets_on_destroy" {
		return "true"
	}
	if key == "delete_backups_on_destroy" {
		return "false"
	}
	if strings.HasSuffix(key, "_seconds") {
		return map[string]string{
			"backup_host_files_seconds": "1", "backup_service_files_seconds": "2",
			"backup_service_db_seconds": "3", "backup_service_wal_seconds": "4",
		}[key]
	}
	return map[string]string{
		"domain": `"example.test"`, "backup_bucket": `"backups"`,
		"launch_template_id": `"lt-123"`, "permissions_boundary_arn": `"arn:boundary"`,
		"region": `"us-test-1"`,
	}[key]
}

func TestOpenBackupPeriodsAndObsoleteKeys(t *testing.T) {
	// R-CB6H-C6FD
	zero := strings.NewReplacer(":1", ":0", ":2", ":0", ":3", ":0", ":4", ":0").Replace(validPropertiesJSON())
	zero = strings.TrimSuffix(zero, "\n\t}") + `,
		"deploy_from_main_only":"wrong type is ignored",
		"backup_full_seconds":-1,
		"backup_incremental_seconds":-1,
		"backup_wal_seconds":-1}`
	account, err := openValue(t, zero)
	if err != nil || account == nil {
		t.Fatalf("zero periods with obsolete keys = %#v, %v", account, err)
	}
	if account.Properties.BackupHostFilesSeconds != 0 || account.Properties.BackupServiceWALSeconds != 0 {
		t.Fatalf("properties = %#v", account.Properties)
	}

	negative := strings.Replace(validPropertiesJSON(), `"backup_service_db_seconds":3`, `"backup_service_db_seconds":-1`, 1)
	account, err = openValue(t, negative)
	if account != nil || err == nil || !strings.HasPrefix(err.Error(), PropertiesParameter+": ") {
		t.Fatalf("negative period = %#v, %v", account, err)
	}
}

func openValue(t *testing.T, value string) (*Account, error) {
	t.Helper()
	calls := 0
	deps := seam.Deps{Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		calls++
		return cloud.Clients{SSM: &fakeSSM{value: value}}, nil
	}}
	account, err := Open(context.Background(), deps, "profile")
	if err != nil && calls != 1 {
		t.Fatalf("cloud calls after error = %d, want 1", calls)
	}
	return account, err
}
