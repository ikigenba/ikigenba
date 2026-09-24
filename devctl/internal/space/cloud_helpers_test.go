package space

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestWaitState(t *testing.T) {
	// R-UAQE-8484
	responses := []cloud.Instance{
		{ID: "i-one", State: cloud.StatePending},
		{ID: "i-one", State: cloud.StateRunning},
		{ID: "i-one", State: cloud.StateRunning, Address: "192.0.2.1"},
	}
	ec2 := &helperEC2{describe: func(_ context.Context, id string) (cloud.Instance, error) {
		if id != "i-one" {
			t.Fatalf("DescribeInstance id = %q", id)
		}
		result := responses[0]
		responses = responses[1:]
		return result, nil
	}}
	var waits []time.Duration
	deps := seam.Deps{After: instantAfter(&waits)}
	want := cloud.Instance{ID: "i-one", State: cloud.StateRunning, Address: "192.0.2.1"}
	got, err := WaitState(context.Background(), deps, ec2, "i-one", cloud.StateRunning)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("WaitState = %#v, %v; want %#v, nil", got, err, want)
	}
	if !reflect.DeepEqual(waits, []time.Duration{PollInterval, PollInterval}) {
		t.Fatalf("waits = %v", waits)
	}

	for _, state := range []cloud.InstanceState{cloud.StateStopped, cloud.StateTerminated} {
		calls := 0
		ec2.describe = func(_ context.Context, id string) (cloud.Instance, error) {
			calls++
			if id != "i-one" {
				t.Fatalf("DescribeInstance id = %q", id)
			}
			return cloud.Instance{ID: id, State: state}, nil
		}
		waits = nil
		got, err = WaitState(context.Background(), deps, ec2, "i-one", state)
		if err != nil || got.State != state || got.Address != "" || calls != 1 || len(waits) != 0 {
			t.Fatalf("WaitState(%s) = %#v, %v; calls=%d waits=%v", state, got, err, calls, waits)
		}
	}

	calls := 0
	ec2.describe = func(context.Context, string) (cloud.Instance, error) {
		calls++
		return cloud.Instance{State: cloud.StatePending}, nil
	}
	waits = nil
	_, err = WaitState(context.Background(), deps, ec2, "i-timeout", cloud.StateRunning)
	var waitErr *WaitError
	if !errors.As(err, &waitErr) || waitErr.Subject != "i-timeout" || waitErr.Want != "be running" {
		t.Fatalf("timeout error = %#v", err)
	}
	if calls != PollAttempts || len(waits) != PollAttempts-1 {
		t.Fatalf("calls, waits = %d, %d; want %d, %d", calls, len(waits), PollAttempts, PollAttempts-1)
	}
}

func TestWaitStateTreatsZeroInstanceAsUnreached(t *testing.T) {
	// R-3N3S-I439
	const id = "i-one"
	running := cloud.Instance{ID: id, State: cloud.StateRunning, Address: "192.0.2.8"}
	responses := []cloud.Instance{{}, {}, running}
	calls := 0
	ec2 := &helperEC2{describe: func(_ context.Context, gotID string) (cloud.Instance, error) {
		calls++
		if gotID != id {
			t.Fatalf("DescribeInstance id = %q", gotID)
		}
		result := responses[0]
		responses = responses[1:]
		return result, nil
	}}
	var waits []time.Duration
	deps := seam.Deps{After: instantAfter(&waits)}
	got, err := WaitState(context.Background(), deps, ec2, id, cloud.StateRunning)
	if err != nil || !reflect.DeepEqual(got, running) || calls != 3 || !reflect.DeepEqual(waits, []time.Duration{PollInterval, PollInterval}) {
		t.Fatalf("WaitState = %#v, %v; calls=%d waits=%v", got, err, calls, waits)
	}

	calls = 0
	waits = nil
	ec2.describe = func(_ context.Context, gotID string) (cloud.Instance, error) {
		calls++
		if gotID != id {
			t.Fatalf("DescribeInstance id = %q", gotID)
		}
		return cloud.Instance{}, nil
	}
	_, err = WaitState(context.Background(), deps, ec2, id, cloud.StateRunning)
	var waitErr *WaitError
	if !errors.As(err, &waitErr) || waitErr.Subject != id || waitErr.Want != "be running" || calls != PollAttempts {
		t.Fatalf("timeout = %#v; calls=%d", err, calls)
	}
	if len(waits) != PollAttempts-1 {
		t.Fatalf("waits = %d, want %d", len(waits), PollAttempts-1)
	}
	for _, wait := range waits {
		if wait != PollInterval {
			t.Fatalf("wait = %s, want %s", wait, PollInterval)
		}
	}
}

