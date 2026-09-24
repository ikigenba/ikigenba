package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/server"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func withHandoffStubs(t *testing.T) {
	t.Helper()
	oldServe, oldStore, oldHandler := serve, newStore, panelHandler
	t.Cleanup(func() { serve, newStore, panelHandler = oldServe, oldStore, oldHandler })
	if reflect.ValueOf(oldServe).Pointer() != reflect.ValueOf(server.Serve).Pointer() || reflect.ValueOf(oldStore).Pointer() != reflect.ValueOf(widget.NewStore).Pointer() || reflect.ValueOf(oldHandler).Pointer() != reflect.ValueOf(panel.Handler).Pointer() {
		t.Fatal("default handoff changed")
	}
}

func readySocket(t *testing.T) (string, *net.UnixConn) {
	t.Helper()
	dir, err := os.MkdirTemp("", "dummy-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "notify.sock")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return path, conn
}

// R-MA22-O9VN R-M1IR-ZVOS R-MCHV-FTD1 R-MG5K-L4L4 R-MDPR-TL3Q R-MHDG-YWBT
func TestRunHandoffAndReady(t *testing.T) {
	withHandoffStubs(t)
	for _, drainText := range []string{"", "2", strings.Repeat("9", 100)} {
		t.Run(drainText, func(t *testing.T) {
			path, notify := readySocket(t)
			ln := &failedListener{err: errors.New("unused")}
			store := widget.NewStore()
			handler := http.NewServeMux()
			var events []string
			var handedWriter io.Writer
			newStore = func() *widget.Store { events = append(events, "store"); return store }
			panelHandler = func(got *widget.Store, w io.Writer) http.Handler {
				events = append(events, "handler")
				if got != store {
					t.Error("different store")
				}
				handedWriter = w
				return handler
			}
			ctx := context.Background()
			serve = func(gotCtx context.Context, gotLn net.Listener, gotHandler http.Handler, gotDrain time.Duration) error {
				events = append(events, "serve")
				if gotCtx != ctx || gotLn != ln || gotHandler != handler {
					t.Error("wrong Serve arguments")
				}
				want := 5 * time.Second
				if drainText == "2" {
					want = 2 * time.Second
				}
				if len(drainText) > 10 {
					want = time.Duration(int64(^uint64(0) >> 1))
				}
				if gotDrain != want {
					t.Errorf("drain=%v want %v", gotDrain, want)
				}
				b := make([]byte, 16)
				if err := notify.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				n, _, err := notify.ReadFromUnix(b)
				if err != nil {
					t.Errorf("read readiness: %v", err)
				} else if string(b[:n]) != "READY=1" {
					t.Errorf("ready=%q", b[:n])
				}
				return nil
			}
			env := map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path, "DRAIN_SECONDS": drainText}
			var out, err recordingWriter
			var unset []string
			var inherited []uintptr
			code := Run(ctx, Process{Pid: 42, LookupEnv: mapLookup(env), Unsetenv: func(k string) error { unset = append(unset, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) {
				events = append(events, "inherit")
				inherited = append(inherited, fd)
				return ln, nil
			}, Stdout: &out, Stderr: &err})
			if code != ExitSuccess || out.Len() != 0 || err.Len() != 0 {
				t.Errorf("code=%d out=%q err=%q", code, out.String(), err.String())
			}
			if deadlineErr := notify.SetReadDeadline(time.Now()); deadlineErr != nil {
				t.Fatal(deadlineErr)
			}
			var extra [16]byte
			var timeout net.Error
			if n, _, readErr := notify.ReadFromUnix(extra[:]); readErr == nil {
				t.Errorf("unexpected second readiness datagram %q", extra[:n])
			} else if !errors.As(readErr, &timeout) || !timeout.Timeout() {
				t.Errorf("checking second readiness datagram: %v", readErr)
			}
			if !reflect.DeepEqual(events, []string{"inherit", "store", "handler", "serve"}) || !reflect.DeepEqual(inherited, []uintptr{3}) || !reflect.DeepEqual(unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
				t.Errorf("events=%v inherited=%v unset=%v", events, inherited, unset)
			}
			if handedWriter == nil {
				t.Fatal("handler got nil writer")
			}
			if _, e := handedWriter.Write([]byte("handler diagnostic\n")); e != nil {
				t.Fatal(e)
			}
			if err.String() != "handler diagnostic\n" || err.calls != 1 {
				t.Errorf("handler writer err=%q calls=%d", err.String(), err.calls)
			}
		})
	}
}

// R-MA22-O9VN R-MDPR-TL3Q
func TestRunWithoutNotification(t *testing.T) {
	withHandoffStubs(t)
	ln := &failedListener{err: errors.New("unused")}
	calls := 0
	serve = func(_ context.Context, _ net.Listener, _ http.Handler, drain time.Duration) error {
		calls++
		if drain != 5*time.Second {
			t.Errorf("unset DRAIN_SECONDS yields %v", drain)
		}
		return nil
	}
	var out, err bytes.Buffer
	code := Run(context.Background(), Process{Pid: 1, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "1", "LISTEN_FDS": "1", "NOTIFY_SOCKET": ""}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Stdout: &out, Stderr: &err})
	if code != ExitSuccess || calls != 1 || out.Len() != 0 || err.Len() != 0 {
		t.Errorf("code=%d calls=%d out=%q err=%q", code, calls, out.String(), err.String())
	}
}

