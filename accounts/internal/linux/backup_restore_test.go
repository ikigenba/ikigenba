package linux_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

var restoreAsOf = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

type hostBackupCommands func(linux.Command) (linux.Result, error)

func (f *hardeningFixture) executeBackupFixtures(handler hostBackupCommands) {
	f.t.Helper()
	base := f.env.Execute
	f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.ResticPath) || c.Name == f.path(linux.LitestreamPath) {
			if c.Stdout == nil {
				f.t.Fatal("backup program had no stdout sink")
			}
			return handler(c)
		}
		return base(ctx, c)
	}
	f.host = linux.New(f.env)
}

func (f *hardeningFixture) backup(state protocol.DesiredState) {
	f.t.Helper()
	if err := f.host.Backup(f.t.Context(), state); err != nil {
		f.t.Fatalf("Backup: %v", err)
	}
}

func (f *hardeningFixture) failedBackupPass(state protocol.DesiredState) {
	f.t.Helper()
	if err := f.host.Backup(f.t.Context(), state); err == nil {
		f.t.Fatal("a failed backup command or filesystem operation returned no error")
	}
}

func (f *hardeningFixture) backupReport(state protocol.DesiredState) protocol.Report {
	f.t.Helper()
	report, err := f.host.Inspect(f.t.Context(), state)
	if err != nil {
		f.t.Fatal(err)
	}
	return report
}

func (f *hardeningFixture) backupDirs(paths ...string) {
	f.t.Helper()
	for _, path := range paths {
		if err := os.MkdirAll(f.path(path), 0o700); err != nil {
			f.t.Fatal(err)
		}
	}
}

func restoreStdout(t *testing.T, c linux.Command, value string) {
	t.Helper()
	if _, err := io.WriteString(c.Stdout, value); err != nil {
		t.Fatal(err)
	}
}

func wholeRestoreState() protocol.DesiredState {
	state := installTestState()
	state.DataVolume = "vol-12345678"
	state.Accounts = nil
	state.Backup.Restore = &protocol.HostRestore{
		ID: "replace_one", Accounts: []protocol.RestoreAccount{{Slug: "acme", GID: 20001}},
	}
	state.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: protocol.Credentials{
		AccessKeyID: "AKIDaccountfixture", SecretAccessKey: "FixtureAccountSecret", SessionToken: "FixtureAccountToken",
	}}}
	return state
}

func (f *hardeningFixture) prepareWholeRestore(state protocol.DesiredState) {
	f.t.Helper()
	f.backupDirs("/srv", "/home")
	f.apply(state)
	f.put(linux.RestoreMarker, "restore "+state.Backup.Restore.ID+"\n", 0o600)
}

func TestWholeRestoreWaitsForApplyAndChecksInputs(t *testing.T) {
	// R-7CGO-HF7K R-Z2JU-7NY6
	for _, tc := range []struct {
		name string
		edit func(*hardeningFixture, *protocol.DesiredState)
		code string
		text string
	}{
		{"missing marker", func(f *hardeningFixture, _ *protocol.DesiredState) {
			if err := os.Remove(f.path(linux.RestoreMarker)); err != nil {
				f.t.Fatal(err)
			}
		}, protocol.CodeNotEmpty, ""},
		{"wrong marker", func(f *hardeningFixture, _ *protocol.DesiredState) {
			f.put(linux.RestoreMarker, "restore an_earlier_replace\n", 0o600)
		}, protocol.CodeNotEmpty, ""},
		{"directory marker", func(f *hardeningFixture, _ *protocol.DesiredState) {
			if err := os.Remove(f.path(linux.RestoreMarker)); err != nil {
				f.t.Fatal(err)
			}
			f.put(linux.RestoreMarker+"/existing", "retain", 0o600)
		}, protocol.CodeNotEmpty, ""},
		{"overlong marker", func(f *hardeningFixture, s *protocol.DesiredState) {
			f.put(linux.RestoreMarker, "restore "+s.Backup.Restore.ID+"\n"+strings.Repeat("x", linux.MaxCapture), 0o600)
		}, protocol.CodeNotEmpty, ""},
		{"linked marker", func(f *hardeningFixture, s *protocol.DesiredState) {
			f.put("/marker-target", "restore "+s.Backup.Restore.ID+"\n", 0o600)
			if err := os.Remove(f.path(linux.RestoreMarker)); err != nil {
				f.t.Fatal(err)
			}
			if err := os.Symlink(f.path("/marker-target"), f.path(linux.RestoreMarker)); err != nil {
				f.t.Fatal(err)
			}
		}, protocol.CodeNotEmpty, ""},
		{"marker in linked data directory", func(f *hardeningFixture, _ *protocol.DesiredState) {
			if err := os.Rename(f.path(linux.DataDir), f.path("/unrelated-restore")); err != nil {
				f.t.Fatal(err)
			}
			if err := os.Symlink(f.path("/unrelated-restore"), f.path(linux.DataDir)); err != nil {
				f.t.Fatal(err)
			}
		}, protocol.CodeNotEmpty, ""},
		{"duplicate", func(_ *hardeningFixture, s *protocol.DesiredState) {
			s.Backup.Restore.Accounts = append(s.Backup.Restore.Accounts, s.Backup.Restore.Accounts[0])
		}, protocol.CodeFailed, machine.ErrInvalidState.Error()},
		{"invalid paths", func(_ *hardeningFixture, s *protocol.DesiredState) {
			s.Backup.Restore.Accounts[0].Databases = []string{"data/../outside.db"}
		}, protocol.CodeFailed, machine.ErrInvalidState.Error()},
		{"invalid uid", func(_ *hardeningFixture, s *protocol.DesiredState) {
			s.Backup.Restore.Accounts[0].GID = 0
		}, protocol.CodeFailed, machine.ErrInvalidState.Error()},
		{"invalid slug", func(_ *hardeningFixture, s *protocol.DesiredState) {
			s.Backup.Restore.Accounts[0].Slug = "../../outside"
		}, protocol.CodeFailed, machine.ErrInvalidState.Error()},
		{"too many accounts", func(_ *hardeningFixture, s *protocol.DesiredState) {
			s.Backup.Restore.Accounts = nil
			for i := 0; i <= protocol.MaxBackupAccounts; i++ {
				s.Backup.Restore.Accounts = append(s.Backup.Restore.Accounts, protocol.RestoreAccount{
					Slug: fmt.Sprintf("acct-%d", i), GID: 20001,
				})
			}
		}, protocol.CodeFailed, machine.ErrInvalidState.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.putBackupProgramStamps()
			f.env.Now = func() time.Time {
				return time.Date(2026, 11, 18, 6, 0, 0, 750000000, time.FixedZone("CST", -6*60*60))
			}
			state := wholeRestoreState()
			f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
				t.Fatalf("rejected restore executed %v", c.Args)
				return linux.Result{}, nil
			})
			f.backup(state)
			before := f.backupReport(state).HostBackup
			if before.Restored != (protocol.Step{ID: state.Backup.Restore.ID, State: protocol.StepPending}) {
				t.Fatalf("before Apply: %+v", before)
			}
			withoutDevice := state
			withoutDevice.DataVolume = ""
			f.apply(withoutDevice)
			f.backup(state)
			if got := f.backupReport(state).HostBackup; got != before {
				t.Fatalf("Apply without data device advanced restore to %+v", got)
			}
			otherID := *state.Backup.Restore
			otherID.ID = "another_replace"
			otherBackup := *state.Backup
			otherBackup.Restore = &otherID
			otherState := state
			otherState.Backup = &otherBackup
			f.apply(otherState)
			f.backup(state)
			if got := f.backupReport(state).HostBackup; got != before {
				t.Fatalf("Apply for a different restore advanced restore to %+v", got)
			}
			f.prepareWholeRestore(state)
			const keptWork = "/srv/.accountsd-restore-acme/kept"
			f.put(keptWork, "existing work", 0o600)
			tc.edit(f, &state)
			beforeRejected := snapshotRestoreTree(t, f.root)
			priorCommands := slices.Clone(f.commands)
			markerPath := f.path(linux.RestoreMarker)
			marker, markerErr := os.Lstat(markerPath)
			f.backup(state)
			assertHostChanges(t, beforeRejected, snapshotRestoreTree(t, f.root), nil)
			if !slices.Equal(f.commands, priorCommands) {
				t.Fatalf("rejected restore executed extra commands: %v", f.commands[len(priorCommands):])
			}
			want := protocol.Step{ID: state.Backup.Restore.ID, State: protocol.StepFailed, Code: tc.code, At: restoreAsOf}
			if got := f.backupReport(state).HostBackup; got.Restored != want || got.Error != tc.text {
				t.Fatalf("restore failure = %+v, want %+v and %q", got, want, tc.text)
			}
			f.expectFile(keptWork, "existing work", 0o600)
			if errors.Is(markerErr, os.ErrNotExist) {
				f.expectAbsent(linux.RestoreMarker)
			} else if after, err := os.Lstat(markerPath); markerErr != nil || err != nil || !os.SameFile(marker, after) ||
				marker.Mode() != after.Mode() || marker.ModTime() != after.ModTime() {
				t.Fatalf("restore changed the marker: before %v (%v), after %v (%v)", marker, markerErr, after, err)
			}
			f.expectAbsent("/srv/acme")
		})
	}
}

