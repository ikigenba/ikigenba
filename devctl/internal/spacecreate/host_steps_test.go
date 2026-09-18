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
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	hostStepDomain  = "foo.sbx.ikigenba.dev"
	hostStepAddress = "3.19.79.227"
	hostStepID      = "i-0c9e94542d98846a8"
)

func TestRunHostStepsReadinessInstallAndEmptyRestore(t *testing.T) {
	// R-YMOE-CCEO R-EDKC-O6NQ R-EES9-1YEF
	harness := newHostStepHarness(false, nil)
	var stdout bytes.Buffer
	err := RunHostSteps(context.Background(), &stdout, harness.deps(), harness.account(), hostStepDomain,
		hostStepAddress, hostStepID, cloud.Zone{ID: "Z02587302QXWONVKW632", Name: "sbx.ikigenba.dev"}, "ops@ikigenba.dev")
	if err != nil {
		t.Fatalf("RunHostSteps(): %v", err)
	}
	wantOutput := "host: ok (status checks passed, cloud-init done)\n" +
		"opsctl: ok (v9.8.7 installed, 10 keys set)\n" +
		"restore: ok (no host backup)\n" +
		"init: ok\n"
	if stdout.String() != wantOutput {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantOutput)
	}
	joined := strings.Join(harness.operations, "\n")
	for _, want := range []string{
		"checks " + hostStepID,
		"ssh 'true'",
		"ssh 'sudo' 'cloud-init' 'status' '--wait'",
		"ssh 'sudo' 'sh' '-c'",
		"list account-backups " + hostStepDomain + "/host/",
		"ssh 'sudo' 'opsctl' 'init'",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("operations missing %q:\n%s", want, joined)
		}
	}
	if countOperations(harness.operations, "'config' 'set'") != 10 {
		t.Fatalf("configuration calls = %d, want 10", countOperations(harness.operations, "'config' 'set'"))
	}
	if countOperations(harness.operations, "'opsctl' 'host' 'restore'") != 0 {
		t.Fatalf("restore calls = %#v, want none", harness.operations)
	}
	wantListing := "list account-backups " + hostStepDomain + "/host/"
	if countOperations(harness.operations, "list ") != 1 || countExactOperations(harness.operations, wantListing) != 1 {
		t.Fatalf("backup listings = %#v, want exactly one %q", harness.operations, wantListing)
	}
	listingAt := operationIndex(harness.operations, wantListing)
	lastConfigAt := operationLastIndex(harness.operations, "'config' 'set'")
	initAt := operationIndex(harness.operations, "'opsctl' 'init'")
	if listingAt <= lastConfigAt || initAt <= listingAt {
		t.Fatalf("backup listing order = %#v", harness.operations)
	}
	if countOperations(harness.operations, "'cloud-init' 'status' '--wait'") != 1 ||
		countOperations(harness.operations, "ssh 'true'") != 1 {
		t.Fatalf("host readiness operations = %#v", harness.operations)
	}
}

func TestRunHostStepsReportsRestoreOnlyAfterRestoreAndReconfigure(t *testing.T) {
	// R-93SF-DMH9
	tests := []struct {
		name            string
		configureFailAt int
		restoreFails    bool
		wantConfigCalls int
	}{
		{name: "restore fails", restoreFails: true, wantConfigCalls: 10},
		{name: "reconfigure fails", configureFailAt: 11, wantConfigCalls: 11},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newHostStepHarness(false, []cloud.Object{{Key: hostStepDomain + "/host/a.tar.zst"}})
			harness.failRestore = test.restoreFails
			harness.failConfigureAt = test.configureFailAt
			var stdout bytes.Buffer
			err := RunHostSteps(context.Background(), &stdout, harness.deps(), harness.account(), hostStepDomain,
				hostStepAddress, hostStepID, cloud.Zone{ID: "ZONE1", Name: "sbx.ikigenba.dev"}, "ops@ikigenba.dev")
			if err == nil {
				t.Fatal("RunHostSteps() error = nil")
			}
			if strings.Contains(stdout.String(), "restore: ok") {
				t.Fatalf("failed restore work was reported complete: %q", stdout.String())
			}
			if countOperations(harness.operations, "'opsctl' 'init'") != 0 {
				t.Fatalf("init followed failed restore work: %#v", harness.operations)
			}
			if countOperations(harness.operations, "'opsctl' 'host' 'restore'") != 1 ||
				harness.configureCalls != test.wantConfigCalls {
				t.Fatalf("restore/configure calls = %#v", harness.operations)
			}
		})
	}
}

