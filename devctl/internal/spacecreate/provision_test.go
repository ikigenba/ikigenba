package spacecreate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const (
	provisionDomain  = "foo.sbx.ikigenba.dev"
	provisionAddress = "3.19.79.227"
	provisionID      = "i-0c9e94542d98846a8"
)

func TestProvisionCreatesResourcesInOrderAndUsesElasticIP(t *testing.T) {
	// R-H6QO-UMYZ R-YHSS-T9FW R-E9WN-IVFN R-EB4J-WN6C R-ECCG-AEX1 R-EIFY-79MI R-YTZS-MYUU
	h := newProvisionHarness()
	var stdout bytes.Buffer
	err := beginProvisioning(context.Background(), &stdout, h.deps(), invocation{
		domain: provisionDomain, acmeEmail: "ops@ikigenba.dev",
	}, h.preflight())
	if err != nil {
		t.Fatalf("beginProvisioning(): %v", err)
	}

	roleName := space.RoleName(provisionDomain)
	if h.roleSpec != (cloud.RoleSpec{
		Name: roleName, AssumeRolePolicy: AssumeRolePolicy, PermissionsBoundaryARN: "arn:boundary",
	}) {
		t.Fatalf("role spec = %#v", h.roleSpec)
	}
	if h.policyRole != roleName || h.policyName != space.PolicyName ||
		!strings.Contains(h.policyDocument, provisionDomain) || !strings.Contains(h.policyDocument, "ZONE1") ||
		!strings.Contains(h.policyDocument, "123456789012") || !strings.Contains(h.policyDocument, "account-backups") {
		t.Fatalf("policy call = role %q name %q document %q", h.policyRole, h.policyName, h.policyDocument)
	}
	if h.profileName != roleName || h.addProfile != roleName || h.addRole != roleName {
		t.Fatalf("profile calls = create %q add (%q, %q)", h.profileName, h.addProfile, h.addRole)
	}
	wantLaunchSpec := cloud.LaunchSpec{
		LaunchTemplateID: "lt-123", InstanceProfile: roleName, Space: provisionDomain,
	}
	if h.launchReadySpec != wantLaunchSpec || h.launchSpec != wantLaunchSpec {
		t.Fatalf("launch-ready spec = %#v, run spec = %#v, want %#v", h.launchReadySpec, h.launchSpec, wantLaunchSpec)
	}
	if !reflect.DeepEqual(h.describeInstanceIDs, []string{provisionID}) {
		t.Fatalf("describe instance IDs = %v, want [%s]", h.describeInstanceIDs, provisionID)
	}
	if h.allocateSpace != provisionDomain || h.associateAllocation != "eipalloc-one" || h.associateInstance != provisionID {
		t.Fatalf("address calls = allocate %q associate (%q, %q)", h.allocateSpace, h.associateAllocation, h.associateInstance)
	}
	if h.recordCalls != 1 || h.recordZone != "ZONE1" {
		t.Fatalf("record calls = %d, zone %q", h.recordCalls, h.recordZone)
	}
	wantChanges := []cloud.RecordChange{
		{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: provisionDomain, Type: "A", TTL: space.RecordTTL, Values: []string{provisionAddress}}},
		{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "*." + provisionDomain, Type: "A", TTL: space.RecordTTL, Values: []string{provisionAddress}}},
	}
	if !reflect.DeepEqual(h.recordChanges, wantChanges) {
		t.Fatalf("record changes = %#v, want %#v", h.recordChanges, wantChanges)
	}

	wantPrefix := []string{
		"create-role", "put-role-policy", "create-instance-profile", "add-role-to-profile",
		"launch-ready", "run-instance", "describe-instance", "allocate-address", "associate-address", "change-records", "change-status",
		"checks", "ssh",
	}
	if len(h.operations) < len(wantPrefix) || !reflect.DeepEqual(h.operations[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("operation prefix = %v, want %v", h.operations, wantPrefix)
	}
	for _, operation := range []string{"create-role", "put-role-policy", "create-instance-profile", "add-role-to-profile", "launch-ready", "run-instance", "allocate-address", "associate-address", "change-records"} {
		if countProvisionOperations(h.operations, operation) != 1 {
			t.Errorf("%s calls = %d, want 1", operation, countProvisionOperations(h.operations, operation))
		}
	}
	if h.sshTargets == 0 || h.wrongSSHTarget {
		t.Fatalf("ssh targets = %d, wrong target = %t", h.sshTargets, h.wrongSSHTarget)
	}

	wantOutput := "account: ok (sbx.ikigenba.dev, us-east-2)\n" +
		"domain: ok (zone sbx.ikigenba.dev ZONE1)\n" +
		"secrets: ok (0 apps)\n" +
		"role: ok (ikigenba-space-foo.sbx.ikigenba.dev)\n" +
		"instance: ok (i-0c9e94542d98846a8 running, 198.51.100.9)\n" +
		"address: ok (elastic ip 3.19.79.227 associated)\n" +
		"records: ok (created foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 3.19.79.227, INSYNC)\n" +
		"host: ok (status checks passed, cloud-init done)\n" +
		"opsctl: ok (v9.8.7 installed, 10 keys set)\n" +
		"init: ok\n" +
		"foo.sbx.ikigenba.dev 3.19.79.227\n"
	if stdout.String() != wantOutput {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantOutput)
	}
}

