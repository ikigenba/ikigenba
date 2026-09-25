package cli

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// R-10H0-0STN R-LPBS-669U
func TestUsageConstant(t *testing.T) {
	const want = "Usage: dummy [command]\n\nServe the dummy control panel on the socket systemd passes in. With no\ncommand, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"
	const declared = Usage
	if declared != want {
		t.Errorf("Usage = %q, want %q", declared, want)
	}
}

// R-LJ8A-9BKD
func TestProcessShape(t *testing.T) {
	if reflect.TypeOf(Run) != reflect.TypeOf((func(context.Context, Process) int)(nil)) {
		t.Fatal("Run has wrong signature")
	}
	writer := reflect.TypeOf((*io.Writer)(nil)).Elem()
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{"Args", reflect.TypeOf([]string(nil))}, {"LookupEnv", reflect.TypeOf((func(string) (string, bool))(nil))},
		{"Unsetenv", reflect.TypeOf((func(string) error)(nil))}, {"Pid", reflect.TypeOf(int(0))},
		{"Stdout", writer}, {"Stderr", writer}, {"Inherit", reflect.TypeOf((func(uintptr) (net.Listener, error))(nil))},
	}
	got := reflect.TypeOf(Process{})
	if got.NumField() != len(want) {
		t.Fatalf("Process fields = %d, want %d", got.NumField(), len(want))
	}
	for i, field := range want {
		if got.Field(i).Name != field.name || got.Field(i).Type != field.typ {
			t.Errorf("field %d = %v, want %v", i, got.Field(i), field)
		}
	}
}

// R-MMB3-ASQ6 R-MNIZ-OKGV R-MOQW-2C7K R-MUSD-6DHG
func TestRunCommandsDoNotTouchServeState(t *testing.T) {
	for _, tc := range []struct{ arg, want string }{{"--version", Version + "\n"}, {"manifest", Manifest}, {"--help", Usage}} {
		t.Run(tc.arg, func(t *testing.T) {
			var out, err recordingWriter
			calls := 0
			p := Process{Args: []string{tc.arg}, LookupEnv: func(string) (string, bool) { calls++; return "", false }, Unsetenv: func(string) error { calls++; return nil }, Inherit: func(uintptr) (net.Listener, error) { calls++; return nil, errors.New("unexpected") }, Stdout: &out, Stderr: &err}
			if code := Run(context.Background(), p); code != ExitSuccess {
				t.Errorf("exit = %d", code)
			}
			if out.String() != tc.want || err.Len() != 0 || calls != 0 {
				t.Errorf("out=%q err=%q calls=%d", out.String(), err.String(), calls)
			}
		})
	}
}

// R-MPYS-G3Y9 R-MR6O-TVOY R-MSEL-7NFN R-MUSD-6DHG R-N0XV-W1MI
func TestRunInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"bogus"}, "dummy: unknown command 'bogus'\n\nsee 'dummy --help' for usage\n"},
		{[]string{"--bogus"}, "dummy: unknown option '--bogus'\n\nsee 'dummy --help' for usage\n"},
		{[]string{"--help", "extra"}, "dummy: unknown command 'extra'\n\nsee 'dummy --help' for usage\n"},
		{[]string{"manifest", "-x"}, "dummy: unknown option '-x'\n\nsee 'dummy --help' for usage\n"},
		{[]string{"bogus", "--later"}, "dummy: unknown command 'bogus'\n\nsee 'dummy --help' for usage\n"},
		{[]string{"--help", "second", "third"}, "dummy: unknown command 'second'\n\nsee 'dummy --help' for usage\n"},
		{[]string{""}, "dummy: unknown command ''\n\nsee 'dummy --help' for usage\n"},
	} {
		var out, err recordingWriter
		calls := 0
		p := Process{Args: tc.args, LookupEnv: func(string) (string, bool) { calls++; return "", false }, Unsetenv: func(string) error { calls++; return nil }, Inherit: func(uintptr) (net.Listener, error) { calls++; return nil, nil }, Stdout: &out, Stderr: &err}
		if code := Run(context.Background(), p); code != ExitUsage {
			t.Errorf("%v exit=%d", tc.args, code)
		}
		if out.Len() != 0 || err.String() != tc.want || err.calls != 1 || calls != 0 {
			t.Errorf("%v out=%q err=%q writes=%d calls=%d", tc.args, out.String(), err.String(), err.calls, calls)
		}
	}
}

