package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantSnapshotUsage = `Usage: opsctl snapshot [SERVICE]

Copy every service's etc/ and state/, and the database of a service that
declares a [database], to snapshots/<service>/ under the prefix in
backup.s3_uri, or just SERVICE when one is named. Every snapshot of one run
carries the same timestamp.

The database copy is rebuilt from the replica litestream.service keeps, so
nothing is stopped; it may trail the live database by the changes litestream
has not yet shipped. Never copied: cache/; etc/env, which holds the service's
secrets; anything opsctl generates; and the database's -wal and -shm and its
litestream metadata directory.

'opsctl restore SERVICE --from URI' puts a snapshot back.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
`

func TestSnapshotCommandReportsResultsAndOperationalInterruptions(t *testing.T) {
	// R-1R6Z-LUR4
	t.Run("all services and named service", func(t *testing.T) {
		root := configuredBackupRoot(t)
		writeBackupServiceFile(t, root, "zeta", "zeta")
		writeBackupServiceFile(t, root, "alpha", "alpha")
		client := &backupCLICloud{failService: "zeta"}
		deps := backupCLIDeps(root, client, successfulBackupExecute)

		stdout, stderr, code := invokeBackupCLI([]string{"snapshot"}, deps)
		basename := "2026-09-16T12:34:56Z.tar.zst"
		want := "alpha: ok (s3://backups.example/host/snapshots/alpha/" + basename + ", 1.5 MiB)\n" +
			"zeta: failed: upload unavailable\n"
		if code != 1 || stdout != want || stderr != "" {
			t.Fatalf("snapshot all = exit %d stdout %q stderr %q, want exit 1 stdout %q and empty stderr", code, stdout, stderr, want)
		}
		if len(client.puts) != 2 || !strings.Contains(client.puts[0], "/alpha/") || !strings.Contains(client.puts[1], "/zeta/") {
			t.Fatalf("snapshot all uploads = %v", client.puts)
		}

		client.failService = ""
		client.puts = nil
		stdout, stderr, code = invokeBackupCLI([]string{"snapshot", "zeta"}, deps)
		want = "zeta: ok (s3://backups.example/host/snapshots/zeta/" + basename + ", 1.5 MiB)\n"
		if code != 0 || stdout != want || stderr != "" {
			t.Fatalf("snapshot zeta = exit %d stdout %q stderr %q, want exit 0 stdout %q and empty stderr", code, stdout, stderr, want)
		}
		if len(client.puts) != 1 || !strings.Contains(client.puts[0], "/zeta/") {
			t.Fatalf("named backup uploads = %v", client.puts)
		}
	})

	t.Run("preworkflow failure keeps its reason", func(t *testing.T) {
		stdout, stderr, code := invokeBackupCLI([]string{"snapshot"}, Deps{Root: t.TempDir(), EUID: 0})
		if code != 1 || stdout != "" || stderr != "opsctl: backup.s3_uri not set\n" {
			t.Fatalf("preworkflow failure = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})

	t.Run("explicit empty service does not select all services", func(t *testing.T) {
		root := configuredBackupRoot(t)
		writeBackupServiceFile(t, root, "alpha", "unrelated")
		client := &backupCLICloud{}
		processUsed := false
		cloudOpened := false
		deps := backupCLIDeps(root, client, func(context.Context, host.Command) (host.Result, error) {
			processUsed = true
			return host.Result{}, errors.New("unexpected process access")
		})
		deps.Cloud.Open = func(context.Context, string) (cloud.Client, error) {
			cloudOpened = true
			return client, nil
		}

		stdout, stderr, code := invokeBackupCLI([]string{"snapshot", ""}, deps)
		if code != 1 || stdout != "" || stderr != "opsctl: invalid service \"\"\n" {
			t.Fatalf("empty service = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
		if processUsed || cloudOpened || len(client.puts) != 0 {
			t.Fatalf("empty service used process=%v cloud=%v uploads=%v", processUsed, cloudOpened, client.puts)
		}
	})

	t.Run("interrupted archive identifies the attempted service", func(t *testing.T) {
		root := configuredBackupRoot(t)
		writeBackupServiceFile(t, root, "alpha", "alpha")
		execute := func(_ context.Context, command host.Command) (host.Result, error) {
			switch command.Name {
			case "getent":
				return host.Result{ExitCode: 2}, nil
			case "zstd":
				return host.Result{Stdout: []byte("partial stdout\n"), Stderr: []byte("partial stderr")}, context.Canceled
			default:
				return host.Result{}, errors.New("unexpected command: " + command.Name)
			}
		}
		stdout, stderr, code := invokeBackupCLI([]string{"snapshot"}, backupCLIDeps(root, &backupCLICloud{}, execute))
		wantOut := "alpha: failed: compress \"alpha\" archive: context canceled\n"
		wantErr := "opsctl: snapshot failed at alpha\n\n> partial stdout\n> partial stderr\n"
		if code != 1 || stdout != wantOut || stderr != wantErr {
			t.Fatalf("interrupted backup = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
		if strings.Contains(stdout, "partial stdout") || strings.Contains(stdout, "partial stderr") {
			t.Fatalf("captured subprocess output leaked to stdout %q", stdout)
		}
	})

	t.Run("interruption between attempts has no service suffix", func(t *testing.T) {
		results := []backup.SnapshotResult{{Service: "alpha", Err: errors.New("archive unavailable")}}
		var stdout, stderr bytes.Buffer
		allOK, err := writeSnapshotResults(&stdout, results)
		if err != nil || allOK {
			t.Fatalf("write failed result = allOK %v, error %v", allOK, err)
		}
		code := writeSnapshotOperationalError(&stderr, results, context.Canceled)
		if got, want := stdout.String(), "alpha: failed: archive unavailable\n"; got != want {
			t.Fatalf("between-attempt report = %q, want %q", got, want)
		}
		if got, want := stderr.String(), "opsctl: snapshot failed\n"; got != want {
			t.Fatalf("between-attempt diagnostic = %q, want %q", got, want)
		}
		if code != 1 {
			t.Fatalf("between-attempt exit = %d, want 1", code)
		}
	})

	t.Run("interruption after successful attempt has no service suffix", func(t *testing.T) {
		results := []backup.SnapshotResult{{Service: "alpha", URI: "alpha.tar.zst", Size: 1048576}}
		var stdout, stderr bytes.Buffer
		allOK, err := writeSnapshotResults(&stdout, results)
		if err != nil || !allOK {
			t.Fatalf("write successful result = allOK %v, error %v", allOK, err)
		}
		code := writeSnapshotOperationalError(&stderr, results, context.Canceled)
		if got, want := stdout.String(), "alpha: ok (alpha.tar.zst, 1.0 MiB)\n"; got != want {
			t.Fatalf("between-attempt report = %q, want %q", got, want)
		}
		if got, want := stderr.String(), "opsctl: snapshot failed\n"; got != want {
			t.Fatalf("between-attempt diagnostic = %q, want %q", got, want)
		}
		if code != 1 {
			t.Fatalf("between-attempt exit = %d, want 1", code)
		}
	})
}

func TestSnapshotGrammarRejectsOptionsAndExcessOperandsBeforeHostAccess(t *testing.T) {
	// R-1SEV-ZMHT
	tests := [][]string{
		{"snapshot", "--unknown"},
		{"snapshot", "-V"},
		{"snapshot", "alpha", "beta"},
		{"snapshot", "alpha", "--help"},
	}
	for _, args := range tests {
		root := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(root, []byte("host state"), 0o600); err != nil {
			t.Fatal(err)
		}
		used := false
		deps := Deps{
			Root: root,
			EUID: 1000,
			Execute: func(context.Context, host.Command) (host.Result, error) {
				used = true
				return host.Result{}, nil
			},
			Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
				used = true
				return nil, nil
			}},
		}
		stdout, stderr, code := invokeBackupCLI(args, deps)
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "opsctl: ") || !strings.HasSuffix(stderr, "\n\nsee 'opsctl snapshot --help' for usage\n") {
			t.Errorf("%q = exit %d stdout %q stderr %q", args, code, stdout, stderr)
		}
		if used {
			t.Errorf("%q accessed a process or cloud boundary", args)
		}
		info, err := os.Lstat(root)
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len("host state")) {
			t.Errorf("%q changed host state: info %v error %v", args, info, err)
		}
	}
}

func TestSnapshotHelpIsExactAndHostIndependent(t *testing.T) {
	// R-1UUO-R5Z7
	for _, euid := range []int{0, 1000} {
		for _, option := range []string{"--help", "-h"} {
			root := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(root, []byte("host state"), 0o600); err != nil {
				t.Fatal(err)
			}
			used := false
			deps := Deps{
				Root: root,
				EUID: euid,
				Execute: func(context.Context, host.Command) (host.Result, error) {
					used = true
					return host.Result{}, nil
				},
				Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
					used = true
					return nil, nil
				}},
			}
			stdout, stderr, code := invokeBackupCLI([]string{"snapshot", option}, deps)
			if code != 0 || stdout != wantSnapshotUsage || stderr != "" {
				t.Errorf("snapshot %s as euid %d = exit %d stdout %q stderr %q", option, euid, code, stdout, stderr)
			}
			if used {
				t.Errorf("snapshot %s as euid %d accessed a process or cloud boundary", option, euid)
			}
			info, err := os.Lstat(root)
			if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len("host state")) {
				t.Errorf("snapshot %s changed host state: info %v error %v", option, info, err)
			}
		}
	}
}

func TestSnapshotValidGrammarRequiresRoot(t *testing.T) {
	// R-1SEV-ZMHT
	for _, args := range [][]string{{"snapshot"}, {"snapshot", "notes"}} {
		root := filepath.Join(t.TempDir(), "host-state")
		if err := os.WriteFile(root, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		deps := Deps{Root: root, EUID: 1000, Execute: func(context.Context, host.Command) (host.Result, error) {
			t.Fatal("unexpected command")
			return host.Result{}, nil
		}, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { t.Fatal("unexpected cloud"); return nil, nil }}}
		stdout, stderr, code := invokeBackupCLI(args, deps)
		if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
			t.Fatalf("root refusal = %d %q %q", code, stdout, stderr)
		}
	}
}
