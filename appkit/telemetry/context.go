package telemetry

import "context"

type recorderContextKey struct{}

// WithRecorder returns ctx carrying r for downstream instrumentation sites.
func WithRecorder(ctx context.Context, r *Recorder) context.Context {
	return context.WithValue(ctx, recorderContextKey{}, r)
}

// RecorderFromContext returns the recorder installed by the server chain.
func RecorderFromContext(ctx context.Context) *Recorder {
	recorder, _ := ctx.Value(recorderContextKey{}).(*Recorder)
	return recorder
}
