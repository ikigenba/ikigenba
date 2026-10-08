package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

// R-JEEW-O6BQ R-E80Y-6RJM R-JD70-AEL1 R-IXZD-5HBP
func TestRunTrailStartsBeforeRequestsAndStopsAfterFinishes(t *testing.T) {
	trail := testMCP(t)
	run := startRun(t, trail)
	for _, path := range []string{"/widgets", "/widgets/table"} {
		run.request(t, http.MethodGet, path, "", "trail-user", "trail-request")
	}
	run.stop(t)
	events := trail.capture.Events()
	if len(events) != 6 {
		t.Fatalf("events=%+v", events)
	}
	names := []string{"service.started", "request.started", "request.finished", "request.started", "request.finished", "service.stopping"}
	for i, name := range names {
		if events[i].Name != name {
			t.Errorf("event %d=%+v want %s", i, events[i], name)
		}
	}
	if events[5].Attrs["reason"] != "explicit run cancellation" {
		t.Errorf("stop=%+v", events[5])
	}
	if run.stderr.Len() != 0 || trail.stderr.Len() != 0 {
		t.Errorf("stderr=%q telemetry=%q", run.stderr.String(), trail.stderr.String())
	}
}

// R-3H2J-QU7G
func TestRunUsesInjectedWidgetIDBytes(t *testing.T) {
	trail := testMCP(t)
	source := bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23})
	run := startConfiguredRun(t, trail, func(p *Process) { p.Rand = source })
	runTool(t, run, "create_widget", json.RawMessage(`{"name":"injected","count":1,"status":"active"}`))
	data, err := runTool(t, run, "list_widgets", nil).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		StructuredContent struct {
			Widgets []struct{ ID, Name string } `json:"widgets"`
		} `json:"structuredContent"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	want := []string{"wgt_0001020304050607"}
	var ids []string
	for _, w := range result.StructuredContent.Widgets {
		ids = append(ids, w.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("ids=%v", ids)
	}
	run.stop(t)
	nilRun := startConfiguredRun(t, testMCP(t), func(p *Process) { p.Rand = nil })
	runTool(t, nilRun, "create_widget", json.RawMessage(`{"name":"random","count":1,"status":"active"}`))
	nilRun.stop(t)
	if trail.stderr.Len() != 0 {
		t.Errorf("telemetry stderr=%q", trail.stderr.String())
	}
}

// R-JFMT-1Y2F R-IXZD-5HBP
func TestRunRefusedStartsAndCommandsLeaveTelemetryUntouched(t *testing.T) {
	cases := []Process{
		{Args: []string{"--version"}}, {Args: []string{"manifest"}}, {Args: []string{"--help"}}, {Args: []string{"bad"}},
		{LookupEnv: mapLookup(map[string]string{"DRAIN_SECONDS": "bad"})},
		{},
		{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "2"})},
		{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1"}), Inherit: func(uintptr) (net.Listener, error) { return nil, errors.New("inherit failed") }},
		{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": "/nonexistent-dummy-notify"}), Inherit: func(uintptr) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }},
	}
	for i, p := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			for _, withWriter := range []bool{false, true} {
				p.Dir, p.Now, p.Version = t.TempDir(), testNow, testVersion
				p.Stdout, p.Stderr = io.Discard, io.Discard
				if !withWriter {
					p.Telemetry = nil
					Run(context.Background(), p)
					continue
				}
				trail := testMCP(t)
				p.Telemetry = trail.writer
				Run(context.Background(), p)
				if err := trail.writer.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				if got := trail.capture.Events(); len(got) != 0 {
					t.Fatalf("refused start events=%+v", got)
				}
				trail.writer.Ready()
				if err := trail.writer.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				events := trail.capture.Events()
				if len(events) != 1 || events[0].Name != "service.started" {
					t.Errorf("Ready after refused start=%+v", events)
				}
			}
		})
	}
}

type deadlineSink struct {
	block   bool
	capture telemetry.Capture
}

func (s *deadlineSink) Deliver(ctx context.Context, e telemetry.Event) error {
	if s.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.capture.Deliver(ctx, e)
}

// R-JAR7-IV3N
func TestRunShutdownWithBlockedSinkRespectsDrain(t *testing.T) {
	sink := &deadlineSink{block: true}
	gate := NewGate(sink)
	trail := testMCP(t)
	trail.writer.Shutdown(context.Background(), "replace fixture writer")
	trail.writer = telemetry.New(telemetry.Config{Service: panel.ServiceName, Version: testVersion, Sink: gate, Stderr: &trail.stderr, Now: func() time.Time { return time.Unix(123, 0) }, Sleep: func(context.Context, time.Duration) {}})
	trail.server = mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Version: testVersion, Telemetry: trail.writer})
	run := startConfiguredRun(t, trail, func(p *Process) {
		p.Gate = gate
		old := p.LookupEnv
		p.LookupEnv = func(key string) (string, bool) {
			if key == "DRAIN_SECONDS" {
				return "1", true
			}
			return old(key)
		}
	})
	started := time.Now()
	run.cancel()
	select {
	case code := <-run.result:
		if code != ExitSuccess {
			t.Errorf("exit=%d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked sink outlasted drain")
	}
	if time.Since(started) >= 2*time.Second {
		t.Error("shutdown exceeded bound")
	}
	if gate.Deliver(context.Background(), telemetry.Event{Name: "after.return"}) == nil {
		t.Error("gate delivered after blocked shutdown")
	}
	if run.stderr.Len() != 0 || run.stdout.Len() != 0 {
		t.Errorf("streams stderr=%q stdout=%q", run.stderr.String(), run.stdout.String())
	}
	if !strings.Contains(trail.stderr.String(), `"event":"service.stopping"`) {
		t.Errorf("telemetry stderr=%q", trail.stderr.String())
	}
}

type orderedLog struct {
	mu                sync.Mutex
	lines             []string
	gate              *Gate
	refusedDuringStop bool
}

func (l *orderedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.gate != nil && strings.Contains(string(p), `"event":"service.stopping"`) {
		l.refusedDuringStop = l.gate.Deliver(context.Background(), telemetry.Event{Name: "during.stop"}) != nil
	}
	l.lines = append(l.lines, string(p))
	return len(p), nil
}
func (l *orderedLog) record(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
}
func (l *orderedLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

type closeRecordingListener struct {
	net.Listener
	log *orderedLog
}

func (l *closeRecordingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &closeRecordingConn{Conn: c, log: l.log}, nil
}

type closeRecordingConn struct {
	net.Conn
	log *orderedLog
}

func (c *closeRecordingConn) Close() error { c.log.record("connection closed"); return c.Conn.Close() }
