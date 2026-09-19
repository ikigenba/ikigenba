package cloud

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type expectedEC2 interface {
	LaunchTemplate(context.Context, string) (string, error)
	ListSpaceInstances(context.Context, string) ([]Instance, error)
	DescribeInstance(context.Context, string) (Instance, error)
	RunInstance(context.Context, LaunchSpec) (Instance, error)
	LaunchReady(context.Context, LaunchSpec) (bool, error)
	StartInstance(context.Context, string) error
	StopInstance(context.Context, string) error
	TerminateInstance(context.Context, string) error
	InstanceChecksPassed(context.Context, string) (bool, error)
	ListSpaceAddresses(context.Context, string) ([]Address, error)
	AllocateAddress(context.Context, string, string) (Address, error)
	AssociateAddress(context.Context, string, string) error
	DisassociateAddress(context.Context, string) error
	ReleaseAddress(context.Context, string) error
}

type expectedSSM interface {
	GetParameter(context.Context, string) (string, error)
	PutSecureParameter(context.Context, string, string) error
	ListParameters(context.Context, string) ([]Parameter, error)
	DeleteParameter(context.Context, string) error
}

type expectedRoute53 interface {
	Zone(context.Context, string) (Zone, error)
	ListRecords(context.Context, string) ([]Record, error)
	FindRecord(context.Context, string, string, string) (Record, bool, error)
	ChangeRecords(context.Context, string, []RecordChange) (string, error)
	ChangeStatus(context.Context, string) (ChangeStatus, error)
}

type expectedS3 interface {
	ListObjects(context.Context, string, string) ([]Object, error)
	PutObject(context.Context, string, string, io.Reader, int64) error
	DeleteObjects(context.Context, string, []string) error
}

type expectedIAM interface {
	PermissionsBoundary(context.Context, string) (string, error)
	RoleExists(context.Context, string) (bool, error)
	CreateRole(context.Context, RoleSpec) error
	PutRolePolicy(context.Context, string, string, string) error
	DeleteRolePolicy(context.Context, string, string) error
	InstanceProfileRoles(context.Context, string) ([]string, bool, error)
	CreateInstanceProfile(context.Context, string) error
	AddRoleToInstanceProfile(context.Context, string, string) error
	RemoveRoleFromInstanceProfile(context.Context, string, string) error
	DeleteInstanceProfile(context.Context, string) error
	DeleteRole(context.Context, string) error
}

type expectedSTS interface {
	CallerAccountID(context.Context) (string, error)
}

func TestOpenerAndClientsContract(t *testing.T) {
	// R-Y7GF-A1BT
	wantOpener := reflect.TypeOf((func(context.Context, string, string) (Clients, error))(nil))
	gotOpener := reflect.TypeOf(Opener(nil))
	if gotOpener.Name() != "Opener" || gotOpener.Kind() != reflect.Func || !gotOpener.ConvertibleTo(wantOpener) {
		t.Fatalf("Opener has type %v, want underlying type %v", gotOpener, wantOpener)
	}
	assertStructFields(t, reflect.TypeOf(Clients{}), []field{
		{"EC2", reflect.TypeOf((*EC2)(nil)).Elem()},
		{"SSM", reflect.TypeOf((*SSM)(nil)).Elem()},
		{"Route53", reflect.TypeOf((*Route53)(nil)).Elem()},
		{"S3", reflect.TypeOf((*S3)(nil)).Elem()},
		{"IAM", reflect.TypeOf((*IAM)(nil)).Elem()},
		{"STS", reflect.TypeOf((*STS)(nil)).Elem()},
	})
}

func TestSessionContract(t *testing.T) {
	// R-8YCX-BKJM
	assertStructFields(t, reflect.TypeOf(Session{}), []field{
		{"AccountID", stringType()},
		{"Clients", reflect.TypeOf(Clients{})},
	})
	if got, want := reflect.TypeOf(Connect), reflect.TypeFor[func(context.Context, Opener, string, string) (Session, error)](); got != want {
		t.Fatalf("Connect has type %v, want %v", got, want)
	}
}