func TestBackupClearsRestoreMarkerWithoutFollowingLinks(t *testing.T) {
	// R-ZKUB-Y82L R-76D6-KKI3
	t.Run("linked data directory", func(t *testing.T) {
		f := newBackupPassFixture(t)
		unrelated := "/unrelated-restore/" + filepath.Base(linux.RestoreMarker)
		f.put(unrelated, "unchanged", 0o600)
		if err := os.Symlink(f.path(filepath.Dir(unrelated)), f.path(linux.DataDir)); err != nil {
			t.Fatal(err)
		}
		before := snapshotApplyHost(t, f.path(filepath.Dir(unrelated)))
		_ = f.host.Backup(t.Context(), f.state)
		assertHostChanges(t, before, snapshotApplyHost(t, f.path(filepath.Dir(unrelated))), nil)
	})
	t.Run("linked marker with unavailable backup directory", func(t *testing.T) {
		f := newBackupPassFixture(t)
		const unrelated = "/unrelated-marker"
		f.put(unrelated, "unchanged", 0o600)
		if err := os.Mkdir(f.path(linux.DataDir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(f.path(unrelated), f.path(linux.RestoreMarker)); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(f.path(linux.BackupDir), f.path("/saved-backup")); err != nil {
			t.Fatal(err)
		}
		f.put(linux.BackupDir, "not a directory", 0o600)
		_ = f.host.Backup(t.Context(), f.state)
		f.expectAbsent(linux.RestoreMarker)
		f.expectFile(unrelated, "unchanged", 0o600)
	})
}

func TestWholeRestoreSelectsNewestTargetsAndRecoversAfterRestart(t *testing.T) {
	// R-7DOK-V6Y9 R-7EWH-8YOY R-7G4D-MQFN R-7JS2-S1NQ R-7M7V-JL54 R-ZJMF-KGBW R-ZKUB-Y82L
	// R-56WT-T1S0
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
	state.Backup.Accounts[0].Credentials.AccessKeyID = "manifest"
	state.Backup.Accounts[0].Credentials.SecretAccessKey = "snapshot"
	state.Backup.Accounts[0].Credentials.SessionToken = "23456"
	state.Backup.Restore.Accounts[0].Databases = []string{"data/default.db", "data/manifest.db"}
	hostRestores, accountRestores, homeRestores := 0, 0, 0
	var databases []string
	wantManifest := f.path(linux.BackupDir + "/manifest/acme")
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.LitestreamPath) {
			f.checkRestoreLitestream(c, state, "-if-replica-exists")
			databases = append(databases, filepath.Base(c.Args[7]))
			if c.Args[6] != f.path("/srv/.accountsd-restore-acme/.database") {
				t.Fatalf("database work output = %s", c.Args[6])
			}
			for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
				f.expectAbsent("/srv/.accountsd-restore-acme/.database" + suffix)
			}
			// When a replica is absent, a successful restore leaves no output.
			if filepath.Base(c.Args[len(c.Args)-1]) == "manifest.db" {
				f.put(strings.TrimPrefix(c.Args[len(c.Args)-2], f.root), "replica", 0o600)
			}
			return linux.Result{}, nil
		}
		args := c.Args[3:]
		hostListing := []string{"snapshots", "--json"}
		hostRestore := []string{"restore", "new-key:" + f.path("/data/ssh"), "--target", f.path("/data/ssh")}
		account := "acme"
		if slices.Equal(args, hostListing) || slices.Equal(args, hostRestore) {
			account = ""
		}
		f.checkRestoreRestic(c, state, account)
		switch {
		case slices.Equal(args, hostListing):
			restoreStdout(t, c, `[{"id":"older","time":"2026-11-17T12:00:00Z"},{"id":"new-key","time":"2026-11-17T13:00:00Z"}]`)
		case slices.Equal(args, hostRestore):
			hostRestores++
			f.put(dataPrivateKey, "restored host key", 0o600)
		case slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}):
			newest := map[string]any{
				"id": "new-snapshot", "time": restoreAsOf,
				"paths": []string{f.path("/home/alice"), wantManifest, "/home/unrooted", f.path("/home/not/named")},
			}
			older := map[string]any{"id": "older", "time": restoreAsOf.Add(-time.Hour), "paths": []string{f.path("/home/older")}}
			if err := json.NewEncoder(c.Stdout).Encode([]any{newest, older}); err != nil {
				t.Fatal(err)
			}
		case slices.Equal(args, []string{"restore", "new-snapshot:" + f.path("/srv"), "--target",
			f.path("/srv/.accountsd-restore-acme"), "--include", "/acme", "--overwrite", "if-changed"}):
			accountRestores++
			f.expectMode("/srv/.accountsd-restore-acme", 0o700)
			f.put("/srv/.accountsd-restore-acme/acme/doc.txt", "account files", 0o600)
			f.put("/srv/.accountsd-restore-acme/acme/data/restic.db", "already restored", 0o600)
			for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
				f.put("/srv/.accountsd-restore-acme/.database"+suffix, "old output", 0o600)
			}
		case slices.Equal(args, []string{"restore", "new-snapshot:" + f.path("/home"), "--target",
			f.path("/home/.accountsd-restore-alice"), "--include", "/alice", "--overwrite", "if-changed"}):
			homeRestores++
			f.expectMode("/home/.accountsd-restore-alice", 0o700)
			if homeRestores == 1 {
				f.put("/home/.accountsd-restore-alice/alice/partial.txt", "first attempt", 0o600)
				return linux.Result{ExitCode: 2}, nil
			}
			f.expectFile("/home/.accountsd-restore-alice/alice/partial.txt", "first attempt", 0o600)
			f.put("/home/.accountsd-restore-alice/alice/doc.txt", "home files", 0o600)
		case slices.Equal(args, []string{"dump", "new-snapshot", wantManifest}):
			restoreStdout(t, c, "23456 34567 0640 data/manifest.db\n123 123 0666 data/manifest.db\n123 123 0666 data/restic.db\n")
		default:
			t.Fatalf("unexpected restic command %v", args)
		}
		return linux.Result{}, nil
	})
	f.prepareWholeRestore(state)
	f.backupDirs("/srv/.accountsd-restore-acme")
	f.setMode("/srv/.accountsd-restore-acme", 0o755)
	f.put("/work-link-target", "retained", 0o600)
	if err := os.Symlink(f.path("/work-link-target"), f.path("/home/.accountsd-restore-alice")); err != nil {
		t.Fatal(err)
	}

	creds := state.Backup.Host
	state.Backup.Host = protocol.Credentials{}
	f.backup(state)
	if got := f.backupReport(state).HostBackup; got.Restored.State != protocol.StepPending || got.Error != machine.NoCredentials {
		t.Fatalf("host waiting for credentials = %+v", got)
	}
	state.Backup.Host = creds
	accountCreds := state.Backup.Accounts
	state.Backup.Accounts = nil
	f.backup(state)
	if got := f.backupReport(state).Backups[0]; got.Restored.State != protocol.StepPending || got.Error != machine.NoCredentials {
		t.Fatalf("account waiting for credentials = %+v", got)
	}
	state.Backup.Accounts = accountCreds
	f.failedBackupPass(state)
	pending := f.backupReport(state)
	if pending.HostBackup.Restored.State != protocol.StepDone || pending.Backups[0].Restored.State != protocol.StepPending ||
		pending.Backups[0].Error == "" || hostRestores != 1 || accountRestores != 1 || homeRestores != 1 {
		t.Fatalf("pass with retry needed = %+v and %d/%d/%d restores", pending, hostRestores, accountRestores, homeRestores)
	}
	f.host = linux.New(f.env)
	f.apply(state)
	if got := f.backupReport(state); got.HostBackup != pending.HostBackup || !slices.EqualFunc(got.Backups, pending.Backups,
		func(a, b protocol.AccountBackup) bool {
			return a.Account == b.Account && a.Error == b.Error && a.Restored == b.Restored
		}) {
		t.Fatalf("restore state lost after reopening: %+v", got)
	}
	f.backup(state)
	f.backup(state)
	report := f.backupReport(state)
	if report.HostBackup.Restored.State != protocol.StepDone || report.Backups[0].Restored.State != protocol.StepDone ||
		report.Backups[0].Error != "" || hostRestores != 1 || accountRestores != 2 || homeRestores != 2 ||
		!slices.Equal(databases, []string{"default.db", "manifest.db"}) {
		t.Fatalf("finished report %+v and %d/%d/%d restores", report, hostRestores, accountRestores, homeRestores)
	}
	f.expectFile(dataPrivateKey, "restored host key", 0o600)
	f.expectFile("/srv/acme/doc.txt", "account files", 0o600)
	f.expectFile("/srv/acme/data/manifest.db", "replica", 0o640)
	f.expectFile("/srv/acme/data/restic.db", "already restored", 0o600)
	f.expectFile("/home/alice/doc.txt", "home files", 0o600)
	f.expectFile("/home/alice/partial.txt", "first attempt", 0o600)
	f.expectFile("/work-link-target", "retained", 0o600)
	if !slices.Contains(f.commands, "chown --no-dereference 23456:34567 "+f.path("/srv/.accountsd-restore-acme/.database")) {
		t.Fatal("manifest ownership was not assigned")
	}
	if os.Getuid() != 0 || os.Getgid() != 0 {
		for _, work := range []string{"/srv/.accountsd-restore-acme", "/home/.accountsd-restore-alice"} {
			if !slices.Contains(f.commands, "chown --no-dereference root:root "+f.path(work)) {
				t.Fatalf("restore work %s did not get the root ownership command", work)
			}
		}
	}
	f.expectAbsent("/home/not")
	f.expectAbsent("/home/unrooted")
	f.expectAbsent("/home/older")
	f.expectAbsent("/srv/acme/data/default.db")
	f.expectAbsent("/srv/.accountsd-restore-acme")
	f.expectAbsent("/home/.accountsd-restore-alice")
	f.expectAbsent(linux.BackupDir + "/restore.yml")

	withoutRestore := state
	restored := state.Backup.Restore.Accounts[0]
	withoutRestore.Accounts = []protocol.Account{{Slug: restored.Slug, GID: restored.GID + 1, State: protocol.AccountActive}}
	withoutRestore.Backup = &protocol.Backup{Bucket: state.Backup.Bucket, Region: state.Backup.Region, Settings: state.Backup.Settings}
	f.backup(withoutRestore)
	f.expectAbsent(linux.RestoreMarker)
	f.put("/unrelated-file", "retained", 0o600)
	if err := os.Symlink(f.path("/unrelated-file"), f.path(linux.RestoreMarker)); err != nil {
		t.Fatal(err)
	}
	f.backup(withoutRestore)
	f.expectAbsent(linux.RestoreMarker)
	f.expectFile("/unrelated-file", "retained", 0o600)
	if got := f.backupReport(state); got.HostBackup.Restored.State != protocol.StepPending || got.Backups[0].Restored.State != protocol.StepPending {
		t.Fatalf("restore history survived removal: %+v", got)
	}
}

