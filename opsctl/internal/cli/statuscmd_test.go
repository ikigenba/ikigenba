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
	// R-VQJT-HDCQ
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
	if err := os.MkdirAll(filepath.Join(root, "opt/alpha/state"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opt/alpha/state/alpha.db"), database, 0o600); err != nil {
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
	if code != 0 || stdout != "alpha v1 active active delete\nzeta - failed active -\n" || stderr != "" {
		t.Fatalf("status = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestStatusEnumerationAndWriteFailuresAreDiagnostics(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing")
	stdout, stderr, code := invoke([]string{"status"}, cli.Deps{Root: missingRoot, EUID: 0})
	if code != 1 || stdout != "" || stderr != "opsctl: status report not produced: service enumeration did not complete: status failed\n" {
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
