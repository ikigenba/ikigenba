package logging_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"appkit/logging"

	"eventplane/correlation"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// R-14QN-I04R
func TestCorrelationMiddleware_PropagatesValidInboundID(t *testing.T) {
	const inbound = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	var observed string
	handler := logging.CorrelationMiddleware(testLogger(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = correlation.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(correlation.Header, inbound)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if observed != inbound {
		t.Errorf("context correlation id = %q, want inbound %q", observed, inbound)
	}
	if got := rr.Header().Get(correlation.Header); got != inbound {
		t.Errorf("response %s = %q, want %q", correlation.Header, got, inbound)
	}
}

// R-15YJ-VRVG
func TestCorrelationMiddleware_MintsIDWhenHeaderAbsent(t *testing.T) {
	var observed string
	handler := logging.CorrelationMiddleware(testLogger(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = correlation.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if len(observed) != 26 || !correlation.Valid(observed) {
		t.Fatalf("context correlation id = %q, want a valid 26-character id", observed)
	}
	if got := rr.Header().Get(correlation.Header); got != observed {
		t.Errorf("response %s = %q, want context id %q", correlation.Header, got, observed)
	}
}

// R-176G-9JM5
func TestCorrelationMiddleware_ReplacesMalformedInboundID(t *testing.T) {
	malformed := []string{
		"too-short",
		"01ARZ3NDEKTSV4RRFFQ69G5FAI",
		"01ARZ3NDEKTSV4RRFFQ69G5FAL",
		"01ARZ3NDEKTSV4RRFFQ69G5FAO",
		"01ARZ3NDEKTSV4RRFFQ69G5FAU",
	}
	for _, inbound := range malformed {
		t.Run(inbound, func(t *testing.T) {
			var observed string
			handler := logging.CorrelationMiddleware(testLogger(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed = correlation.FromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(correlation.Header, inbound)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if observed == inbound || !correlation.Valid(observed) {
				t.Errorf("context correlation id = %q after malformed inbound %q; want a different valid id", observed, inbound)
			}
			if got := rr.Header().Get(correlation.Header); got != observed || got == inbound {
				t.Errorf("response %s = %q, want fresh context id %q", correlation.Header, got, observed)
			}
		})
	}
}

// R-18EC-NBCU
func TestNewULID_UsesCrockfordAlphabet(t *testing.T) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	sawZero, sawOne := false, false
	for range 500 {
		id := logging.NewULID()
		if len(id) != 26 {
			t.Fatalf("len(NewULID()) = %d for %q, want 26", len(id), id)
		}
		for _, char := range id {
			if !strings.ContainsRune(alphabet, char) {
				t.Fatalf("NewULID() = %q contains non-Crockford character %q", id, char)
			}
			sawZero = sawZero || char == '0'
			sawOne = sawOne || char == '1'
		}
	}
	if !sawZero || !sawOne {
		t.Fatalf("500 ULIDs saw digit 0 = %t, digit 1 = %t; want both", sawZero, sawOne)
	}
}
