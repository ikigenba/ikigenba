package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantRestoreUsage = `Usage: opsctl restore SERVICE [--at <timestamp> | --from <uri>]

Replace /opt/SERVICE/etc/ and /opt/SERVICE/state/ with a backup, and, when
SERVICE declares a [database], replace that database with what litestream
holds. Without --at or --from both halves are the newest there is. With
--from, everything comes from the one snapshot at that URI instead, database
included. Nothing under bin/ or share/ is touched.

SERVICE's socket and service are stopped for the restore, socket first so no
request starts the service again mid-restore, and started again after it; so
is litestream.service, because the files step deletes the database the app and
litestream both hold open. A restore is a brief outage. An app whose socket
was already stopped is left stopped, unless an earlier failed restore stopped
it: a successful retry on the same running host starts it again. A disabled
app stays disabled: neither of its units is enabled or started. A failed
restore leaves them all stopped.

Before litestream.service comes back, /etc/litestream.yml is regenerated from
the manifest the restore put in place, so a database restored into a host that
never ran SERVICE is replicated from the start line on.

Options:
  --at <timestamp>    restore the service as it was at this RFC 3339 moment
  --from <uri>        restore the service from the snapshot at this s3:// URI

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself. The two
are not the same instant, because the tarball is written on a timer and the
database is replicated continuously.

--from takes etc/, state/, and the database from a snapshot 'opsctl snapshot'
wrote, and reads neither the backups nor litestream's replica. A snapshot holds
no etc/env, so --from writes it as 'opsctl install' does, from the parameter
/<host.name>/SERVICE and the manifest the snapshot holds. It cannot be
combined with --at.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
  host.name       the fully-qualified name this host answers at
  apps.drain_seconds  how long the app may drain when stopped (default 5)
  backup.service_db_seconds  how often a declared database is snapshotted whole
  backup.service_wal_seconds  how often a declared database's committed changes are shipped
`

func TestRestoreHelpIsExactAndInert(t *testing.T) {
	// R-29HH-CEVJ
	for _, euid := range []int{0, 1000} {
		for _, option := range []string{"--help", "-h"} {
			root := filepath.Join(t.TempDir(), "host-state")
			if err := os.WriteFile(root, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			used := false
			deps := Deps{Root: root, EUID: euid, Execute: func(context.Context, host.Command) (host.Result, error) {
				used = true
				return host.Result{}, errors.New("unexpected process access")
			}, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
				used = true
				return nil, errors.New("unexpected cloud access")
			}}}
			stdout, stderr, code := invokeBackupCLI([]string{"restore", option}, deps)
			if code != 0 || stdout != wantRestoreUsage || stderr != "" || used {
				t.Fatalf("restore %s euid %d = exit %d stdout %q stderr %q used %v", option, euid, code, stdout, stderr, used)
			}
			if info, err := os.Lstat(root); err != nil || !info.Mode().IsRegular() || info.Size() != int64(len("unchanged")) {
				t.Fatalf("help changed host state: %v, %v", info, err)
			}
		}
	}
}

func TestRestoreGrammarAcceptsAtInEitherPosition(t *testing.T) {
	// R-289K-YN4U
	const stamp = "2026-09-16T10:30:00.123Z"
	wantTime, _ := time.Parse(time.RFC3339, stamp)
	for _, args := range [][]string{{"notes", "--at", stamp}, {"--at", stamp, "notes"}} {
		got, message := parseRestoreInvocation(args)
		if message != "" || got.service != "notes" || got.at == nil || !got.at.Equal(wantTime) {
			t.Fatalf("parseRestoreInvocation(%q) = %+v, %q", args, got, message)
		}
	}
	for _, args := range [][]string{{"notes", "--from", "s3://other.example/seed/item.tar.zst"}, {"--from", "s3://other.example/seed/item.tar.zst", "notes"}} {
		got, message := parseRestoreInvocation(args)
		if message != "" || got.service != "notes" || got.at != nil || got.from != "s3://other.example/seed/item.tar.zst" {
			t.Fatalf("from invocation = %+v, %q", got, message)
		}
	}
	got, message := parseRestoreInvocation([]string{"notes"})
	if message != "" || got.service != "notes" || got.at != nil {
		t.Fatalf("newest invocation = %+v, %q", got, message)
	}
}

