package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
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

// R-PE6U-PF31 R-PFER-36TQ R-EUBD-X7ZQ
func TestRunReportsDrainOverrunAfterHandlerStarts(t *testing.T) {
	for _, tc := range []struct {
		name, value   string
		set, complete bool
		duration      time.Duration
	}{
		{"one", "1", true, false, time.Second},
		{"unset", "", false, false, 5 * time.Second},
		{"empty", "", true, false, 5 * time.Second},
		{"huge", strings.Repeat("9", 100), true, true, 7 * time.Second},
	} {
		srv := testMCP(t)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			path, notify := readySocket(t)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			env := map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}
			if tc.set {
				env["DRAIN_SECONDS"] = tc.value
			}
			var stdout, stderr bytes.Buffer
			result := make(chan int, 1)
			go func() {
				result <- Run(ctx, Process{Pid: 42, LookupEnv: mapLookup(env), Inherit: func(uintptr) (net.Listener, error) { return listener, nil }, Banner: emptyBanner, MCP: srv, Stdout: &stdout, Stderr: &stderr})
			}()
			waitReady(t, notify)
			conn, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			if err = conn.SetDeadline(time.Now().Add(12 * time.Second)); err != nil {
				t.Fatal(err)
			}
			body := "name=drained&count=2&status=active"
			header := fmt.Sprintf("POST /widgets HTTP/1.1\r\nHost: dummy\r\nX-User-Id: test-user\r\nContent-Type: application/x-www-form-urlencoded\r\nExpect: 100-continue\r\nContent-Length: %d\r\n\r\n", len(body))
			if _, err = io.WriteString(conn, header); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(conn)
			response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
			if err != nil || response.StatusCode != http.StatusContinue {
				t.Fatalf("continue=%v error=%v", response, err)
			}
			_ = response.Body.Close()
			started := time.Now()
			cancel()
			if tc.complete {
				if err = conn.SetReadDeadline(started.Add(tc.duration)); err != nil {
					t.Fatal(err)
				}
				_, err = reader.Peek(1)
				var netErr net.Error
				if !errors.As(err, &netErr) || !netErr.Timeout() {
					t.Fatalf("large drain closed early: %v", err)
				}
				select {
				case code := <-result:
					t.Fatalf("large drain returned early: %d", code)
				default:
				}
				if err = conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, err = io.WriteString(conn, body); err != nil {
					t.Fatal(err)
				}
				response, err = http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || response.StatusCode != http.StatusSeeOther {
					t.Fatalf("complete response=%v error=%v", response, err)
				}
			} else {
				_, err = reader.Peek(1)
				elapsed := time.Since(started)
				if !errors.Is(err, io.EOF) {
					t.Fatalf("cutoff did not close without response: %v", err)
				}
				if elapsed < tc.duration || elapsed >= tc.duration+time.Second {
					t.Errorf("cutoff after %v, want [%v,%v)", elapsed, tc.duration, tc.duration+time.Second)
				}
			}
			select {
			case code := <-result:
				want, diagnostic := ExitServerFailed, "dummy: stopped with 1 request unfinished\n"
				if tc.complete {
					want, diagnostic = ExitSuccess, ""
				}
				if code != want || stdout.Len() != 0 || stderr.String() != diagnostic {
					t.Errorf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				if !tc.complete && time.Since(started) >= tc.duration+time.Second {
					t.Error("Run returned too late")
				}
			case <-time.After(time.Second):
				t.Fatal("Run did not return promptly")
			}
		})
	}
}

// R-ET3H-JG91 R-QFU0-CZZ9 R-EUBD-X7ZQ
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

// R-QH1W-QRPY R-ERVL-5OIC R-EUBD-X7ZQ
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
		if code != ExitServerFailed || out.Len() != 0 || !strings.HasPrefix(diagnostics.String(), "dummy: ") || !strings.HasSuffix(diagnostics.String(), "\n") || strings.Count(diagnostics.String(), "\n") != 1 {
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

// R-PHUJ-UQB4
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