func TestRunHostStepsRestoresSelectedBackupAndReconfigures(t *testing.T) {
	// R-93SF-DMH9
	objects := []cloud.Object{{Key: hostStepDomain + "/host/not-the-selected-backup.tar.zst"}}
	harness := newHostStepHarness(false, objects)
	var stdout bytes.Buffer
	err := RunHostSteps(context.Background(), &stdout, harness.deps(), harness.account(), hostStepDomain,
		hostStepAddress, hostStepID, cloud.Zone{ID: "ZONE1", Name: "sbx.ikigenba.dev"}, "ops@ikigenba.dev")
	if err != nil {
		t.Fatalf("RunHostSteps(): %v", err)
	}
	const selected = "host/2026-09-12T14:22:51Z.tar.zst"
	if !strings.Contains(stdout.String(), "restore: ok ("+selected+", 10 keys set again)\n") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "not-the-selected") {
		t.Fatalf("restore report used listing instead of restore result: %q", stdout.String())
	}
	if countOperations(harness.operations, "'opsctl' 'host' 'restore'") != 1 ||
		countOperations(harness.operations, "'config' 'set'") != 20 {
		t.Fatalf("restore/configure operations = %#v", harness.operations)
	}
	restoreAt := operationIndex(harness.operations, "'opsctl' 'host' 'restore'")
	lastConfigAt := operationLastIndex(harness.operations, "'config' 'set'")
	if restoreAt < 0 || lastConfigAt <= restoreAt {
		t.Fatalf("configuration was not reapplied after restore: %#v", harness.operations)
	}
}

func TestRunHostStepsOmitsRestoreWhenBackupsAreDeleted(t *testing.T) {
	// R-EH81-THVT
	harness := newHostStepHarness(true, []cloud.Object{{Key: "must-not-be-read"}})
	var stdout bytes.Buffer
	if err := RunHostSteps(context.Background(), &stdout, harness.deps(), harness.account(), hostStepDomain,
		hostStepAddress, hostStepID, cloud.Zone{ID: "ZONE1", Name: "sbx.ikigenba.dev"}, "ops@ikigenba.dev"); err != nil {
		t.Fatalf("RunHostSteps(): %v", err)
	}
	joined := strings.Join(harness.operations, "\n")
	if strings.Contains(joined, "list ") || strings.Contains(joined, "'host' 'restore'") ||
		strings.Contains(stdout.String(), "restore: ok") {
		t.Fatalf("restore work occurred with deletion policy: operations=%q stdout=%q", joined, stdout.String())
	}
}

func TestRunHostStepsReturnsInitFailureWithoutReportingInit(t *testing.T) {
	// R-YSRW-9745
	harness := newHostStepHarness(true, nil)
	harness.failInit = true
	var stdout bytes.Buffer
	err := RunHostSteps(context.Background(), &stdout, harness.deps(), harness.account(), hostStepDomain,
		hostStepAddress, hostStepID, cloud.Zone{ID: "ZONE1", Name: "sbx.ikigenba.dev"}, "ops@ikigenba.dev")
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || err.Error() != "init: ssh ec2-user@3.19.79.227 sudo opsctl init: exit status 2" {
		t.Fatalf("RunHostSteps() error = %#v", err)
	}
	if reflect.ValueOf(err).Pointer() != reflect.ValueOf(commandErr).Pointer() {
		t.Fatalf("RunHostSteps() wrapped init error: returned %#v, command error %#v", err, commandErr)
	}
	if strings.Contains(stdout.String(), "init: ok") {
		t.Fatalf("failed init was reported complete: %q", stdout.String())
	}
	if countOperations(harness.operations, "'opsctl' 'init'") != 1 {
		t.Fatalf("init calls = %#v, want exactly one", harness.operations)
	}
	if operationLastIndex(harness.operations, "'opsctl' 'init'") != len(harness.operations)-1 {
		t.Fatalf("init was not the final operation: %#v", harness.operations)
	}
}

