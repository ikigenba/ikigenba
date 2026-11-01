package main_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

func TestBinaryHelp(t *testing.T) {
	// R-KOEN-P405
	if os.Geteuid() == 0 {
		t.Fatal("the binary wiring test requires an ordinary user")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "turns")
	build, err := seam.Exec(context.Background(), seam.Cmd{
		Path: "go", Args: []string{"build", "-o", binary, "."},
		Env: []string{"GOPROXY=off", "GOCACHE=" + filepath.Join(dir, "go-cache"), "TMPDIR=" + dir},
	})
	if err != nil || build.ExitCode != 0 {
		t.Fatalf("build: error %v, exit %d, stdout %s, stderr %s", err, build.ExitCode, build.Stdout, build.Stderr)
	}
	result, err := seam.Exec(context.Background(), seam.Cmd{
		Path: binary, Args: []string{"--help"}, Dir: dir,
		Env: []string{"HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "XDG_STATE_HOME=" + dir},
	})
	const want = `Usage: turns [options] <command> [arguments]

Push this account's agent session turns to an ikigenba space. Never run as root.

Commands:
  configure store the space and token turns pushes with
  install   install the user timer that pushes every five minutes
  push      push new turns to the space
  status    show what is here and what the space holds
  version   print the version
  help      print this help

Options:
  -h, --help     print this help
  --version      print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error
  3  refused: turns must not run as root
  4  refused: not configured

Run 'turns <command> --help' for details on a command.
`
	if err != nil || result.ExitCode != 0 || len(result.Stderr) != 0 || string(result.Stdout) != want {
		t.Fatalf("help: error %v, exit %d, stdout %q, stderr %q", err, result.ExitCode, result.Stdout, result.Stderr)
	}
}
