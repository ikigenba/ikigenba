package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type requestByteReader struct {
	io.Reader
	read int
}

func (r *requestByteReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func assertRequestByteCounts(t *testing.T, events []telemetry.Event, response *httptest.ResponseRecorder, requestBytes int) {
	t.Helper()
	finished := 0
	for _, event := range events {
		if event.Name != "request.finished" {
			continue
		}
		finished++
		if event.Attrs["request_bytes"] != int64(requestBytes) || event.Attrs["response_bytes"] != int64(response.Body.Len()) {
			t.Fatalf("finished byte counts=%#v, want request=%d response=%d", event.Attrs, requestBytes, response.Body.Len())
		}
	}
	if finished != 1 {
		t.Fatalf("request.finished count=%d, want 1", finished)
	}
}

func TestRequestBodyBytesCountOnlyReads(t *testing.T) {
	// R-OTR3-X6MT: GET bodies remain unread, regardless of their declared length, on both successful and refused identity requests.
	st := openTokenTestStore(t)
	_, session := tokenTestIdentity(t, st, "byte-counts")
	f := newTrail(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }}, nil)
	const body = "request body with multibyte text: 雪"
	for _, path := range []string{"/", "/check", "/me"} {
		t.Run(path, func(t *testing.T) {
			for _, signedIn := range []bool{false, true} {
				for _, length := range []int64{-1, 0, int64(len(body)), int64(len(body) + 500)} {
					reader := &requestByteReader{Reader: strings.NewReader(body)}
					r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, reader)
					r.ContentLength = length
					r.Host = "auth.green.example"
					if signedIn {
						r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
					}
					w, events := f.request(t, r)
					if reader.read != 0 {
						t.Fatalf("%s signedIn=%t length=%d: read %d body bytes", path, signedIn, length, reader.read)
					}
					assertRequestByteCounts(t, events, w, 0)
				}
			}
		})
	}
}

func TestTokenCreationRequestBodyBytes(t *testing.T) {
	// R-OTR3-X6MT: successful token creation consumes and counts the whole form body, in bytes rather than characters.
	for _, unknownLength := range []bool{false, true} {
		st := openTokenTestStore(t)
		_, session := tokenTestIdentity(t, st, "token-byte-counts")
		f := newTrail(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }}, nil)
		const body = "name=雪+key&expires=never&unused=☃"
		reader := &requestByteReader{Reader: strings.NewReader(body)}
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/tokens", reader)
		r.ContentLength = int64(len(body))
		if unknownLength {
			r.ContentLength = -1
		}
		r.Host = "auth.green.example"
		r.Header.Set("Origin", "https://auth.green.example")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		w, events := f.request(t, r)
		if w.Code != http.StatusOK {
			t.Fatalf("token creation status=%d body=%q", w.Code, w.Body.String())
		}
		if reader.read != len(body) {
			t.Fatalf("read=%d, want whole body length %d", reader.read, len(body))
		}
		assertRequestByteCounts(t, events, w, len(body))
	}
}

func TestResponseBodyByteCountsAcrossRoutes(t *testing.T) {
	// R-OTR3-X6MT: response counts describe the actual answer body, including shared assets, route misses, redirects, and handled errors.
	st := openSignInStore(t)
	f := newTrail(t, Config{Store: st, WorkspaceDomain: "green.example"}, nil)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/_appkit/theme.css", http.StatusOK},
		{http.MethodGet, "/_appkit/missing", http.StatusNotFound},
		{http.MethodPatch, "/missing", http.StatusNotFound},
		{http.MethodPut, "/", http.StatusMethodNotAllowed},
		{http.MethodPost, "/tokens", http.StatusForbidden},
		{http.MethodGet, "/login/google/callback?state=unknown", http.StatusBadRequest},
		{http.MethodPost, "/logout", http.StatusFound},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			r := trailRequest(tc.method, tc.path)
			if tc.path == "/logout" {
				r.Header.Set("Origin", "https://auth.green.example")
			}
			w, events := f.request(t, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d, want %d", w.Code, tc.status)
			}
			assertRequestByteCounts(t, events, w, 0)
		})
	}
	failServerStore(t, st)
	r := trailRequest(http.MethodGet, "/me")
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w, events := f.request(t, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("failing store status=%d, want 500", w.Code)
	}
	assertRequestByteCounts(t, events, w, 0)
}
