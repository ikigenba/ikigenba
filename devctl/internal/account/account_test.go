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

type fakeEC2 struct {
	instances []cloud.Instance
	err       error
}

func (f *fakeEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return f.instances, f.err
}
func (*fakeEC2) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (*fakeEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (*fakeEC2) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) { return false, nil }
func (*fakeEC2) StartInstance(context.Context, string) error                 { return nil }
func (*fakeEC2) StopInstance(context.Context, string) error                  { return nil }
func (*fakeEC2) TerminateInstance(context.Context, string) error             { return nil }
func (*fakeEC2) InstanceChecksPassed(context.Context, string) (bool, error)  { return false, nil }
func (*fakeEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) { return nil, nil }
func (*fakeEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	return cloud.Address{}, nil
}
func (*fakeEC2) AssociateAddress(context.Context, string, string) error { return nil }
func (*fakeEC2) DisassociateAddress(context.Context, string) error      { return nil }
func (*fakeEC2) ReleaseAddress(context.Context, string) error           { return nil }

type fakeRoute53 struct {
	zones      []cloud.Zone
	records    []cloud.Record
	err        error
	recordZone string
}

func (f *fakeRoute53) ListZones(context.Context) ([]cloud.Zone, error) { return f.zones, f.err }
func (f *fakeRoute53) ListRecords(_ context.Context, zoneID string) ([]cloud.Record, error) {
	f.recordZone = zoneID
	return f.records, f.err
}
func (*fakeRoute53) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	return "", nil
}
func (*fakeRoute53) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	return "", nil
}

type fakeSTS struct {
	id  string
	err error
}

func (f *fakeSTS) CallerAccountID(context.Context) (string, error) { return f.id, f.err }

func assertOpenType(func(context.Context, seam.Deps, string) (*Account, error)) {}

func TestAccountShapeAndCallerAccountID(t *testing.T) {
	// R-Z6TI-DDTY
	assertOpenType(Open)
	assertFields(t, reflect.TypeOf(Account{}), []fieldSpec{
		{"Profile", reflect.TypeFor[string]()},
		{"Properties", reflect.TypeFor[Properties]()},
		{"Clients", reflect.TypeFor[cloud.Clients]()},
	})
	var _ interface {
		CallerAccountID(context.Context) (string, error)
		Zone(context.Context, string) (cloud.Zone, error)
		Delegation(context.Context, cloud.Zone, string) (string, error)
		Spaces(context.Context) ([]Space, error)
		Space(context.Context, string) (Space, error)
	} = (*Account)(nil)

	// R-ZK8E-KUZL
	wantErr := errors.New("identity failed")
	for _, tc := range []struct {
		name string
		sts  *fakeSTS
	}{
		{"success", &fakeSTS{id: "123456789012"}},
		{"error", &fakeSTS{err: wantErr}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (&Account{Clients: cloud.Clients{STS: tc.sts}}).CallerAccountID(context.Background())
			if got != tc.sts.id || !errors.Is(err, tc.sts.err) {
				t.Fatalf("CallerAccountID = %q, %v", got, err)
			}
		})
	}
}

