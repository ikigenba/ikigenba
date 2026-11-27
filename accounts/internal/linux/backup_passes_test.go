package linux_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
	walSQLite   = "SQLite format 3\x00\x00\x00\x02\x02"
	plainSQLite = "SQLite format 3\x00\x00\x00\x01\x01"
)

type backupPassFixture struct {
	*hardeningFixture
	state          protocol.DesiredState
	at             time.Time
	backupCommands []linux.Command
	execute        func(linux.Command) (linux.Result, error)
}

func newBackupPassFixture(t *testing.T) *backupPassFixture {
	t.Helper()
	f := &backupPassFixture{hardeningFixture: newHardeningFixture(t), state: installTestState(),
		at: time.Date(2026, 11, 26, 12, 0, 0, 123456789, time.FixedZone("fixture", -5*3600))}
	f.putBackupProgramStamps()
	b := f.state.Backup
	b.Host.Expires = f.at.Add(time.Hour)
	b.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: protocol.Credentials{
		AccessKeyID: "AccountFixtureKey234", SecretAccessKey: "AccountFixtureSecret345", SessionToken: "AccountFixtureToken456",
		Expires: b.Host.Expires,
	}}}
	b.Settings.Hourly, b.Settings.Daily, b.Settings.Monthly, b.Settings.WindowDays = 24, 35, 12, 35
	base := f.env.Execute
	f.env.Now = func() time.Time { return f.at }
	f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) {
		t.Fatal("unexpected backup install fetch")
		return nil, nil
	}
	f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.ResticPath) || c.Name == f.path(linux.LitestreamPath) || c.Name == "systemctl" && (c.Args[0] == "daemon-reload" || strings.Contains(strings.Join(c.Args, " "), "accountsd-litestream@")) {
			f.backupCommands = append(f.backupCommands, c)
			if f.execute != nil {
				return f.execute(c)
			}
			if c.Name == f.path(linux.ResticPath) && c.Args[3] == "backup" {
				f.summary(c)
			}
			return linux.Result{}, nil
		}
		return base(ctx, c)
	}
	f.host = linux.New(f.env)
	return f
}

func (f *backupPassFixture) stream() {
	f.t.Helper()
	if err := f.host.Stream(f.t.Context(), f.state); err != nil {
		f.t.Fatal(err)
	}
}

func (f *backupPassFixture) backup() {
	f.t.Helper()
	if err := f.host.Backup(f.t.Context(), f.state); err != nil {
		f.t.Fatal(err)
	}
}

func (f *backupPassFixture) report() protocol.Report {
	f.t.Helper()
	r, err := f.host.Inspect(f.t.Context(), f.state)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func (f *backupPassFixture) output(c linux.Command, s string) {
	f.t.Helper()
	if c.Stdout == nil {
		f.t.Fatalf("missing streaming output for %v", c.Args)
	}
	if _, err := io.WriteString(c.Stdout, s); err != nil {
		f.t.Fatal(err)
	}
}

func (f *backupPassFixture) summary(c linux.Command) {
	f.t.Helper()
	f.output(c, fmt.Sprintf(`{"message_type":"summary","snapshot_id":"fixture","backup_end":%q}`+"\n", f.at.Format(time.RFC3339Nano)))
}

func (f *backupPassFixture) commandsFor(program string) []linux.Command {
	var out []linux.Command
	for _, c := range f.backupCommands {
		if c.Name == program {
			out = append(out, c)
		}
	}
	return out
}

func TestBackupMemberGateCleansOnlyStreamUnits(t *testing.T) {
	// R-Y6RR-TK2S
	for _, tc := range []struct {
		name       string
		invalidate func(*protocol.DesiredState)
	}{
		{"missing", func(s *protocol.DesiredState) { s.Backup = nil }},
		{"short bucket", func(s *protocol.DesiredState) { s.Backup.Bucket = "ab" }},
		{"long bucket", func(s *protocol.DesiredState) { s.Backup.Bucket = strings.Repeat("a", 64) }},
		{"uppercase bucket", func(s *protocol.DesiredState) { s.Backup.Bucket = "Abc" }},
		{"invalid bucket prefix", func(s *protocol.DesiredState) { s.Backup.Bucket = ".abc" }},
		{"invalid bucket suffix", func(s *protocol.DesiredState) { s.Backup.Bucket = "abc-" }},
		{"empty region", func(s *protocol.DesiredState) { s.Backup.Region = "" }},
		{"invalid region", func(s *protocol.DesiredState) { s.Backup.Region = "us_test" }},
		{"nonpositive interval", func(s *protocol.DesiredState) { s.Backup.Settings.EveryMinutes = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put(linux.BackupDir+"/acme.yml", "configuration", 0o600)
			f.put(linux.BackupDir+"/acme.env", "credentials", 0o600)
			f.put(linux.BackupDir+"/meta/acme/deep/data", "metadata", 0o600)
			f.put("/untouched/outside", "leave intact", 0o644)
			if err := os.Symlink(f.path("/untouched"), f.path(linux.BackupDir+"/meta/acme/link")); err != nil {
				t.Fatal(err)
			}
			tc.invalidate(&f.state)
			f.backup()
			f.stream()
			var got [][]string
			for _, c := range f.backupCommands {
				got = append(got, c.Args)
			}
			want := [][]string{{"is-active", "accountsd-litestream@acme.service"}, {"stop", "accountsd-litestream@acme.service"}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("operations = %v, want %v", got, want)
			}
			for _, p := range []string{"/acme.yml", "/acme.env", "/meta/acme"} {
				f.expectAbsent(linux.BackupDir + p)
			}
			f.expectFile("/untouched/outside", "leave intact", 0o644)
		})
	}
}

func TestBackupsPreserveUncredentialedAccounts(t *testing.T) {
	// R-Y7ZO-7BTH
	f := newBackupPassFixture(t)
	f.state.Backup.Host.AccessKeyID = "invalid!key"
	f.state.Accounts = append(f.state.Accounts, protocol.Account{Slug: "bravo", State: protocol.AccountActive})
	bad := f.state.Backup.Accounts[0].Credentials
	bad.SessionToken = ""
	f.state.Backup.Accounts = append(f.state.Backup.Accounts, protocol.AccountCredentials{Account: "bravo", Credentials: bad})
	f.put(dataPrivateKey, "key", 0o600)
	f.put(dataPublicKey, "public", 0o644)
	f.put("/srv/acme/data/a.db", walSQLite, 0o600)
	f.put("/srv/bravo/data/b.db", walSQLite, 0o600)
	f.put(linux.BackupDir+"/bravo.yml", "configuration", 0o600)
	f.put(linux.BackupDir+"/bravo.env", "credentials", 0o600)
	f.stream()
	f.backup()
	for _, c := range f.backupCommands {
		if strings.Contains(strings.Join(c.Args, " "), "bravo") || strings.Contains(strings.Join(c.Args, " "), f.state.Backup.HostPrefix) {
			t.Fatalf("operation for unusable credentials: %v", c.Args)
		}
	}
	f.expectFile(linux.BackupDir+"/bravo.yml", "configuration", 0o600)
	f.expectFile(linux.BackupDir+"/bravo.env", "credentials", 0o600)
	report := f.report()
	if got := report.Backups[1]; got.Error != machine.NoCredentials || len(got.Databases) != 1 || report.HostBackup.Error != machine.NoCredentials {
		t.Fatalf("account report = %+v, host = %+v", got, report.HostBackup)
	}
}

func TestAccountResticWaitsForItsWalk(t *testing.T) {
	// R-6M4L-VEX6
	f := newBackupPassFixture(t)
	f.stream()
	f.host = linux.New(f.env)
	f.backupCommands = nil
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic reused an earlier host's walk: %v", got)
	}
	configured := f.state.Backup
	f.state.Backup = nil
	f.stream()
	f.state.Backup = configured
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic before walk: %v", got)
	}
	f.stream()
	f.state.Accounts = append(f.state.Accounts, protocol.Account{Slug: "bravo", State: protocol.AccountActive})
	f.state.Backup.Accounts = append(f.state.Backup.Accounts, protocol.AccountCredentials{Account: "bravo", Credentials: configured.Host})
	f.backup()
	got := f.commandsFor(f.path(linux.ResticPath))
	if len(got) == 0 || !slices.Equal(got[0].Args[3:], []string{"cat", "config"}) {
		t.Fatalf("restic after walk: %v", got)
	}
	for _, c := range got {
		if strings.Contains(c.Args[1], "bravo") {
			t.Fatalf("restic ran for newly added account: %v", c.Args)
		}
	}
	deep := "/srv/bravo/data/" + strings.Repeat("deep/", linux.MaxDepth+1)
	if err := os.MkdirAll(f.path(deep), 0o700); err != nil {
		t.Fatal(err)
	}
	f.stream()
	if row := f.report().Backups[1]; row.Error != machine.DataTooLarge {
		t.Fatalf("bravo walk = %+v", row)
	}
	f.backupCommands = nil
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) == 0 || !strings.Contains(got[0].Args[1], "/bravo/") || !slices.Equal(got[0].Args[3:], []string{"cat", "config"}) {
		t.Fatalf("restic after a walk hit a bound = %v", got)
	}
	active := f.state.Accounts
	f.state.Accounts = nil
	f.stream()
	f.state.Accounts = active
	f.at = f.at.Add(time.Hour)
	f.backupCommands = nil
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic reused a walk for an account absent from the last stream pass: %v", got)
	}
}

