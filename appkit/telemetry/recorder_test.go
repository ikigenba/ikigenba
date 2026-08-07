package telemetry

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type receivedBatch struct {
	Records []receivedWireRecord `json:"records"`
	Dropped *uint64              `json:"dropped,omitempty"`
}

type receivedWireRecord struct {
	ID   string `json:"id"`
	Time string `json:"time"`
	Record
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
		r.Record(Record{Op: stringID(i)})
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count != 4096 {
		t.Fatalf("ring count = %d, want 4096", r.count)
	}
	if r.dropped != 64 {
		t.Fatalf("dropped = %d, want 64", r.dropped)
	}
	if got := r.ring[r.head].Op; got != stringID(64) {
		t.Errorf("oldest retained op = %q, want %q", got, stringID(64))
	}
	newest := (r.head + r.count - 1) % r.capacity
	if got := r.ring[newest].Op; got != stringID(4096+63) {
		t.Errorf("newest retained op = %q, want %q", got, stringID(4096+63))
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
		r.Record(Record{Op: stringID(i)})
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
			seen[rec.Op] = true
		}
	}
	for i := 0; i < 1000; i++ {
		if !seen[stringID(i)] {
			t.Fatalf("record %q did not reach sink", stringID(i))
		}
	}
}

// R-PKUI-T1UT
func TestRecorderDefaultIdentityIsUniqueCrockfordULIDForEveryAcceptedRecord(t *testing.T) {
	sink := newBatchSink()
	server := httptest.NewServer(sink)
	defer server.Close()
	r := New(Options{Enabled: true, IngestURL: server.URL})
	for i := 0; i < 1000; i++ {
		r.Record(Record{Op: stringID(i)})
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pattern := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	seen := make(map[string]struct{}, 1000)
	for _, batch := range sink.snapshot() {
		for _, rec := range batch.Records {
			if !pattern.MatchString(rec.ID) {
				t.Fatalf("record %q id = %q, want Crockford ULID", rec.Op, rec.ID)
			}
			if _, duplicate := seen[rec.ID]; duplicate {
				t.Fatalf("duplicate id %q", rec.ID)
			}
			seen[rec.ID] = struct{}{}
		}
	}
	if len(seen) != 1000 {
		t.Fatalf("sink received %d distinct records, want 1000", len(seen))
	}
}

// R-PNAB-KLC7
func TestRecorderDefaultTimeIsUTCWithinAcceptanceWindow(t *testing.T) {
	sink := newBatchSink()
	server := httptest.NewServer(sink)
	defer server.Close()
	r := New(Options{Enabled: true, IngestURL: server.URL})
	before := time.Now().UTC()
	for i := 0; i < 32; i++ {
		r.Record(Record{Op: stringID(i)})
	}
	after := time.Now().UTC()
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	received := 0
	for _, batch := range sink.snapshot() {
		for _, rec := range batch.Records {
			received++
			if !strings.HasSuffix(rec.Time, "Z") {
				t.Errorf("record %q time = %q, want Z offset", rec.Op, rec.Time)
			}
			stamp, err := time.Parse(time.RFC3339Nano, rec.Time)
			if err != nil {
				t.Errorf("record %q time %q is not RFC3339Nano: %v", rec.Op, rec.Time, err)
				continue
			}
			if stamp.Before(before) || stamp.After(after) {
				t.Errorf("record %q time %s outside acceptance window [%s, %s]", rec.Op, stamp, before, after)
			}
		}
	}
	if received != 32 {
		t.Fatalf("sink received %d records, want 32", received)
	}
}

// R-POI7-YD2W
func TestRecorderInjectedStampSeamsControlIdentityAndTimeInEnqueueOrder(t *testing.T) {
	sink := newBatchSink()
	server := httptest.NewServer(sink)
	defer server.Close()
	fixed := time.Date(2026, time.August, 5, 17, 4, 3, 987654321, time.FixedZone("test", -5*60*60))
	ids := []string{"first-id", "second-id", "third-id"}
	next := 0
	r := New(Options{
		Enabled: true, IngestURL: server.URL,
		Now: func() time.Time { return fixed },
		NewID: func() string {
			id := ids[next]
			next++
			return id
		},
	})
	for _, op := range []string{"first", "second", "third"} {
		r.Record(Record{Op: op})
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var got []receivedWireRecord
	for _, batch := range sink.snapshot() {
		got = append(got, batch.Records...)
	}
	if len(got) != len(ids) {
		t.Fatalf("sink received %d records, want %d", len(got), len(ids))
	}
	wantTime := fixed.UTC().Format(time.RFC3339Nano)
	for i, rec := range got {
		if rec.ID != ids[i] || rec.Time != wantTime {
			t.Errorf("record %d stamp = (%q, %q), want (%q, %q)", i, rec.ID, rec.Time, ids[i], wantTime)
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
		r.Record(Record{Op: stringID(i)})
	}
	r.Start(context.Background())
	waitFor(t, time.Second, func() bool { return len(sink.snapshot()) == 1 })
	for i := 0; i < 4; i++ {
		r.Record(Record{Op: "successful-" + stringID(i)})
	}
	waitFor(t, time.Second, func() bool { return len(sink.snapshot()) == 2 })
	r.Record(Record{Op: "after-success"})
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
		r.Record(Record{Op: stringID(i)})
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
	r.Record(Record{Op: "live-after-refusal"})
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		for _, batch := range sink.snapshot() {
			for _, rec := range batch.Records {
				if rec.Op == "live-after-refusal" {
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
	r.Record(Record{Op: "immediately-before-close"})
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	batches := sink.snapshot()
	if len(batches) != 1 || len(batches[0].Records) != 1 || batches[0].Records[0].Op != "immediately-before-close" {
		t.Fatalf("close batches = %#v, want buffered record", batches)
	}
}

func TestRecorderDisabledPerformsNoRequests(t *testing.T) {
	// R-PTNV-XRYV
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	r := New(Options{Enabled: false, IngestURL: server.URL, FlushEvery: time.Millisecond})
	r.Start(context.Background())
	for i := 0; i < 5000; i++ {
		r.Record(Record{Op: stringID(i)})
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
