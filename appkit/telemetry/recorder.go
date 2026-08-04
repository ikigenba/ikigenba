package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"registry"
)

const (
	defaultCapacity    = 4096
	defaultBatchMax    = 256
	defaultFlushEvery  = time.Second
	defaultHTTPTimeout = 2 * time.Second
)

// Options configures a Recorder. Zero-valued sizing and timing fields use the
// package defaults.
type Options struct {
	Service    string
	IngestURL  string
	Enabled    bool
	Capacity   int
	BatchMax   int
	FlushEvery time.Duration
	Client     *http.Client
	Logger     *slog.Logger
}

// Recorder is a bounded, asynchronous, best-effort telemetry sink.
type Recorder struct {
	service    string
	ingestURL  string
	enabled    bool
	capacity   int
	batchMax   int
	flushEvery time.Duration
	client     *http.Client
	logger     *slog.Logger

	mu      sync.Mutex
	ring    []Record
	head    int
	count   int
	dropped uint64
	wake    chan struct{}

	flushMu sync.Mutex
	start   sync.Once
	stop    context.CancelFunc
	done    chan struct{}
	close   sync.Once
}

// New creates a recorder. Disabled recorders retain no records and perform no
// HTTP requests.
func New(opts Options) *Recorder {
	if opts.IngestURL == "" {
		opts.IngestURL = registry.BaseURL("telemetry") + "/ingest"
	}
	if opts.Capacity <= 0 {
		opts.Capacity = defaultCapacity
	}
	if opts.BatchMax <= 0 {
		opts.BatchMax = defaultBatchMax
	}
	if opts.BatchMax > opts.Capacity {
		opts.BatchMax = opts.Capacity
	}
	if opts.FlushEvery <= 0 {
		opts.FlushEvery = defaultFlushEvery
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	return &Recorder{
		service:    opts.Service,
		ingestURL:  opts.IngestURL,
		enabled:    opts.Enabled,
		capacity:   opts.Capacity,
		batchMax:   opts.BatchMax,
		flushEvery: opts.FlushEvery,
		client:     opts.Client,
		logger:     opts.Logger,
		ring:       make([]Record, opts.Capacity),
		wake:       make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
}

// Record adds rec without doing I/O. When the ring is full it replaces the
// oldest entry. A nil or disabled recorder is a no-op.
func (r *Recorder) Record(rec Record) {
	if r == nil || !r.enabled {
		return
	}
	if rec.Service == "" {
		rec.Service = r.service
	}

	r.mu.Lock()
	if r.count == r.capacity {
		r.ring[r.head] = rec
		r.head = (r.head + 1) % r.capacity
		r.dropped++
	} else {
		index := (r.head + r.count) % r.capacity
		r.ring[index] = rec
		r.count++
	}
	ready := r.count >= r.batchMax
	r.mu.Unlock()

	if ready {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

// Start launches the flush loop and returns immediately. Calling Start more
// than once is harmless.
func (r *Recorder) Start(ctx context.Context) {
	if r == nil || !r.enabled {
		return
	}
	r.start.Do(func() {
		loopCtx, cancel := context.WithCancel(ctx)
		r.stop = cancel
		go r.run(loopCtx)
	})
}

func (r *Recorder) run(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.flushEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.flush(ctx)
		case <-r.wake:
			r.flush(ctx)
		}
	}
}

// Close stops the flush loop, then makes one bounded attempt to deliver all
// records still buffered. It deliberately never reports an error.
func (r *Recorder) Close(ctx context.Context) error {
	if r == nil || !r.enabled {
		return nil
	}
	r.close.Do(func() {
		if r.stop != nil {
			r.stop()
			<-r.done
		}
		flushCtx, cancel := context.WithTimeout(ctx, defaultHTTPTimeout)
		defer cancel()
		r.flush(flushCtx)
	})
	return nil
}

func (r *Recorder) flush(ctx context.Context) {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	for {
		batch, dropped := r.takeBatch()
		if len(batch) == 0 {
			return
		}
		if r.post(ctx, batch, dropped) && dropped != 0 {
			r.mu.Lock()
			if r.dropped >= dropped {
				r.dropped -= dropped
			} else {
				r.dropped = 0
			}
			r.mu.Unlock()
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (r *Recorder) takeBatch() ([]Record, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count == 0 {
		return nil, 0
	}
	n := r.batchMax
	if r.count < n {
		n = r.count
	}
	batch := make([]Record, n)
	for i := range batch {
		batch[i] = r.ring[(r.head+i)%r.capacity]
	}
	r.head = (r.head + n) % r.capacity
	r.count -= n
	return batch, r.dropped
}

func (r *Recorder) post(ctx context.Context, records []Record, dropped uint64) bool {
	body := struct {
		Records []Record `json:"records"`
		Dropped uint64   `json:"dropped,omitempty"`
	}{Records: records, Dropped: dropped}
	encoded, err := json.Marshal(body)
	if err != nil {
		r.logger.Debug("telemetry batch encode failed", "error", err)
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.ingestURL, bytes.NewReader(encoded))
	if err != nil {
		r.logger.Debug("telemetry request creation failed", "error", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Debug("telemetry batch post failed", "error", err)
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		r.logger.Debug("telemetry batch rejected", "status", resp.StatusCode)
		return false
	}
	return true
}