func TestRestoreGrammarRejectsInvalidInvocationsBeforeHostAccess(t *testing.T) {
	// R-289K-YN4U
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: nil, want: "restore needs SERVICE"},
		{args: []string{"notes", "--from", "s3://bucket"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3://bucket/"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3:///key"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3://u@bucket/key"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3://bucket:443/key"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3:key"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3://bucket/key?x=1"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3://bucket/key?"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3://bucket/key#x"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "s3://bucket/key#"}, want: "--from takes an s3:// URI"},

		{args: []string{"one", "two"}, want: "restore takes one SERVICE"},
		{args: []string{"notes", "--at"}, want: "--at takes an RFC 3339 timestamp"},
		{args: []string{"notes", "--at", "tomorrow"}, want: "--at takes an RFC 3339 timestamp"},
		{args: []string{"--at", "2026-09-16T10:30:00Z", "--at", "2026-09-16T10:30:00Z", "notes"}, want: "duplicate option '--at'"},
		{args: []string{"notes", "--from"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--from", "https://bucket/key"}, want: "--from takes an s3:// URI"},
		{args: []string{"notes", "--at", "2026-09-16T10:30:00Z", "--from", "s3://bucket/key"}, want: "--at and --from cannot be combined"},
		{args: []string{"notes", "--from", "s3://bucket/key", "--at", "2026-09-16T10:30:00Z"}, want: "--at and --from cannot be combined"},
		{args: []string{"notes", "--from", "s3://bucket/key", "--from", "s3://bucket/key"}, want: "duplicate option '--from'"},
		{args: []string{"notes", "--force"}, want: "unknown option '--force'"},
		{args: []string{"notes", "-h"}, want: "unknown option '-h'"},
	} {
		used := false
		root := filepath.Join(t.TempDir(), "host-state")
		if err := os.WriteFile(root, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		deps := Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
			used = true
			return host.Result{}, nil
		}, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			used = true
			return nil, nil
		}}}
		stdout, stderr, code := invokeBackupCLI(append([]string{"restore"}, test.args...), deps)
		wantErr := "opsctl: " + test.want + "\n\nsee 'opsctl restore --help' for usage\n"
		if code != 2 || stdout != "" || stderr != wantErr || used {
			t.Errorf("restore %q = exit %d stdout %q stderr %q used %v", test.args, code, stdout, stderr, used)
		}
	}
}

func TestRestoreInvalidNonRootInvocationReportsGrammarBeforeRefusal(t *testing.T) {
	// R-289K-YN4U
	root := filepath.Join(t.TempDir(), "host-state")
	if err := os.WriteFile(root, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	used := false
	deps := Deps{Root: root, EUID: 1000, Execute: func(context.Context, host.Command) (host.Result, error) {
		used = true
		return host.Result{}, errors.New("unexpected process access")
	}, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
		used = true
		return nil, errors.New("unexpected cloud access")
	}}}
	stdout, stderr, code := invokeBackupCLI([]string{"restore", "--at"}, deps)
	want := "opsctl: --at takes an RFC 3339 timestamp\n\nsee 'opsctl restore --help' for usage\n"
	if code != 2 || stdout != "" || stderr != want || used {
		t.Fatalf("invalid nonroot restore = exit %d stdout %q stderr %q used %v", code, stdout, stderr, used)
	}
	if info, err := os.Lstat(root); err != nil || !info.Mode().IsRegular() || info.Size() != int64(len("unchanged")) {
		t.Fatalf("invalid nonroot restore changed host state: %v, %v", info, err)
	}
}

