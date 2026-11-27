package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/access"
	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

const (
	backupKeyMarker     = "ACCESSKYACCESSKYACCESSKY"
	backupSecretMarker  = "SECRETKYSECRETKYSECRETKY"
	backupSessionMarker = "TOKENSESTOKENSESTOKENSES"
	backupAccountMarker = "ACCTFAILACCTFAILACCTFAIL"
	backupHostMarker    = "HOSTFAILHOSTFAILHOSTFAIL"
	backupDBPathMarker  = "DATABASEDATABASEDATABASE"
	backupDBErrorMarker = "DBFAILEDDBFAILEDDBFAILED"
	backupFileMarker    = "FILEPATHFILEPATHFILEPATH"
	backupCopyMarker    = "COPYPATHCOPYPATHCOPYPATH"
	backupRestoreMarker = "RESTFAILRESTFAILRESTFAIL"
	finalAccountMarker  = "ACCTLOCKACCTLOCKACCTLOCK"
	finalHostMarker     = "HOSTLOCKHOSTLOCKHOSTLOCK"
	replaceAcctMarker   = "REPLACEDREPLACEDREPLACED"
	replaceHostMarker   = "WHOLEKEYWHOLEKEYWHOLEKEY"
)

type backupSecrecyCloud struct {
	boxCloud
	now func() time.Time
}

func (c backupSecrecyCloud) AssumeRole(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
	return cloud.Credentials(backupSecretCredentials(r.SessionName, c.now())), nil
}

func (backupSecrecyCloud) HasKeys(context.Context, string, string) (bool, error) {
	return false, nil
}

func backupSecretCredentials(session string, at time.Time) protocol.Credentials {
	return protocol.Credentials{
		AccessKeyID: backupKeyMarker + "-" + session, SecretAccessKey: backupSecretMarker + "-" + session,
		SessionToken: backupSessionMarker + "-" + session, Expires: at.Add(time.Hour),
	}
}

type backupSecrecyDaemon struct {
	run       *integrationRun
	clock     *drivenClock
	signature int
	region    string
	host      string
}

func newBackupSecrecyRun(t *testing.T) *backupSecrecyDaemon {
	t.Helper()
	clock := newDrivenClock()
	hosts := settings.SandboxHosts
	hosts.Domain = "hosts.one.test"
	mc, err := memory.New(memory.Config{
		Path: filepath.Join(t.TempDir(), "cloud.json"), Hosts: hosts, Now: clock.now, Rand: &integrationRandom{},
	})
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "accountsd"), []byte("daemon fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "services")
	writeAccountsURL(t, path, "https://accounts.one.test")
	run := startIntegration(t, integrationOptions{
		now: clock.now, after: clock.after, sleep: clock.sleep, noSandbox: true, path: path, env: hostEnvironment(hosts),
		random: &secrecyRandom{}, email: emailMarker + "@fixture.test",
		executable: func() (string, error) { return filepath.Join(bin, "accounts"), nil },
		cloud: func(context.Context, string) (cloud.Cloud, error) {
			return backupSecrecyCloud{boxCloud{mc}, clock.now}, nil
		},
	})
	var created tools.HostChange
	run.call(tools.ToolCreateHost, struct{}{}, &created)
	return &backupSecrecyDaemon{run: run, clock: clock, region: hosts.Region, host: created.Host}
}

func (d *backupSecrecyDaemon) request(method, path string, current tools.HostRead, body io.Reader, status int) *http.Response {
	t := d.run.t
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, d.run.base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	d.signature++
	request.Header.Set(hostauth.HeaderName, fakeHostIdentity(d.region, current.InstanceID, d.clock.now(), d.signature))
	if body != nil {
		request.Header.Set("Content-Type", protocol.ContentJSON)
	}
	response, err := d.run.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		_ = response.Body.Close()
		t.Fatalf("%s %s for %s in %s answered %d, want %d", method, path, current.InstanceID, current.State, response.StatusCode, status)
	}
	return response
}

func (d *backupSecrecyDaemon) report(current tools.HostRead, report protocol.Report) {
	t := d.run.t
	t.Helper()
	report.Protocol, report.Attempted, report.Applied = protocol.Version, current.Version, current.Version
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	response := d.request(http.MethodPost, protocol.ReportPath, current, bytes.NewReader(data), http.StatusNoContent)
	_ = response.Body.Close()
}

