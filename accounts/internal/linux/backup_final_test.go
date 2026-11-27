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
	// R-WDVY-0I1K R-W6KJ-PVLE R-WBG5-8YK6
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	f.put(dataPrivateKey, "private", 0o600)
	f.put(dataPublicKey, "public", 0o644)
	f.put("/srv/acme/data/app.db", "SQLite format 3\x00\x00\x00\x02\x02", 0o600)
	f.put("/srv/acme/data/other.db", "SQLite format 3\x00\x00\x00\x02\x02", 0o600)
	f.put("/srv/beta/document.txt", "plain text", 0o600)
	state := installTestState()
	state.Accounts = append(state.Accounts, protocol.Account{Slug: "beta", GID: 20002, State: protocol.AccountActive})
	state.Backup.Accounts = []protocol.AccountCredentials{
		{Account: "acme", Credentials: state.Backup.Host},
		{Account: "beta", Credentials: state.Backup.Host},
	}

	now := restoreAsOf
	f.env.Now = func() time.Time { return now }
	var snapshots []string
	failAccount := false
	otherCaughtUp := true
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.LitestreamPath) {
			path := strings.TrimPrefix(c.Args[len(c.Args)-1], f.root)
			switch {
			case c.Args[0] == "status":
				restoreStdout(t, c, fmt.Sprintf(`[{"database":%q,"status":"ok","local_txid":"0000000000000002"}]`, path))
			case c.Args[0] == "ltx" && c.Args[5] == "0":
				maxTX := "0000000000000002"
				if path == "/srv/acme/data/other.db" && !otherCaughtUp {
					maxTX = "0000000000000001"
				}
				restoreStdout(t, c, fmt.Sprintf(`[{"timestamp":"2026-11-18T12:00:00Z","max_txid":%q}]`, maxTX))
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
	if !slices.Equal(snapshots, []string{"acme", "beta", state.Host}) {
		t.Fatalf("initial scheduled snapshots = %v", snapshots)
	}
	snapshots = nil
	state.Backup.Close = "close_one"
	state.Version = 5
	pending := protocol.Step{ID: state.Backup.Close, State: protocol.StepPending}
	now = now.Add(time.Minute)
	f.backup(state)
	if report := f.backupReport(state); report.Backups[0].Final != pending || report.Backups[1].Final != pending ||
		report.HostBackup.Final != pending || len(snapshots) != 0 {
		t.Fatalf("unlocked close report %+v", report)
	}
	f.applyCloseLock(state)
	f.stream(state)
	f.backup(state)
	wantBeta := protocol.Step{ID: state.Backup.Close, State: protocol.StepDone, At: now}
	if report := f.backupReport(state); report.Backups[0].Final != pending || report.Backups[1].Final != wantBeta ||
		report.HostBackup.Final != pending || !slices.Equal(snapshots, []string{"beta"}) {
		t.Fatalf("same-time sync finished close: %v snapshots, %+v", snapshots, report)
	}

	now = now.Add(time.Second)
	otherCaughtUp = false
	f.stream(state)
	f.backup(state)
	partial := f.backupReport(state)
	if dbs := partial.Backups[0].Databases; len(dbs) != 2 || dbs[0].Synced != now || dbs[1].Synced != wantBeta.At ||
		partial.Backups[0].Final != pending || partial.Backups[1].Final != wantBeta || partial.HostBackup.Final != pending ||
		!slices.Equal(snapshots, []string{"beta"}) {
		t.Fatalf("one synced database finished the account: %v snapshots, %+v", snapshots, partial)
	}
	otherCaughtUp = true
	f.stream(state)
	f.host = linux.New(f.env)
	now = now.Add(linux.FinalSyncTimeout)
	f.backup(state)
	if report := f.backupReport(state); report.Backups[0].Final != pending || report.HostBackup.Final != pending ||
		!slices.Equal(snapshots, []string{"beta"}) {
		t.Fatalf("restart discarded persisted sync: %v snapshots, %+v", snapshots, report)
	}
	f.stream(state)
	failAccount = true
	_ = f.host.Backup(t.Context(), state)
	wantAccount := protocol.Step{ID: state.Backup.Close, State: protocol.StepFailed, At: now, Code: protocol.CodeFailed}
	wantHost := protocol.Step{ID: state.Backup.Close, State: protocol.StepDone, At: now}
	report := f.backupReport(state)
	if report.Backups[0].Final != wantAccount || report.Backups[1].Final != wantBeta || report.HostBackup.Final != wantHost ||
		!strings.Contains(report.Backups[0].Error, "final snapshot failure") || !slices.Equal(snapshots, []string{"beta", "acme", state.Host}) {
		t.Fatalf("failed account and completed host report %+v, commands %v", report, snapshots)
	}

	f.host = linux.New(f.env)
	f.backup(state)
	if got := f.backupReport(state); got.Backups[0].Final != wantAccount || got.HostBackup.Final != wantHost || len(snapshots) != 3 {
		t.Fatalf("same version retried final after restart: %v and %+v", snapshots, got)
	}

	state.Version++
	f.applyCloseLock(state)
	failAccount = false
	now = now.Add(time.Second)
	f.stream(state)
	f.backup(state)
	f.backup(state)
	wantAccount = protocol.Step{ID: state.Backup.Close, State: protocol.StepDone, At: now}
	wantSnapshots := []string{"beta", "acme", state.Host, "acme"}
	if got := f.backupReport(state); got.HostBackup.Final != wantHost || got.Backups[0].Final != wantAccount || !slices.Equal(snapshots, wantSnapshots) {
		t.Fatalf("higher version report %+v, snapshots %v", got, snapshots)
	}
	f.host = linux.New(f.env)
	state.Version++
	f.applyCloseLock(state)
	now = now.Add(time.Second)
	f.stream(state)
	f.backup(state)
	if got := f.backupReport(state); got.HostBackup.Final != wantHost || got.Backups[0].Final != wantAccount || !slices.Equal(snapshots, wantSnapshots) {
		t.Fatalf("higher version retried completed finals after restart: %+v, snapshots %v", got, snapshots)
	}
	state.Backup.Close = "close_other"
	pending = protocol.Step{ID: state.Backup.Close, State: protocol.StepPending}
	f.backup(state)
	if got := f.backupReport(state); got.HostBackup.Final != pending || got.Backups[0].Final != pending ||
		got.Backups[1].Final != pending || !slices.Equal(snapshots, wantSnapshots) {
		t.Fatalf("new close used the previous close's lock or finals: %+v, snapshots %v", got, snapshots)
	}
	noBackup := state
	noBackup.Backup = nil
	if got := f.backupReport(noBackup); got.HostBackup.Final != (protocol.Step{}) || got.Backups[0].Final != (protocol.Step{}) {
		t.Fatalf("finished finals visible without a backup member: %+v", got)
	}
	state.Backup.Close = ""
	if got := f.backupReport(state); got.HostBackup.Final != (protocol.Step{}) || got.Backups[0].Final != (protocol.Step{}) {
		t.Fatalf("finished finals visible without a close: %+v", got)
	}
}

func TestCloseFinalRunsForAccountWithoutStreamedDatabases(t *testing.T) {
	// R-WDVY-0I1K
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	f.put("/srv/acme/document.txt", "plain text", 0o600)
	state := installTestState()
	state.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: state.Backup.Host}}
	state.Backup.Host = protocol.Credentials{}
	now := restoreAsOf.In(time.FixedZone("fixture", -5*60*60)).Add(500 * time.Millisecond)
	f.env.Now = func() time.Time { return now }
	var snapshots []string
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.LitestreamPath) {
			t.Fatalf("empty database walk ran Litestream: %v", c.Args)
		}
		switch c.Args[3] {
		case "backup":
			snapshots = append(snapshots, c.Args[6])
			restoreStdout(t, c, fmt.Sprintf(`{"message_type":"summary","snapshot_id":"copy","backup_end":%q}`+"\n", now.Format(time.RFC3339Nano)))
		case "snapshots":
			restoreStdout(t, c, "[]")
		}
		return linux.Result{}, nil
	})
	f.stream(state)
	f.backup(state)
	initial := f.backupReport(state).Backups[0]
	if initial.Snapshot != now.UTC().Truncate(time.Second) || !slices.Equal(snapshots, []string{"acme"}) {
		t.Fatalf("initial scheduled backup: %+v, snapshots %v", initial, snapshots)
	}

	state.Backup.Close = "close_empty"
	state.Version = 5
	now = now.Add(time.Second)
	f.applyCloseLock(state)
	f.stream(state)
	f.backup(state)
	wantFinal := protocol.Step{ID: state.Backup.Close, State: protocol.StepDone, At: now.UTC().Truncate(time.Second)}
	got := f.backupReport(state).Backups[0]
	if got.Final != wantFinal || got.Snapshot != wantFinal.At || !slices.Equal(snapshots, []string{"acme", "acme"}) {
		t.Fatalf("close with zero streamed databases: %+v, snapshots %v", got, snapshots)
	}
}