func TestWaitAssociate(t *testing.T) {
	// R-RUIM-IAS8
	const allocationID = "eipalloc-1"
	const instanceID = "i-one"
	notFound := &cloud.Error{Code: "InvalidAllocationID.NotFound"}
	calls := 0
	ec2 := &associateEC2{associate: func(_ context.Context, gotAllocation, gotInstance string) error {
		calls++
		if gotAllocation != allocationID || gotInstance != instanceID {
			t.Fatalf("AssociateAddress(%q, %q)", gotAllocation, gotInstance)
		}
		if calls < 3 {
			return notFound
		}
		return nil
	}}
	var waits []time.Duration
	deps := seam.Deps{After: instantAfter(&waits)}
	if err := WaitAssociate(context.Background(), deps, ec2, allocationID, instanceID); err != nil || calls != 3 || !reflect.DeepEqual(waits, []time.Duration{PollInterval, PollInterval}) {
		t.Fatalf("WaitAssociate = %v; calls=%d waits=%v", err, calls, waits)
	}

	calls = 0
	waits = nil
	ec2.associate = func(_ context.Context, gotAllocation, gotInstance string) error {
		calls++
		if gotAllocation != allocationID || gotInstance != instanceID {
			t.Fatalf("AssociateAddress(%q, %q)", gotAllocation, gotInstance)
		}
		return notFound
	}
	err := WaitAssociate(context.Background(), deps, ec2, allocationID, instanceID)
	var waitErr *WaitError
	if !errors.As(err, &waitErr) || waitErr.Subject != allocationID || waitErr.Want != "become associable" || calls != PollAttempts {
		t.Fatalf("timeout = %#v; calls=%d", err, calls)
	}
	if len(waits) != PollAttempts-1 {
		t.Fatalf("waits = %d, want %d", len(waits), PollAttempts-1)
	}
	for _, wait := range waits {
		if wait != PollInterval {
			t.Fatalf("wait = %s, want %s", wait, PollInterval)
		}
	}

	other := &cloud.Error{Code: "Gateway.NotAttached"}
	calls = 0
	waits = nil
	ec2.associate = func(_ context.Context, gotAllocation, gotInstance string) error {
		calls++
		if gotAllocation != allocationID || gotInstance != instanceID {
			t.Fatalf("AssociateAddress(%q, %q)", gotAllocation, gotInstance)
		}
		return other
	}
	err = WaitAssociate(context.Background(), deps, ec2, allocationID, instanceID)
	if !errors.Is(err, other) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(other).Pointer() || calls != 1 || len(waits) != 0 {
		t.Fatalf("other error = %v; calls=%d waits=%v", err, calls, waits)
	}
}