func TestDatabaseDiscoveryUsesValidRegularSQLiteFiles(t *testing.T) {
	// R-6OKE-MYEK
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/z.db", plainSQLite, 0o660)
	f.put("/srv/acme/data/mixed.db", "SQLite format 3\x00\x00\x00\x02\x01", 0o600)
	f.put("/srv/acme/data/a.db", walSQLite, 0o640)
	f.put("/srv/acme/data/b.db", "not a database", 0o600)
	f.put("/srv/acme/data/invalid\x01name", walSQLite, 0o600)
	f.put("/srv/acme/data/invalid\xffname", walSQLite, 0o600)
	f.put("/outside/linked.db", walSQLite, 0o600)
	if err := os.Symlink(f.path("/srv/acme/data/a.db"), f.path("/srv/acme/data/linked.db")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.path("/outside"), f.path("/srv/acme/data/linked-dir")); err != nil {
		t.Fatal(err)
	}
	before := f.stat("/srv/acme/data/a.db")
	f.stream()
	got := f.report().Backups[0]
	if len(got.Databases) != 3 || got.Databases[0].Path != "data/a.db" || got.Databases[0].Error != "" ||
		got.Databases[1].Path != "data/mixed.db" || got.Databases[1].Error != machine.NotWAL ||
		got.Databases[2].Path != "data/z.db" || got.Databases[2].Error != machine.NotWAL || got.DatabaseTotal != 3 || got.DatabaseErrors != 2 {
		t.Fatalf("walk report = %+v", got)
	}
	config, err := os.ReadFile(f.path(linux.BackupDir + "/acme.yml"))
	if err != nil || !strings.Contains(string(config), "/srv/acme/data/a.db") || strings.Contains(string(config), "/srv/acme/data/z.db") || strings.Contains(string(config), "/srv/acme/data/mixed.db") {
		t.Fatalf("config = %s, %v", config, err)
	}
	assertUnchanged(t, f.path("/srv/acme/data/a.db"), before)
	f.expectFile("/srv/acme/data/a.db", walSQLite, 0o640)
	f.expectFile("/srv/acme/data/z.db", plainSQLite, 0o660)
	if err := os.Rename(f.path("/srv/acme/data"), f.path("/outside/original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.path("/outside/original"), f.path("/srv/acme/data")); err != nil {
		t.Fatal(err)
	}
	f.stream()
	if got := f.report().Backups[0]; len(got.Databases) != 0 || got.DatabaseTotal != 0 {
		t.Fatalf("walk followed a symlink at the data directory: %+v", got)
	}
}

func TestDatabaseWalkBoundsKeepPriorCompletedFacts(t *testing.T) {
	// R-6PSB-0Q59
	f := newBackupPassFixture(t)
	for i := range linux.MaxDatabasesPerAccount + 1 {
		f.put(fmt.Sprintf("/srv/acme/data/db%03d", i), walSQLite, 0o600)
	}
	f.stream()
	before := f.report().Backups[0]
	if before.DatabaseTotal != linux.MaxDatabasesPerAccount+1 || len(before.Databases) != linux.MaxDatabasesPerAccount ||
		before.DatabaseErrors != 1 || before.Error != machine.TooManyDatabases {
		t.Fatalf("overflow report = %+v", before)
	}
	if err := os.MkdirAll(f.path("/srv/acme/data/"+strings.Repeat("deep/", linux.MaxDepth+1)), 0o700); err != nil {
		t.Fatal(err)
	}
	f.stream()
	after := f.report().Backups[0]
	before.Error = machine.DataTooLarge
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("depth overflow report changed the retained facts: %+v", after)
	}
}

func TestDepthLimitedWalkRejectsUnreadableFallback(t *testing.T) {
	// R-6M4L-VEX6 R-6PSB-0Q59 R-5VAT-GGLW
	for _, corruption := range []string{"oversized", "missing", "stale"} {
		t.Run(corruption, func(t *testing.T) {
			f := newBackupPassFixture(t)
			const status = linux.BackupDir + "/status/acme.json"
			f.put("/srv/acme/data/old.db", walSQLite, 0o660)
			f.stream()
			older, err := os.ReadFile(f.path(status))
			if err != nil {
				t.Fatal(err)
			}
			f.put("/srv/acme/data/new.db", walSQLite, 0o660)
			f.stream()
			switch corruption {
			case "oversized":
				newer, err := os.ReadFile(f.path(status))
				if err != nil {
					t.Fatal(err)
				}
				f.put(status, string(newer)+strings.Repeat(" ", linux.MaxCapture), 0o600)
				if got := f.report().Backups[0]; len(got.Databases) != 0 || got.DatabaseTotal != 0 {
					t.Fatalf("Inspect retained oversized facts: %+v", got)
				}
			case "missing":
				if err := os.Remove(f.path(status)); err != nil {
					t.Fatal(err)
				}
			case "stale":
				f.put(status, string(older), 0o600)
			}
			if err := os.MkdirAll(f.path("/srv/acme/data/"+strings.Repeat("deep/", linux.MaxDepth+1)), 0o700); err != nil {
				t.Fatal(err)
			}
			f.stream()
			f.backupCommands = nil
			f.backup()
			if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
				t.Fatalf("restic used an unreadable fallback: %v", got)
			}
			if got := f.report().Backups[0]; got.Error != machine.DataTooLarge || len(got.Databases) != 0 ||
				got.DatabaseTotal != 0 || got.DatabaseErrors != 0 {
				t.Fatalf("limited walk retained unreadable facts: %+v", got)
			}
		})
	}
}

func TestDepthLimitedWalkUsesOnlyKeptFacts(t *testing.T) {
	// R-6M4L-VEX6 R-6PSB-0Q59
	for _, dir := range []string{linux.BackupDir, linux.BackupDir + "/status"} {
		t.Run(dir, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put("/srv/acme/data/old.db", walSQLite, 0o660)
			f.stream()
			previous := f.report().Backups[0]

			restore := f.obstructBackupDirectory(dir)
			f.put("/srv/acme/data/old.db", plainSQLite, 0o660)
			f.put("/srv/acme/data/new.db", walSQLite, 0o660)
			if err := f.host.Stream(t.Context(), f.state); err == nil {
				t.Fatal("Stream accepted an unwritable status directory")
			}
			restore()
			if err := os.Remove(f.path("/srv/acme/data/new.db")); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(f.path("/srv/acme/data/"+strings.Repeat("deep/", linux.MaxDepth+1)), 0o700); err != nil {
				t.Fatal(err)
			}
			f.at = f.at.Add(linux.InstallRetry)
			f.stream()
			f.backupCommands = nil
			f.backup()
			if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
				t.Fatalf("restic ignored the intervening unkept walk: %v", got)
			}
			previous.Error = machine.DataTooLarge
			if got := f.report().Backups[0]; !reflect.DeepEqual(got, previous) {
				t.Fatalf("limited walk after an unkept walk = %+v, want %+v", got, previous)
			}

			f.host = linux.New(f.env)
			f.backup()
			if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
				t.Fatalf("restic used facts before a walk by this host: %v", got)
			}
			f.stream()
			if got := f.report().Backups[0]; !reflect.DeepEqual(got, previous) {
				t.Fatalf("limited walk after restart = %+v, want %+v", got, previous)
			}
			f.backup()
			f.requireResticConfig("acme")
			f.expectFile(linux.BackupDir+"/manifest/acme", fmt.Sprintf("%d %d 0660 data/old.db\n", os.Getuid(), os.Getgid()), 0o600)
		})
	}
}

func TestCompleteWalkRestoresEligibilityAfterLimitedWalk(t *testing.T) {
	// R-6M4L-VEX6
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/old.db", walSQLite, 0o600)
	f.stream()
	restore := f.obstructBackupDirectory(linux.BackupDir)
	f.put("/srv/acme/data/old.db", plainSQLite, 0o600)
	f.put("/srv/acme/data/new.db", walSQLite, 0o660)
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("Stream accepted an unwritable status directory")
	}
	restore()
	deep := f.path("/srv/acme/data/deep")
	if err := os.MkdirAll(deep+"/"+strings.Repeat("more/", linux.MaxDepth), 0o700); err != nil {
		t.Fatal(err)
	}
	f.at = f.at.Add(linux.InstallRetry)
	f.stream()
	f.backupCommands = nil
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic ignored the intervening unkept walk: %v", got)
	}
	if err := os.RemoveAll(deep); err != nil {
		t.Fatal(err)
	}
	f.stream()
	f.backup()
	f.requireResticConfig("acme")
	f.expectFile(linux.BackupDir+"/manifest/acme", fmt.Sprintf("%d %d 0660 data/new.db\n", os.Getuid(), os.Getgid()), 0o600)
}

func TestDepthLimitedWalkUsesSavedStatusAfterRestart(t *testing.T) {
	// R-6M4L-VEX6 R-6PSB-0Q59 R-5VAT-GGLW
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o660)
	f.stream()
	f.host = linux.New(f.env)
	if err := os.MkdirAll(f.path("/srv/acme/data/"+strings.Repeat("deep/", linux.MaxDepth+1)), 0o700); err != nil {
		t.Fatal(err)
	}
	f.stream()
	f.backup()
	f.requireResticConfig("acme")
	f.expectFile(linux.BackupDir+"/manifest/acme", fmt.Sprintf("%d %d 0660 data/app.db\n", os.Getuid(), os.Getgid()), 0o600)
}

func TestObstructedWalkFromPreviousPlacementIsNotReused(t *testing.T) {
	// R-6M4L-VEX6 R-6PSB-0Q59 R-5RN4-B5DT
	for _, omitted := range []bool{false, true} {
		t.Run(fmt.Sprintf("previous placement omitted %t", omitted), func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put("/srv/acme/data/app.db", walSQLite, 0o660)
			f.stream()
			f.host = linux.New(f.env)
			restore := f.obstructBackupDirectory(linux.BackupDir)
			if err := f.host.Stream(t.Context(), f.state); err == nil {
				t.Fatal("Stream accepted an unwritable backup directory")
			}
			restore()
			f.at = f.at.Add(linux.InstallRetry)

			if omitted {
				withoutAccount := f.state
				withoutAccount.Accounts = nil
				f.state.Version++
				withoutAccount.Version = f.state.Version
				if err := f.host.Stream(t.Context(), withoutAccount); err != nil {
					t.Fatal(err)
				}
			}
			f.state.Accounts[0].GID++
			f.state.Version++
			if got := f.report().Backups[0]; got.DatabaseTotal != 0 || len(got.Databases) != 0 {
				t.Fatalf("the new account reported paths from the previous placement: %+v", got)
			}
			if err := os.MkdirAll(f.path("/srv/acme/data/"+strings.Repeat("deep/", linux.MaxDepth+1)), 0o700); err != nil {
				t.Fatal(err)
			}
			f.stream()
			f.backup()
			f.requireResticConfig("acme")
			f.expectFile(linux.BackupDir+"/manifest/acme", "", 0o600)
		})
	}
}

