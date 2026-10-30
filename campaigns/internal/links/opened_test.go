package links_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns/internal/links"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

var _ func(links.Config) http.Handler = links.Handler

// R-U1A5-1NJG
func TestConfigContract(t *testing.T) {
	f := setup(t)
	cfg := links.Config(struct {
		Store              *store.Store
		Telemetry          *telemetry.Writer
		Events             *events.Emitter
		DefaultDestination string
	}{f.st, f.writer, f.emitter, "https://fallback.example/"})
	handler := links.Handler
	answer(t, serve(handler(cfg), "/t/"+f.piece.Token, identity.Caller{}), f.campaign.Destination)
}

func serve(handler http.Handler, path string, caller identity.Caller) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = r.WithContext(identity.NewContext(r.Context(), caller))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func flushBoth(t *testing.T, f *fixture) {
	t.Helper()
	flush(t, f)
	if err := f.emitter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func memberPieces(t *testing.T, f *fixture) []store.PieceRow {
	t.Helper()
	m, err := f.st.GetMember(context.Background(), f.campaign.ID, f.piece.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	return m.Pieces
}

// R-U65Q-KQI8 R-U7DM-YI8X
func TestFirstOpenAndOnlyFirstOpen(t *testing.T) {
	f := setup(t)
	caller := identity.Caller{UserID: "owner", RequestID: "first-open-request"}
	answer(t, request(t, f, http.MethodGet, "/t/"+f.piece.Token, nil, &caller), f.campaign.Destination)
	flushBoth(t, f)
	got := f.capture.Events()
	wantAttrs := telemetry.Attrs{"campaign": f.campaign.ID, "candidate": f.piece.Candidate, "at": f.now.UTC().Truncate(time.Second).Format(time.RFC3339)}
	if len(got) != 2 || got[0].Name != trail.EventVisited || got[1].Name != trail.EventMemberOpened || !reflect.DeepEqual(got[1].Attrs, wantAttrs) || got[1].RequestID != caller.RequestID || got[1].User != "" {
		t.Fatalf("first open trail: %+v", got)
	}
	bus := f.bus.Events()
	if len(bus) != 1 || bus[0].Name != trail.EventMemberOpened || !reflect.DeepEqual(bus[0].Attrs, events.Attrs(wantAttrs)) || bus[0].RequestID != caller.RequestID || bus[0].User != "" {
		t.Fatalf("first open bus: %+v", bus)
	}
	for _, p := range memberPieces(t, f) {
		answer(t, request(t, f, http.MethodGet, "/t/"+p.Piece.Token, nil, &caller), f.campaign.Destination)
	}
	for _, item := range []struct{ method, path string }{{http.MethodGet, "/t/" + strings.Repeat("a", 26)}, {http.MethodPost, "/t/" + f.piece.Token}, {http.MethodGet, "/t/"}} {
		answer(t, request(t, f, item.method, item.path, nil, &caller), "https://fallback.example/")
	}
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	answer(t, request(t, f, http.MethodGet, "/t/"+f.piece.Token, nil, &caller), "https://fallback.example/")
	flushBoth(t, f)
	if len(f.bus.Events()) != 1 {
		t.Fatalf("later bus events: %+v", f.bus.Events())
	}
	for _, e := range f.capture.Events()[2:] {
		if e.Name != trail.EventVisited {
			t.Fatalf("later trail event: %+v", e)
		}
	}
}

// R-U8LJ-C9ZM
func TestConcurrentFirstVisits(t *testing.T) {
	f := setup(t)
	pieces := memberPieces(t, f)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			<-start
			w := serve(f.h, "/t/"+pieces[i%len(pieces)].Piece.Token, identity.Caller{UserID: "owner", RequestID: fmt.Sprintf("parallel-%d", i)})
			answer(t, w, f.campaign.Destination)
		})
	}
	close(start)
	wg.Wait()
	flushBoth(t, f)
	count := 0
	for _, p := range pieces {
		vs, err := f.st.ListVisits(context.Background(), p.Piece.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		count += len(vs)
	}
	if count != 8 {
		t.Fatalf("visits = %d", count)
	}
	var opened []telemetry.Event
	for _, e := range f.capture.Events() {
		if e.Name == trail.EventMemberOpened {
			opened = append(opened, e)
		}
	}
	bus := f.bus.Events()
	if len(opened) != 1 || len(bus) != 1 || opened[0].RequestID != bus[0].RequestID || !strings.HasPrefix(opened[0].RequestID, "parallel-") {
		t.Fatalf("opens: trail %+v bus %+v", opened, bus)
	}
}

