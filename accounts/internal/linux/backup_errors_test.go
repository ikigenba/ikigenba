package linux_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const acmeStreamUnit = "accountsd-litestream@acme.service"

func TestStreamPassErrorsPersistByMethod(t *testing.T) {
	// R-MIPV-8JLT R-N3G5-QN7M R-N10C-Z3Q8
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	f.state.Accounts = append(f.state.Accounts, protocol.Account{Slug: "bravo", GID: 20002, State: protocol.AccountActive})
	f.state.Backup.Accounts = append(f.state.Backup.Accounts, protocol.AccountCredentials{Account: "bravo", Credentials: f.state.Backup.Host})
	f.put("/srv/bravo/data/app.db", walSQLite, 0o600)
	const original, unsaved = "job failed", "replacement failed"
	diagnostic := original
	f.execute = func(c linux.Command) (linux.Result, error) {
		if c.Name == "systemctl" && len(c.Args) == 2 && c.Args[1] == acmeStreamUnit {
			if c.Args[0] == "is-active" {
				return linux.Result{ExitCode: 3}, nil
			}
			if c.Args[0] == "restart" && diagnostic != "" {
				return linux.Result{ExitCode: 1, Stderr: []byte("earlier line\n" + diagnostic + "\n \t\r\n")}, nil
			}
		}
		if c.Name == f.path(linux.ResticPath) && c.Args[3] == "backup" {
			f.summary(c)
		}
		return linux.Result{}, nil
	}
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("failed restart returned nil")
	}
	if got := f.commandsFor(f.path(linux.LitestreamPath)); len(got) != 3 || !strings.Contains(got[0].Args[2], "bravo.yml") {
		t.Fatalf("stream commands after the failed restart = %v", got)
	}
	prefix := (&linux.CommandError{Label: "systemctl restart", Result: linux.Result{ExitCode: 1}}).Error() + ": "
	assertError := func(host *linux.Host, want string) {
		t.Helper()
		if got := inspectBackupFacts(t, host, f.state).Backups[0].Error; got != want {
			t.Fatalf("acme error = %q, want %q", got, want)
		}
	}
	assertError(f.host, prefix+original)
	assertError(linux.New(f.env), prefix+original)
	f.backup()
	assertError(f.host, prefix+original)
	ungated := f.state
	ungated.Backup = nil
	if err := f.host.Stream(t.Context(), ungated); err != nil {
		t.Fatal(err)
	}
	assertError(f.host, prefix+original)

	diagnostic = unsaved
	restore := f.obstructBackupDirectory(linux.BackupDir + "/status")
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("obstructed status did not fail Stream")
	}
	assertError(f.host, prefix+unsaved)
	restore()
	assertError(linux.New(f.env), prefix+original)
	assertError(f.host, prefix+unsaved)
	diagnostic = ""
	f.stream()
	assertError(linux.New(f.env), "")
	assertError(f.host, "")
}

func TestStreamCleanupFailuresOnlyReportForBackedUpAccounts(t *testing.T) {
	// R-MIPV-8JLT R-MYKK-7K8U
	for _, tc := range []struct {
		name   string
		gate   bool
		active bool
	}{
		{name: "no backup", active: true},
		{name: "removed", gate: true},
		{name: "active with no WAL paths", gate: true, active: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupPassFixture(t)
			var stderr bytes.Buffer
			f.env.Stderr = &stderr
			f.host = linux.New(f.env)
			f.put(linux.BackupDir+"/acme.yml", "old configuration", 0o600)
			f.put(linux.BackupDir+"/acme.env", "old environment", 0o600)
			f.put(linux.BackupDir+"/meta/acme/data/app.db", "old metadata", 0o600)
			pass := f.state
			if !tc.gate {
				pass.Backup = nil
			}
			if !tc.active {
				pass.Accounts = []protocol.Account{{Slug: "acme", GID: f.state.Accounts[0].GID, State: protocol.AccountRemoved}}
			}
			const diagnostic = "fixture stop error"
			f.execute = func(c linux.Command) (linux.Result, error) {
				if c.Name == "systemctl" && c.Args[0] == "stop" {
					return linux.Result{}, errors.New(diagnostic)
				}
				return linux.Result{}, nil
			}
			if err := f.host.Stream(t.Context(), pass); (err != nil) != tc.gate {
				t.Fatalf("Stream returned %v", err)
			}
			if !tc.gate && !strings.Contains(stderr.String(), diagnostic) {
				t.Fatalf("gated cleanup diagnostic = %q", stderr.String())
			}
			assertAcmeStreamUnitCalls(t, f, "is-active", "stop")
			for _, path := range []string{"/acme.yml", "/acme.env", "/meta/acme"} {
				f.expectAbsent(linux.BackupDir + path)
			}
			var want string
			if tc.gate && tc.active {
				want = (&linux.CommandError{Label: "systemctl stop", Err: errors.New(diagnostic)}).Error()
			}
			if got := f.report().Backups[0].Error; got != want {
				t.Fatalf("acme error = %q, want %q", got, want)
			}
		})
	}
}