func TestNewPlacementCannotUsePreviousPlacementsWalk(t *testing.T) {
	// R-6M4L-VEX6 R-6PSB-0Q59 R-5RN4-B5DT
	for _, restarted := range []bool{false, true} {
		t.Run(fmt.Sprintf("restarted %t", restarted), func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put("/srv/acme/data/old.db", walSQLite, 0o660)
			f.stream()
			f.state.Accounts[0].GID++
			f.state.Version++
			if restarted {
				f.host = linux.New(f.env)
			}
			f.backupCommands = nil
			f.backup()
			if got := f.resticVerbs(); len(got) != 0 {
				t.Fatalf("the new account used the previous placement's walk: %v", got)
			}
			if got := f.report().Backups[0]; got.DatabaseTotal != 0 || len(got.Databases) != 0 {
				t.Fatalf("the new account reported paths from the previous placement: %+v", got)
			}
			if err := os.MkdirAll(f.path("/srv/acme/data/"+strings.Repeat("deep/", linux.MaxDepth+1)), 0o700); err != nil {
				t.Fatal(err)
			}
			f.stream()
			if got := f.report().Backups[0]; got.Error != machine.DataTooLarge || got.DatabaseTotal != 0 || len(got.Databases) != 0 {
				t.Fatalf("the new account retained paths from the previous placement: %+v", got)
			}
			f.backup()
			f.requireResticConfig("acme")
			f.expectFile(linux.BackupDir+"/manifest/acme", "", 0o600)
		})
	}
}

func TestWithholdingSurvivesAStreamPassWithNoBackupMember(t *testing.T) {
	// R-6M4L-VEX6
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/kept.db", walSQLite, 0o660)
	f.stream()
	restore := f.obstructBackupDirectory(linux.BackupDir)
	f.put("/srv/acme/data/unkept.db", walSQLite, 0o660)
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("Stream accepted an unwritable backup directory")
	}
	restore()

	skipped := f.state
	skipped.Backup = nil
	f.at = f.at.Add(linux.InstallRetry)
	if err := f.host.Stream(t.Context(), skipped); err != nil {
		t.Fatal(err)
	}
	deep := f.path("/srv/acme/data/deep")
	if err := os.MkdirAll(deep+"/"+strings.Repeat("more/", linux.MaxDepth), 0o700); err != nil {
		t.Fatal(err)
	}
	f.stream()
	f.backupCommands = nil
	f.backup()
	if got := f.resticVerbs(); len(got) != 0 {
		t.Fatalf("restic ran after a limited walk of an account with an unkept walk: %v", got)
	}
	if err := os.RemoveAll(deep); err != nil {
		t.Fatal(err)
	}
	f.stream()
	f.backup()
	f.requireResticConfig("acme")
}