func TestWholeRestoreRemovesLitestreamConfigOnFailure(t *testing.T) {
	// R-7M7V-JL54
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
	state.Backup.Restore.Accounts[0].Databases = []string{"data/app.db"}
	const work = "/srv/.accountsd-restore-acme"
	const config = work + "/litestream.yml"
	var previous []byte
	var stale os.FileInfo
	var databases int
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.LitestreamPath) {
			f.checkRestoreLitestream(c, state, "-if-replica-exists")
			databases++
			if databases == 1 {
				var err error
				previous, err = os.ReadFile(filepath.Clean(c.Args[2]))
				if err != nil {
					t.Fatal(err)
				}
				return linux.Result{ExitCode: 1}, nil
			}
			if os.SameFile(stale, f.stat(config)) {
				t.Fatal("a stale restore config was reused")
			}
			return linux.Result{}, nil
		}
		args := c.Args[3:]
		switch {
		case slices.Equal(args, []string{"snapshots", "--json"}):
			return linux.Result{ExitCode: 10}, nil
		case slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}):
			restoreStdout(t, c, `[{"id":"copy","time":"2026-11-18T12:00:00Z"}]`)
		case args[0] == "restore":
			f.backupDirs(work + "/acme")
		default:
			t.Fatalf("unexpected restic command %v", args)
		}
		return linux.Result{}, nil
	})
	f.prepareWholeRestore(state)
	f.failedBackupPass(state)
	f.expectMode(work, 0o700)
	f.expectAbsent(config)
	f.put(config, string(previous), 0o600)
	stale = f.stat(config)
	f.backup(state)
	if got := f.backupReport(state).Backups[0].Restored; got.State != protocol.StepDone || databases != 2 {
		t.Fatalf("restore after a command failure = %+v, database commands = %d", got, databases)
	}
	f.expectAbsent(config)
}

func TestWholeRestoreWithNoSnapshots(t *testing.T) {
	// R-7CGO-HF7K R-7DOK-V6Y9 R-7EWH-8YOY R-7G4D-MQFN R-76D6-KKI3
	for _, status := range []int{0, 10} {
		t.Run(fmt.Sprintf("exit %d", status), func(t *testing.T) {
			f := newHardeningFixture(t)
			f.putBackupProgramStamps()
			state := wholeRestoreState()
			state.Accounts = installTestState().Accounts
			state.Backup.Close = "close_one"
			state.Backup.Restores = []protocol.RestoreRequest{
				{ID: "partial_one", Account: "acme", Kind: protocol.RestoreFiles, AsOf: restoreAsOf},
			}
			f.put(dataPrivateKey, "existing private key", 0o600)
			f.put(dataPublicKey, "existing public key", 0o644)
			var listings []string
			f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
				args := c.Args[3:]
				account := ""
				if len(args) == 4 {
					account = "acme"
				}
				f.checkRestoreRestic(c, state, account)
				if !slices.Equal(args, []string{"snapshots", "--json"}) && !slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}) {
					t.Fatalf("unexpected restore command: %v", args)
				}
				listings = append(listings, strings.Join(args, " "))
				if status == 10 {
					restoreStdout(t, c, `[{"id":"ignored","time":"2026-11-18T12:00:00Z"}]`)
				} else {
					restoreStdout(t, c, `[]`)
				}
				return linux.Result{ExitCode: status}, nil
			})
			f.prepareWholeRestore(state)
			if err := f.host.Stream(t.Context(), state); err != nil {
				t.Fatal(err)
			}
			f.backupDirs(filepath.Dir(linux.ResticCache))
			before := snapshotRestoreTree(t, f.root)
			priorCommands := slices.Clone(f.commands)
			f.backup(state)
			f.backup(state)
			assertHostChanges(t, before, snapshotRestoreTree(t, f.root), nil)
			if !slices.Equal(f.commands, priorCommands) {
				t.Fatalf("restore with no snapshots executed extra commands: %v", f.commands[len(priorCommands):])
			}
			report := f.backupReport(state)
			want := protocol.Step{ID: state.Backup.Restore.ID, State: protocol.StepDone, At: f.env.Now().UTC()}
			if report.HostBackup.Restored != want || report.Backups[0].Restored != want || len(report.Restores) != 0 ||
				!slices.Equal(listings, []string{"snapshots --json", "snapshots --host acme --json"}) {
				t.Fatalf("host/account reports = %+v/%+v, listings = %v", report.HostBackup, report.Backups, listings)
			}
			f.expectFile(dataPrivateKey, "existing private key", 0o600)
			f.expectFile(dataPublicKey, "existing public key", 0o644)
			f.expectAbsent("/srv/acme")
			f.forbidRestoreWork()
		})
	}
}

