package trail_test

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
)

func TestDeclarationsAndPublicAPI(t *testing.T) {
	// R-WYIG-1RPV R-WZQC-FJGK
	want := []events.Emission{
		{Event: "transfer.started", Attrs: []string{"transfer", "direction", "message", "attachment", "draft", "path"}},
		{Event: "transfer.finished", Attrs: []string{"transfer", "direction", "message", "attachment", "draft", "path", "status", "reason", "bytes"}},
		{Event: "draft.created", Attrs: []string{"draft", "message", "thread", "attachments"}},
		{Event: "draft.sent", Attrs: []string{"draft", "message", "thread", "attachments"}},
		{Event: "draft.discarded", Attrs: []string{"draft", "message", "thread", "attachments"}},
	}
	if got := []string{trail.TransferStarted, trail.TransferFinished, trail.DraftCreated, trail.DraftSent, trail.DraftDiscarded}; !reflect.DeepEqual(got, []string{want[0].Event, want[1].Event, want[2].Event, want[3].Event, want[4].Event}) {
		t.Fatal(got)
	}
	a, b := trail.Emits(), trail.Emits()
	if !reflect.DeepEqual(a, want) || !reflect.DeepEqual(b, want) {
		t.Fatalf("declarations: %#v", a)
	}
	a[0].Event = "changed.event"
	for i := range a {
		a[i].Attrs[0] = "changed"
	}
	if !reflect.DeepEqual(b, want) || !reflect.DeepEqual(trail.Emits(), want) {
		t.Fatal("declarations alias")
	}
}

func TestCaller(t *testing.T) {
	// R-X0Y8-TB79
	transfer := store.Transfer{UserID: "owner", UserEmail: "caller-label", RequestID: "original"}
	want := identity.Caller{UserID: transfer.UserID, Email: transfer.UserEmail, RequestID: transfer.RequestID}
	if got := trail.Caller(transfer); got != want {
		t.Fatalf("caller = %#v", got)
	}
	if got := trail.Caller(store.Transfer{}); got != (identity.Caller{}) {
		t.Fatalf("empty caller = %#v", got)
	}
}

type sinks struct {
	writer    *telemetry.Writer
	emitter   *events.Emitter
	telemetry *telemetry.Capture
	bus       *events.Capture
}

func newSinks(t *testing.T) sinks {
	t.Helper()
	now := func() time.Time { return time.Unix(500, 0) }
	pause := func(context.Context, time.Duration) {}
	s := sinks{telemetry: &telemetry.Capture{}, bus: &events.Capture{}}
	s.writer = telemetry.New(telemetry.Config{Service: "gmail", Version: "test", Sink: s.telemetry, Stderr: io.Discard, Now: now, Sleep: pause, Rand: bytes.NewReader(make([]byte, 4096))})
	s.emitter = events.New(events.Config{Service: "gmail", Sink: s.bus, Stderr: io.Discard, Now: now, Sleep: pause, Rand: bytes.NewReader(make([]byte, 4096)), Emits: trail.Emits(), Telemetry: s.writer})
	t.Cleanup(func() { s.emitter.Shutdown(context.Background()); s.writer.Shutdown(context.Background(), "test") })
	return s
}