func TestDatabaseWalkStopsAtVisitLimit(t *testing.T) {
	// R-6PSB-0Q59
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/legacy.db", plainSQLite, 0o600)
	data := f.path("/srv/acme/data")
	addEntry := func(i int) {
		t.Helper()
		if err := os.Symlink("missing", filepath.Join(data, fmt.Sprintf("link%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	for i := range linux.MaxVisit - 1 {
		addEntry(i)
	}
	f.stream()
	want := f.report().Backups[0]
	if want.Error != "" || want.DatabaseTotal != 1 || len(want.Databases) != 1 || want.Databases[0].Path != "data/legacy.db" {
		t.Fatalf("walk at visit limit = %+v", want)
	}
	addEntry(linux.MaxVisit - 1)
	f.stream()
	want.Error = machine.DataTooLarge
	if got := f.report().Backups[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("walk one entry over visit limit = %+v, want %+v", got, want)
	}
}

func TestLitestreamWritesExactProtectedConfigAndRestartsOnSchedule(t *testing.T) {
	// R-YIYR-N9HQ R-5FG4-HFYV R-YLEK-ESZ4 R-5GO0-V7PK
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/z.db", walSQLite, 0o600)
	f.put("/srv/acme/data/a\".db", walSQLite, 0o600)
	b := f.state.Backup
	b.Accounts[0].Credentials.AccessKeyID = "accountKey"
	b.Accounts[0].Credentials.SecretAccessKey = "accountSecret"
	b.Accounts[0].Credentials.SessionToken = "accountToken"
	initial := b.Accounts[0].Credentials
	const envPath = linux.BackupDir + "/acme.env"
	checkEnv := func(c protocol.Credentials) {
		t.Helper()
		want := fmt.Sprintf("# expires %s\n# litestream %s\nAWS_ACCESS_KEY_ID=%s\nAWS_SECRET_ACCESS_KEY=%s\nAWS_SESSION_TOKEN=%s\nAWS_REGION=%s\n",
			c.Expires.UTC().Format(time.RFC3339), linux.LitestreamDigest, c.AccessKeyID, c.SecretAccessKey, c.SessionToken, b.Region)
		f.expectFile(envPath, want, 0o600)
	}
	firstProbe := true
	checkRestart := func(c linux.Command) (linux.Result, error) {
		if firstProbe && c.Name == "systemctl" && slices.Equal(c.Args, []string{"is-active", "accountsd-litestream@acme.service"}) {
			f.expectAbsent(envPath)
			firstProbe = false
		}
		if c.Name == "systemctl" && slices.Equal(c.Args, []string{"restart", "accountsd-litestream@acme.service"}) {
			checkEnv(b.Accounts[0].Credentials)
		}
		if c.Name == "systemctl" && slices.Equal(c.Args, []string{"is-active", "accountsd-litestream@inactive.service"}) {
			return linux.Result{ExitCode: 3}, nil
		}
		return linux.Result{}, nil
	}
	f.execute = checkRestart
	f.stream()
	f.expectFile(linux.BackupDir+"/acme.yml", `snapshot:
  interval: 24h
  retention: 840h
shutdown-sync-timeout: 30s
dbs:
  - path: "/srv/acme/data/a\".db"
    meta-path: "/var/lib/accountsd/backup/meta/acme/data/a\".db"
    replica:
      type: s3
      bucket: "fixture-bucket"
      path: "accounts/acme/data/a\".db"
      region: "us-test-2"
  - path: "/srv/acme/data/z.db"
    meta-path: "/var/lib/accountsd/backup/meta/acme/data/z.db"
    replica:
      type: s3
      bucket: "fixture-bucket"
      path: "accounts/acme/data/z.db"
      region: "us-test-2"
`, 0o600)
	f.expectFile("/etc/systemd/system/"+linux.LitestreamUnit, `[Unit]
RequiresMountsFor=/srv

[Service]
EnvironmentFile=/var/lib/accountsd/backup/%i.env
ExecStart=/usr/local/bin/litestream replicate -config /var/lib/accountsd/backup/%i.yml -no-expand-env
Restart=always
RestartSec=5
TimeoutStopSec=60
`, 0o644)
	checkEnv(initial)
	unit := f.commandsFor("systemctl")
	if len(unit) != 3 || !slices.Equal(unit[0].Args, []string{"daemon-reload"}) || unit[1].Args[0] != "is-active" || unit[2].Args[0] != "restart" {
		t.Fatalf("initial systemctl calls = %v", unit)
	}
	expiry := initial.Expires
	b.Accounts[0].Credentials.AccessKeyID = "newKey"
	b.Accounts[0].Credentials.Expires = expiry.Add(time.Hour)
	f.backupCommands = nil
	f.at = expiry.Add(-linux.RestartBefore - time.Second)
	oldEnv := snapshotApplyHost(t, f.root)[envPath]
	f.stream()
	if got := f.commandsFor("systemctl"); len(got) != 1 || got[0].Args[0] != "is-active" {
		t.Fatalf("early rotation commands = %v", got)
	}
	if got := snapshotApplyHost(t, f.root)[envPath]; !reflect.DeepEqual(got, oldEnv) {
		t.Fatal("environment file changed without a restart")
	}
	checkEnv(initial)
	f.at = expiry.Add(-linux.RestartBefore + time.Second)
	f.backupCommands = nil
	f.stream()
	if got := f.commandsFor("systemctl"); len(got) != 2 || got[1].Args[0] != "restart" {
		t.Fatalf("rotation commands = %v", got)
	}
	checkEnv(b.Accounts[0].Credentials)
	currentEnv, err := os.ReadFile(f.path(envPath))
	if err != nil {
		t.Fatal(err)
	}
	f.put(envPath, strings.Replace(string(currentEnv), linux.LitestreamDigest, "old stamp", 1), 0o600)
	f.backupCommands = nil
	f.stream()
	if got := f.commandsFor("systemctl"); len(got) != 2 || got[0].Args[0] != "is-active" || got[1].Args[0] != "restart" {
		t.Fatalf("stale environment stamp commands = %v", got)
	}
	f.put("/untouched/config", "do not follow", 0o600)
	if err := os.Symlink(f.path("/untouched/config"), f.path(linux.BackupDir+"/inactive.yml")); err != nil {
		t.Fatal(err)
	}
	f.put(linux.BackupDir+"/inactive.env", "old environment", 0o600)
	f.put(linux.BackupDir+"/meta/inactive/file", "old metadata", 0o600)
	f.state.Accounts = nil
	f.backupCommands = nil
	f.stream()
	var staleCalls []string
	for _, c := range f.commandsFor("systemctl") {
		staleCalls = append(staleCalls, strings.Join(c.Args, " "))
	}
	slices.Sort(staleCalls)
	if !slices.Equal(staleCalls, []string{"is-active accountsd-litestream@acme.service", "is-active accountsd-litestream@inactive.service", "stop accountsd-litestream@acme.service"}) {
		t.Fatalf("stale unit commands = %v", staleCalls)
	}
	for _, path := range []string{"/acme.yml", "/acme.env", "/meta/acme", "/inactive.yml", "/inactive.env", "/meta/inactive"} {
		f.expectAbsent(linux.BackupDir + path)
	}
	f.expectFile("/untouched/config", "do not follow", 0o600)
}

func TestLitestreamRecordsReplicaHealthOnlyAfterThreeAnswers(t *testing.T) {
	// R-5HVX-8ZG9 R-5RN4-B5DT
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/one.db", walSQLite, 0o600)
	f.put("/srv/acme/data/two.db", walSQLite, 0o600)
	f.put("/srv/acme/data/legacy.db", plainSQLite, 0o600)
	phase := 0
	f.execute = func(c linux.Command) (linux.Result, error) {
		if c.Name != f.path(linux.LitestreamPath) {
			return linux.Result{}, nil
		}
		if len(c.Args) < 6 {
			t.Fatalf("litestream command = %v", c.Args)
		}
		creds := f.state.Backup.Accounts[0].Credentials
		want := []string{"AWS_ACCESS_KEY_ID=" + creds.AccessKeyID, "AWS_SECRET_ACCESS_KEY=" + creds.SecretAccessKey,
			"AWS_SESSION_TOKEN=" + creds.SessionToken, "AWS_REGION=" + f.state.Backup.Region}
		if !slices.Equal(c.Env, want) {
			t.Fatalf("litestream env = %v, want %v", c.Env, want)
		}
		db := filepath.Base(c.Args[len(c.Args)-1])
		configPath, dbPath := f.path(linux.BackupDir+"/acme.yml"), f.path("/srv/acme/data/"+db)
		if db != "one.db" && db != "two.db" {
			t.Fatalf("litestream database = %s", db)
		}
		switch {
		case slices.Equal(c.Args, []string{"status", "-config", configPath, "-no-expand-env", "-json", dbPath}):
			f.output(c, fmt.Sprintf(`[{"database":"/srv/acme/data/%s","status":"ok","local_txid":"0000000000000010"}]`, db))
		case slices.Equal(c.Args, []string{"ltx", "-config", configPath, "-no-expand-env", "-level", "0", "-json", dbPath}):
			if phase == 3 {
				f.output(c, "[]")
				break
			}
			tx := "0f"
			if db == "one.db" && phase == 0 || db == "two.db" && phase == 1 {
				tx = "10"
			}
			latest := "2026-11-26T06:00:00Z"
			if db == "two.db" {
				latest = "2026-11-26T07:00:00Z"
			}
			f.output(c, fmt.Sprintf(`[{"timestamp":"2026-11-26T05:00:00Z","max_txid":%q},{"timestamp":%q,"max_txid":"01"}]`, tx, latest))
		case slices.Equal(c.Args, []string{"ltx", "-config", configPath, "-no-expand-env", "-level", "9", "-json", dbPath}):
			if phase == 2 {
				return linux.Result{ExitCode: 1}, nil
			}
			if phase == 3 {
				f.output(c, "[]")
				break
			}
			f.output(c, `[{"timestamp":"2026-11-15T05:00:00Z"},{"timestamp":"2026-11-14T05:00:00Z"}]`)
		default:
			t.Fatalf("unexpected litestream arguments %v", c.Args)
		}
		return linux.Result{}, nil
	}
	f.stream()
	first := f.at.UTC().Truncate(time.Second)
	firstReport := f.report().Backups[0]
	if len(firstReport.Databases) != 3 || !firstReport.Databases[1].Synced.Equal(first) || !firstReport.Databases[2].Synced.IsZero() {
		t.Fatalf("sync progress before the remote replica caught up = %+v", firstReport)
	}
	if got := f.commandsFor(f.path(linux.LitestreamPath)); len(got) != 6 ||
		got[0].Args[0] != "status" || got[1].Args[5] != "0" || got[2].Args[5] != "9" ||
		got[3].Args[0] != "status" || got[4].Args[5] != "0" || got[5].Args[5] != "9" {
		t.Fatalf("health command order = %v", got)
	}
	phase++
	f.at = f.at.Add(time.Minute)
	f.stream()
	second := f.at.UTC().Truncate(time.Second)
	before := f.report().Backups[0]
	oldest := time.Date(2026, 11, 14, 5, 0, 0, 0, time.UTC)
	written := time.Date(2026, 11, 26, 6, 0, 0, 0, time.UTC)
	latest := written.Add(time.Hour)
	if len(before.Databases) != 3 || before.DatabaseTotal != 3 || before.DatabaseErrors != 1 || before.Databases[0].Error != machine.NotWAL ||
		before.Databases[1].Oldest != oldest || before.Databases[1].Written != written || before.Databases[1].Synced != first ||
		before.Databases[2].Written != latest || before.Databases[2].Synced != second || before.Synced != first || before.Written != latest {
		t.Fatalf("replica health = %+v", before)
	}
	phase++
	f.at = f.at.Add(time.Minute)
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("failed ltx returned nil")
	}
	failed := before
	failed.Error = "litestream ltx: exit status 1"
	if after := f.report().Backups[0]; !reflect.DeepEqual(failed, after) {
		t.Fatalf("failed health commands changed facts: %+v", after)
	}
	phase++
	f.at = f.at.Add(time.Minute)
	f.stream()
	for i := 1; i < len(before.Databases); i++ {
		before.Databases[i].Oldest = time.Time{}
	}
	if after := f.report().Backups[0]; !reflect.DeepEqual(before, after) {
		t.Fatalf("empty replica listings did not clear only the oldest timestamp: %+v", after)
	}
}

func TestResticAccountCommandsAndReports(t *testing.T) {
	// R-5J3T-MR6Y R-5KBQ-0IXN R-5LJM-EAOC R-YTXV-375Z R-5MRI-S2F1 R-5RN4-B5DT R-5VAT-GGLW
	f := newBackupPassFixture(t)
	f.state.Host = "host01"
	f.state.Backup.Settings.CheckDays = 2
	f.state.Users = []protocol.User{
		{Name: "zulu", Account: "acme", State: protocol.UserActive},
		{Name: "alpha", Account: "acme", State: protocol.UserLocked},
		{Name: "linked", Account: "acme", State: protocol.UserActive},
		{Name: "absent", Account: "acme", State: protocol.UserActive},
		{Name: "deleted", Account: "acme", State: protocol.UserDeleted},
		{Name: "other", Account: "bravo", State: protocol.UserActive},
	}
	f.put("/home/zulu/file", "z", 0o600)
	f.put("/home/alpha/file", "a", 0o600)
	f.put("/home/deleted/file", "deleted", 0o600)
	f.put("/home/other/file", "other", 0o600)
	if err := os.Symlink(f.path("/home/zulu"), f.path("/home/linked")); err != nil {
		t.Fatal(err)
	}
	name := "data/app[1]?.db"
	const other = "data/z.db"
	const vanished = "/srv/acme/data/vanished.db"
	const manifest = linux.BackupDir + "/manifest/acme"
	f.put("/srv/acme/"+other, walSQLite, 0o600)
	f.put("/srv/acme/"+name, walSQLite, 0o640)
	f.setMode("/srv/acme/"+name, os.ModeSetgid|0o640)
	f.put(vanished, walSQLite, 0o600)
	f.put("/srv/acme/data/legacy.db", plainSQLite, 0o600)
	f.stream()
	if err := os.Remove(f.path(vanished)); err != nil {
		t.Fatal(err)
	}
	manifestLine := func(path, mode string) string {
		t.Helper()
		stat, ok := f.stat("/srv/acme/" + path).Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("missing ownership metadata")
		}
		return fmt.Sprintf("%d %d %s %s\n", stat.Uid, stat.Gid, mode, path)
	}
	wantManifest := manifestLine(name, "2640") + manifestLine(other, "0600")
	f.backupCommands = nil
	f.execute = func(c linux.Command) (linux.Result, error) {
		switch c.Args[3] {
		case "cat":
			f.expectAbsent(manifest)
			return linux.Result{ExitCode: 10}, nil
		case "init":
			f.expectAbsent(manifest)
		case "backup":
			f.expectFile(manifest, wantManifest, 0o600)
			f.output(c, `{"message_type":"summary","snapshot_id":"previous","backup_end":"2000-01-01T00:00:00Z"}`+"\n")
			f.summary(c)
			f.output(c, `{"message_type":"summary","snapshot_id":"","backup_end":"2000-01-01T00:00:00Z"}`+"\n")
		case "snapshots":
			f.output(c, "[")
			for i := range 40 {
				if i != 0 {
					f.output(c, ",")
				}
				f.output(c, fmt.Sprintf(`{"time":%q}`, f.at.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano)))
			}
			f.output(c, "]")
		}
		return linux.Result{}, nil
	}
	f.backup()
	var wantArgs = [][]string{{"cat", "config"}, {"init"}}
	snapshot := []string{"backup", "--json", "--host", "acme"}
	for _, path := range []string{`data/app\[1]\?.db`, other} {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			snapshot = append(snapshot, "--exclude", f.path("/srv/acme/"+path)+suffix)
		}
	}
	for _, home := range []string{"/home/alpha", "/home/zulu"} {
		for _, suffix := range []string{"/.cache", "/.npm", "/.local/share/claude", "/.local/state/claude"} {
			snapshot = append(snapshot, "--exclude", f.path(home+suffix))
		}
	}
	snapshot = append(snapshot, f.path("/srv/acme"), f.path("/home/alpha"), f.path("/home/zulu"), f.path(linux.BackupDir+"/manifest/acme"))
	wantArgs = append(wantArgs, snapshot,
		[]string{"forget", "--host", "acme", "--group-by", "host", "--keep-hourly", "24", "--keep-daily", "35", "--keep-monthly", "12", "--prune"},
		[]string{"snapshots", "--host", "acme", "--json"}, []string{"check", "--read-data-subset=" + linux.CheckSubset})
	got := f.commandsFor(f.path(linux.ResticPath))
	if len(got) != len(wantArgs) {
		t.Fatalf("restic commands = %v", got)
	}
	creds := f.state.Backup.Accounts[0].Credentials
	env := []string{"AWS_ACCESS_KEY_ID=" + creds.AccessKeyID, "AWS_SECRET_ACCESS_KEY=" + creds.SecretAccessKey, "AWS_SESSION_TOKEN=" + creds.SessionToken,
		"AWS_DEFAULT_REGION=us-test-2", "RESTIC_CACHE_DIR=" + f.path(linux.ResticCache+"/acme")}
	for i, c := range got {
		want := append([]string{"-r", "s3:s3.us-test-2.amazonaws.com/fixture-bucket/accounts/acme/files", "--insecure-no-password"}, wantArgs[i]...)
		if !slices.Equal(c.Args, want) || !slices.Equal(c.Env, env) {
			t.Fatalf("restic command %d = %v, env = %v, want %v, %v", i, c.Args, c.Env, want, env)
		}
	}
	f.expectFile(manifest, wantManifest, 0o600)
	before := f.report().Backups[0]
	at := f.at.UTC().Truncate(time.Second)
	var snapshots []int64
	for i := 39; i >= 8; i-- {
		snapshots = append(snapshots, f.at.Unix()+int64(i))
	}
	if before.Snapshot != at || before.Checked != at || !slices.Equal(before.Snapshots, snapshots) || before.Error != "" {
		t.Fatalf("backup facts = %+v", before)
	}
	f.host = linux.New(f.env)
	if after := f.report().Backups[0]; !reflect.DeepEqual(before, after) {
		t.Fatalf("after restart: %+v", after)
	}
	f.execute = nil
	f.stream()
	f.backupCommands = nil
	f.at = f.at.Add(time.Minute)
	f.backup()
	if len(f.backupCommands) != 0 {
		t.Fatalf("restic ran before schedule after restart: %v", f.backupCommands)
	}
}

