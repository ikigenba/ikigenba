package telemetry

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type receivedBatch struct {
	Records []Record `json:"records"`
	Dropped *uint64  `json:"dropped,omitempty"`
}

type batchSink struct {
	mu      sync.Mutex
	batches []receivedBatch
	wake    chan struct{}
	reject  int
}

func newBatchSink() *batchSink {
	return &batchSink{wake: make(chan struct{}, 1)}
}

func (s *batchSink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var batch receivedBatch
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.batches = append(s.batches, batch)
	reject := s.reject > 0
	if reject {
		s.reject--
	}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	if reject {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *batchSink) snapshot() []receivedBatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]receivedBatch(nil), s.batches...)
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRecorderFullRingKeepsNewestRecords(t *testing.T) {
	// R-1AU5-EUU8
	r := New(Options{Enabled: true, Capacity: 4096, BatchMax: 256})
	for i := 0; i < 4096+64; i++ {
		r.Record(Record{ID: stringID(i)})
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count != 4096 {
		t.Fatalf("ring count = %d, want 4096", r.count)
	}
	if r.dropped != 64 {
		t.Fatalf("dropped = %d, want 64", r.dropped)
	}
	if got := r.ring[r.head].ID; got != stringID(64) {
		t.Errorf("oldest retained id = %q, want %q", got, stringID(64))
	}
	newest := (r.head + r.count - 1) % r.capacity
	if got := r.ring[newest].ID; got != stringID(4096+63) {
		t.Errorf("newest retained id = %q, want %q", got, stringID(4096+63))
	}
}

func TestRecorderBatchesOneThousandRecordsAtMaximum256(t *testing.T) {
	// R-1C21-SMKX
	sink := newBatchSink()
	server := httptest.NewServer(sink)
	defer server.Close()
	r := New(Options{Enabled: true, IngestURL: server.URL, FlushEvery: 5 * time.Millisecond})
	r.Start(context.Background())
	defer r.Close(context.Background())

	for i := 0; i < 1000; i++ {
		r.Record(Record{ID: stringID(i)})
	}
	waitFor(t, 2*time.Second, func() bool {
		total := 0
		for _, batch := range sink.snapshot() {
			total += len(batch.Records)
		}
		return total == 1000
	})

	seen := make(map[string]bool, 1000)
	for _, batch := range sink.snapshot() {
		if len(batch.Records) > 256 {
			t.Fatalf("batch has %d records, want at most 256", len(batch.Records))
		}
		for _, rec := range batch.Records {
			seen[rec.ID] = true
		}
	}
	for i := 0; i < 1000; i++ {
		if !seen[stringID(i)] {
			t.Fatalf("record %q did not reach sink", stringID(i))
		}
	}
}

func TestRecorderReportsDroppedCountOnceAfterSuccess(t *testing.T) {
	// R-1D9Y-6EBM
	sink := newBatchSink()
	sink.reject = 1
	server := httptest.NewServer(sink)
	defer server.Close()
	r := New(Options{Enabled: true, IngestURL: server.URL, Capacity: 4, BatchMax: 4, FlushEvery: time.Hour})
	for i := 0; i < 7; i++ {
		r.Record(Record{ID: stringID(i)})
	}
	r.Start(context.Background())
	waitFor(t, time.Second, func() bool { return len(sink.snapshot()) == 1 })
	for i := 0; i < 4; i++ {
		r.Record(Record{ID: "successful-" + stringID(i)})
	}
	waitFor(t, time.Second, func() bool { return len(sink.snapshot()) == 2 })
	r.Record(Record{ID: "after-success"})
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	batches := sink.snapshot()
	if len(batches) != 3 {
		t.Fatalf("received %d batches, want 3", len(batches))
	}
	if batches[0].Dropped == nil || *batches[0].Dropped != 3 {
		t.Fatalf("failed batch dropped = %v, want 3", batches[0].Dropped)
	}
	if batches[1].Dropped == nil || *batches[1].Dropped != 3 {
		t.Fatalf("next successful batch dropped = %v, want 3", batches[1].Dropped)
	}
	if batches[2].Dropped != nil {
		t.Fatalf("batch after success repeated dropped count %d", *batches[2].Dropped)
	}
}

func TestRecorderRefusedSinkNeverBlocksAndLaterLiveSinkReceives(t *testing.T) {
	// R-1EHU-K62B
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	r := New(Options{
		Enabled: true, IngestURL: "http://" + address, Capacity: 64, BatchMax: 16,
		FlushEvery: time.Hour, Client: &http.Client{Timeout: 100 * time.Millisecond},
	})
	r.Start(context.Background())
	started := time.Now()
	for i := 0; i < 5000; i++ {
		r.Record(Record{ID: stringID(i)})
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("5000 Record calls took %v, want under one second", elapsed)
	}
	waitFor(t, time.Second, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.count == 0
	})
	// count reaches zero when the last refused batch is removed from the ring;
	// taking flushMu also proves that its real HTTP attempt has returned.
	r.flushMu.Lock()
	r.flushMu.Unlock()

	liveListener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("reopen refused address: %v", err)
	}
	sink := newBatchSink()
	httpServer := &http.Server{Handler: sink}
	go httpServer.Serve(liveListener)
	defer httpServer.Shutdown(context.Background())
	r.Record(Record{ID: "live-after-refusal"})
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		for _, batch := range sink.snapshot() {
			for _, rec := range batch.Records {
				if rec.ID == "live-after-refusal" {
					return true
				}
			}
		}
		return false
	})
}

func TestRecorderCloseFlushesBufferedRecord(t *testing.T) {
	// R-1FPQ-XXT0
	sink := newBatchSink()
	server := httptest.NewServer(sink)
	defer server.Close()
	r := New(Options{Enabled: true, IngestURL: server.URL, FlushEvery: time.Hour})
	r.Start(context.Background())
	r.Record(Record{ID: "immediately-before-close"})
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	batches := sink.snapshot()
	if len(batches) != 1 || len(batches[0].Records) != 1 || batches[0].Records[0].ID != "immediately-before-close" {
		t.Fatalf("close batches = %#v, want buffered record", batches)
	}
}

func TestRecorderDisabledPerformsNoRequests(t *testing.T) {
	// R-1GXN-BPJP
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	r := New(Options{Enabled: false, IngestURL: server.URL, FlushEvery: time.Millisecond})
	r.Start(context.Background())
	for i := 0; i < 5000; i++ {
		r.Record(Record{ID: stringID(i)})
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := requests.Load(); got != 0 {
		t.Fatalf("disabled recorder made %d requests, want zero", got)
	}
}

func stringID(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	return string(buf[pos:])
}