func (s sinks) flush(t *testing.T) {
	t.Helper()
	if err := s.emitter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTransferEvents(t *testing.T) {
	// R-X265-72XY R-X3E1-KUON R-X4LX-YMFC
	transfer := store.Transfer{ID: "transfer-id", Direction: "pull", Message: "message-id", Attachment: "part-id", Draft: "draft-id", Path: "safe/path", Status: "failed", Reason: "provider", Bytes: 912, UserID: "owner", UserEmail: "caller-label", RequestID: "original"}
	for _, withCaller := range []bool{false, true} {
		for _, withCause := range []bool{false, true} {
			s := newSinks(t)
			ctx := context.Background()
			if withCaller {
				ctx = identity.NewContext(ctx, identity.Caller{UserID: "other", RequestID: "other-request"})
			}
			if withCause {
				ctx = events.NewContext(ctx, events.Cause{ID: "evt_0123456789abcdef", Depth: 3})
			}
			trail.Record(ctx, s.writer, s.emitter, trail.TransferStarted, transfer)
			trail.Record(ctx, s.writer, s.emitter, trail.TransferFinished, transfer)
			for _, name := range []string{trail.DraftCreated, trail.DraftSent, trail.DraftDiscarded, "unknown.event", ""} {
				trail.Record(ctx, s.writer, s.emitter, name, transfer)
			}
			s.flush(t)
			recorded, emitted := s.telemetry.Events(), s.bus.Events()
			if len(recorded) != 2 || len(emitted) != 2 {
				t.Fatalf("event counts %d %d", len(recorded), len(emitted))
			}
			for i, name := range []string{trail.TransferStarted, trail.TransferFinished} {
				want := telemetry.Attrs{"transfer": transfer.ID, "direction": transfer.Direction, "message": transfer.Message, "attachment": transfer.Attachment, "draft": transfer.Draft, "path": transfer.Path}
				if i == 1 {
					want["status"], want["reason"], want["bytes"] = transfer.Status, transfer.Reason, transfer.Bytes
				}
				if recorded[i].Name != name || emitted[i].Name != name || !reflect.DeepEqual(recorded[i].Attrs, want) || !reflect.DeepEqual(emitted[i].Attrs, events.Attrs(want)) {
					t.Fatalf("events %#v %#v", recorded[i], emitted[i])
				}
				if recorded[i].User != transfer.UserID || recorded[i].RequestID != transfer.RequestID || emitted[i].User != transfer.UserID || emitted[i].RequestID != transfer.RequestID {
					t.Fatal("original caller lost")
				}
				cause, depth := "", 0
				if withCause {
					cause, depth = "evt_0123456789abcdef", 4
				}
				if emitted[i].Cause != cause || emitted[i].Depth != depth {
					t.Fatalf("cause/depth %#v", emitted[i])
				}
			}
		}
	}
}

func TestDraftEvents(t *testing.T) {
	// R-X5TU-CE61 R-X71Q-Q5WQ R-X4LX-YMFC
	d := store.Draft{ID: "draft-id", Message: "message-id", Thread: "thread-id", UserID: "stored-owner", Attachments: []store.Attachment{{Path: "first"}, {Path: "second"}}}
	for _, withCaller := range []bool{false, true} {
		for _, withCause := range []bool{false, true} {
			s := newSinks(t)
			ctx := context.Background()
			user, request := "", ""
			if withCaller {
				user, request = "current-owner", "current-request"
				ctx = identity.NewContext(ctx, identity.Caller{UserID: user, RequestID: request})
			}
			if withCause {
				ctx = events.NewContext(ctx, events.Cause{ID: "evt_abcdef0123456789", Depth: 5})
			}
			names := []string{trail.DraftCreated, trail.DraftSent, trail.DraftDiscarded}
			for _, name := range names {
				trail.RecordDraft(ctx, s.writer, s.emitter, name, d)
			}
			for _, name := range []string{trail.TransferStarted, trail.TransferFinished, "unknown.event", ""} {
				trail.RecordDraft(ctx, s.writer, s.emitter, name, d)
			}
			s.flush(t)
			recorded, emitted := s.telemetry.Events(), s.bus.Events()
			if len(recorded) != 3 || len(emitted) != 3 {
				t.Fatalf("event counts %d %d", len(recorded), len(emitted))
			}
			for i, name := range names {
				want := telemetry.Attrs{"draft": d.ID, "message": d.Message, "thread": d.Thread, "attachments": int64(2)}
				if recorded[i].Name != name || emitted[i].Name != name || !reflect.DeepEqual(recorded[i].Attrs, want) || !reflect.DeepEqual(emitted[i].Attrs, events.Attrs(want)) {
					t.Fatalf("events %#v %#v", recorded[i], emitted[i])
				}
				if recorded[i].User != user || recorded[i].RequestID != request || emitted[i].User != user || emitted[i].RequestID != request {
					t.Fatal("context caller lost")
				}
				cause, depth := "", 0
				if withCause {
					cause, depth = "evt_abcdef0123456789", 6
				}
				if emitted[i].Cause != cause || emitted[i].Depth != depth {
					t.Fatalf("cause/depth %#v", emitted[i])
				}
			}
		}
	}
}
