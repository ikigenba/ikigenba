package linux_test

import (
	"os"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
)

func TestLitestreamRestartsForNewGIDAndClearsOldMetadata(t *testing.T) {
	// R-6RQD-ZBLR
	f := newBackupPassFixture(t)
	f.state.Accounts[0].GID = 20001
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	f.stream()

	const meta = linux.BackupDir + "/meta/acme"
	const target = "/outside/keep"
	f.put(meta+"/data/app.db/state", "previous account", 0o600)
	f.put(target, "symlink target", 0o600)
	if err := os.Symlink(f.path("/outside"), f.path(meta+"/outside")); err != nil {
		t.Fatal(err)
	}
	f.host = linux.New(f.env)
	f.backupCommands = nil
	f.stream()
	assertAcmeStreamUnitCalls(t, f, "is-active")
	f.expectFile(meta+"/data/app.db/state", "previous account", 0o600)

	f.state.Accounts[0].GID = 20002
	f.backupCommands = nil
	failRestart := true
	f.execute = func(c linux.Command) (linux.Result, error) {
		if c.Name == "systemctl" && len(c.Args) == 2 && c.Args[0] == "restart" {
			f.expectAbsent(meta)
			f.expectFile(target, "symlink target", 0o600)
			if failRestart {
				return linux.Result{ExitCode: 1}, nil
			}
		}
		return linux.Result{}, nil
	}
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("Stream accepted a failed restart")
	}
	assertAcmeStreamUnitCalls(t, f, "is-active", "restart")

	failRestart = false
	f.backupCommands = nil
	f.stream()
	assertAcmeStreamUnitCalls(t, f, "is-active", "restart")

	f.backupCommands = nil
	f.stream()
	assertAcmeStreamUnitCalls(t, f, "is-active")
}

func TestLitestreamRemovesLastRestartOnlyWhenCleaningStaleConfig(t *testing.T) {
	// R-6RQD-ZBLR
	for _, tc := range []struct {
		name       string
		configured bool
		cleanup    []string
		returned   []string
	}{
		{name: "stale config", configured: true, cleanup: []string{"is-active", "stop"}, returned: []string{"is-active"}},
		{name: "no config", returned: []string{"is-active", "restart"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.state.Accounts[0].GID = 20001
			f.put("/srv/acme/data/app.db", walSQLite, 0o600)
			f.stream()

			const config = linux.BackupDir + "/acme.yml"
			const env = linux.BackupDir + "/acme.env"
			configData, err := os.ReadFile(f.path(config))
			if err != nil {
				t.Fatal(err)
			}
			envData, err := os.ReadFile(f.path(env))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.configured {
				if err := os.Remove(f.path(config)); err != nil {
					t.Fatal(err)
				}
			}
			accounts := f.state.Accounts
			f.state.Accounts = nil
			f.backupCommands = nil
			f.stream()
			assertAcmeStreamUnitCalls(t, f, tc.cleanup...)
			f.expectAbsent(config)
			if tc.configured {
				f.expectAbsent(env)
			} else {
				f.expectFile(env, string(envData), 0o600)
			}

			f.state.Accounts = accounts
			f.state.Accounts[0].GID = 20002
			f.put(config, string(configData), 0o600)
			f.put(env, string(envData), 0o600)
			f.backupCommands = nil
			f.stream()
			assertAcmeStreamUnitCalls(t, f, tc.returned...)
		})
	}
}

func TestLitestreamGIDChangeDoesNotFollowMetadataParentLink(t *testing.T) {
	// R-6RQD-ZBLR R-76D6-KKI3
	f := newBackupPassFixture(t)
	f.put("/srv/acme/data/app.db", walSQLite, 0o600)
	f.stream()

	const meta = linux.BackupDir + "/meta"
	f.put(meta+"/acme/kept", "leave this alone", 0o600)
	if err := os.Rename(f.path(meta), f.path("/outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.path("/outside"), f.path(meta)); err != nil {
		t.Fatal(err)
	}
	f.state.Accounts[0].GID++
	f.backupCommands = nil
	if err := f.host.Stream(t.Context(), f.state); err == nil {
		t.Fatal("Stream accepted a metadata parent symlink")
	}
	assertAcmeStreamUnitCalls(t, f, "is-active")
	f.expectFile("/outside/acme/kept", "leave this alone", 0o600)
}

func assertAcmeStreamUnitCalls(t *testing.T, f *backupPassFixture, want ...string) {
	t.Helper()
	var got []string
	for _, c := range f.commandsFor("systemctl") {
		if len(c.Args) != 2 || c.Args[1] != "accountsd-litestream@acme.service" {
			t.Fatalf("unexpected systemctl call %v", c.Args)
		}
		got = append(got, c.Args[0])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("acme unit calls = %v, want %v", got, want)
	}
}