func snapshotRestoreTree(t *testing.T, root string) map[string]applyHostFile {
	t.Helper()
	tree := snapshotApplyHost(t, root)
	for path := range tree {
		if path == linux.BackupDir || strings.HasPrefix(path, linux.BackupDir+"/") ||
			path == linux.ResticCache || strings.HasPrefix(path, linux.ResticCache+"/") {
			delete(tree, path)
		}
	}
	return tree
}

func (f *hardeningFixture) checkRestoreRestic(c linux.Command, state protocol.DesiredState, account string) {
	f.t.Helper()
	b := state.Backup
	creds := b.Host
	prefix := strings.TrimRight(b.HostPrefix, "/")
	cache := ".host"
	if account != "" {
		i := slices.IndexFunc(b.Accounts, func(c protocol.AccountCredentials) bool { return c.Account == account })
		if i < 0 {
			f.t.Fatalf("fixture has no credentials for %q", account)
		}
		creds = b.Accounts[i].Credentials
		prefix, cache = protocol.FilesPrefix(account), account
	}
	wantArgs := []string{"-r", "s3:s3." + b.Region + ".amazonaws.com/" + b.Bucket + "/" + prefix, "--insecure-no-password"}
	wantEnv := []string{"AWS_ACCESS_KEY_ID=" + creds.AccessKeyID, "AWS_SECRET_ACCESS_KEY=" + creds.SecretAccessKey,
		"AWS_SESSION_TOKEN=" + creds.SessionToken, "AWS_DEFAULT_REGION=" + b.Region,
		"RESTIC_CACHE_DIR=" + f.path(linux.ResticCache+"/"+cache)}
	if len(c.Args) < 4 || !slices.Equal(c.Args[:3], wantArgs) || !slices.Equal(c.Env, wantEnv) {
		f.t.Fatalf("wrong restic arguments or credential environment: %v", c.Args)
	}
}

func (f *hardeningFixture) checkRestoreLitestream(c linux.Command, state protocol.DesiredState, flag string) {
	f.t.Helper()
	if len(c.Args) < 8 || c.Args[0] != "restore" || c.Args[1] != "-config" || c.Args[3] != "-no-expand-env" || c.Args[4] != flag {
		f.t.Fatalf("wrong Litestream arguments: %v", c.Args)
	}
	if c.Args[len(c.Args)-3] != "-o" {
		f.t.Fatalf("missing Litestream output flag: %v", c.Args)
	}
	database := c.Args[len(c.Args)-1]
	path, ok := strings.CutPrefix(database, f.path("/srv/acme")+"/")
	if !ok || !strings.HasPrefix(c.Args[len(c.Args)-2], f.path("/srv/.accountsd-restore-")) {
		f.t.Fatalf("wrong database and work paths: %v", c.Args)
	}
	b := state.Backup
	creds := b.Accounts[0].Credentials
	wantEnv := []string{"AWS_ACCESS_KEY_ID=" + creds.AccessKeyID, "AWS_SECRET_ACCESS_KEY=" + creds.SecretAccessKey,
		"AWS_SESSION_TOKEN=" + creds.SessionToken, "AWS_REGION=" + b.Region}
	if !slices.Equal(c.Env, wantEnv) {
		f.t.Fatal("wrong Litestream credential environment")
	}
	config := strings.TrimPrefix(c.Args[2], f.root)
	if c.Args[2] != filepath.Join(filepath.Dir(c.Args[len(c.Args)-2]), "litestream.yml") {
		f.t.Fatalf("Litestream config was outside work directory: %v", c.Args)
	}
	quote := func(s string) string {
		value, _ := json.Marshal(s)
		return string(value)
	}
	want := fmt.Sprintf("snapshot:\n  interval: 24h\n  retention: %dh\nshutdown-sync-timeout: %ds\ndbs:\n", 24*b.Settings.WindowDays, linux.ShutdownSyncTimeout/time.Second) +
		fmt.Sprintf("  - path: %s\n    meta-path: %s\n    replica:\n      type: s3\n      bucket: %s\n      path: %s\n      region: %s\n",
			quote("/srv/acme/"+path), quote(linux.BackupDir+"/meta/acme/"+path), quote(b.Bucket),
			quote(protocol.DataPrefix("acme")+"/"+strings.TrimPrefix(path, "data/")), quote(b.Region))
	f.expectFile(config, want, 0o600)
}

func TestWholeRestoreRetriesUnexpectedExitThreeTimes(t *testing.T) {
	// R-7HCA-0I6C
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
	secret := state.Backup.Accounts[0].Credentials.SecretAccessKey
	wantFailure := "restic restore: exit status 11: failed restore " + machine.Redacted
	calls := 0
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		args := c.Args[3:]
		switch {
		case slices.Equal(args, []string{"snapshots", "--json"}):
			return linux.Result{ExitCode: 10}, nil
		case slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}):
			restoreStdout(t, c, `[{"id":"copy","time":"2026-11-18T12:00:00Z"}]`)
		case args[0] == "restore":
			calls++
			return linux.Result{ExitCode: 11, Stderr: []byte("failed restore " + secret + "\n")}, nil
		default:
			t.Fatalf("unexpected command %v", args)
		}
		return linux.Result{}, nil
	})
	f.prepareWholeRestore(state)
	creds := state.Backup.Accounts
	state.Backup.Accounts = nil
	for range linux.RestoreAttempts + 1 {
		f.backup(state)
		got := f.backupReport(state).Backups[0]
		if got.Restored.State != protocol.StepPending || got.Error != machine.NoCredentials || calls != 0 {
			t.Fatalf("credential wait counted as an attempt: got %+v and %d commands", got, calls)
		}
	}
	state.Backup.Accounts = creds
	for pass := 1; pass <= linux.RestoreAttempts+1; pass++ {
		f.host = linux.New(f.env)
		f.apply(state)
		if pass <= linux.RestoreAttempts {
			if err := f.host.Backup(t.Context(), state); err == nil || err.Error() != wantFailure {
				t.Fatalf("pass %d did not return the redacted restore failure", pass)
			}
		} else {
			f.backup(state)
		}
		got := f.backupReport(state).Backups[0]
		if calls != min(pass, linux.RestoreAttempts) || got.Error != wantFailure {
			t.Fatalf("pass %d gave %d failed commands and did not report the redacted restore failure", pass, calls)
		}
		wantStep := protocol.Step{ID: state.Backup.Restore.ID, State: protocol.StepPending}
		if pass >= linux.RestoreAttempts {
			wantStep = protocol.Step{ID: state.Backup.Restore.ID, State: protocol.StepFailed, Code: protocol.CodeFailed, At: f.env.Now().UTC()}
		}
		if got.Restored != wantStep {
			t.Fatalf("pass %d reported %+v, want %+v", pass, got.Restored, wantStep)
		}
	}
}

