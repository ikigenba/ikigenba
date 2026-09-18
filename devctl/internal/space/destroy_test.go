package space

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
)

const destroyDomain = "foo.sbx.ikigenba.dev"

func TestDestroyExistingSpaceInOrder(t *testing.T) {
	// R-UIKQ-FPMI R-UL0J-793W R-UPW4-QC2O R-DSU2-631X R-DU1Y-JUSM R-3KXX-07CJ R-JCCP-CNWX
	state := newDestroyState(t, false, false)
	state.instances = []cloud.Instance{{ID: "i-one", Space: destroyDomain, State: cloud.StateRunning, Address: "18.220.10.5"}}
	state.addresses = []cloud.Address{{AllocationID: "eipalloc-one", AssociationID: "eipassoc-one", IP: "18.220.10.5", Space: destroyDomain}}
	state.zones = []cloud.Zone{{ID: "zone-one", Name: "sbx.ikigenba.dev"}}
	state.records = []cloud.Record{
		{Name: destroyDomain, Type: "A", TTL: 60, Values: []string{"18.220.10.5"}},
		{Name: `\052.` + destroyDomain, Type: "A", TTL: 61, Values: []string{"18.220.10.5"}},
		{Name: destroyDomain, Type: "TXT", Values: []string{"keep"}},
	}
	state.roleExists = true
	state.execStdout = "arbitrary output that devctl must ignore\n"

	stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain})
	if err != nil {
		t.Fatalf("Run(destroy) error = %v", err)
	}
	want := "retire: ok (opsctl retire)\n" +
		"instance: ok (i-one terminated)\n" +
		"address: ok (elastic ip 18.220.10.5 released)\n" +
		"records: ok (deleted " + destroyDomain + ", *." + destroyDomain + ")\n" +
		"secrets: ok (kept, delete_secrets_on_destroy=false)\n" +
		"backups: ok (kept, delete_backups_on_destroy=false)\n" +
		"role: ok (" + RoleName(destroyDomain) + " deleted)\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	wantOps := []string{
		"ListSpaceInstances", "ssh sudo opsctl retire", "TerminateInstance i-one", "DescribeInstance i-one",
		"ListSpaceAddresses", "DisassociateAddress eipassoc-one", "ReleaseAddress eipalloc-one",
		"ListZones", "ListRecords zone-one", "ChangeRecords zone-one",
		"RoleExists " + RoleName(destroyDomain), "InstanceProfileRoles " + RoleName(destroyDomain),
		"DeleteRolePolicy " + RoleName(destroyDomain) + " space", "DeleteInstanceProfile " + RoleName(destroyDomain),
		"DeleteRole " + RoleName(destroyDomain),
	}
	if !reflect.DeepEqual(state.ops, wantOps) {
		t.Fatalf("operations = %#v, want %#v", state.ops, wantOps)
	}
	if len(state.changedRecords) != 2 || !reflect.DeepEqual(state.changedRecords[0].Record, state.records[0]) || !reflect.DeepEqual(state.changedRecords[1].Record, state.records[1]) {
		t.Fatalf("record changes = %#v", state.changedRecords)
	}
}

func TestDestroyDeletesSecretsAndBackups(t *testing.T) {
	// R-UNGB-YSLA R-UOO8-CKBZ R-DSU2-631X
	state := newDestroyState(t, true, true)
	state.instances = []cloud.Instance{{ID: "i-one", Space: destroyDomain, State: cloud.StateRunning}}
	state.parameters = []cloud.Parameter{{Name: "one"}, {Name: "two"}, {Name: "three"}}
	state.zones = nil

	stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain, "--no-backup"})
	if err != nil {
		t.Fatalf("Run(destroy) error = %v", err)
	}
	if strings.Contains(stdout, "retire:") {
		t.Fatalf("stdout unexpectedly contains retire step: %q", stdout)
	}
	for _, line := range []string{
		"instance: ok (i-one terminated)\n",
		"address: ok (no elastic ip)\n",
		"records: ok (already gone)\n",
		"secrets: ok (3 parameters deleted)\n",
		"backups: ok (0 objects deleted)\n",
		"role: ok (already gone)\n",
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("stdout %q lacks %q", stdout, line)
		}
	}
	wantDeletes := []string{"one", "two", "three"}
	if !reflect.DeepEqual(state.deletedParameters, wantDeletes) {
		t.Fatalf("deleted parameters = %v", state.deletedParameters)
	}
	if state.listParameterPrefix != secrets.Prefix(destroyDomain) {
		t.Fatalf("ListParameters prefix = %q", state.listParameterPrefix)
	}
	if state.listBucket != "backups" || state.listPrefix != BackupPrefix(destroyDomain) || state.deleteBucket != "backups" || state.deletedKeys == nil || len(state.deletedKeys) != 0 {
		t.Fatalf("S3 calls = list(%q,%q), delete(%q,%#v)", state.listBucket, state.listPrefix, state.deleteBucket, state.deletedKeys)
	}
}