type blockedTelemetry struct {
	capture telemetry.Capture
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedTelemetry) Deliver(ctx context.Context, e telemetry.Event) error {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.capture.Deliver(ctx, e)
}

type blockedEvents struct {
	capture events.Capture
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedEvents) Deliver(ctx context.Context, e events.Event) error {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.capture.Deliver(ctx, e)
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("operation did not finish")
	}
}

func blockedSinks(t *testing.T, f *fixture, blockTrail, blockBus bool) (*blockedTelemetry, *blockedEvents) {
	t.Helper()
	tr := &blockedTelemetry{entered: make(chan struct{}), release: make(chan struct{})}
	bus := &blockedEvents{entered: make(chan struct{}), release: make(chan struct{})}
	if !blockTrail {
		close(tr.release)
	}
	if !blockBus {
		close(bus.release)
	}
	f.writer.Shutdown(context.Background(), "test")
	f.emitter.Shutdown(context.Background())
	f.writer = telemetry.New(telemetry.Config{Service: "campaigns", Sink: tr, Now: func() time.Time { return f.now }, Rand: strings.NewReader(string(randomFixture(61))), Sleep: func(context.Context, time.Duration) {}})
	f.emitter = events.New(events.Config{Service: "campaigns", Sink: bus, Emits: trail.Emits(), Now: func() time.Time { return f.now }, Rand: strings.NewReader(string(randomFixture(62))), Sleep: func(context.Context, time.Duration) {}})
	f.h = links.Handler(f.config())
	t.Cleanup(func() {
		if blockTrail {
			select {
			case <-tr.release:
			default:
				close(tr.release)
			}
		}
		if blockBus {
			select {
			case <-bus.release:
			default:
				close(bus.release)
			}
		}
	})
	return tr, bus
}

// R-U9TF-Q1QB
func TestVisitsAnswerWhileSinksBlock(t *testing.T) {
	for _, blocks := range []struct{ trail, bus bool }{{true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprintf("%t-%t", blocks.trail, blocks.bus), func(t *testing.T) {
			f := setup(t)
			tr, bus := blockedSinks(t, f, blocks.trail, blocks.bus)
			server := httptest.NewServer(f.h)
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			var first http.Header
			for i := range 2 {
				resp, err := client.Get(server.URL + "/t/" + f.piece.Token)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil || len(body) != 0 || resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != f.campaign.Destination {
					t.Fatalf("response %d %v %q %v", resp.StatusCode, resp.Header, body, err)
				}
				resp.Header.Del("Date")
				if i == 0 {
					first = resp.Header
				} else if !reflect.DeepEqual(first, resp.Header) {
					t.Fatalf("answers differ: %v / %v", first, resp.Header)
				}
			}
			await(t, tr.entered)
			await(t, bus.entered)
		})
	}
}

// R-U65Q-KQI8
func TestLiveOpenUsesEarliestVisitLeftByEarlierRelease(t *testing.T) {
	f := setup(t)
	first, err := f.st.Visit(context.Background(), f.piece.Token, store.VisitInput{})
	if err != nil {
		t.Fatal(err)
	}
	clearMarkers(t, f)
	f.now = f.now.Add(time.Hour)
	answer(t, serve(f.h, "/t/"+f.piece.Token, identity.Caller{UserID: "owner", RequestID: "legacy-first"}), f.campaign.Destination)
	flushBoth(t, f)
	tr, bus := f.capture.Events(), f.bus.Events()
	want := first.First.UTC().Format(time.RFC3339)
	if len(tr) != 2 || len(bus) != 1 || tr[1].Name != trail.EventMemberOpened || tr[1].Attrs["at"] != want || bus[0].Attrs["at"] != want {
		t.Fatalf("earliest open: trail %+v bus %+v want %s", tr, bus, want)
	}
}
