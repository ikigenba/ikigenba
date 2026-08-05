package telemetry

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"eventplane/correlation"
)

func rootTestRecorder(t *testing.T) (*Recorder, *batchSink) {
	t.Helper()
	sink := newBatchSink()
	server := httptest.NewServer(sink)
	t.Cleanup(server.Close)
	recorder := New(Options{Enabled: true, IngestURL: server.URL, FlushEvery: time.Millisecond})
	recorder.Start(context.Background())
	t.Cleanup(func() {
		if err := recorder.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return recorder, sink
}

func rootTestRecords(t *testing.T, sink *batchSink, want int) []Record {
	t.Helper()
	waitFor(t, time.Second, func() bool {
		total := 0
		for _, batch := range sink.snapshot() {
			total += len(batch.Records)
		}
		return total >= want
	})
	var records []Record
	for _, batch := range sink.snapshot() {
		for _, record := range batch.Records {
			records = append(records, record.Record)
		}
	}
	return records
}

func TestStartRootReplacesAmbientCorrelationAndRecordsNewRoot(t *testing.T) {
	// R-XP15-H34E
	recorder, sink := rootTestRecorder(t)
	ambient := correlation.New()
	ctx := correlation.WithContext(context.Background(), ambient)
	gotCtx, gotID := recorder.StartRoot(ctx, "gmail:poll-cycle", map[string]any{"mailbox": "inbox"})

	if gotID == ambient {
		t.Fatal("StartRoot adopted the ambient correlation id")
	}
	if !correlation.Valid(gotID) {
		t.Fatalf("StartRoot id %q is not valid", gotID)
	}
	if installed := correlation.FromContext(gotCtx); installed != gotID {
		t.Fatalf("context correlation id = %q, want %q", installed, gotID)
	}
	records := rootTestRecords(t, sink, 1)
	if len(records) != 1 {
		t.Fatalf("received %d records, want exactly 1", len(records))
	}
	if got := records[0]; got.Kind != KindRoot || got.CorrelationID != gotID || got.Op != "gmail:poll-cycle" {
		t.Fatalf("root record = %#v, want kind root, correlation %q, and caller op", got, gotID)
	}
}

func TestStartChainAdoptsOrMintsCorrelationAndRecordsOneRoot(t *testing.T) {
	// R-XQ91-UUV3
	tests := []struct {
		name    string
		context func() (context.Context, string)
	}{
		{"adopts existing", func() (context.Context, string) {
			id := correlation.New()
			return correlation.WithContext(context.Background(), id), id
		}},
		{"mints missing", func() (context.Context, string) { return context.Background(), "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder, sink := rootTestRecorder(t)
			ctx, existing := test.context()
			gotCtx, gotID := recorder.StartChain(ctx, "run:run-42", nil)
			if existing != "" && gotID != existing {
				t.Fatalf("StartChain id = %q, want existing %q", gotID, existing)
			}
			if !correlation.Valid(gotID) || correlation.FromContext(gotCtx) != gotID {
				t.Fatalf("StartChain did not return and install a valid id: %q", gotID)
			}
			records := rootTestRecords(t, sink, 1)
			if len(records) != 1 {
				t.Fatalf("received %d records, want exactly 1", len(records))
			}
			if got := records[0]; got.Kind != KindRoot || got.CorrelationID != gotID || got.Op != "run:run-42" {
				t.Fatalf("root record = %#v, want chain id %q and caller op", got, gotID)
			}
		})
	}
}

func TestRootHelpersPreserveCorrelationWhenRecorderIsNil(t *testing.T) {
	// R-XRGY-8MLS
	var recorder *Recorder
	rootCtx, rootID := recorder.StartRoot(context.Background(), "wiki:pipeline-cycle", nil)
	chainCtx, chainID := recorder.StartChain(context.Background(), "run:run-43", nil)
	for name, result := range map[string]struct {
		ctx context.Context
		id  string
	}{"StartRoot": {rootCtx, rootID}, "StartChain": {chainCtx, chainID}} {
		if result.id == "" || !correlation.Valid(result.id) {
			t.Errorf("%s id = %q, want non-empty valid id", name, result.id)
		}
		if got := correlation.FromContext(result.ctx); got != result.id {
			t.Errorf("%s context id = %q, want %q", name, got, result.id)
		}
	}
}