func TestProvisionWaitsForLaunchReadinessBeforeReportingRole(t *testing.T) {
	// R-H6QO-UMYZ
	h := newProvisionHarness()
	h.failAt = "launch-ready"
	var stdout bytes.Buffer
	err := provision(context.Background(), &stdout, h.deps(), invocation{
		domain: provisionDomain, acmeEmail: "ops@ikigenba.dev",
	}, h.preflight())
	if reflect.ValueOf(err) != reflect.ValueOf(errProvisionTest) {
		t.Fatalf("provision() error = %v, want unchanged sentinel", err)
	}
	wantOperations := []string{
		"create-role", "put-role-policy", "create-instance-profile", "add-role-to-profile", "launch-ready",
	}
	if !reflect.DeepEqual(h.operations, wantOperations) {
		t.Fatalf("operations = %v, want %v", h.operations, wantOperations)
	}
	if strings.Contains(stdout.String(), "role: ok") {
		t.Fatalf("stdout = %q, must not report role before launch readiness", stdout.String())
	}
}

func TestProvisionFailuresStopWithoutRollback(t *testing.T) {
	// R-E9WN-IVFN R-EIFY-79MI
	stages := []string{
		"create-role", "put-role-policy", "create-instance-profile", "add-role-to-profile",
		"launch-ready", "run-instance", "describe-instance", "allocate-address", "associate-address",
		"change-records", "change-status", "checks", "ssh",
	}
	for failIndex, failAt := range stages {
		t.Run(failAt, func(t *testing.T) {
			h := newProvisionHarness()
			h.failAt = failAt
			var stdout bytes.Buffer
			err := provision(context.Background(), &stdout, h.deps(), invocation{
				domain: provisionDomain, acmeEmail: "ops@ikigenba.dev",
			}, h.preflight())
			if !errors.Is(err, errProvisionTest) {
				t.Fatalf("provision() error = %v, want sentinel", err)
			}
			wantOperations := stages[:failIndex+1]
			if !reflect.DeepEqual(h.operations, wantOperations) {
				t.Fatalf("operations = %v, want %v", h.operations, wantOperations)
			}
			if len(h.cleanupCalls) != 0 {
				t.Fatalf("cleanup calls = %v, want none", h.cleanupCalls)
			}
			if strings.Contains(stdout.String(), provisionDomain+" "+provisionAddress) {
				t.Fatalf("final success reported: %q", stdout.String())
			}
		})
	}
}

var errProvisionTest = errors.New("provision test failure")

type provisionHarness struct {
	operations          []string
	failAt              string
	roleSpec            cloud.RoleSpec
	policyRole          string
	policyName          string
	policyDocument      string
	profileName         string
	addProfile          string
	addRole             string
	launchReadySpec     cloud.LaunchSpec
	launchSpec          cloud.LaunchSpec
	describeInstanceIDs []string
	allocateSpace       string
	associateAllocation string
	associateInstance   string
	recordCalls         int
	recordZone          string
	recordChanges       []cloud.RecordChange
	sshTargets          int
	wrongSSHTarget      bool
	cleanupCalls        []string
}

