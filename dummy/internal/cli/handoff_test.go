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
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

type testTrail struct {
	server  *mcp.Server
	writer  *telemetry.Writer
	capture *telemetry.Capture
	stderr  bytes.Buffer
}

const testVersion = "test display"

func testMCP(t *testing.T) *testTrail {
	t.Helper()
	t.Setenv(services.Variable, "")
	trail := &testTrail{capture: &telemetry.Capture{}}
	trail.writer = telemetry.New(telemetry.Config{Service: panel.ServiceName, Version: testVersion, Sink: trail.capture, Stderr: &trail.stderr, Now: func() time.Time { return time.Unix(123, 0) }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 8192))})
	trail.server = mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Version: testVersion, Telemetry: trail.writer})
	t.Cleanup(func() { trail.writer.Shutdown(context.Background(), "test finished") })
	return trail
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

// R-PE6U-PF31 R-PFER-36TQ R-HUGI-VVDH R-IWRG-RPL0 R-IXZD-5HBP
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
		var ordering *orderedLog
		var sink *deadlineSink
		var gate *Gate
		if tc.name == "one" {
			ordering = &orderedLog{}
			sink = &deadlineSink{}
			gate = NewGate(sink)
			ordering.gate = gate
			srv.writer.Shutdown(context.Background(), "replace fixture writer")
			srv.writer = telemetry.New(telemetry.Config{Service: panel.ServiceName, Version: testVersion, Sink: gate, Stderr: ordering, Now: func() time.Time { return time.Unix(123, 0) }, Sleep: func(context.Context, time.Duration) {}})
			srv.server = mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Version: testVersion, Telemetry: srv.writer})
		}
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
			var processStderr io.Writer = &stderr
			inherited := listener
			if ordering != nil {
				processStderr = ordering
				inherited = &closeRecordingListener{Listener: listener, log: ordering}
			}
			result := make(chan int, 1)
			go func() {
				result <- Run(ctx, Process{Version: testVersion, Dir: t.TempDir(), Now: testNow, Pid: 42, LookupEnv: mapLookup(env), Inherit: func(uintptr) (net.Listener, error) { return inherited, nil }, Banner: emptyBanner, MCP: srv.server, Telemetry: srv.writer, Gate: gate, Rand: testWidgetRand(), Stdout: &stdout, Stderr: processStderr})
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
			header := fmt.Sprintf("POST /widgets HTTP/1.1\r\nHost: dummy\r\nX-User-Id: test-user\r\nX-Request-Id: cut-off\r\nContent-Type: application/x-www-form-urlencoded\r\nExpect: 100-continue\r\nContent-Length: %d\r\n\r\n", len(body))
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
				if ordering != nil {
					before := len(sink.capture.Events())
					if gate.Deliver(context.Background(), telemetry.Event{Name: "after.return"}) == nil || len(sink.capture.Events()) != before {
						t.Error("gate delivered after Run returned")
					}
					if !ordering.refusedDuringStop {
						t.Error("gate did not refuse during service.stopping write")
					}
					lines := ordering.snapshot()
					stop, closed, overrun := -1, -1, -1
					for i, line := range lines {
						if strings.Contains(line, `"event":"service.stopping"`) {
							stop = i
							if !strings.Contains(line, `"reason":"context canceled"`) {
								t.Errorf("stop line=%q", line)
							}
						}
						if line == "connection closed" && closed < 0 {
							closed = i
						}
						if line == diagnostic {
							overrun = i
						}
					}
					if stop < 0 || closed <= stop || overrun <= stop {
						t.Errorf("shutdown ordering=%q", lines)
					}
					for _, event := range sink.capture.Events() {
						if event.Name == "service.stopping" || (event.RequestID == "cut-off" && event.Name == "request.finished") {
							t.Errorf("delivered cutoff event=%+v", event)
						}
					}
					diagnostic = ""
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

// R-QFU0-CZZ9
func TestRunNoRequestsAndNoNotification(t *testing.T) {
	for _, env := range []map[string]string{{}, {"NOTIFY_SOCKET": ""}} {
		srv := testMCP(t)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ln = &cancelOnAccept{Listener: ln, cancel: cancel}
		env["LISTEN_PID"], env["LISTEN_FDS"] = "42", "1"
		calls := 0
		var out, diagnostics bytes.Buffer
		code := Run(ctx, Process{Version: testVersion, Dir: t.TempDir(), Now: testNow, Pid: 42, LookupEnv: mapLookup(env), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: func(page.User) page.Banner { calls++; return page.Banner{} }, MCP: srv.server, Telemetry: srv.writer, Rand: testWidgetRand(), Stdout: &out, Stderr: &diagnostics})
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

// R-JBZ3-WMUC R-ERVL-5OIC
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
		ln := &observingListener{err: errors.New("first\nsecond\rthird")}
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
		code := Run(context.Background(), Process{Version: testVersion, Dir: t.TempDir(), Now: testNow, Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: emptyBanner, MCP: srv.server, Telemetry: srv.writer, Rand: testWidgetRand(), Stdout: &out, Stderr: &diagnostics})
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
	code := Run(context.Background(), Process{Version: testVersion, Dir: t.TempDir(), Now: testNow, Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: emptyBanner, MCP: srv.server, Telemetry: srv.writer, Rand: testWidgetRand(), Stdout: &out, Stderr: &diagnostics})
	if code != ExitServerFailed || out.Len() != 0 || diagnostics.String() != "dummy: "+wantErr.Error()+"\n" || ln.accepts != 0 {
		t.Errorf("code=%d out=%q diagnostic=%q accepts=%d", code, out.String(), diagnostics.String(), ln.accepts)
	}
}

type cancelOnAccept struct {
	net.Listener
	cancel context.CancelFunc
}

func (l *cancelOnAccept) Accept() (net.Conn, error) { l.cancel(); return l.Listener.Accept() }

func (l *cancelOnAccept) Close() error {
	err := l.Listener.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
