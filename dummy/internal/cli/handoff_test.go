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
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
)

func testMCP(t *testing.T) *mcp.Server {
	t.Helper()
	t.Setenv(services.Variable, "")
	return mcp.NewServer(mcp.ServerConfig{Name: "dummy", Version: Version, Stderr: io.Discard})
}
func emptyBanner(page.User) page.Banner { return page.Banner{} }
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

// R-EYA8-3P84 R-EVUF-C5QQ
func TestRunReportsDrainOverrunAfterHandlerStarts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	path, notify := readySocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv := testMCP(t)
	var stdout, stderr bytes.Buffer
	result := make(chan int, 1)
	go func() {
		result <- Run(ctx, Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "DRAIN_SECONDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return listener, nil }, Banner: emptyBanner, MCP: srv, Stdout: &stdout, Stderr: &stderr})
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

// R-EJNF-IGBS R-EUMI-YE01 R-EVUF-C5QQ R-EOJ1-1JAK
func TestRunNoRequestsAndNoNotification(t *testing.T) {
	for _, env := range []map[string]string{{}, {"NOTIFY_SOCKET": ""}} {
		srv := testMCP(t)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		env["LISTEN_PID"], env["LISTEN_FDS"] = "42", "1"
		calls := 0
		var out, diagnostics bytes.Buffer
		code := Run(ctx, Process{Pid: 42, LookupEnv: mapLookup(env), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: func(page.User) page.Banner { calls++; return page.Banner{} }, MCP: srv, Stdout: &out, Stderr: &diagnostics})
		if code != ExitSuccess || calls != 0 || out.Len() != 0 || diagnostics.Len() != 0 {
			t.Errorf("code=%d banner=%d out=%q diagnostics=%q", code, calls, out.String(), diagnostics.String())
		}
	}
}

type observingListener struct {
	beforeAccept func()
	err          error
	accepts      int
}

func (l *observingListener) Accept() (net.Conn, error) {
	l.accepts++
	if l.beforeAccept != nil {
		l.beforeAccept()
	}
	return nil, l.err
}
func (l *observingListener) Close() error   { return nil }
func (l *observingListener) Addr() net.Addr { return testAddr("observing") }

// R-EM38-9ZT6 R-EYA8-3P84 R-EVUF-C5QQ
func TestRunNotifiesBeforeAcceptAndReportsServeFailure(t *testing.T) {
	for _, abstract := range []bool{false, true} {
		srv := testMCP(t)
		path, notify := readySocket(t)
		if abstract {
			_ = notify.Close()
			path = "@" + filepath.Base(filepath.Dir(path))
			var err error
			notify, err = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = notify.Close() })
		}
		ln := &observingListener{err: errors.New("serve failure")}
		ln.beforeAccept = func() {
			if err := notify.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			var packet [32]byte
			n, _, err := notify.ReadFromUnix(packet[:])
			if err != nil || string(packet[:n]) != "READY=1" {
				t.Fatalf("readiness=%q error=%v", packet[:n], err)
			}
		}
		var out, diagnostics recordingWriter
		code := Run(context.Background(), Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: emptyBanner, MCP: srv, Stdout: &out, Stderr: &diagnostics})
		if code != ExitServerFailed || out.Len() != 0 || diagnostics.String() != "dummy: serve failure\n" || diagnostics.calls != 1 {
			t.Errorf("code=%d out=%q diagnostic=%q writes=%d", code, out.String(), diagnostics.String(), diagnostics.calls)
		}
		if err := notify.SetReadDeadline(time.Now()); err != nil {
			t.Fatal(err)
		}
		var packet [32]byte
		_, _, err := notify.ReadFromUnix(packet[:])
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Errorf("extra notification check: %v", err)
		}
	}
}

// R-EQYT-T2RY
func TestReadyFailurePreventsAccept(t *testing.T) {
	srv := testMCP(t)
	ln := &observingListener{err: errors.New("unexpected accept")}
	path := filepath.Join(t.TempDir(), "missing.sock")
	wantErr := notifyReady(path)
	if wantErr == nil {
		t.Fatal("notification unexpectedly succeeded")
	}
	var out, diagnostics recordingWriter
	code := Run(context.Background(), Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: emptyBanner, MCP: srv, Stdout: &out, Stderr: &diagnostics})
	if code != ExitServerFailed || out.Len() != 0 || diagnostics.String() != "dummy: "+wantErr.Error()+"\n" || ln.accepts != 0 {
		t.Errorf("code=%d out=%q diagnostic=%q accepts=%d", code, out.String(), diagnostics.String(), ln.accepts)
	}
}

type overlapWriter struct {
	mu              sync.Mutex
	active, overlap bool
	entered         chan struct{}
	release         chan struct{}
	buf             bytes.Buffer
}

func (w *overlapWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.active {
		w.overlap = true
	}
	w.active = true
	w.mu.Unlock()
	w.entered <- struct{}{}
	<-w.release
	for range 100 {
		runtime.Gosched()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active = false
	return w.buf.Write(p)
}

// R-F0Q0-V8PI
func TestRunSerializesConcurrentHandlerDiagnostics(t *testing.T) {
	srv := testMCP(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	path, notify := readySocket(t)
	writer := &overlapWriter{entered: make(chan struct{}, 32), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan int, 1)
	go func() {
		result <- Run(ctx, Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: emptyBanner, MCP: srv, Stdout: io.Discard, Stderr: writer})
	}()
	if err := notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var packet [32]byte
	if _, _, err := notify.ReadFromUnix(packet[:]); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	done := make(chan error, 32)
	for range 32 {
		go func() {
			resp, err := client.Get("http://" + ln.Addr().String() + "/widgets")
			if err == nil {
				_, err = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			done <- err
		}()
	}
	<-writer.entered
	close(writer.release)
	for range 32 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	if code := <-result; code != ExitSuccess {
		t.Errorf("exit=%d", code)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.overlap {
		t.Error("overlapping Stderr.Write calls")
	}
	if strings.Count(writer.buf.String(), "\n") != 32 {
		t.Errorf("diagnostics=%q", writer.buf.String())
	}
}
