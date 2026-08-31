package agentkit

import (
	"context"
	"iter"
	"net/http"
)

// Provider is the composed wire-format and endpoint adapter driven for one
// round-trip.
type Provider interface {
	BuildRequest(ctx context.Context, state RequestState) (*http.Request, error)
	Decode(ctx context.Context, resp *http.Response) iter.Seq2[Event, error]
	Classify(status int, header http.Header, body []byte) error
	Identity() Identity
}
