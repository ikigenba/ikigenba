package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
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

			var stdout bytes.Buffer
			var stderr recordingWriter
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

			var stdout bytes.Buffer
			var stderr recordingWriter
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
			if stderr.calls != 1 {
				t.Errorf("stderr Write calls = %d, want 1", stderr.calls)
			}
			if lookupCalls != 0 || listenCalls != 0 {
				t.Errorf("LookupEnv calls = %d, Listen calls = %d; want both zero", lookupCalls, listenCalls)
			}
		})
	}
}

// R-MUUD-Z6X1 R-N0XV-W1MI
func TestRunRejectsMissingPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		lookup func(string) (string, bool)
	}{
		{name: "nil lookup"},
		{name: "unset", lookup: func(string) (string, bool) { return "ignored", false }},
		{name: "empty", lookup: func(string) (string, bool) { return "", true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr recordingWriter
			listenCalls := 0
			exit := Run(context.Background(), Process{
				LookupEnv: test.lookup,
				Stdout:    &stdout,
				Stderr:    &stderr,
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
			if got, want := stderr.String(), "dummy: PORT is not set\n"; got != want {
				t.Errorf("stderr = %q, want %q", got, want)
			}
			if stderr.calls != 1 {
				t.Errorf("stderr Write calls = %d, want 1", stderr.calls)
			}
			if listenCalls != 0 {
				t.Errorf("Listen calls = %d, want 0", listenCalls)
			}
		})
	}
}

// R-MW2A-CYNQ R-ZOWQ-BRGU
func TestRunPortNumberGrammar(t *testing.T) {
	t.Parallel()

	valid := []string{"1", "9", "10", "3000", "9999", "10000", "65535"}
	for _, port := range valid {
		var stdout bytes.Buffer
		var stderr recordingWriter
		listenCalls := 0
		var network, address string
		exit := Run(context.Background(), Process{
			LookupEnv: mapLookup(map[string]string{"PORT": port}),
			Stdout:    &stdout,
			Stderr:    &stderr,
			Listen: func(gotNetwork, gotAddress string) (net.Listener, error) {
				listenCalls++
				network, address = gotNetwork, gotAddress
				return nil, errors.New("observed accepted port")
			},
		})
		if exit != ExitServerFailed {
			t.Errorf("PORT %q: Run exit = %d, want ExitServerFailed after bind attempt", port, exit)
		}
		if listenCalls != 1 || network != "tcp" || address != net.JoinHostPort("127.0.0.1", port) {
			t.Errorf("PORT %q: Listen calls = %d with %q, %q; want one TCP bind attempt", port, listenCalls, network, address)
		}
		if stdout.Len() != 0 {
			t.Errorf("PORT %q: stdout = %q, want empty", port, stdout.String())
		}
	}

	invalid := []string{
		"", "0", "00", "01", "00001", "65536", "99999", "100000",
		"+1", "-1", " 1", "1 ", "1\n", "1.0", "1a", "１２",
	}
	for _, port := range invalid {
		var stdout bytes.Buffer
		var stderr recordingWriter
		listenCalls := 0
		exit := Run(context.Background(), Process{
			LookupEnv: mapLookup(map[string]string{"PORT": port}),
			Stdout:    &stdout,
			Stderr:    &stderr,
			Listen: func(string, string) (net.Listener, error) {
				listenCalls++
				return nil, errors.New("unexpected listen")
			},
		})
		if exit != ExitUsage {
			t.Errorf("PORT %q: Run exit = %d, want ExitUsage", port, exit)
		}
		if stdout.Len() != 0 {
			t.Errorf("PORT %q: stdout = %q, want empty", port, stdout.String())
		}
		want := "dummy: PORT is '" + port + "', not a port number\n"
		if port == "" {
			want = "dummy: PORT is not set\n"
		}
		if stderr.String() != want {
			t.Errorf("PORT %q: stderr = %q, want %q", port, stderr.String(), want)
		}
		if stderr.calls != 1 {
			t.Errorf("PORT %q: stderr Write calls = %d, want 1", port, stderr.calls)
		}
		if listenCalls != 0 {
			t.Errorf("PORT %q: Listen calls = %d, want 0", port, listenCalls)
		}
	}
}

// R-ZQ4M-PJ7J
func TestRunHandsValidPortToServer(t *testing.T) {
	originalServe, originalHandler := serve, serverHandler
	t.Cleanup(func() {
		serve, serverHandler = originalServe, originalHandler
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	listener := newBlockingListener()
	listenCalls := 0
	var network, address string
	handler := http.NewServeMux()
	handlerCalls := 0
	serverHandler = func() http.Handler {
		handlerCalls++
		return handler
	}
	serveCalls := 0
	var gotCtx context.Context
	var gotListener net.Listener
	var gotHandler http.Handler
	serve = func(callCtx context.Context, callListener net.Listener, callHandler http.Handler) error {
		serveCalls++
		gotCtx, gotListener, gotHandler = callCtx, callListener, callHandler
		return callListener.Close()
	}
	var stdout, stderr bytes.Buffer
	exit := Run(ctx, Process{
		LookupEnv: mapLookup(map[string]string{"PORT": "65535"}),
		Stdout:    &stdout,
		Stderr:    &stderr,
		Listen: func(gotNetwork, gotAddress string) (net.Listener, error) {
			listenCalls++
			network, address = gotNetwork, gotAddress
			return listener, nil
		},
	})
	if exit != ExitSuccess {
		t.Errorf("Run exit = %d, want ExitSuccess", exit)
	}
	if listenCalls != 1 || network != "tcp" || address != "127.0.0.1:65535" {
		t.Errorf("Listen calls = %d with %q, %q; want one with tcp, 127.0.0.1:65535", listenCalls, network, address)
	}
	if serveCalls != 1 {
		t.Errorf("Serve calls = %d, want 1", serveCalls)
	}
	if gotCtx != ctx {
		t.Error("Serve did not receive Run's context")
	}
	if gotListener != listener {
		t.Error("Serve did not receive the listener returned by Listen")
	}
	if handlerCalls != 1 || gotHandler != handler {
		t.Errorf("Handler calls = %d and Serve handler = %T; want one call and its returned handler", handlerCalls, gotHandler)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q; want both empty", stdout.String(), stderr.String())
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe free port: %v", err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	if err = probe.Close(); err != nil {
		t.Fatalf("close port probe: %v", err)
	}
	serveCalls, handlerCalls = 0, 0
	gotCtx, gotListener, gotHandler = nil, nil, nil
	var listeningAddr net.Addr
	exit = Run(ctx, Process{
		LookupEnv: mapLookup(map[string]string{"PORT": port}),
		Stdout:    &stdout,
		Stderr:    &stderr,
		Listening: func(addr net.Addr) { listeningAddr = addr },
	})
	if exit != ExitSuccess {
		t.Fatalf("Run with nil Listen exit = %d, want ExitSuccess", exit)
	}
	if serveCalls != 1 || handlerCalls != 1 || gotCtx != ctx || gotListener == nil || gotHandler != handler {
		t.Errorf("nil Listen path called Serve %d and Handler %d times with context match %t, listener %v, handler match %t", serveCalls, handlerCalls, gotCtx == ctx, gotListener, gotHandler == handler)
	}
	if _, ok := gotListener.(*net.TCPListener); !ok {
		t.Errorf("nil Listen produced listener %T, want *net.TCPListener from net.Listen", gotListener)
	}
	if listeningAddr == nil {
		t.Fatal("nil Listen path did not report its bound address")
	}
	_, gotPort, splitErr := net.SplitHostPort(listeningAddr.String())
	if splitErr != nil {
		t.Fatalf("split listening address: %v", splitErr)
	}
	if gotPort != port {
		t.Errorf("nil Listen bound port = %s, want %s", gotPort, port)
	}
}

// R-N0XV-W1MI
func TestRunServerFailuresKeepStdoutEmptyAndWriteOneDiagnostic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		listen func(string, string) (net.Listener, error)
	}{
		{name: "bind", listen: func(string, string) (net.Listener, error) {
			return nil, errors.New("bind failed")
		}},
		{name: "serve", listen: func(string, string) (net.Listener, error) {
			return &failedListener{err: errors.New("accept failed")}, nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr recordingWriter
			exit := Run(context.Background(), Process{
				LookupEnv: mapLookup(map[string]string{"PORT": "3000"}),
				Stdout:    &stdout,
				Stderr:    &stderr,
				Listen:    test.listen,
			})
			if exit == ExitSuccess {
				t.Error("Run exit = ExitSuccess, want failure")
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if stderr.calls != 1 {
				t.Errorf("stderr Write calls = %d, want 1", stderr.calls)
			}
			if !strings.HasPrefix(stderr.String(), "dummy: ") {
				t.Errorf("stderr = %q, want first line to begin dummy: ", stderr.String())
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

// R-ICZ1-6UGJ R-QQN7-AF03 R-8TZB-3NSI
func TestRunBindsServesSilentlyAndDrainsOnCancellation(t *testing.T) {
	originalHandler := serverHandler
	t.Cleanup(func() { serverHandler = originalHandler })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tracked := &closeTrackingListener{Listener: listener, closed: make(chan struct{})}
	t.Cleanup(func() { _ = tracked.Close() })
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}

	requestStarted := make(chan struct{})
	finishResponse := make(chan struct{})
	serverHandler = func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(requestStarted)
			_, _ = io.WriteString(w, "start-")
			w.(http.Flusher).Flush()
			<-finishResponse
			_, _ = io.WriteString(w, "finish")
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	listenCalls := 0
	listening := make(chan net.Addr, 1)
	exitResult := make(chan int, 1)
	go func() {
		exitResult <- Run(ctx, Process{
			LookupEnv: mapLookup(map[string]string{"PORT": port}),
			Stdout:    &stdout,
			Stderr:    &stderr,
			Listen: func(network, address string) (net.Listener, error) {
				listenCalls++
				if network != "tcp" || address != net.JoinHostPort("127.0.0.1", port) {
					t.Errorf("Listen called with %q, %q", network, address)
				}
				return tracked, nil
			},
			Listening: func(address net.Addr) { listening <- address },
		})
	}()
	if address := <-listening; address != tracked.Addr() {
		t.Errorf("Listening address = %v, want listener Addr %v", address, tracked.Addr())
	}

	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	if _, err = io.WriteString(connection, "GET / HTTP/1.1\r\nHost: dummy\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	responseResult := make(chan *http.Response, 1)
	responseErrors := make(chan error, 1)
	go func() {
		response, readErr := http.ReadResponse(bufio.NewReader(connection), nil)
		if readErr != nil {
			responseErrors <- readErr
			return
		}
		responseResult <- response
	}()
	<-requestStarted
	cancel()
	<-tracked.closed
	select {
	case exit := <-exitResult:
		t.Fatalf("Run returned %d before accepted response completed", exit)
	default:
	}
	close(finishResponse)

	var response *http.Response
	select {
	case err = <-responseErrors:
		t.Fatalf("read response: %v", err)
	case response = <-responseResult:
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	_ = response.Body.Close()
	_ = connection.Close()
	if got := string(body); got != "start-finish" {
		t.Errorf("response body = %q, want %q", got, "start-finish")
	}
	if exit := <-exitResult; exit != ExitSuccess {
		t.Errorf("Run exit = %d, want ExitSuccess", exit)
	}
	if listenCalls != 1 {
		t.Errorf("Listen calls = %d, want 1", listenCalls)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q; want both empty", stdout.String(), stderr.String())
	}
}

// R-ICZ1-6UGJ
func TestRunUsesNetListenWhenFactoryIsNil(t *testing.T) {
	originalServe, originalHandler := serve, serverHandler
	t.Cleanup(func() {
		serve, serverHandler = originalServe, originalHandler
	})

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe free port: %v", err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	if err = probe.Close(); err != nil {
		t.Fatalf("close port probe: %v", err)
	}

	wantHandler := http.NewServeMux()
	serverHandler = func() http.Handler { return wantHandler }
	var servedListener net.Listener
	serve = func(_ context.Context, listener net.Listener, handler http.Handler) error {
		servedListener = listener
		if handler != wantHandler {
			t.Errorf("Serve handler = %T, want configured handler", handler)
		}
		return listener.Close()
	}
	var listeningAddr net.Addr
	exit := Run(context.Background(), Process{
		LookupEnv: mapLookup(map[string]string{"PORT": port}),
		Stdout:    io.Discard,
		Stderr:    io.Discard,
		Listening: func(addr net.Addr) { listeningAddr = addr },
	})
	if exit != ExitSuccess {
		t.Fatalf("Run exit = %d, want ExitSuccess", exit)
	}
	if _, ok := servedListener.(*net.TCPListener); !ok {
		t.Fatalf("listener = %T, want *net.TCPListener from net.Listen", servedListener)
	}
	wantAddress := net.JoinHostPort("127.0.0.1", port)
	if got := servedListener.Addr().String(); got != wantAddress {
		t.Errorf("bound address = %q, want %q", got, wantAddress)
	}
	if listeningAddr != servedListener.Addr() {
		t.Errorf("Listening address = %v, want listener Addr %v", listeningAddr, servedListener.Addr())
	}
}

// R-QUAW-FQ86
func TestRunReportsExactBindErrorAndLeavesHolderListening(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold port: %v", err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	_, port, err := net.SplitHostPort(holder.Addr().String())
	if err != nil {
		t.Fatalf("split holder address: %v", err)
	}
	probe, wantErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if wantErr == nil {
		_ = probe.Close()
		t.Fatal("second listen unexpectedly succeeded")
	}

	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), Process{
		LookupEnv: mapLookup(map[string]string{"PORT": port}),
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	if exit != ExitServerFailed {
		t.Errorf("Run exit = %d, want ExitServerFailed", exit)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if got, want := stderr.String(), "dummy: "+wantErr.Error()+"\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}

	accepted := make(chan error, 1)
	go func() {
		connection, acceptErr := holder.Accept()
		if acceptErr == nil {
			_ = connection.Close()
		}
		accepted <- acceptErr
	}()
	connection, err := net.Dial("tcp", holder.Addr().String())
	if err != nil {
		t.Fatalf("dial holder after Run: %v", err)
	}
	_ = connection.Close()
	if err = <-accepted; err != nil {
		t.Errorf("holder accept after Run: %v", err)
	}
}

// R-QVIS-THYV
func TestRunReportsExactServeError(t *testing.T) {
	originalServe, originalHandler := serve, serverHandler
	t.Cleanup(func() { serve, serverHandler = originalServe, originalHandler })
	wantErr := errors.New("server broke")
	serve = func(context.Context, net.Listener, http.Handler) error { return wantErr }
	serverHandler = http.NotFoundHandler
	listener := newBlockingListener()
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), Process{
		LookupEnv: mapLookup(map[string]string{"PORT": "1"}),
		Stdout:    &stdout,
		Stderr:    &stderr,
		Listen: func(string, string) (net.Listener, error) {
			return listener, nil
		},
	})
	_ = listener.Close()
	if exit != ExitServerFailed {
		t.Errorf("Run exit = %d, want ExitServerFailed", exit)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if got, want := stderr.String(), "dummy: "+wantErr.Error()+"\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
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

type recordingWriter struct {
	bytes.Buffer
	calls int
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.calls++
	return w.Buffer.Write(p)
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

type closeTrackingListener struct {
	net.Listener
	closed chan struct{}
}

func (l *closeTrackingListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return l.Listener.Close()
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
