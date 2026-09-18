package space

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const actionProperties = `{
  "domain":"sbx.ikigenba.dev",
  "backup_bucket":"backups",
  "launch_template_id":"lt-one",
  "permissions_boundary_arn":"arn:boundary",
  "region":"us-east-2",
  "delete_secrets_on_destroy":false,
  "delete_backups_on_destroy":false,
  "backup_host_files_seconds":0,
  "backup_service_files_seconds":0,
  "backup_service_db_seconds":0,
  "backup_service_wal_seconds":0
}`

func TestRunList(t *testing.T) {
	// R-U3XX-UGQ6 R-DMQK-98CG R-8YVJ-0IZI
	tests := []struct {
		name      string
		instances []cloud.Instance
		want      string
	}{
		{
			name: "populated",
			instances: []cloud.Instance{
				{ID: "i-new", Space: "new.sbx.ikigenba.dev", State: cloud.StateRunning, Address: "18.220.10.5"},
				{ID: "i-bar", Space: "bar.sbx.ikigenba.dev", State: cloud.StateStopped},
				{ID: "i-foo", Space: "foo.sbx.ikigenba.dev", State: cloud.StateRunning, Address: "3.19.79.227"},
			},
			want: "bar.sbx.ikigenba.dev stopped -\n" +
				"foo.sbx.ikigenba.dev running 3.19.79.227\n" +
				"new.sbx.ikigenba.dev running 18.220.10.5\n",
		},
		{name: "empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ec2 := newActionEC2(t, test.instances)
			deps, assertCheckout := actionDeps(t, ec2, func(context.Context, seam.Cmd) (seam.Result, error) {
				t.Fatal("space list executed an external process")
				return seam.Result{}, nil
			})
			var stdout bytes.Buffer
			if err := Run(context.Background(), []string{"list"}, &stdout, deps, "sandbox"); err != nil {
				t.Fatalf("Run(list) error = %v", err)
			}
			if got := stdout.String(); got != test.want {
				t.Fatalf("stdout = %q, want %q", got, test.want)
			}
			if got := ec2.calls; !reflect.DeepEqual(got, []string{"ListSpaceInstances"}) {
				t.Fatalf("EC2 calls = %v", got)
			}
			assertCheckout()
		})
	}
}

func TestRunStatusRelaysOpaqueOutput(t *testing.T) {
	// R-S9WX-LSSA R-8YVJ-0IZI
	for _, output := range []string{"crm v1 running wal\ndashboard v2 stopped delete\n", "no trailing newline", ""} {
		t.Run(output, func(t *testing.T) {
			ec2 := newActionEC2(t, []cloud.Instance{{
				ID: "i-foo", Space: "foo.sbx.ikigenba.dev", State: cloud.StateRunning, Address: "3.19.79.227",
			}})
			var commands []seam.Cmd
			deps, assertCheckout := actionDeps(t, ec2, func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
				commands = append(commands, cmd)
				return seam.Result{Stdout: []byte(output)}, nil
			})
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"status", "foo.sbx.ikigenba.dev"}, &stdout, deps, "sandbox")
			if err != nil {
				t.Fatalf("Run(status) error = %v", err)
			}
			if got := stdout.String(); got != output {
				t.Fatalf("stdout = %q, want byte-for-byte %q", got, output)
			}
			wantCommand := seam.Cmd{
				Path: "ssh",
				Args: []string{
					"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new",
					"ec2-user@3.19.79.227", "'sudo' 'opsctl' 'status'",
				},
				Dir: deps.Dir,
			}
			if !reflect.DeepEqual(commands, []seam.Cmd{wantCommand}) {
				t.Fatalf("commands = %#v, want %#v", commands, []seam.Cmd{wantCommand})
			}
			if got := ec2.calls; !reflect.DeepEqual(got, []string{"ListSpaceInstances"}) {
				t.Fatalf("EC2 calls = %v", got)
			}
			assertCheckout()
		})
	}
}