func TestWholeRestoreBoundsManifest(t *testing.T) {
	// R-7IK6-E9X1
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
	watch, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(watch) })
	calls := 0
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.LitestreamPath) {
			calls++
			last := c.Args[len(c.Args)-1]
			want := f.path(fmt.Sprintf("/srv/acme/data/db-%03d", calls-1))
			if last != want {
				t.Fatalf("database %d = %s, want %s", calls, last, want)
			}
			return linux.Result{}, nil
		}
		args := c.Args[3:]
		switch {
		case slices.Equal(args, []string{"snapshots", "--json"}):
			return linux.Result{ExitCode: 10}, nil
		case slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}):
			writeOlderRestoreSnapshots(f, c)
			restoreStdout(t, c, `{"id":"copy","time":"2026-11-18T12:00:00Z","excludes":"`)
			restoreStdout(t, c, strings.Repeat("x", linux.MaxJournalLine*2))
			restoreStdout(t, c, fmt.Sprintf(`","paths":[%q]}]`, f.path(linux.BackupDir+"/manifest/acme")))
		case slices.Equal(args, []string{"restore", "copy:" + f.path("/srv"), "--target",
			f.path("/srv/.accountsd-restore-acme"), "--include", "/acme", "--overwrite", "if-changed"}):
			const restored = "/srv/.accountsd-restore-acme/acme/document"
			f.put(restored, "contents the restore must never read", 0o600)
			if _, err := unix.InotifyAddWatch(watch, f.path(restored), unix.IN_ACCESS); err != nil {
				t.Fatal(err)
			}
		case args[0] == "dump":
			// The UID's leading zeroes make this line overlong without
			// changing its value or making the path itself invalid.
			restoreStdout(t, c, strings.Repeat("0", linux.MaxJournalLine*2))
			restoreStdout(t, c, "20001 20001 0660 data/db-oversize\n")
			for i := range linux.MaxDatabasesPerAccount + 10 {
				restoreStdout(t, c, fmt.Sprintf("20001 20001 0660 data/db-%03d\n", i))
			}
		default:
			t.Fatalf("unexpected command %v", args)
		}
		return linux.Result{}, nil
	})
	f.prepareWholeRestore(state)
	f.backup(state)
	if got := f.backupReport(state).Backups[0]; got.Restored.State != protocol.StepDone || calls != linux.MaxDatabasesPerAccount {
		t.Fatalf("bounded manifest gave %d restores, account %+v", calls, got)
	}
	var events [512]byte
	if n, err := unix.Read(watch, events[:]); !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("watch for reads of the restored file returned %d bytes, %v", n, err)
	}
	f.expectFile("/srv/acme/document", "contents the restore must never read", 0o600)
}

func writeOlderRestoreSnapshots(f *hardeningFixture, c linux.Command) {
	f.t.Helper()
	paths := make([]string, 120)
	for i := range paths {
		paths[i] = f.path(fmt.Sprintf("/home/user-%03d", i))
	}
	encoded, err := json.Marshal(paths)
	if err != nil {
		f.t.Fatal(err)
	}
	var baseline, during runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&baseline)
	restoreStdout(f.t, c, "[")
	for i := range 1536 {
		restoreStdout(f.t, c, fmt.Sprintf(`{"id":"%064x","time":"2026-11-17T12:00:00Z","paths":%s},`, i, encoded))
	}
	// The listing is still open, so a reader that collected the whole
	// array would still hold its buffer or its decoded entries.
	runtime.GC()
	runtime.ReadMemStats(&during)
	if during.HeapInuse > baseline.HeapInuse+(4<<20) {
		f.t.Fatalf("restore listing retained %d bytes of heap", during.HeapInuse-baseline.HeapInuse)
	}
}

func TestWholeRestoreRefusesOccupiedTargetsAndRetriesRemainingMoves(t *testing.T) {
	// R-7EWH-8YOY R-7G4D-MQFN
	for _, occupiedInitially := range []bool{true, false} {
		t.Run(fmt.Sprintf("occupied initially %t", occupiedInitially), func(t *testing.T) {
			f := newHardeningFixture(t)
			f.putBackupProgramStamps()
			state := wholeRestoreState()
			accountCalls, homeCalls, dumps := 0, 0, 0
			manifest := f.path(linux.BackupDir + "/manifest/acme")
			f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
				args := c.Args[3:]
				switch {
				case slices.Equal(args, []string{"snapshots", "--json"}):
					return linux.Result{ExitCode: 10}, nil
				case slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}):
					restoreStdout(t, c, fmt.Sprintf(`[{"id":"copy","time":"2026-11-18T12:00:00Z","paths":[%q,%q]}]`, f.path("/home/alice"), manifest))
				case args[0] == "restore" && args[5] == "/acme":
					accountCalls++
					f.put("/srv/.accountsd-restore-acme/acme/doc", "account", 0o600)
				case args[0] == "restore" && args[5] == "/alice":
					homeCalls++
					if homeCalls == 1 {
						f.put("/home/alice/doc", "arrived after target check", 0o600)
					}
					f.put("/home/.accountsd-restore-alice/alice/doc", "home", 0o600)
				case slices.Equal(args, []string{"dump", "copy", manifest}):
					dumps++
				default:
					t.Fatalf("unexpected command %v", args)
				}
				return linux.Result{}, nil
			})
			f.prepareWholeRestore(state)
			f.backupDirs("/srv/acme", "/home/alice")
			if occupiedInitially {
				f.put("/home/alice/doc", "existing", 0o600)
			}
			if occupiedInitially {
				f.backup(state)
			} else {
				f.failedBackupPass(state)
			}
			if occupiedInitially {
				got := f.backupReport(state).Backups[0]
				if got.Restored.State != protocol.StepFailed || got.Restored.Code != protocol.CodeNotEmpty || accountCalls != 0 || homeCalls != 0 {
					t.Fatalf("nonempty target produced %+v, account/home calls %d/%d", got, accountCalls, homeCalls)
				}
				f.expectFile("/home/alice/doc", "existing", 0o600)
				f.expectAbsent("/srv/.accountsd-restore-acme")
				return
			}
			f.expectFile("/home/alice/doc", "arrived after target check", 0o600)
			f.expectFile("/srv/acme/doc", "account", 0o600)
			if got := f.backupReport(state).Backups[0]; got.Restored.State != protocol.StepPending || got.Error == "" {
				t.Fatalf("partially moved restore = %+v", got)
			}
			if err := os.Remove(f.path("/home/alice/doc")); err != nil {
				t.Fatal(err)
			}
			f.host = linux.New(f.env)
			f.apply(state)
			f.backup(state)
			f.expectFile("/home/alice/doc", "home", 0o600)
			if got := f.backupReport(state).Backups[0]; got.Restored.State != protocol.StepDone || accountCalls != 1 || homeCalls != 2 || dumps != 1 {
				t.Fatalf("restarted restore %+v, account/home/dump calls %d/%d/%d", got, accountCalls, homeCalls, dumps)
			}
		})
	}
}

func partialRestoreState() protocol.DesiredState {
	state := installTestState()
	state.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: state.Backup.Host}}
	state.Backup.Host = protocol.Credentials{}
	return state
}

