package main

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/dummy/internal/cli"
)

// R-S00S-C6IE R-AOZE-83CM
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
	if sig == syscall.SIGTERM {
		servicesPath := filepath.Join(directory, "services.json")
		if err := os.WriteFile(servicesPath, []byte(`{"services":[{"name":"dummy","url":"/widgets","icon":"","enabled":true}]}`), 0600); err != nil {
			t.Fatal(err)
		}
		command.Env = append(command.Env, "IKIGENBA_SERVICES="+servicesPath)
	}
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
	if sig == syscall.SIGTERM {
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://dummy/widgets", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-User-Id", "test-user")
		req.Header.Set("X-User-Email", "user@example.test")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read child response: %v, close: %v", err, closeErr)
		}
		if !strings.Contains(string(body), `class="launcher"`) {
			t.Error("main did not pass appkit banner source to handler")
		}
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

// R-S00S-C6IE
func TestMainConstructsKitAndPassesProcessState(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(mainProjectRoot(t), "cmd", "dummy", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	var process *ast.CompositeLit
	var newCall *ast.CallExpr
	var kitName, contextName string
	fileSet := token.NewFileSet()
	expression := func(node ast.Node) string {
		var buffer bytes.Buffer
		if err := format.Node(&buffer, fileSet, node); err != nil {
			t.Fatal(err)
		}
		return buffer.String()
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if assignment, ok := node.(*ast.AssignStmt); ok && len(assignment.Rhs) == 1 && len(assignment.Lhs) > 0 {
				if call, ok := assignment.Rhs[0].(*ast.CallExpr); ok {
					switch expression(call.Fun) {
					case "appkit.New":
						kitName = expression(assignment.Lhs[0])
					case "signal.NotifyContext":
						contextName = expression(assignment.Lhs[0])
					}
				}
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch expression(call.Fun) {
			case "appkit.New":
				calls = append(calls, "New")
				newCall = call
			case "cli.Run":
				calls = append(calls, "Run")
				if len(call.Args) != 2 || contextName == "" || expression(call.Args[0]) != contextName {
					t.Error("Run must receive signal context and Process")
					return true
				}
				process, _ = call.Args[1].(*ast.CompositeLit)
			}
			return true
		})
	}
	if strings.Join(calls, ",") != "New,Run" {
		t.Fatalf("kit/run calls = %v", calls)
	}
	if len(newCall.Args) != 1 || expression(newCall.Args[0]) != "panel.ServiceName" {
		t.Error("kit does not use panel.ServiceName")
	}
	if process == nil || expression(process.Type) != "cli.Process" {
		t.Fatal("missing Process literal")
	}
	want := map[string]string{"Args": "os.Args[1:]", "LookupEnv": "os.LookupEnv", "Unsetenv": "os.Unsetenv", "Pid": "os.Getpid()", "Stdout": "os.Stdout", "Stderr": "os.Stderr", "Banner": kitName + ".Banner"}
	for _, field := range process.Elts {
		keyValue, ok := field.(*ast.KeyValueExpr)
		if !ok {
			t.Fatal("Process uses positional fields")
		}
		key := expression(keyValue.Key)
		if key == "Inherit" && expression(keyValue.Value) == "nil" {
			continue
		}
		if value, exists := want[key]; !exists || value != expression(keyValue.Value) {
			t.Errorf("Process.%s = %s", key, expression(keyValue.Value))
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing Process fields: %v", want)
	}
}