func TestRunStatusRequiresRunningSpace(t *testing.T) {
	// R-U6DQ-M07K
	ec2 := newActionEC2(t, []cloud.Instance{{
		ID: "i-bar", Space: "bar.sbx.ikigenba.dev", State: cloud.StateStopped,
	}})
	execCalls := 0
	deps, _ := actionDeps(t, ec2, func(context.Context, seam.Cmd) (seam.Result, error) {
		execCalls++
		return seam.Result{}, nil
	})
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"status", "bar.sbx.ikigenba.dev"}, &stdout, deps, "sandbox")
	var stateErr *NotRunningError
	if !errors.As(err, &stateErr) || stateErr.Domain != "bar.sbx.ikigenba.dev" || stateErr.State != cloud.StateStopped {
		t.Fatalf("error = %#v", err)
	}
	if reflect.TypeOf(err) != reflect.TypeOf(stateErr) {
		t.Fatalf("error type = %T, want *NotRunningError", err)
	}
	if got, want := err.Error(), "'bar.sbx.ikigenba.dev' is stopped"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if stdout.Len() != 0 || execCalls != 0 {
		t.Fatalf("stdout = %q, Exec calls = %d", stdout.String(), execCalls)
	}
}

func TestRunLookupFailurePrecedesEffects(t *testing.T) {
	// R-U55U-88GV
	wantErr := &actionLookupError{}
	for _, command := range []string{"status", "stop"} {
		t.Run(command, func(t *testing.T) {
			ec2 := newActionEC2(t, nil)
			ec2.listErr = wantErr
			execCalls := 0
			deps, _ := actionDeps(t, ec2, func(context.Context, seam.Cmd) (seam.Result, error) {
				execCalls++
				return seam.Result{}, nil
			})
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{command, "foo.sbx.ikigenba.dev"}, &stdout, deps, "sandbox")
			if reflect.ValueOf(err) != reflect.ValueOf(wantErr) {
				t.Fatalf("error = %#v, want identical error %#v", err, wantErr)
			}
			if stdout.Len() != 0 || execCalls != 0 {
				t.Fatalf("stdout = %q, Exec calls = %d", stdout.String(), execCalls)
			}
			if got := ec2.calls; !reflect.DeepEqual(got, []string{"ListSpaceInstances"}) {
				t.Fatalf("EC2 calls = %v", got)
			}
		})
	}
}

type actionLookupError struct{}

func (*actionLookupError) Error() string { return "lookup failed" }

func TestRunLookupUsesDomainOperand(t *testing.T) {
	// R-U55U-88GV
	const domain = "operand.sbx.ikigenba.dev"
	for _, command := range []string{"status", "stop"} {
		t.Run(command, func(t *testing.T) {
			ec2 := newActionEC2(t, nil)
			deps, _ := actionDeps(t, ec2, func(context.Context, seam.Cmd) (seam.Result, error) {
				t.Fatal("lookup failure executed an external process")
				return seam.Result{}, nil
			})
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{command, domain}, &stdout, deps, "sandbox")
			var noSpaceErr *account.NoSpaceError
			if !errors.As(err, &noSpaceErr) || noSpaceErr.Domain != domain {
				t.Fatalf("error = %#v, want *account.NoSpaceError for operand %q", err, domain)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if got := ec2.calls; !reflect.DeepEqual(got, []string{"ListSpaceInstances"}) {
				t.Fatalf("EC2 calls = %v", got)
			}
		})
	}
}

func TestRunStop(t *testing.T) {
	// R-U8TJ-DJOY R-8XNM-MR8T
	tests := []struct {
		name      string
		state     cloud.InstanceState
		want      string
		wantCalls []string
	}{
		{
			name:      "running",
			state:     cloud.StateRunning,
			want:      "instance: ok (i-0c9e94542d98846a8 stopped)\n",
			wantCalls: []string{"ListSpaceInstances", "StopInstance i-0c9e94542d98846a8", "DescribeInstance i-0c9e94542d98846a8"},
		},
		{
			name:      "already stopped",
			state:     cloud.StateStopped,
			want:      "instance: ok (already stopped)\n",
			wantCalls: []string{"ListSpaceInstances"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ec2 := newActionEC2(t, []cloud.Instance{{
				ID: "i-0c9e94542d98846a8", Space: "foo.sbx.ikigenba.dev", State: test.state,
			}})
			deps, assertCheckout := actionDeps(t, ec2, func(context.Context, seam.Cmd) (seam.Result, error) {
				t.Fatal("space stop executed an external process")
				return seam.Result{}, nil
			})
			var stdout bytes.Buffer
			if err := Run(context.Background(), []string{"stop", "foo.sbx.ikigenba.dev"}, &stdout, deps, "sandbox"); err != nil {
				t.Fatalf("Run(stop) error = %v", err)
			}
			if got := stdout.String(); got != test.want {
				t.Fatalf("stdout = %q, want %q", got, test.want)
			}
			if !reflect.DeepEqual(ec2.calls, test.wantCalls) {
				t.Fatalf("EC2 calls = %v, want %v", ec2.calls, test.wantCalls)
			}
			assertCheckout()
		})
	}
}