func TestResticErrorContinuesOtherAccountsAndUnlocksOnce(t *testing.T) {
	// R-5NZF-5U5Q R-5P7B-JLWF R-5LJM-EAOC R-5RN4-B5DT
	f := newBackupPassFixture(t)
	f.put(dataPrivateKey, "host private", 0o600)
	f.put(dataPublicKey, "host public", 0o644)
	for _, slug := range []string{"delta", "charlie", "bravo"} {
		f.state.Accounts = append(f.state.Accounts, protocol.Account{Slug: slug, State: protocol.AccountActive})
		f.state.Backup.Accounts = append(f.state.Backup.Accounts, protocol.AccountCredentials{Account: slug, Credentials: f.state.Backup.Host})
	}
	f.state.Accounts = append(f.state.Accounts, protocol.Account{Slug: "removed", State: protocol.AccountRemoved})
	f.stream()
	f.execute = func(c linux.Command) (linux.Result, error) {
		verb := c.Args[3]
		if verb == "backup" {
			f.summary(c)
		}
		switch {
		case strings.Contains(c.Args[1], "/acme/") && verb == "forget":
			return linux.Result{ExitCode: 11, Stderr: []byte("first\nlast\rline\n \t\r\n")}, nil
		case verb == "unlock":
			return linux.Result{ExitCode: 7}, nil
		case strings.Contains(c.Args[1], "/bravo/") && verb == "backup":
			return linux.Result{ExitCode: 3}, nil
		case strings.Contains(c.Args[1], "/charlie/") && verb == "forget":
			return linux.Result{ExitCode: 5}, nil
		default:
			return linux.Result{}, nil
		}
	}
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("backup failure returned nil")
	}
	commands := make(map[string][]string)
	restic := f.commandsFor(f.path(linux.ResticPath))
	for i, c := range restic {
		repo := c.Args[1]
		slug := filepath.Base(filepath.Dir(repo))
		commands[slug] = append(commands[slug], c.Args[3])
		if c.Args[3] == "unlock" {
			if i == 0 || len(c.Args) != 4 || !slices.Equal(c.Args[:3], restic[i-1].Args[:3]) || !slices.Equal(c.Env, restic[i-1].Env) {
				t.Fatalf("unlock did not follow the failed command with its repository arguments and environment: %v", c.Args)
			}
		}
	}
	for slug, want := range map[string][]string{
		"acme": {"cat", "backup", "forget", "unlock"}, "bravo": {"cat", "backup", "forget", "snapshots"},
		"charlie": {"cat", "backup", "forget"}, "delta": {"cat", "backup", "forget", "snapshots"},
		f.state.Host: {"cat", "backup", "forget"},
	} {
		if !slices.Equal(commands[slug], want) {
			t.Fatalf("commands for %s = %v, want %v", slug, commands[slug], want)
		}
	}
	got := f.report().Backups
	want := []string{"restic forget: exit status 11: last line", "restic backup: exit status 3", "restic forget: exit status 5", ""}
	wantAccounts := []string{"acme", "bravo", "charlie", "delta"}
	if len(got) != len(wantAccounts) {
		t.Fatalf("backup reports = %+v", got)
	}
	for i, row := range got {
		if row.Account != wantAccounts[i] || row.Error != want[i] || row.Snapshot.IsZero() {
			t.Fatalf("backup %s facts = %+v, want error %q and recorded snapshot", row.Account, row, want[i])
		}
	}
	f.at = f.at.Add(time.Hour)
	f.execute = nil
	f.backup()
	for _, row := range f.report().Backups {
		if row.Error != "" {
			t.Fatalf("successful backup retained %s error %q", row.Account, row.Error)
		}
	}
}

func TestBackupRetriesOnlyWhenNoSnapshotWasRecorded(t *testing.T) {
	// R-5J3T-MR6Y R-5LJM-EAOC R-5P7B-JLWF
	for _, tc := range []struct {
		verb string
		code int
		want []string
	}{
		{"cat", 1, []string{"cat"}},
		{"backup", 3, []string{"cat", "backup", "forget", "snapshots"}},
		{"forget", 1, []string{"cat", "backup", "forget"}},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.state.Backup.Settings.EveryMinutes = 60
			f.stream()
			f.backup()
			initial := f.report().Backups[0].Snapshot
			f.at = f.at.Add(time.Hour)
			f.backupCommands = nil
			f.execute = func(c linux.Command) (linux.Result, error) {
				verb := c.Args[3]
				if verb == "backup" {
					f.summary(c)
				}
				if verb == tc.verb {
					return linux.Result{ExitCode: tc.code, Stderr: []byte("fixture failure")}, nil
				}
				return linux.Result{}, nil
			}
			if err := f.host.Backup(t.Context(), f.state); err == nil {
				t.Fatal("failed backup returned nil")
			}
			if got := f.resticVerbs(); !slices.Equal(got, tc.want) {
				t.Fatalf("failed pass commands = %v, want %v", got, tc.want)
			}
			report := f.report().Backups[0]
			wantSnapshot := f.at.UTC().Truncate(time.Second)
			if tc.verb == "cat" {
				wantSnapshot = initial
			}
			wantError := fmt.Sprintf("restic %s: exit status %d: fixture failure", tc.verb, tc.code)
			if report.Snapshot != wantSnapshot || report.Error != wantError {
				t.Fatalf("after failed pass = %+v, want snapshot at %s and error %q", report, wantSnapshot, wantError)
			}

			f.execute = nil
			f.backupCommands = nil
			f.at = f.at.Add(time.Minute)
			f.backup()
			if tc.verb == "cat" {
				want := []string{"cat", "backup", "forget", "snapshots"}
				if got := f.resticVerbs(); !slices.Equal(got, want) {
					t.Fatalf("retry without a snapshot = %v, want %v", got, want)
				}
				if got := f.report().Backups[0]; got.Error != "" || got.Snapshot != f.at.UTC().Truncate(time.Second) {
					t.Fatalf("successful retry = %+v", got)
				}
				return
			}
			if got := f.resticVerbs(); len(got) != 0 {
				t.Fatalf("restic retried before the next snapshot was due: %v", got)
			}
			if got := f.report().Backups[0]; got.Snapshot != report.Snapshot || got.Error != report.Error {
				t.Fatalf("idle pass cleared the recorded snapshot or error: %+v", got)
			}
		})
	}
}

func (f *backupPassFixture) resticVerbs() []string {
	var verbs []string
	for _, c := range f.commandsFor(f.path(linux.ResticPath)) {
		verbs = append(verbs, c.Args[3])
	}
	return verbs
}