func TestWaitChecks(t *testing.T) {
	// R-UBYA-LVYT
	calls := 0
	ec2 := &helperEC2{checks: func(_ context.Context, id string) (bool, error) {
		calls++
		if id != "i-one" {
			t.Fatalf("InstanceChecksPassed id = %q", id)
		}
		return calls == 3, nil
	}}
	var waits []time.Duration
	err := WaitChecks(context.Background(), seam.Deps{After: instantAfter(&waits)}, ec2, "i-one")
	if err != nil || calls != 3 || !reflect.DeepEqual(waits, []time.Duration{PollInterval, PollInterval}) {
		t.Fatalf("WaitChecks = %v; calls=%d waits=%v", err, calls, waits)
	}

	calls = 0
	ec2.checks = func(context.Context, string) (bool, error) { calls++; return false, nil }
	waits = nil
	err = WaitChecks(context.Background(), seam.Deps{After: instantAfter(&waits)}, ec2, "i-timeout")
	var waitErr *WaitError
	if !errors.As(err, &waitErr) || waitErr.Subject != "i-timeout" || waitErr.Want != "pass its status checks" {
		t.Fatalf("timeout error = %#v", err)
	}
	if calls != PollAttempts || len(waits) != PollAttempts-1 {
		t.Fatalf("calls, waits = %d, %d; want %d, %d", calls, len(waits), PollAttempts, PollAttempts-1)
	}
}

func TestWaitLaunchReady(t *testing.T) {
	// R-UD66-ZNPI
	spec := cloud.LaunchSpec{
		LaunchTemplateID: "lt-one",
		InstanceProfile:  "profile-one",
		Space:            "app.example",
	}
	calls := 0
	ec2 := &helperEC2{launchReady: func(_ context.Context, got cloud.LaunchSpec) (bool, error) {
		calls++
		if !reflect.DeepEqual(got, spec) {
			t.Fatalf("LaunchReady spec = %#v, want %#v", got, spec)
		}
		return calls == 3, nil
	}}
	var waits []time.Duration
	err := WaitLaunchReady(context.Background(), seam.Deps{After: instantAfter(&waits)}, ec2, spec)
	if err != nil || calls != 3 || !reflect.DeepEqual(waits, []time.Duration{PollInterval, PollInterval}) {
		t.Fatalf("WaitLaunchReady = %v; calls=%d waits=%v", err, calls, waits)
	}

	wantErr := errors.New("launch probe failed")
	calls = 0
	waits = nil
	ec2.launchReady = func(context.Context, cloud.LaunchSpec) (bool, error) {
		calls++
		return false, wantErr
	}
	err = WaitLaunchReady(context.Background(), seam.Deps{After: instantAfter(&waits)}, ec2, spec)
	if !errors.Is(err, wantErr) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(wantErr).Pointer() || calls != 1 || len(waits) != 0 {
		t.Fatalf("error WaitLaunchReady = %v; calls=%d waits=%v", err, calls, waits)
	}

	calls = 0
	waits = nil
	ec2.launchReady = func(context.Context, cloud.LaunchSpec) (bool, error) {
		calls++
		return false, nil
	}
	err = WaitLaunchReady(context.Background(), seam.Deps{After: instantAfter(&waits)}, ec2, spec)
	var waitErr *WaitError
	if !errors.As(err, &waitErr) || waitErr.Subject != spec.InstanceProfile || waitErr.Want != "become usable for launch" {
		t.Fatalf("timeout error = %#v", err)
	}
	if calls != PollAttempts || len(waits) != PollAttempts-1 {
		t.Fatalf("calls, waits = %d, %d; want %d, %d", calls, len(waits), PollAttempts, PollAttempts-1)
	}
}