// R-LXV2-UKGP R-LZ2Z-8C7E R-W59L-WD8G
func TestDrainValidationPrecedesSocket(t *testing.T) {
	for _, s := range []string{"0", "01", "-1", "2.5", "5s", " 5", "abc", "1 ", "１２"} {
		var out, err recordingWriter
		var looked []string
		inherit, unset := 0, 0
		p := Process{LookupEnv: func(k string) (string, bool) {
			looked = append(looked, k)
			if k == "DRAIN_SECONDS" {
				return s, true
			}
			return "", false
		}, Unsetenv: func(string) error { unset++; return nil }, Inherit: func(uintptr) (net.Listener, error) { inherit++; return nil, nil }, Stdout: &out, Stderr: &err}
		if code := Run(context.Background(), p); code != ExitUsage {
			t.Errorf("%q exit=%d", s, code)
		}
		want := "dummy: DRAIN_SECONDS is '" + s + "', not a positive whole number of seconds\n"
		if out.Len() != 0 || err.String() != want || err.calls != 1 || inherit != 0 || unset != 0 || !reflect.DeepEqual(looked, []string{"DRAIN_SECONDS"}) {
			t.Errorf("%q out=%q err=%q looked=%v inherit=%d unset=%d", s, out.String(), err.String(), looked, inherit, unset)
		}
	}
	for _, s := range []string{"1", "5", "9223372036854775808", strings.Repeat("9", 100)} {
		if _, ok := parseDrain(s); !ok {
			t.Errorf("valid drain %q rejected", s)
		}
	}
}

// R-M1IR-ZVOS
func TestDrainDuration(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want time.Duration
	}{{"", 5 * time.Second}, {"1", time.Second}, {"9223372036", 9223372036 * time.Second}, {"9223372037", time.Duration(math.MaxInt64)}, {strings.Repeat("9", 100), time.Duration(math.MaxInt64)}} {
		got, ok := parseDrain(tc.s)
		if !ok || got != tc.want {
			t.Errorf("parseDrain(%q) = %v,%t want %v", tc.s, got, ok, tc.want)
		}
	}
}

// R-M2QO-DNFH R-W6HI-A4Z5 R-W7PE-NWPU
func TestSocketCount(t *testing.T) {
	const hint = "\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3000 dummy'\n"
	for _, tc := range []struct {
		pid, fds string
		want     string
	}{
		{"", "1", "dummy: no socket was passed in" + hint}, {"43", "1", "dummy: no socket was passed in" + hint},
		{"42", "", "dummy: no socket was passed in" + hint}, {"42", "0", "dummy: no socket was passed in" + hint},
		{"42", "-1", "dummy: no socket was passed in" + hint}, {"42", "1x", "dummy: no socket was passed in" + hint},
		{"42", "2", "dummy: 2 sockets were passed in, expected 1" + hint}, {"42", "0002", "dummy: 0002 sockets were passed in, expected 1" + hint},
	} {
		var out, err recordingWriter
		calls := 0
		p := Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": tc.pid, "LISTEN_FDS": tc.fds}), Unsetenv: func(string) error { calls++; return nil }, Inherit: func(uintptr) (net.Listener, error) { calls++; return nil, nil }, Stdout: &out, Stderr: &err}
		if code := Run(context.Background(), p); code != ExitUsage {
			t.Errorf("%q,%q exit=%d", tc.pid, tc.fds, code)
		}
		if out.Len() != 0 || err.String() != tc.want || err.calls != 1 || calls != 0 {
			t.Errorf("%q,%q out=%q err=%q writes=%d calls=%d", tc.pid, tc.fds, out.String(), err.String(), err.calls, calls)
		}
	}
}

// R-M6ED-IYNK R-M7M9-WQE9 R-W8XB-1OGJ
func TestRunTakesOnlyDescriptorThree(t *testing.T) {
	var out, err recordingWriter
	var unset []string
	var fds []uintptr
	p := Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "0001"}), Unsetenv: func(k string) error { unset = append(unset, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) {
		fds = append(fds, fd)
		return nil, errors.New("bad descriptor")
	}, Stdout: &out, Stderr: &err}
	if code := Run(context.Background(), p); code != ExitServerFailed {
		t.Errorf("exit=%d", code)
	}
	if !reflect.DeepEqual(unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) || !reflect.DeepEqual(fds, []uintptr{3}) || out.Len() != 0 || err.String() != "dummy: bad descriptor\n" || err.calls != 1 {
		t.Errorf("unset=%v fds=%v out=%q err=%q writes=%d", unset, fds, out.String(), err.String(), err.calls)
	}
}

