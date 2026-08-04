package telemetry

import (
	"context"

	"eventplane/correlation"
)

// StartRoot begins a new correlation chain, ignoring any chain already carried
// by ctx, and records the caller's named origin when telemetry is enabled.
func (r *Recorder) StartRoot(ctx context.Context, op string, detail map[string]any) (context.Context, string) {
	id := correlation.New()
	ctx = correlation.WithContext(ctx, id)
	r.Record(Record{CorrelationID: id, Kind: KindRoot, Op: op, Detail: detail})
	return ctx, id
}

// StartChain marks a named origin within a chain, adopting the correlation id
// on ctx when present and minting one otherwise.
func (r *Recorder) StartChain(ctx context.Context, op string, detail map[string]any) (context.Context, string) {
	ctx, id := correlation.Ensure(ctx)
	r.Record(Record{CorrelationID: id, Kind: KindRoot, Op: op, Detail: detail})
	return ctx, id
}