func newProvisionHarness() *provisionHarness { return &provisionHarness{} }

func (h *provisionHarness) preflight() preflightResult {
	return preflightResult{
		accountID: "123456789012",
		zone:      cloud.Zone{ID: "ZONE1", Name: "sbx.ikigenba.dev"},
		account: &account.Account{
			Properties: account.Properties{
				Domain: "sbx.ikigenba.dev", Region: "us-east-2", BackupBucket: "account-backups",
				LaunchTemplateID: "lt-123", PermissionsBoundaryARN: "arn:boundary", DeleteBackupsOnDestroy: true,
			},
			Clients: cloud.Clients{
				EC2: provisionEC2{h}, SSM: provisionSSM{h}, Route53: provisionRoute53{h},
				S3: provisionS3{h}, IAM: provisionIAM{h}, STS: provisionSTS{},
			},
		},
	}
}

func (h *provisionHarness) deps() seam.Deps {
	return seam.Deps{Dir: ".", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		if command.Path == "curl" {
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v9.8.7","published_at":"2026-09-17T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://downloads.example/opsctl-install.sh"}]}]`)}, nil
		}
		h.sshTargets++
		if len(command.Args) < 7 || command.Args[6] != "ec2-user@"+provisionAddress {
			h.wrongSSHTarget = true
		}
		if err := h.operation("ssh"); err != nil {
			return seam.Result{}, err
		}
		return seam.Result{}, nil
	}}
}

func (h *provisionHarness) cleanup(name string) error {
	h.cleanupCalls = append(h.cleanupCalls, name)
	return nil
}

func (h *provisionHarness) operation(name string) error {
	h.operations = append(h.operations, name)
	if h.failAt == name {
		return errProvisionTest
	}
	return nil
}

type provisionIAM struct{ h *provisionHarness }

func (f provisionIAM) RoleExists(context.Context, string) (bool, error) { return false, nil }
func (f provisionIAM) CreateRole(_ context.Context, spec cloud.RoleSpec) error {
	f.h.roleSpec = spec
	return f.h.operation("create-role")
}
func (f provisionIAM) PutRolePolicy(_ context.Context, role, policy, document string) error {
	f.h.policyRole, f.h.policyName, f.h.policyDocument = role, policy, document
	return f.h.operation("put-role-policy")
}
func (f provisionIAM) DeleteRolePolicy(context.Context, string, string) error {
	return f.h.cleanup("delete-role-policy")
}
func (provisionIAM) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	return nil, false, nil
}
func (f provisionIAM) CreateInstanceProfile(_ context.Context, name string) error {
	f.h.profileName = name
	return f.h.operation("create-instance-profile")
}
func (f provisionIAM) AddRoleToInstanceProfile(_ context.Context, profile, role string) error {
	f.h.addProfile, f.h.addRole = profile, role
	return f.h.operation("add-role-to-profile")
}
func (f provisionIAM) RemoveRoleFromInstanceProfile(context.Context, string, string) error {
	return f.h.cleanup("remove-role-from-profile")
}
func (f provisionIAM) DeleteInstanceProfile(context.Context, string) error {
	return f.h.cleanup("delete-instance-profile")
}
func (f provisionIAM) DeleteRole(context.Context, string) error {
	return f.h.cleanup("delete-role")
}

type provisionEC2 struct{ h *provisionHarness }

