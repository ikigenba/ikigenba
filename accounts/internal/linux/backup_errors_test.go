package linux_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const acmeStreamUnit = "accountsd-litestream@acme.service"

func TestBackupRedactsCommandDiagnostics(t *testing.T) {
	// R-56WT-T1S0
	const key, secret, token = "OutputKey", "OutputSecret", "OutputSessionToken"
	const longerKey, crossToken = "OutputKeyFromBravo", "AnotherAccountSessionToken"
	const crossPrefix = "AnotherAccountSession"
	const message = "failure " + key + " / " + secret + " / " + token
	const masked = "failure " + machine.Redacted + " / " + machine.Redacted + " / " + machine.Redacted
	forgetPrefix := (&linux.CommandError{Label: "restic forget", Result: linux.Result{ExitCode: 1}}).Error() + ": "
	beforeErrorLimit := strings.Repeat("x", protocol.MaxBackupError-len(forgetPrefix)-10)
	lastLine := "\ncut " + crossPrefix
	fullCapture := strings.Repeat("x", linux.MaxCapture-len(lastLine)) + lastLine
	for _, tc := range []struct {
		name, program, verb, output, want string
	}{
		{name: "restic forget", program: "restic", verb: "forget", output: message, want: masked},
		{name: "litestream status", program: "litestream", verb: "status", output: message, want: masked},
		{name: "another account and longest first", program: "restic", verb: "cat",
			output: longerKey + " / " + crossToken, want: machine.Redacted + " / " + machine.Redacted},
		{name: "mask before error limit", program: "restic", verb: "forget",
			output: beforeErrorLimit + token, want: beforeErrorLimit + machine.Redacted},
		{name: "exact capture", program: "restic", verb: "cat",
			output: fullCapture, want: "cut " + machine.Redacted},
		{name: "short capture", program: "restic", verb: "cat",
			output: fullCapture[1:], want: "cut " + crossPrefix},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put("/srv/acme/data/app.db", walSQLite, 0o600)
			f.state.Backup.Host.AccessKeyID = "status"
			f.state.Backup.Accounts[0].Credentials = protocol.Credentials{AccessKeyID: key, SecretAccessKey: secret, SessionToken: token}
			f.state.Backup.Accounts = append(f.state.Backup.Accounts, protocol.AccountCredentials{Account: "bravo",
				Credentials: protocol.Credentials{AccessKeyID: longerKey, SessionToken: crossToken}})
			var fail bool
			f.execute = func(c linux.Command) (linux.Result, error) {
				switch c.Name {
				case f.path(linux.ResticPath):
					if fail && tc.program == "restic" && c.Args[3] == tc.verb {
						return linux.Result{ExitCode: 1, Stderr: []byte(tc.output)}, nil
					}
					if c.Args[3] == "backup" {
						f.summary(c)
					}
				case f.path(linux.LitestreamPath):
					if fail && tc.program == "litestream" && c.Args[0] == tc.verb {
						return linux.Result{ExitCode: 1, Stderr: []byte(tc.output)}, nil
					}
				}
				return linux.Result{}, nil
			}
			f.stream()
			pass := f.host.Stream
			if tc.program == "restic" {
				pass = f.host.Backup
			} else {
				f.backup()
			}
			fail = true
			label := tc.program + " " + tc.verb
			want := (&linux.CommandError{Label: label, Result: linux.Result{ExitCode: 1}}).Error() + ": " + tc.want
			if err := pass(t.Context(), f.state); err == nil || err.Error() != want {
				t.Fatalf("pass error = %v, want %q", err, want)
			}
			if got := f.report().Backups[0].Error; got != want {
				t.Fatalf("reported error = %q, want %q", got, want)
			}
		})
	}
}