func TestConnect(t *testing.T) {
	// R-Q5B4-FD08
	ctx := context.Background()
	events := []string{}
	sts := &recordingSTS{events: &events, accountID: "295229566359"}
	clients := Clients{STS: sts}
	openCalls := 0
	open := func(gotCtx context.Context, profile, region string) (Clients, error) {
		openCalls++
		events = append(events, "open:"+profile+":"+region)
		if gotCtx != ctx {
			t.Error("Connect changed the context passed to open")
		}
		return clients, nil
	}

	got, err := Connect(ctx, open, " Root.Profile ", "Us-EAST-2")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if openCalls != 1 {
		t.Fatalf("open calls = %d, want 1", openCalls)
	}
	if !reflect.DeepEqual(events, []string{"open: Root.Profile :Us-EAST-2", "sts"}) {
		t.Fatalf("calls = %v, want opener once followed by STS", events)
	}
	if got.AccountID != "295229566359" || !reflect.DeepEqual(got.Clients, clients) {
		t.Fatalf("Connect = %#v, want account id and opened clients", got)
	}
}

func TestConnectPropagatesErrorsAndReturnsZeroSession(t *testing.T) {
	ctx := context.Background()
	t.Run("opener", func(t *testing.T) {
		want := errors.New("open failed")
		calls := 0
		got, err := Connect(ctx, func(context.Context, string, string) (Clients, error) {
			calls++
			return Clients{STS: &recordingSTS{accountID: "must not be used"}}, want
		}, "profile", "region")
		if err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(want).Pointer() || got != (Session{}) || calls != 1 {
			t.Fatalf("Connect = %#v, %v with %d calls; want zero session, original error, one call", got, err, calls)
		}
	})

	t.Run("caller identity", func(t *testing.T) {
		cloudErr := &Error{Service: "sts", Operation: "GetCallerIdentity", Err: errors.New("session expired")}
		wrapped := fmt.Errorf("identity: %w", cloudErr)
		got, err := Connect(ctx, func(context.Context, string, string) (Clients, error) {
			return Clients{STS: &recordingSTS{err: wrapped}}, nil
		}, "profile", "region")
		if err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(wrapped).Pointer() || got != (Session{}) {
			t.Fatalf("Connect = %#v, %v; want zero session and unchanged client error", got, err)
		}
		var matched *Error
		if !errors.As(err, &matched) || matched != cloudErr || matched.Error() != "sts GetCallerIdentity: session expired" {
			t.Fatalf("Connect error = %#v; want wrapped concrete cloud error", err)
		}
	})
}

func TestErrorContract(t *testing.T) {
	// R-Y8OB-NT2I
	assertStructFields(t, reflect.TypeOf(Error{}), []field{
		{"Service", reflect.TypeOf("")},
		{"Operation", reflect.TypeOf("")},
		{"Subject", reflect.TypeOf("")},
		{"Code", reflect.TypeOf("")},
		{"Err", reflect.TypeOf((*error)(nil)).Elem()},
	})
	cause := errors.New("cause")
	err := &Error{Err: cause}
	gotCause := err.Unwrap()
	if reflect.TypeOf(gotCause) != reflect.TypeOf(cause) || reflect.ValueOf(gotCause).Pointer() != reflect.ValueOf(cause).Pointer() {
		t.Fatalf("Unwrap() = %v, want original error", err.Unwrap())
	}
	var _ error = err
}

