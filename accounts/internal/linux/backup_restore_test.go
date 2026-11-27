package linux_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	state.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: state.Backup.Host}}
	return state
}

func (f *hardeningFixture) prepareWholeRestore(state protocol.DesiredState) {
	f.t.Helper()
	f.backupDirs("/srv", "/home")
	f.apply(state)
	f.put(linux.RestoreMarker, "restore "+state.Backup.Restore.ID+"\n", 0o600)
}

func TestWholeRestoreWaitsForApplyAndChecksInputs(t *testing.T) {
	// R-UVJ8-V1XA R-Z2JU-7NY6
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
		{"linked marker", func(f *hardeningFixture, s *protocol.DesiredState) {
			f.put("/marker-target", "restore "+s.Backup.Restore.ID+"\n", 0o600)
			if err := os.Remove(f.path(linux.RestoreMarker)); err != nil {
				f.t.Fatal(err)
			}
			if err := os.Symlink(f.path("/marker-target"), f.path(linux.RestoreMarker)); err != nil {
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
			f.prepareWholeRestore(state)
			tc.edit(f, &state)
			f.backup(state)
			want := protocol.Step{ID: state.Backup.Restore.ID, State: protocol.StepFailed, Code: tc.code, At: f.env.Now().UTC()}
			if got := f.backupReport(state).HostBackup; got.Restored != want || got.Error != tc.text {
				t.Fatalf("restore failure = %+v, want %+v and %q", got, want, tc.text)
			}
			f.expectAbsent("/srv/acme")
		})
	}
}

func TestWholeRestoreSelectsNewestTargetsAndRecoversAfterRestart(t *testing.T) {
	// R-UZ6Y-0D5D R-V1MQ-RWMR R-V2UN-5ODG R-VA61-GATM R-ZJMF-KGBW R-ZKUB-Y82L
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
	state.Backup.Restore.Accounts[0].Databases = []string{"data/default.db", "data/manifest.db"}
	hostRestores, accountRestores, homeRestores := 0, 0, 0
	wantManifest := f.path(linux.BackupDir + "/manifest/acme")
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.LitestreamPath) {
			f.checkRestoreLitestream(c, state, "-if-replica-exists")
			// When a replica is absent, a successful restore leaves no output.
			if filepath.Base(c.Args[len(c.Args)-1]) == "manifest.db" {
				f.put(strings.TrimPrefix(c.Args[len(c.Args)-2], f.root), "replica", 0o600)
			}
			return linux.Result{}, nil
		}
		f.checkRestoreRestic(c, state)
		args := c.Args[3:]
		switch {
		case slices.Equal(args, []string{"snapshots", "--json"}):
			restoreStdout(t, c, `[{"id":"older","time":"2026-11-17T12:00:00Z"},{"id":"new-key","time":"2026-11-17T13:00:00Z"}]`)
		case slices.Equal(args, []string{"restore", "new-key:" + f.path("/data/ssh"), "--target", f.path("/data/ssh")}):
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
			f.put("/srv/.accountsd-restore-acme/acme/doc.txt", "account files", 0o600)
		case slices.Equal(args, []string{"restore", "new-snapshot:" + f.path("/home"), "--target",
			f.path("/home/.accountsd-restore-alice"), "--include", "/alice", "--overwrite", "if-changed"}):
			homeRestores++
			if homeRestores == 1 {
				return linux.Result{ExitCode: 2}, nil
			}
			f.put("/home/.accountsd-restore-alice/alice/doc.txt", "home files", 0o600)
		case slices.Equal(args, []string{"dump", "new-snapshot", wantManifest}):
			restoreStdout(t, c, "23456 34567 0640 data/manifest.db\n123 123 0666 data/manifest.db\n")
		default:
			t.Fatalf("unexpected restic command %v", args)
		}
		return linux.Result{}, nil
	})
	f.prepareWholeRestore(state)

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
		report.Backups[0].Error != "" || hostRestores != 1 || accountRestores != 2 || homeRestores != 2 {
		t.Fatalf("finished report %+v and %d/%d/%d restores", report, hostRestores, accountRestores, homeRestores)
	}
	f.expectFile(dataPrivateKey, "restored host key", 0o600)
	f.expectFile("/srv/acme/doc.txt", "account files", 0o600)
	f.expectFile("/srv/acme/data/manifest.db", "replica", 0o640)
	f.expectFile("/home/alice/doc.txt", "home files", 0o600)
	if !slices.Contains(f.commands, "chown --no-dereference 23456:34567 "+f.path("/srv/.accountsd-restore-acme/.database")) {
		t.Fatal("manifest ownership was not assigned")
	}
	f.expectAbsent("/home/not")
	f.expectAbsent("/home/unrooted")
	f.expectAbsent("/home/older")
	f.expectAbsent("/srv/acme/data/default.db")
	f.expectAbsent("/srv/.accountsd-restore-acme")
	f.expectAbsent("/home/.accountsd-restore-alice")
	f.expectAbsent(linux.BackupDir + "/restore.yml")

	withoutRestore := state
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

