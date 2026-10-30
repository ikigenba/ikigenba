package trail_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

type rig struct {
	w  *telemetry.Writer
	em *events.Emitter
	wc *telemetry.Capture
	ec *events.Capture
}

func setup(t *testing.T, ws telemetry.Sink, es events.Sink) rig {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	r := rig{wc: &telemetry.Capture{}, ec: &events.Capture{}}
	if ws == nil {
		ws = r.wc
	}
	if es == nil {
		es = r.ec
	}
	now := func() time.Time { return time.Date(2026, 10, 5, 9, 0, 0, 123456789, time.UTC) }
	r.w = telemetry.New(telemetry.Config{Service: "campaigns", Sink: ws, Now: now, Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	r.em = events.New(events.Config{Service: "campaigns", Sink: es, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 20000)), Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard, Emits: trail.Emits()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		r.em.Shutdown(ctx)
		r.w.Shutdown(ctx, "test")
	})
	return r
}
func (r rig) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.em.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

// R-VCKI-HB8X R-CDHM-9G0Y R-VF0B-8UQB R-VG87-MMH0 R-VORI-B0NV
func TestDeclarations(t *testing.T) {
	want := []events.Emission{{Event: "campaign.member.added", Attrs: []string{"campaign", "candidate", "prospect_list"}}, {Event: "campaign.member.rejected", Attrs: []string{"campaign", "candidate", "reason"}}, {Event: "campaign.piece.delivered", Attrs: []string{"campaign", "candidate", "piece", "channel", "how"}}, {Event: "campaign.member.opened", Attrs: []string{"campaign", "candidate", "at"}}}
	if trail.EventMemberAdded != want[0].Event || trail.EventMemberRejected != want[1].Event || trail.EventPieceDelivered != want[2].Event || trail.EventVisited != "campaign.visited" || trail.EventMemberOpened != want[3].Event {
		t.Fatal("names")
	}
	got := trail.Emits()
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	got[0].Event = "changed"
	got[1].Attrs[0] = "changed"
	if !reflect.DeepEqual(trail.Emits(), want) {
		t.Fatal("shared declarations")
	}
	r := setup(t, nil, nil)
	rec := httptest.NewRecorder()
	events.DeclarationsHandler(r.em, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/declarations", nil))
	body := `{"emits":[{"event":"campaign.member.added","attrs":["campaign","candidate","prospect_list"]},{"event":"campaign.member.opened","attrs":["campaign","candidate","at"]},{"event":"campaign.member.rejected","attrs":["campaign","candidate","reason"]},{"event":"campaign.piece.delivered","attrs":["campaign","candidate","piece","channel","how"]}],"accepts":[]}`
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" || rec.Body.String() != body {
		t.Fatalf("%d %s %q", rec.Code, rec.Header(), rec.Body.String())
	}
}