func TestCloseHostFinalWithoutActiveAccountsRetriesOnlyGreaterLockVersion(t *testing.T) {
	// R-W6KJ-PVLE R-WBG5-8YK6
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	f.put(dataPrivateKey, "private", 0o600)
	f.put(dataPublicKey, "public", 0o644)
	state := installTestState()
	state.Accounts[0].State = protocol.AccountRemoved
	now := restoreAsOf
	f.env.Now = func() time.Time { return now }
	var snapshots []string
	failSnapshot := false
	const failure = "host final snapshot failure"
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name != f.path(linux.ResticPath) {
			t.Fatalf("unexpected backup program: %s", c.Name)
		}
		switch c.Args[3] {
		case "backup":
			snapshots = append(snapshots, c.Args[6])
			if failSnapshot {
				return linux.Result{ExitCode: 1, Stderr: []byte(failure + "\n")}, nil
			}
			restoreStdout(t, c, fmt.Sprintf(`{"message_type":"summary","snapshot_id":"copy","backup_end":%q}`+"\n", now.Format(time.RFC3339)))
		case "snapshots":
			restoreStdout(t, c, "[]")
		}
		return linux.Result{}, nil
	})
	f.backup(state)
	if got := f.backupReport(state); got.HostBackup.Snapshot != now || !slices.Equal(snapshots, []string{state.Host}) {
		t.Fatalf("initial scheduled backup: %+v, snapshots %v", got, snapshots)
	}

	state.Backup.Close = "close_host"
	state.Version = 5
	now = now.Add(time.Second)
	f.applyCloseLock(state)
	failSnapshot = true
	f.failedBackupPass(state)
	wantFailed := protocol.Step{ID: state.Backup.Close, State: protocol.StepFailed, At: now, Code: protocol.CodeFailed}
	wantSnapshots := []string{state.Host, state.Host}
	checkFailed := func() {
		t.Helper()
		got := f.backupReport(state)
		if got.HostBackup.Final != wantFailed || !strings.Contains(got.HostBackup.Error, failure) || !slices.Equal(snapshots, wantSnapshots) {
			t.Fatalf("version %d: %+v, snapshots %v", state.Version, got, snapshots)
		}
	}
	checkFailed()

	f.host = linux.New(f.env)
	failSnapshot = false
	for _, version := range []int64{5, 4} {
		state.Version = version
		now = now.Add(time.Second)
		f.applyCloseLock(state)
		f.backup(state)
		checkFailed()
	}
	state.Version = 6
	now = now.Add(time.Second)
	f.backup(state)
	checkFailed()
	f.applyCloseLock(state)
	f.backup(state)
	wantDone := protocol.Step{ID: state.Backup.Close, State: protocol.StepDone, At: now}
	wantSnapshots = append(wantSnapshots, state.Host)
	if got := f.backupReport(state); got.HostBackup.Final != wantDone || got.HostBackup.Snapshot != now ||
		got.HostBackup.Error != "" || !slices.Equal(snapshots, wantSnapshots) {
		t.Fatalf("greater lock version: %+v, snapshots %v", got, snapshots)
	}
}

func TestCloseFinalTimeoutCountsFromLock(t *testing.T) {
	// R-WDVY-0I1K R-W6KJ-PVLE R-WBG5-8YK6
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

	state.Version++
	now = now.Add(time.Second)
	f.applyCloseLock(state)
	now = now.Add(linux.FinalSyncTimeout - time.Second)
	f.backup(state)
	report = f.backupReport(state)
	if report.HostBackup.Final != wantFailed || report.Backups[0].Final != wantFailed || report.Backups[1].Final != wantFailed {
		t.Fatalf("before renewed timeout: %+v", report)
	}
	now = now.Add(time.Second)
	f.backup(state)
	wantFailed.At = now
	report = f.backupReport(state)
	if report.HostBackup.Final != wantFailed || report.HostBackup.Error != machine.NoCredentials ||
		report.Backups[0].Final != wantFailed || report.Backups[0].Error != machine.ErrNotSynced.Error() ||
		report.Backups[1].Final != wantFailed || report.Backups[1].Error != machine.NoCredentials {
		t.Fatalf("at renewed timeout: %+v", report)
	}
}