func TestRestoreCommandReadsHostAndUsesNginxWrite(t *testing.T) {
	// R-XHYL-DFWU R-K9DU-97YV R-LZ82-QJF7
	root := configuredBackupRoot(t)
	store := config.Store{Root: root}
	if err := store.Set("host.name", "HOST.Example.Test."); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("host.apex", "notes"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/nginx/conf.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var/lib/ikigenba"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "var/lib/ikigenba/sentinel"), []byte("untouched sibling"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := newHostCLICloud()
	stamp := "2026-09-16T10:00:00Z"
	uri := "s3://backups.example/host/notes/" + stamp + ".tar.zst"
	archive := makeRestoreCLIArchive(t, map[string]string{
		"etc/manifest.toml": "app = \"notes\"\n",
		"state/value":       "restored",
	})
	client.objects[uri] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, archive...)
	var commands []string
	executeZstd := roundTripZstdExecute(nil)
	execute := func(ctx context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, strings.Join(append([]string{command.Name}, command.Args...), " "))
		if command.Name == "zstd" {
			return executeZstd(ctx, command)
		}
		if command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-notes.socket"}) {
			return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\n")}, nil
		}
		if command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}) {
			return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=disabled\n")}, nil
		}
		if command.Name == "getent" && reflect.DeepEqual(command.Args, []string{"passwd", "ikigenba"}) {
			return host.Result{Stdout: []byte("ikigenba:x:1234:1234::/nonexistent:/usr/sbin/nologin\n")}, nil
		}
		if command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--group", "--name", "ikigenba"}) {
			return host.Result{Stdout: []byte("ikigenba\n")}, nil
		}
		if command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--user", "ikigenba"}) {
			return host.Result{Stdout: []byte("1234\n")}, nil
		}
		if command.Name == "chown" {
			return host.Result{}, nil
		}
		return host.Result{}, fmt.Errorf("unexpected command %q", commands[len(commands)-1])
	}
	stdout, stderr, code := invokeBackupCLI([]string{"restore", "--at", "2026-09-16T10:30:00Z", "notes"}, hostCLIDeps(root, client, execute))
	want := "source: ok (notes/" + stamp + ".tar.zst, 0.0 MiB)\n" +
		"stop: ok (no ikigenba-notes.socket)\n" +
		"files: ok (/opt/notes/etc, /opt/notes/state, 2 files)\n" +
		"start: ok (no ikigenba-notes.socket)\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Fatalf("restore command = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootFS.Close() })
	configuration, err := rootFS.ReadFile("etc/nginx/conf.d/ikigenba.conf")
	if err != nil || !bytes.Contains(configuration, []byte("server_name         notes.host.example.test example.test;")) || bytes.Contains(configuration, []byte("HOST.Example.Test.")) {
		t.Fatalf("nginx.Write output = %q, %v", configuration, err)
	}
	wantCommands := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket",
		"systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		"id --user ikigenba", "id --group --name ikigenba", "getent passwd ikigenba",
		"systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		"id --user ikigenba", "id --group --name ikigenba",
	}
	if len(commands) != len(wantCommands)+1 || !reflect.DeepEqual(commands[:len(wantCommands)], wantCommands) || !strings.HasPrefix(commands[len(wantCommands)], "chown root:ikigenba "+filepath.Join(root, "var/lib/ikigenba")+" "+filepath.Join(root, "var/lib/ikigenba/.services-")) {
		t.Fatalf("commands = %v; nginx callback must use Write without test/reload", commands)
	}
	if got, err := rootFS.ReadFile("var/lib/ikigenba/services.json"); err != nil || string(got) != "{\n  \"services\": []\n}\n" {
		t.Fatalf("services file = %q, %v", got, err)
	}
	if got, err := rootFS.ReadFile("var/lib/ikigenba/sentinel"); err != nil || string(got) != "untouched sibling" {
		t.Fatalf("services sibling = %q, %v", got, err)
	}
	if info, err := rootFS.Lstat("var/lib/ikigenba/sentinel"); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("services sibling mode = %v, %v", info, err)
	}
}

