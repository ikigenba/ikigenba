package events_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

// R-G9EH-3DFB R-GAMD-H560 R-GBU9-UWWP R-GXSG-QS97 R-H089-IBQL R-H1G5-W3HA
func TestCauseContext(t *testing.T) {
	ctx := context.Background()
	if c, ok := events.FromContext(ctx); ok || c != (events.Cause{}) {
		t.Fatal(c, ok)
	}
	for _, c := range []events.Cause{{}, {"malformed", -1}, {"evt_0123456789abcdef", 4}} {
		derived, cancel := context.WithCancel(events.NewContext(ctx, c))
		got, ok := events.FromContext(derived)
		cancel()
		if !ok || got != c {
			t.Fatal(got, ok)
		}
		ctx = derived
	}
	caller := identity.Caller{UserID: "u", Email: "email", RequestID: "request"}
	ctx = identity.NewContext(ctx, caller)
	ctx = events.NewContext(ctx, events.Cause{})
	got, ok := identity.FromContext(ctx)
	if !ok || got != caller {
		t.Fatal(got, ok)
	}
	ctx = identity.NewContext(ctx, identity.Caller{})
	cause, ok := events.FromContext(ctx)
	if !ok || cause != (events.Cause{}) {
		t.Fatal(cause, ok)
	}
}

// R-H9DX-DBDA R-HALT-R33Z R-HBTQ-4UUO R-HJ54-FHAU R-HKD0-T91J R-HLKX-70S8 R-HMST-KSIX
func TestForwardAndMiddleware(t *testing.T) {
	if events.CauseHeader != "X-Event-Cause" || events.DepthHeader != "X-Event-Depth" {
		t.Fatal("header constants")
	}
	r := httptest.NewRequest(http.MethodPost, "http://repos/path", nil)
	r.Header[events.CauseHeader] = []string{"old", "older"}
	r.Header[events.DepthHeader] = []string{"8", "9"}
	r.Header.Set("Other", "value")
	before := r.Header.Clone()
	events.Forward(context.Background(), r)
	if !reflect.DeepEqual(before, r.Header) {
		t.Fatal("changed absent cause")
	}
	for _, c := range []events.Cause{{"evt_0123456789abcdef", 0}, {"evt_0123456789abcdef", 123}, {"malformed", -1}} {
		events.Forward(events.NewContext(context.Background(), c), r)
		if !reflect.DeepEqual(r.Header.Values(events.CauseHeader), []string{c.ID}) || !reflect.DeepEqual(r.Header.Values(events.DepthHeader), []string{strconv.Itoa(c.Depth)}) || r.Header.Get("Other") != "value" {
			t.Fatal(r.Header)
		}
		if c.Depth >= 0 {
			calls := 0
			events.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				calls++
				got, ok := events.FromContext(r.Context())
				if !ok || got != c {
					t.Fatal(got, ok)
				}
			})).ServeHTTP(httptest.NewRecorder(), r)
			if calls != 1 {
				t.Fatal(calls)
			}
		}
	}
	empty := &http.Request{}
	events.Forward(context.Background(), empty)
	if empty.Header != nil {
		t.Fatal("allocated unchanged headers")
	}
	events.Forward(events.NewContext(context.Background(), events.Cause{}), empty)
	if !reflect.DeepEqual(empty.Header.Values(events.CauseHeader), []string{""}) {
		t.Fatal(empty.Header)
	}
}

// R-HD1M-IMLD R-HE9I-WEC2 R-1PE3-7P8S R-HGPB-NXTG
func TestMiddlewareHeaderValidation(t *testing.T) {
	type key struct{}
	original := events.Cause{"evt_0000000000000001", 9}
	ctx, cancel := context.WithDeadline(events.NewContext(context.WithValue(context.Background(), key{}, "value"), original), time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	cancel()
	cases := []struct {
		id, depth string
		valid     bool
	}{{"evt_0123456789abcdef", "0003", true}, {"evt_0123456789abcdef", "0", true}, {"evt_0123456789abcdef", "-1", false}, {"evt_0123456789abcdef", "+1", false}, {"evt_0123456789abcdef", " 1", false}, {"evt_0123456789abcdef", "1.0", false}, {"evt_0123456789abcdef", "١", false}, {"evt_0123456789abcdef", "999999999999999999999999", false}, {"evt_0123456789abcdeF", "1", false}, {"", "1", false}, {"evt_0123456789abcdef", "", false}}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodPost, "http://repos/path?q=1", nil).WithContext(ctx)
		r.Header[events.CauseHeader] = []string{tc.id, "evt_0000000000000002"}
		r.Header[events.DepthHeader] = []string{tc.depth, "1"}
		w := httptest.NewRecorder()
		before := r.Header.Clone()
		calls := 0
		events.Middleware(http.HandlerFunc(func(gotW http.ResponseWriter, gotR *http.Request) {
			calls++
			if gotW != w {
				t.Fatal("writer replaced")
			}
			if !tc.valid && gotR != r {
				t.Fatal("invalid request replaced")
			}
			if tc.valid && (gotR == r || !reflect.DeepEqual(gotR.WithContext(r.Context()), r)) {
				t.Fatal("request changed")
			}
			cause, ok := events.FromContext(gotR.Context())
			want := original
			if tc.valid {
				n, _ := strconv.Atoi(tc.depth)
				want = events.Cause{tc.id, n}
			}
			if !ok || cause != want || gotR.Context().Value(key{}) != "value" || gotR.Context().Err() != context.Canceled {
				t.Fatal(cause, ok)
			}
			a, aok := ctx.Deadline()
			b, bok := gotR.Context().Deadline()
			if aok != bok || a != b {
				t.Fatal("deadline changed")
			}
			gotW.Header().Set("Only", "next")
			gotW.WriteHeader(201)
			_, _ = gotW.Write([]byte("body"))
		})).ServeHTTP(w, r)
		if calls != 1 || w.Code != 201 || w.Body.String() != "body" || len(w.Header()) != 1 || !reflect.DeepEqual(r.Header, before) {
			t.Fatal(calls, w, r.Header)
		}
	}
}

// R-HHX8-1PK5
func TestMiddlewareConcurrent(t *testing.T) {
	h := events.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		c, ok := events.FromContext(r.Context())
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		if !ok || c.Depth != n {
			t.Errorf("cause %v %v", c, ok)
		}
	}))
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			r := httptest.NewRequest(http.MethodGet, "http://repos/?n="+strconv.Itoa(i), nil)
			events.Forward(events.NewContext(r.Context(), events.Cause{"evt_0123456789abcdef", i}), r)
			h.ServeHTTP(httptest.NewRecorder(), r)
		})
	}
	wg.Wait()
}