func TestWaitInsync(t *testing.T) {
	// R-UFLZ-R76W
	statuses := []cloud.ChangeStatus{cloud.ChangePending, cloud.ChangeInsync}
	var ids []string
	route53 := &helperRoute53{changeStatus: func(_ context.Context, id string) (cloud.ChangeStatus, error) {
		ids = append(ids, id)
		status := statuses[0]
		statuses = statuses[1:]
		return status, nil
	}}
	var waits []time.Duration
	if err := WaitInsync(context.Background(), seam.Deps{After: instantAfter(&waits)}, route53, "change-one"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"change-one", "change-one"}) || !reflect.DeepEqual(waits, []time.Duration{PollInterval}) {
		t.Fatalf("ids=%v waits=%v", ids, waits)
	}

	wantErr := errors.New("status failed")
	route53.changeStatus = func(context.Context, string) (cloud.ChangeStatus, error) { return "", wantErr }
	if err := WaitInsync(context.Background(), seam.Deps{}, route53, "change-error"); err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(wantErr).Pointer() {
		t.Fatalf("error = %v, want unchanged %v", err, wantErr)
	}

	calls := 0
	waits = nil
	route53.changeStatus = func(context.Context, string) (cloud.ChangeStatus, error) {
		calls++
		return cloud.ChangePending, nil
	}
	err := WaitInsync(context.Background(), seam.Deps{After: instantAfter(&waits)}, route53, "change-timeout")
	var waitErr *WaitError
	if !errors.As(err, &waitErr) || waitErr.Subject != "change-timeout" || waitErr.Want != "reach INSYNC" {
		t.Fatalf("timeout error = %#v", err)
	}
	if calls != PollAttempts || len(waits) != PollAttempts-1 {
		t.Fatalf("calls=%d waits=%d", calls, len(waits))
	}
}

func TestElasticIP(t *testing.T) {
	// R-U4MW-B9IN R-UGTW-4YXL
	addresses := []cloud.Address{
		{AllocationID: "other", Space: "other.example"},
		{AllocationID: "wanted", IP: "192.0.2.2", Space: "app.example"},
	}
	var roots []string
	ec2 := &helperEC2{addresses: func(_ context.Context, root string) ([]cloud.Address, error) {
		roots = append(roots, root)
		return addresses, nil
	}}
	got, found, err := ElasticIP(context.Background(), ec2, "example", "app.example")
	if err != nil || !found || !reflect.DeepEqual(got, addresses[1]) {
		t.Fatalf("ElasticIP = %#v, %v, %v", got, found, err)
	}
	_, found, err = ElasticIP(context.Background(), ec2, "example", "missing.example")
	if err != nil || found {
		t.Fatalf("missing ElasticIP found=%v err=%v", found, err)
	}
	addresses = append(addresses, cloud.Address{AllocationID: "duplicate", Space: "app.example"})
	_, _, err = ElasticIP(context.Background(), ec2, "example", "app.example")
	if err == nil || !strings.Contains(err.Error(), "app.example") {
		t.Fatalf("duplicate error = %v", err)
	}
	if !reflect.DeepEqual(roots, []string{"example", "example", "example"}) {
		t.Fatalf("ListSpaceAddresses roots = %v", roots)
	}
}

func TestReleaseElasticIP(t *testing.T) {
	// R-UI1S-IQOA
	var calls []string
	ec2 := &helperEC2{
		disassociate: func(_ context.Context, id string) error { calls = append(calls, "disassociate "+id); return nil },
		release:      func(_ context.Context, id string) error { calls = append(calls, "release "+id); return nil },
	}
	err := ReleaseElasticIP(context.Background(), ec2, cloud.Address{AssociationID: "assoc", AllocationID: "alloc"})
	if err != nil || !reflect.DeepEqual(calls, []string{"disassociate assoc", "release alloc"}) {
		t.Fatalf("associated release: calls=%v err=%v", calls, err)
	}
	calls = nil
	err = ReleaseElasticIP(context.Background(), ec2, cloud.Address{AllocationID: "alloc2"})
	if err != nil || !reflect.DeepEqual(calls, []string{"release alloc2"}) {
		t.Fatalf("unassociated release: calls=%v err=%v", calls, err)
	}
}

