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

// R-CC9P-VOA9 R-CDHM-9G0Y R-2KW2-8M30 R-2M3Y-MDTP R-2NBV-05KE
func TestDeclarations(t *testing.T) {
	want := []events.Emission{{Event: "campaign.member.added", Attrs: []string{"campaign", "candidate", "prospect_list"}}, {Event: "campaign.member.rejected", Attrs: []string{"campaign", "candidate", "reason"}}, {Event: "campaign.piece.delivered", Attrs: []string{"campaign", "candidate", "piece", "channel", "how"}}}
	if trail.EventMemberAdded != want[0].Event || trail.EventMemberRejected != want[1].Event || trail.EventPieceDelivered != want[2].Event || trail.EventVisited != "campaign.visited" {
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
	body := `{"emits":[{"event":"campaign.member.added","attrs":["campaign","candidate","prospect_list"]},{"event":"campaign.member.rejected","attrs":["campaign","candidate","reason"]},{"event":"campaign.piece.delivered","attrs":["campaign","candidate","piece","channel","how"]}],"accepts":[]}`
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" || rec.Body.String() != body {
		t.Fatalf("%d %s %q", rec.Code, rec.Header(), rec.Body.String())
	}
}

// R-1XPY-YYZT R-1YXV-CQQI R-2H8D-3AUX R-2IG9-H2LM R-2ESK-BRDJ R-2G0G-PJ48 R-3FG8-RYOP R-CQWI-GX6L R-CVS4-005D
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

type badTelemetry struct{ received chan telemetry.Event }

func (s badTelemetry) Deliver(_ context.Context, e telemetry.Event) error {
	s.received <- e
	return telemetry.ErrRejected
}

// R-21DO-4A7W
func TestIndependentHalves(t *testing.T) {
	ctx := contextWithCaller(context.Background(), "caller-a", "request-a")
	attrs := telemetry.Attrs{"campaign": "c", "candidate": "b", "prospect_list": ""}
	checkTrail := func(r rig, name string, want telemetry.Attrs) {
		t.Helper()
		if err := r.w.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := r.wc.Events()
		if len(got) != 1 || got[0].Name != name || got[0].RequestID != "request-a" || got[0].User != "caller-a" || !reflect.DeepEqual(got[0].Attrs, want) {
			t.Fatalf("surviving trail: %+v", got)
		}
	}
	for _, err := range []error{errors.New("failed"), fmt.Errorf("refused: %w", events.ErrRejected)} {
		received := make(chan events.Event, 100)
		r := setup(t, nil, badEvents{err, received})
		trail.MemberAdded(ctx, r.w, r.em, "c", "b", "")
		select {
		case got := <-received:
			if got.Name != trail.EventMemberAdded || !reflect.DeepEqual(telemetry.Attrs(got.Attrs), attrs) || got.RequestID != "request-a" || got.User != "caller-a" {
				t.Fatal(got)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("failing bus never received event")
		}
		checkTrail(r, trail.EventMemberAdded, attrs)
		stop, cancel := context.WithCancel(context.Background())
		cancel()
		r.em.Shutdown(stop)
	}
	r := setup(t, nil, nil)
	r.em.Shutdown(context.Background())
	trail.MemberRejected(ctx, r.w, r.em, "c", "b", "why")
	r.flush(t)
	checkTrail(r, trail.EventMemberRejected, telemetry.Attrs{"campaign": "c", "candidate": "b", "reason": "why"})
	if len(r.ec.Events()) != 0 {
		t.Fatal("stopped emitter received event")
	}
	received := make(chan telemetry.Event, 10)
	r = setup(t, badTelemetry{received}, nil)
	trail.PieceDelivered(ctx, r.w, r.em, "c", "b", "p", "mail", "hand")
	r.flush(t)
	want := telemetry.Attrs{"campaign": "c", "candidate": "b", "piece": "p", "channel": "mail", "how": "hand"}
	select {
	case got := <-received:
		if got.Name != trail.EventPieceDelivered || !reflect.DeepEqual(got.Attrs, want) || got.RequestID != "request-a" || got.User != "caller-a" {
			t.Fatal(got)
		}
	default:
		t.Fatal("failing writer never received event")
	}
	got := r.ec.Events()
	if len(got) != 1 || got[0].Name != trail.EventPieceDelivered || !reflect.DeepEqual(telemetry.Attrs(got[0].Attrs), want) || got[0].RequestID != "request-a" || got[0].User != "caller-a" || got[0].Cause != "" || got[0].Depth != 0 {
		t.Fatalf("surviving bus: %+v", got)
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

// R-CS4E-UOXA
func TestReturnsWhileSinksBlock(t *testing.T) {
	wt := blockingTelemetry{make(chan struct{}, 1), make(chan struct{})}
	et := blockingEvents{make(chan struct{}, 1), make(chan struct{})}
	r := setup(t, wt, et)
	defer close(wt.release)
	defer close(et.release)
	done := make(chan struct{})
	go func() {
		trail.MemberAdded(context.Background(), r.w, r.em, "c", "b", "")
		trail.MemberRejected(context.Background(), r.w, r.em, "c", "b", "why")
		trail.PieceDelivered(context.Background(), r.w, r.em, "c", "b", "p", "mail", "hand")
		trail.Visited(context.Background(), r.w, "c", "p")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recording blocked")
	}
	for _, ch := range []chan struct{}{wt.entered, et.entered} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("sink not entered")
		}
	}
}

// R-22LK-I1YL
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
		names := []string{trail.EventMemberAdded, trail.EventMemberRejected, trail.EventPieceDelivered, trail.EventVisited}
		attrs := []telemetry.Attrs{{"campaign": "c" + id, "candidate": "b" + id, "prospect_list": "h" + id}, {"campaign": "c" + id, "candidate": "b" + id, "reason": "why" + id}, {"campaign": "c" + id, "candidate": "b" + id, "piece": "p" + id, "channel": "mail", "how": "hand"}, {"campaign": "c" + id, "piece": "p" + id}}
		for j, name := range names {
			expected := record{Name: name, RequestID: "q" + id, User: "u" + id, Attrs: attrs[j]}
			if j == 3 {
				expected.User = ""
			}
			wantTrail["q"+id+name] = expected
			if j < 3 {
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
