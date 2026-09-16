package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantBackupUsage = `Usage: opsctl backup [SERVICE]

Copy every service's etc/ and state/ to the prefix in backup.s3_uri, under the
service's own name, or just SERVICE when one is named.

Never copied: cache/, anything opsctl generates, and -- for a service that
declares a [database] -- the database file, its -wal and -shm, and its
litestream metadata directory. Those are replicated continuously by
litestream.service. The host's own /etc/ is 'opsctl host backup'.

A service declares its database with a [database] table in etc/manifest.toml
naming its engine and its path. 'opsctl init' writes the timer that runs this
at backup.service_files_seconds.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
`

func TestBackupCommandReportsResultsAndOperationalInterruptions(t *testing.T) {
	// R-I252-Y8ZD
	t.Run("all services and named service", func(t *testing.T) {
		root := configuredBackupRoot(t)
		writeBackupServiceFile(t, root, "zeta", "zeta")
		writeBackupServiceFile(t, root, "alpha", "alpha")
		client := &backupCLICloud{failService: "zeta"}
		deps := backupCLIDeps(root, client, successfulBackupExecute)

		stdout, stderr, code := invokeBackupCLI([]string{"backup"}, deps)
		basename := "2026-09-16T12:34:56Z.tar.zst"
		want := "alpha: ok (" + basename + ", 1.5 MiB)\n" +
			"zeta: failed: upload \"s3://backups.example/host/zeta/" + basename + "\": upload unavailable\n"
		if code != 1 || stdout != want || stderr != "" {
			t.Fatalf("backup all = exit %d stdout %q stderr %q, want exit 1 stdout %q and empty stderr", code, stdout, stderr, want)
		}
		if len(client.puts) != 2 || !strings.Contains(client.puts[0], "/alpha/") || !strings.Contains(client.puts[1], "/zeta/") {
			t.Fatalf("backup all uploads = %v", client.puts)
		}

		client.failService = ""
		client.puts = nil
		stdout, stderr, code = invokeBackupCLI([]string{"backup", "zeta"}, deps)
		want = "zeta: ok (" + basename + ", 1.5 MiB)\n"
		if code != 0 || stdout != want || stderr != "" {
			t.Fatalf("backup zeta = exit %d stdout %q stderr %q, want exit 0 stdout %q and empty stderr", code, stdout, stderr, want)
		}
		if len(client.puts) != 1 || !strings.Contains(client.puts[0], "/zeta/") {
			t.Fatalf("named backup uploads = %v", client.puts)
		}
	})

	t.Run("preworkflow failure keeps its reason", func(t *testing.T) {
		stdout, stderr, code := invokeBackupCLI([]string{"backup"}, Deps{Root: t.TempDir(), EUID: 0})
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

		stdout, stderr, code := invokeBackupCLI([]string{"backup", ""}, deps)
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
		stdout, stderr, code := invokeBackupCLI([]string{"backup"}, backupCLIDeps(root, &backupCLICloud{}, execute))
		wantOut := "alpha: failed: compress \"alpha\" archive: context canceled\n"
		wantErr := "opsctl: backup failed at alpha\n\n> partial stdout\n> partial stderr\n"
		if code != 1 || stdout != wantOut || stderr != wantErr {
			t.Fatalf("interrupted backup = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
		if strings.Contains(stdout, "partial stdout") || strings.Contains(stdout, "partial stderr") {
			t.Fatalf("captured subprocess output leaked to stdout %q", stdout)
		}
	})

	t.Run("interruption between attempts has no service suffix", func(t *testing.T) {
		results := []backup.FileResult{{Service: "alpha", Err: errors.New("archive unavailable")}}
		var stdout, stderr bytes.Buffer
		allOK, err := writeBackupResults(&stdout, results)
		if err != nil || allOK {
			t.Fatalf("write failed result = allOK %v, error %v", allOK, err)
		}
		writeBackupOperationalError(&stderr, results, context.Canceled)
		if got, want := stdout.String(), "alpha: failed: archive unavailable\n"; got != want {
			t.Fatalf("between-attempt report = %q, want %q", got, want)
		}
		if got, want := stderr.String(), "opsctl: backup failed\n"; got != want {
			t.Fatalf("between-attempt diagnostic = %q, want %q", got, want)
		}
	})
}

func TestBackupGrammarRejectsOptionsAndExcessOperandsBeforeHostAccess(t *testing.T) {
	// R-DHOK-RQ3B
	tests := [][]string{
		{"backup", "--unknown"},
		{"backup", "-V"},
		{"backup", "alpha", "beta"},
		{"backup", "alpha", "--help"},
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
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "opsctl: ") || !strings.HasSuffix(stderr, "\n\nsee 'opsctl backup --help' for usage\n") {
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

func TestBackupHelpIsExactAndHostIndependent(t *testing.T) {
	// R-DIWH-5HU0
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
			stdout, stderr, code := invokeBackupCLI([]string{"backup", option}, deps)
			if code != 0 || stdout != wantBackupUsage || stderr != "" {
				t.Errorf("backup %s as euid %d = exit %d stdout %q stderr %q", option, euid, code, stdout, stderr)
			}
			if used {
				t.Errorf("backup %s as euid %d accessed a process or cloud boundary", option, euid)
			}
			info, err := os.Lstat(root)
			if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len("host state")) {
				t.Errorf("backup %s changed host state: info %v error %v", option, info, err)
			}
		}
	}
}

func configuredBackupRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	store := config.Store{Root: root}
	for key, value := range map[string]string{
		"aws.region": "us-east-2", "backup.s3_uri": "s3://backups.example/host/",
	} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeBackupServiceFile(t *testing.T, root, service, content string) {
	t.Helper()
	filename := filepath.Join(root, "opt", service, "state", "value")
	if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func backupCLIDeps(root string, client *backupCLICloud, execute func(context.Context, host.Command) (host.Result, error)) Deps {
	return Deps{
		Root: root, EUID: 0, Execute: execute,
		Now: func() time.Time { return time.Date(2026, 9, 16, 12, 34, 56, 0, time.UTC) },
		Cloud: cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
			if region != "us-east-2" {
				return nil, errors.New("wrong region: " + region)
			}
			return client, nil
		}},
	}
}