func TestPutRecords(t *testing.T) {
	// R-U5US-P19C R-UJ9O-WIEZ
	var gotZone string
	var gotChanges []cloud.RecordChange
	changeRecordsCalls := 0
	statuses := []cloud.ChangeStatus{cloud.ChangePending, cloud.ChangeInsync}
	route53 := &helperRoute53{
		changeRecords: func(_ context.Context, zone string, changes []cloud.RecordChange) (string, error) {
			changeRecordsCalls++
			gotZone, gotChanges = zone, changes
			return "change-one", nil
		},
		changeStatus: func(_ context.Context, id string) (cloud.ChangeStatus, error) {
			if id != "change-one" {
				t.Fatalf("ChangeStatus id = %q", id)
			}
			status := statuses[0]
			statuses = statuses[1:]
			return status, nil
		},
	}
	var waits []time.Duration
	err := PutRecords(context.Background(), seam.Deps{After: instantAfter(&waits)}, route53, cloud.Zone{ID: "zone-one"}, "app.example", "192.0.2.3")
	wantChanges := []cloud.RecordChange{
		{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "app.example", Type: "A", TTL: RecordTTL, Values: []string{"192.0.2.3"}}},
		{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "*.app.example", Type: "A", TTL: RecordTTL, Values: []string{"192.0.2.3"}}},
	}
	if err != nil || changeRecordsCalls != 1 || gotZone != "zone-one" || !reflect.DeepEqual(gotChanges, wantChanges) || !reflect.DeepEqual(waits, []time.Duration{PollInterval}) {
		t.Fatalf("PutRecords: ChangeRecords calls=%d zone=%q changes=%#v waits=%v err=%v", changeRecordsCalls, gotZone, gotChanges, waits, err)
	}

	calls := 0
	route53.changeStatus = func(context.Context, string) (cloud.ChangeStatus, error) { calls++; return cloud.ChangePending, nil }
	waits = nil
	err = PutRecords(context.Background(), seam.Deps{After: instantAfter(&waits)}, route53, cloud.Zone{ID: "zone-one"}, "app.example", "192.0.2.3")
	var waitErr *WaitError
	if !errors.As(err, &waitErr) || waitErr.Subject != "change-one" || waitErr.Want != "reach INSYNC" {
		t.Fatalf("timeout error = %#v", err)
	}
	if changeRecordsCalls != 2 || calls != PollAttempts || len(waits) != PollAttempts-1 {
		t.Fatalf("ChangeRecords calls, status calls, waits = %d, %d, %d; want 2, %d, %d", changeRecordsCalls, calls, len(waits), PollAttempts, PollAttempts-1)
	}
}

func TestDeleteRecords(t *testing.T) {
	// R-UKHL-AA5O
	records := []cloud.Record{
		{Name: "app.example", Type: "A", TTL: 123, Values: []string{"192.0.2.4"}},
		{Name: `\052.app.example`, Type: "A", TTL: 456, Values: []string{"192.0.2.5"}},
		{Name: "*.app.example", Type: "A", TTL: 789, Values: []string{"192.0.2.6"}},
		{Name: "app.example", Type: "TXT", TTL: 7, Values: []string{"ignore"}},
		{Name: "elsewhere.example", Type: "A", TTL: 8, Values: []string{"ignore"}},
	}
	var calls int
	var got []cloud.RecordChange
	route53 := &helperRoute53{
		listRecords: func(_ context.Context, zone string) ([]cloud.Record, error) {
			if zone != "zone-one" {
				t.Fatalf("ListRecords zone = %q", zone)
			}
			return records, nil
		},
		changeRecords: func(_ context.Context, zone string, changes []cloud.RecordChange) (string, error) {
			calls++
			if zone != "zone-one" {
				t.Fatalf("ChangeRecords zone = %q", zone)
			}
			got = changes
			return "change-one", nil
		},
	}
	deleted, err := DeleteRecords(context.Background(), route53, cloud.Zone{ID: "zone-one"}, "app.example")
	want := []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: records[0]}, {Action: cloud.ChangeDelete, Record: records[1]}, {Action: cloud.ChangeDelete, Record: records[2]}}
	if wantNames := []string{"app.example", "*.app.example"}; err != nil || !reflect.DeepEqual(deleted, wantNames) || calls != 1 || !reflect.DeepEqual(got, want) {
		t.Fatalf("DeleteRecords = %v, %v; calls=%d changes=%#v", deleted, err, calls, got)
	}
	records = records[3:]
	deleted, err = DeleteRecords(context.Background(), route53, cloud.Zone{ID: "zone-one"}, "app.example")
	if err != nil || len(deleted) != 0 || deleted == nil || calls != 1 {
		t.Fatalf("empty DeleteRecords = %v, %v; ChangeRecords calls=%d", deleted, err, calls)
	}
}