func (d *backupSecrecyDaemon) drive(retired string, done func(tools.HostRead) bool) tools.HostRead {
	d.run.t.Helper()
	var current tools.HostRead
	driveUntil(d.run.t, d.clock, func() bool {
		d.run.call(tools.ToolHost, tools.HostArgs{Host: d.host}, &current)
		if done(current) {
			return true
		}
		if current.InstanceID != "" && current.InstanceID != retired {
			d.report(current, protocol.Report{})
		}
		return false
	})
	return current
}

func (d *backupSecrecyDaemon) checkCredentials(current tools.HostRead, account string) {
	t := d.run.t
	t.Helper()
	response := d.request(http.MethodGet, protocol.StatePath, current, nil, http.StatusOK)
	defer func() { _ = response.Body.Close() }()
	state, err := protocol.DecodeState(response.Body)
	if err != nil || state.Backup == nil {
		t.Fatalf("state omitted backup configuration: %v", err)
	}
	roles := state.Backup.Accounts
	if state.Backup.Host != backupSecretCredentials(access.SessionName(d.host, ""), d.clock.now()) ||
		len(roles) != 1 || roles[0].Account != account ||
		roles[0].Credentials != backupSecretCredentials(access.SessionName(d.host, account), d.clock.now()) {
		t.Fatal("state did not carry the issued host and account credentials")
	}
}

// R-AG9R-XYHR R-KZBC-ZF9T
func TestRunAWSBackupSecrecy(t *testing.T) {
	const account = "acme"
	d := newBackupSecrecyRun(t)
	run := d.run
	run.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Host: d.host, Account: account}, nil)
	current := d.drive("", func(h tools.HostRead) bool {
		var a tools.AccountRead
		run.call(tools.ToolAccount, tools.AccountArgs{Account: account}, &a)
		return a.State == store.AccountActive && h.State == store.HostRunning
	})
	d.checkCredentials(current, account)
	database, files := d.failFileRestore(current, account)
	closeID, replaceID := d.failHostBackups(account)
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	events := run.capture.Events()
	assertBackupTrailSecrecy(t, events)
	for _, op := range []struct{ id, kind, account, status, reason string }{
		{database, store.OpRestore, account, store.OpDone, ""},
		{files, store.OpRestore, account, store.OpFailed, ops.ReasonRestoreFailed},
		{closeID, store.OpClose, "", store.OpFailed, ops.ReasonFinalFailed},
		{replaceID, store.OpReplace, "", store.OpFailed, ops.ReasonRestoreFailed},
	} {
		var finished int
		for _, event := range events {
			if event.Name != trail.EventOperationFinished || event.Attrs["operation"] != op.id {
				continue
			}
			finished++
			if event.Attrs["kind"] != op.kind || event.Attrs["host"] != d.host || event.Attrs["account"] != op.account ||
				event.Attrs["status"] != op.status || event.Attrs["reason"] != op.reason {
				t.Errorf("%s operation %s finished with unexpected attributes", op.kind, op.id)
			}
		}
		if finished != 1 {
			t.Errorf("%s operation %s has %d finished events, want 1", op.kind, op.id, finished)
		}
	}
}