func TestDestroyAlreadyGone(t *testing.T) {
	// R-UIKQ-FPMI R-UL0J-793W R-UNGB-YSLA R-UOO8-CKBZ R-UPW4-QC2O R-DWHR-BEA0
	state := newDestroyState(t, true, true)
	stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain})
	if err != nil {
		t.Fatalf("Run(destroy) error = %v", err)
	}
	want := "instance: ok (already gone)\n" +
		"address: ok (already gone)\n" +
		"records: ok (already gone)\n" +
		"secrets: ok (already gone)\n" +
		"backups: ok (already gone)\n" +
		"role: ok (already gone)\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	if strings.Contains(strings.Join(state.ops, "\n"), "ssh") || strings.Contains(strings.Join(state.ops, "\n"), "TerminateInstance") {
		t.Fatalf("operations = %v", state.ops)
	}
}

func TestDestroyRetainedBackupBranches(t *testing.T) {
	// R-DWHR-BEA0 R-E1DC-UH8S
	t.Run("missing instance", func(t *testing.T) {
		state := newDestroyState(t, false, false)
		stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain})
		if err != nil || !strings.HasPrefix(stdout, "retire: ok (already gone)\n") || strings.Contains(strings.Join(state.ops, " "), "ssh") {
			t.Fatalf("stdout=%q err=%v ops=%v", stdout, err, state.ops)
		}
	})
	t.Run("skip existing instance", func(t *testing.T) {
		state := newDestroyState(t, false, false)
		state.instances = []cloud.Instance{{ID: "i-one", Space: destroyDomain, State: cloud.StateRunning}}
		stdout, err := runDestroyTest(t, state, []string{"destroy", "--no-backup", destroyDomain, "--no-backup"})
		if err != nil || !strings.HasPrefix(stdout, "retire: skipped (--no-backup)\n") || strings.Contains(strings.Join(state.ops, " "), "ssh") {
			t.Fatalf("stdout=%q err=%v ops=%v", stdout, err, state.ops)
		}
	})
}

func TestDestroyRefusesNonRunningRetire(t *testing.T) {
	// R-DYXK-2XRE
	state := newDestroyState(t, false, false)
	state.instances = []cloud.Instance{{ID: "i-stopped", Space: destroyDomain, State: cloud.StateStopped}}
	stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain})
	var stateErr *RetireStateError
	if stdout != "" || !errors.As(err, &stateErr) || stateErr.ID != "i-stopped" || stateErr.Domain != destroyDomain || stateErr.Profile != "sandbox" {
		t.Fatalf("stdout=%q error=%#v", stdout, err)
	}
	if got := state.ops; !reflect.DeepEqual(got, []string{"ListSpaceInstances"}) {
		t.Fatalf("operations = %v", got)
	}
}

func TestDestroyRetireFailurePreventsCloudMutation(t *testing.T) {
	// R-3KXX-07CJ R-DSU2-631X
	state := newDestroyState(t, false, false)
	state.instances = []cloud.Instance{{ID: "i-one", Space: destroyDomain, State: cloud.StateRunning, Address: "18.220.10.5"}}
	state.execStatus = 7
	state.execStderr = "backup crm failed\nsecond line\n"
	stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain})
	var commandErr *host.CommandError
	if stdout != "" || !errors.As(err, &commandErr) {
		t.Fatalf("stdout=%q error=%#v", stdout, err)
	}
	if got := commandErr.Detail(); got != "> backup crm failed\n> second line" {
		t.Fatalf("Detail() = %q", got)
	}
	if got := state.ops; !reflect.DeepEqual(got, []string{"ListSpaceInstances", "ssh sudo opsctl retire"}) {
		t.Fatalf("operations = %v", got)
	}
}

