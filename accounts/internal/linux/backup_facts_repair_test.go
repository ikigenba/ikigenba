package linux_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const accountManifest = linux.BackupDir + "/manifest/acme"

func inspectBackupFacts(t *testing.T, host *linux.Host, state protocol.DesiredState) protocol.Report {
	t.Helper()
	report, err := host.Inspect(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func expectBlankAccountBackup(t *testing.T, report protocol.Report) {
	t.Helper()
	want := []protocol.AccountBackup{{Account: "acme"}}
	if !reflect.DeepEqual(report.Backups, want) {
		t.Fatalf("account backup facts = %+v, want %+v", report.Backups, want)
	}
}

func TestBackupFactsDropForRemovedAndOmittedAccounts(t *testing.T) {
	// R-AIO3-P7TK
	for _, method := range []string{"Stream", "Backup"} {
		for _, removed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/removed=%t", method, removed), func(t *testing.T) {
				f := newBackupPassFixture(t)
				f.state.Backup.Settings.CheckDays = 7
				f.put("/srv/acme/data/app.db", walSQLite, 0o600)
				f.stream()
				f.backup()
				before := f.report().Backups[0]
				if before.Snapshot.IsZero() || before.Checked.IsZero() || len(before.Databases) != 1 {
					t.Fatalf("setup did not record backup facts: %+v", before)
				}
				f.expectFile(accountManifest, fmt.Sprintf("%d %d 0600 data/app.db\n", os.Getuid(), os.Getgid()), 0o600)

				inactive := f.state
				inactive.Version++
				inactive.Backup = nil
				inactive.Accounts = nil
				if removed {
					account := f.state.Accounts[0]
					account.State = protocol.AccountRemoved
					inactive.Accounts = []protocol.Account{account}
				}
				pass := f.host.Stream
				if method == "Backup" {
					pass = f.host.Backup
				}
				if err := pass(t.Context(), inactive); err != nil {
					t.Fatal(err)
				}
				f.expectAbsent(accountManifest)
				expectBlankAccountBackup(t, f.report())
				expectBlankAccountBackup(t, inspectBackupFacts(t, linux.New(f.env), f.state))

				f.state.Version = inactive.Version + 1
				f.at = f.at.Add(time.Minute)
				f.backupCommands = nil
				f.backup()
				if got := f.resticVerbs(); len(got) != 0 {
					t.Fatalf("backup used the dropped walk: %v", got)
				}
				expectBlankAccountBackup(t, f.report())
				f.stream()
				f.backup()
				f.requireResticConfig("acme")
			})
		}
	}
}

func TestBackupFactsKeepAccountsNamedByWholeRestore(t *testing.T) {
	// R-AIO3-P7TK
	for _, method := range []string{"Stream", "Backup"} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/active=%t", method, active), func(t *testing.T) {
				f := newBackupPassFixture(t)
				f.put("/srv/acme/data/app.db", walSQLite, 0o600)
				f.stream()
				f.backup()
				want := f.report().Backups
				if want[0].Snapshot.IsZero() || len(want[0].Databases) != 1 {
					t.Fatalf("setup did not record backup facts: %+v", want)
				}

				restore := f.state
				restore.Version++
				if !active {
					restore.Accounts = nil
				}
				backup := *f.state.Backup
				backup.Restore = &protocol.HostRestore{ID: "restore-1", Accounts: []protocol.RestoreAccount{{
					Slug: "acme", GID: f.state.Accounts[0].GID,
				}}}
				restore.Backup = &backup
				pass := f.host.Stream
				if method == "Backup" {
					pass = f.host.Backup
				}
				if err := pass(t.Context(), restore); err != nil {
					t.Fatal(err)
				}
				f.expectFile(accountManifest, fmt.Sprintf("%d %d 0600 data/app.db\n", os.Getuid(), os.Getgid()), 0o600)
				for _, host := range []*linux.Host{f.host, linux.New(f.env)} {
					if got := inspectBackupFacts(t, host, f.state).Backups; !reflect.DeepEqual(got, want) {
						t.Fatalf("restore dropped account facts: got %+v, want %+v", got, want)
					}
				}
			})
		}
	}
}