// R-MCHV-FTD1
func TestRunNotifiesAbstractSocket(t *testing.T) {
	withHandoffStubs(t)
	dir, err := os.MkdirTemp("", "dummy-abstract-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	address := "@" + filepath.Base(dir)
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notify.Close() })
	serve = func(context.Context, net.Listener, http.Handler, time.Duration) error {
		if err := notify.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		var packet [16]byte
		n, _, err := notify.ReadFromUnix(packet[:])
		if err != nil || string(packet[:n]) != "READY=1" {
			t.Errorf("abstract readiness = %q, error %v", packet[:n], err)
		}
		return nil
	}
	var out, diagnostics bytes.Buffer
	code := Run(context.Background(), Process{Pid: 3, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "3", "LISTEN_FDS": "1", "NOTIFY_SOCKET": address}), Inherit: func(uintptr) (net.Listener, error) { return &failedListener{}, nil }, Stdout: &out, Stderr: &diagnostics})
	if code != ExitSuccess || out.Len() != 0 || diagnostics.Len() != 0 {
		t.Errorf("code=%d out=%q stderr=%q", code, out.String(), diagnostics.String())
	}
}

// R-MA22-O9VN
func TestRunBuildsNoStoreBeforeSocketIsTaken(t *testing.T) {
	withHandoffStubs(t)
	newStore = func() *widget.Store { t.Error("store built before socket"); return nil }
	panelHandler = func(*widget.Store, io.Writer) http.Handler { t.Error("handler built before socket"); return nil }
	serve = func(context.Context, net.Listener, http.Handler, time.Duration) error {
		t.Error("served without socket")
		return nil
	}
	for _, args := range [][]string{{"--version"}, {"bogus"}, nil} {
		p := Process{Args: args, Pid: 9, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "9", "LISTEN_FDS": "1"}), Inherit: func(uintptr) (net.Listener, error) { return nil, errors.New("take failed") }, Stdout: io.Discard, Stderr: io.Discard}
		Run(context.Background(), p)
	}
}

// R-WA57-FG78
func TestReadyFailurePreventsServe(t *testing.T) {
	withHandoffStubs(t)
	ln := &failedListener{err: errors.New("unused")}
	serve = func(context.Context, net.Listener, http.Handler, time.Duration) error {
		t.Error("served after notify failed")
		return nil
	}
	path := filepath.Join(t.TempDir(), "missing.sock")
	wantErr := notifyReady(path)
	if wantErr == nil {
		t.Fatal("notification unexpectedly succeeded")
	}
	var out, err recordingWriter
	code := Run(context.Background(), Process{Pid: 1, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "1", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Stdout: &out, Stderr: &err})
	if code != ExitServerFailed || out.Len() != 0 || err.calls != 1 || err.String() != "dummy: "+wantErr.Error()+"\n" {
		t.Errorf("code=%d out=%q err=%q writes=%d", code, out.String(), err.String(), err.calls)
	}
}