func TestBackupDecodesCommandOutputWithCredentialBytes(t *testing.T) {
	// R-56WT-T1S0
	for _, stamp := range []string{"2025-11-26T12:00:00Z", "2026-11-26T12:00:00Z"} {
		t.Run(stamp, func(t *testing.T) {
			f := newBackupPassFixture(t)
			const path = "/srv/acme/data/2025.db"
			f.state.Backup.Accounts[0].Credentials.AccessKeyID = "2025"
			f.execute = func(c linux.Command) (linux.Result, error) {
				switch c.Name {
				case f.path(linux.ResticPath):
					switch c.Args[3] {
					case "backup":
						f.output(c, fmt.Sprintf(`{"message_type":"summary","snapshot_id":"fixture","backup_end":%q}`+"\n", stamp))
					case "snapshots":
						f.output(c, fmt.Sprintf(`[{"time":%q}]`, stamp))
					}
				case f.path(linux.LitestreamPath):
					switch c.Args[0] {
					case "status":
						f.output(c, fmt.Sprintf(`[{"database":%q,"status":"ok","local_txid":"2025"}]`, path))
					case "ltx":
						f.output(c, fmt.Sprintf(`[{"timestamp":%q,"max_txid":"2025"}]`, stamp))
					}
				}
				return linux.Result{}, nil
			}
			f.put(path, walSQLite, 0o600)
			f.stream()
			f.backup()
			want, err := time.Parse(time.RFC3339, stamp)
			if err != nil {
				t.Fatal(err)
			}
			got := f.report().Backups[0]
			if !got.Snapshot.Equal(want) || len(got.Snapshots) != 1 || got.Snapshots[0] != want.Unix() {
				t.Fatalf("snapshot times = %v and %v, want %v", got.Snapshot, got.Snapshots, want)
			}
			if len(got.Databases) != 1 {
				t.Fatalf("databases = %+v", got.Databases)
			}
			db := got.Databases[0]
			if db.Path != "data/2025.db" || !db.Written.Equal(want) || !db.Oldest.Equal(want) || !db.Synced.Equal(f.at.Truncate(time.Second)) {
				t.Fatalf("decoded database facts = %+v, want timestamps %v and sync %v", db, want, f.at)
			}
		})
	}
}

func TestBackupRedactsWrappedExecutionFailures(t *testing.T) {
	// R-56WT-T1S0
	for _, tc := range []struct {
		name string
		wrap func(string, error) error
	}{
		{name: "path error", wrap: func(path string, cause error) error {
			return &os.PathError{Op: "fork/exec", Path: path, Err: cause}
		}},
		{name: "execution error", wrap: func(name string, cause error) error {
			return &exec.Error{Name: name, Err: cause}
		}},
		{name: "wrapped cause", wrap: func(_ string, cause error) error {
			return cause
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupPassFixture(t)
			f.put("/srv/acme/data/app.db", plainSQLite, 0o600)
			const prefix, suffix = "Expired", "Key"
			f.state.Backup.Accounts[0].Credentials.SessionToken = prefix + suffix
			program := f.path(linux.ResticPath)
			cause := fmt.Errorf("%s%w", prefix, errors.New(suffix))
			expectedCause := errors.New(machine.Redacted)
			want := (&linux.CommandError{Label: "restic cat", Err: fmt.Errorf("start failed: %w", tc.wrap(program, expectedCause))}).Error()
			f.stream()
			f.execute = func(c linux.Command) (linux.Result, error) {
				if c.Name == program && c.Args[3] == "cat" {
					return linux.Result{}, fmt.Errorf("start failed: %w", tc.wrap(c.Name, cause))
				}
				return linux.Result{}, nil
			}
			err := f.host.Backup(t.Context(), f.state)
			if err == nil || err.Error() != want {
				t.Fatal("backup failure did not redact the credential across the wrapper boundary")
			}
			got := inspectBackupFacts(t, linux.New(f.env), f.state).Backups[0].Error
			if got != want {
				t.Fatal("persisted failure did not redact the credential across the wrapper boundary")
			}
		})
	}
}

func TestStreamPassErrorsPersistByMethod(t *testing.T) {
	// R-6KEZ-OP5L R-755A-6SRE R-72PH-F9A0
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
	// R-6KEZ-OP5L R-709O-NPSM
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
	// R-6KEZ-OP5L R-72PH-F9A0
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
	// R-6KEZ-OP5L
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
	// R-6KEZ-OP5L
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
	// R-72PH-F9A0
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
	// R-709O-NPSM R-6KEZ-OP5L
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
