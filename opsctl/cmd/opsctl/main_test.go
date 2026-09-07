package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestBinaryHelp(t *testing.T) {
	// R-N0T5-G71B
	t.Cleanup(func() { _ = os.Remove("opsctl.help.test") })
	build := exec.Command("go", "build", "-o", "opsctl.help.test", ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/opsctl: %v\n%s", err, out)
	}

	cmd := exec.Command("./opsctl.help.test", "--help")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("opsctl --help: %v", err)
		}
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	want := `Usage: opsctl [options] <command> [arguments]

Operate the ikigenba platform host. Must run as root.

Commands:
  config    read and write the host configuration store
  version   print the version

Options:
  -h, --help     print this help
  -V, --version  print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: opsctl must run as root

Run 'opsctl <command> --help' for details on a command.
`
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}