func TestHostBackupDetectsChangedKeysAndReportsFacts(t *testing.T) {
	// R-5QF7-XDN4 R-5SV0-OX4I R-5VAT-GGLW
	f := newBackupPassFixture(t)
	f.put(dataPrivateKey, "key one", 0o600)
	f.backup()
	if len(f.backupCommands) != 0 {
		t.Fatalf("backup without both keys = %v", f.backupCommands)
	}
	f.put(dataPublicKey, "public one", 0o644)
	f.execute = func(c linux.Command) (linux.Result, error) {
		switch c.Args[3] {
		case "cat":
			return linux.Result{ExitCode: 10}, nil
		case "backup":
			f.summary(c)
		}
		return linux.Result{}, nil
	}
	f.backup()
	f.execute = nil
	got := f.commandsFor(f.path(linux.ResticPath))
	if len(got) != 4 || !slices.Equal(got[0].Args, []string{"-r", "s3:s3.us-test-2.amazonaws.com/fixture-bucket/hosts/host01/7", "--insecure-no-password", "cat", "config"}) ||
		!slices.Equal(got[1].Args[3:], []string{"init"}) ||
		!slices.Equal(got[2].Args[3:], []string{"backup", "--json", "--host", "host01", f.path(linux.HostKeyDir)}) ||
		!slices.Equal(got[3].Args[3:], []string{"forget", "--host", "host01", "--group-by", "host", "--keep-hourly", "24", "--keep-daily", "35", "--keep-monthly", "12", "--prune"}) {
		t.Fatalf("host commands = %v", got)
	}
	b := f.state.Backup
	wantEnv := []string{"AWS_ACCESS_KEY_ID=" + b.Host.AccessKeyID, "AWS_SECRET_ACCESS_KEY=" + b.Host.SecretAccessKey,
		"AWS_SESSION_TOKEN=" + b.Host.SessionToken, "AWS_DEFAULT_REGION=" + b.Region, "RESTIC_CACHE_DIR=" + f.path(linux.ResticCache+"/.host")}
	for _, c := range got {
		if !slices.Equal(c.Env, wantEnv) || !slices.Equal(c.Args[:3], got[0].Args[:3]) {
			t.Fatalf("unexpected repository or environment for host command %v", c.Args)
		}
	}
	before := f.report().HostBackup
	if before.Snapshot != f.at.UTC().Truncate(time.Second) || before.Error != "" {
		t.Fatalf("host backup = %+v", before)
	}
	f.host = linux.New(f.env)
	if after := f.report().HostBackup; after != before {
		t.Fatalf("host after restart = %+v, want %+v", after, before)
	}
	f.backupCommands = nil
	f.at = f.at.Add(time.Minute)
	f.backup()
	if len(f.backupCommands) != 0 {
		t.Fatalf("backup ran for unchanged keys: %v", f.backupCommands)
	}
	f.put(dataPublicKey, "public two", 0o644)
	f.backup()
	if after := f.report().HostBackup; after.Snapshot != f.at.UTC().Truncate(time.Second) || after.Error != "" {
		t.Fatalf("changed key backup = %+v", after)
	}
	last := f.at.UTC().Truncate(time.Second)
	f.at = f.at.Add(time.Minute)
	f.put(dataPrivateKey, "key two", 0o600)
	f.execute = func(c linux.Command) (linux.Result, error) {
		if !slices.Equal(c.Args[3:], []string{"cat", "config"}) {
			t.Fatalf("unexpected command after host repository failed: %v", c.Args)
		}
		return linux.Result{ExitCode: 1, Stderr: []byte("host repository unavailable\n")}, nil
	}
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("private key change did not attempt a backup")
	}
	if got := f.report().HostBackup; got.Snapshot != last || got.Error != "restic cat: exit status 1: host repository unavailable" {
		t.Fatalf("failed host backup = %+v", got)
	}
	f.execute = nil
	f.backup()
	if got := f.report().HostBackup; got.Snapshot != f.at.UTC().Truncate(time.Second) || got.Error != "" {
		t.Fatalf("successful host backup retained error: %+v", got)
	}
	f.at = f.at.Add(time.Duration(b.Settings.EveryMinutes) * time.Minute)
	f.backup()
	if got := f.report().HostBackup; got.Snapshot != f.at.UTC().Truncate(time.Second) || got.Error != "" {
		t.Fatalf("scheduled host backup with unchanged keys = %+v", got)
	}
	f.state.Backup.HostPrefix = ""
	if after := f.report().HostBackup; after.Error != machine.NoCredentials || after.Snapshot != f.at.UTC().Truncate(time.Second) {
		t.Fatalf("uncredentialed host backup = %+v", after)
	}
}

func TestBackupOutputSkipsLargeValuesAndLines(t *testing.T) {
	// R-7U8Y-HYUT
	f := newBackupPassFixture(t)
	f.stream()
	f.execute = func(c linux.Command) (linux.Result, error) {
		if c.Stdout == nil {
			t.Fatalf("nil Stdout for %v", c.Args)
		}
		large := strings.Repeat("x", 1<<20)
		switch c.Args[3] {
		case "backup":
			// Hold the command open after 128 MiB without retaining its output.
			buffer := make([]byte, 64<<10)
			for i := range buffer {
				buffer[i] = 'x'
			}
			var before, during runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			for i := range 4096 {
				if _, err := c.Stdout.Write(buffer); err != nil {
					t.Fatal(err)
				}
				if i == 2047 {
					runtime.GC()
					runtime.ReadMemStats(&during)
				}
			}
			if during.HeapAlloc > before.HeapAlloc+(16<<20) {
				t.Fatalf("backup retained at least %d bytes of output", during.HeapAlloc-before.HeapAlloc)
			}
			f.output(c, "\n")
			f.summary(c)
		case "snapshots":
			at := f.at.Format(time.RFC3339Nano)
			f.output(c, `[{"time":"`+large+`"},{"excludes":["`+large+`"],"time":"`+at+`"}]`)
		}
		return linux.Result{}, nil
	}
	f.backup()
	row := f.report().Backups[0]
	if row.Snapshot != f.at.UTC().Truncate(time.Second) || !slices.Equal(row.Snapshots, []int64{f.at.Unix()}) {
		t.Fatalf("backup facts after large output = %+v", row)
	}
}

func TestBackupPassesUsePrivateModes(t *testing.T) {
	// R-Z62M-471G
	t.Run("active", func(t *testing.T) {
		f := newBackupPassFixture(t)
		f.put("/srv/acme/data/app.db", walSQLite, 0o660)
		f.put(linux.ResticCache+"/left-alone", "existing cache", 0o644)
		f.setMode(linux.BackupDir, 0o755)
		f.setMode(linux.ResticCache, 0o755)
		before := snapshotApplyHost(t, f.root)
		f.stream()
		after := snapshotApplyHost(t, f.root)
		assertPrivateBackupWrites(t, before, after)
		for path, entry := range after {
			if (path == linux.ResticCache || strings.HasPrefix(path, linux.ResticCache+"/")) && !reflect.DeepEqual(entry, before[path]) {
				t.Errorf("Stream changed existing cache path %s", path)
			}
		}
		f.setMode(linux.BackupDir, 0o755)
		before = snapshotApplyHost(t, f.root)
		f.backup()
		after = snapshotApplyHost(t, f.root)
		assertPrivateBackupWrites(t, before, after)
		f.expectMode(linux.ResticCache, 0o700)
		for path, entry := range after {
			if _, exists := before[path]; !exists && strings.HasPrefix(path, linux.ResticCache+"/") && entry.mode.IsDir() && entry.mode != os.ModeDir|0o700 {
				t.Errorf("Backup created cache directory %s with mode %v", path, entry.mode)
			}
		}
	})
	t.Run("missing backup member", func(t *testing.T) {
		stream := newHardeningFixture(t)
		backup := newHardeningFixture(t)
		state := protocol.DesiredState{}
		if err := stream.host.Stream(t.Context(), state); err != nil {
			t.Fatal(err)
		}
		stream.expectMode(linux.BackupDir, 0o700)
		stream.expectAbsent(linux.ResticCache)
		if err := backup.host.Backup(t.Context(), state); err != nil {
			t.Fatal(err)
		}
		backup.expectMode(linux.BackupDir, 0o700)
		backup.expectMode(linux.ResticCache, 0o700)
	})
}

func assertPrivateBackupWrites(t *testing.T, before, after map[string]applyHostFile) {
	t.Helper()
	if got := after[linux.BackupDir].mode; got != os.ModeDir|0o700 {
		t.Errorf("%s mode = %v, want a directory with mode 0700", linux.BackupDir, got)
	}
	for path, entry := range after {
		if !strings.HasPrefix(path, linux.BackupDir+"/") || reflect.DeepEqual(entry, before[path]) {
			continue
		}
		want := os.FileMode(0o600)
		if entry.mode.IsDir() {
			if _, exists := before[path]; exists {
				continue
			}
			want = os.ModeDir | 0o700
		}
		if entry.mode != want {
			t.Errorf("%s mode = %v, want %v", path, entry.mode, want)
		}
	}
}

func TestBackupPassesLeaveUnrelatedFilesAndOnlyRunBackupCommands(t *testing.T) {
	// R-5WIP-U8CL
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o660)
	f.put("/home/user/.bashrc", "fixture", 0o644)
	f.put("/etc/other.conf", "fixture", 0o644)
	f.put("/var/cache/existing/file", "fixture", 0o640)
	f.expectAbsent("/etc/systemd")
	f.expectAbsent("/var/cache/accountsd")
	before := snapshotApplyHost(t, f.root)
	f.stream()
	afterStream := snapshotApplyHost(t, f.root)
	unit := "/etc/systemd/system/" + linux.LitestreamUnit
	assertHostChanges(t, withoutBackupTrees(before, linux.BackupDir), withoutBackupTrees(afterStream, linux.BackupDir), []string{unit, linux.LitestreamPath})
	if !afterStream[unit].mode.IsRegular() {
		t.Errorf("Stream did not create %s", unit)
	}
	assertBackupPassCommands(t, f, map[string][]string{
		"systemctl":                  {"daemon-reload", "is-active", "restart"},
		f.path(linux.LitestreamPath): {"status", "ltx"},
	})
	f.backupCommands = nil
	f.backup()
	afterBackup := snapshotApplyHost(t, f.root)
	assertHostChanges(t, withoutBackupTrees(afterStream, linux.BackupDir, linux.ResticCache), withoutBackupTrees(afterBackup, linux.BackupDir, linux.ResticCache), []string{linux.ResticPath, linux.ResticCache})
	if !afterBackup["/var/cache/accountsd"].mode.IsDir() {
		t.Error("Backup did not create /var/cache/accountsd")
	}
	assertBackupPassCommands(t, f, map[string][]string{f.path(linux.ResticPath): {"cat", "backup", "forget", "snapshots"}})
}

