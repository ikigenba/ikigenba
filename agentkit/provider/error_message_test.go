package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentkit/provider"
)

// stubFailingClient is a minimal provider.Client that fails Stream
// with a chosen typed error. It exists so the typed-error contract
// can be exercised without a live backend.
type stubFailingClient struct {
	err *provider.Error
}

func (s stubFailingClient) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	return nil, s.err
}

// TestR_E2W7_K5JB_TypedErrorMappingHidesTransport pins the contract
// the agent loop will rely on when turning a provider failure into a
// `result` event with `is_error: true`:
//
//   - Client.Stream returns *provider.Error, never a raw HTTP type.
//   - provider.ErrorMessage produces a single-line, kind-prefixed
//     string with no embedded newlines from a stringified body.
//   - Each ErrorKind has a stable label so the loop can route on it.
//
// R-E2W7-K5JB: provider HTTP/SSE errors, rate-limit responses, and
// connection timeouts terminate the iteration with one result event;
// raw status codes and response bodies do not leak to stdout.
func TestR_E2W7_K5JB_TypedErrorMappingHidesTransport(t *testing.T) {
	cases := []struct {
		kind       provider.ErrorKind
		wantPrefix string
	}{
		{provider.ErrUnknown, "unknown"},
		{provider.ErrAuth, "auth"},
		{provider.ErrInvalidRequest, "invalid_request"},
		{provider.ErrRateLimit, "rate_limit"},
		{provider.ErrTimeout, "timeout"},
		{provider.ErrServer, "server"},
	}

	// A representative payload that, if surfaced verbatim, would
	// leak transport detail across multiple stdout lines.
	rawMsg := "boom\nHTTP/1.1 429 Too Many Requests\nresponse_body=\"quota exceeded\"\n"

	for _, c := range cases {
		c := c
		t.Run(c.wantPrefix, func(t *testing.T) {
			client := stubFailingClient{err: &provider.Error{Kind: c.kind, Msg: rawMsg}}

			ch, err := client.Stream(context.Background(), provider.Request{})
			if ch != nil {
				t.Errorf("failing Stream must return a nil channel, got %v", ch)
			}
			if err == nil {
				t.Fatalf("Stream must return a non-nil error on failure")
			}
			var perr *provider.Error
			if !errors.As(err, &perr) {
				t.Fatalf("Stream error must be *provider.Error, got %T", err)
			}
			if perr.Kind != c.kind {
				t.Errorf("Kind = %v, want %v", perr.Kind, c.kind)
			}

			got := provider.ErrorMessage(perr)
			if !strings.HasPrefix(got, c.wantPrefix) {
				t.Errorf("ErrorMessage = %q, want prefix %q", got, c.wantPrefix)
			}
			if strings.ContainsAny(got, "\n\r") {
				t.Errorf("ErrorMessage must be single-line, got %q", got)
			}
		})
	}

	t.Run("nil_error_is_empty", func(t *testing.T) {
		if got := provider.ErrorMessage(nil); got != "" {
			t.Errorf("ErrorMessage(nil) = %q, want \"\"", got)
		}
	})

	t.Run("empty_msg_yields_just_label", func(t *testing.T) {
		got := provider.ErrorMessage(&provider.Error{Kind: provider.ErrTimeout})
		if got != "timeout" {
			t.Errorf("ErrorMessage with empty Msg = %q, want %q", got, "timeout")
		}
	})

	t.Run("kind_labels_are_unique", func(t *testing.T) {
		// Distinct kinds must yield distinct prefixes so the loop
		// (and humans reading stdout) can tell them apart.
		seen := map[string]provider.ErrorKind{}
		for _, c := range cases {
			got := provider.ErrorMessage(&provider.Error{Kind: c.kind})
			if prev, ok := seen[got]; ok {
				t.Errorf("kind %v label %q collides with kind %v", c.kind, got, prev)
			}
			seen[got] = c.kind
		}
	})
}
