package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
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
	var open func(context.Context, string, string) (Clients, error) = Opener(nil)
	var opener Opener = open
	if _, ok := any(opener).(func(context.Context, string, string) (Clients, error)); ok {
		t.Fatal("Opener is an alias, want a defined func type")
	}
	_ = Clients(struct {
		EC2     EC2
		SSM     SSM
		Route53 Route53
		S3      S3
		IAM     IAM
		STS     STS
	}{})
}

func TestSessionContract(_ *testing.T) {
	// R-8YCX-BKJM
	_ = Session(struct {
		AccountID string
		Clients   Clients
	}{})
	_ = []func(context.Context, Opener, string, string) (Session, error){Connect}
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
	_ = Error(struct {
		Service   string
		Operation string
		Subject   string
		Code      string
		Err       error
	}{})
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
	_ = NotFoundError(struct {
		Kind string
		Name string
	}{})
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
	// R-6N60-DGIR
	states := []InstanceState{StatePending, StateRunning, StateShuttingDown, StateTerminated, StateStopping, StateStopped}
	wantStates := []InstanceState{"pending", "running", "shutting-down", "terminated", "stopping", "stopped"}
	if !reflect.DeepEqual(states, wantStates) {
		t.Fatalf("instance states = %v, want %v", states, wantStates)
	}
	pending, running, shuttingDown := StatePending, StateRunning, StateShuttingDown
	terminated, stopping, stopped := StateTerminated, StateStopping, StateStopped
	for _, c := range []struct {
		name  string
		value InstanceState
		want  string
	}{
		{"StatePending", pending, "pending"},
		{"StateRunning", running, "running"},
		{"StateShuttingDown", shuttingDown, "shutting-down"},
		{"StateTerminated", terminated, "terminated"},
		{"StateStopping", stopping, "stopping"},
		{"StateStopped", stopped, "stopped"},
	} {
		if string(c.value) != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.value, c.want)
		}
	}
	const state InstanceState = "x"
	_ = string(state)
	if _, ok := any(state).(string); ok {
		t.Fatal("InstanceState is an alias of string, want a defined type")
	}
	_ = Instance(struct {
		ID      string
		Space   string
		State   InstanceState
		Address string
	}{})
	_ = Address(struct {
		AllocationID  string
		AssociationID string
		IP            string
		Space         string
	}{})
	_ = LaunchSpec(struct {
		LaunchTemplateID string
		InstanceProfile  string
		Domain           string
		Space            string
	}{})
}

func TestEC2InterfaceContract(_ *testing.T) {
	// R-QA6P-YFZ0
	var gotEC2 EC2
	wantEC2 := expectedEC2(gotEC2)
	gotEC2 = wantEC2
	_ = gotEC2
}

func TestSSMContract(_ *testing.T) {
	// R-YDJX-6W1A
	_ = Parameter(struct {
		Name  string
		Value string
	}{})
	var gotSSM SSM
	wantSSM := expectedSSM(gotSSM)
	gotSSM = wantSSM
	_ = gotSSM
}

func TestRoute53ValueContracts(t *testing.T) {
	// R-6ODW-R89G
	_ = Zone(struct {
		ID   string
		Name string
	}{})
	_ = Record(struct {
		Name   string
		Type   string
		TTL    int64
		Values []string
	}{})
	const action ChangeAction = "x"
	_ = string(action)
	if _, ok := any(action).(string); ok {
		t.Fatal("ChangeAction is an alias of string, want a defined type")
	}
	if ChangeUpsert != "UPSERT" || ChangeDelete != "DELETE" {
		t.Fatalf("change actions = %q, %q", ChangeUpsert, ChangeDelete)
	}
	_ = RecordChange(struct {
		Action ChangeAction
		Record Record
	}{})
	const status ChangeStatus = "x"
	_ = string(status)
	if _, ok := any(status).(string); ok {
		t.Fatal("ChangeStatus is an alias of string, want a defined type")
	}
	if ChangePending != "PENDING" || ChangeInsync != "INSYNC" {
		t.Fatalf("change statuses = %q, %q", ChangePending, ChangeInsync)
	}
	upsert, deleteAction := ChangeUpsert, ChangeDelete
	_ = []ChangeAction{upsert, deleteAction}
	pending, insync := ChangePending, ChangeInsync
	_ = []ChangeStatus{pending, insync}
}

func TestRoute53InterfaceContract(_ *testing.T) {
	// R-QBEM-C7PP
	var gotRoute53 Route53
	wantRoute53 := expectedRoute53(gotRoute53)
	gotRoute53 = wantRoute53
	_ = gotRoute53
}

func TestIAMContract(_ *testing.T) {
	// R-QCMI-PZGE
	_ = RoleSpec(struct {
		Name                   string
		AssumeRolePolicy       string
		PermissionsBoundaryARN string
	}{})
	var gotIAM IAM
	wantIAM := expectedIAM(gotIAM)
	gotIAM = wantIAM
	_ = gotIAM
}

func TestSTSContract(_ *testing.T) {
	// R-YKVB-HIHG
	var gotSTS STS
	wantSTS := expectedSTS(gotSTS)
	gotSTS = wantSTS
	_ = gotSTS
}

func TestS3Contract(_ *testing.T) {
	// R-CCED-PY62
	_ = Object(struct {
		Key      string
		Size     int64
		Modified time.Time
	}{})
	var gotS3 S3
	wantS3 := expectedS3(gotS3)
	gotS3 = wantS3
	_ = gotS3
}

func TestSpaceContract(_ *testing.T) {
	// R-QDUF-3R73
	_ = Space(struct {
		Domain  string
		ID      string
		State   InstanceState
		Address string
	}{})
	_ = []func(context.Context, EC2, string) ([]Space, error){Spaces}
	_ = []func(context.Context, EC2, string, string) (Space, error){LookupSpace}
}

func TestNoSpaceErrorContract(t *testing.T) {
	// R-QF2B-HIXS
	_ = NoSpaceError(struct{ Domain string }{})
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