func TestBackupPassesDoNotWriteThroughLinksToOtherHostPaths(t *testing.T) {
	// R-5WIP-U8CL
	for _, tc := range []struct {
		path string
		run  func(*linux.Host, context.Context, protocol.DesiredState) error
	}{
		{"/etc/systemd", (*linux.Host).Stream},
		{"/var/lib/accountsd", (*linux.Host).Stream},
		{"/var/cache/accountsd", (*linux.Host).Backup},
	} {
		t.Run(tc.path, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put("/srv/acme/data/app.db", walSQLite, 0o660)
			f.put("/unrelated/sentinel", "unchanged", 0o640)
			target := f.path("/unrelated/target")
			if err := os.Rename(f.path(tc.path), target); errors.Is(err, os.ErrNotExist) {
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(f.path(tc.path)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, f.path(tc.path)); err != nil {
				t.Fatal(err)
			}
			before := snapshotApplyHost(t, f.path("/unrelated"))
			_ = tc.run(f.host, t.Context(), f.state)
			assertHostChanges(t, before, snapshotApplyHost(t, f.path("/unrelated")), nil)
		})
	}
}

func TestStreamModeCorrectionLeavesUnrelatedHardLinkUntouched(t *testing.T) {
	// R-5WIP-U8CL
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o660)
	f.stream()
	unit := "/etc/systemd/system/" + linux.LitestreamUnit
	const unrelated = "/unrelated-unit"
	f.setMode(unit, 0o600)
	if err := os.Link(f.path(unit), f.path(unrelated)); err != nil {
		t.Fatal(err)
	}
	before := snapshotApplyHost(t, f.root)
	_ = f.host.Stream(t.Context(), f.state)
	after := snapshotApplyHost(t, f.root)
	if !reflect.DeepEqual(after[unrelated], before[unrelated]) {
		t.Errorf("Stream changed the file hard-linked to %s", unit)
	}
}

func (f *backupPassFixture) obstructBackupDirectory(path string) func() {
	f.t.Helper()
	const saved = "/saved-backup-directory"
	if err := os.Rename(f.path(path), f.path(saved)); err != nil {
		f.t.Fatal(err)
	}
	f.put(path, "not a directory", 0o600)
	return func() {
		if err := os.Remove(f.path(path)); err != nil {
			f.t.Fatal(err)
		}
		if err := os.Rename(f.path(saved), f.path(path)); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *backupPassFixture) requireResticConfig(slug string) {
	f.t.Helper()
	commands := f.commandsFor(f.path(linux.ResticPath))
	repo := protocol.FilesPrefix(slug)
	if slug == f.state.Host {
		repo = strings.TrimRight(f.state.Backup.HostPrefix, "/")
	}
	for _, c := range commands {
		if strings.HasSuffix(c.Args[1], "/"+repo) {
			if !slices.Equal(c.Args[3:], []string{"cat", "config"}) {
				f.t.Fatalf("first command for %s = %v", slug, c.Args)
			}
			return
		}
	}
	f.t.Fatalf("restic did not open %s: %v", slug, commands)
}

func TestBackupWithholdsUnkeptWalkAndRetries(t *testing.T) {
	// R-6M4L-VEX6 R-6NCI-96NV
	for _, tc := range []struct {
		name  string
		dir   string
		prior bool
	}{
		{"first walk", linux.BackupDir, false},
		{"previous walk in restored directory", linux.BackupDir, true},
		{"failed status write", linux.BackupDir + "/status", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put(dataPrivateKey, "private", 0o600)
			f.put(dataPublicKey, "public", 0o644)
			if tc.prior {
				f.put("/srv/acme/data/old.db", walSQLite, 0o660)
				f.stream()
				f.put("/srv/acme/data/old.db", plainSQLite, 0o660)
			}
			restore := f.obstructBackupDirectory(tc.dir)
			const db = "data/app.db"
			f.put("/srv/acme/"+db, walSQLite, 0o660)
			if err := f.host.Stream(t.Context(), f.state); err == nil {
				t.Fatal("Stream accepted a path that cannot hold its walk")
			}
			if !tc.prior {
				if got := f.report().Backups[0].Error; !strings.Contains(got, linux.BackupDir+"/acme.yml") {
					t.Fatalf("live Stream pass error = %q", got)
				}
			}
			restore()
			if !tc.prior {
				if got := inspectBackupFacts(t, linux.New(f.env), f.state).Backups[0].Error; got != "" {
					t.Fatalf("new Host inherited an unkept error: %q", got)
				}
			}
			f.backupCommands = nil
			f.backup()
			f.requireResticConfig(f.state.Host)
			for _, c := range f.commandsFor(f.path(linux.ResticPath)) {
				if strings.HasSuffix(c.Args[1], "/"+protocol.FilesPrefix("acme")) {
					t.Fatalf("restic used an unkept walk: %v", c.Args)
				}
			}
			if got := f.report().Backups[0].Error; got != machine.WalkUnrecorded {
				t.Fatalf("account error = %q, want %q", got, machine.WalkUnrecorded)
			}

			f.at = f.at.Add(linux.InstallRetry)
			f.stream()
			// Backup uses this walk's WAL paths even when the live files
			// change before that pass.
			f.put("/srv/acme/"+db, plainSQLite, 0o660)
			f.put("/srv/acme/data/late.db", walSQLite, 0o660)
			f.backupCommands = nil
			f.backup()
			f.requireResticConfig("acme")
			f.expectFile(linux.BackupDir+"/manifest/acme", fmt.Sprintf("%d %d 0660 %s\n", os.Getuid(), os.Getgid(), db), 0o600)
			for _, c := range f.commandsFor(f.path(linux.ResticPath)) {
				if c.Args[3] != "backup" || c.Args[6] != "acme" {
					continue
				}
				var want []string
				for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
					want = append(want, "--exclude", f.path("/srv/acme/"+db)+suffix)
				}
				want = append(want, f.path("/srv/acme"), f.path(linux.BackupDir+"/manifest/acme"))
				if !slices.Equal(c.Args[7:], want) {
					t.Fatalf("restic exclusions after recovery = %v, want %v", c.Args[7:], want)
				}
			}
			if got := f.report().Backups[0]; got.Error != "" || got.Snapshot.IsZero() {
				t.Fatalf("account did not recover after a kept walk: %+v", got)
			}
		})
	}
}

func TestAccountResticRechecksWalkAfterCommand(t *testing.T) {
	// R-6M4L-VEX6 R-6NCI-96NV
	for _, kept := range []bool{true, false} {
		name := "kept"
		if !kept {
			name = "unkept"
		}
		t.Run(name, func(t *testing.T) {
			f := newBackupPassFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			started, release := make(chan struct{}), make(chan struct{})
			var commands []linux.Command
			base := f.env.Execute
			f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
				if c.Name != f.path(linux.ResticPath) {
					return base(ctx, c)
				}
				commands = append(commands, c)
				switch c.Args[3] {
				case "cat":
					close(started)
					select {
					case <-release:
					case <-ctx.Done():
						return linux.Result{}, ctx.Err()
					}
				case "backup":
					f.summary(c)
				}
				return linux.Result{}, nil
			}
			f.host = linux.New(f.env)
			f.put("/srv/acme/data/old.db", walSQLite, 0o660)
			f.stream()
			backupDone := make(chan error, 1)
			go func() { backupDone <- f.host.Backup(ctx, f.state) }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("Backup never reached the account's cat config")
			}

			restore := func() {}
			if !kept {
				restore = f.obstructBackupDirectory(linux.BackupDir + "/status")
			}
			f.put("/srv/acme/data/old.db", plainSQLite, 0o660)
			f.put("/srv/acme/data/new.db", walSQLite, 0o660)
			streamDone := make(chan error, 1)
			go func() { streamDone <- f.host.Stream(ctx, f.state) }()
			select {
			case err := <-streamDone:
				if (err == nil) != kept {
					t.Fatalf("Stream error = %v, kept walk = %t", err, kept)
				}
			case <-ctx.Done():
				t.Fatal("Stream waited for restic")
			}
			restore()
			close(release)
			select {
			case err := <-backupDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("Backup did not finish")
			}
			if !kept {
				if len(commands) != 1 || f.report().Backups[0].Error != machine.WalkUnrecorded {
					t.Fatalf("restic continued after the failed walk write: %v", commands)
				}
				return
			}
			if len(commands) < 2 || commands[1].Args[3] != "backup" ||
				!slices.Contains(commands[1].Args, f.path("/srv/acme/data/new.db")) ||
				slices.Contains(commands[1].Args, f.path("/srv/acme/data/old.db")) {
				t.Fatalf("snapshot did not use the new kept walk: %v", commands)
			}
			f.expectFile(linux.BackupDir+"/manifest/acme", fmt.Sprintf("%d %d 0660 data/new.db\n", os.Getuid(), os.Getgid()), 0o600)
		})
	}
}

func TestBackupReadsBackLatestKeptWalk(t *testing.T) {
	// R-6M4L-VEX6 R-6NCI-96NV
	f := newBackupPassFixture(t)
	const status = linux.BackupDir + "/status"
	f.put("/srv/acme/data/old.db", walSQLite, 0o660)
	f.stream()
	if err := os.Rename(f.path(status), f.path("/older-status")); err != nil {
		t.Fatal(err)
	}
	f.put("/srv/acme/data/app.db", walSQLite, 0o660)
	f.stream()
	if err := os.Rename(f.path(status), f.path("/newer-status")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.path("/older-status"), f.path(status)); err != nil {
		t.Fatal(err)
	}
	f.backupCommands = nil
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic used an older kept walk: %v", got)
	}
	if got := f.report().Backups[0].Error; got != machine.WalkUnrecorded {
		t.Fatalf("account error for older walk = %q", got)
	}
	if err := os.Rename(f.path(status), f.path("/older-status")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.path("/newer-status"), f.path(status)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(f.path(status))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := status + "/" + entry.Name()
		data, err := os.ReadFile(f.path(path))
		if err != nil {
			t.Fatal(err)
		}
		// The JSON prefix still decodes if a reader truncates instead of
		// rejecting an oversized file.
		f.put(path, string(data)+strings.Repeat(" ", linux.MaxCapture), 0o600)
	}
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic used a walk from an oversized file: %v", got)
	}
	if got := f.report().Backups[0].Error; got != machine.WalkUnrecorded {
		t.Fatalf("account error for oversized walk = %q", got)
	}
}