func TestRestoreRegenerationFailuresLeaveServicesFileUnchanged(t *testing.T) {
	// R-K9DU-97YV R-LZ82-QJF7
	for _, test := range []struct {
		name          string
		nginxFails    bool
		wantAccountID int
	}{
		{name: "nginx fails", nginxFails: true},
		{name: "services write fails", wantAccountID: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := configuredBackupRoot(t)
			store := config.Store{Root: root}
			if err := store.Set("host.name", "host.example.test"); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "var/lib/ikigenba"), 0o750); err != nil {
				t.Fatal(err)
			}
			const previous = "previous services file\n"
			servicesPath := filepath.Join(root, "var/lib/ikigenba/services.json")
			if err := os.WriteFile(servicesPath, []byte(previous), 0o600); err != nil {
				t.Fatal(err)
			}
			if !test.nginxFails {
				if err := os.MkdirAll(filepath.Join(root, "etc/nginx/conf.d"), 0o750); err != nil {
					t.Fatal(err)
				}
			}
			client := newHostCLICloud()
			client.objects["s3://backups.example/host/notes/2026-09-16T10:00:00Z.tar.zst"] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, makeRestoreCLITar(t, "restored")...)
			accountCalls := 0
			executeZstd := roundTripZstdExecute(nil)
			execute := func(ctx context.Context, command host.Command) (host.Result, error) {
				if command.Name == "zstd" {
					return executeZstd(ctx, command)
				}
				if command.Name == "systemctl" && len(command.Args) == 4 && command.Args[0] == "show" {
					return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=disabled\n")}, nil
				}
				if command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--user", "ikigenba"}) {
					accountCalls++
					return host.Result{}, errors.New("account unavailable")
				}
				return host.Result{}, fmt.Errorf("unexpected command %q %v", command.Name, command.Args)
			}
			stdout, stderr, code := invokeBackupCLI([]string{"restore", "notes"}, hostCLIDeps(root, client, execute))
			if code != 1 || !strings.Contains(stderr, "nginx regeneration") || !strings.Contains(stdout, "files: ok (") || strings.Contains(stdout, "start:") || accountCalls != test.wantAccountID {
				t.Fatalf("restore = %d, %q, %q, account calls %d", code, stdout, stderr, accountCalls)
			}
			if data := readRestoreServicesFile(t, root); data != previous {
				t.Fatalf("services file = %q", data)
			}
		})
	}
}