func TestPartialDatabaseRestoreNamesCopiesAndKeepsResults(t *testing.T) {
	// R-7KZZ-5TEF R-ZB34-W251 R-7NFR-XCVT R-7M7V-JL54 R-ZIEJ-6OL7
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := partialRestoreState()
	state.Backup.Host = installTestState().Backup.Host
	f.put(dataPrivateKey, "host private key", 0o600)
	f.put(dataPublicKey, "host public key", 0o644)
	state.Accounts = append(state.Accounts, protocol.Account{Slug: "inactive", GID: 20002, State: protocol.AccountRemoved})
	state.Backup.Accounts = append(state.Backup.Accounts, protocol.AccountCredentials{Account: "inactive", Credentials: state.Backup.Accounts[0].Credentials})
	state.Backup.Restores = []protocol.RestoreRequest{
		{ID: "z_database", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "b_waiting", Account: "absent", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "b_inactive", Account: "inactive", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "a_hidden", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/.cache", AsOf: restoreAsOf},
		{ID: "a_invalid", Account: "acme", Kind: protocol.RestoreFiles, Path: "../escape", AsOf: restoreAsOf},
		{ID: "!!invalid", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "invalid_kind", Account: "acme", Kind: "unknown", Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "invalid_slug", Account: "../acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "invalid_path", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/../app.db", AsOf: restoreAsOf},
		{ID: "invalid_user", Account: "acme", Kind: protocol.RestoreFiles, User: "absent", AsOf: restoreAsOf},
	}
	f.put(linux.RestoreMarker, "restore earlier_replace\n", 0o600)
	f.put("/srv/acme/data/app.db", "original database", 0o640)
	f.put("/srv/acme/data/app.restored-20261118T120000Z.db", "older copy", 0o600)
	live := f.stat("/srv/acme/data/app.db").Sys().(*syscall.Stat_t)
	var calls []string
	var backupOrder []string
	var assignedOwners []string
	f.observe = func(c linux.Command) {
		if c.Name == "chown" && len(c.Args) == 3 && c.Args[0] == "--no-dereference" &&
			strings.HasPrefix(c.Args[2], f.path("/srv/.accountsd-restore-")) && filepath.Base(c.Args[2]) == "database" {
			f.expectAbsent(strings.TrimPrefix(filepath.Join(filepath.Dir(c.Args[2]), "litestream.yml"), f.root))
			assignedOwners = append(assignedOwners, c.Args[1])
		}
	}
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.ResticPath) {
			f.checkRestoreRestic(c, state, "")
			verb := c.Args[3]
			backupOrder = append(backupOrder, verb)
			switch verb {
			case "cat", "forget":
			case "backup":
				restoreStdout(t, c, fmt.Sprintf("{\"message_type\":\"summary\",\"snapshot_id\":\"key\",\"backup_end\":%q}\n",
					f.env.Now().Format(time.RFC3339)))
			default:
				t.Fatalf("unexpected host backup command %v", c.Args)
			}
			return linux.Result{}, nil
		}
		backupOrder = append(backupOrder, "litestream")
		f.checkRestoreLitestream(c, state, "-timestamp")
		f.expectAbsent(linux.RestoreMarker)
		if c.Args[5] != "2026-11-18T12:00:01Z" || filepath.Base(c.Args[7]) != "database" {
			t.Fatalf("wrong restore timestamp or work output: %v", c.Args)
		}
		if f.stat(strings.TrimPrefix(filepath.Dir(c.Args[7]), f.root)).Mode().Perm() != 0o700 {
			t.Fatal("restore work directory is not private")
		}
		if (os.Getuid() != 0 || os.Getgid() != 0) && !slices.Contains(f.commands, "chown --no-dereference root:root "+filepath.Dir(c.Args[7])) {
			t.Fatal("restore work directory was not assigned to root")
		}
		calls = append(calls, filepath.Base(c.Args[8]))
		f.put(strings.TrimPrefix(c.Args[7], f.root), "saved "+filepath.Base(c.Args[8]), 0o600)
		return linux.Result{}, nil
	})
	f.backup(state)
	if want := []string{"cat", "backup", "forget", "litestream", "litestream"}; !slices.Equal(backupOrder, want) {
		t.Fatalf("backup and partial restore order = %v, want %v", backupOrder, want)
	}
	f.host = linux.New(f.env)
	f.backup(state)
	errText := machine.ErrInvalidState.Error()
	want := []protocol.RestoreResult{
		{ID: "!!invalid", State: protocol.StepFailed, Error: errText},
		{ID: "a_hidden", State: protocol.StepDone, Copy: "data/.cache.restored-20261118T120000Z"},
		{ID: "a_invalid", State: protocol.StepFailed, Error: errText},
		{ID: "invalid_kind", State: protocol.StepFailed, Error: errText},
		{ID: "invalid_path", State: protocol.StepFailed, Error: errText},
		{ID: "invalid_slug", State: protocol.StepFailed, Error: errText},
		{ID: "invalid_user", State: protocol.StepFailed, Error: errText},
		{ID: "z_database", State: protocol.StepDone, Copy: "data/app.restored-20261118T120000Z-2.db"},
	}
	if got := f.backupReport(state).Restores; !slices.Equal(got, want) || !slices.Equal(calls, []string{".cache", "app.db"}) {
		t.Fatalf("results = %+v, calls %v", got, calls)
	}
	if wantOwners := []string{fmt.Sprintf("%d:%d", state.Accounts[0].GID, state.Accounts[0].GID), fmt.Sprintf("%d:%d", live.Uid, live.Gid)}; !slices.Equal(assignedOwners, wantOwners) {
		t.Fatalf("database ownership commands = %v, want %v", assignedOwners, wantOwners)
	}
	f.expectFile("/srv/acme/data/app.db", "original database", 0o640)
	f.expectFile("/srv/acme/data/app.restored-20261118T120000Z.db", "older copy", 0o600)
	f.expectFile("/srv/acme/"+want[1].Copy, "saved .cache", 0o660)
	f.expectFile("/srv/acme/"+want[len(want)-1].Copy, "saved app.db", 0o640)
	f.forbidRestoreWork()

	original := state.Backup.Restores
	state.Backup.Restores = nil
	f.backup(state)
	state.Backup.Restores = original
	if got := f.backupReport(state).Restores; len(got) != 0 {
		t.Fatalf("removed request results are still present: %v", got)
	}
	state.Backup.Restores = []protocol.RestoreRequest{
		{ID: "new_waiting", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/waiting.db", AsOf: restoreAsOf},
	}
	credentials := state.Backup.Accounts
	state.Backup.Accounts = nil
	f.backup(state)
	if got := f.backupReport(state).Restores; len(got) != 0 || len(calls) != 2 {
		t.Fatalf("request lacking credentials reported %+v and ran %v", got, calls)
	}
	state.Backup.Accounts = credentials
	f.backup(state)
	wantWaiting := []protocol.RestoreResult{
		{ID: "new_waiting", State: protocol.StepDone, Copy: "data/waiting.restored-20261118T120000Z.db"},
	}
	if got := f.backupReport(state).Restores; !slices.Equal(got, wantWaiting) || !slices.Equal(calls, []string{".cache", "app.db", "waiting.db"}) {
		t.Fatalf("request after receiving credentials reported %+v and ran %v", got, calls)
	}
	f.expectFile("/srv/acme/"+wantWaiting[0].Copy, "saved waiting.db", 0o660)
}

func (f *hardeningFixture) forbidRestoreWork() {
	f.t.Helper()
	for _, dir := range []string{"/srv", "/home"} {
		matches, err := filepath.Glob(f.path(dir + "/.accountsd-restore-*"))
		if err != nil || len(matches) != 0 {
			f.t.Fatalf("%s work directories remain: %v, %v", dir, matches, err)
		}
	}
}

