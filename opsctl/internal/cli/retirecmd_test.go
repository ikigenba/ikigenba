package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantRetireUsage = `Usage: opsctl retire

Take a host's final backup before it is discarded. Stop every app's socket and
service, sockets first so no request starts a service again, then
litestream.service so it ships every committed change it holds, then copy
every service's files and the host's own configuration to backup.s3_uri
exactly as 'opsctl backup' and 'opsctl host backup' would.

Nothing is deleted or disabled. The units are left stopped; on a host kept
after all, a reboot or 'opsctl restart APP' brings every enabled app back, and
a disabled app stays down until 'opsctl enable'.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
`

func TestRetireHelpIsExactAndHostIndependent(t *testing.T) {
	// R-XD2Z-UCY2
	for _, euid := range []int{0, 1000} {
		for _, option := range []string{"--help", "-h"} {
			deps, assertInert := inertHostCommandDeps(t, euid)
			assertRootUnread := observeRetireRootAccess(t, deps.Root)
			stdout, stderr, code := invokeBackupCLI([]string{"retire", option}, deps)
			assertRootUnread()
			assertInert()
			if code != 0 || stdout != wantRetireUsage || stderr != "" {
				t.Errorf("retire %s as euid %d = exit %d stdout %q stderr %q", option, euid, code, stdout, stderr)
			}
		}
	}
}