func TestRestoreSourceFailureLeavesServicesFileUnchanged(t *testing.T) {
	// R-K9DU-97YV R-LZ82-QJF7
	root := configuredBackupRoot(t)
	store := config.Store{Root: root}
	if err := store.Set("host.name", "host.example.test"); err != nil {
		t.Fatal(err)
	}
	servicesPath := filepath.Join(root, "var/lib/ikigenba/services.json")
	if err := os.MkdirAll(filepath.Dir(servicesPath), 0o750); err != nil {
		t.Fatal(err)
	}
	const previous = "previous services file\n"
	if err := os.WriteFile(servicesPath, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	commands := 0
	stdout, stderr, code := invokeBackupCLI([]string{"restore", "notes"}, hostCLIDeps(root, newHostCLICloud(), func(context.Context, host.Command) (host.Result, error) {
		commands++
		return host.Result{}, errors.New("unexpected command")
	}))
	if code != 1 || !strings.HasPrefix(stdout, "source: failed: ") || !strings.HasPrefix(stderr, "opsctl: restore notes failed at source\n") || commands != 0 {
		t.Fatalf("source failure = %d, %q, %q, commands %d", code, stdout, stderr, commands)
	}
	if data := readRestoreServicesFile(t, root); data != previous {
		t.Fatalf("services file = %q", data)
	}
}

func readRestoreServicesFile(t *testing.T, root string) string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()
	data, err := rootFS.ReadFile("var/lib/ikigenba/services.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRestoreCommandAtReachesDomainSelectionInEitherPosition(t *testing.T) {
	// R-289K-YN4U R-XHYL-DFWU R-2J8O-EKT3
	const (
		older  = "2026-09-16T10:00:00Z"
		later  = "2026-09-16T11:00:00Z"
		cutoff = "2026-09-16T10:30:00Z"
	)
	for _, test := range []struct {
		name      string
		args      []string
		wantStamp string
		wantValue string
	}{
		{name: "option before service", args: []string{"restore", "--at", cutoff, "notes"}, wantStamp: older, wantValue: "older"},
		{name: "option after service", args: []string{"restore", "notes", "--at", cutoff}, wantStamp: older, wantValue: "older"},
		{name: "nil selects newest", args: []string{"restore", "notes"}, wantStamp: later, wantValue: "later"},
		{name: "from before service", args: []string{"restore", "--from", "s3://other.example/seed/object.tar.zst", "notes"}, wantValue: "snapshot"},
		{name: "from after service", args: []string{"restore", "notes", "--from", "s3://other.example/seed/object.tar.zst"}, wantValue: "snapshot"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := configuredBackupRoot(t)
			store := config.Store{Root: root}
			if err := store.Set("host.name", "host.example.test"); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "etc/nginx/conf.d"), 0o750); err != nil {
				t.Fatal(err)
			}
			client := newHostCLICloud()
			client.objects["s3://other.example/seed/object.tar.zst"] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, makeRestoreCLITar(t, "snapshot")...)
			for stamp, value := range map[string]string{older: "older", later: "later"} {
				uri := "s3://backups.example/host/notes/" + stamp + ".tar.zst"
				client.objects[uri] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, makeRestoreCLITar(t, value)...)
			}
			executeZstd := roundTripZstdExecute(nil)
			execute := func(ctx context.Context, command host.Command) (host.Result, error) {
				if command.Name == "zstd" {
					return executeZstd(ctx, command)
				}
				if command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-notes.socket"}) {
					return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\n")}, nil
				}
				if command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}) {
					return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=disabled\n")}, nil
				}
				if command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--user", "ikigenba"}) {
					return host.Result{Stdout: []byte("1234\n")}, nil
				}
				if command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--group", "--name", "ikigenba"}) {
					return host.Result{Stdout: []byte("ikigenba\n")}, nil
				}
				if command.Name == "chown" {
					return host.Result{}, nil
				}
				return host.Result{}, fmt.Errorf("unexpected command %q %v", command.Name, command.Args)
			}

			stdout, stderr, code := invokeBackupCLI(test.args, hostCLIDeps(root, client, execute))
			wantPrefix := "source: ok (notes/" + test.wantStamp + ".tar.zst, "
			if test.wantValue == "snapshot" {
				wantPrefix = "source: ok (s3://other.example/seed/object.tar.zst, "
			}
			if code != 0 || stderr != "" || !strings.HasPrefix(stdout, wantPrefix) {
				t.Fatalf("%q = exit %d stdout %q stderr %q, want source prefix %q", test.args, code, stdout, stderr, wantPrefix)
			}
			rootFS, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := rootFS.ReadFile("opt/notes/state/value")
			closeErr := rootFS.Close()
			if err := errors.Join(readErr, closeErr); err != nil || string(data) != test.wantValue {
				t.Fatalf("selected restore contents = %q, %v, want %q", data, err, test.wantValue)
			}
		})
	}
}