func TestDestroyRetireSuccessIgnoresOutput(t *testing.T) {
	// R-JCCP-CNWX
	outputs := []struct {
		name   string
		stdout string
		stderr string
	}{
		{name: "opaque streams", stdout: `{"backed_up":["host"],"unknown":true}`, stderr: "warning: arbitrary stderr\n"},
		{name: "misleading report", stdout: `{"backed_up":["crm"]}`, stderr: `{"error":"not an error"}`},
		{name: "non-json streams", stdout: "not-json", stderr: "also not-json"},
		{name: "empty streams"},
	}
	for _, output := range outputs {
		t.Run(output.name, func(t *testing.T) {
			state := newDestroyState(t, false, false)
			state.instances = []cloud.Instance{{ID: "i-one", Space: destroyDomain, State: cloud.StateRunning, Address: "18.220.10.5"}}
			state.execStdout = output.stdout
			state.execStderr = output.stderr
			stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain})
			want := "retire: ok (opsctl retire)\n" +
				"instance: ok (i-one terminated)\n" +
				"address: ok (no elastic ip)\n" +
				"records: ok (already gone)\n" +
				"secrets: ok (kept, delete_secrets_on_destroy=false)\n" +
				"backups: ok (kept, delete_backups_on_destroy=false)\n" +
				"role: ok (already gone)\n"
			if err != nil || stdout != want {
				t.Fatalf("stdout=%q error=%v", stdout, err)
			}
			for _, operation := range state.ops {
				if strings.HasPrefix(operation, "ListObjects ") {
					t.Fatalf("retire success read backup bucket: %v", state.ops)
				}
			}
		})
	}
}

func TestDestroyStopsAtFirstFailure(t *testing.T) {
	// R-DSU2-631X R-TPB5-97TU
	state := newDestroyState(t, true, true)
	state.instances = []cloud.Instance{{ID: "i-one", Space: destroyDomain, State: cloud.StateRunning}}
	state.addresses = []cloud.Address{{AllocationID: "alloc", IP: "18.220.10.5", Space: destroyDomain}}
	state.releaseErr = errors.New("release failed")
	stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain})
	if !errors.Is(err, state.releaseErr) || stdout != "instance: ok (i-one terminated)\n" {
		t.Fatalf("stdout=%q error=%v", stdout, err)
	}
	if strings.Contains(strings.Join(state.ops, " "), "ListZones") {
		t.Fatalf("continued after failure: %v", state.ops)
	}
}

func TestDestroyReportsSingleDeletedRecord(t *testing.T) {
	// R-DU1Y-JUSM
	state := newDestroyState(t, true, true)
	state.zones = []cloud.Zone{{ID: "zone-one", Name: "sbx.ikigenba.dev"}}
	state.records = []cloud.Record{{Name: "*." + destroyDomain, Type: "A", TTL: 300, Values: []string{"1.2.3.4"}}}
	stdout, err := runDestroyTest(t, state, []string{"destroy", destroyDomain})
	if err != nil || !strings.Contains(stdout, "records: ok (deleted *."+destroyDomain+")\n") {
		t.Fatalf("stdout=%q error=%v", stdout, err)
	}
	if len(state.changedRecords) != 1 || !reflect.DeepEqual(state.changedRecords[0].Record, state.records[0]) {
		t.Fatalf("changes = %#v", state.changedRecords)
	}
}

func runDestroyTest(t *testing.T, state *destroyState, args []string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	err := Run(context.Background(), args, &stdout, state.deps(), "sandbox")
	return stdout.String(), err
}

