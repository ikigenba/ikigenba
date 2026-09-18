package main

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/dummy/internal/cli"
)

// R-DM3V-Y883
func TestMainWiring(t *testing.T) {
	root := mainProjectRoot(t)
	binary := filepath.Join(t.TempDir(), "dummy")
	build := exec.Command("go")
	build.Args = []string{"go", "build", "-o", binary, "./cmd/dummy"}
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build dummy: %v\n%s", err, output)
	}

	stdout, stderr, exit := runBinary(t, binary, []string{"--version"}, nil)
	if exit != cli.ExitSuccess || stdout != cli.Version+"\n" || stderr != "" {
		t.Errorf("--version: exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}

	stdout, stderr, exit = runBinary(t, binary, []string{"bogus"}, nil)
	if exit != cli.ExitUsage || stdout != "" || stderr == "" {
		t.Errorf("bogus: exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}

	for _, signal := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		port := freePort(t)
		command := binaryCommand(binary, nil)
		command.Env = []string{"PORT=" + port}
		var commandStdout, commandStderr bytes.Buffer
		command.Stdout = &commandStdout
		command.Stderr = &commandStderr
		if err := command.Start(); err != nil {
			t.Fatalf("start serve case for %v: %v", signal, err)
		}
		waitUntilListening(t, port)
		if err := command.Process.Signal(signal); err != nil {
			t.Fatalf("send %v: %v", signal, err)
		}
		if err := command.Wait(); err != nil {
			t.Errorf("serve case for %v exited with error: %v", signal, err)
		}
		if commandStdout.Len() != 0 || commandStderr.Len() != 0 {
			t.Errorf("serve case for %v: stdout=%q stderr=%q", signal, commandStdout.String(), commandStderr.String())
		}
	}
}

func mainProjectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func runBinary(t *testing.T, binary string, args, environment []string) (string, string, int) {
	t.Helper()
	command := binaryCommand(binary, args)
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("run dummy: %v", err)
	}
	return stdout.String(), stderr.String(), exitError.ExitCode()
}

func binaryCommand(binary string, args []string) *exec.Cmd {
	commandArgs := make([]string, 1, len(args)+1)
	commandArgs[0] = binary
	commandArgs = append(commandArgs, args...)
	return &exec.Cmd{Path: binary, Args: commandArgs}
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe free port: %v", err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if err = listener.Close(); err != nil {
		t.Fatalf("close port probe: %v", err)
	}
	return port
}

func waitUntilListening(t *testing.T, port string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	address := net.JoinHostPort("127.0.0.1", port)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			if closeErr := connection.Close(); closeErr != nil {
				t.Errorf("close readiness connection: %v", closeErr)
			}
			return
		}
	}
	t.Fatalf("dummy did not listen on %s", address)
}
