package cli

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

func TestSpaceCreateAddressStep(t *testing.T) {
	// R-PX9J-BIQU
	t.Run("associate lag then success", func(t *testing.T) {
		fake := &addressStepFake{associateErrs: []error{&cloud.Error{Code: "InvalidAllocationID.NotFound"}}}
		result := invokeWithDeps(addressStepDeps(t, fake), "space", "create", "sbx1", "--acme-email", "ops@ikigenba.dev")
		lines := strings.Split(strings.TrimRight(result.stdout, "\n"), "\n")
		const want = "address: ok (elastic ip 18.118.7.42 associated)"
		if len(lines) < 6 || lines[5] != want {
			t.Fatalf("stdout = %q", result.stdout)
		}
		if fake.allocateCount != 1 || fake.allocateDomain != "ikigenba.dev" || fake.allocateSpace != "sbx1.ikigenba.dev" {
			t.Fatalf("AllocateAddress calls = %d, domain %q, space %q", fake.allocateCount, fake.allocateDomain, fake.allocateSpace)
		}
		wantAssociate := [][2]string{{"eipalloc-1", "i-0c9e94542d98846a8"}, {"eipalloc-1", "i-0c9e94542d98846a8"}}
		if fake.associateCount != 2 || !reflect.DeepEqual(fake.associates, wantAssociate) {
			t.Fatalf("AssociateAddress calls = %d %#v", fake.associateCount, fake.associates)
		}
	})

	t.Run("allocate limit", func(t *testing.T) {
		fake := &addressStepFake{allocateErr: &cloud.Error{
			Service: "ec2", Operation: "AllocateAddress", Code: "AddressLimitExceeded",
		}}
		result := invokeWithDeps(addressStepDeps(t, fake), "space", "create", "sbx1", "--acme-email", "ops@ikigenba.dev")
		if result.code != 1 || result.stderr != "devctl: ec2 AllocateAddress: AddressLimitExceeded\n" || strings.Contains(result.stdout, "address:") {
			t.Fatalf("result = %#v", result)
		}
		if len(fake.calls) == 0 || fake.calls[len(fake.calls)-1] != "AllocateAddress" {
			t.Fatalf("calls = %v", fake.calls)
		}
		if fake.releaseCount != 0 || fake.terminateCount != 0 || fake.deleteRoleCount != 0 || fake.associateCount != 0 {
			t.Fatalf("later calls: release %d terminate %d delete-role %d associate %d", fake.releaseCount, fake.terminateCount, fake.deleteRoleCount, fake.associateCount)
		}
	})
}

func addressStepDeps(t *testing.T, fake *addressStepFake) seam.Deps {
	t.Helper()
	deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	deps.After = func(time.Duration) <-chan time.Time {
		ready := make(chan time.Time, 1)
		ready <- time.Time{}
		return ready
	}
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{EC2: fake, IAM: fake, Route53: fake, STS: fake}, nil
	}
	return deps
}

type addressStepFake struct {
	cloud.EC2
	cloud.IAM
	cloud.Route53
	cloud.STS

	calls                         []string
	associates                    [][2]string
	associateErrs                 []error
	allocateErr                   error
	allocateCount, associateCount int
	releaseCount, terminateCount  int
	deleteRoleCount               int
	allocateDomain, allocateSpace string
}

func (f *addressStepFake) call(name string) { f.calls = append(f.calls, name) }

func (f *addressStepFake) CallerAccountID(context.Context) (string, error) {
	f.call("CallerAccountID")
	return "123456789012", nil
}

func (f *addressStepFake) Zone(context.Context, string) (cloud.Zone, error) {
	f.call("Zone")
	return cloud.Zone{ID: "ZROOT", Name: "ikigenba.dev"}, nil
}

func (f *addressStepFake) FindRecord(context.Context, string, string, string) (cloud.Record, bool, error) {
	f.call("FindRecord")
	return cloud.Record{}, false, nil
}

func (f *addressStepFake) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	f.call("ChangeRecords")
	return "", errors.New("stop after address")
}

func (f *addressStepFake) LaunchTemplate(context.Context, string) (string, error) {
	f.call("LaunchTemplate")
	return "lt-root", nil
}

func (f *addressStepFake) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	f.call("ListSpaceInstances")
	return nil, nil
}

func (f *addressStepFake) DescribeInstance(_ context.Context, id string) (cloud.Instance, error) {
	f.call("DescribeInstance")
	return cloud.Instance{ID: id, State: cloud.StateRunning, Address: "3.19.79.227"}, nil
}

func (f *addressStepFake) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	f.call("RunInstance")
	return cloud.Instance{ID: "i-0c9e94542d98846a8"}, nil
}

func (f *addressStepFake) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) {
	f.call("LaunchReady")
	return true, nil
}

func (f *addressStepFake) AllocateAddress(_ context.Context, domain, space string) (cloud.Address, error) {
	f.call("AllocateAddress")
	f.allocateCount++
	f.allocateDomain, f.allocateSpace = domain, space
	if f.allocateErr != nil {
		return cloud.Address{}, f.allocateErr
	}
	return cloud.Address{AllocationID: "eipalloc-1", IP: "18.118.7.42"}, nil
}

func (f *addressStepFake) AssociateAddress(_ context.Context, allocationID, instanceID string) error {
	f.call("AssociateAddress")
	f.associateCount++
	f.associates = append(f.associates, [2]string{allocationID, instanceID})
	if f.associateCount <= len(f.associateErrs) {
		return f.associateErrs[f.associateCount-1]
	}
	return nil
}

func (f *addressStepFake) ReleaseAddress(context.Context, string) error {
	f.call("ReleaseAddress")
	f.releaseCount++
	return nil
}

func (f *addressStepFake) TerminateInstance(context.Context, string) error {
	f.call("TerminateInstance")
	f.terminateCount++
	return nil
}

func (f *addressStepFake) PermissionsBoundary(context.Context, string) (string, error) {
	f.call("PermissionsBoundary")
	return "arn:boundary", nil
}

func (f *addressStepFake) RoleExists(context.Context, string) (bool, error) {
	f.call("RoleExists")
	return false, nil
}

func (f *addressStepFake) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	f.call("InstanceProfileRoles")
	return nil, false, nil
}

func (f *addressStepFake) CreateRole(context.Context, cloud.RoleSpec) error {
	f.call("CreateRole")
	return nil
}

func (f *addressStepFake) PutRolePolicy(context.Context, string, string, string) error {
	f.call("PutRolePolicy")
	return nil
}

func (f *addressStepFake) CreateInstanceProfile(context.Context, string) error {
	f.call("CreateInstanceProfile")
	return nil
}

func (f *addressStepFake) AddRoleToInstanceProfile(context.Context, string, string) error {
	f.call("AddRoleToInstanceProfile")
	return nil
}

func (f *addressStepFake) DeleteRole(context.Context, string) error {
	f.call("DeleteRole")
	f.deleteRoleCount++
	return nil
}
