package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestStatusPrintsExactRowsAndTreatsFindingsAsSuccess(t *testing.T) {
	// R-MJ50-BUA0
	root := t.TempDir()
	for _, name := range []string{"zeta", "alpha"} {
		if err := os.MkdirAll(filepath.Join(root, "opt", name, "etc"), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	execute := func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name == "systemctl" {
			state := "failed"
			if command.Args[len(command.Args)-1] == "ikigenba-alpha.service" {
				state = "active"
			}
			return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=" + state + "\n")}, nil
		}
		if filepath.Base(command.Name) == "alpha" {
			return host.Result{Stdout: []byte("v1\n")}, nil
		}
		return host.Result{ExitCode: 1}, nil
	}
	stdout, stderr, code := invoke([]string{"status"}, cli.Deps{Root: root, EUID: 0, Execute: execute})
	if code != 0 || stdout != "alpha v1 active -\nzeta - failed -\n" || stderr != "" {
		t.Fatalf("status = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestStatusEnumerationAndWriteFailuresAreDiagnostics(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing")
	stdout, stderr, code := invoke([]string{"status"}, cli.Deps{Root: missingRoot, EUID: 0})
	if code != 1 || stdout != "" || stderr != "opsctl: status failed\n" {
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
