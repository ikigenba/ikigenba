package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

type gateSinkFunc func(context.Context, telemetry.Event) error

func (f gateSinkFunc) Deliver(ctx context.Context, e telemetry.Event) error { return f(ctx, e) }

// R-HS0Q-4BW3 R-HT8M-I3MS
func TestGateForwardsExactlyOnceWithContextEventAndError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	want := errors.New("next sink failed")
	event := telemetry.Event{Name: "gate.probe", Attrs: telemetry.Attrs{"value": "kept"}}
	calls := 0
	var sink telemetry.Sink = NewGate(gateSinkFunc(func(gotCtx context.Context, got telemetry.Event) error {
		calls++
		if gotCtx != ctx || !reflect.DeepEqual(got, event) {
			t.Errorf("forwarded context or event changed: %+v", got)
		}
		return want
	}))
	if err := sink.Deliver(ctx, event); !errors.Is(err, want) || !reflect.DeepEqual(err, want) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	cancel()
	if err := sink.Deliver(ctx, event); !errors.Is(err, want) || !reflect.DeepEqual(err, want) || calls != 2 {
		t.Fatalf("cancelled context: err=%v calls=%d", err, calls)
	}
}

// R-HT8M-I3MS R-2WC9-8QLN
func TestGatePassesThroughAfterCancellationBeforeDrainDeadline(t *testing.T) {
	trail := testMCP(t)
	trail.writer.Shutdown(context.Background(), "replace fixture writer")
	trail.capture = &telemetry.Capture{}
	gate := NewGate(trail.capture)
	trail.writer = telemetry.New(telemetry.Config{Service: panel.ServiceName, Version: Version, Sink: gate, Stderr: &trail.stderr, Now: func() time.Time { return time.Unix(123, 0) }, Sleep: func(context.Context, time.Duration) {}})
	trail.server = mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Version: Version, Telemetry: trail.writer})
	run := startConfiguredRun(t, trail, func(p *Process) { p.Gate = gate })
	conn, err := net.Dial("tcp", strings.TrimPrefix(run.endpoint, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err = conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	body := "name=finished&count=1&status=active"
	_, err = fmt.Fprintf(conn, "POST /widgets HTTP/1.1\r\nHost: dummy\r\nX-User-Id: gate-user\r\nX-Request-Id: gate-request\r\nContent-Type: application/x-www-form-urlencoded\r\nExpect: 100-continue\r\nContent-Length: %d\r\n\r\n", len(body))
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
	if err != nil || response.StatusCode != http.StatusContinue {
		t.Fatalf("continue=%v err=%v", response, err)
	}
	_ = response.Body.Close()
	run.cancel()
	probe := telemetry.Event{Name: "before.deadline"}
	if err = gate.Deliver(context.Background(), probe); err != nil {
		t.Fatalf("early delivery refused: %v", err)
	}
	_, err = io.WriteString(conn, body)
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusSeeOther {
		t.Fatalf("response=%v err=%v", response, err)
	}
	select {
	case code := <-run.result:
		if code != ExitSuccess {
			t.Fatalf("exit=%d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completed request failed to stop promptly")
	}
	events := trail.capture.Events()
	probes, stops := 0, 0
	for _, e := range events {
		if e.Name == probe.Name {
			probes++
		}
		if e.Name == "service.stopping" {
			stops++
		}
	}
	if probes != 1 || stops != 1 || events[len(events)-1].Name != "service.stopping" {
		t.Errorf("events=%+v", events)
	}
	if trail.stderr.Len() != 0 || run.stderr.Len() != 0 || run.stdout.Len() != 0 {
		t.Errorf("streams=%q %q %q", trail.stderr.String(), run.stderr.String(), run.stdout.String())
	}
}
