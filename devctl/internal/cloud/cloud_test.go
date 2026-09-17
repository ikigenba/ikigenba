package cloud

import (
	"context"
	"errors"
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
	"testing"
	"time"
)

type expectedEC2 interface {
	ListSpaceInstances(context.Context) ([]Instance, error)
	DescribeInstance(context.Context, string) (Instance, error)
	RunInstance(context.Context, LaunchSpec) (Instance, error)
	StartInstance(context.Context, string) error
	StopInstance(context.Context, string) error
	TerminateInstance(context.Context, string) error
	InstanceChecksPassed(context.Context, string) (bool, error)
	ListSpaceAddresses(context.Context) ([]Address, error)
	AllocateAddress(context.Context, string) (Address, error)
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
	ListZones(context.Context) ([]Zone, error)
	ListRecords(context.Context, string) ([]Record, error)
	ChangeRecords(context.Context, string, []RecordChange) (string, error)
	ChangeStatus(context.Context, string) (ChangeStatus, error)
}

type expectedS3 interface {
	ListObjects(context.Context, string, string) ([]Object, error)
	PutObject(context.Context, string, string, io.Reader, int64) error
	DeleteObjects(context.Context, string, []string) error
}

type expectedIAM interface {
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
	// R-Y9W8-1KT7
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{"subject and code", &Error{Service: "ssm", Operation: "GetParameter", Subject: "/ikigenba/account", Code: "ParameterNotFound"}, "ssm GetParameter /ikigenba/account: ParameterNotFound"},
		{"no subject", &Error{Service: "ec2", Operation: "RunInstances", Code: "InsufficientInstanceCapacity"}, "ec2 RunInstances: InsufficientInstanceCapacity"},
		{"wrapped message", &Error{Service: "route53", Operation: "ChangeResourceRecordSets", Err: errors.New("Throttling")}, "route53 ChangeResourceRecordSets: Throttling"},
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

func TestEC2ValueContracts(t *testing.T) {
	// R-YB44-FCJW
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
	assertStructFields(t, reflect.TypeOf(LaunchSpec{}), []field{{"LaunchTemplateID", stringType()}, {"InstanceProfile", stringType()}, {"Space", stringType()}})
}

func TestEC2InterfaceContract(t *testing.T) {
	// R-YCC0-T4AL
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
	// R-YH7M-C79D
	assertInterface(t, reflect.TypeOf((*Route53)(nil)).Elem(), reflect.TypeOf((*expectedRoute53)(nil)).Elem())
}

func TestIAMContract(t *testing.T) {
	// R-YJNF-3QQR
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