// R-1XPY-YYZT R-1YXV-CQQI R-2H8D-3AUX R-2IG9-H2LM R-2ESK-BRDJ R-2G0G-PJ48 R-3FG8-RYOP R-VJVW-RXP3 R-CVS4-005D
func TestRecordsAndContexts(t *testing.T) {
	for _, withCaller := range []bool{false, true} {
		for _, done := range []bool{false, true} {
			for _, cause := range []bool{false, true} {
				r := setup(t, nil, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				user, request := "", ""
				if withCaller {
					user = "user-a"
					request = "request-a"
					ctx = contextWithCaller(ctx, user, request)
				}
				if cause {
					ctx = events.NewContext(ctx, events.Cause{ID: "evt_0123456789abcdef", Depth: 1000000})
				}
				if done {
					cancel()
				}
				trail.MemberAdded(ctx, r.w, r.em, "campaign-a", "candidate-a", "")
				trail.MemberRejected(ctx, r.w, r.em, "", "candidate-b", "why")
				trail.PieceDelivered(ctx, r.w, r.em, "campaign-a", "candidate-a", "piece-a", "mail", "hand")
				trail.Visited(ctx, r.w, "campaign-a", "piece-a")
				r.w.Emit(ctx, "tool.called", telemetry.Attrs{"tool": "deliver"})
				r.flush(t)
				we, ee := r.wc.Events(), r.ec.Events()
				wants := []telemetry.Attrs{{"campaign": "campaign-a", "candidate": "candidate-a", "prospect_list": ""}, {"campaign": "", "candidate": "candidate-b", "reason": "why"}, {"campaign": "campaign-a", "candidate": "candidate-a", "piece": "piece-a", "channel": "mail", "how": "hand"}, {"campaign": "campaign-a", "piece": "piece-a"}}
				names := []string{trail.EventMemberAdded, trail.EventMemberRejected, trail.EventPieceDelivered, trail.EventVisited}
				if len(we) != 5 || len(ee) != 3 {
					t.Fatalf("counts %d %d", len(we), len(ee))
				}
				for i := range wants {
					wantUser := user
					if i == 3 {
						wantUser = ""
					}
					if we[i].Name != names[i] || !reflect.DeepEqual(we[i].Attrs, wants[i]) || we[i].RequestID != request || we[i].User != wantUser {
						t.Fatalf("trail %d: %+v", i, we[i])
					}
				}
				for i, e := range ee {
					if e.Name != names[i] || !reflect.DeepEqual(telemetry.Attrs(e.Attrs), wants[i]) || e.RequestID != request || e.User != user {
						t.Fatalf("bus: %+v", e)
					}
					wantCause := ""
					depth := 0
					if cause {
						wantCause = "evt_0123456789abcdef"
						depth = 1000001
					}
					if e.Cause != wantCause || e.Depth != depth {
						t.Fatal(e)
					}
				}
			}
		}
	}
}
func contextWithCaller(ctx context.Context, user, request string) context.Context {
	return identity.NewContext(ctx, identity.Caller{UserID: user, RequestID: request})
}

type badEvents struct {
	err      error
	received chan events.Event
}

func (s badEvents) Deliver(_ context.Context, e events.Event) error {
	select {
	case s.received <- e:
	default:
	}
	return s.err
}

type badTelemetry struct {
	received chan telemetry.Event
	err      error
}

func (s badTelemetry) Deliver(_ context.Context, e telemetry.Event) error {
	select {
	case s.received <- e:
	default:
	}
	return s.err
}

// R-VMBP-JH6H
func TestIndependentHalves(t *testing.T) {
	ctx := contextWithCaller(context.Background(), "caller-a", "request-a")
	for _, call := range busCalls() {
		t.Run(call.name, func(t *testing.T) {
			checkTrail := func(r rig) {
				t.Helper()
				if err := r.w.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				got := r.wc.Events()
				if len(got) != 1 || got[0].Name != call.name || got[0].RequestID != "request-a" || got[0].User != call.user || !reflect.DeepEqual(got[0].Attrs, call.attrs) {
					t.Fatalf("surviving trail: %+v", got)
				}
			}
			for _, err := range []error{errors.New("failed"), fmt.Errorf("refused: %w", events.ErrRejected)} {
				received := make(chan events.Event, 100)
				r := setup(t, nil, badEvents{err, received})
				call.record(ctx, r)
				select {
				case <-received:
				case <-time.After(3 * time.Second):
					t.Fatal("bus never received event")
				}
				checkTrail(r)
				stop, cancel := context.WithCancel(context.Background())
				cancel()
				r.em.Shutdown(stop)
			}
			r := setup(t, nil, nil)
			r.em.Shutdown(context.Background())
			call.record(ctx, r)
			r.flush(t)
			checkTrail(r)
			if len(r.ec.Events()) != 0 {
				t.Fatal("stopped emitter received event")
			}
			for _, err := range []error{errors.New("failed"), telemetry.ErrRejected} {
				received := make(chan telemetry.Event, 100)
				r = setup(t, badTelemetry{received: received, err: err}, nil)
				call.record(ctx, r)
				select {
				case <-received:
				case <-time.After(3 * time.Second):
					t.Fatal("writer never received event")
				}
				if e := r.em.Flush(context.Background()); e != nil {
					t.Fatal(e)
				}
				got := r.ec.Events()
				if len(got) != 1 || got[0].Name != call.name || !reflect.DeepEqual(telemetry.Attrs(got[0].Attrs), call.attrs) || got[0].RequestID != "request-a" || got[0].User != call.user || got[0].Cause != "" || got[0].Depth != 0 {
					t.Fatalf("surviving bus: %+v", got)
				}
				stop, cancel := context.WithCancel(context.Background())
				cancel()
				r.w.Shutdown(stop, "test")
			}
		})
	}
}

type busCall struct {
	name, user string
	attrs      telemetry.Attrs
	record     func(context.Context, rig)
}

func busCalls() []busCall {
	return []busCall{
		{trail.EventMemberAdded, "caller-a", telemetry.Attrs{"campaign": "c", "candidate": "b", "prospect_list": ""}, func(ctx context.Context, r rig) { trail.MemberAdded(ctx, r.w, r.em, "c", "b", "") }},
		{trail.EventMemberRejected, "caller-a", telemetry.Attrs{"campaign": "c", "candidate": "b", "reason": "why"}, func(ctx context.Context, r rig) { trail.MemberRejected(ctx, r.w, r.em, "c", "b", "why") }},
		{trail.EventPieceDelivered, "caller-a", telemetry.Attrs{"campaign": "c", "candidate": "b", "piece": "p", "channel": "mail", "how": "hand"}, func(ctx context.Context, r rig) { trail.PieceDelivered(ctx, r.w, r.em, "c", "b", "p", "mail", "hand") }},
		{trail.EventMemberOpened, "", telemetry.Attrs{"campaign": "c", "candidate": "b", "at": time.Time{}.Format(time.RFC3339)}, func(ctx context.Context, r rig) { trail.MemberOpened(ctx, r.w, r.em, "c", "b", time.Time{}) }},
	}
}

type blockingTelemetry struct{ entered, release chan struct{} }

func (s blockingTelemetry) Deliver(context.Context, telemetry.Event) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

type blockingEvents struct{ entered, release chan struct{} }

func (s blockingEvents) Deliver(context.Context, events.Event) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

// R-VL3T-5PFS
func TestReturnsWhileSinksBlock(t *testing.T) {
	for _, mode := range []string{"writer", "emitter", "both"} {
		t.Run(mode, func(t *testing.T) {
			wt := blockingTelemetry{make(chan struct{}, 1), make(chan struct{})}
			et := blockingEvents{make(chan struct{}, 1), make(chan struct{})}
			var ws telemetry.Sink
			var es events.Sink
			if mode != "emitter" {
				ws = wt
			}
			if mode != "writer" {
				es = et
			}
			r := setup(t, ws, es)
			defer close(wt.release)
			defer close(et.release)
			done := make(chan struct{})
			go func() {
				for _, call := range busCalls() {
					call.record(context.Background(), r)
				}
				trail.Visited(context.Background(), r.w, "c", "p")
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("recording blocked")
			}
			channels := []chan struct{}{}
			if ws != nil {
				channels = append(channels, wt.entered)
			}
			if es != nil {
				channels = append(channels, et.entered)
			}
			for _, ch := range channels {
				select {
				case <-ch:
				case <-time.After(3 * time.Second):
					t.Fatal("sink not entered")
				}
			}
		})
	}
}

// R-VNJL-X8X6
func TestConcurrentCalls(t *testing.T) {
	r := setup(t, nil, nil)
	type record struct {
		Name, RequestID, User, Cause string
		Depth                        int
		Attrs                        telemetry.Attrs
	}
	wantTrail, wantBus := map[string]record{}, map[string]record{}
	var wg sync.WaitGroup
	for i := range 40 {
		id := fmt.Sprint(i)
		names := []string{trail.EventMemberAdded, trail.EventMemberRejected, trail.EventPieceDelivered, trail.EventVisited, trail.EventMemberOpened}
		attrs := []telemetry.Attrs{{"campaign": "c" + id, "candidate": "b" + id, "prospect_list": "h" + id}, {"campaign": "c" + id, "candidate": "b" + id, "reason": "why" + id}, {"campaign": "c" + id, "candidate": "b" + id, "piece": "p" + id, "channel": "mail", "how": "hand"}, {"campaign": "c" + id, "piece": "p" + id}, {"campaign": "c" + id, "candidate": "b" + id, "at": time.Time{}.Format(time.RFC3339)}}
		for j, name := range names {
			expected := record{Name: name, RequestID: "q" + id, User: "u" + id, Attrs: attrs[j]}
			if j >= 3 {
				expected.User = ""
			}
			wantTrail["q"+id+name] = expected
			if j != 3 {
				expected.Cause = fmt.Sprintf("evt_%016x", i)
				expected.Depth = i + 1
				wantBus["q"+id+name] = expected
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := contextWithCaller(context.Background(), "u"+id, "q"+id)
			ctx = events.NewContext(ctx, events.Cause{ID: fmt.Sprintf("evt_%016x", i), Depth: i})
			trail.MemberAdded(ctx, r.w, r.em, "c"+id, "b"+id, "h"+id)
			trail.MemberRejected(ctx, r.w, r.em, "c"+id, "b"+id, "why"+id)
			trail.PieceDelivered(ctx, r.w, r.em, "c"+id, "b"+id, "p"+id, "mail", "hand")
			trail.Visited(ctx, r.w, "c"+id, "p"+id)
			trail.MemberOpened(ctx, r.w, r.em, "c"+id, "b"+id, time.Time{})
		}()
	}
	wg.Wait()
	r.flush(t)
	for _, e := range r.wc.Events() {
		key := e.RequestID + e.Name
		want, ok := wantTrail[key]
		got := record{Name: e.Name, RequestID: e.RequestID, User: e.User, Attrs: e.Attrs}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected or duplicate trail %+v, want %+v", got, want)
		}
		delete(wantTrail, key)
	}
	for _, e := range r.ec.Events() {
		key := e.RequestID + e.Name
		want, ok := wantBus[key]
		got := record{Name: e.Name, RequestID: e.RequestID, User: e.User, Cause: e.Cause, Depth: e.Depth, Attrs: telemetry.Attrs(e.Attrs)}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected or duplicate bus %+v, want %+v", got, want)
		}
		delete(wantBus, key)
	}
	if len(wantTrail) != 0 || len(wantBus) != 0 {
		t.Fatalf("missing trail %v bus %v", wantTrail, wantBus)
	}
}

