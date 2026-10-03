package identity_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

func TestOptionalPublicContract(_ *testing.T) {
	// R-6CUR-QCGS: the declared signature is usable by a consumer.
	exerciseOptionalSignature(identity.Optional)
}

func exerciseOptionalSignature(optional func(http.Handler) http.Handler) {
	handler := optional(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestOptionalSignedInCaller(t *testing.T) {
	// R-6FAK-HVY6
	for _, email := range [][]string{nil, {""}, {"", "ignored"}, {" email ", "ignored"}} {
		for _, requestID := range [][]string{nil, {""}, {"", "ignored"}, {" request ", "ignored"}} {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Header["X-User-Id"] = []string{" user ", "ignored"}
			r.Header["X-User-Email"] = email
			r.Header["X-Request-Id"] = requestID
			want := identity.Caller{UserID: " user ", Email: r.Header.Get("X-User-Email"), RequestID: r.Header.Get("X-Request-Id")}
			w := httptest.NewRecorder()
			calls := 0
			identity.Optional(http.HandlerFunc(func(gotWriter http.ResponseWriter, gotRequest *http.Request) {
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
}

func TestOptionalGuestCaller(t *testing.T) {
	// R-6GIG-VNOV
	for _, user := range [][]string{nil, {""}, {"", "ignored-user"}} {
		for _, email := range [][]string{nil, {""}, {" email ", "other-email"}, {"", "other-email"}} {
			for _, requestID := range [][]string{nil, {""}, {"", "ignored"}, {" request ", "ignored"}} {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header["X-User-Id"] = user
				r.Header["X-User-Email"] = email
				r.Header["X-Request-Id"] = requestID
				want := identity.Caller{RequestID: r.Header.Get("X-Request-Id")}
				w := httptest.NewRecorder()
				calls := 0
				identity.Optional(http.HandlerFunc(func(gotWriter http.ResponseWriter, gotRequest *http.Request) {
					calls++
					if gotWriter != w {
						t.Error("response writer replaced")
					}
					got, ok := identity.FromContext(gotRequest.Context())
					if !ok || got != want {
						t.Errorf("guest caller: %+v, %v; want %+v", got, ok, want)
					}
				})).ServeHTTP(w, r)
				if calls != 1 {
					t.Fatalf("next called %d times, want once", calls)
				}
			}
		}
	}
}

func TestOptionalPreservesRequestContext(t *testing.T) {
	// R-6HQD-9FFK
	for _, user := range []string{"", "user"} {
		type key struct{}
		deadline := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
		base, cancel := context.WithDeadline(context.WithValue(context.Background(), key{}, "value"), deadline)
		t.Cleanup(cancel)
		r := httptest.NewRequest(http.MethodPost, "/path?query=value", bytes.NewBufferString("body")).WithContext(base)
		r.Header.Set("X-User-Id", user)
		r.Trailer = http.Header{"X-Trailer": {"value"}}
		var got *http.Request
		identity.Optional(http.HandlerFunc(func(_ http.ResponseWriter, next *http.Request) {
			got = next
		})).ServeHTTP(httptest.NewRecorder(), r)
		if got == nil {
			t.Fatal("next was not called")
		}
		// Compare request values through the HTTP seam, excluding only context.
		if !reflect.DeepEqual(got, r.WithContext(got.Context())) {
			t.Fatal("request fields changed")
		}
		if got.URL != r.URL || got.Body != r.Body {
			t.Fatal("request URL or body replaced")
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
		if r.Context() != base {
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
}

func TestOptionalWritesOnlyNextResponse(t *testing.T) {
	// R-N11E-PAVK
	for _, headers := range []http.Header{
		{},
		{"X-User-Email": {"orphan-email"}, "X-Request-Id": {"guest-request"}},
		{"X-User-Id": {"", "ignored"}, "X-User-Email": {"ignored"}},
		{"X-User-Id": {"user"}, "X-User-Email": {"email"}, "X-Request-Id": {"request"}, "X-Other": {"other"}},
	} {
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
			r.Header = headers.Clone()
			want := &responseWrites{ResponseRecorder: httptest.NewRecorder()}
			got := &responseWrites{ResponseRecorder: httptest.NewRecorder()}
			next.ServeHTTP(want, r)
			identity.Optional(next).ServeHTTP(got, r)
			if !slices.Equal(got.statuses, want.statuses) || !slices.Equal(got.bodies, want.bodies) {
				t.Fatalf("response writes: statuses=%v bodies=%q, want statuses=%v bodies=%q", got.statuses, got.bodies, want.statuses, want.bodies)
			}
			if got.Body.String() != want.Body.String() {
				t.Fatalf("response body: %q, want %q", got.Body.String(), want.Body.String())
			}
			if !reflect.DeepEqual(got.Header(), want.Header()) {
				t.Fatalf("response headers: %v, want %v", got.Header(), want.Header())
			}
		}
	}
}

func TestOptionalConcurrent(t *testing.T) {
	// R-N29B-32M9
	h := identity.Optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := identity.Caller{UserID: r.Header.Get("X-User-Id"), RequestID: r.Header.Get("X-Request-Id")}
		if want.UserID != "" {
			want.Email = r.Header.Get("X-User-Email")
		}
		c, ok := identity.FromContext(r.Context())
		if !ok || c != want {
			t.Errorf("concurrent caller: %+v %v, want %+v", c, ok, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := 0; i < 64; i++ {
		group.Go(func() {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("X-Request-Id", "request-"+strconv.Itoa(i))
			r.Header.Set("X-User-Email", "email-"+strconv.Itoa(i))
			if i%2 == 0 {
				r.Header.Set("X-User-Id", "user-"+strconv.Itoa(i))
			}
			w := httptest.NewRecorder()
			<-start
			h.ServeHTTP(w, r)
			if w.Code != http.StatusNoContent {
				t.Errorf("concurrent status: %d, want %d", w.Code, http.StatusNoContent)
			}
		})
	}
	close(start)
	group.Wait()
}