func (d *backupSecrecyDaemon) failFileRestore(current tools.HostRead, account string) (string, string) {
	t := d.run.t
	t.Helper()
	snapshot := d.clock.now()
	dbPath := "data/" + backupDBPathMarker + ".db"
	// The database must have synced at least once to accept this as-of time.
	d.report(current, protocol.Report{
		Backups: []protocol.AccountBackup{{Account: account, Error: backupAccountMarker, Snapshots: []int64{snapshot.Unix()},
			Databases: []protocol.Database{{Path: dbPath, Oldest: snapshot, Error: backupDBErrorMarker}}}},
		HostBackup: protocol.HostBackup{Error: backupHostMarker},
	})
	var database, files tools.RestoreMade
	stamp := snapshot.Format(time.RFC3339)
	d.run.call(tools.ToolRestoreDatabase, tools.RestoreDatabaseArgs{Account: account, Path: dbPath, AsOf: stamp}, &database)
	filePath := backupFileMarker
	d.run.call(tools.ToolRestoreFiles, tools.RestoreFilesArgs{Account: account, Path: &filePath, Snapshot: stamp}, &files)
	if database.Restore == "" || files.Restore == "" || database.Restore == files.Restore ||
		database.Kind != protocol.RestoreDatabase || files.Kind != protocol.RestoreFiles {
		t.Fatal("the tools did not create the two distinct restores")
	}
	d.run.call(tools.ToolHost, tools.HostArgs{Host: d.host}, &current)
	d.report(current, protocol.Report{Restores: []protocol.RestoreResult{
		{ID: database.Restore, State: protocol.StepDone, Copy: backupCopyMarker},
		{ID: files.Restore, State: protocol.StepFailed, Error: backupRestoreMarker},
	}})
	d.drive("", func(tools.HostRead) bool {
		events := d.run.capture.Events()
		return finishedBackupOperation(events, database.Restore) && finishedBackupOperation(events, files.Restore)
	})
	return database.Restore, files.Restore
}

func (d *backupSecrecyDaemon) failHostBackups(account string) (string, string) {
	t := d.run.t
	t.Helper()
	args := tools.HostArgs{Host: d.host}
	var closed, replaced tools.HostChange
	d.run.call(tools.ToolCloseHost, args, &closed)
	if closed.Operation == "" {
		t.Fatal("close did not start an operation")
	}
	var current tools.HostRead
	d.run.call(tools.ToolHost, args, &current)
	final := protocol.Step{ID: closed.Operation, State: protocol.StepFailed, Code: protocol.CodeFailed}
	d.report(current, protocol.Report{
		Backups:    []protocol.AccountBackup{{Account: account, Final: final, Error: finalAccountMarker}},
		HostBackup: protocol.HostBackup{Final: final, Error: finalHostMarker},
	})
	current = d.drive("", func(h tools.HostRead) bool { return h.State == store.HostFailed })
	retired := current.InstanceID
	d.run.call(tools.ToolReplaceHost, tools.ReplaceHostArgs{Host: d.host, From: store.ReplaceBackup}, &replaced)
	if replaced.Operation == "" || replaced.Operation == closed.Operation {
		t.Fatal("replace did not start a new operation")
	}
	current = d.drive(retired, func(h tools.HostRead) bool { return h.State == store.HostRestoring })
	if current.InstanceID == "" || current.InstanceID == retired {
		t.Fatal("replacement did not launch a new instance")
	}
	restored := protocol.Step{ID: replaced.Operation, State: protocol.StepFailed, Code: protocol.CodeFailed}
	d.report(current, protocol.Report{
		Backups:    []protocol.AccountBackup{{Account: account, Restored: restored, Error: replaceAcctMarker}},
		HostBackup: protocol.HostBackup{Restored: restored, Error: replaceHostMarker},
	})
	d.drive(retired, func(h tools.HostRead) bool { return h.State == store.HostFailed })
	return closed.Operation, replaced.Operation
}

func finishedBackupOperation(events []telemetry.Event, id string) bool {
	return slices.ContainsFunc(events, func(event telemetry.Event) bool {
		return event.Name == trail.EventOperationFinished && event.Attrs["operation"] == id
	})
}

func assertBackupTrailSecrecy(t *testing.T, events []telemetry.Event) {
	t.Helper()
	assertTrailSecrecy(t, events)
	markers := []string{
		backupKeyMarker, backupSecretMarker, backupSessionMarker, backupAccountMarker, backupHostMarker,
		backupDBPathMarker, backupDBErrorMarker, backupFileMarker, backupCopyMarker, backupRestoreMarker,
		finalAccountMarker, finalHostMarker, replaceAcctMarker, replaceHostMarker,
	}
	for i, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		raw, wire := strings.ToUpper(fmt.Sprint(event)), strings.ToUpper(string(encoded))
		for _, marker := range markers {
			if strings.Contains(raw, marker) || strings.Contains(wire, marker) {
				t.Fatalf("backup secret appeared in captured event %d (%s)", i, event.Name)
			}
		}
	}
}