type hostStepHarness struct {
	deleteBackups   bool
	objects         []cloud.Object
	operations      []string
	failInit        bool
	failRestore     bool
	failConfigureAt int
	configureCalls  int
}

func newHostStepHarness(deleteBackups bool, objects []cloud.Object) *hostStepHarness {
	return &hostStepHarness{deleteBackups: deleteBackups, objects: objects}
}

func (h *hostStepHarness) deps() seam.Deps {
	return seam.Deps{Dir: ".", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		logical := command.Args[len(command.Args)-1]
		h.operations = append(h.operations, "ssh "+logical)
		switch {
		case strings.Contains(logical, "opsctl-install.sh"):
			return seam.Result{Stdout: []byte("opsctl v9.8.7\n")}, nil
		case strings.Contains(logical, "'config' 'set'"):
			h.configureCalls++
			if h.configureCalls == h.failConfigureAt {
				return seam.Result{ExitCode: 4}, nil
			}
			return seam.Result{}, nil
		case strings.Contains(logical, "'opsctl' 'host' 'restore'"):
			if h.failRestore {
				return seam.Result{ExitCode: 3}, nil
			}
			return seam.Result{Stdout: []byte(hostStepDomain + "/host/2026-09-12T14:22:51Z.tar.zst\n")}, nil
		case h.failInit && strings.Contains(logical, "'opsctl' 'init'"):
			return seam.Result{ExitCode: 2}, nil
		default:
			return seam.Result{}, nil
		}
	}}
}

func (h *hostStepHarness) account() *account.Account {
	return &account.Account{
		Properties: account.Properties{
			Region:                    "us-east-2",
			BackupBucket:              "account-backups",
			DeleteBackupsOnDestroy:    h.deleteBackups,
			BackupHostFilesSeconds:    11,
			BackupServiceFilesSeconds: 22,
			BackupServiceDBSeconds:    33,
			BackupServiceWALSeconds:   44,
		},
		Clients: cloud.Clients{EC2: hostStepEC2{h}, S3: hostStepS3{h}},
	}
}

type hostStepEC2 struct{ h *hostStepHarness }

func (f hostStepEC2) InstanceChecksPassed(_ context.Context, id string) (bool, error) {
	f.h.operations = append(f.h.operations, "checks "+id)
	return true, nil
}
func (hostStepEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) { return nil, nil }
func (hostStepEC2) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (hostStepEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (hostStepEC2) StartInstance(context.Context, string) error                 { return nil }
func (hostStepEC2) StopInstance(context.Context, string) error                  { return nil }
func (hostStepEC2) TerminateInstance(context.Context, string) error             { return nil }
func (hostStepEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) { return nil, nil }
func (hostStepEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	return cloud.Address{}, nil
}
func (hostStepEC2) AssociateAddress(context.Context, string, string) error { return nil }
func (hostStepEC2) DisassociateAddress(context.Context, string) error      { return nil }
func (hostStepEC2) ReleaseAddress(context.Context, string) error           { return nil }

type hostStepS3 struct{ h *hostStepHarness }

func (f hostStepS3) ListObjects(_ context.Context, bucket, prefix string) ([]cloud.Object, error) {
	f.h.operations = append(f.h.operations, fmt.Sprintf("list %s %s", bucket, prefix))
	return f.h.objects, nil
}
func (hostStepS3) PutObject(context.Context, string, string, io.Reader, int64) error {
	return nil
}
func (hostStepS3) DeleteObjects(context.Context, string, []string) error { return nil }

func countOperations(operations []string, contains string) int {
	count := 0
	for _, operation := range operations {
		if strings.Contains(operation, contains) {
			count++
		}
	}
	return count
}

func countExactOperations(operations []string, want string) int {
	count := 0
	for _, operation := range operations {
		if operation == want {
			count++
		}
	}
	return count
}

func operationIndex(operations []string, contains string) int {
	for index, operation := range operations {
		if strings.Contains(operation, contains) {
			return index
		}
	}
	return -1
}

func operationLastIndex(operations []string, contains string) int {
	for index := len(operations) - 1; index >= 0; index-- {
		if strings.Contains(operations[index], contains) {
			return index
		}
	}
	return -1
}