// R-VDSE-V2ZM R-VHG4-0E7P R-VIO0-E5YE R-VJVW-RXP3
func TestMemberOpenedContextsAndOrder(t *testing.T) {
	for _, at := range []time.Time{{}, time.Date(2026, 3, 4, 9, 15, 30, 750000000, time.FixedZone("west", -5*60*60))} {
		for _, withCaller := range []bool{false, true} {
			for _, done := range []bool{false, true} {
				for _, cause := range []bool{false, true} {
					for _, ids := range [][2]string{{"c", "b"}, {"", ""}} {
						r := setup(t, nil, nil)
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						request := ""
						if withCaller {
							request = "request-a"
							ctx = contextWithCaller(ctx, "owner", request)
						}
						if cause {
							ctx = events.NewContext(ctx, events.Cause{ID: "evt_0123456789abcdef", Depth: 2})
						}
						if done {
							cancel()
						}
						trail.Visited(ctx, r.w, ids[0], "p")
						trail.MemberOpened(ctx, r.w, r.em, ids[0], ids[1], at)
						r.w.Emit(ctx, "tool.called", telemetry.Attrs{"tool": "after"})
						trail.MemberAdded(ctx, r.w, r.em, "later-c", "later-b", "")
						r.flush(t)
						we, ee := r.wc.Events(), r.ec.Events()
						if len(we) != 4 || len(ee) != 2 {
							t.Fatalf("counts %d %d", len(we), len(ee))
						}
						want := telemetry.Attrs{"campaign": ids[0], "candidate": ids[1], "at": at.UTC().Format(time.RFC3339)}
						if we[0].Name != trail.EventVisited || we[1].Name != trail.EventMemberOpened || we[2].Name != "tool.called" || we[3].Name != trail.EventMemberAdded {
							t.Fatalf("trail order: %+v", we)
						}
						if !reflect.DeepEqual(we[1].Attrs, want) || we[1].RequestID != request || we[1].User != "" {
							t.Fatalf("opened trail: %+v", we[1])
						}
						if ee[0].Name != trail.EventMemberOpened || ee[1].Name != trail.EventMemberAdded || !reflect.DeepEqual(telemetry.Attrs(ee[0].Attrs), want) || ee[0].RequestID != request || ee[0].User != "" {
							t.Fatalf("opened bus/order: %+v", ee)
						}
						wantCause := ""
						depth := 0
						if cause {
							wantCause = "evt_0123456789abcdef"
							depth = 3
						}
						if ee[0].Cause != wantCause || ee[0].Depth != depth {
							t.Fatalf("cause: %+v", ee[0])
						}
					}
				}
			}
		}
	}
}