func actionDeps(t *testing.T, ec2 cloud.EC2, exec seam.Runner) (seam.Deps, func()) {
	t.Helper()
	directory := t.TempDir()
	marker := filepath.Join(directory, "tracked")
	const contents = "unchanged\n"
	if err := os.WriteFile(marker, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	cloudCalls := 0
	deps := seam.Deps{
		Dir:  directory,
		Exec: exec,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			cloudCalls++
			if profile != "sandbox" {
				t.Fatalf("cloud profile = %q", profile)
			}
			if cloudCalls == 1 {
				if region != "" {
					t.Fatalf("bootstrap region = %q", region)
				}
				return cloud.Clients{SSM: &actionSSM{t: t, properties: actionProperties}}, nil
			}
			if cloudCalls == 2 {
				if region != "us-east-2" {
					t.Fatalf("account region = %q", region)
				}
				return guardedClients(t, ec2), nil
			}
			t.Fatalf("Cloud calls = %d", cloudCalls)
			return cloud.Clients{}, nil
		},
	}
	return deps, func() {
		t.Helper()
		if cloudCalls != 2 {
			t.Fatalf("Cloud calls = %d, want 2", cloudCalls)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 1 || entries[0].Name() != "tracked" {
			t.Fatalf("checkout entries = %v, %v", entries, err)
		}
		after, err := entries[0].Info()
		if err != nil || after.Size() != int64(len(contents)) || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("checkout marker changed: before=%v after=%v err=%v", before, after, err)
		}
	}
}

func guardedClients(t *testing.T, ec2 cloud.EC2) cloud.Clients {
	t.Helper()
	return cloud.Clients{
		EC2: ec2, SSM: &actionSSM{t: t}, Route53: &actionRoute53{t: t},
		S3: &actionS3{t: t}, IAM: &actionIAM{t: t}, STS: &actionSTS{t: t},
	}
}

type actionEC2 struct {
	t         *testing.T
	instances []cloud.Instance
	listErr   error
	calls     []string
}

func newActionEC2(t *testing.T, instances []cloud.Instance) *actionEC2 {
	return &actionEC2{t: t, instances: instances}
}

func (f *actionEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	f.calls = append(f.calls, "ListSpaceInstances")
	return f.instances, f.listErr
}
func (f *actionEC2) DescribeInstance(_ context.Context, id string) (cloud.Instance, error) {
	f.calls = append(f.calls, "DescribeInstance "+id)
	return cloud.Instance{ID: id, State: cloud.StateStopped}, nil
}
func (f *actionEC2) StopInstance(_ context.Context, id string) error {
	f.calls = append(f.calls, "StopInstance "+id)
	return nil
}
func (f *actionEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	f.t.Fatal("unexpected EC2 mutation: RunInstance")
	return cloud.Instance{}, nil
}
func (f *actionEC2) StartInstance(context.Context, string) error {
	f.t.Fatal("unexpected EC2 mutation: StartInstance")
	return nil
}
func (f *actionEC2) TerminateInstance(context.Context, string) error {
	f.t.Fatal("unexpected EC2 mutation: TerminateInstance")
	return nil
}
func (f *actionEC2) InstanceChecksPassed(context.Context, string) (bool, error) {
	f.t.Fatal("unexpected EC2 read: InstanceChecksPassed")
	return false, nil
}
func (f *actionEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) {
	f.t.Fatal("unexpected EC2 read: ListSpaceAddresses")
	return nil, nil
}
func (f *actionEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	f.t.Fatal("unexpected EC2 mutation: AllocateAddress")
	return cloud.Address{}, nil
}
func (f *actionEC2) AssociateAddress(context.Context, string, string) error {
	f.t.Fatal("unexpected EC2 mutation: AssociateAddress")
	return nil
}
func (f *actionEC2) DisassociateAddress(context.Context, string) error {
	f.t.Fatal("unexpected EC2 mutation: DisassociateAddress")
	return nil
}
func (f *actionEC2) ReleaseAddress(context.Context, string) error {
	f.t.Fatal("unexpected EC2 mutation: ReleaseAddress")
	return nil
}

