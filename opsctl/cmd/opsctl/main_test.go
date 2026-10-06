package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
)

func TestBinaryHelp(t *testing.T) {
	// R-N0T5-G71B
	// R-1DS3-EDLH
	stdout, stderr, code := runBinary(t, buildOpsctl(t), exec.Command("./opsctl", "--help"))
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
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
  snapshot  copy a service's files and database to S3 as one tarball
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
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestBinaryPassesArgumentsStreamsAndExitCode builds the opsctl binary and
// runs it, observing that arguments reach cli.Run, its output reaches the
// process's standard streams, and its return value is the exit code.
func TestBinaryPassesArgumentsStreamsAndExitCode(t *testing.T) {
	// R-F8UL-1VTD
	dir := buildOpsctl(t)

	t.Run("unknown command", func(t *testing.T) {
		stdout, stderr, code := runBinary(t, dir, exec.Command("./opsctl", "nosuchcommand"))
		if code != 2 {
			t.Errorf("exit code = %d, want 2", code)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty", stdout)
		}
		want := "opsctl: unknown command 'nosuchcommand'\n\nsee 'opsctl --help' for usage\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("version", func(t *testing.T) {
		var wantOut, wantErr bytes.Buffer
		if code := cli.Run([]string{"version"}, strings.NewReader(""), &wantOut, &wantErr, cli.Deps{}); code != 0 || wantErr.Len() != 0 {
			t.Fatalf("cli.Run version = %d, stderr %q; want 0 and empty", code, wantErr.String())
		}
		if strings.Count(wantOut.String(), "\n") != 1 || !strings.HasSuffix(wantOut.String(), "\n") {
			t.Fatalf("cli.Run version stdout = %q, want one line", wantOut.String())
		}
		stdout, stderr, code := runBinary(t, dir, exec.Command("./opsctl", "version"))
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
		if stdout != wantOut.String() {
			t.Errorf("stdout = %q, want %q", stdout, wantOut.String())
		}
	})
}

// buildOpsctl installs this package's binary, named opsctl, into a fresh
// temporary directory and returns that directory.
func buildOpsctl(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	build := exec.Command("go", "install", ".")
	build.Env = append(os.Environ(), "GOBIN="+dir)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go install ./cmd/opsctl: %v\n%s", err, out)
	}
	return dir
}

// runBinary runs cmd, whose path is relative to dir, with an empty
// environment and empty stdin, returning its stdout, stderr, and exit code.
func runBinary(t *testing.T, dir string, cmd *exec.Cmd) (string, string, int) {
	t.Helper()
	cmd.Dir = dir
	cmd.Env = []string{}
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("%v: %v", cmd.Args, err)
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}