func TestSpaces(t *testing.T) {
	// R-TH13-SW5F
	ec2 := &fakeEC2{instances: []cloud.Instance{
		{ID: "i-z", Space: "z.example", State: cloud.StateStopped, Address: "192.0.2.2"},
		{ID: "i-dead", Space: "a.example", State: cloud.StateTerminated},
		{ID: "i-a", Space: "a.example", State: cloud.StateRunning, Address: "192.0.2.1"},
	}}
	account := &Account{Clients: cloud.Clients{EC2: ec2}}
	got, err := account.Spaces(context.Background())
	want := []Space{
		{Domain: "a.example", ID: "i-a", State: cloud.StateRunning, Address: "192.0.2.1"},
		{Domain: "z.example", ID: "i-z", State: cloud.StateStopped, Address: "192.0.2.2"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Spaces = %#v, %v; want %#v", got, err, want)
	}

	ec2.instances = nil
	got, err = account.Spaces(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("empty Spaces = %#v, %v", got, err)
	}

	ec2.instances = []cloud.Instance{
		{ID: "i-1", Space: "duplicate.example", State: cloud.StateRunning},
		{ID: "i-2", Space: "duplicate.example", State: cloud.StateStopped},
	}
	if _, err = account.Spaces(context.Background()); err == nil || !strings.Contains(err.Error(), "duplicate.example") {
		t.Fatalf("duplicate Spaces error = %v", err)
	}

	ec2.err = errors.New("list failed")
	if _, err = account.Spaces(context.Background()); !errors.Is(err, ec2.err) {
		t.Fatalf("Spaces error = %v", err)
	}
}

func TestSpace(t *testing.T) {
	// R-ZFCT-1S0T
	ec2 := &fakeEC2{instances: []cloud.Instance{{
		ID: "i-1", Space: "app.example", State: cloud.StateRunning, Address: "192.0.2.1",
	}}}
	account := &Account{Clients: cloud.Clients{EC2: ec2}}
	got, err := account.Space(context.Background(), "app.example")
	want := Space{Domain: "app.example", ID: "i-1", State: cloud.StateRunning, Address: "192.0.2.1"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Space = %#v, %v; want %#v", got, err, want)
	}

	_, err = account.Space(context.Background(), "missing.example")
	var noSpace *NoSpaceError
	if !errors.As(err, &noSpace) || noSpace.Domain != "missing.example" {
		t.Fatalf("missing Space error = %#v", err)
	}

	ec2.instances = append(ec2.instances, cloud.Instance{ID: "i-2", Space: "app.example", State: cloud.StateStopped})
	if _, err = account.Space(context.Background(), "app.example"); err == nil || !strings.Contains(err.Error(), "app.example") {
		t.Fatalf("duplicate Space error = %v", err)
	}
}

func TestZone(t *testing.T) {
	// R-ZGKP-FJRI
	route53 := &fakeRoute53{zones: []cloud.Zone{
		{ID: "broad", Name: "example"},
		{ID: "wrong-boundary", Name: "ample.example"},
		{ID: "specific", Name: "dev.example"},
	}}
	account := &Account{Clients: cloud.Clients{Route53: route53}}
	got, err := account.Zone(context.Background(), "api.dev.example")
	want := cloud.Zone{ID: "specific", Name: "dev.example"}
	if err != nil || got != want {
		t.Fatalf("Zone = %#v, %v; want %#v", got, err, want)
	}

	got, err = account.Zone(context.Background(), "dev.example")
	if err != nil || got != want {
		t.Fatalf("exact Zone = %#v, %v; want %#v", got, err, want)
	}

	_, err = account.Zone(context.Background(), "notexample")
	var noZone *NoZoneError
	if !errors.As(err, &noZone) || noZone.Domain != "notexample" {
		t.Fatalf("missing Zone error = %#v", err)
	}

	route53.err = errors.New("zones failed")
	if _, err = account.Zone(context.Background(), "dev.example"); !errors.Is(err, route53.err) {
		t.Fatalf("Zone error = %v", err)
	}
}

func TestDelegation(t *testing.T) {
	// R-ZJ0I-738W
	route53 := &fakeRoute53{records: []cloud.Record{
		{Name: "example", Type: "NS"},
		{Name: "dev.example", Type: "NS"},
		{Name: "api.dev.example", Type: "NS"},
		{Name: "host.api.dev.example", Type: "A"},
		{Name: "notapi.dev.example", Type: "NS"},
	}}
	account := &Account{Clients: cloud.Clients{Route53: route53}}
	zone := cloud.Zone{ID: "Z123", Name: "example"}
	got, err := account.Delegation(context.Background(), zone, "host.api.dev.example")
	if err != nil || got != "api.dev.example" || route53.recordZone != "Z123" {
		t.Fatalf("Delegation = %q, %v; zone = %q", got, err, route53.recordZone)
	}

	got, err = account.Delegation(context.Background(), zone, "other.example")
	if err != nil || got != "" {
		t.Fatalf("empty Delegation = %q, %v", got, err)
	}

	route53.err = errors.New("records failed")
	if _, err = account.Delegation(context.Background(), zone, "host.api.dev.example"); !errors.Is(err, route53.err) {
		t.Fatalf("Delegation error = %v", err)
	}
}
