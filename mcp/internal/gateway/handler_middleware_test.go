package gateway_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
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
	// R-YDVB-62TA
	writer, _ := handlerTelemetry(t, nil)
	h := gateway.Handler(handlerConfig(t, writer))
	reference := identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("identity gate bypassed") }))
	for _, path := range []string{"/mcp", "/mcp/", "/mcp/a", "/mcp/a,,b", "/mcp/a/b", "/mcp/../"} {
		for _, method := range []string{"GET", "POST", "HEAD", "CUSTOM"} {
			for _, headers := range []http.Header{{}, {"X-User-Id": []string{"", "later"}, "X-Request-Id": []string{"req"}}} {
				endpointSame(t, h, reference, path, method, "bogus", "", headers)
			}
		}
	}
}
func TestHandlerRequestTrail(t *testing.T) {
	// R-26U9-RVG9 R-O1E7-TCYD R-PW62-IPRR R-PXDY-WHIG R-2826-5N6Y R-2SSG-NQSR R-3HP0-MB1R
	writer, capture := handlerTelemetry(t, nil)
	h := gateway.Handler(handlerConfig(t, writer))
	cases := []struct{ path, method, user, id string }{
		{"/", "GET", "first", "provided"}, {"/", "HEAD", "", ""}, {"/", "POST", "", ""}, {"/setup.txt", "GET", "", ""}, {"/setup.sh", "GET", "person", ""}, {"/setup.sh", "HEAD", "", ""}, {"/setup.txt", "POST", "", ""}, {"/_appkit/theme.css", "GET", "person", ""}, {"/_appkit/theme.css", "GET", "", ""}, {"/unknown", "CUSTOM", "person", ""}, {"/setup.txt/x", "GET", "", ""}, {"/mcp/a,,b", "POST", "person", ""}, {"/mcp", "DELETE", "person", ""}, {"/mcp/a,,b", "POST", "", ""},
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
		if !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"status": int64(answer.Code), "duration_us": int64(0), "request_bytes": int64(0), "response_bytes": int64(answer.Body.Len())}) {
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

type handlerReadBody struct {
	io.ReadCloser
	bytes int64
}

func (b *handlerReadBody) Read(p []byte) (int, error) {
	if len(p) > 3 {
		p = p[:3]
	}
	n, err := b.ReadCloser.Read(p)
	b.bytes += int64(n)
	return n, err
}

type handlerWriteCounter struct {
	http.ResponseWriter
	bytes int64
}

func (w *handlerWriteCounter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func TestHandlerReadRequestBytes(t *testing.T) {
	// R-O1E7-TCYD R-PW62-IPRR R-PXDY-WHIG
	for _, path := range []string{"/mcp", "/mcp/missing"} {
		t.Run(path, func(t *testing.T) {
			writer, capture := handlerTelemetry(t, nil)
			h := gateway.Handler(handlerConfig(t, writer))
			type counts struct{ request, response int64 }
			completed := make(chan counts, 1)
			c := endpointClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := &handlerReadBody{ReadCloser: r.Body}
				r.Body = body
				// A declared length is not a body-byte count.
				r.ContentLength = 1
				out := &handlerWriteCounter{ResponseWriter: w}
				h.ServeHTTP(out, r)
				completed <- counts{body.bytes, out.bytes}
			}), path)
			result, err := c.CallTool(context.Background(), endpointCaller, "services", nil)
			if err != nil || result.IsError() {
				t.Fatal(result, err)
			}
			got := <-completed
			handlerFlush(t, writer)
			events := capture.Events()
			if len(events) != 3 || events[0].Name != "request.started" || events[1].Name != "tool.called" || events[2].Name != "request.finished" {
				t.Fatal(events)
			}
			if got.request <= 1 || got.response <= 0 || !reflect.DeepEqual(events[2].Attrs, telemetry.Attrs{"status": int64(200), "duration_us": int64(0), "request_bytes": got.request, "response_bytes": got.response}) {
				t.Fatal(events[2], got)
			}
		})
	}
}

type handlerShortWriter struct{ *httptest.ResponseRecorder }

func (w handlerShortWriter) Write(p []byte) (int, error) {
	if len(p) > 3 {
		_, _ = w.ResponseRecorder.Write(p[:3])
		return 3, io.ErrShortWrite
	}
	return w.ResponseRecorder.Write(p)
}

func TestHandlerUnreadAndShortResponseBytes(t *testing.T) {
	// R-O1E7-TCYD R-PW62-IPRR R-PXDY-WHIG
	for _, nilBody := range []bool{false, true} {
		t.Run(fmt.Sprint(nilBody), func(t *testing.T) {
			writer, capture := handlerTelemetry(t, nil)
			h := gateway.Handler(handlerConfig(t, writer))
			r := httptest.NewRequest("CUSTOM", "/unknown", strings.NewReader("unread body"))
			if nilBody {
				r.Body = nil
			}
			r.ContentLength = 999
			r.Header.Set("X-User-Id", "person")
			answer := httptest.NewRecorder()
			h.ServeHTTP(handlerShortWriter{answer}, r)
			handlerFlush(t, writer)
			events := capture.Events()
			if len(events) != 2 || events[0].Name != "request.started" || events[1].Name != "request.finished" || answer.Body.String() != "not" {
				t.Fatal(events, answer.Body.String())
			}
			if !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"status": int64(404), "duration_us": int64(0), "request_bytes": int64(0), "response_bytes": int64(3)}) {
				t.Fatal(events[1])
			}
		})
	}
}

type handlerCopyWriter struct {
	*httptest.ResponseRecorder
	written, copied int64
}

func (w *handlerCopyWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *handlerCopyWriter) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.Copy(w.ResponseRecorder, r)
	w.copied += n
	return n, err
}

func TestHandlerAssetResponseBytes(t *testing.T) {
	// R-O1E7-TCYD R-PW62-IPRR R-PXDY-WHIG
	writer, capture := handlerTelemetry(t, nil)
	h := gateway.Handler(handlerConfig(t, writer))
	r := httptest.NewRequest("GET", "/_appkit/theme.css", strings.NewReader("unread body"))
	r.Header.Set("X-User-Id", "person")
	answer := &handlerCopyWriter{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(answer, r)
	handlerFlush(t, writer)
	events := capture.Events()
	if len(events) != 2 || events[0].Name != "request.started" || events[1].Name != "request.finished" || answer.Code != 200 || answer.Body.Len() == 0 {
		t.Fatal(events, answer.Code, answer.Body.Len())
	}
	if !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"status": int64(200), "duration_us": int64(0), "request_bytes": int64(0), "response_bytes": answer.written + answer.copied}) {
		t.Fatal(events[1], answer.written, answer.copied)
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