func TestBackupFactsPruneBatchesWithoutFollowingLinks(t *testing.T) {
	// R-AIO3-P7TK
	f := newBackupPassFixture(t)
	for i := range linux.ReadBatch + 1 {
		f.put(fmt.Sprintf("%s/manifest/old%d", linux.BackupDir, i), "old manifest", 0o600)
		f.put(fmt.Sprintf("%s/status/old%d.json", linux.BackupDir, i), "obsolete record", 0o600)
	}
	const target = "/outside-backup"
	f.put(target, "target", 0o600)
	for _, path := range []string{linux.BackupDir + "/status/oldlink.json", linux.BackupDir + "/manifest/oldlink"} {
		if err := os.Symlink(f.path(target), f.path(path)); err != nil {
			t.Fatal(err)
		}
	}
	f.state.Backup = nil
	f.backup()
	for _, dir := range []string{"status", "manifest"} {
		entries, err := os.ReadDir(f.path(linux.BackupDir + "/" + dir))
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s after pruning = %v, %v", dir, entries, err)
		}
	}
	f.expectFile(target, "target", 0o600)
}

func TestBackupFactsDropAccountSteps(t *testing.T) {
	// R-AIO3-P7TK
	t.Run("final", func(t *testing.T) {
		f := newBackupPassFixture(t)
		f.state.Backup.Host = protocol.Credentials{}
		f.stream()
		f.state.Backup.Close = "close_drop"
		f.state.Version++
		f.applyCloseLock(f.state)
		f.backup()
		if step := f.report().Backups[0].Final; step.ID != f.state.Backup.Close || step.State != protocol.StepDone {
			t.Fatalf("setup did not record a completed final: %+v", step)
		}
		dropped := f.state
		dropped.Backup, dropped.Accounts = nil, nil
		dropped.Version++
		if err := f.host.Backup(t.Context(), dropped); err != nil {
			t.Fatal(err)
		}
		for _, host := range []*linux.Host{f.host, linux.New(f.env)} {
			step := inspectBackupFacts(t, host, f.state).Backups[0].Final
			if want := (protocol.Step{ID: f.state.Backup.Close, State: protocol.StepPending}); step != want {
				t.Fatalf("dropped account's final = %+v, want %+v", step, want)
			}
		}
	})

	t.Run("restored", func(t *testing.T) {
		f := newHardeningFixture(t)
		f.putBackupProgramStamps()
		state := wholeRestoreState()
		f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
			restoreStdout(t, c, "[]")
			return linux.Result{}, nil
		})
		f.prepareWholeRestore(state)
		f.backup(state)
		id := state.Backup.Restore.ID
		if step := f.backupReport(state).Backups[0].Restored; step.ID != id || step.State != protocol.StepDone {
			t.Fatalf("setup did not record a completed restore: %+v", step)
		}
		dropped := state
		dropped.Backup = nil
		dropped.Version++
		if err := f.host.Stream(t.Context(), dropped); err != nil {
			t.Fatal(err)
		}
		for _, host := range []*linux.Host{f.host, linux.New(f.env)} {
			report := inspectBackupFacts(t, host, state)
			step := report.Backups[0].Restored
			if want := (protocol.Step{ID: id, State: protocol.StepPending}); step != want {
				t.Fatalf("dropped account's restore = %+v, want %+v", step, want)
			}
			if hostStep := report.HostBackup.Restored; hostStep.ID != id || hostStep.State != protocol.StepDone {
				t.Fatalf("account pruning changed the host restore: %+v", hostStep)
			}
		}
	})
}

func TestBackupFactsIsolateAccountGIDs(t *testing.T) {
	// R-AJW0-2ZK9
	f := newBackupPassFixture(t)
	f.state.Backup.Settings.CheckDays = 7
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	f.stream()
	f.backup()
	if before := f.report().Backups[0]; before.Snapshot.IsZero() || before.Checked.IsZero() || len(before.Databases) != 1 {
		t.Fatalf("setup did not record backup facts: %+v", before)
	}
	f.state.Accounts[0].GID++
	f.state.Version++
	f.at = f.at.Add(time.Minute)
	expectBlankAccountBackup(t, f.report())
	expectBlankAccountBackup(t, inspectBackupFacts(t, linux.New(f.env), f.state))
	f.backupCommands = nil
	f.backup()
	if got := f.resticVerbs(); len(got) != 0 {
		t.Fatalf("backup used the old placement's walk: %v", got)
	}
	expectBlankAccountBackup(t, f.report())
	f.stream()
	f.backup()
	if got, want := f.resticVerbs(), []string{"cat", "backup", "forget", "snapshots", "check"}; !slices.Equal(got, want) {
		t.Fatalf("first restic operations for new gid = %v, want %v", got, want)
	}
}