func TestDeleteRole(t *testing.T) {
	// R-U72P-2T01 R-ULPH-O1WD
	var calls []string
	iam := &helperIAM{
		roleExists: func(_ context.Context, name string) (bool, error) {
			calls = append(calls, "exists "+name)
			return true, nil
		},
		profileRoles: func(_ context.Context, name string) ([]string, bool, error) {
			calls = append(calls, "profile "+name)
			return []string{"role-a", "role-b"}, true, nil
		},
		deletePolicy: func(_ context.Context, role, policy string) error {
			calls = append(calls, "policy "+role+" "+policy)
			return nil
		},
		removeRole: func(_ context.Context, profile, role string) error {
			calls = append(calls, "remove "+profile+" "+role)
			return nil
		},
		deleteProfile: func(_ context.Context, name string) error { calls = append(calls, "delete-profile "+name); return nil },
		deleteRole:    func(_ context.Context, name string) error { calls = append(calls, "delete-role "+name); return nil },
	}
	deleted, err := DeleteRole(context.Background(), iam, "app.example")
	name := RoleName("app.example")
	want := []string{"exists " + name, "profile " + name, "policy " + name + " " + PolicyName, "remove " + name + " role-a", "remove " + name + " role-b", "delete-profile " + name, "delete-role " + name}
	if err != nil || !deleted || !reflect.DeepEqual(calls, want) {
		t.Fatalf("DeleteRole = %v, %v; calls=%v", deleted, err, calls)
	}

	calls = nil
	iam.roleExists = func(_ context.Context, name string) (bool, error) {
		calls = append(calls, "exists "+name)
		return true, nil
	}
	iam.profileRoles = func(_ context.Context, name string) ([]string, bool, error) {
		calls = append(calls, "profile "+name)
		return nil, false, nil
	}
	deleted, err = DeleteRole(context.Background(), iam, "app.example")
	want = []string{"exists " + name, "profile " + name, "policy " + name + " " + PolicyName, "delete-profile " + name, "delete-role " + name}
	if err != nil || !deleted || !reflect.DeepEqual(calls, want) {
		t.Fatalf("role-only DeleteRole = %v, %v; calls=%v", deleted, err, calls)
	}

	calls = nil
	iam.roleExists = func(_ context.Context, name string) (bool, error) {
		calls = append(calls, "exists "+name)
		return false, nil
	}
	iam.profileRoles = func(_ context.Context, name string) ([]string, bool, error) {
		calls = append(calls, "profile "+name)
		return []string{"profile-role"}, true, nil
	}
	deleted, err = DeleteRole(context.Background(), iam, "app.example")
	want = []string{"exists " + name, "profile " + name, "policy " + name + " " + PolicyName, "remove " + name + " profile-role", "delete-profile " + name, "delete-role " + name}
	if err != nil || !deleted || !reflect.DeepEqual(calls, want) {
		t.Fatalf("profile-only DeleteRole = %v, %v; calls=%v", deleted, err, calls)
	}

	calls = nil
	iam.roleExists = func(_ context.Context, name string) (bool, error) {
		calls = append(calls, "exists "+name)
		return false, nil
	}
	iam.profileRoles = func(_ context.Context, name string) ([]string, bool, error) {
		calls = append(calls, "profile "+name)
		return nil, false, nil
	}
	deleted, err = DeleteRole(context.Background(), iam, "app.example")
	if err != nil || deleted || !reflect.DeepEqual(calls, []string{"exists " + name, "profile " + name}) {
		t.Fatalf("absent DeleteRole = %v, %v; calls=%v", deleted, err, calls)
	}
}