func TestRestoreCommandRetainsDomainStopFailureEndToEnd(t *testing.T) {
	// R-XHYL-DFWU
	root := configuredBackupRoot(t)
	store := config.Store{Root: root}
	if err := store.Set("host.name", "host.example.test"); err != nil {
		t.Fatal(err)
	}
	client := newHostCLICloud()
	stamp := "2026-09-16T10:00:00Z"
	uri := "s3://backups.example/host/notes/" + stamp + ".tar.zst"
	archive := makeRestoreCLIArchive(t, map[string]string{
		"etc/manifest.toml": "app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n",
		"state/value":       "restored",
	})
	client.objects[uri] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, archive...)
	var commands []string
	executeZstd := roundTripZstdExecute(nil)
	execute := func(ctx context.Context, command host.Command) (host.Result, error) {
		text := strings.Join(append([]string{command.Name}, command.Args...), " ")
		commands = append(commands, text)
		switch {
		case command.Name == "zstd":
			return executeZstd(ctx, command)
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-notes.socket"}):
			return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=active\n")}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}):
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"show", "--property=LoadState", "--property=ActiveState", "litestream.service"}):
			return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=active\n")}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"stop", "ikigenba-notes.socket"}):
			return host.Result{}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"stop", "ikigenba-notes.service"}):
			return host.Result{}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"stop", "litestream.service"}):
			return host.Result{ExitCode: 1, Stdout: []byte("partial stop\n"), Stderr: []byte("bus secret\r\n")}, nil
		default:
			return host.Result{}, fmt.Errorf("unexpected command %q", text)
		}
	}

	stdout, stderr, code := invokeBackupCLI([]string{"restore", "notes"}, hostCLIDeps(root, client, execute))
	wantOut := "source: ok (notes/" + stamp + ".tar.zst, 0.0 MiB)\n" +
		"stop: failed: stop litestream.service: exit status 1\n"
	wantErr := "opsctl: restore notes failed at stop\n\n" +
		"ikigenba-notes.socket and ikigenba-notes.service were left stopped\n" +
		"> partial stop\n> bus secret\r\n"
	if code != 1 || stdout != wantOut || stderr != wantErr {
		t.Fatalf("restore stop failure = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	wantCommands := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket",
		"systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		"systemctl stop ikigenba-notes.socket",
		"systemctl stop ikigenba-notes.service",
		"systemctl show --property=LoadState --property=ActiveState litestream.service",
		"systemctl stop litestream.service",
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %v, want %v; failure must stop the workflow", commands, wantCommands)
	}
	if strings.Contains(stdout, "partial stop") || strings.Contains(stdout, "bus secret") || strings.Contains(stderr, "exit status 1") {
		t.Fatalf("report reason duplicated or captured output leaked: stdout %q stderr %q", stdout, stderr)
	}
}

func TestRestoreCommandRequiresHostNameBeforeDomainAccess(t *testing.T) {
	// R-XHYL-DFWU
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
		want  string
	}{
		{name: "missing", setup: func(*testing.T, string) {}, want: "opsctl: host.name not set\n"},
		{name: "empty", setup: func(t *testing.T, root string) {
			if err := (config.Store{Root: root}).Set("host.name", ""); err != nil {
				t.Fatal(err)
			}
		}, want: "opsctl: host.name not set\n"},
		{name: "empty after normalization", setup: func(t *testing.T, root string) {
			if err := (config.Store{Root: root}).Set("host.name", "."); err != nil {
				t.Fatal(err)
			}
		}, want: "opsctl: host.name not set\n"},
		{name: "corrupt", setup: func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "etc/ikigenba"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "etc/ikigenba/config.json"), []byte("[]"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: " is corrupt\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.setup(t, root)
			used := false
			deps := Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) { used = true; return host.Result{}, nil }, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { used = true; return nil, nil }}}
			stdout, stderr, code := invokeBackupCLI([]string{"restore", "notes"}, deps)
			if code != 1 || stdout != "" || used || !strings.HasSuffix(stderr, test.want) {
				t.Fatalf("host.name %s = exit %d stdout %q stderr %q used %v", test.name, code, stdout, stderr, used)
			}
		})
	}
}