func successfulBackupExecute(_ context.Context, command host.Command) (host.Result, error) {
	switch command.Name {
	case "getent":
		return host.Result{ExitCode: 2}, nil
	case "zstd":
		compressed := make([]byte, 1530921)
		copy(compressed, []byte{0x28, 0xb5, 0x2f, 0xfd})
		return host.Result{Stdout: compressed}, nil
	default:
		return host.Result{}, errors.New("unexpected command: " + command.Name)
	}
}

func invokeBackupCLI(args []string, deps Deps) (string, string, int) {
	var stdout, stderr bytes.Buffer
	code := Run(args, nil, &stdout, &stderr, deps)
	return stdout.String(), stderr.String(), code
}

type backupCLICloud struct {
	puts        []string
	failService string
}

func (*backupCLICloud) GetObject(context.Context, string) (io.ReadCloser, error) {
	panic("unexpected GetObject")
}

func (client *backupCLICloud) PutObject(_ context.Context, uri string, body io.Reader) error {
	if _, err := io.Copy(io.Discard, body); err != nil {
		return err
	}
	client.puts = append(client.puts, uri)
	if client.failService != "" && strings.Contains(uri, "/"+client.failService+"/") {
		return errors.New("upload unavailable")
	}
	return nil
}

func (*backupCLICloud) ListObjects(context.Context, string) ([]cloud.Object, error) {
	panic("unexpected ListObjects")
}

func (*backupCLICloud) ReadSecrets(context.Context, string) (map[string]string, error) {
	panic("unexpected ReadSecrets")
}
