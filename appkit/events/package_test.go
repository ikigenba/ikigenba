package events_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
)

type packageTransport func(*http.Request) (*http.Response, error)

func (f packageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func exercisePackage(t *testing.T) []string {
	t.Helper()
	ctx := events.NewContext(context.Background(), events.Cause{"evt_0000000000000001", 2})
	cause, ok := events.FromContext(ctx)
	if !ok || cause.Depth != 2 {
		t.Fatal(cause, ok)
	}
	capture := new(events.Capture)
	var diagnostics bytes.Buffer
	emitter := events.New(events.Config{Service: "repos", Sink: capture, Stderr: &diagnostics, Now: func() time.Time { return record().Time }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 64)), Emits: []events.Emission{{Event: "repo.pushed", Attrs: []string{"count"}}}})
	t.Cleanup(func() { emitter.Shutdown(context.Background()) })
	emitter.Ready()
	emitter.Emit(ctx, "repo.pushed", events.Attrs{"count": 1})
	emitter.Emit(ctx, "bad", nil)
	if err := emitter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := capture.Events()
	if len(snapshot) != 1 {
		t.Fatal(snapshot)
	}
	e := snapshot[0]
	data, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded events.Event
	if err = decoded.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://repos/", nil)
	events.Forward(ctx, req)
	response := httptest.NewRecorder()
	events.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := events.FromContext(r.Context())
		if !ok || c != cause {
			t.Error(c, ok)
		}
		w.WriteHeader(204)
	})).ServeHTTP(response, req)
	declarations := httptest.NewRecorder()
	events.DeclarationsHandler(emitter, events.Handlers{"repo.pushed": func(context.Context, events.Delivery) events.Outcome { return events.OK() }}).ServeHTTP(declarations, httptest.NewRequest(http.MethodGet, "http://repos/", nil))
	incoming := httptest.NewRecorder()
	emitReq := httptest.NewRequest(http.MethodPost, "http://events/", bytes.NewReader(data))
	emitReq.Header.Set("Content-Type", "application/json")
	events.EmitHandler(capture).ServeHTTP(incoming, emitReq)
	e.Seq = 1
	e.Received = e.Time
	delivered, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	body := append(append([]byte(nil), delivered[:len(delivered)-1]...), []byte(`,"attempt":1}`)...)
	handler := events.DeliveryHandler(events.Handlers{"repo.pushed": func(c context.Context, d events.Delivery) events.Outcome {
		got, ok := events.FromContext(c)
		if !ok || got.ID != d.Event.ID {
			t.Error(got, ok)
		}
		return events.OK()
	}})
	deliveryReply := httptest.NewRecorder()
	deliveryReq := httptest.NewRequest(http.MethodPost, "http://repos/", bytes.NewReader(body))
	deliveryReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(deliveryReply, deliveryReq)
	client := &http.Client{Transport: packageTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"outcome":"ok"}`))}, nil
	})}
	result, err := events.Send(ctx, client, "repos", events.Delivery{Event: e, Attempt: 1})
	if err != nil || result.Outcome != events.OK() {
		t.Fatal(result, err)
	}
	if err = events.NewSocketSink().Deliver(ctx, events.Event{}); err == nil {
		t.Fatal("invalid socket event accepted")
	}
	for _, o := range []events.Outcome{events.OK(), events.Skip(), events.Fail("failure")} {
		_ = o.Kind()
		_ = o.Message()
	}
	emits, err := json.Marshal(emitter.Emits())
	if err != nil {
		t.Fatal(err)
	}
	emitter.Shutdown(context.Background())
	return []string{string(data), string(delivered), diagnostics.String(), string(emits), declarations.Body.String(), incoming.Body.String(), deliveryReply.Body.String()}
}

// R-H2O2-9V7Z
func TestPackageUsesOnlySuppliedWriters(t *testing.T) {
	stdout, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdout.Close() }()
	stderr, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stderr.Close() }()
	oldOut, oldErr := os.Stdout, os.Stderr
	oldLog := log.Writer()
	var logger bytes.Buffer
	os.Stdout, os.Stderr = stdout, stderr
	log.SetOutput(&logger)
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr; log.SetOutput(oldLog) }()
	exercisePackage(t)
	for _, f := range []*os.File{stdout, stderr} {
		stat, err := f.Stat()
		if err != nil || stat.Size() != 0 {
			t.Fatalf("unexpected process output %v %v", stat, err)
		}
	}
	if logger.Len() != 0 {
		t.Fatalf("unexpected default logger output %q", logger.String())
	}
}

// R-H3VY-NMYO
func TestPackageIndependentOfWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	first := exercisePackage(t)
	t.Chdir(t.TempDir())
	second := exercisePackage(t)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cwd changed behavior %q vs %q", first, second)
	}
}