type destroyState struct {
	t                   *testing.T
	deleteSecrets       bool
	deleteBackups       bool
	instances           []cloud.Instance
	addresses           []cloud.Address
	zones               []cloud.Zone
	records             []cloud.Record
	parameters          []cloud.Parameter
	objects             []cloud.Object
	roleExists          bool
	profileRoles        []string
	profileExists       bool
	execStdout          string
	execStderr          string
	execStatus          int
	releaseErr          error
	ops                 []string
	changedRecords      []cloud.RecordChange
	deletedParameters   []string
	listParameterPrefix string
	listBucket          string
	listPrefix          string
	deleteBucket        string
	deletedKeys         []string
}

func newDestroyState(t *testing.T, deleteSecrets, deleteBackups bool) *destroyState {
	return &destroyState{t: t, deleteSecrets: deleteSecrets, deleteBackups: deleteBackups}
}

func (s *destroyState) deps() seam.Deps {
	cloudCalls := 0
	return seam.Deps{
		Dir: s.t.TempDir(),
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			if cloudCalls == 1 {
				return cloud.Clients{SSM: &destroyBootstrap{s: s}}, nil
			}
			return cloud.Clients{EC2: s, SSM: s, Route53: s, S3: s, IAM: s, STS: s}, nil
		},
		Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			if cmd.Path != "ssh" || len(cmd.Args) != 8 || cmd.Args[7] != "'sudo' 'opsctl' 'retire'" {
				s.t.Fatalf("host command = %#v", cmd)
			}
			s.ops = append(s.ops, "ssh sudo opsctl retire")
			return seam.Result{ExitCode: s.execStatus, Stdout: []byte(s.execStdout), Stderr: []byte(s.execStderr)}, nil
		},
		After: func(time.Duration) <-chan time.Time {
			ready := make(chan time.Time, 1)
			ready <- time.Time{}
			return ready
		},
	}
}

type destroyBootstrap struct{ s *destroyState }

func (b *destroyBootstrap) GetParameter(context.Context, string) (string, error) {
	return fmt.Sprintf(`{"domain":"sbx.ikigenba.dev","backup_bucket":"backups","launch_template_id":"lt","permissions_boundary_arn":"arn","region":"us-east-2","delete_secrets_on_destroy":%t,"delete_backups_on_destroy":%t,"backup_host_files_seconds":0,"backup_service_files_seconds":0,"backup_service_db_seconds":0,"backup_service_wal_seconds":0}`, b.s.deleteSecrets, b.s.deleteBackups), nil
}
func (*destroyBootstrap) PutSecureParameter(context.Context, string, string) error { return nil }
func (*destroyBootstrap) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, nil
}
func (*destroyBootstrap) DeleteParameter(context.Context, string) error { return nil }

func (s *destroyState) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	s.ops = append(s.ops, "ListSpaceInstances")
	return s.instances, nil
}
func (s *destroyState) DescribeInstance(_ context.Context, id string) (cloud.Instance, error) {
	s.ops = append(s.ops, "DescribeInstance "+id)
	return cloud.Instance{ID: id, State: cloud.StateTerminated}, nil
}
func (s *destroyState) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	panic("unexpected RunInstance")
}
func (s *destroyState) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) {
	panic("unexpected LaunchReady")
}
func (s *destroyState) StartInstance(context.Context, string) error {
	panic("unexpected StartInstance")
}
func (s *destroyState) StopInstance(context.Context, string) error { panic("unexpected StopInstance") }
func (s *destroyState) TerminateInstance(_ context.Context, id string) error {
	s.ops = append(s.ops, "TerminateInstance "+id)
	return nil
}
func (s *destroyState) InstanceChecksPassed(context.Context, string) (bool, error) {
	panic("unexpected InstanceChecksPassed")
}
func (s *destroyState) ListSpaceAddresses(context.Context) ([]cloud.Address, error) {
	s.ops = append(s.ops, "ListSpaceAddresses")
	return s.addresses, nil
}
func (s *destroyState) AllocateAddress(context.Context, string) (cloud.Address, error) {
	panic("unexpected AllocateAddress")
}
func (s *destroyState) AssociateAddress(context.Context, string, string) error {
	panic("unexpected AssociateAddress")
}
func (s *destroyState) DisassociateAddress(_ context.Context, id string) error {
	s.ops = append(s.ops, "DisassociateAddress "+id)
	return nil
}
func (s *destroyState) ReleaseAddress(_ context.Context, id string) error {
	s.ops = append(s.ops, "ReleaseAddress "+id)
	return s.releaseErr
}

