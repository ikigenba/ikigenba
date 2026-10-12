package gmail_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
)

func captureWriter(t *testing.T, negative bool) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	sink := &telemetry.Capture{}
	var mu sync.Mutex
	i := 0
	w := telemetry.New(telemetry.Config{Service: "gmail", Sink: sink, Stderr: io.Discard, Rand: bytes.NewReader(make([]byte, 4096)), Sleep: func(context.Context, time.Duration) {}, Now: func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		n := i
		i++
		if negative && n%3 == 1 {
			n -= 2
		}
		return time.Unix(100, 0).Add(time.Duration(n) * 10 * time.Microsecond)
	}})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		w.Shutdown(ctx, "synthetic test complete")
	})
	return w, sink
}

func flushWriter(t *testing.T, w *telemetry.Writer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRequestEvents(t *testing.T) {
	// R-ID7S-JLFP R-5SK0-T98O R-GN0X-1AMA R-GO8T-F2CZ
	aid := "current-attachment"
	responses := []reply{
		labelsJSON(),
		labelsJSON(), okJSON(`{"messages":[{"id":"message"}]}`), okJSON(messageJSON("message", 1)),
		labelsJSON(), okJSON(`{"messages":[]}`),
		labelsJSON(), okJSON(messageJSON("message", 1)),
		okJSON(`{"id":"message"}`), okJSON(`{}`),
		okJSON(attachmentJSON(4, nil, &aid)), okJSON(`{"data":"ZGF0YQ=="}`), okJSON(`{"raw":"ZGF0YQ=="}`),
		okJSON(draftResponse), okJSON(draftResponse),
		{}, okJSON(draftResponse), okJSON(`{"id":"sent"}`), {status: 204},
	}
	f, _ := newFake(t, responses...)
	f.replies[15] = reply{headers: http.Header{"Location": {f.server.URL + "/session"}}}
	w, sink := captureWriter(t, false)
	c := gmail.NewClient(gmail.Config{HTTP: f.server.Client(), BaseURL: f.server.URL, Telemetry: w})
	ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "synthetic-user", RequestID: "synthetic-request"})
	var errs []error
	_, err := c.Labels(ctx, syntheticToken)
	errs = append(errs, err)
	_, err = c.Search(ctx, syntheticToken, gmail.Query{})
	errs = append(errs, err)
	_, err = c.Thread(ctx, syntheticToken, "thread", "")
	errs = append(errs, err)
	_, err = c.Show(ctx, syntheticToken, "message")
	errs = append(errs, err)
	_, err = c.Stat(ctx, syntheticToken, "message")
	errs = append(errs, err)
	_, err = c.ThreadStats(ctx, syntheticToken, "thread")
	errs = append(errs, err)
	_, err = c.Attachment(ctx, syntheticToken, "message", "1", 4)
	errs = append(errs, err)
	_, err = c.Raw(ctx, syntheticToken, "message", 4)
	errs = append(errs, err)
	_, err = c.CreateDraft(ctx, syntheticToken, "", gmail.Build(composition()))
	errs = append(errs, err)
	_, err = c.GetDraft(ctx, syntheticToken, "draft")
	errs = append(errs, err)
	_, err = c.UpdateDraft(ctx, syntheticToken, "draft", "", gmail.Build(composition()))
	errs = append(errs, err)
	_, err = c.SendDraft(ctx, syntheticToken, "draft")
	errs = append(errs, err)
	err = c.DeleteDraft(ctx, syntheticToken, "draft")
	errs = append(errs, err)
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	flushWriter(t, w)
	expected := []string{gmail.EndpointLabels, gmail.EndpointLabels, gmail.EndpointList, gmail.EndpointGet, gmail.EndpointLabels, gmail.EndpointThread, gmail.EndpointLabels, gmail.EndpointGet, gmail.EndpointGet, gmail.EndpointThread, gmail.EndpointGet, gmail.EndpointAttachment, gmail.EndpointGet, gmail.EndpointCreate, gmail.EndpointDraft, gmail.EndpointUpdate, gmail.EndpointChunk, gmail.EndpointSend, gmail.EndpointDelete}
	events := sink.Events()
	if len(events) != len(expected) || len(f.calls()) != len(expected) {
		t.Fatal(len(events), len(f.calls()))
	}
	for i, e := range events {
		want := telemetry.Attrs{"endpoint": expected[i], "status": int64(200), "duration_us": int64(10)}
		if i == len(expected)-1 {
			want["status"] = int64(204)
		}
		if e.Name != gmail.Called || e.User != "synthetic-user" || e.RequestID != "synthetic-request" || !reflect.DeepEqual(e.Attrs, want) {
			t.Fatal(i, e, want)
		}
		call := f.calls()[i]
		if expected[i] == gmail.EndpointChunk {
			if call.headers.Get("Authorization") != "" {
				t.Fatal(call.headers)
			}
		} else if call.headers.Get("Authorization") != "Bearer "+syntheticToken {
			t.Fatal(call.headers)
		}
	}
}

func TestFailureEventsAndSecrecy(t *testing.T) {
	// R-ID7S-JLFP R-2BVJ-C5DD
	secret := "synthetic-sensitive-input"
	for _, status := range []int{0, 401, 403, 500} {
		w, sink := captureWriter(t, true)
		c := gmail.NewClient(gmail.Config{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			if status == 0 {
				return nil, errors.New(secret + syntheticToken)
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"` + secret + `"}}`)), Header: make(http.Header)}, nil
		})}, Telemetry: w})
		_, err := c.Search(context.Background(), syntheticToken, gmail.Query{Text: secret, Label: secret})
		if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), syntheticToken) {
			t.Fatal(err)
		}
		flushWriter(t, w)
		events := sink.Events()
		if len(events) != 1 || events[0].Name != gmail.Called || events[0].RequestID != "" || events[0].User != "" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"endpoint": gmail.EndpointLabels, "status": int64(status), "duration_us": int64(0)}) {
			t.Fatal(events)
		}
	}
	// Session URLs are credentials too, even when their transport rejects a piece.
	f, _ := newFake(t, reply{})
	location := f.server.URL + "/credential-session?secret=" + secret
	f.replies[0] = reply{headers: http.Header{"Location": {location}}}
	w, sink := captureWriter(t, false)
	hc := f.server.Client()
	base := hc.Transport
	hc.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/credential-session" {
			return nil, errors.New(location)
		}
		return base.RoundTrip(r)
	})
	c := gmail.NewClient(gmail.Config{HTTP: hc, BaseURL: f.server.URL, Telemetry: w})
	_, err := c.UpdateDraft(context.Background(), syntheticToken, "draft", "", gmail.Build(composition()))
	requireError(t, err, gmail.ErrProvider)
	if strings.Contains(err.Error(), location) || strings.Contains(err.Error(), secret) {
		t.Fatal(err)
	}
	flushWriter(t, w)
	for _, e := range sink.Events() {
		for _, a := range e.Attrs {
			if s, ok := a.(string); ok && (strings.Contains(s, secret) || strings.Contains(s, location)) {
				t.Fatal(e)
			}
		}
	}
	// Gmail's own JSON diagnostic or malformed success body never reaches an error.
	for _, in := range invocations() {
		if in.name == "delete" {
			continue
		}
		_, c := newFake(t, append(append([]reply(nil), in.pre...), okJSON(`{"id":7,"message":"`+secret+`","raw":"`+secret+`","labels":"`+secret+`","messages":"`+secret+`"}`))...)
		_, err := in.call(c, context.Background())
		if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), syntheticToken) {
			t.Fatal(in.name, err)
		}
	}
}