func TestBackupPassErrorRequiresAnAccountFilesystemFailure(t *testing.T) {
	// R-MIPV-8JLT R-N10C-Z3Q8
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	f.put(dataPrivateKey, "host private", 0o600)
	f.put(dataPublicKey, "host public", 0o600)
	const streamMessage = "first status failure"
	f.execute = func(c linux.Command) (linux.Result, error) {
		if c.Name == f.path(linux.LitestreamPath) {
			if c.Args[0] == "status" {
				return linux.Result{ExitCode: 1, Stderr: []byte(streamMessage)}, nil
			}
			return linux.Result{ExitCode: 2, Stderr: []byte("later failure")}, nil
		}
		return linux.Result{}, nil
	}
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("failed stream status returned nil")
	}
	f.execute = nil
	const cache = linux.ResticCache + "/acme"
	f.put(cache, "cannot create the repository cache here", 0o600)
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("blocked cache returned nil")
	}
	f.requireResticConfig(f.state.Host)
	for _, c := range f.commandsFor(f.path(linux.ResticPath)) {
		if strings.HasSuffix(c.Args[1], "/"+protocol.FilesPrefix("acme")) {
			t.Fatalf("restic ran with its cache blocked: %v", c.Args)
		}
	}
	want := (&linux.CommandError{Label: "litestream status", Result: linux.Result{ExitCode: 1}}).Error() + ": " + streamMessage
	if got := f.report().Backups[0].Error; got != want {
		t.Fatalf("acme error = %q, want first Stream failure %q", got, want)
	}
	f.stream()
	if got := f.report().Backups[0].Error; !strings.Contains(got, cache) {
		t.Fatalf("stream did not leave Backup's pass error visible: %q", got)
	}
	if err := os.Remove(f.path(cache)); err != nil {
		t.Fatal(err)
	}
	const manifest = linux.BackupDir + "/manifest"
	f.put(manifest, "cannot hold account manifests", 0o600)
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("blocked manifest returned nil")
	}
	if got := f.report().Backups[0].Error; !strings.Contains(got, manifest+"/acme") {
		t.Fatalf("blocked manifest error = %q", got)
	}
	if err := os.Remove(f.path(manifest)); err != nil {
		t.Fatal(err)
	}
	f.backup()
	if got := f.report().Backups[0].Error; got != "" {
		t.Fatalf("successful backup kept a pass error: %q", got)
	}
}

func TestFilesRestoreSharesBackupPassError(t *testing.T) {
	// R-MIPV-8JLT
	f := newBackupPassFixture(t)
	const cache = linux.ResticCache + "/acme"
	f.put(cache, "blocked cache", 0o600)
	f.state.Backup.Restores = []protocol.RestoreRequest{{ID: "fixture_restore_first", Kind: protocol.RestoreFiles, Account: "acme", AsOf: f.at.UTC()}}
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("files restore with a blocked cache returned nil")
	}
	if got := f.report().Backups[0].Error; !strings.Contains(got, cache) {
		t.Fatalf("backup pass error = %q", got)
	}
	if err := os.Remove(f.path(cache)); err != nil {
		t.Fatal(err)
	}
	f.backup()
	if got := f.report().Backups[0].Error; !strings.Contains(got, cache) || len(f.commandsFor(f.path(linux.ResticPath))) != 0 {
		t.Fatalf("unchanged request changed pass error or ran restic: %q, %v", got, f.resticVerbs())
	}
	f.state.Backup.Restores[0].ID = "fixture_restore_next"
	f.backup()
	if got := f.report().Backups[0].Error; got != "" {
		t.Fatalf("files restore command left prior pass error: %q", got)
	}
	if got := f.resticVerbs(); len(got) != 1 || got[0] != "snapshots" {
		t.Fatalf("files restore commands = %v", got)
	}
}