func TestFindApex(t *testing.T) {
	// R-U8AL-GKQQ R-UMXE-1TN2
	assertFields(t, ApexRecord{}, struct {
		Record cloud.Record
		Found  bool
		Holder string
	}{})
	accept := func(func(context.Context, cloud.Route53, cloud.EC2, string, string) (ApexRecord, error)) {}
	accept(FindApex)

	record := cloud.Record{Name: "ikigenba.dev", Type: "A", TTL: 60, Values: []string{"18.118.7.42"}}
	var findArgs []string
	found := true
	route53 := &helperRoute53{findRecord: func(_ context.Context, zoneID, name, recordType string) (cloud.Record, bool, error) {
		findArgs = []string{zoneID, name, recordType}
		return record, found, nil
	}}
	addressCalls := 0
	addresses := []cloud.Address{{IP: "18.118.7.42", Space: "sbx1.ikigenba.dev"}}
	ec2 := &helperEC2{addresses: func(_ context.Context, root string) ([]cloud.Address, error) {
		addressCalls++
		if root != "ikigenba.dev" {
			t.Fatalf("root = %q", root)
		}
		return addresses, nil
	}}
	got, err := FindApex(context.Background(), route53, ec2, "ZONE1", "ikigenba.dev")
	if err != nil || !reflect.DeepEqual(got, ApexRecord{Record: record, Found: true, Holder: "sbx1.ikigenba.dev"}) {
		t.Fatalf("FindApex = %#v, %v", got, err)
	}
	if !reflect.DeepEqual(findArgs, []string{"ZONE1", "ikigenba.dev", "A"}) || addressCalls != 1 {
		t.Fatalf("find args=%v address calls=%d", findArgs, addressCalls)
	}

	record.Values = []string{"203.0.113.9"}
	got, err = FindApex(context.Background(), route53, ec2, "ZONE1", "ikigenba.dev")
	if err != nil || !got.Found || got.Holder != "" || !reflect.DeepEqual(got.Record, record) {
		t.Fatalf("unheld FindApex = %#v, %v", got, err)
	}

	found = false
	addressCalls = 0
	got, err = FindApex(context.Background(), route53, ec2, "ZONE1", "ikigenba.dev")
	if err != nil || !reflect.DeepEqual(got, ApexRecord{}) || addressCalls != 0 {
		t.Fatalf("absent FindApex = %#v, %v; address calls=%d", got, err, addressCalls)
	}

	found = true
	record.Values = []string{"18.118.7.42"}
	addresses = append(addresses, cloud.Address{IP: "18.118.7.42", Space: "sbx2.ikigenba.dev"})
	_, err = FindApex(context.Background(), route53, ec2, "ZONE1", "ikigenba.dev")
	if err == nil || !strings.Contains(err.Error(), "ikigenba.dev") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func instantAfter(waits *[]time.Duration) func(time.Duration) <-chan time.Time {
	return func(duration time.Duration) <-chan time.Time {
		*waits = append(*waits, duration)
		ready := make(chan time.Time, 1)
		ready <- time.Time{}
		return ready
	}
}

type associateEC2 struct {
	helperEC2
	associate func(context.Context, string, string) error
}

func (f *associateEC2) AssociateAddress(ctx context.Context, allocationID, instanceID string) error {
	return f.associate(ctx, allocationID, instanceID)
}

type helperEC2 struct {
	describe     func(context.Context, string) (cloud.Instance, error)
	checks       func(context.Context, string) (bool, error)
	launchReady  func(context.Context, cloud.LaunchSpec) (bool, error)
	addresses    func(context.Context, string) ([]cloud.Address, error)
	disassociate func(context.Context, string) error
	release      func(context.Context, string) error
}

func (*helperEC2) LaunchTemplate(context.Context, string) (string, error) { return "", nil }
func (*helperEC2) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return nil, nil
}
func (f *helperEC2) DescribeInstance(ctx context.Context, id string) (cloud.Instance, error) {
	return f.describe(ctx, id)
}
func (*helperEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (f *helperEC2) LaunchReady(ctx context.Context, spec cloud.LaunchSpec) (bool, error) {
	return f.launchReady(ctx, spec)
}
func (*helperEC2) StartInstance(context.Context, string) error     { return nil }
func (*helperEC2) StopInstance(context.Context, string) error      { return nil }
func (*helperEC2) TerminateInstance(context.Context, string) error { return nil }
func (f *helperEC2) InstanceChecksPassed(ctx context.Context, id string) (bool, error) {
	return f.checks(ctx, id)
}
func (f *helperEC2) ListSpaceAddresses(ctx context.Context, root string) ([]cloud.Address, error) {
	return f.addresses(ctx, root)
}
func (*helperEC2) AllocateAddress(context.Context, string, string) (cloud.Address, error) {
	return cloud.Address{}, nil
}
func (*helperEC2) AssociateAddress(context.Context, string, string) error { return nil }
func (f *helperEC2) DisassociateAddress(ctx context.Context, id string) error {
	return f.disassociate(ctx, id)
}
func (f *helperEC2) ReleaseAddress(ctx context.Context, id string) error { return f.release(ctx, id) }

type helperRoute53 struct {
	listRecords   func(context.Context, string) ([]cloud.Record, error)
	findRecord    func(context.Context, string, string, string) (cloud.Record, bool, error)
	changeRecords func(context.Context, string, []cloud.RecordChange) (string, error)
	changeStatus  func(context.Context, string) (cloud.ChangeStatus, error)
}

func (*helperRoute53) Zone(context.Context, string) (cloud.Zone, error) { return cloud.Zone{}, nil }
func (f *helperRoute53) FindRecord(ctx context.Context, zoneID, name, recordType string) (cloud.Record, bool, error) {
	if f.findRecord == nil {
		return cloud.Record{}, false, nil
	}
	return f.findRecord(ctx, zoneID, name, recordType)
}
func (f *helperRoute53) ListRecords(ctx context.Context, zone string) ([]cloud.Record, error) {
	return f.listRecords(ctx, zone)
}
func (f *helperRoute53) ChangeRecords(ctx context.Context, zone string, changes []cloud.RecordChange) (string, error) {
	return f.changeRecords(ctx, zone, changes)
}
func (f *helperRoute53) ChangeStatus(ctx context.Context, id string) (cloud.ChangeStatus, error) {
	return f.changeStatus(ctx, id)
}

type helperIAM struct {
	roleExists    func(context.Context, string) (bool, error)
	profileRoles  func(context.Context, string) ([]string, bool, error)
	deletePolicy  func(context.Context, string, string) error
	removeRole    func(context.Context, string, string) error
	deleteProfile func(context.Context, string) error
	deleteRole    func(context.Context, string) error
}

func (f *helperIAM) RoleExists(ctx context.Context, name string) (bool, error) {
	return f.roleExists(ctx, name)
}
func (*helperIAM) PermissionsBoundary(context.Context, string) (string, error) { return "", nil }
func (*helperIAM) CreateRole(context.Context, cloud.RoleSpec) error            { return nil }
func (*helperIAM) PutRolePolicy(context.Context, string, string, string) error { return nil }
func (f *helperIAM) DeleteRolePolicy(ctx context.Context, role, policy string) error {
	return f.deletePolicy(ctx, role, policy)
}
func (f *helperIAM) InstanceProfileRoles(ctx context.Context, name string) ([]string, bool, error) {
	return f.profileRoles(ctx, name)
}
func (*helperIAM) CreateInstanceProfile(context.Context, string) error            { return nil }
func (*helperIAM) AddRoleToInstanceProfile(context.Context, string, string) error { return nil }
func (f *helperIAM) RemoveRoleFromInstanceProfile(ctx context.Context, profile, role string) error {
	return f.removeRole(ctx, profile, role)
}
func (f *helperIAM) DeleteInstanceProfile(ctx context.Context, name string) error {
	return f.deleteProfile(ctx, name)
}
func (f *helperIAM) DeleteRole(ctx context.Context, name string) error {
	return f.deleteRole(ctx, name)
}
