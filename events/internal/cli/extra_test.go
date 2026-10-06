package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/cli"
)

type refusingSink struct{}

func (s refusingSink) Deliver(context.Context, telemetry.Event) error {
	return errors.New("sink refused")
}

// R-IKPT-SS28 R-98YL-W8I0
func TestWriterPauseInjection(t *testing.T) {
	t.Setenv(services.Variable, "")
	dir := t.TempDir()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pauses := make(chan time.Duration, 8)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	var out, errw bytes.Buffer
	done := make(chan int, 1)
	p := cli.Process{Pid: 12, Dir: dir, Stdout: &out, Stderr: &errw, Sink: refusingSink{}, Rand: repeatByte(0x5a), LookupEnv: func(k string) (string, bool) {
		switch k {
		case "LISTEN_PID":
			return "12", true
		case "LISTEN_FDS":
			return "1", true
		case "DRAIN_SECONDS":
			return "1", true
		}
		return "", false
	}, Inherit: func(uintptr) (net.Listener, error) { return l, nil }, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, Sleep: func(_ context.Context, d time.Duration) { pauses <- d }, SweepAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, RefreshAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
	go func() { done <- cli.Run(ctx, p) }()
	select {
	case d := <-pauses:
		if d != telemetry.RetryBackoff {
			t.Fatal(d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer never called injected Sleep")
	}
	cancel(errors.New("SIGTERM"))
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop")
	}
	if out.Len() != 0 {
		t.Fatal(out.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(errw.String()), "\n") {
		if !strings.HasPrefix(line, "events: undelivered event: {") {
			t.Fatal(line)
		}
	}
}

type failListener struct{ net.Listener }

func (f failListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }

// R-9MDI-3PNN
func TestServingFailureDiagnostic(t *testing.T) {
	t.Setenv(services.Variable, "")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	var errw writes
	code := cli.Run(context.Background(), cli.Process{Pid: 12, Dir: t.TempDir(), Stdout: &out, Stderr: &errw, Sink: &telemetry.Capture{}, Rand: repeatByte(0x5a), LookupEnv: func(k string) (string, bool) {
		if k == "LISTEN_PID" {
			return "12", true
		}
		if k == "LISTEN_FDS" {
			return "1", true
		}
		return "", false
	}, Inherit: func(uintptr) (net.Listener, error) { return failListener{l}, nil }, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, SweepAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, RefreshAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	if code != 1 || out.Len() != 0 || errw.Calls != 1 || errw.String() != "events: accept failed\n" {
		t.Fatal(code, out.String(), errw.String(), errw.Calls)
	}
}

// R-AZ4W-FGNR
func TestRunDoesNotUseGlobalLogger(t *testing.T) {
	var global bytes.Buffer
	prior := log.Writer()
	log.SetOutput(&global)
	defer log.SetOutput(prior)
	f := startRun(t, t.TempDir(), map[string]string{})
	for _, path := range []string{"/", "/nope"} {
		r := f.request(t, "GET", path, "", "logger")
		_ = body(t, r)
	}
	_, _ = f.mcp().ListTools(context.Background(), identity.Caller{UserID: "user"})
	emit(t, f, emitted(f, 1, 0), 422)
	f.stop(t)
	if global.Len() != 0 {
		t.Fatal(global.String())
	}
}

// R-9L5L-PXWY R-E0E3-6NDU
func TestRunCutsOffIncompleteEmit(t *testing.T) {
	dir := t.TempDir()
	f := startRun(t, dir, map[string]string{"DRAIN_SECONDS": "1"})
	conn, err := f.client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	// The request enters EmitHandler but the body stays incomplete through the drain.
	_, err = io.WriteString(conn, "POST /emit HTTP/1.1\r\nHost: events.test\r\nContent-Type: application/json\r\nContent-Length: 200\r\nExpect: 100-continue\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	status, err := reader.ReadString('\n')
	if err != nil || status != "HTTP/1.1 100 Continue\r\n" {
		t.Fatal(status, err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "{"); err != nil {
		t.Fatal(err)
	}
	f.cancel(errors.New("SIGTERM"))
	select {
	case code := <-f.done:
		if code != 1 || !strings.HasSuffix(f.errw.String(), "events: stopped with 1 request unfinished\n") {
			t.Fatal(code, f.errw.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("drain did not finish")
	}
	f.client.CloseIdleConnections()
	entries, err := os.ReadDir(filepath.Join(dir, "state"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "events.db" {
		t.Fatal(entries, err)
	}
}
