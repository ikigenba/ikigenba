// Package main tests the devctl entry point by building and running it.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cli"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantTopLevelUsage = `Usage: devctl [options] <command> [arguments]

Manage the platform from the developer's machine. Never run as root.

Commands:
  version   print the version
  space     list, create, destroy, stop, start, and inspect spaces
  secrets   push and list an app's secrets for a space
  build     build the suite at a commit into a release
  deploy    put a release on a space
  rollback  put a space back on the release it ran before
  restore   put a space's app back from its backups
  golden    capture a space's data as a named golden set
  seed      give a space a golden set's or another space's data
  apex      point the root domain at one app on one space

Options:
  --help              print this help
  --version           print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: devctl must not run as root

Run 'devctl <command> --help' for details on a command.
`

func TestBuiltBinaryHelp(t *testing.T) {
	// R-F59F-A9GW
	if os.Geteuid() == 0 {
		t.Fatal("test must run as a non-root user")
	}
	binary := filepath.Join(t.TempDir(), "devctl")
	moduleDir := filepath.Clean(filepath.Join("..", ".."))
	build, err := seam.Exec(t.Context(), seam.Cmd{
		Path: "go", Args: []string{"build", "-o", binary, "./cmd/devctl"}, Dir: moduleDir,
	})
	if err != nil {
		t.Fatalf("start go build: %v", err)
	}
	if build.ExitCode != 0 {
		t.Fatalf("go build exited %d: %s", build.ExitCode, build.Stderr)
	}

	result, err := seam.Exec(t.Context(), seam.Cmd{Path: binary, Args: []string{"--help"}, Dir: moduleDir})
	if err != nil {
		t.Fatalf("start devctl: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	if got := string(result.Stdout); got != wantTopLevelUsage {
		t.Errorf("stdout = %q, want %q", got, wantTopLevelUsage)
	}
	if len(result.Stderr) != 0 {
		t.Errorf("stderr = %q, want empty", result.Stderr)
	}
}

func TestBuiltBinaryVersionMatchesRun(t *testing.T) {
	// R-SRWN-WY8Y
	if os.Geteuid() == 0 {
		t.Fatal("test must run as a non-root user")
	}
	var wantStdout, wantStderr bytes.Buffer
	if code := cli.Run(t.Context(), []string{"--version"}, strings.NewReader(""), &wantStdout, &wantStderr, seam.Deps{EUID: 1}); code != 0 {
		t.Fatalf("cli.Run --version exit code = %d, want 0", code)
	}

	binary := filepath.Join(t.TempDir(), "devctl")
	moduleDir := filepath.Clean(filepath.Join("..", ".."))
	build, err := seam.Exec(t.Context(), seam.Cmd{
		Path: "go", Args: []string{"build", "-o", binary, "./cmd/devctl"}, Dir: moduleDir,
	})
	if err != nil {
		t.Fatalf("start go build: %v", err)
	}
	if build.ExitCode != 0 {
		t.Fatalf("go build exited %d: %s", build.ExitCode, build.Stderr)
	}

	result, err := seam.Exec(t.Context(), seam.Cmd{Path: binary, Args: []string{"--version"}, Dir: moduleDir})
	if err != nil {
		t.Fatalf("start devctl: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	if got := string(result.Stdout); got != wantStdout.String() {
		t.Errorf("stdout = %q, want %q", got, wantStdout.String())
	}
	if len(result.Stderr) != 0 {
		t.Errorf("stderr = %q, want empty", result.Stderr)
	}
}