func (f *hardeningFixture) checkRestoreRestic(c linux.Command, state protocol.DesiredState) {
	f.t.Helper()
	b := state.Backup
	creds := b.Host
	prefix := strings.TrimRight(b.HostPrefix, "/")
	cache := ".host"
	if len(c.Args) > 1 && strings.HasSuffix(c.Args[1], "/"+protocol.FilesPrefix("acme")) {
		creds = b.Accounts[0].Credentials
		prefix, cache = protocol.FilesPrefix("acme"), "acme"
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
	if filepath.Dir(c.Args[2]) != filepath.Dir(c.Args[len(c.Args)-2]) {
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
	// R-V0EU-E4W2
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
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
			return linux.Result{ExitCode: 11, Stderr: []byte("failed restore\n")}, nil
		default:
			t.Fatalf("unexpected command %v", args)
		}
		return linux.Result{}, nil
	})
	f.prepareWholeRestore(state)
	for pass := 1; pass <= linux.RestoreAttempts+1; pass++ {
		f.host = linux.New(f.env)
		f.apply(state)
		if pass <= linux.RestoreAttempts {
			f.failedBackupPass(state)
		} else {
			f.backup(state)
		}
		got := f.backupReport(state).Backups[0]
		if calls != min(pass, linux.RestoreAttempts) || !strings.Contains(got.Error, "failed restore") {
			t.Fatalf("pass %d gave %d failed commands and %+v", pass, calls, got)
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

func TestWholeRestoreBoundsManifestAndKeepsFirstMetadata(t *testing.T) {
	// R-V8Y5-2J2X
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := wholeRestoreState()
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
			restoreStdout(t, c, `[{"id":"copy","time":"2026-11-18T12:00:00Z","excludes":"`)
			restoreStdout(t, c, strings.Repeat("x", linux.MaxJournalLine*2))
			restoreStdout(t, c, fmt.Sprintf(`","paths":[%q]}]`, f.path(linux.BackupDir+"/manifest/acme")))
		case args[0] == "restore":
			f.backupDirs("/srv/.accountsd-restore-acme/acme")
		case args[0] == "dump":
			restoreStdout(t, c, strings.Repeat("x", linux.MaxJournalLine*2)+"\n")
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
}

func TestWholeRestoreRefusesOccupiedTargetsAndRetriesRemainingMoves(t *testing.T) {
	// R-V1MQ-RWMR R-V2UN-5ODG
	for _, occupiedInitially := range []bool{true, false} {
		t.Run(fmt.Sprintf("occupied initially %t", occupiedInitially), func(t *testing.T) {
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
					f.put("/srv/.accountsd-restore-acme/acme/doc", "account", 0o600)
				case args[0] == "restore" && args[5] == "/alice":
					homeCalls++
					if homeCalls == 1 {
						f.put("/home/alice/doc", "arrived after target check", 0o600)
					}
					f.put("/home/.accountsd-restore-alice/alice/doc", "home", 0o600)
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
			if got := f.backupReport(state).Backups[0]; got.Restored.State != protocol.StepDone || accountCalls != 1 || homeCalls != 2 {
				t.Fatalf("restarted restore %+v, account/home calls %d/%d", got, accountCalls, homeCalls)
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
	// R-V5AF-X7UU R-ZB34-W251 R-V6IC-AZLJ R-ZIEJ-6OL7
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	state := partialRestoreState()
	state.Backup.Restores = []protocol.RestoreRequest{
		{ID: "z_database", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "b_waiting", Account: "absent", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "a_hidden", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/.cache", AsOf: restoreAsOf},
		{ID: "a_invalid", Account: "acme", Kind: protocol.RestoreFiles, Path: "../escape", AsOf: restoreAsOf},
		{ID: "!!invalid", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "invalid_kind", Account: "acme", Kind: "unknown", Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "invalid_slug", Account: "../acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: restoreAsOf},
		{ID: "invalid_path", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/../app.db", AsOf: restoreAsOf},
		{ID: "invalid_user", Account: "acme", Kind: protocol.RestoreFiles, User: "absent", AsOf: restoreAsOf},
	}
	f.put("/srv/acme/data/app.db", "original database", 0o640)
	f.put("/srv/acme/data/app.restored-20261118T120000Z.db", "older copy", 0o600)
	var calls []string
	f.executeBackupFixtures(func(c linux.Command) (linux.Result, error) {
		f.checkRestoreLitestream(c, state, "-timestamp")
		if c.Args[5] != "2026-11-18T12:00:01Z" || filepath.Base(c.Args[7]) != "database" {
			t.Fatalf("wrong restore timestamp or work output: %v", c.Args)
		}
		if f.stat(strings.TrimPrefix(filepath.Dir(c.Args[7]), f.root)).Mode().Perm() != 0o700 {
			t.Fatal("restore work directory is not private")
		}
		calls = append(calls, filepath.Base(c.Args[8]))
		f.put(strings.TrimPrefix(c.Args[7], f.root), "saved "+filepath.Base(c.Args[8]), 0o600)
		return linux.Result{}, nil
	})
	f.backup(state)
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
	state.Backup.Accounts = nil
	f.backup(state)
	if len(calls) != 2 {
		t.Fatalf("requests lacking credentials ran: %v", calls)
	}
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
	// R-7VGU-VQLI
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

func TestPartialFilesRestoreEscapesPatternAndCopiesIntoHome(t *testing.T) {
	// R-ZB34-W251
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
		f.checkRestoreRestic(c, state)
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
	f.expectFile("/home/alice/"+want[0].Copy, "saved", 0o600)
	f.expectFile("/home/alice/"+want[2].Copy, "saved", 0o600)
	f.expectFile("/home/alice/docs/a[1].txt", "live", 0o600)
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
	// R-V2UN-5ODG
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