func (s *destroyState) GetParameter(context.Context, string) (string, error) {
	panic("unexpected GetParameter")
}
func (s *destroyState) PutSecureParameter(context.Context, string, string) error {
	panic("unexpected PutSecureParameter")
}
func (s *destroyState) ListParameters(_ context.Context, prefix string) ([]cloud.Parameter, error) {
	s.ops = append(s.ops, "ListParameters "+prefix)
	s.listParameterPrefix = prefix
	return s.parameters, nil
}
func (s *destroyState) DeleteParameter(_ context.Context, name string) error {
	s.ops = append(s.ops, "DeleteParameter "+name)
	s.deletedParameters = append(s.deletedParameters, name)
	return nil
}

func (s *destroyState) ListZones(context.Context) ([]cloud.Zone, error) {
	s.ops = append(s.ops, "ListZones")
	return s.zones, nil
}
func (s *destroyState) ListRecords(_ context.Context, zone string) ([]cloud.Record, error) {
	s.ops = append(s.ops, "ListRecords "+zone)
	return s.records, nil
}
func (s *destroyState) ChangeRecords(_ context.Context, zone string, changes []cloud.RecordChange) (string, error) {
	s.ops = append(s.ops, "ChangeRecords "+zone)
	s.changedRecords = append([]cloud.RecordChange(nil), changes...)
	return "change-one", nil
}
func (s *destroyState) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	panic("unexpected ChangeStatus")
}

func (s *destroyState) ListObjects(_ context.Context, bucket, prefix string) ([]cloud.Object, error) {
	s.ops = append(s.ops, "ListObjects "+bucket+" "+prefix)
	s.listBucket, s.listPrefix = bucket, prefix
	return s.objects, nil
}
func (s *destroyState) PutObject(context.Context, string, string, io.Reader, int64) error {
	panic("unexpected PutObject")
}
func (s *destroyState) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	s.ops = append(s.ops, "DeleteObjects "+bucket)
	s.deleteBucket = bucket
	s.deletedKeys = append([]string{}, keys...)
	return nil
}

func (s *destroyState) RoleExists(_ context.Context, name string) (bool, error) {
	s.ops = append(s.ops, "RoleExists "+name)
	return s.roleExists, nil
}
func (s *destroyState) CreateRole(context.Context, cloud.RoleSpec) error {
	panic("unexpected CreateRole")
}
func (s *destroyState) PutRolePolicy(context.Context, string, string, string) error {
	panic("unexpected PutRolePolicy")
}
func (s *destroyState) DeleteRolePolicy(_ context.Context, role, policy string) error {
	s.ops = append(s.ops, "DeleteRolePolicy "+role+" "+policy)
	return nil
}
func (s *destroyState) InstanceProfileRoles(_ context.Context, name string) ([]string, bool, error) {
	s.ops = append(s.ops, "InstanceProfileRoles "+name)
	return s.profileRoles, s.profileExists, nil
}
func (s *destroyState) CreateInstanceProfile(context.Context, string) error {
	panic("unexpected CreateInstanceProfile")
}
func (s *destroyState) AddRoleToInstanceProfile(context.Context, string, string) error {
	panic("unexpected AddRoleToInstanceProfile")
}
func (s *destroyState) RemoveRoleFromInstanceProfile(_ context.Context, profile, role string) error {
	s.ops = append(s.ops, "RemoveRoleFromInstanceProfile "+profile+" "+role)
	return nil
}
func (s *destroyState) DeleteInstanceProfile(_ context.Context, name string) error {
	s.ops = append(s.ops, "DeleteInstanceProfile "+name)
	return nil
}
func (s *destroyState) DeleteRole(_ context.Context, name string) error {
	s.ops = append(s.ops, "DeleteRole "+name)
	return nil
}
func (s *destroyState) CallerAccountID(context.Context) (string, error) {
	panic("unexpected CallerAccountID")
}