func TestRetireGrammarAndRootRefusalPrecedeHostAccess(t *testing.T) {
	// R-YRBV-O7VR
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"retire", "--force"}, "opsctl: unknown option '--force'\n\nsee 'opsctl retire --help' for usage\n"},
		{[]string{"retire", "-V"}, "opsctl: unknown option '-V'\n\nsee 'opsctl retire --help' for usage\n"},
		{[]string{"retire", "extra"}, "opsctl: retire takes no arguments\n\nsee 'opsctl retire --help' for usage\n"},
		{[]string{"retire", "extra", "more"}, "opsctl: retire takes no arguments\n\nsee 'opsctl retire --help' for usage\n"},
	} {
		deps, assertInert := inertHostCommandDeps(t, 1000)
		stdout, stderr, code := invokeBackupCLI(test.args, deps)
		assertInert()
		if code != 2 || stdout != "" || stderr != test.want {
			t.Errorf("%q = exit %d stdout %q stderr %q", test.args, code, stdout, stderr)
		}
	}

	deps, assertInert := inertHostCommandDeps(t, 1000)
	stdout, stderr, code := invokeBackupCLI([]string{"retire"}, deps)
	assertInert()
	if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
		t.Errorf("nonroot retire = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestRetireRendersSynchronizedSuccessAndArchiveFindings(t *testing.T) {
	// R-XBV3-GL7D
	result := backup.RetireResult{
		Services:          []string{"alpha", "beta", "crm", "dashboard", "epsilon", "zeta"},
		Disabled:          []string{"crm", "zeta"},
		ServicesStopped:   true,
		LitestreamStopped: true,
		SyncedDatabases:   []string{"app.db", "main.db"},
		Files: []backup.FileResult{
			{Service: "alpha", Object: "stamp.tar.zst", Size: 1024 * 1024},
			{Service: "beta", Object: "stamp.tar.zst", Size: 1024 * 1024},
			{Service: "crm", Object: "stamp.tar.zst", Size: 1024 * 1024},
			{Service: "dashboard", Object: "stamp.tar.zst", Size: 1024 * 1024},
			{Service: "epsilon", Object: "stamp.tar.zst", Size: 1024 * 1024},
			{Service: "zeta", Err: errors.New("upload rejected")},
		},
		Host: backup.FileResult{Service: "host", Object: "stamp.tar.zst", Size: 1536},
	}
	var stdout, stderr bytes.Buffer
	code := renderRetireOutcome(&stdout, &stderr, result, nil)
	want := "services: ok (alpha, beta stopped; crm already inactive, disabled; dashboard, epsilon stopped; zeta already inactive, disabled)\n" +
		"litestream: ok (stopped, app.db, main.db synced)\n" +
		"alpha: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"beta: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"crm: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"dashboard: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"epsilon: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"zeta: failed: upload rejected\n" +
		"host: ok (stamp.tar.zst, 1.5 KiB)\n"
	if code != exitFail || stdout.String() != want || stderr.String() != "" {
		t.Fatalf("renderRetireOutcome = exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}

	result.Files[5] = backup.FileResult{Service: "zeta", Object: "stamp.tar.zst", Size: 2 * 1024 * 1024}
	stdout.Reset()
	code = renderRetireOutcome(&stdout, &stderr, result, nil)
	want = "services: ok (alpha, beta stopped; crm already inactive, disabled; dashboard, epsilon stopped; zeta already inactive, disabled)\n" +
		"litestream: ok (stopped, app.db, main.db synced)\n" +
		"alpha: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"beta: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"crm: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"dashboard: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"epsilon: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"zeta: ok (stamp.tar.zst, 2.0 MiB)\n" +
		"host: ok (stamp.tar.zst, 1.5 KiB)\n"
	if code != exitOK || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("successful render = exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRetireServiceReportKeepsAdjacentDisabledAppsSeparate(t *testing.T) {
	// R-XBV3-GL7D
	result := backup.RetireResult{
		Services:          []string{"alpha", "beta", "gamma", "delta"},
		Disabled:          []string{"alpha", "beta"},
		ServicesStopped:   true,
		LitestreamStopped: true,
		Files: []backup.FileResult{
			{Service: "alpha", Object: "stamp.tar.zst", Size: 1024 * 1024},
			{Service: "beta", Object: "stamp.tar.zst", Size: 1024 * 1024},
			{Service: "gamma", Object: "stamp.tar.zst", Size: 1024 * 1024},
			{Service: "delta", Object: "stamp.tar.zst", Size: 1024 * 1024},
		},
		Host: backup.FileResult{Service: "host", Object: "stamp.tar.zst", Size: 1024},
	}
	var stdout, stderr bytes.Buffer
	code := renderRetireOutcome(&stdout, &stderr, result, nil)
	want := "services: ok (alpha already inactive, disabled; beta already inactive, disabled; gamma, delta stopped)\n" +
		"litestream: ok (stopped)\n" +
		"alpha: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"beta: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"delta: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"gamma: ok (stamp.tar.zst, 1.0 MiB)\n" +
		"host: ok (stamp.tar.zst, 1.0 KiB)\n"
	if code != exitOK || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("adjacent disabled services = exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRetireOperationalErrorsRetainOnlyCompletedReports(t *testing.T) {
	// R-HZPA-6PHZ
	t.Run("sync failure remains primary and both command captures render", func(t *testing.T) {
		// R-YUZK-TJ3U
		root := configuredBackupRoot(t)
		manifest := filepath.Join(root, "opt/alpha/etc/manifest.toml")
		if err := os.MkdirAll(filepath.Dir(manifest), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifest, []byte("app = \"alpha\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		execute := func(_ context.Context, command host.Command) (host.Result, error) {
			text := strings.Join(append([]string{command.Name}, command.Args...), " ")
			switch {
			case text == "systemctl show --property=LoadState --property=ActiveState ikigenba-alpha.socket",
				text == "systemctl show --property=LoadState --property=ActiveState ikigenba-alpha.service":
				return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\n")}, nil
			case text == "systemctl show --property=LoadState --property=UnitFileState ikigenba-alpha.socket":
				return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=disabled\n")}, nil
			case command.Name == "litestream":
				return host.Result{ExitCode: 9, Stdout: []byte("sync partial\n"), Stderr: []byte("sync rejected\n")}, nil
			case text == "systemctl stop litestream.service":
				return host.Result{ExitCode: 5, Stdout: []byte("stop partial\n"), Stderr: []byte("stop rejected\n")}, nil
			default:
				return host.Result{}, fmt.Errorf("unexpected command %q", text)
			}
		}
		stdout, stderr, code := invokeBackupCLI([]string{"retire"}, hostCLIDeps(root, newHostCLICloud(), execute))
		database := filepath.Join(root, "opt/alpha/state/app.db")
		wantOut := "services: ok (none)\n" +
			"litestream: failed: sync " + database + ": exit status 9\n"
		wantErr := "opsctl: retire failed at litestream\n\n" +
			"sync " + database + ": exit status 9\n" +
			"> sync partial\n> sync rejected\n" +
			"stop litestream.service: exit status 5\n" +
			"> stop partial\n> stop rejected\n"
		if code != int(exitFail) || stdout != wantOut || stderr != wantErr {
			t.Fatalf("sync plus stop failure = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})

	t.Run("failed phase and subprocess detail", func(t *testing.T) {
		root := configuredBackupRoot(t)
		writeBackupServiceFile(t, root, "alpha", "state")
		client := newHostCLICloud()
		serviceStopped := false
		execute := func(_ context.Context, command host.Command) (host.Result, error) {
			text := strings.Join(append([]string{command.Name}, command.Args...), " ")
			switch text {
			case "systemctl show --property=LoadState --property=ActiveState ikigenba-alpha.socket":
				return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\n")}, nil
			case "systemctl show --property=LoadState --property=ActiveState ikigenba-alpha.service":
				if serviceStopped {
					return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=inactive\n")}, nil
				}
				return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=active\n")}, nil
			case "systemctl show --property=LoadState --property=UnitFileState ikigenba-alpha.socket":
				return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=disabled\n")}, nil
			case "systemctl stop ikigenba-alpha.service":
				serviceStopped = true
				return host.Result{}, nil
			case "systemctl stop litestream.service":
				return host.Result{ExitCode: 1, Stdout: []byte("partial\n"), Stderr: []byte("bus failed")}, nil
			default:
				return host.Result{}, fmt.Errorf("unexpected command %q", text)
			}
		}
		stdout, stderr, code := invokeBackupCLI([]string{"retire"}, hostCLIDeps(root, client, execute))
		wantOut := "services: ok (alpha stopped)\nlitestream: failed: stop litestream.service: exit status 1\n"
		wantErr := "opsctl: retire failed at litestream\n\n> partial\n> bus failed\n"
		if code != int(exitFail) || stdout != wantOut || stderr != wantErr {
			t.Fatalf("phase failure = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})

	t.Run("failed archive row is not duplicated", func(t *testing.T) {
		root := configuredBackupRoot(t)
		writeBackupServiceFile(t, root, "alpha", "state")
		client := newHostCLICloud()
		serviceStopped := false
		execute := func(_ context.Context, command host.Command) (host.Result, error) {
			text := strings.Join(append([]string{command.Name}, command.Args...), " ")
			switch {
			case text == "systemctl show --property=LoadState --property=ActiveState ikigenba-alpha.socket":
				return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\n")}, nil
			case text == "systemctl show --property=LoadState --property=ActiveState ikigenba-alpha.service":
				if serviceStopped {
					return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=inactive\n")}, nil
				}
				return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=active\n")}, nil
			case text == "systemctl show --property=LoadState --property=UnitFileState ikigenba-alpha.socket":
				return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=disabled\n")}, nil
			case text == "systemctl stop ikigenba-alpha.service":
				serviceStopped = true
				return host.Result{}, nil
			case text == "systemctl stop litestream.service":
				return host.Result{}, nil
			case text == "systemctl show --property=LoadState --property=ActiveState litestream.service":
				return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=inactive\n")}, nil
			case command.Name == "getent":
				return host.Result{ExitCode: 2}, nil
			case command.Name == "zstd":
				return host.Result{Stderr: []byte("encoder stopped")}, context.Canceled
			default:
				return host.Result{}, fmt.Errorf("unexpected command %q", text)
			}
		}
		stdout, stderr, code := invokeBackupCLI([]string{"retire"}, hostCLIDeps(root, client, execute))
		wantOut := "services: ok (alpha stopped)\nlitestream: ok (stopped)\nalpha: failed: compress \"alpha\" archive: context canceled\n"
		wantErr := "opsctl: retire failed at alpha\n\n> encoder stopped\n"
		if code != int(exitFail) || stdout != wantOut || stderr != wantErr || strings.Count(stdout, "alpha: failed:") != 1 {
			t.Fatalf("archive failure = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})

	t.Run("failed host archive row is retained", func(t *testing.T) {
		root := configuredBackupRoot(t)
		client := newHostCLICloud()
		execute := func(_ context.Context, command host.Command) (host.Result, error) {
			text := strings.Join(append([]string{command.Name}, command.Args...), " ")
			switch {
			case text == "systemctl stop litestream.service":
				return host.Result{}, nil
			case text == "systemctl show --property=LoadState --property=ActiveState litestream.service":
				return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=inactive\n")}, nil
			case command.Name == "zstd":
				return host.Result{Stdout: []byte("partial archive")}, context.Canceled
			default:
				return host.Result{}, fmt.Errorf("unexpected command %q", text)
			}
		}
		stdout, stderr, code := invokeBackupCLI([]string{"retire"}, hostCLIDeps(root, client, execute))
		wantOut := "services: ok (none)\nlitestream: ok (stopped)\nhost: failed: compress \"host\" archive: context canceled\n"
		wantErr := "opsctl: retire failed at host\n\n> partial archive\n"
		if code != int(exitFail) || stdout != wantOut || stderr != wantErr || strings.Count(stdout, "host: failed:") != 1 {
			t.Fatalf("host archive failure = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})

	t.Run("preworkflow failure", func(t *testing.T) {
		stdout, stderr, code := invokeBackupCLI([]string{"retire"}, Deps{Root: t.TempDir(), EUID: 0})
		if code != int(exitFail) || stdout != "" || stderr != "opsctl: backup.s3_uri not set\n" {
			t.Fatalf("preworkflow = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})
}

func observeRetireRootAccess(t *testing.T, root string) func() {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if _, err := syscall.InotifyAddWatch(fd, root, syscall.IN_ACCESS|syscall.IN_OPEN|syscall.IN_MODIFY); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		buffer := make([]byte, syscall.SizeofInotifyEvent*4)
		n, err := syscall.Read(fd, buffer)
		if errors.Is(err, syscall.EAGAIN) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("retire help accessed deps.Root: observed %d filesystem event bytes", n)
		}
	}
}

func TestRetireWithoutServicesArchivesOnlyHostAndRunsNoOtherProject(t *testing.T) {
	// R-Z3IV-HXAP R-Z2AZ-45K0
	root := configuredBackupRoot(t)
	client := newHostCLICloud()
	var commands []string
	execute := func(ctx context.Context, command host.Command) (host.Result, error) {
		text := strings.Join(append([]string{command.Name}, command.Args...), " ")
		commands = append(commands, text)
		switch {
		case reflect.DeepEqual(command.Args, []string{"stop", "litestream.service"}) && command.Name == "systemctl":
			return host.Result{}, nil
		case text == "systemctl show --property=LoadState --property=ActiveState litestream.service":
			return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=inactive\n")}, nil
		case command.Name == "zstd":
			return fixedSizeZstdExecute(1536)(ctx, command)
		default:
			return host.Result{}, fmt.Errorf("unexpected command %q", text)
		}
	}
	stdout, stderr, code := invokeBackupCLI([]string{"retire"}, hostCLIDeps(root, client, execute))
	want := "services: ok (none)\nlitestream: ok (stopped)\nhost: ok (2026-09-16T12:34:56Z.tar.zst, 1.5 KiB)\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Fatalf("retire empty host = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if wantCommands := []string{"systemctl stop litestream.service", "systemctl show --property=LoadState --property=ActiveState litestream.service", "zstd --quiet --stdout"}; !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %v, want %v; retirement must not terminate the machine or run another project", commands, wantCommands)
	}
	if len(client.objects) != 1 {
		t.Fatalf("published objects = %v, want only host archive", reflect.ValueOf(client.objects).MapKeys())
	}
	for uri := range client.objects {
		if filepath.Base(filepath.Dir(uri)) != "host" {
			t.Fatalf("published non-host object %q", uri)
		}
	}
}
