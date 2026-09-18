package cli

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// R-ML36-X0ZH
func TestUsageConstant(t *testing.T) {
	t.Parallel()

	const want = "Usage: dummy [command]\n\nServe the Dummy page at 127.0.0.1:$PORT. With no command, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"
	if Usage != want {
		t.Errorf("Usage = %q, want %q", Usage, want)
	}
}

func TestRunCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		want string
	}{
		// R-MMB3-ASQ6
		{name: "version", arg: "--version", want: Version + "\n"},
		// R-MNIZ-OKGV
		{name: "manifest", arg: "manifest", want: Manifest},
		// R-MOQW-2C7K
		{name: "help", arg: "--help", want: Usage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			lookupCalls := 0
			listenCalls := 0
			exit := Run(context.Background(), Process{
				Args: []string{test.arg},
				LookupEnv: func(string) (string, bool) {
					lookupCalls++
					return "3000", true
				},
				Stdout: &stdout,
				Stderr: &stderr,
				Listen: func(string, string) (net.Listener, error) {
					listenCalls++
					return nil, errors.New("unexpected listen")
				},
			})
			if exit != ExitSuccess {
				t.Errorf("Run exit = %d, want ExitSuccess", exit)
			}
			if stdout.String() != test.want {
				t.Errorf("stdout = %q, want %q", stdout.String(), test.want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			if lookupCalls != 0 || listenCalls != 0 {
				t.Errorf("LookupEnv calls = %d, Listen calls = %d; want both zero", lookupCalls, listenCalls)
			}
		})
	}
}

// R-MPYS-G3Y9 R-MR6O-TVOY R-MSEL-7NFN R-MTMH-LF6C
func TestRunRejectsInvalidArgumentsBeforeEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown command", args: []string{"bogus"}, want: "dummy: unknown command 'bogus'\n\nsee 'dummy --help' for usage\n"},
		{name: "unknown option", args: []string{"--bogus"}, want: "dummy: unknown option '--bogus'\n\nsee 'dummy --help' for usage\n"},
		{name: "empty command", args: []string{""}, want: "dummy: unknown command ''\n\nsee 'dummy --help' for usage\n"},
		{name: "surplus after version", args: []string{"--version", "extra"}, want: "dummy: unknown command 'extra'\n\nsee 'dummy --help' for usage\n"},
		{name: "surplus option after manifest", args: []string{"manifest", "-x"}, want: "dummy: unknown option '-x'\n\nsee 'dummy --help' for usage\n"},
		{name: "first unknown wins", args: []string{"bogus", "--later"}, want: "dummy: unknown command 'bogus'\n\nsee 'dummy --help' for usage\n"},
		{name: "second surplus wins", args: []string{"--help", "second", "third"}, want: "dummy: unknown command 'second'\n\nsee 'dummy --help' for usage\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			lookupCalls := 0
			listenCalls := 0
			exit := Run(context.Background(), Process{
				Args: test.args,
				LookupEnv: func(string) (string, bool) {
					lookupCalls++
					return "3000", true
				},
				Stdout: &stdout,
				Stderr: &stderr,
				Listen: func(string, string) (net.Listener, error) {
					listenCalls++
					return nil, errors.New("unexpected listen")
				},
			})
			if exit != ExitUsage {
				t.Errorf("Run exit = %d, want ExitUsage", exit)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if stderr.String() != test.want {
				t.Errorf("stderr = %q, want %q", stderr.String(), test.want)
			}
			if lookupCalls != 0 || listenCalls != 0 {
				t.Errorf("LookupEnv calls = %d, Listen calls = %d; want both zero", lookupCalls, listenCalls)
			}
		})
	}
}

