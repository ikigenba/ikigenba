package smarthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
)

func TestEmitsIndependentDeclarations(t *testing.T) {
	// R-D8DS-9HMQ R-AF8V-QN7T
	declare := smarthttp.Emits
	want := []events.Emission{{Event: "repo.pushed", Attrs: []string{"repo", "ref", "old", "new"}}}
	var first, second []events.Emission
	first, second = declare(), declare()
	same(t, first, want)
	first[0].Attrs[0] = "changed"
	first[0].Event = "changed.event"
	same(t, second, want)
	same(t, declare(), want)
	second[0] = events.Emission{Event: "other.event", Attrs: []string{"other"}}
	same(t, declare(), want)
}

func assertBusMatches(f *fixture) {
	f.t.Helper()
	trail := f.events()
	must(f.t, f.bus.Flush(deadline(f.t)))
	want := make(map[string]events.Attrs)
	for _, e := range trail {
		if e.Name == "repo.pushed" {
			key := e.RequestID + "/" + e.Attrs["ref"].(string) + "/" + e.Attrs["old"].(string) + "/" + e.Attrs["new"].(string)
			if _, duplicate := want[key]; duplicate {
				f.t.Fatalf("duplicate trail push %s", key)
			}
			want[key] = events.Attrs(e.Attrs)
		}
	}
	seen := make(map[string]events.Attrs)
	for _, e := range f.busCapture.Events() {
		same(f.t, e.Name, "repo.pushed")
		key := e.RequestID + "/" + e.Attrs["ref"].(string) + "/" + e.Attrs["old"].(string) + "/" + e.Attrs["new"].(string)
		if _, duplicate := seen[key]; duplicate {
			f.t.Fatalf("duplicate bus push %s", key)
		}
		seen[key] = e.Attrs
	}
	same(f.t, seen, want)
}

func TestPushBusEnvelope(t *testing.T) {
	// R-D75V-VPW1 R-EDUD-9OUC R-ECMG-VX3N
	for _, cause := range []*events.Cause{nil, {ID: "evt_0123456789abcdef", Depth: 0}, {ID: "evt_fedcba9876543210", Depth: 1000000}} {
		t.Run(causeName(cause), func(t *testing.T) {
			f := setup(t)
			f.create("notes")
			work := f.working("source")
			f.commit(work, "envelope")
			f.gitRun(work, "tag", "mark")
			handler := f.handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if cause != nil {
					r = r.WithContext(events.NewContext(r.Context(), *cause))
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			f.client(work, "push", server.URL+"/notes.git", "main", "mark")
			assertBusMatches(f)
			captured := f.busCapture.Events()
			same(t, len(captured), 2)
			for _, e := range captured {
				same(t, e.RequestID, "request")
				same(t, e.User, "alice")
				if cause == nil {
					same(t, e.Cause, "")
					same(t, e.Depth, 0)
				} else {
					same(t, e.Cause, cause.ID)
					same(t, e.Depth, cause.Depth+1)
				}
			}
			// No-change push, malformed fetch and advertisements emit nothing else.
			f.client(work, "push", server.URL+"/notes.git", "main", "mark")
			f.request("POST", "/notes.git/git-receive-pack", strings.NewReader("0000"))
			f.request("POST", "/notes.git/git-upload-pack", strings.NewReader("invalid"))
			f.request("GET", "/notes.git/info/refs?service=git-upload-pack", nil)
			assertBusMatches(f)
			same(t, len(f.busCapture.Events()), 2)
		})
	}
}

func causeName(c *events.Cause) string {
	if c == nil {
		return "no-cause"
	}
	return c.ID
}

type rejectedBus struct{ capture *events.Capture }

func (s rejectedBus) Deliver(ctx context.Context, e events.Event) error {
	_ = s.capture.Deliver(ctx, e)
	return events.ErrRejected
}

func TestPushBusRejectedSinkStillEmitsOnce(t *testing.T) {
	// R-ECMG-VX3N
	f := setup(t)
	f.bus.Shutdown(deadline(t))
	f.bus = events.New(events.Config{Service: "repos", Emits: smarthttp.Emits(), Sink: rejectedBus{f.busCapture}, Now: func() time.Time { return epoch }, Rand: new(sequence), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) { t.Error("rejected delivery slept") }})
	t.Cleanup(func() { f.bus.Shutdown(deadline(t)) })
	f.create("notes")
	work := f.working("source")
	f.commit(work, "rejected delivery")
	f.gitRun(work, "tag", "mark")
	f.client(work, "push", f.server().URL+"/notes.git", "main", "mark")
	assertBusMatches(f)
	same(t, len(f.busCapture.Events()), 2)
}

func TestPushBusDroppedOrMalformedStillEmitsOnce(t *testing.T) {
	// R-ECMG-VX3N R-EDUD-9OUC
	for _, ending := range []string{"dropped", "malformed"} {
		t.Run(ending, func(t *testing.T) {
			f := setup(t)
			f.bus.Shutdown(deadline(t))
			var calls atomic.Int64
			var diagnostic bytes.Buffer
			f.bus = events.New(events.Config{Service: "repos", Emits: smarthttp.Emits(), Sink: f.busCapture, Now: func() time.Time { calls.Add(1); return epoch }, Rand: new(sequence), Stderr: &diagnostic, Sleep: func(context.Context, time.Duration) { t.Error("undelivered event slept") }})
			t.Cleanup(func() { f.bus.Shutdown(deadline(t)) })
			if ending == "dropped" {
				f.bus.Shutdown(deadline(t))
			}
			f.create("notes")
			work := f.working("source")
			f.commit(work, "undelivered")
			f.gitRun(work, "tag", "mark")
			handler := f.handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if ending == "malformed" {
					r = r.WithContext(events.NewContext(r.Context(), events.Cause{ID: "invalid", Depth: 7}))
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			f.client(work, "push", server.URL+"/notes.git", "main", "mark")
			// Neither rejected shape reaches delivery; each clock call is an Emit.
			same(t, calls.Load(), int64(2))
			same(t, len(f.busCapture.Events()), 0)
			want := make(map[string]any)
			for _, e := range f.events() {
				if e.Name == "repo.pushed" {
					want[e.Attrs["ref"].(string)] = e.Attrs
				}
			}
			seen := make(map[string]any)
			for line := range strings.SplitSeq(strings.TrimSpace(diagnostic.String()), "\n") {
				_, envelope, ok := strings.Cut(line, " event: ")
				if !ok {
					t.Fatalf("missing undelivered envelope: %s", line)
				}
				var e struct {
					Event     string       `json:"event"`
					RequestID string       `json:"request_id"`
					User      string       `json:"user"`
					Cause     string       `json:"cause"`
					Depth     int          `json:"depth"`
					Attrs     events.Attrs `json:"attrs"`
				}
				must(t, json.Unmarshal([]byte(envelope), &e))
				same(t, e.Event, "repo.pushed")
				same(t, e.RequestID, "request")
				same(t, e.User, "alice")
				if ending == "malformed" {
					same(t, e.Cause, "invalid")
					same(t, e.Depth, 8)
				} else {
					same(t, e.Cause, "")
					same(t, e.Depth, 0)
				}
				seen[e.Attrs["ref"].(string)] = e.Attrs
			}
			same(t, seen, want)
		})
	}
}