func TestRestoreCommandReportsHostApexReadFailure(t *testing.T) {
	// R-XHYL-DFWU
	root := t.TempDir()
	var keys []string
	store := restoreStoreFunc(func(key string) (string, error) {
		keys = append(keys, key)
		if key == "host.name" {
			return "host.example.test", nil
		}
		return "", errors.New("apex read failed")
	})
	used := false
	deps := Deps{
		Root: root,
		EUID: 0,
		Execute: func(context.Context, host.Command) (host.Result, error) {
			used = true
			return host.Result{}, errors.New("unexpected process access")
		},
		Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			used = true
			return nil, errors.New("unexpected cloud access")
		}},
	}
	var stdout, stderr bytes.Buffer
	code := runRestoreWithStore([]string{"notes"}, &stdout, &stderr, deps, store)
	wantErr := "opsctl: config get failed for \"" + filepath.Join(root, "etc/ikigenba/config.json") + "\": apex read failed\n"
	if code != exitFail || stdout.String() != "" || stderr.String() != wantErr || used {
		t.Fatalf("host.apex read failure = exit %d stdout %q stderr %q used %v", code, stdout.String(), stderr.String(), used)
	}
	if !reflect.DeepEqual(keys, []string{"host.name", "host.apex"}) {
		t.Fatalf("configuration reads = %v, want host.name then host.apex", keys)
	}
}

func TestRestoreCommandRejectsApexWithoutParentBeforeRestore(t *testing.T) {
	// R-XHYL-DFWU R-2J8O-EKT3
	root := configuredBackupRoot(t)
	store := config.Store{Root: root}
	if err := store.Set("host.name", "LOCALHOST."); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("host.apex", "notes"); err != nil {
		t.Fatal(err)
	}
	used := false
	deps := Deps{
		Root: root,
		EUID: 0,
		Execute: func(context.Context, host.Command) (host.Result, error) {
			used = true
			return host.Result{}, errors.New("unexpected process access")
		},
		Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			used = true
			return nil, errors.New("unexpected cloud access")
		}},
	}
	for _, args := range [][]string{{"restore", "notes"}, {"restore", "notes", "--from", "s3://bucket/key"}} {
		stdout, stderr, code := invokeBackupCLI(args, deps)
		if code != 1 || stdout != "" || stderr != "opsctl: host.apex is set but host.name 'localhost' has no parent domain\n" || used {
			t.Fatalf("invalid restore apex = exit %d stdout %q stderr %q used %v", code, stdout, stderr, used)
		}
		for _, name := range []string{"opt/notes", "run/opsctl/restore/notes.active", "etc/nginx/conf.d/ikigenba.conf"} {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid restore apex changed %s: %v", name, err)
			}
		}
	}
}

