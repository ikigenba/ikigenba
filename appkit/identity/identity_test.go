package identity_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

func TestPublicContract(t *testing.T) {
	// R-HO9Q-DBQ4 R-KA5Z-TNTL: the imported package and ordered three-field value are usable.
	c := identity.Caller{"user", "email", "request"}
	if c.UserID != "user" || c.Email != "email" || c.RequestID != "request" {
		t.Fatalf("caller fields: %+v", c)
	}
	// R-CXI0-4DBA: usable as a string constant.
	const body string = identity.MissingBody
	wMissing := httptest.NewRecorder()
	identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(wMissing, httptest.NewRequest(http.MethodGet, "/", nil))
	if wMissing.Body.String() != body {
		t.Fatalf("missing body: %q", body)
	}
	// R-2LZM-O9RP R-KDTO-YZ1O R-KF1L-CQSD R-KG9H-QIJ2: exact public signatures used below.
	var require func(http.Handler) http.Handler
	var fromContext func(context.Context) (identity.Caller, bool)
	var newContext func(context.Context, identity.Caller) context.Context
	var forward func(identity.Caller, *http.Request)
	require, fromContext, newContext, forward = identity.Require, identity.FromContext, identity.NewContext, identity.Forward
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	forward(c, r)
	w := httptest.NewRecorder()
	require(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok := fromContext(newContext(r.Context(), c))
		if !ok || got != c {
			t.Fatalf("caller: %+v, %v", got, ok)
		}
	})).ServeHTTP(w, r)
}

func TestRequireMissingIdentity(t *testing.T) {
	// R-KHHE-4A9R
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, user := range [][]string{nil, {""}, {"", "second-user"}} {
			for _, requestID := range [][]string{nil, {""}, {"", "second-id"}, {" req ", "ignored"}} {
				r := httptest.NewRequest(method, "/", nil)
				r.Header["X-User-Id"] = user
				r.Header["X-Request-Id"] = requestID
				w := httptest.NewRecorder()
				identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Error("missing identity reached next")
				})).ServeHTTP(w, r)
				wantBody := identity.MissingBody
				if method == http.MethodHead {
					wantBody = ""
				}
				if w.Code != http.StatusInternalServerError || !slices.Equal(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != wantBody {
					t.Fatalf("missing response: %d %v %q", w.Code, w.Header(), w.Body.String())
				}
			}
		}
	}
}

func TestRequireCaller(t *testing.T) {
	// R-KL53-9LHU
	for _, optional := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header["X-User-Id"] = []string{" user ", "ignored"}
		want := identity.Caller{UserID: " user "}
		if optional {
			r.Header["X-User-Email"] = []string{" email ", "ignored"}
			r.Header["X-Request-Id"] = []string{" request ", "ignored"}
			want.Email, want.RequestID = " email ", " request "
		}
		w := httptest.NewRecorder()
		calls := 0
		identity.Require(http.HandlerFunc(func(gotWriter http.ResponseWriter, gotRequest *http.Request) {
			calls++
			if gotWriter != w {
				t.Error("response writer replaced")
			}
			got, ok := identity.FromContext(gotRequest.Context())
			if !ok || got != want {
				t.Errorf("caller: %+v, %v; want %+v", got, ok, want)
			}
		})).ServeHTTP(w, r)
		if calls != 1 {
			t.Fatalf("next called %d times, want once", calls)
		}
	}
}

type responseWrites struct {
	*httptest.ResponseRecorder
	statuses []int
	bodies   []string
}

func (w *responseWrites) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	w.ResponseRecorder.WriteHeader(status)
}

func (w *responseWrites) Write(body []byte) (int, error) {
	w.bodies = append(w.bodies, string(body))
	return w.ResponseRecorder.Write(body)
}

func TestRequireWritesOnlyNextResponse(t *testing.T) {
	// R-2N7J-21IE
	for _, next := range []http.Handler{
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Next", "header only")
		}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Next", "yes")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("next's body"))
		}),
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User-Id", "user")
		want := &responseWrites{ResponseRecorder: httptest.NewRecorder()}
		got := &responseWrites{ResponseRecorder: httptest.NewRecorder()}
		next.ServeHTTP(want, r)
		identity.Require(next).ServeHTTP(got, r)
		if !slices.Equal(got.statuses, want.statuses) || !slices.Equal(got.bodies, want.bodies) {
			t.Fatalf("response writes: statuses=%v bodies=%q, want statuses=%v bodies=%q", got.statuses, got.bodies, want.statuses, want.bodies)
		}
		if got.Body.String() != want.Body.String() {
			t.Fatalf("response body: %q, want %q", got.Body.String(), want.Body.String())
		}
		if len(got.Header()) != len(want.Header()) {
			t.Fatalf("response headers: %v, want %v", got.Header(), want.Header())
		}
		for name, values := range want.Header() {
			if !slices.Equal(got.Header().Values(name), values) {
				t.Fatalf("response header %s: %q, want %q", name, got.Header().Values(name), values)
			}
		}
	}
}

