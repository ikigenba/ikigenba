package linux_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func (f *hardeningFixture) stream(state protocol.DesiredState) {
	f.t.Helper()
	if err := f.host.Stream(f.t.Context(), state); err != nil {
		f.t.Fatalf("Stream: %v", err)
	}
}

func (f *hardeningFixture) applyCloseLock(state protocol.DesiredState) {
	f.t.Helper()
	// Close's lock is recorded before data volume reconciliation.
	state.Accounts = nil
	f.apply(state)
}

func TestCloseFinalsWaitForNewSyncAndKeepVersionedResults(t *testing.T) {
	// R-UMZY-6NQF R-UPFQ-Y77T R-URVJ-PQP7
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	f.put(dataPrivateKey, "private", 0o600)
	f.put(dataPublicKey, "public", 0o644)
	f.put("/srv/acme/data/app.db", "SQLite format 3\x00\x00\x00\x02\x02", 0o600)
	state := installTestState()
	state.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: state.Backup.Host}}

	now := restoreAsOf
	f.env.Now = func() time.Time { return now }
	var snapshots []string
	failAccount := false
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.LitestreamPath) {
			switch {
			case c.Args[0] == "status":
				restoreStdout(t, c, `[{"database":"/srv/acme/data/app.db","status":"ok","local_txid":"0000000000000002"}]`)
			case c.Args[0] == "ltx" && c.Args[5] == "0":
				restoreStdout(t, c, `[{"timestamp":"2026-11-18T12:00:00Z","max_txid":"0000000000000002"}]`)
			default:
				restoreStdout(t, c, "[]")
			}
			return linux.Result{}, nil
		}
		switch c.Args[3] {
		case "backup":
			slug := c.Args[6]
			snapshots = append(snapshots, slug)
			if failAccount && slug == "acme" {
				return linux.Result{ExitCode: 1, Stderr: []byte("final snapshot failure\n")}, nil
			}
			restoreStdout(t, c, `{"message_type":"summary","snapshot_id":"copy","backup_end":`+strconv.Quote(now.Format(time.RFC3339Nano))+"}\n")
		case "snapshots":
			restoreStdout(t, c, "[]")
		}
		return linux.Result{}, nil
	})
	f.stream(state)
	f.backup(state)
	if !slices.Equal(snapshots, []string{"acme", state.Host}) {
		t.Fatalf("initial scheduled snapshots = %v", snapshots)
	}
	snapshots = nil
	state.Backup.Close = "close_one"
	state.Version = 5
	pending := protocol.Step{ID: state.Backup.Close, State: protocol.StepPending}
	now = now.Add(time.Minute)
	f.backup(state)
	if report := f.backupReport(state); report.Backups[0].Final != pending || report.HostBackup.Final != pending {
		t.Fatalf("unlocked close report %+v", report)
	}
	f.applyCloseLock(state)
	f.stream(state)
	f.backup(state)
	if report := f.backupReport(state); report.Backups[0].Final != pending || report.HostBackup.Final != pending || len(snapshots) != 0 {
		t.Fatalf("same-time sync finished close: %v snapshots, %+v", snapshots, report)
	}

	now = now.Add(time.Second)
	f.stream(state)
	f.host = linux.New(f.env)
	now = now.Add(linux.FinalSyncTimeout)
	f.backup(state)
	if report := f.backupReport(state); report.Backups[0].Final != pending || report.HostBackup.Final != pending || len(snapshots) != 0 {
		t.Fatalf("restart discarded persisted sync: %v snapshots, %+v", snapshots, report)
	}
	f.stream(state)
	failAccount = true
	_ = f.host.Backup(t.Context(), state)
	wantAccount := protocol.Step{ID: state.Backup.Close, State: protocol.StepFailed, At: now, Code: protocol.CodeFailed}
	wantHost := protocol.Step{ID: state.Backup.Close, State: protocol.StepDone, At: now}
	report := f.backupReport(state)
	if report.Backups[0].Final != wantAccount || report.HostBackup.Final != wantHost ||
		!strings.Contains(report.Backups[0].Error, "final snapshot failure") || !slices.Equal(snapshots, []string{"acme", state.Host}) {
		t.Fatalf("failed account and completed host report %+v, commands %v", report, snapshots)
	}

	f.host = linux.New(f.env)
	f.backup(state)
	if got := f.backupReport(state); got.Backups[0].Final != wantAccount || got.HostBackup.Final != wantHost || len(snapshots) != 2 {
		t.Fatalf("same version retried final after restart: %v and %+v", snapshots, got)
	}

	state.Version++
	f.applyCloseLock(state)
	failAccount = false
	now = now.Add(time.Second)
	f.stream(state)
	f.backup(state)
	f.backup(state)
	if got := f.backupReport(state); got.HostBackup.Final != wantHost || got.Backups[0].Final != (protocol.Step{
		ID: state.Backup.Close, State: protocol.StepDone, At: now,
	}) || !slices.Equal(snapshots, []string{"acme", state.Host, "acme"}) {
		t.Fatalf("higher version report %+v, snapshots %v", got, snapshots)
	}
	state.Backup.Close = ""
	if got := f.backupReport(state); got.HostBackup.Final != (protocol.Step{}) || got.Backups[0].Final != (protocol.Step{}) {
		t.Fatalf("finished finals visible without a close: %+v", got)
	}
}

func TestCloseFinalTimeoutCountsFromLock(t *testing.T) {
	// R-UMZY-6NQF R-UPFQ-Y77T
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	f.put("/srv/acme/data/app.db", "SQLite format 3\x00\x00\x00\x02\x02", 0o600)
	state := installTestState()
	state.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: state.Backup.Host}}
	state.Accounts = append(state.Accounts, protocol.Account{Slug: "no-creds", GID: 20002, State: protocol.AccountActive})
	state.Backup.Host = protocol.Credentials{}
	state.Backup.Close = "close_one"
	state.Version = 2

	now := restoreAsOf
	f.env.Now = func() time.Time { return now }
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		switch {
		case c.Name == f.path(linux.LitestreamPath):
			restoreStdout(t, c, "[]")
		case c.Args[3] == "backup":
			// Ordinary scheduled snapshots cannot satisfy a final sync.
			restoreStdout(t, c, fmt.Sprintf(`{"message_type":"summary","snapshot_id":"scheduled","backup_end":%q}`+"\n", now.Format(time.RFC3339)))
		case c.Args[3] == "snapshots":
			restoreStdout(t, c, "[]")
		}
		return linux.Result{}, nil
	})
	f.applyCloseLock(state)
	f.stream(state)
	now = now.Add(linux.FinalSyncTimeout - time.Second)
	f.backup(state)
	wantPending := protocol.Step{ID: state.Backup.Close, State: protocol.StepPending}
	report := f.backupReport(state)
	if report.HostBackup.Final != wantPending || report.Backups[0].Final != wantPending || report.Backups[1].Final != wantPending {
		t.Fatalf("before timeout: %+v", report)
	}
	now = now.Add(time.Second)
	f.backup(state)
	wantFailed := protocol.Step{ID: state.Backup.Close, State: protocol.StepFailed, At: now, Code: protocol.CodeFailed}
	report = f.backupReport(state)
	if report.HostBackup.Final != wantFailed || report.HostBackup.Error != machine.NoCredentials ||
		report.Backups[0].Final != wantFailed || report.Backups[0].Error != machine.ErrNotSynced.Error() ||
		report.Backups[1].Final != wantFailed || report.Backups[1].Error != machine.NoCredentials {
		t.Fatalf("at timeout: %+v", report)
	}
}