func TestRestoreOutcomePreservesReportsStoppedUnitsAndCommandDetail(t *testing.T) {
	// R-XHYL-DFWU
	commandErr := &host.CommandError{Label: "stop litestream.service", Result: host.Result{ExitCode: 1, Stdout: []byte("partial\n"), Stderr: []byte("secret\r\n")}}
	report := backup.RestoreReport{Steps: []backup.RestoreStep{
		{Name: "source", Detail: "notes/stamp.tar.zst, 1.0 MiB"},
		{Name: "stop", Err: fmt.Errorf("stop refused\nprivate detail: %w", commandErr)},
	}}
	runErr := &backup.RestoreError{Service: "notes", Stage: "stop", Err: commandErr, Stopped: []string{"ikigenba-notes.socket", "ikigenba-notes.service", "litestream.service"}}
	var stdout, stderr bytes.Buffer
	code := renderRestoreOutcome(&stdout, &stderr, "notes", report, runErr)
	wantOut := "source: ok (notes/stamp.tar.zst, 1.0 MiB)\nstop: failed: stop refused\n"
	wantErr := "opsctl: restore notes failed at stop\n\nikigenba-notes.socket, ikigenba-notes.service, and litestream.service were left stopped\n> partial\n> secret\r\n"
	if code != exitFail || stdout.String() != wantOut || stderr.String() != wantErr {
		t.Fatalf("restore failure = exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "private detail") || strings.Contains(stdout.String(), "partial") || strings.Contains(stdout.String(), "secret") {
		t.Fatalf("failed finding leaked detail: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = renderRestoreOutcome(&stdout, &stderr, "notes", backup.RestoreReport{}, errors.New("backup.s3_uri not set"))
	if code != exitFail || stdout.Len() != 0 || stderr.String() != "opsctl: backup.s3_uri not set\n" {
		t.Fatalf("preworkflow failure = exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	callbackErr := &backup.RestoreError{Service: "notes", Stage: "nginx regeneration", Err: errors.New("publish failed"), Stopped: []string{"ikigenba-notes.service"}}
	code = renderRestoreOutcome(&stdout, &stderr, "notes", backup.RestoreReport{Steps: []backup.RestoreStep{{Name: "files", Detail: "published"}}}, callbackErr)
	wantOut = "files: ok (published)\n"
	wantErr = "opsctl: notes: nginx regeneration: publish failed\n\nikigenba-notes.service was left stopped\n"
	if code != exitFail || stdout.String() != wantOut || stderr.String() != wantErr {
		t.Fatalf("unreported callback failure = exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRestoreReportWriteFailureIsOperational(t *testing.T) {
	// R-XHYL-DFWU
	report := backup.RestoreReport{Steps: []backup.RestoreStep{{Name: "source", Detail: "ready"}}}
	var stderr bytes.Buffer
	code := renderRestoreOutcome(failingRestoreWriter{}, &stderr, "notes", report, nil)
	if code != exitFail || stderr.String() != "opsctl: restore notes report not produced\n" {
		t.Fatalf("report write failure = exit %d stderr %q", code, stderr.String())
	}
}

type failingRestoreWriter struct{}

func (failingRestoreWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type restoreStoreFunc func(string) (string, error)

func (f restoreStoreFunc) Get(key string) (string, error) { return f(key) }

func makeRestoreCLITar(t *testing.T, content string) []byte {
	return makeRestoreCLIArchive(t, map[string]string{"state/value": content})
}

func makeRestoreCLIArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg,
			Uid: os.Getuid(), Gid: os.Getgid(),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestRestoreFromFailuresRenderReportedSteps(t *testing.T) {
	// R-2J8O-EKT3
	const uri = "s3://other.example/seed/object.tar.zst"
	for _, stage := range []string{"source", "secrets"} {
		t.Run(stage, func(t *testing.T) {
			root := configuredBackupRoot(t)
			if err := (config.Store{Root: root}).Set("host.name", "HOST.Example.Test."); err != nil {
				t.Fatal(err)
			}
			client := &restoreFromCLICloud{hostCLICloud: newHostCLICloud()}
			if stage == "secrets" {
				client.objects[uri] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, makeRestoreCLIArchive(t, map[string]string{"etc/manifest.toml": "app = \"notes\"\nsecrets = [\"TOKEN\"]\n"})...)
			}
			executeZstd := roundTripZstdExecute(nil)
			deps := Deps{Root: root, EUID: 0, Now: func() time.Time { return time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC) }, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }}, Execute: func(ctx context.Context, command host.Command) (host.Result, error) {
				if command.Name == "zstd" {
					return executeZstd(ctx, command)
				}
				t.Fatalf("unexpected process %s %v", command.Name, command.Args)
				return host.Result{}, nil
			}}
			stdout, stderr, code := invokeBackupCLI([]string{"restore", "notes", "--from", uri}, deps)
			if code != 1 || stderr != "opsctl: restore notes failed at "+stage+"\n" || !strings.Contains(stdout, stage+": failed: ") {
				t.Fatalf("%s failure = %d %q %q", stage, code, stdout, stderr)
			}
			if stage == "source" && stdout != "source: failed: "+uri+": no such object\n" {
				t.Fatalf("source row = %q", stdout)
			}
			if stage == "secrets" && (!strings.HasPrefix(stdout, "source: ok (") || client.parameter != "/host.example.test/notes") {
				t.Fatalf("secrets output/parameter = %q / %q", stdout, client.parameter)
			}
			if _, err := os.Stat(filepath.Join(root, "opt/notes")); !os.IsNotExist(err) {
				t.Fatalf("failed restore wrote service: %v", err)
			}
		})
	}
}

type restoreFromCLICloud struct {
	*hostCLICloud
	parameter string
}

func (client *restoreFromCLICloud) ReadSecrets(_ context.Context, parameter string) (map[string]string, error) {
	client.parameter = parameter
	return map[string]string{}, nil
}