type actionSSM struct {
	t          *testing.T
	properties string
}

func (f *actionSSM) GetParameter(_ context.Context, name string) (string, error) {
	if f.properties == "" || name != account.PropertiesParameter {
		f.t.Fatalf("unexpected SSM GetParameter(%q)", name)
	}
	return f.properties, nil
}
func (f *actionSSM) PutSecureParameter(context.Context, string, string) error {
	f.t.Fatal("unexpected SSM mutation: PutSecureParameter")
	return nil
}
func (f *actionSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	f.t.Fatal("unexpected SSM read: ListParameters")
	return nil, nil
}
func (f *actionSSM) DeleteParameter(context.Context, string) error {
	f.t.Fatal("unexpected SSM mutation: DeleteParameter")
	return nil
}

type actionRoute53 struct{ t *testing.T }

func (f *actionRoute53) ListZones(context.Context) ([]cloud.Zone, error) {
	f.t.Fatal("unexpected Route53 read: ListZones")
	return nil, nil
}
func (f *actionRoute53) ListRecords(context.Context, string) ([]cloud.Record, error) {
	f.t.Fatal("unexpected Route53 read: ListRecords")
	return nil, nil
}
func (f *actionRoute53) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	f.t.Fatal("unexpected Route53 mutation: ChangeRecords")
	return "", nil
}
func (f *actionRoute53) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	f.t.Fatal("unexpected Route53 read: ChangeStatus")
	return "", nil
}

type actionS3 struct{ t *testing.T }

func (f *actionS3) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	f.t.Fatal("unexpected S3 read: ListObjects")
	return nil, nil
}
func (f *actionS3) PutObject(context.Context, string, string, io.Reader, int64) error {
	f.t.Fatal("unexpected S3 mutation: PutObject")
	return nil
}
func (f *actionS3) DeleteObjects(context.Context, string, []string) error {
	f.t.Fatal("unexpected S3 mutation: DeleteObjects")
	return nil
}

type actionIAM struct{ t *testing.T }

func (f *actionIAM) unexpected(name string) { f.t.Fatal("unexpected IAM operation: " + name) }
func (f *actionIAM) RoleExists(context.Context, string) (bool, error) {
	f.unexpected("RoleExists")
	return false, nil
}
func (f *actionIAM) CreateRole(context.Context, cloud.RoleSpec) error {
	f.unexpected("CreateRole")
	return nil
}
func (f *actionIAM) PutRolePolicy(context.Context, string, string, string) error {
	f.unexpected("PutRolePolicy")
	return nil
}
func (f *actionIAM) DeleteRolePolicy(context.Context, string, string) error {
	f.unexpected("DeleteRolePolicy")
	return nil
}
func (f *actionIAM) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	f.unexpected("InstanceProfileRoles")
	return nil, false, nil
}
func (f *actionIAM) CreateInstanceProfile(context.Context, string) error {
	f.unexpected("CreateInstanceProfile")
	return nil
}
func (f *actionIAM) AddRoleToInstanceProfile(context.Context, string, string) error {
	f.unexpected("AddRoleToInstanceProfile")
	return nil
}
func (f *actionIAM) RemoveRoleFromInstanceProfile(context.Context, string, string) error {
	f.unexpected("RemoveRoleFromInstanceProfile")
	return nil
}
func (f *actionIAM) DeleteInstanceProfile(context.Context, string) error {
	f.unexpected("DeleteInstanceProfile")
	return nil
}
func (f *actionIAM) DeleteRole(context.Context, string) error {
	f.unexpected("DeleteRole")
	return nil
}

type actionSTS struct{ t *testing.T }

func (f *actionSTS) CallerAccountID(context.Context) (string, error) {
	f.t.Fatal("unexpected STS read: CallerAccountID")
	return "", nil
}
