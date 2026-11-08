package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type retiredRegistry struct {
	*commandHarness
	instances []cloud.Instance
}

func (f *retiredRegistry) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return f.instances, nil
}
func (f *retiredRegistry) StopInstance(context.Context, string) error {
	f.t.Fatal("stopped ambiguous space")
	return nil
}
func TestCreateRefusesAnySurvivingInstance(t *testing.T) {
	// R-VMHE-RWGY R-VNPB-5O7N
	const oldID = "i-0c9e94542d98846a8"
	const otherID = "i-0a1b2c3d4e5f60718"
	for _, tc := range []struct {
		name      string
		instances []cloud.Instance
		id        string
	}{
		{name: "active", instances: []cloud.Instance{{ID: oldID, Space: testDomain, State: cloud.StateRunning}}, id: oldID},
		{name: "active before retired", instances: []cloud.Instance{{ID: otherID, Space: testDomain, State: cloud.StateStopped, Retired: moveTime}, {ID: oldID, Space: testDomain, State: cloud.StateRunning}}, id: oldID},
		{name: "only retired", instances: []cloud.Instance{{ID: oldID, Space: testDomain, State: cloud.StateStopped, Retired: moveTime}}, id: oldID + " retired"},
		{name: "retired sorted", instances: []cloud.Instance{{ID: oldID, Space: testDomain, State: cloud.StateStopped, Retired: moveTime}, {ID: otherID, Space: testDomain, State: cloud.StateStopped, Retired: moveTime}}, id: otherID + " retired"},
		{name: "terminated", instances: []cloud.Instance{{ID: oldID, Space: testDomain, State: cloud.StateTerminated}, {ID: otherID, Space: testDomain, State: cloud.StateTerminated, Retired: moveTime}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &retiredRegistry{commandHarness: newCommandHarness(t), instances: tc.instances}
			f.createRoleErr = errors.New("stopped after secrets")
			deps := f.deps()
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				return cloud.Clients{EC2: f, SSM: f, IAM: f, Route53: f, S3: f, STS: f}, nil
			}
			result := invokeWithDeps(deps, "space", "create", "sbx1", "--acme-email=x")
			if tc.id != "" {
				assertResult(t, result, 2, "", "devctl: a space at '"+testDomain+"' already exists ("+tc.id+")\n")
				if len(f.mutations) != 0 {
					t.Fatal(f.mutations)
				}
			} else if result.code != 1 || !strings.HasSuffix(result.stdout, "secrets: ok (0 apps)\n") || result.stderr != "devctl: stopped after secrets\n" {
				t.Fatal(result)
			}
		})
	}
}
func TestStopRefusesDuplicateActiveInstances(t *testing.T) {
	// R-FDSH-77DG
	f := &retiredRegistry{commandHarness: newCommandHarness(t), instances: []cloud.Instance{
		{ID: "i-0f1e2d3c4b5a69788", Space: testDomain, State: cloud.StateRunning},
		{ID: "i-0c9e94542d98846a8", Space: testDomain, State: cloud.StateRunning},
	}}
	deps := f.deps()
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{EC2: f, STS: f}, nil
	}
	result := invokeWithDeps(deps, "space", "stop", "sbx1")
	assertResult(t, result, 2, "", "devctl: 'sbx1.ikigenba.dev' has more than one active instance (i-0c9e94542d98846a8, i-0f1e2d3c4b5a69788)\n")
}
