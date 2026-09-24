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
	// R-U2SQ-QM0P
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
  backup    back up a service's files to S3
  cert      obtain and inspect the host's certificate
  config    read and write the host configuration store
  disable   stop an installed app and keep it from starting
  dns       manage DNS records in the zones opsctl owns
  enable    let a disabled app start again, and start it
  host      back up and restore the host's own configuration
  init      run the setup sequence behind one preflight
  install   install an app from a built file
  nginx     generate the platform's nginx configuration
  restart   restart an installed app's service
  restore   restore a service from its backups
  retire    stop every service and take the host's final backup
  status    print every installed app, its version and its state
  uninstall take an app off the host, keeping its data
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