func TestOldPlacementBackupCannotOverwriteNewPlacementFacts(t *testing.T) {
	// R-AJW0-2ZK9
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	old, next := f.state, f.state
	next.Accounts = slices.Clone(old.Accounts)
	next.Accounts[0].GID++
	next.Version++

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	base := f.env.Execute
	var resticVerbs []string
	held := false
	f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
		if c.Name != f.path(linux.ResticPath) {
			return base(ctx, c)
		}
		resticVerbs = append(resticVerbs, c.Args[3])
		if c.Args[3] == "backup" {
			if !held {
				held = true
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return linux.Result{}, ctx.Err()
				}
			}
			_, err := fmt.Fprintf(c.Stdout, "{\"message_type\":\"summary\",\"snapshot_id\":\"fixture\",\"backup_end\":%q}\n", f.at.Format(time.RFC3339Nano))
			return linux.Result{}, err
		}
		return linux.Result{}, nil
	}
	f.host = linux.New(f.env)
	f.stream()
	var oldErr error
	go func() {
		defer close(finished)
		oldErr = f.host.Backup(ctx, old)
	}()
	unblock := sync.OnceFunc(func() { close(release) })
	defer func() {
		unblock()
		select {
		case <-finished:
		case <-ctx.Done():
			t.Error("old backup did not finish after release")
		}
	}()
	select {
	case <-finished:
		t.Fatalf("old backup did not reach its snapshot command: %v", oldErr)
	case <-started:
	case <-ctx.Done():
		t.Fatal("old backup did not reach its snapshot command")
	}
	streamDone := make(chan error, 1)
	go func() { streamDone <- f.host.Stream(ctx, next) }()
	select {
	case err := <-streamDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("stream did not finish while the old snapshot was in progress")
	}
	unblock()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("old backup did not finish after release")
	}
	if oldErr != nil {
		t.Fatal(oldErr)
	}
	for _, host := range []*linux.Host{f.host, linux.New(f.env)} {
		got := inspectBackupFacts(t, host, next).Backups[0]
		if !got.Snapshot.IsZero() || len(got.Databases) != 1 || got.Databases[0].Path != "data/app.db" {
			t.Fatalf("old backup overwrote new placement facts: %+v", got)
		}
	}

	resticVerbs = nil
	if err := f.host.Backup(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if want := []string{"cat", "backup", "forget", "snapshots"}; !slices.Equal(resticVerbs, want) {
		t.Fatalf("restic operations for new placement = %v, want %v", resticVerbs, want)
	}
}

func TestRestoreFactsUseAccountGIDBeforeRestoreGID(t *testing.T) {
	// R-AJW0-2ZK9
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		restoreStdout(t, c, "[]")
		return linux.Result{}, nil
	})
	f.prepareWholeRestore(state)
	f.backup(state)
	restarted := linux.New(f.env)
	restore := state.Backup.Restore
	if step := inspectBackupFacts(t, restarted, state).Backups[0].Restored; step.ID != restore.ID || step.State != protocol.StepDone {
		t.Fatalf("restore-only state reported %+v", step)
	}
	state.Accounts = []protocol.Account{{Slug: "acme", GID: restore.Accounts[0].GID + 1, State: protocol.AccountActive}}
	for _, host := range []*linux.Host{f.host, restarted} {
		step := inspectBackupFacts(t, host, state).Backups[0].Restored
		if want := (protocol.Step{ID: restore.ID, State: protocol.StepPending}); step != want {
			t.Fatalf("state with a new account gid reported %+v instead of %+v", step, want)
		}
	}
}

