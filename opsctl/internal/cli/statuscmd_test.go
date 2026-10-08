package cli_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestStatusPrintsExactRowsAndTreatsFindingsAsSuccess(t *testing.T) {
	// R-F2SH-MZ8O
	root := t.TempDir()
	for _, name := range []string{"zeta", "alpha"} {
		if err := os.MkdirAll(filepath.Join(root, "opt", name, "etc"), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	manifest := []byte("app = \"alpha\"\n[database]\nengine = \"sqlite\"\npath = \"state/alpha.db\"\n")
	if err := os.WriteFile(filepath.Join(root, "opt/alpha/etc/manifest.toml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	database := make([]byte, 4096)
	copy(database, "SQLite format 3\x00")
	binary.BigEndian.PutUint16(database[16:18], 4096)
	database[18], database[19] = 1, 1 // Persistent rollback journal mode.
	database[21], database[22], database[23] = 64, 32, 32
	binary.BigEndian.PutUint32(database[44:48], 4)
	binary.BigEndian.PutUint32(database[56:60], 1)
	if err := os.MkdirAll(filepath.Join(root, "var/opt/ikigenba/alpha/state"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "var/opt/ikigenba/alpha/state/alpha.db"), database, 0o600); err != nil {
		t.Fatal(err)
	}
	execute := func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name == "systemctl" {
			state := "failed"
			if command.Args[len(command.Args)-1] == "ikigenba-alpha.service" {
				state = "active"
			}
			if filepath.Ext(command.Args[len(command.Args)-1]) == ".socket" {
				return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n")}, nil
			}
			return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=" + state + "\n")}, nil
		}
		if filepath.Base(command.Name) == "alpha" {
			return host.Result{Stdout: []byte("v1\n")}, nil
		}
		return host.Result{ExitCode: 1}, nil
	}
	stdout, stderr, code := invoke([]string{"status"}, cli.Deps{Root: root, EUID: 0, Execute: execute})
	if code != 0 || stdout != "alpha v1 - active active delete\nzeta - - failed active -\n" || stderr != "" {
		t.Fatalf("status = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestStatusEnumerationAndWriteFailuresAreDiagnostics(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing")
	stdout, stderr, code := invoke([]string{"status"}, cli.Deps{Root: missingRoot, EUID: 0})
	if code != 1 || stdout != "" || stderr != "opsctl: open "+missingRoot+": no such file or directory\n" {
		t.Fatalf("enumeration failure = exit %d stdout %q stderr %q", code, stdout, stderr)
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "opt"), 0o750); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	code = cli.Run([]string{"status"}, nil, statusFailWriter{}, &diagnostic, cli.Deps{Root: root, EUID: 0})
	if code != 1 || diagnostic.String() != "opsctl: status failed: write report: status output unavailable\n" {
		t.Fatalf("write failure = exit %d stderr %q", code, diagnostic.String())
	}
}

type statusFailWriter struct{}

func (statusFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("status output unavailable")
}

func TestReleasedStatusPrintsCommitLabelAndDataOnlyRows(t *testing.T) {
	// R-F2SH-MZ8O
	for _, label := range []string{"", "candidate/one"} {
		t.Run(label, func(t *testing.T) {
			root, sha := cliReleasedRoot(t, label)
			if err := os.MkdirAll(filepath.Join(root, "var/opt/ikigenba/orphan/state"), 0750); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := invoke([]string{"status"}, cli.Deps{Root: root, EUID: 0, Execute: func(_ context.Context, cmd host.Command) (host.Result, error) {
				if cmd.Name != "systemctl" {
					t.Fatalf("release status queried app binary: %#v", cmd)
				}
				state := "failed"
				if filepath.Ext(cmd.Args[len(cmd.Args)-1]) == ".socket" {
					state = "active"
				}
				return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=" + state + "\nUnitFileState=enabled\n")}, nil
			}})
			wantLabel := label
			if label == "" {
				wantLabel = "-"
			}
			want := "notes " + sha[:7] + " " + wantLabel + " failed active -\norphan - - - - -\n"
			if code != 0 || stdout != want || stderr != "" {
				t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
			}
		})
	}
}

func TestStatusReportsInvalidCurrentDirectly(t *testing.T) {
	// R-F2SH-MZ8O
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "opt/ikigenba/current"), 0700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := invoke([]string{"status"}, cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("status executed command before current refusal")
		return host.Result{}, nil
	}})
	if code != 1 || stdout != "" || stderr != "opsctl: /opt/ikigenba/current does not name a release\n" {
		t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func cliReleasedRoot(t *testing.T, label string) (string, string) {
	t.Helper()
	root := t.TempDir()
	sha := "1234567890abcdef1234567890abcdef12345678"
	dir := "opt/ikigenba/releases/" + sha
	writeUninstallFile(t, root, dir+"/release.json", `{"sha":"`+sha+`"}`)
	writeUninstallFile(t, root, dir+"/notes/etc/manifest.toml", "app = \"notes\"\n")
	writeUninstallFile(t, root, dir+"/notes/bin/notes", "release binary")
	if label != "" {
		writeUninstallFile(t, root, dir+"/label", label+"\n")
	}
	if err := os.Symlink("releases/"+sha, filepath.Join(root, "opt/ikigenba/current")); err != nil {
		t.Fatal(err)
	}
	return root, sha
}
