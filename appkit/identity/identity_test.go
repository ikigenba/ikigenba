package identity_test

import (
	"bytes"
	"context"
	"io"
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
	// R-KBDW-7FKA: usable as a constant.
	const body = identity.MissingBody
	if body != "identity header missing\n" {
		t.Fatalf("missing body: %q", body)
	}
	// R-KCLS-L7AZ R-KDTO-YZ1O R-KF1L-CQSD R-KG9H-QIJ2: exact public signatures used below.
	var require func(string, io.Writer, http.Handler) http.Handler
	var fromContext func(context.Context) (identity.Caller, bool)
	var newContext func(context.Context, identity.Caller) context.Context
	var forward func(identity.Caller, *http.Request)
	require, fromContext, newContext, forward = identity.Require, identity.FromContext, identity.NewContext, identity.Forward
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	forward(c, r)
	w := httptest.NewRecorder()
	require("app", nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok := fromContext(newContext(r.Context(), c))
		if !ok || got != c {
			t.Fatalf("caller: %+v, %v", got, ok)
		}
	})).ServeHTTP(w, r)
}

type writes struct {
	values [][]byte
}

func (w *writes) Write(p []byte) (int, error) {
	w.values = append(w.values, bytes.Clone(p))
	return len(p), nil
}

func TestRequireMissingIdentity(t *testing.T) {
	// R-KHHE-4A9R R-KIPA-I20G
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, user := range [][]string{nil, {""}, {"", "second-user"}} {
			for _, requestID := range [][]string{nil, {""}, {"", "second-id"}, {" req ", "ignored"}} {
				r := httptest.NewRequest(method, "/", nil)
				r.Header["X-User-Id"] = user
				r.Header["X-Request-Id"] = requestID
				w := httptest.NewRecorder()
				var log writes
				identity.Require("sample", &log, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Error("missing identity reached next")
				})).ServeHTTP(w, r)
				wantBody := identity.MissingBody
				if method == http.MethodHead {
					wantBody = ""
				}
				if w.Code != http.StatusInternalServerError || !slices.Equal(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != wantBody {
					t.Fatalf("missing response: %d %v %q", w.Code, w.Header(), w.Body.String())
				}
				id := r.Header.Get("X-Request-Id")
				if id == "" {
					id = "-"
				}
				wantLog := "sample: request " + id + ": X-User-Id is missing\n"
				if len(log.values) != 1 || string(log.values[0]) != wantLog {
					t.Fatalf("diagnostic writes: %q, want one %q", log.values, wantLog)
				}
			}
		}
	}
}

func TestRequireNilDiagnostics(t *testing.T) {
	// R-KJX6-VTR5
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		identity.Require("sample", nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("missing identity reached next")
		})).ServeHTTP(w, httptest.NewRequest(method, "/", nil))
		body := identity.MissingBody
		if method == http.MethodHead {
			body = ""
		}
		if w.Code != http.StatusInternalServerError || !slices.Equal(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != body {
			t.Fatalf("nil diagnostic response: %d %v %q", w.Code, w.Header(), w.Body.String())
		}
	}
}

func TestRequireCallerAndResponse(t *testing.T) {
	// R-KL53-9LHU R-KNKW-14Z8
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
		var log bytes.Buffer
		calls := 0
		identity.Require("app", &log, http.HandlerFunc(func(gotWriter http.ResponseWriter, gotRequest *http.Request) {
			calls++
			if gotWriter != w {
				t.Error("response writer replaced")
			}
			got, ok := identity.FromContext(gotRequest.Context())
			if !ok || got != want {
				t.Errorf("caller: %+v, %v; want %+v", got, ok, want)
			}
			gotWriter.Header().Set("X-Next", "yes")
			gotWriter.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(gotWriter, "next's body")
		})).ServeHTTP(w, r)
		if calls != 1 || log.Len() != 0 || w.Code != http.StatusAccepted || len(w.Header()) != 1 || w.Header().Get("X-Next") != "yes" || w.Body.String() != "next's body" {
			t.Fatalf("next result: calls=%d log=%q status=%d headers=%v body=%q", calls, log.String(), w.Code, w.Header(), w.Body.String())
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
	identity.Require("app", nil, http.HandlerFunc(func(_ http.ResponseWriter, next *http.Request) {
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
	var log writes
	h := identity.Require("app", &log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if len(log.values) != 32 {
		t.Fatalf("concurrent diagnostic count: %d", len(log.values))
	}
}