func TestBackupRejectsWalkKeptByAnotherHost(t *testing.T) {
	// R-6M4L-VEX6
	f := newBackupPassFixture(t)
	const status = linux.BackupDir + "/status"
	f.put("/srv/acme/data/new.db", walSQLite, 0o660)
	f.stream()
	if err := os.Rename(f.path(status), f.path("/first-host-status")); err != nil {
		t.Fatal(err)
	}

	f.host = linux.New(f.env)
	f.stream()
	if err := os.Rename(f.path(status), f.path("/second-host-status")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.path("/first-host-status"), f.path(status)); err != nil {
		t.Fatal(err)
	}
	f.backupCommands = nil
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic reused the other host's saved walk: %v", got)
	}

	if err := os.Rename(f.path(status), f.path("/first-host-status")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.path("/second-host-status"), f.path(status)); err != nil {
		t.Fatal(err)
	}
	file := status + "/acme.json"
	raw, err := os.ReadFile(f.path(file))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "data/new.db") {
		t.Fatal("account status did not store the new path")
	}
	// A pre-provenance record can contain exactly the same walk. It is
	// readable for reporting and cannot stand in for the kept publication.
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "WalkToken")
	legacyData, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	f.put(file, string(legacyData), 0o600)
	if got := f.report().Backups[0].Databases; len(got) != 1 || got[0].Path != "data/new.db" {
		t.Fatalf("legacy walk facts were unreadable: %+v", got)
	}
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic used a legacy walk without provenance: %v", got)
	}
	// Change the stored paths without changing the publication token.
	f.put(file, strings.ReplaceAll(string(raw), "data/new.db", "data/alt.db"), 0o600)
	f.backup()
	if got := f.commandsFor(f.path(linux.ResticPath)); len(got) != 0 {
		t.Fatalf("restic used a walk with altered saved paths: %v", got)
	}

	f.put(file, string(raw), 0o600)
	f.backup()
	f.requireResticConfig("acme")
	f.expectFile(linux.BackupDir+"/manifest/acme", fmt.Sprintf("%d %d 0660 data/new.db\n", os.Getuid(), os.Getgid()), 0o600)
}

func withoutBackupTrees(entries map[string]applyHostFile, trees ...string) map[string]applyHostFile {
	filtered := make(map[string]applyHostFile, len(entries))
	for path, entry := range entries {
		if !slices.ContainsFunc(trees, func(dir string) bool { return path == dir || strings.HasPrefix(path, dir+"/") }) {
			filtered[path] = entry
		}
	}
	return filtered
}

func assertBackupPassCommands(t *testing.T, f *backupPassFixture, allowed map[string][]string) {
	t.Helper()
	if len(f.commands) != 0 {
		t.Errorf("unexpected commands: %v", f.commands)
	}
	for _, c := range f.backupCommands {
		verb := 0
		if c.Name == f.path(linux.ResticPath) {
			verb = 3
		}
		if len(c.Args) <= verb || !slices.Contains(allowed[c.Name], c.Args[verb]) {
			t.Errorf("unexpected command: %s %v", c.Name, c.Args)
		}
	}
}

func TestBackupRunsDueCheckIndependentlyOfSnapshot(t *testing.T) {
	// R-5MRI-S2F1 R-5J3T-MR6Y
	f := newBackupPassFixture(t)
	f.state.Backup.Settings.EveryMinutes = 7 * 24 * 60
	f.state.Backup.Settings.CheckDays = 1
	f.stream()
	f.backup()
	previous := f.report().Backups[0]
	f.backupCommands = nil
	f.at = f.at.Add(24*time.Hour - time.Second)
	f.backup()
	if len(f.backupCommands) != 0 {
		t.Fatalf("early check commands = %v", f.backupCommands)
	}
	f.at = f.at.Add(time.Second)
	f.backup()
	got := f.commandsFor(f.path(linux.ResticPath))
	if len(got) != 2 || !slices.Equal(got[0].Args[3:], []string{"cat", "config"}) ||
		!slices.Equal(got[1].Args[3:], []string{"check", "--read-data-subset=" + linux.CheckSubset}) {
		t.Fatalf("check-only commands = %v", got)
	}
	row := f.report().Backups[0]
	if row.Snapshot != previous.Snapshot || row.Checked != f.at.UTC().Truncate(time.Second) {
		t.Fatalf("check-only report = %+v, snapshot was %s", row, previous.Snapshot)
	}
}

func TestBackupWithholdsDueCheckAndContinuesWithNextAccount(t *testing.T) {
	// R-6NCI-96NV
	f := newBackupPassFixture(t)
	f.state.Backup.Settings.EveryMinutes = 7 * 24 * 60
	f.state.Backup.Settings.CheckDays = 1
	f.state.Accounts = append(f.state.Accounts, protocol.Account{Slug: "bravo", State: protocol.AccountActive})
	f.state.Backup.Accounts = append(f.state.Backup.Accounts, protocol.AccountCredentials{Account: "bravo", Credentials: f.state.Backup.Host})
	f.stream()
	f.backup()
	previous := f.report().Backups[0]
	f.at = f.at.Add(24 * time.Hour)

	// Fail publication for acme alone, then restore its previous status
	// including the schedule that makes its check due and snapshot early.
	status := f.path(linux.BackupDir + "/status/acme.json")
	saved := f.path("/saved-account-status")
	if err := os.Rename(status, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(status, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("Stream replaced a directory with its status file")
	}
	if err := os.Remove(status); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, status); err != nil {
		t.Fatal(err)
	}
	f.backupCommands = nil
	f.backup()
	f.requireResticConfig("bravo")
	var verbs []string
	for _, c := range f.commandsFor(f.path(linux.ResticPath)) {
		if !strings.HasSuffix(c.Args[1], "/"+protocol.FilesPrefix("bravo")) {
			t.Fatalf("unkept walk did not withhold commands: %v", c.Args)
		}
		verbs = append(verbs, c.Args[3])
	}
	if got := f.report(); got.Backups[0].Error != machine.WalkUnrecorded || got.Backups[0].Snapshot != previous.Snapshot ||
		got.Backups[0].Checked != previous.Checked || !slices.Equal(verbs, []string{"cat", "check"}) {
		t.Fatalf("error and continued checks = %+v, %v", got.Backups, verbs)
	}
	f.state.Backup.Settings.CheckDays = 0
	f.backupCommands = nil
	f.backup()
	if got := f.report().Backups[0].Error; got != machine.WalkUnrecorded || len(f.commandsFor(f.path(linux.ResticPath))) != 0 {
		t.Fatalf("idle backup cleared account error: %q, commands %v", got, f.backupCommands)
	}
}

func TestBackupReportPreservesInstallErrorsAcrossRestart(t *testing.T) {
	// R-5RN4-B5DT R-5VAT-GGLW
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	for _, path := range []string{linux.ResticPath, linux.LitestreamPath} {
		if err := os.Remove(f.path(path)); err != nil {
			t.Fatal(err)
		}
	}
	message := "installer unavailable " + strings.Repeat("long details ", 10000)
	f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) {
		return nil, errors.New(message)
	}
	f.host = linux.New(f.env)
	resticErr := linux.InstallRestic(t.Context(), f.env, linux.ResticVersion, linux.ResticDigest)
	litestreamErr := linux.InstallLitestream(t.Context(), f.env, linux.LitestreamVersion, linux.LitestreamDigest)
	if resticErr == nil || litestreamErr == nil {
		t.Fatal("fixture installer failed to return its error")
	}
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("failed Litestream installation returned nil")
	}
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("failed restic installation returned nil")
	}
	before := f.report().Backups
	if len(before) != 1 || len(before[0].Databases) != 1 || before[0].Error != resticErr.Error() || before[0].Databases[0].Error != litestreamErr.Error() {
		t.Fatalf("install errors in live report = %+v", before)
	}
	f.host = linux.New(f.env)
	if after := f.report().Backups; !reflect.DeepEqual(before, after) {
		t.Fatalf("install errors after restart = %+v, want %+v", after, before)
	}
}

func TestResticRejectsInvalidEndOfLastSummary(t *testing.T) {
	// R-5LJM-EAOC R-5P7B-JLWF
	for _, tc := range []struct{ name, end string }{
		{"numeric end", `17`}, {"null end", `null`}, {"invalid timestamp", `"not a timestamp"`}, {"no summary", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.stream()
			f.execute = func(c linux.Command) (linux.Result, error) {
				if c.Args[3] == "backup" && tc.end != "" {
					f.summary(c)
					f.output(c, `{"message_type":"summary","snapshot_id":"last","backup_end":`+tc.end+"}\n")
				}
				return linux.Result{}, nil
			}
			if err := f.host.Backup(t.Context(), f.state); err == nil {
				t.Fatal("backup accepted an invalid summary end")
			}
			got := f.commandsFor(f.path(linux.ResticPath))
			if len(got) != 2 || got[1].Args[3] != "backup" {
				t.Fatalf("backup ran more commands after the invalid summary: %v", got)
			}
			if row := f.report().Backups[0]; !row.Snapshot.IsZero() || row.Error == "" {
				t.Fatalf("invalid final summary recorded backup facts: %+v", row)
			}
		})
	}
}