// R-AXIO-WHJH R-N0XV-W1MI
func TestRunReturnsDeclaredExitCodes(t *testing.T) {
	p := Process{Stderr: io.Discard}
	for _, args := range [][]string{{"--version"}, {"bogus"}, nil} {
		p.Args = args
		if len(args) > 0 && args[0] == "--version" {
			p.Stdout = io.Discard
		}
		code := Run(context.Background(), p)
		if code != ExitSuccess && code != ExitServerFailed && code != ExitUsage {
			t.Errorf("exit=%d", code)
		}
	}
}

// R-5IC7-VGNQ
func TestPackagesDoNotReachPastProcessSeam(t *testing.T) {
	forbidden := map[string]bool{"Args": true, "Environ": true, "Getenv": true, "LookupEnv": true, "Setenv": true, "Unsetenv": true, "Clearenv": true, "Getpid": true, "Stdin": true, "Stdout": true, "Stderr": true, "Exit": true}
	for _, dir := range []string{".", "internal/cli", "internal/server", "internal/panel", "internal/widget"} {
		files, err := filepath.Glob(filepath.Join(projectRoot(t), dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			parsed, e := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if e != nil {
				t.Fatal(e)
			}
			osNames := map[string]bool{}
			for _, imp := range parsed.Imports {
				name, e := strconv.Unquote(imp.Path.Value)
				if e != nil {
					t.Fatal(e)
				}
				if name == "os/signal" {
					t.Errorf("%s imports os/signal", path)
				}
				if name == "os" {
					alias := "os"
					if imp.Name != nil {
						alias = imp.Name.Name
					}
					if alias == "." {
						t.Errorf("%s dot imports os", path)
					}
					osNames[alias] = true
				}
			}
			ast.Inspect(parsed, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if ok && osNames[ident.Name] && forbidden[sel.Sel.Name] {
					t.Errorf("%s references os.%s", path, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

// R-WBD3-T7XX
func TestModuleNeverOpensListeningSocket(t *testing.T) {
	forbidden := map[string]map[string]bool{"net": {"Listen": true, "ListenTCP": true, "ListenUnix": true, "ListenUDP": true, "ListenUnixgram": true, "ListenIP": true, "ListenMulticastUDP": true, "ListenPacket": true}, "net/http": {"ListenAndServe": true, "ListenAndServeTLS": true}, "syscall": {"Socket": true, "Bind": true, "Listen": true}}
	methodNames := map[string]bool{"Listen": true, "ListenPacket": true, "ListenAndServe": true, "ListenAndServeTLS": true}
	for _, dir := range []string{"cmd/dummy", "internal/cli", "internal/server", "internal/panel", "internal/widget"} {
		files, _ := filepath.Glob(filepath.Join(projectRoot(t), dir, "*.go"))
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			parsed, e := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if e != nil {
				t.Fatal(e)
			}
			aliases := map[string]string{}
			for _, imp := range parsed.Imports {
				importPath, unquoteErr := strconv.Unquote(imp.Path.Value)
				if unquoteErr != nil {
					t.Fatal(unquoteErr)
				}
				if forbidden[importPath] != nil {
					alias := filepath.Base(importPath)
					if imp.Name != nil {
						alias = imp.Name.Name
					}
					if alias == "." {
						t.Errorf("%s dot imports %s", path, importPath)
					}
					aliases[alias] = importPath
				}
			}
			ast.Inspect(parsed, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if ok && forbidden[aliases[id.Name]][sel.Sel.Name] {
					t.Errorf("%s calls %s.%s", path, id.Name, sel.Sel.Name)
				} else if methodNames[sel.Sel.Name] && (!ok || aliases[id.Name] == "") {
					t.Errorf("%s calls forbidden listening method %s", path, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

func mapLookup(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

type recordingWriter struct {
	bytes.Buffer
	calls int
}

func (w *recordingWriter) Write(p []byte) (int, error) { w.calls++; return w.Buffer.Write(p) }

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

type failedListener struct{ err error }

func (l *failedListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *failedListener) Close() error              { return nil }
func (l *failedListener) Addr() net.Addr            { return testAddr("failed") }
