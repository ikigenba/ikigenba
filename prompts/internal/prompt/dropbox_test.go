package prompt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPFetcherClassifiesMirrorFailures(t *testing.T) {
	tests := []struct {
		name string
		code int
		want error
	}{
		{name: "missing", code: http.StatusNotFound, want: ErrNotFound},
		{name: "unavailable", code: http.StatusServiceUnavailable, want: ErrSourceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.code)
			}))
			defer ts.Close()

			_, err := NewHTTPFetcher(ts.URL).Fetch(context.Background(), "/prompts/test.md")
			if !errors.Is(err, tt.want) {
				t.Fatalf("Fetch error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestHTTPFetcherClassifiesTransportFailureAsSourceUnavailable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := ts.URL
	ts.Close()

	_, err := NewHTTPFetcher(base).Fetch(context.Background(), "/prompts/test.md")
	if !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("Fetch error = %v, want ErrSourceUnavailable", err)
	}
}
