package eval

import (
	"context"
	"log/slog"
	"sync"

	"agentkit/accounting"
)

// CaptureHandler is a slog.Handler that pulls cost_usd and duration_ms off P0c's
// per-call accounting record — the runner attaches a *slog.Logger built over it to
// the llm wrapper so a real call's cost + latency are captured WITHOUT a second
// timing path (the design's "from P0c's per-call cost_usd / duration_ms"). It sums
// over records (an agent run emits one per turn), so a multi-turn site's total cost
// and total latency are captured. It is reset per case.
type CaptureHandler struct {
	mu      sync.Mutex
	costUSD float64
	durMS   int64
	calls   int
}

// NewCaptureHandler builds a capture handler whose Logger() is attached to the
// llm wrapper so a real call's cost + latency are pulled off P0c's accounting
// record.
func NewCaptureHandler() *CaptureHandler { return &CaptureHandler{} }

// Logger returns a *slog.Logger that feeds this handler.
func (h *CaptureHandler) Logger() *slog.Logger { return slog.New(h) }

// Result returns the accumulated cost (USD), latency (ms), and call count since
// the last Reset.
func (h *CaptureHandler) Result() (costUSD float64, durMS int64, calls int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.costUSD, h.durMS, h.calls
}

// Reset zeroes the accumulators (called per case before the real call).
func (h *CaptureHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.costUSD, h.durMS, h.calls = 0, 0, 0
}

func (h *CaptureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *CaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case accounting.FieldCostUSD:
			h.costUSD += a.Value.Float64()
		case accounting.FieldDurationMS:
			h.durMS += a.Value.Int64()
		}
		return true
	})
	return nil
}

// WithAttrs / WithGroup ignore the pre-bound call-site attribution (the runner
// only cares about cost/latency); the handler is shared so the accumulators see
// every record regardless of the bound group.
func (h *CaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *CaptureHandler) WithGroup(string) slog.Handler      { return h }
