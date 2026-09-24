package main

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/dummy/internal/cli"
)

// R-LGSH-HS2Z R-AOZE-83CM
func TestMainWiring(t *testing.T) {
	root := mainProjectRoot(t)
	binary := filepath.Join(t.TempDir(), "dummy")
	build := exec.Command("go")
	build.Args = []string{"go", "build", "-o", binary, "./cmd/dummy"}
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build dummy: %v\n%s", err, output)
	}

	for _, test := range []struct {
		name   string
		args   []string
		exit   int
		stdout string
		stderr string
	}{
		{name: "version", args: []string{"--version"}, exit: cli.ExitSuccess, stdout: cli.Version + "\n"},
		{name: "invalid command", args: []string{"bogus"}, exit: cli.ExitUsage, stderr: "dummy: unknown command 'bogus'\n\nsee 'dummy --help' for usage\n"},
		{name: "bare without socket", exit: cli.ExitUsage, stderr: "dummy: no socket was passed in\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3000 dummy'\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, exit := runBinary(t, binary, test.args)
			if exit != test.exit || stdout != test.stdout || stderr != test.stderr {
				t.Errorf("exit=%d stdout=%q stderr=%q; want exit=%d stdout=%q stderr=%q", exit, stdout, stderr, test.exit, test.stdout, test.stderr)
			}
		})
	}

	for _, sig := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		t.Run(sig.String(), func(t *testing.T) {
			serveAndSignal(t, binary, sig)
		})
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

func runBinary(t *testing.T, binary string, args []string) (string, string, int) {
	t.Helper()
	commandArgs := append([]string{binary}, args...)
	command := &exec.Cmd{Path: binary, Args: commandArgs}
	command.Env = []string{}
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

// R-M7M9-WQE9 R-MJT9-QFT7
func serveAndSignal(t *testing.T, binary string, sig os.Signal) {
	t.Helper()
	directory, err := os.MkdirTemp("", "dummy-exec-")
	if err != nil {
		t.Fatalf("create short socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	socketPath := filepath.Join(directory, "serve.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close parent listener: %v", err)
		}
	}()
	listener.SetUnlinkOnClose(false)
	passedFile, err := listener.File()
	if err != nil {
		t.Fatalf("duplicate socket for child: %v", err)
	}
	defer func() {
		if err := passedFile.Close(); err != nil {
			t.Errorf("close passed socket file: %v", err)
		}
	}()
	decoyFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open non-listener for descriptor 4: %v", err)
	}
	defer func() {
		if err := decoyFile.Close(); err != nil {
			t.Errorf("close descriptor 4: %v", err)
		}
	}()

	notifyPath := filepath.Join(directory, "notify.sock")
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listen for readiness: %v", err)
	}
	defer func() {
		if err := notify.Close(); err != nil {
			t.Errorf("close readiness listener: %v", err)
		}
	}()
	if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set readiness deadline: %v", err)
	}

	command := exec.Command("/bin/sh")
	command.Args = []string{"/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`, binary}
	command.Env = []string{"NOTIFY_SOCKET=" + notifyPath}
	// Only descriptor 3 is a listener. Readiness proves the child took it.
	command.ExtraFiles = []*os.File{passedFile, decoyFile}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start serve case: %v", err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()

	var datagram [32]byte
	n, _, err := notify.ReadFromUnix(datagram[:])
	if err != nil {
		t.Fatalf("wait for readiness: %v", err)
	}
	if got := string(datagram[:n]); got != "READY=1" {
		t.Fatalf("readiness = %q, want READY=1", got)
	}
	if err := command.Process.Signal(sig); err != nil {
		t.Fatalf("send %v: %v", sig, err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case err = <-waited:
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		err = <-waited
		finished = true
		t.Fatalf("serve case for %v did not exit after signal: %v", sig, err)
	}
	finished = true
	if err != nil {
		t.Errorf("serve case for %v exited with error: %v", sig, err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("serve case for %v: stdout=%q stderr=%q", sig, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Errorf("socket path after child exit: %v", err)
	}
	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Errorf("socket no longer accepts queued connections: %v", err)
	} else if err := connection.Close(); err != nil {
		t.Errorf("close queued connection: %v", err)
	}
}