func TestPartialDatabaseRestoreRefusesSymlinkAndCollision(t *testing.T) {
	// R-7NFR-XCVT R-7VGU-VQLI
	for _, linked := range []bool{true, false} {
		t.Run(fmt.Sprintf("linked data directory %t", linked), func(t *testing.T) {
			f := newHardeningFixture(t)
			f.putBackupProgramStamps()
			state := partialRestoreState()
			state.Backup.Restores = []protocol.RestoreRequest{
				{ID: "database_copy", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
			}
			const copyPath = "/srv/acme/data/app.restored-20261118T120000Z.db"
			external := t.TempDir()
			if linked {
				f.backupDirs("/srv/acme")
				if err := os.Symlink(external, f.path("/srv/acme/data")); err != nil {
					t.Fatal(err)
				}
			} else {
				f.backupDirs("/srv/acme/data")
			}
			calls := 0
			f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
				calls++
				f.checkRestoreLitestream(c, state, "-timestamp")
				f.put(strings.TrimPrefix(c.Args[7], f.root), "restored output", 0o600)
				f.put(copyPath, "another writer's entry", 0o600)
				return linux.Result{}, nil
			})
			f.failedBackupPass(state)
			results := f.backupReport(state).Restores
			if len(results) != 1 || results[0].ID != state.Backup.Restores[0].ID || results[0].State != protocol.StepFailed ||
				results[0].Copy != "" || results[0].Error == "" {
				t.Fatalf("unsafe destination result = %+v", results)
			}
			if linked {
				if calls != 0 {
					t.Fatalf("linked path reached a restore command: %d", calls)
				}
				entries, err := os.ReadDir(external)
				if err != nil || len(entries) != 0 {
					t.Fatalf("target of symlink changed: %v, %v", entries, err)
				}
			} else {
				if calls != 1 {
					t.Fatalf("restore command calls = %d", calls)
				}
				f.expectFile(copyPath, "another writer's entry", 0o600)
			}
			f.forbidRestoreWork()
		})
	}
}

func TestPartialRestoresRemoveWorkResidue(t *testing.T) {
	// R-7NFR-XCVT R-7ONO-B4MI
	for _, tc := range []struct {
		kind   string
		path   string
		copy   string
		file   string
		mode   os.FileMode
		status int
	}{
		{protocol.RestoreDatabase, "data/app.db", "data/app.restored-20261118T120000Z.db", "", 0o660, 0},
		{protocol.RestoreDatabase, "data/app.db", "data/app.restored-20261118T120000Z.db", "", 0o660, 1},
		{protocol.RestoreFiles, "", "restored/20261118T120000Z", "/document", 0o600, 0},
		{protocol.RestoreFiles, "", "restored/20261118T120000Z", "/document", 0o600, 1},
	} {
		t.Run(fmt.Sprintf("%s exit %d", tc.kind, tc.status), func(t *testing.T) {
			f := newHardeningFixture(t)
			f.putBackupProgramStamps()
			f.backupDirs("/srv/acme/data")
			const unrelated = "/unrelated"
			f.put(unrelated, "unchanged", 0o600)
			state := partialRestoreState()
			state.Backup.Restores = []protocol.RestoreRequest{
				{ID: "copy_request", Account: "acme", Kind: tc.kind, Path: tc.path, AsOf: restoreAsOf},
			}
			calls := 0
			f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
				var output string
				if tc.kind == protocol.RestoreDatabase {
					f.checkRestoreLitestream(c, state, "-timestamp")
					output = c.Args[7]
				} else {
					f.checkRestoreRestic(c, state, "acme")
					switch c.Args[3] {
					case "snapshots":
						restoreStdout(t, c, `[{"id":"copy","time":"2026-11-18T12:00:00Z"}]`)
						return linux.Result{}, nil
					case "restore":
						output = c.Args[6]
					default:
						t.Fatalf("unexpected files restore command %v", c.Args)
					}
				}
				calls++
				output = strings.TrimPrefix(output, f.root)
				f.put(output+tc.file, "restored data", 0o600)
				work := filepath.Dir(output)
				for i := range linux.ReadBatch + 1 {
					f.put(filepath.Join(work, fmt.Sprintf("leftover-%d", i)), "staging residue", 0o600)
				}
				blocked := []string{filepath.Join(work, "residue", "deep"), filepath.Join(work, "residue", "other")}
				for _, dir := range blocked {
					f.put(dir+"/part", "staging residue", 0o600)
				}
				if err := os.Symlink(f.path(unrelated), f.path(blocked[0]+"/link")); err != nil {
					t.Fatal(err)
				}
				f.setMode(blocked[0], 0)
				f.setMode(blocked[1], 0o500)
				t.Cleanup(func() {
					for _, dir := range blocked {
						if _, err := os.Lstat(f.path(dir)); err == nil {
							f.setMode(dir, 0o700)
						}
					}
				})
				return linux.Result{ExitCode: tc.status}, nil
			})
			if tc.status == 0 {
				f.backup(state)
			} else {
				f.failedBackupPass(state)
			}
			f.host = linux.New(f.env)
			f.backup(state)
			got := f.backupReport(state).Restores
			if len(got) != 1 || got[0].ID != "copy_request" || calls != 1 {
				t.Fatalf("restore reports %+v and ran %d times", got, calls)
			}
			if tc.status == 0 {
				if want := (protocol.RestoreResult{ID: "copy_request", State: protocol.StepDone, Copy: tc.copy}); got[0] != want {
					t.Fatalf("successful restore reports %+v", got[0])
				}
				f.expectFile("/srv/acme/"+tc.copy+tc.file, "restored data", tc.mode)
			} else {
				if got[0].State != protocol.StepFailed || got[0].Copy != "" || !strings.Contains(got[0].Error, "exit status 1") {
					t.Fatalf("failed restore reports %+v", got[0])
				}
				f.expectAbsent("/srv/acme/" + tc.copy)
			}
			f.expectFile(unrelated, "unchanged", 0o600)
			f.forbidRestoreWork()
		})
	}
}

func TestPartialFilesRestoreEscapesPatternAndCopiesIntoHome(t *testing.T) {
	// R-7ONO-B4MI R-7M7V-JL54 R-7JS2-S1NQ R-ZB34-W251
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := partialRestoreState()
	state.Users = []protocol.User{{Name: "alice", Account: "acme", UID: 22001, State: protocol.UserLocked}}
	state.Backup.Restores = []protocol.RestoreRequest{
		{ID: "copy_files", Account: "acme", Kind: protocol.RestoreFiles, User: "alice", Path: "docs/a[1].txt", AsOf: restoreAsOf},
		{ID: "no_snapshot", Account: "acme", Kind: protocol.RestoreFiles, AsOf: restoreAsOf.Add(-time.Second)},
		{ID: "second_copy", Account: "acme", Kind: protocol.RestoreFiles, User: "alice", Path: "docs/a[1].txt", AsOf: restoreAsOf},
	}
	f.put("/home/alice/docs/a[1].txt", "live", 0o600)
	copies := 0
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		f.checkRestoreRestic(c, state, "acme")
		args := c.Args[3:]
		switch {
		case slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}):
			restoreStdout(t, c, `[{"id":"wanted","time":"2026-11-18T12:00:00.9Z"},{"id":"newer","time":"2026-11-18T12:00:01Z"}]`)
		case len(args) == 6 && args[0] == "restore" && args[1] == "wanted:"+f.path("/home/alice") &&
			args[2] == "--target" && strings.HasPrefix(args[3], f.path("/home/.accountsd-restore-")) &&
			args[4] == "--include" && args[5] == `/docs/a\[1].txt`:
			copies++
			f.put(strings.TrimPrefix(args[3], f.root)+"/docs/a[1].txt", "saved", 0o600)
		default:
			t.Fatalf("unexpected command %v", args)
		}
		return linux.Result{}, nil
	})
	f.backup(state)
	want := []protocol.RestoreResult{
		{ID: "copy_files", State: protocol.StepDone, Copy: "restored/20261118T120000Z/docs/a[1].txt"},
		{ID: "no_snapshot", State: protocol.StepFailed, Error: machine.ErrNoSnapshot.Error()},
		{ID: "second_copy", State: protocol.StepDone, Copy: "restored/20261118T120000Z-2/docs/a[1].txt"},
	}
	if got := f.backupReport(state).Restores; copies != 2 || !slices.Equal(got, want) {
		t.Fatalf("files restored %d times, got %+v", copies, got)
	}
	f.expectMode("/home/alice/restored", 0o700)
	f.expectMode("/home/alice/restored/20261118T120000Z", 0o700)
	if !slices.ContainsFunc(f.commands, func(command string) bool {
		return strings.HasPrefix(command, "chown --no-dereference 22001:22001 "+f.path("/home/.accountsd-restore-")) && strings.HasSuffix(command, "/restored")
	}) {
		t.Fatal("new restored directory was not assigned to the user")
	}
	f.expectFile("/home/alice/"+want[0].Copy, "saved", 0o600)
	f.expectFile("/home/alice/"+want[2].Copy, "saved", 0o600)
	f.expectFile("/home/alice/docs/a[1].txt", "live", 0o600)
	f.forbidRestoreWork()
}

