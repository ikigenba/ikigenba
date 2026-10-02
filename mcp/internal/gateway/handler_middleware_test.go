package gateway_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func handlerTelemetry(t testing.TB, sink telemetry.Sink) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	var capture *telemetry.Capture
	if sink == nil {
		capture = &telemetry.Capture{}
		sink = capture
	}
	now := func() time.Time { return time.Unix(123, 0) }
	writer := telemetry.New(telemetry.Config{Service: gateway.ServiceName, Version: "test", Sink: sink, Stderr: io.Discard, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0xab}, 4096))})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		writer.Shutdown(ctx, "test ended")
	})
	return writer, capture
}
func handlerFlush(t testing.TB, writer *telemetry.Writer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := writer.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func handlerConfig(t *testing.T, writer *telemetry.Writer) gateway.Config {
	t.Helper()
	t.Setenv(services.Variable, "")
	// R-1G0H-CX4Z
	return gateway.Config{Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: gateway.NewServer("test", writer), ServicesPath: "", Budget: time.Second, Telemetry: writer}
}
func TestHandlerIdentity(t *testing.T) {
	// R-1TFD-KEAM
	writer, _ := handlerTelemetry(t, nil)
	h := gateway.Handler(handlerConfig(t, writer))
	reference := identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("identity gate bypassed") }))
	for _, path := range []string{"/", "/mcp", "/mcp/a,,b", "/_appkit/theme.css", "/unknown"} {
		for _, method := range []string{"GET", "POST", "HEAD", "CUSTOM"} {
			for _, headers := range []http.Header{{}, {"X-User-Id": []string{"", "later"}, "X-Request-Id": []string{"req"}}} {
				endpointSame(t, h, reference, path, method, "bogus", "", headers)
			}
		}
	}
}
func TestHandlerRequestTrail(t *testing.T) {
	// R-26U9-RVG9 R-JV6I-P3SI R-2826-5N6Y R-2SSG-NQSR R-3HP0-MB1R
	writer, capture := handlerTelemetry(t, nil)
	h := gateway.Handler(handlerConfig(t, writer))
	cases := []struct{ path, method, user, id string }{
		{"/", "GET", "first", "provided"}, {"/", "HEAD", "", ""}, {"/_appkit/theme.css", "GET", "person", ""}, {"/unknown", "CUSTOM", "person", ""}, {"/mcp/a,,b", "POST", "person", ""}, {"/mcp", "DELETE", "person", ""},
	}
	for _, tc := range cases {
		before := len(capture.Events())
		headers := http.Header{"X-User-Id": []string{tc.user, "second"}, "X-Request-Id": []string{tc.id, "later"}}
		answer := endpointRaw(h, tc.path, tc.method, "bogus", "", headers)
		handlerFlush(t, writer)
		events := capture.Events()[before:]
		if len(events) != 2 || events[0].Name != "request.started" || events[1].Name != "request.finished" {
			t.Fatal(events)
		}
		if !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": tc.method, "path": tc.path}) {
			t.Fatal(events[0])
		}
		if len(events[1].Attrs) != 2 || events[1].Attrs["status"] != int64(answer.Code) {
			t.Fatal(events[1], answer.Code)
		}
		if _, ok := events[1].Attrs["duration_us"].(int64); !ok {
			t.Fatal(events[1])
		}
		id := events[0].RequestID
		if tc.id != "" {
			if id != tc.id {
				t.Fatal(id)
			}
		} else if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
			t.Fatal(id)
		}
		for _, event := range events {
			if event.RequestID != id || event.User != tc.user || event.Service != gateway.ServiceName {
				t.Fatal(event)
			}
		}
	}
}
func TestHandlerConcurrent(t *testing.T) {
	// R-1UN9-Y61B
	writer, capture := handlerTelemetry(t, nil)
	h := gateway.Handler(handlerConfig(t, writer))
	c := endpointClient(t, h, "/mcp")
	var group sync.WaitGroup
	for i := 0; i < 40; i++ {
		group.Go(func() {
			result, err := c.CallTool(context.Background(), endpointCaller, "services", nil)
			if err != nil || result.IsError() {
				t.Errorf("call: %v %v", result, err)
			}
		})
	}
	group.Wait()
	handlerFlush(t, writer)
	if len(capture.Events()) != 120 {
		t.Fatal(len(capture.Events()))
	}
}

type handlerSink func(context.Context, telemetry.Event) error

func (s handlerSink) Deliver(ctx context.Context, e telemetry.Event) error { return s(ctx, e) }
func TestHandlerAnswersWithoutDelivery(t *testing.T) {
	// R-2U0D-1IJG
	baselineWriter, _ := handlerTelemetry(t, nil)
	baseline := gateway.Handler(handlerConfig(t, baselineWriter))
	for _, mode := range []string{"blocked", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			release := make(chan struct{})
			defer close(release)
			sink := handlerSink(func(ctx context.Context, _ telemetry.Event) error {
				if mode == "rejected" {
					return fmt.Errorf("reject: %w", telemetry.ErrRejected)
				}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			writer, _ := handlerTelemetry(t, sink)
			h := gateway.Handler(handlerConfig(t, writer))
			done := make(chan struct{})
			go func() {
				defer close(done)
				for _, tc := range []struct{ path, method, user string }{{"/", "GET", "person"}, {"/_appkit/theme.css", "GET", "person"}, {"/unknown", "GET", "person"}, {"/mcp", "GET", "person"}, {"/mcp", "POST", ""}} {
					endpointSame(t, h, baseline, tc.path, tc.method, "bogus", "", http.Header{"X-User-Id": []string{tc.user}})
				}
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("answer waited on sink")
			}
		})
	}
}