// R-STSK-D18S
func TestProcessShapeAndDefaultListener(t *testing.T) {
	t.Parallel()

	wantRunType := reflect.TypeOf((func(context.Context, Process) int)(nil))
	if got := reflect.TypeOf(Run); got != wantRunType {
		t.Fatalf("Run type = %v, want %v", got, wantRunType)
	}

	typeOfWriter := reflect.TypeOf((*io.Writer)(nil)).Elem()
	typeOfListenerFactory := reflect.TypeOf((func(string, string) (net.Listener, error))(nil))
	typeOfListening := reflect.TypeOf((func(net.Addr))(nil))
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{"Args", reflect.TypeOf([]string(nil))},
		{"LookupEnv", reflect.TypeOf((func(string) (string, bool))(nil))},
		{"Stdout", typeOfWriter},
		{"Stderr", typeOfWriter},
		{"Listen", typeOfListenerFactory},
		{"Listening", typeOfListening},
	}
	processType := reflect.TypeOf(Process{})
	if processType.NumField() != len(wantFields) {
		t.Fatalf("Process has %d fields, want %d", processType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := processType.Field(index)
		if field.Name != want.name || field.Type != want.typeOf {
			t.Errorf("Process field %d = %s %v, want %s %v", index, field.Name, field.Type, want.name, want.typeOf)
		}
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe free port: %v", err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	if err = probe.Close(); err != nil {
		t.Fatalf("close port probe: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var listeningAddr net.Addr
	exit := Run(ctx, Process{
		LookupEnv: mapLookup(map[string]string{"PORT": port}),
		Stdout:    io.Discard,
		Stderr:    io.Discard,
		Listening: func(addr net.Addr) { listeningAddr = addr },
	})
	if exit != ExitSuccess {
		t.Fatalf("Run exit = %d, want ExitSuccess", exit)
	}
	if listeningAddr == nil {
		t.Fatal("Run did not bind with the default net.Listen")
	}
	_, gotPort, err := net.SplitHostPort(listeningAddr.String())
	if err != nil {
		t.Fatalf("split listening address: %v", err)
	}
	if gotPort != port {
		t.Errorf("bound port = %s, want %s", gotPort, port)
	}
}

// R-AWAS-IPSS
func TestExitCodeConstants(t *testing.T) {
	t.Parallel()

	if ExitSuccess != 0 || ExitServerFailed != 1 || ExitUsage != 2 {
		t.Errorf("exit codes = (%d, %d, %d), want (0, 1, 2)", ExitSuccess, ExitServerFailed, ExitUsage)
	}
}

// R-AXIO-WHJH
func TestRunReturnsOnlyDeclaredExitCodes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []Process{
		{Args: []string{"--version"}, Stdout: io.Discard},
		{Args: []string{"bogus"}, Stderr: io.Discard},
		{Stderr: io.Discard},
		{
			LookupEnv: mapLookup(map[string]string{"PORT": "3000"}),
			Stderr:    io.Discard,
			Listen: func(string, string) (net.Listener, error) {
				return nil, errors.New("bind failed")
			},
		},
		{
			LookupEnv: mapLookup(map[string]string{"PORT": "3000"}),
			Stderr:    io.Discard,
			Listen: func(string, string) (net.Listener, error) {
				return &failedListener{err: errors.New("accept failed")}, nil
			},
		},
		{
			LookupEnv: mapLookup(map[string]string{"PORT": "3000"}),
			Stderr:    io.Discard,
			Listen: func(string, string) (net.Listener, error) {
				return newBlockingListener(), nil
			},
		},
	}
	allowed := map[int]bool{ExitSuccess: true, ExitServerFailed: true, ExitUsage: true}
	for index, process := range cases {
		if exit := Run(ctx, process); !allowed[exit] {
			t.Errorf("case %d: Run returned undeclared exit code %d", index, exit)
		}
	}
}

// R-TN2J-IWT8
func TestListeningCallback(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listener := newBlockingListener()
	calls := 0
	var gotAddr net.Addr
	exit := Run(ctx, Process{
		LookupEnv: mapLookup(map[string]string{"PORT": "3000"}),
		Stderr:    io.Discard,
		Listen: func(network, address string) (net.Listener, error) {
			if network != "tcp" || address != "127.0.0.1:3000" {
				t.Errorf("listen called with %q, %q", network, address)
			}
			return listener, nil
		},
		Listening: func(addr net.Addr) {
			calls++
			gotAddr = addr
		},
	})
	if exit != ExitSuccess {
		t.Fatalf("Run exit = %d, want ExitSuccess", exit)
	}
	if calls != 1 {
		t.Errorf("Listening calls = %d, want 1", calls)
	}
	if gotAddr != listener.Addr() {
		t.Errorf("Listening address = %v, want bound address %v", gotAddr, listener.Addr())
	}

	for name, process := range map[string]Process{
		"version":      {Args: []string{"--version"}, Stdout: io.Discard},
		"usage":        {Args: []string{"bogus"}, Stderr: io.Discard},
		"missing PORT": {Stderr: io.Discard},
		"invalid PORT": {
			LookupEnv: mapLookup(map[string]string{"PORT": "not-a-port"}),
			Stderr:    io.Discard,
		},
		"bind failure": {
			LookupEnv: mapLookup(map[string]string{"PORT": "3000"}),
			Stderr:    io.Discard,
			Listen: func(string, string) (net.Listener, error) {
				return nil, errors.New("bind failed")
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			callbackCalls := 0
			process.Listening = func(net.Addr) { callbackCalls++ }
			Run(context.Background(), process)
			if callbackCalls != 0 {
				t.Errorf("Listening calls = %d, want 0", callbackCalls)
			}
		})
	}
}

// R-N4IA-6LSH
func TestPackagesDoNotReachPastProcessSeam(t *testing.T) {
	t.Parallel()

	for _, directory := range []string{"internal/cli", "internal/server"} {
		files, err := filepath.Glob(filepath.Join(projectRoot(t), directory, "*.go"))
		if err != nil {
			t.Fatalf("list %s source: %v", directory, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if parseErr != nil {
				t.Fatalf("parse imports in %s: %v", path, parseErr)
			}
			osNames := make(map[string]bool)
			for _, imported := range parsed.Imports {
				pathValue, unquoteErr := strconv.Unquote(imported.Path.Value)
				if unquoteErr != nil {
					t.Fatalf("unquote import in %s: %v", path, unquoteErr)
				}
				if pathValue == "os/signal" {
					t.Errorf("%s imports os/signal", path)
				}
				if pathValue == "os" {
					name := "os"
					if imported.Name != nil {
						name = imported.Name.Name
					}
					if name == "." {
						t.Errorf("%s dot-imports os, preventing seam verification", path)
					}
					osNames[name] = true
				}
			}
			parsed, parseErr = parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if parseErr != nil {
				t.Fatalf("parse %s: %v", path, parseErr)
			}
			forbidden := map[string]bool{
				"Args": true, "Environ": true, "Getenv": true, "LookupEnv": true,
				"Stdin": true, "Stdout": true, "Stderr": true, "Exit": true,
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if ok && osNames[identifier.Name] && forbidden[selector.Sel.Name] {
					t.Errorf("%s references os.%s", path, selector.Sel.Name)
				}
				return true
			})
		}
	}
}

func mapLookup(environment map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := environment[key]
		return value, ok
	}
}

type testAddr string

func (a testAddr) Network() string { return "tcp" }
func (a testAddr) String() string  { return string(a) }

type failedListener struct {
	err error
}

func (l *failedListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *failedListener) Close() error              { return nil }
func (l *failedListener) Addr() net.Addr            { return testAddr("127.0.0.1:3000") }

type blockingListener struct {
	closed chan struct{}
}

func newBlockingListener() *blockingListener {
	return &blockingListener{closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}
func (l *blockingListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}
func (l *blockingListener) Addr() net.Addr { return testAddr("127.0.0.1:3000") }

var _ net.Listener = (*failedListener)(nil)
var _ net.Listener = (*blockingListener)(nil)