func TestPartialFilesRestoreSkipsOccupiedTimestampDirectory(t *testing.T) {
	// R-7ONO-B4MI
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := partialRestoreState()
	state.Backup.Restores = []protocol.RestoreRequest{
		{ID: "copy_files", Account: "acme", Kind: protocol.RestoreFiles, Path: "docs/a.txt", AsOf: restoreAsOf},
		{ID: "copy_whole", Account: "acme", Kind: protocol.RestoreFiles, AsOf: restoreAsOf},
		{ID: "missing_file", Account: "acme", Kind: protocol.RestoreFiles, Path: "docs/missing.txt", AsOf: restoreAsOf},
	}
	const occupied = "/srv/acme/restored/20261118T120000Z"
	f.put(occupied+"/unrelated", "retained", 0o600)
	var output []string
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		f.checkRestoreRestic(c, state, "acme")
		args := c.Args[3:]
		if slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}) {
			restoreStdout(t, c, `[{"id":"wanted","time":"2026-11-18T06:00:00.9-06:00"}]`)
			return linux.Result{}, nil
		}
		if len(args) < 4 || !slices.Equal(args[:3], []string{"restore", "wanted:" + f.path("/srv/acme"), "--target"}) ||
			!strings.HasPrefix(args[3], f.path("/srv/.accountsd-restore-")) || filepath.Base(args[3]) != "r" {
			t.Fatalf("restore args = %v", args)
		}
		restored := strings.TrimPrefix(args[3], f.root)
		f.expectMode(filepath.Dir(restored), 0o700)
		output = append(output, args[3])
		switch len(output) {
		case 1:
			if !slices.Equal(args[4:], []string{"--include", "/docs/a.txt"}) {
				t.Fatalf("include arguments = %v", args[4:])
			}
			f.put(restored+"/docs/a.txt", "saved", 0o600)
			return linux.Result{}, nil
		case 2:
			if len(args) != 4 {
				t.Fatalf("whole-folder restore arguments = %v", args)
			}
			f.put(restored+"/unpublished", "failed", 0o600)
			return linux.Result{ExitCode: 1}, nil
		default:
			if !slices.Equal(args[4:], []string{"--include", "/docs/missing.txt"}) {
				t.Fatalf("include arguments = %v", args[4:])
			}
			return linux.Result{}, nil
		}
	})
	f.failedBackupPass(state)
	got := f.backupReport(state).Restores
	want := protocol.RestoreResult{ID: "copy_files", State: protocol.StepDone, Copy: "restored/20261118T120000Z-2/docs/a.txt"}
	if len(got) != 3 || got[0] != want || got[1].ID != "copy_whole" || got[1].State != protocol.StepFailed || got[1].Copy != "" ||
		!strings.Contains(got[1].Error, "exit status 1") || got[2].ID != "missing_file" || got[2].State != protocol.StepFailed ||
		got[2].Copy != "" || got[2].Error == "" || len(output) != 3 {
		t.Fatalf("partial files results = %+v, output directories = %v", got, output)
	}
	if want := fmt.Sprintf("chown --no-dereference %[1]d:%[1]d %s", state.Accounts[0].GID, output[0]); !slices.Contains(f.commands, want) {
		t.Fatalf("missing account ownership command %q", want)
	}
	if mode := f.stat("/srv/acme/restored/20261118T120000Z-2").Mode(); mode.Perm() != 0o770 || mode&os.ModeSetgid == 0 {
		t.Fatalf("account copy directory mode = %v, want 02770", mode)
	}
	f.expectFile("/srv/acme/"+want.Copy, "saved", 0o600)
	f.expectFile(occupied+"/unrelated", "retained", 0o600)
	f.expectAbsent(occupied + "/docs")
	f.expectAbsent(occupied + "-3")
	f.forbidRestoreWork()
}

func TestWholeRestoreStatePrunesAbsentPartialRequest(t *testing.T) {
	// R-ZIEJ-6OL7
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	f.backupDirs("/srv/acme/data")
	state := partialRestoreState()
	state.Backup.Restores = []protocol.RestoreRequest{
		{ID: "database_copy", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
	}
	calls := 0
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		f.checkRestoreLitestream(c, state, "-timestamp")
		calls++
		f.put(strings.TrimPrefix(c.Args[7], f.root), "database copy", 0o600)
		return linux.Result{}, nil
	})
	f.backup(state)
	first := []protocol.RestoreResult{
		{ID: "database_copy", State: protocol.StepDone, Copy: "data/app.restored-20261118T120000Z.db"},
	}
	if got := f.backupReport(state).Restores; !slices.Equal(got, first) {
		t.Fatalf("first partial restore = %+v", got)
	}

	whole := wholeRestoreState()
	whole.Backup.Restores = state.Backup.Restores
	f.backup(whole)
	if got := f.backupReport(whole).Restores; !slices.Equal(got, first) || calls != 1 {
		t.Fatalf("whole restore state discarded or ran a retained partial request: %+v, %d commands", got, calls)
	}
	whole.Backup.Restores = nil
	f.backup(whole)
	f.host = linux.New(f.env)
	if got := f.backupReport(state).Restores; len(got) != 0 {
		t.Fatalf("whole restore state kept a dropped partial request: %+v", got)
	}
	f.backup(state)
	second := []protocol.RestoreResult{
		{ID: "database_copy", State: protocol.StepDone, Copy: "data/app.restored-20261118T120000Z-2.db"},
	}
	if got := f.backupReport(state).Restores; !slices.Equal(got, second) || calls != 2 {
		t.Fatalf("reintroduced partial restore = %+v, %d commands", got, calls)
	}
}

func TestWholeRestoreRetriesTargetThatWasNeverMoved(t *testing.T) {
	// R-7G4D-MQFN
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
	accountCalls, homeCalls := 0, 0
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		args := c.Args[3:]
		switch {
		case slices.Equal(args, []string{"snapshots", "--json"}):
			return linux.Result{ExitCode: 10}, nil
		case slices.Equal(args, []string{"snapshots", "--host", "acme", "--json"}):
			restoreStdout(t, c, fmt.Sprintf(`[{"id":"copy","time":"2026-11-18T12:00:00Z","paths":[%q]}]`, f.path("/home/alice")))
		case args[0] == "restore" && args[5] == "/acme":
			accountCalls++
			if accountCalls == 2 {
				f.put("/srv/.accountsd-restore-acme/acme/doc", "account", 0o600)
			}
		case args[0] == "restore" && args[5] == "/alice":
			homeCalls++
			if homeCalls == 1 {
				f.put("/home/alice/doc", "arrived after target check", 0o600)
			}
			f.put("/home/.accountsd-restore-alice/alice/doc", "home", 0o600)
		default:
			t.Fatalf("unexpected restore command %v", args)
		}
		return linux.Result{}, nil
	})
	f.prepareWholeRestore(state)
	f.failedBackupPass(state)
	f.expectAbsent("/srv/acme")
	if err := os.Remove(f.path("/home/alice/doc")); err != nil {
		t.Fatal(err)
	}
	f.host = linux.New(f.env)
	f.apply(state)
	f.backup(state)
	f.expectFile("/srv/acme/doc", "account", 0o600)
	f.expectFile("/home/alice/doc", "home", 0o600)
	if got := f.backupReport(state).Backups[0]; got.Restored.State != protocol.StepDone || accountCalls != 2 || homeCalls != 2 {
		t.Fatalf("restarted account restore %+v, account/home calls %d/%d", got, accountCalls, homeCalls)
	}
}