// R-QVIS-THYV R-MHDG-YWBT
func TestRunReportsServeFailure(t *testing.T) {
	withHandoffStubs(t)
	wantErr := errors.New("server broke")
	serve = func(context.Context, net.Listener, http.Handler, time.Duration) error { return wantErr }
	var out, err recordingWriter
	code := Run(context.Background(), Process{Pid: 7, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "7", "LISTEN_FDS": "1"}), Inherit: func(uintptr) (net.Listener, error) { return &failedListener{}, nil }, Stdout: &out, Stderr: &err})
	if code != ExitServerFailed || out.Len() != 0 || err.String() != "dummy: server broke\n" || err.calls != 1 {
		t.Errorf("code=%d out=%q err=%q writes=%d", code, out.String(), err.String(), err.calls)
	}
}

// R-MILD-CO2I
func TestRunSerializesHandlerAndServeDiagnostics(t *testing.T) {
	withHandoffStubs(t)
	writer := &overlapWriter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	var handlerWriter io.Writer
	serveReturned := make(chan struct{})
	handlerDone := make(chan struct{})
	panelHandler = func(_ *widget.Store, w io.Writer) http.Handler { handlerWriter = w; return http.NotFoundHandler() }
	serve = func(context.Context, net.Listener, http.Handler, time.Duration) error {
		go func() { defer close(handlerDone); _, _ = handlerWriter.Write([]byte("handler\n")) }()
		<-writer.entered
		close(serveReturned)
		return errors.New("serve failure")
	}
	result := make(chan int, 1)
	go func() {
		result <- Run(context.Background(), Process{Pid: 7, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "7", "LISTEN_FDS": "1"}), Inherit: func(uintptr) (net.Listener, error) { return &failedListener{}, nil }, Stdout: io.Discard, Stderr: writer})
	}()
	<-serveReturned
	for range 100 {
		runtime.Gosched()
	}
	close(writer.release)
	<-handlerDone
	code := <-result
	if code != ExitServerFailed || writer.overlap || writer.String() != "handler\ndummy: serve failure\n" {
		t.Errorf("code=%d overlap=%t output=%q", code, writer.overlap, writer.String())
	}
}

// R-MILD-CO2I R-QVIS-THYV
func TestRunReportsDrainOverrunAfterHandlerStarts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	path, notify := readySocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var stdout, stderr bytes.Buffer
	result := make(chan int, 1)
	go func() {
		result <- Run(ctx, Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "DRAIN_SECONDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return listener, nil }, Stdout: &stdout, Stderr: &stderr})
	}()
	if err = notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var ready [16]byte
	n, _, err := notify.ReadFromUnix(ready[:])
	if err != nil || string(ready[:n]) != "READY=1" {
		t.Fatalf("ready datagram %q, error %v", ready[:n], err)
	}
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	request := "POST /widgets HTTP/1.1\r\nHost: dummy\r\nX-User-Id: test-user\r\nContent-Type: application/x-www-form-urlencoded\r\nExpect: 100-continue\r\nContent-Length: 100\r\n\r\nname=x"
	if _, err = io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "100 Continue") {
		t.Fatalf("first response line = %q, error %v", line, err)
	}
	cancel()
	select {
	case code := <-result:
		if code != ExitServerFailed {
			t.Errorf("exit = %d, want %d", code, ExitServerFailed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after drain")
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "dummy: stopped with 1 request unfinished\n") {
		t.Errorf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	if lines := strings.Count(stderr.String(), "dummy: stopped with 1 request unfinished\n"); lines != 1 {
		t.Errorf("drain diagnostics = %d", lines)
	}
}

type overlapWriter struct {
	mu      sync.Mutex
	active  bool
	overlap bool
	buf     bytes.Buffer
	entered chan struct{}
	release chan struct{}
}

func (w *overlapWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.active {
		w.overlap = true
	}
	w.active = true
	w.mu.Unlock()
	if string(p) == "handler\n" {
		w.entered <- struct{}{}
		<-w.release
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active = false
	return w.buf.Write(p)
}
func (w *overlapWriter) String() string { w.mu.Lock(); defer w.mu.Unlock(); return w.buf.String() }