func TestRequirePreservesRequestContext(t *testing.T) {
	// R-KMCZ-ND8J
	type key struct{}
	deadline := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	base, cancel := context.WithDeadline(context.WithValue(context.Background(), key{}, "value"), deadline)
	defer cancel()
	r := httptest.NewRequest(http.MethodPost, "/path?query=value", bytes.NewBufferString("body")).WithContext(base)
	r.Header.Set("X-User-Id", "user")
	r.Trailer = http.Header{"X-Trailer": {"value"}}
	var got *http.Request
	identity.Require(http.HandlerFunc(func(_ http.ResponseWriter, next *http.Request) {
		got = next
	})).ServeHTTP(httptest.NewRecorder(), r)
	if got == nil {
		t.Fatal("next was not called")
	}
	if got.Method != r.Method || got.URL != r.URL || got.Proto != r.Proto || got.ProtoMajor != r.ProtoMajor || got.ProtoMinor != r.ProtoMinor || got.Body != r.Body || got.ContentLength != r.ContentLength || got.Host != r.Host || got.RemoteAddr != r.RemoteAddr || got.RequestURI != r.RequestURI || got.TLS != r.TLS || got.Close != r.Close {
		t.Fatal("request fields changed")
	}
	got.Header.Set("X-Shared", "header")
	got.Trailer.Set("X-Shared", "trailer")
	if r.Header.Get("X-Shared") != "header" || r.Trailer.Get("X-Shared") != "trailer" {
		t.Fatal("request maps replaced")
	}
	if got.Context().Value(key{}) != "value" {
		t.Fatal("context value lost")
	}
	if d, ok := got.Context().Deadline(); !ok || !d.Equal(deadline) {
		t.Fatalf("deadline: %v %v", d, ok)
	}
	if _, ok := identity.FromContext(r.Context()); ok {
		t.Fatal("incoming context replaced")
	}
	cancel()
	select {
	case <-got.Context().Done():
		if got.Context().Err() != context.Canceled {
			t.Fatalf("cancellation error: %v", got.Context().Err())
		}
	default:
		t.Fatal("cancellation lost")
	}
}

func TestContextCaller(t *testing.T) {
	// R-KOSS-EWPX
	for _, c := range []identity.Caller{{}, {UserID: "user"}, {"u", " e ", " r "}} {
		ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "outer"})
		ctx = identity.NewContext(ctx, c)
		ctx, cancel := context.WithCancel(ctx)
		got, ok := identity.FromContext(ctx)
		cancel()
		if !ok || got != c {
			t.Fatalf("context caller: %+v %v, want %+v", got, ok, c)
		}
	}
}

func TestContextWithoutCaller(t *testing.T) {
	// R-KQ0O-SOGM
	type key struct{}
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), key{}, identity.Caller{UserID: "unrelated"})} {
		got, ok := identity.FromContext(ctx)
		if ok || got != (identity.Caller{}) {
			t.Fatalf("absent caller: %+v %v", got, ok)
		}
	}
}

func TestForward(t *testing.T) {
	// R-KR8L-6G7B R-YLML-I4K4 R-YMUH-VWAT
	for mask := 0; mask < 8; mask++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header = http.Header{
			"X-User-Id": {"old-user", "second"}, "X-User-Email": {"old-email", "second"}, "X-Request-Id": {"old-request", "second"},
			"Authorization": {"untouched", "also untouched"}, "X-Other": {"other"},
		}
		c := identity.Caller{}
		if mask&1 != 0 {
			c.UserID = " user "
		}
		if mask&2 != 0 {
			c.Email = " email "
		}
		if mask&4 != 0 {
			c.RequestID = " request "
		}
		identity.Forward(c, r)
		for name, value := range map[string]string{"X-User-Id": c.UserID, "X-User-Email": c.Email, "X-Request-Id": c.RequestID} {
			if value == "" {
				if _, exists := r.Header[name]; exists {
					t.Errorf("empty field header retained: %s", name)
				}
			} else if !slices.Equal(r.Header.Values(name), []string{value}) {
				t.Errorf("forwarded %s: %q", name, r.Header.Values(name))
			}
		}
		if !slices.Equal(r.Header.Values("Authorization"), []string{"untouched", "also untouched"}) || !slices.Equal(r.Header.Values("X-Other"), []string{"other"}) {
			t.Fatal("unrelated headers changed")
		}
		for name := range r.Header {
			if name != "X-User-Id" && name != "X-User-Email" && name != "X-Request-Id" && name != "Authorization" && name != "X-Other" {
				t.Fatalf("extra header: %s", name)
			}
		}
	}
}

func TestRequireConcurrent(t *testing.T) {
	// R-KUWA-BRFE
	h := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := identity.FromContext(r.Context())
		if !ok || c.UserID != r.Header.Get("X-User-Id") || c.RequestID != r.Header.Get("X-Request-Id") {
			t.Errorf("concurrent caller: %+v %v", c, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	var group sync.WaitGroup
	for i := 0; i < 64; i++ {
		group.Go(func() {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("X-Request-Id", strconv.Itoa(i))
			want := http.StatusInternalServerError
			if i%2 == 0 {
				r.Header.Set("X-User-Id", "user-"+strconv.Itoa(i))
				want = http.StatusNoContent
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Errorf("concurrent status: %d, want %d", w.Code, want)
			}
		})
	}
	group.Wait()
}