func (provisionEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) { return nil, nil }
func (f provisionEC2) LaunchReady(_ context.Context, spec cloud.LaunchSpec) (bool, error) {
	f.h.launchReadySpec = spec
	if err := f.h.operation("launch-ready"); err != nil {
		return false, err
	}
	return true, nil
}
func (f provisionEC2) DescribeInstance(_ context.Context, id string) (cloud.Instance, error) {
	f.h.describeInstanceIDs = append(f.h.describeInstanceIDs, id)
	if err := f.h.operation("describe-instance"); err != nil {
		return cloud.Instance{}, err
	}
	if id != provisionID {
		return cloud.Instance{}, fmt.Errorf("unexpected instance ID %q", id)
	}
	return cloud.Instance{ID: provisionID, State: cloud.StateRunning, Address: "198.51.100.9"}, nil
}
func (f provisionEC2) RunInstance(_ context.Context, spec cloud.LaunchSpec) (cloud.Instance, error) {
	f.h.launchSpec = spec
	if err := f.h.operation("run-instance"); err != nil {
		return cloud.Instance{}, err
	}
	return cloud.Instance{ID: provisionID, State: cloud.StatePending}, nil
}
func (provisionEC2) StartInstance(context.Context, string) error { return nil }
func (f provisionEC2) StopInstance(context.Context, string) error {
	return f.h.cleanup("stop-instance")
}
func (f provisionEC2) TerminateInstance(context.Context, string) error {
	return f.h.cleanup("terminate-instance")
}
func (f provisionEC2) InstanceChecksPassed(context.Context, string) (bool, error) {
	if err := f.h.operation("checks"); err != nil {
		return false, err
	}
	return true, nil
}
func (provisionEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) { return nil, nil }
func (f provisionEC2) AllocateAddress(_ context.Context, spaceName string) (cloud.Address, error) {
	f.h.allocateSpace = spaceName
	if err := f.h.operation("allocate-address"); err != nil {
		return cloud.Address{}, err
	}
	return cloud.Address{AllocationID: "eipalloc-one", IP: provisionAddress, Space: spaceName}, nil
}
func (f provisionEC2) AssociateAddress(_ context.Context, allocationID, instanceID string) error {
	f.h.associateAllocation, f.h.associateInstance = allocationID, instanceID
	return f.h.operation("associate-address")
}
func (f provisionEC2) DisassociateAddress(context.Context, string) error {
	return f.h.cleanup("disassociate-address")
}
func (f provisionEC2) ReleaseAddress(context.Context, string) error {
	return f.h.cleanup("release-address")
}

type provisionRoute53 struct{ h *provisionHarness }

func (provisionRoute53) ListZones(context.Context) ([]cloud.Zone, error) { return nil, nil }
func (provisionRoute53) ListRecords(context.Context, string) ([]cloud.Record, error) {
	return nil, nil
}
func (f provisionRoute53) ChangeRecords(_ context.Context, zone string, changes []cloud.RecordChange) (string, error) {
	for _, change := range changes {
		if change.Action == cloud.ChangeDelete {
			return "", f.h.cleanup("delete-records")
		}
	}
	f.h.recordCalls++
	f.h.recordZone = zone
	f.h.recordChanges = append([]cloud.RecordChange(nil), changes...)
	if err := f.h.operation("change-records"); err != nil {
		return "", err
	}
	return "change-one", nil
}
func (f provisionRoute53) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	if err := f.h.operation("change-status"); err != nil {
		return "", err
	}
	return cloud.ChangeInsync, nil
}

type provisionSSM struct{ h *provisionHarness }

func (provisionSSM) GetParameter(context.Context, string) (string, error)     { return "", nil }
func (provisionSSM) PutSecureParameter(context.Context, string, string) error { return nil }
func (provisionSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, nil
}
func (f provisionSSM) DeleteParameter(context.Context, string) error {
	return f.h.cleanup("delete-parameter")
}

type provisionS3 struct{ h *provisionHarness }

func (provisionS3) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	return nil, fmt.Errorf("unexpected ListObjects")
}
func (provisionS3) PutObject(context.Context, string, string, io.Reader, int64) error { return nil }
func (f provisionS3) DeleteObjects(context.Context, string, []string) error {
	return f.h.cleanup("delete-objects")
}

type provisionSTS struct{}

func (provisionSTS) CallerAccountID(context.Context) (string, error) { return "", nil }

func countProvisionOperations(operations []string, want string) int {
	count := 0
	for _, operation := range operations {
		if operation == want {
			count++
		}
	}
	return count
}