func TestStreamPassErrorNamesUnreadableDatabasePath(t *testing.T) {
	// R-MIPV-8JLT
	f := newBackupPassFixture(t)
	const blocked = "/srv/acme/data/app.db"
	f.put(blocked, walSQLite, 0o000)
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("unreadable database did not fail Stream")
	}
	if got := f.report().Backups[0].Error; !strings.Contains(got, blocked) {
		t.Fatalf("stream pass error = %q", got)
	}
	if err := os.Chmod(f.path(blocked), 0o600); err != nil {
		t.Fatal(err)
	}
	f.stream()
	if got := f.report().Backups[0].Error; got != "" {
		t.Fatalf("readable database left prior pass error: %q", got)
	}
}

func TestBackupInspectErrorPrecedence(t *testing.T) {
	// R-N10C-Z3Q8
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	f.stream()
	f.execute = func(c linux.Command) (linux.Result, error) {
		switch {
		case c.Name == f.path(linux.ResticPath) && c.Args[3] == "backup":
			f.summary(c)
		case c.Name == f.path(linux.ResticPath) && c.Args[3] == "forget", c.Name == f.path(linux.LitestreamPath):
			return linux.Result{ExitCode: 1}, nil
		}
		return linux.Result{}, nil
	}
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("failed forget returned nil")
	}
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("failed Litestream command returned nil")
	}
	assertError := func(want string) {
		t.Helper()
		if got := f.report().Backups[0].Error; got != want {
			t.Fatalf("acme error = %q, want %q", got, want)
		}
	}
	assertError((&linux.CommandError{Label: "restic forget", Result: linux.Result{ExitCode: 1}}).Error())
	const installError = "fixture installation error"
	f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) { return nil, errors.New(installError) }
	f.host = linux.New(f.env)
	if err := os.Remove(f.path(linux.ResticPath)); err != nil {
		t.Fatal(err)
	}
	if err := f.host.Backup(t.Context(), f.state); err == nil {
		t.Fatal("failed restic install returned nil")
	}
	assertError(installError)
	depth := strings.Repeat("/nested", linux.MaxDepth+1)
	f.put("/srv/acme/data"+depth+"/app.db", walSQLite, 0o600)
	if err := f.host.Stream(t.Context(), f.state); err != nil {
		t.Fatal(err)
	}
	assertError(machine.DataTooLarge)
	f.state.Backup.Accounts = nil
	assertError(machine.NoCredentials)
}

func TestBackupCommandErrorKeepsBoundedUTF8Prefix(t *testing.T) {
	// R-MYKK-7K8U R-MIPV-8JLT
	for _, stream := range []bool{false, true} {
		label := "restic forget"
		if stream {
			label = "systemctl restart"
		}
		t.Run(label, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put("/srv/acme/data/app.db", walSQLite, 0o600)
			const token = "界"
			const prior = "older diagnostic\n"
			const last = "\n\t\r \n"
			line := strings.Repeat(token, (1<<20)/len(token)+1)
			f.execute = func(c linux.Command) (linux.Result, error) {
				if stream && c.Name == "systemctl" && c.Args[0] == "restart" ||
					!stream && c.Name == f.path(linux.ResticPath) && c.Args[3] == "forget" {
					return linux.Result{ExitCode: 1, Stderr: []byte(prior + line + last)}, nil
				}
				if c.Name == f.path(linux.ResticPath) && c.Args[3] == "backup" {
					f.summary(c)
				}
				return linux.Result{}, nil
			}
			pass := f.host.Stream
			if !stream {
				f.stream()
				pass = f.host.Backup
			}
			if err := pass(t.Context(), f.state); err == nil {
				t.Fatal("command failure returned nil")
			}
			prefix := (&linux.CommandError{Label: label, Result: linux.Result{ExitCode: 1}}).Error() + ": "
			want := prefix + strings.Repeat(token, (protocol.MaxBackupError-len(prefix))/len(token))
			if got := f.report().Backups[0].Error; got != want || !utf8.ValidString(got) {
				t.Fatalf("acme error = %q (%d bytes), want %q (%d bytes)", got, len(got), want, len(want))
			}
		})
	}
}