func TestBackupFactsSurviveRestartAndDiscardOversizedRecords(t *testing.T) {
	// R-B26H-TJOO
	f := newBackupPassFixture(t)
	f.state.Backup.Settings.CheckDays = 7
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	f.put(dataPrivateKey, "private key", 0o600)
	f.put(dataPublicKey, "public key", 0o644)
	f.stream()
	f.backup()
	want := f.report()
	if want.HostBackup.Snapshot.IsZero() || want.Backups[0].Snapshot.IsZero() ||
		want.Backups[0].Checked.IsZero() || len(want.Backups[0].Databases) != 1 {
		t.Fatalf("setup did not record backup facts: %+v, %+v", want.HostBackup, want.Backups)
	}

	f.host = linux.New(f.env)
	got := f.report()
	if !reflect.DeepEqual(got.Backups, want.Backups) || got.HostBackup != want.HostBackup {
		t.Fatalf("backup facts after restart = %+v, %+v, want %+v, %+v",
			got.HostBackup, got.Backups, want.HostBackup, want.Backups)
	}
	f.backupCommands = nil
	f.stream()
	f.backup()
	if got := f.resticVerbs(); len(got) != 0 {
		t.Fatalf("restarted backup ignored persisted due times: %v", got)
	}

	for _, path := range []string{linux.BackupDir + "/status/acme.json", linux.BackupDir + "/status/.host.json"} {
		data, err := os.ReadFile(f.path(path))
		if err != nil {
			t.Fatal(err)
		}
		f.put(path, string(data)+strings.Repeat(" ", linux.MaxCapture), 0o600)
	}
	got = f.report()
	expectBlankAccountBackup(t, got)
	if got.HostBackup != (protocol.HostBackup{}) {
		t.Fatalf("oversized host status supplied facts: %+v", got.HostBackup)
	}
}

func TestHostRestoreErrorYieldsToUnusableCredentials(t *testing.T) {
	// R-B0YL-FRXZ
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
	f.executeBackupFixtures(func(_ linux.Command) (linux.Result, error) {
		return linux.Result{ExitCode: 1, Stderr: []byte("repository unavailable\n")}, nil
	})
	f.prepareWholeRestore(state)
	f.failedBackupPass(state)
	wantStep := protocol.Step{ID: state.Backup.Restore.ID, State: protocol.StepPending}
	got := f.backupReport(state).HostBackup
	if got.Restored != wantStep || got.Error != "restic snapshots: exit status 1: repository unavailable" {
		t.Fatalf("report after host restore failure = %+v", got)
	}
	state.Backup.Host.SessionToken = ""
	got = f.backupReport(state).HostBackup
	if got.Restored != wantStep || got.Error != machine.NoCredentials {
		t.Fatalf("report after host credentials became unusable = %+v", got)
	}
}

func TestHostBackupReportsLatestErrorUnlessCredentialsAreUnusable(t *testing.T) {
	// R-B0YL-FRXZ
	f := newBackupPassFixture(t)
	if got := f.report().HostBackup; got != (protocol.HostBackup{}) {
		t.Fatalf("initial host backup = %+v", got)
	}
	f.put(dataPrivateKey, "private key", 0o600)
	f.put(dataPublicKey, "public key", 0o644)
	f.backup()
	at := f.at.UTC().Truncate(time.Second)
	if got := f.report().HostBackup; got.Snapshot != at || got.Error != "" {
		t.Fatalf("host backup after success = %+v", got)
	}
	f.put(dataPrivateKey, "changed private key", 0o600)
	f.execute = func(_ linux.Command) (linux.Result, error) {
		return linux.Result{ExitCode: 1, Stderr: []byte("repository unavailable\n")}, nil
	}
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("expected failed host repository command")
	}
	if got := f.report().HostBackup; got.Snapshot != at || got.Error != "restic cat: exit status 1: repository unavailable" {
		t.Fatalf("host backup after failure = %+v", got)
	}
	backup := f.state.Backup
	f.state.Backup = nil
	if got := f.report().HostBackup; got.Snapshot != at || got.Error != machine.NoCredentials {
		t.Fatalf("host backup with missing backup configuration = %+v", got)
	}
	f.state.Backup = backup
	creds := backup.Host
	backup.Host.SessionToken = ""
	if got := f.report().HostBackup; got.Snapshot != at || got.Error != machine.NoCredentials {
		t.Fatalf("host backup with unusable credentials = %+v", got)
	}
	backup.Host = creds
	f.execute = nil
	f.backup()
	if got := f.report().HostBackup; got.Snapshot != at || got.Error != "" {
		t.Fatalf("host backup after recovery = %+v", got)
	}
}