func TestErrorFormatting(t *testing.T) {
	// R-Q6J0-T4QX
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{"subject and code", &Error{Service: "ssm", Operation: "GetParameter", Subject: "/sbx1.ikigenba.dev/crm", Code: "ParameterNotFound"}, "ssm GetParameter /sbx1.ikigenba.dev/crm: ParameterNotFound"},
		{"no subject", &Error{Service: "ec2", Operation: "RunInstances", Code: "InsufficientInstanceCapacity"}, "ec2 RunInstances: InsufficientInstanceCapacity"},
		{"route53 code", &Error{Service: "route53", Operation: "ChangeResourceRecordSets", Code: "Throttling"}, "route53 ChangeResourceRecordSets: Throttling"},
		{"wrapped message", &Error{Service: "sts", Operation: "GetCallerIdentity", Err: errors.New("arbitrary provider message")}, "sts GetCallerIdentity: arbitrary provider message"},
		{"code takes precedence", &Error{Service: "ssm", Operation: "GetParameter", Code: "ParameterNotFound", Err: errors.New("transport failure")}, "ssm GetParameter: ParameterNotFound"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.err.Error(); got != test.want {
				t.Fatalf("Error() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestErrorMatchesThroughWrapping(t *testing.T) {
	cloudErr := &Error{Service: "ec2", Operation: "RunInstances", Code: "InsufficientInstanceCapacity"}
	err := fmt.Errorf("command failed: %w", cloudErr)
	var got *Error
	if !errors.As(err, &got) || got != cloudErr {
		t.Fatalf("errors.As(%v) = %#v, want original *Error", err, got)
	}
}

func TestNotFoundErrorContract(t *testing.T) {
	// R-Q7QX-6WHM
	assertStructFields(t, reflect.TypeOf(NotFoundError{}), []field{
		{"Kind", stringType()},
		{"Name", stringType()},
	})
	tests := []struct {
		kind string
		want string
	}{
		{"hosted zone", "no hosted zone 'ikigenba.dev'"},
		{"launch template", "no launch template 'ikigenba.dev'"},
		{"permissions boundary", "no permissions boundary 'ikigenba.dev'"},
	}
	for _, test := range tests {
		err := &NotFoundError{Kind: test.kind, Name: "ikigenba.dev"}
		if got := err.Error(); got != test.want {
			t.Errorf("Error() = %q, want %q", got, test.want)
		}
		var matched *NotFoundError
		if wrapped := fmt.Errorf("lookup: %w", err); !errors.As(wrapped, &matched) || matched != err {
			t.Errorf("errors.As did not recover concrete %s error", test.kind)
		}
	}
}

func TestEC2ValueContracts(t *testing.T) {
	// R-Q8YT-KO8B
	states := []InstanceState{StatePending, StateRunning, StateShuttingDown, StateTerminated, StateStopping, StateStopped}
	wantStates := []InstanceState{"pending", "running", "shutting-down", "terminated", "stopping", "stopped"}
	if !reflect.DeepEqual(states, wantStates) {
		t.Fatalf("instance states = %v, want %v", states, wantStates)
	}
	assertExactStringConstants(t, "InstanceState", map[string]string{
		"StatePending":      "pending",
		"StateRunning":      "running",
		"StateShuttingDown": "shutting-down",
		"StateTerminated":   "terminated",
		"StateStopping":     "stopping",
		"StateStopped":      "stopped",
	})
	assertNamedString(t, reflect.TypeOf(InstanceState("")), "InstanceState")
	assertStructFields(t, reflect.TypeOf(Instance{}), []field{{"ID", stringType()}, {"Space", stringType()}, {"State", reflect.TypeOf(InstanceState(""))}, {"Address", stringType()}})
	assertStructFields(t, reflect.TypeOf(Address{}), []field{{"AllocationID", stringType()}, {"AssociationID", stringType()}, {"IP", stringType()}, {"Space", stringType()}})
	assertStructFields(t, reflect.TypeOf(LaunchSpec{}), []field{{"LaunchTemplateID", stringType()}, {"InstanceProfile", stringType()}, {"Domain", stringType()}, {"Space", stringType()}})
}

func TestEC2InterfaceContract(t *testing.T) {
	// R-QA6P-YFZ0
	assertInterface(t, reflect.TypeOf((*EC2)(nil)).Elem(), reflect.TypeOf((*expectedEC2)(nil)).Elem())
}

func TestSSMContract(t *testing.T) {
	// R-YDJX-6W1A
	assertStructFields(t, reflect.TypeOf(Parameter{}), []field{{"Name", stringType()}, {"Value", stringType()}})
	assertInterface(t, reflect.TypeOf((*SSM)(nil)).Elem(), reflect.TypeOf((*expectedSSM)(nil)).Elem())
}

func TestRoute53ValueContracts(t *testing.T) {
	// R-YERT-KNRZ
	assertStructFields(t, reflect.TypeOf(Zone{}), []field{{"ID", stringType()}, {"Name", stringType()}})
	assertStructFields(t, reflect.TypeOf(Record{}), []field{{"Name", stringType()}, {"Type", stringType()}, {"TTL", reflect.TypeOf(int64(0))}, {"Values", reflect.TypeOf([]string{})}})
	assertNamedString(t, reflect.TypeOf(ChangeAction("")), "ChangeAction")
	if ChangeUpsert != "UPSERT" || ChangeDelete != "DELETE" {
		t.Fatalf("change actions = %q, %q", ChangeUpsert, ChangeDelete)
	}
	assertStructFields(t, reflect.TypeOf(RecordChange{}), []field{{"Action", reflect.TypeOf(ChangeAction(""))}, {"Record", reflect.TypeOf(Record{})}})
	assertNamedString(t, reflect.TypeOf(ChangeStatus("")), "ChangeStatus")
	if ChangePending != "PENDING" || ChangeInsync != "INSYNC" {
		t.Fatalf("change statuses = %q, %q", ChangePending, ChangeInsync)
	}
	assertExactStringConstants(t, "ChangeAction", map[string]string{
		"ChangeUpsert": "UPSERT",
		"ChangeDelete": "DELETE",
	})
	assertExactStringConstants(t, "ChangeStatus", map[string]string{
		"ChangePending": "PENDING",
		"ChangeInsync":  "INSYNC",
	})
}

func TestRoute53InterfaceContract(t *testing.T) {
	// R-QBEM-C7PP
	assertInterface(t, reflect.TypeOf((*Route53)(nil)).Elem(), reflect.TypeOf((*expectedRoute53)(nil)).Elem())
}

func TestIAMContract(t *testing.T) {
	// R-QCMI-PZGE
	assertStructFields(t, reflect.TypeOf(RoleSpec{}), []field{{"Name", stringType()}, {"AssumeRolePolicy", stringType()}, {"PermissionsBoundaryARN", stringType()}})
	assertInterface(t, reflect.TypeOf((*IAM)(nil)).Elem(), reflect.TypeOf((*expectedIAM)(nil)).Elem())
}

func TestSTSContract(t *testing.T) {
	// R-YKVB-HIHG
	assertInterface(t, reflect.TypeOf((*STS)(nil)).Elem(), reflect.TypeOf((*expectedSTS)(nil)).Elem())
}

func TestS3Contract(t *testing.T) {
	// R-CCED-PY62
	assertStructFields(t, reflect.TypeOf(Object{}), []field{{"Key", stringType()}, {"Size", reflect.TypeOf(int64(0))}, {"Modified", reflect.TypeOf(time.Time{})}})
	assertInterface(t, reflect.TypeOf((*S3)(nil)).Elem(), reflect.TypeOf((*expectedS3)(nil)).Elem())
}

func TestSpaceContract(t *testing.T) {
	// R-QDUF-3R73
	assertStructFields(t, reflect.TypeOf(Space{}), []field{
		{"Domain", stringType()},
		{"ID", stringType()},
		{"State", reflect.TypeOf(InstanceState(""))},
		{"Address", stringType()},
	})
	if got, want := reflect.TypeOf(Spaces), reflect.TypeFor[func(context.Context, EC2, string) ([]Space, error)](); got != want {
		t.Fatalf("Spaces has type %v, want %v", got, want)
	}
	if got, want := reflect.TypeOf(LookupSpace), reflect.TypeFor[func(context.Context, EC2, string, string) (Space, error)](); got != want {
		t.Fatalf("LookupSpace has type %v, want %v", got, want)
	}
}

func TestNoSpaceErrorContract(t *testing.T) {
	// R-QF2B-HIXS
	assertStructFields(t, reflect.TypeOf(NoSpaceError{}), []field{{"Domain", stringType()}})
	err := &NoSpaceError{Domain: "gone.ikigenba.dev"}
	if got := err.Error(); got != "no space at 'gone.ikigenba.dev'" {
		t.Fatalf("Error() = %q, want no space message", got)
	}
	var matched *NoSpaceError
	if wrapped := fmt.Errorf("lookup: %w", err); !errors.As(wrapped, &matched) || matched != err {
		t.Fatalf("errors.As did not recover original *NoSpaceError")
	}
}

func TestSpacesFiltersMapsAndSortsInstances(t *testing.T) {
	// R-2ZKD-LLTT
	ctx := context.Background()
	ec2 := &listEC2{instances: []Instance{
		{ID: "i-z", Space: "zulu.ikigenba.dev", State: StateStopped, Address: "203.0.113.2"},
		{ID: "i-dead", Space: "alpha.ikigenba.dev", State: StateTerminated, Address: "203.0.113.9"},
		{ID: "i-a", Space: "alpha.ikigenba.dev", State: StateRunning, Address: "203.0.113.1"},
	}}
	got, err := Spaces(ctx, ec2, "ikigenba.dev")
	if err != nil {
		t.Fatalf("Spaces: %v", err)
	}
	want := []Space{
		{Domain: "alpha.ikigenba.dev", ID: "i-a", State: StateRunning, Address: "203.0.113.1"},
		{Domain: "zulu.ikigenba.dev", ID: "i-z", State: StateStopped, Address: "203.0.113.2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Spaces = %#v, want %#v", got, want)
	}
	if ec2.calls != 1 || ec2.ctx != ctx || ec2.domain != "ikigenba.dev" {
		t.Fatalf("ListSpaceInstances calls = %d with (%v, %q), want once with original context and domain", ec2.calls, ec2.ctx, ec2.domain)
	}
}

func TestSpacesEmptyAndDuplicateBehavior(t *testing.T) {
	// R-2ZKD-LLTT
	t.Run("empty", func(t *testing.T) {
		got, err := Spaces(context.Background(), &listEC2{}, "ikigenba.dev")
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("Spaces = %#v, %v; want non-nil empty result and nil error", got, err)
		}
	})

	t.Run("duplicate active instances", func(t *testing.T) {
		const duplicate = "sbx1.ikigenba.dev"
		got, err := Spaces(context.Background(), &listEC2{instances: []Instance{
			{ID: "i-1", Space: duplicate, State: StateRunning},
			{ID: "i-2", Space: duplicate, State: StateStopped},
		}}, "ikigenba.dev")
		if err == nil || got != nil || !strings.Contains(err.Error(), duplicate) {
			t.Fatalf("Spaces = %#v, %v; want nil result and duplicate-naming error", got, err)
		}
	})

	t.Run("terminated instance is not a duplicate", func(t *testing.T) {
		const space = "sbx1.ikigenba.dev"
		got, err := Spaces(context.Background(), &listEC2{instances: []Instance{
			{ID: "i-dead", Space: space, State: StateTerminated},
			{ID: "i-live", Space: space, State: StatePending},
		}}, "ikigenba.dev")
		want := []Space{{Domain: space, ID: "i-live", State: StatePending}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Spaces = %#v, %v; want %#v, nil", got, err, want)
		}
	})
}

func TestLookupSpace(t *testing.T) {
	// R-QHI4-92F6
	t.Run("found", func(t *testing.T) {
		ec2 := &listEC2{instances: []Instance{
			{ID: "i-b", Space: "sbx2.ikigenba.dev", State: StateStopped},
			{ID: "i-a", Space: "sbx1.ikigenba.dev", State: StateRunning, Address: "203.0.113.1"},
		}}
		got, err := LookupSpace(context.Background(), ec2, "ikigenba.dev", "sbx1.ikigenba.dev")
		want := Space{Domain: "sbx1.ikigenba.dev", ID: "i-a", State: StateRunning, Address: "203.0.113.1"}
		if err != nil || got != want {
			t.Fatalf("LookupSpace = %#v, %v; want %#v, nil", got, err, want)
		}
	})

	t.Run("absent", func(t *testing.T) {
		got, err := LookupSpace(context.Background(), &listEC2{}, "ikigenba.dev", "gone.ikigenba.dev")
		var noSpace *NoSpaceError
		if got != (Space{}) || !errors.As(err, &noSpace) || noSpace.Domain != "gone.ikigenba.dev" || err.Error() != "no space at 'gone.ikigenba.dev'" {
			t.Fatalf("LookupSpace = %#v, %#v; want zero space and exact *NoSpaceError", got, err)
		}
	})

	t.Run("list failure", func(t *testing.T) {
		want := errors.New("list failed")
		got, err := LookupSpace(context.Background(), &listEC2{err: want}, "ikigenba.dev", "sbx1.ikigenba.dev")
		if got != (Space{}) || err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(want).Pointer() {
			t.Fatalf("LookupSpace = %#v, %v; want zero space and original error", got, err)
		}
	})
}

type recordingSTS struct {
	events    *[]string
	accountID string
	err       error
}

func (s *recordingSTS) CallerAccountID(context.Context) (string, error) {
	if s.events != nil {
		*s.events = append(*s.events, "sts")
	}
	return s.accountID, s.err
}

type listEC2 struct {
	EC2
	ctx       context.Context
	domain    string
	calls     int
	instances []Instance
	err       error
}

func (e *listEC2) ListSpaceInstances(ctx context.Context, domain string) ([]Instance, error) {
	e.ctx = ctx
	e.domain = domain
	e.calls++
	return e.instances, e.err
}

type field struct {
	name      string
	fieldType reflect.Type
}

func assertStructFields(t *testing.T, got reflect.Type, want []field) {
	t.Helper()
	if got.Kind() != reflect.Struct || got.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want exactly %d", got.Name(), got.NumField(), len(want))
	}
	for i, expected := range want {
		actual := got.Field(i)
		if actual.Name != expected.name || actual.Type != expected.fieldType || !actual.IsExported() {
			t.Errorf("%s field %d = exported %s %v, want exported %s %v", got.Name(), i, actual.Name, actual.Type, expected.name, expected.fieldType)
		}
	}
}

func assertInterface(t *testing.T, got, want reflect.Type) {
	t.Helper()
	if got.Kind() != reflect.Interface || got.NumMethod() != want.NumMethod() {
		t.Fatalf("%s has %d methods, want exactly %d", got.Name(), got.NumMethod(), want.NumMethod())
	}
	for i := range want.NumMethod() {
		gotMethod := got.Method(i)
		wantMethod := want.Method(i)
		if gotMethod.Name != wantMethod.Name || gotMethod.Type != wantMethod.Type {
			t.Errorf("%s method %d = %s %v, want %s %v", got.Name(), i, gotMethod.Name, gotMethod.Type, wantMethod.Name, wantMethod.Type)
		}
	}
}

func assertNamedString(t *testing.T, got reflect.Type, name string) {
	t.Helper()
	if got.Name() != name || got.Kind() != reflect.String {
		t.Fatalf("type = %s (kind %s), want named string %s", got.Name(), got.Kind(), name)
	}
}

func assertExactStringConstants(t *testing.T, typeName string, want map[string]string) {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate cloud_test.go")
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(filepath.Dir(testFile), "cloud.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse cloud.go: %v", err)
	}
	info, err := (&types.Config{Importer: importer.Default()}).Check("cloud", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatalf("type-check cloud.go: %v", err)
	}
	wantType := info.Scope().Lookup(typeName)
	if wantType == nil {
		t.Fatalf("type %s is not declared", typeName)
	}
	got := make(map[string]string)
	for _, name := range info.Scope().Names() {
		object, ok := info.Scope().Lookup(name).(*types.Const)
		if ok && object.Exported() && types.Identical(object.Type(), wantType.Type()) {
			got[name] = constant.StringVal(object.Val())
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("exported %s constants = %v, want exactly %v", typeName, got, want)
	}
}

func stringType() reflect.Type { return reflect.TypeOf("") }
