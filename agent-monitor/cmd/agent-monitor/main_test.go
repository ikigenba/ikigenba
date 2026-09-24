package main_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-monitor")
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find go: %v", err)
	}
	cmd := &exec.Cmd{Path: goPath, Args: []string{"go", "build", "-o", path, "./cmd/agent-monitor"}}
	cmd.Dir = "../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v: %s", err, output)
	}
	return path
}

func TestMainWiring(t *testing.T) {
	// The built binary forwards arguments and real streams, then exits with Run's code.
	path := buildBinary(t)
	cmd := &exec.Cmd{Path: path, Args: []string{path, "mystery"}, Env: childEnvironment(nil)}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("exit = %v, want 2", err)
	}
	if stdout.Len() != 0 || stderr.String() != "agent-monitor: unknown command 'mystery'\n\nsee 'agent-monitor --help' for usage\n" {
		t.Fatalf("streams: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

}

func TestBareBinary(t *testing.T) {
	// R-DW3L-I1WZ: A bare built binary prints the declared usage and succeeds.
	path := buildBinary(t)
	cmd := &exec.Cmd{Path: path, Args: []string{path}, Env: childEnvironment(nil)}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bare exit: %v", err)
	}
	if stdout.String() != cli.Usage || stderr.Len() != 0 {
		t.Fatalf("streams: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func childEnvironment(home *string) []string {
	env := make([]string, 0, 1)
	if home != nil {
		env = append(env, "HOME="+*home)
	}
	return env
}

func TestListWithEmptyHome(t *testing.T) {
	// R-DXBH-VTNO: each harness lists an empty home without creating files.
	path := buildBinary(t)
	for _, harness := range []string{"claude", "codex", "grok"} {
		t.Run(harness, func(t *testing.T) {
			home := t.TempDir()
			cmd := &exec.Cmd{Path: path, Args: []string{path, "list", harness}, Env: childEnvironment(&home)}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("list exit: %v; stderr=%q", err, stderr.String())
			}
			if stdout.String() != session.Table(nil) || stderr.Len() != 0 {
				t.Fatalf("streams: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatalf("home changed: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestListWithoutHome(t *testing.T) {
	// R-DYJE-9LED: absent and empty HOME reach the same real-process diagnostic.
	path := buildBinary(t)
	empty := ""
	for _, tc := range []struct {
		name string
		home *string
	}{{"absent", nil}, {"empty", &empty}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &exec.Cmd{Path: path, Args: []string{path, "list", "claude"}, Env: childEnvironment(tc.home)}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			var exitErr *exec.ExitError
			if !asExitError(err, &exitErr) || exitErr.ExitCode() != 3 {
				t.Fatalf("exit = %v, want 3", err)
			}
			if stdout.Len() != 0 || stderr.String() != "agent-monitor: cannot find the home directory: HOME is not set\n" {
				t.Fatalf("streams: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestFullDeviceWriteFailure(t *testing.T) {
	// R-2LK1-LM9L: A real stdout write failure reaches stderr and the process exit.
	output, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := output.Close(); err != nil {
			t.Error(err)
		}
	})

	path := buildBinary(t)
	cmd := &exec.Cmd{Path: path, Args: []string{path}}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = output, &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("exit = %v, want 1", err)
	}
	line := stderr.String()
	if !strings.HasPrefix(line, "agent-monitor: write error: ") || !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("write failure diagnostic = %q", line)
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	ok := errors.As(err, &exitErr)
	if ok {
		*target = exitErr
	}
	return ok
}
