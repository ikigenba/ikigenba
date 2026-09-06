package render_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ikigenba/ikigenba/agent-repl/internal/render"
	"github.com/ikigenba/ikigenba/agentkit"
)

func TestLogSinkInitialSummaryAndFanout(t *testing.T) {
	// R-WH2F-GYP7, R-WIAB-UQFW
	var first, second bytes.Buffer
	sink := render.NewLogSink(&first, &second)
	if usage, cost, ok := sink.Summary(); ok || usage != (agentkit.Usage{}) || cost != 0 {
		t.Fatalf("initial Summary = (%+v, %d, %v), want zero, zero, false", usage, cost, ok)
	}

	parts := [][]byte{[]byte("first\n"), []byte("second"), []byte("\n")}
	for _, part := range parts {
		if n, err := sink.Write(part); err != nil || n != len(part) {
			t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(part))
		}
	}
	if got, want := first.String(), "first\nsecond\n"; got != want {
		t.Fatalf("first destination = %q, want %q", got, want)
	}
	if got, want := second.String(), first.String(); got != want {
		t.Fatalf("second destination = %q, want %q", got, want)
	}
	if _, _, ok := sink.Summary(); ok {
		t.Fatal("non-summary input unexpectedly produced a summary")
	}
}

func TestLogSinkExtractsLastSummary(t *testing.T) {
	// R-WIAB-UQFW
	var destination bytes.Buffer
	sink := render.NewLogSink(&destination)
	firstUsage := agentkit.Usage{InputTokens: 1, OutputTokens: 2}
	firstCost := agentkit.Cost(3)
	secondUsage := agentkit.Usage{CachedTokens: 4, ReasoningTokens: 5}
	secondCost := agentkit.Cost(6)

	writeRecord(t, sink, agentkit.LogRecord{Type: agentkit.RecordSummary, Usage: &firstUsage, Cost: &firstCost})
	writeRecord(t, sink, agentkit.LogRecord{Type: agentkit.RecordSummary, Usage: &secondUsage, Cost: &secondCost})

	usage, cost, ok := sink.Summary()
	if !ok || usage != secondUsage || cost != secondCost {
		t.Fatalf("Summary = (%+v, %d, %v), want (%+v, %d, true)", usage, cost, ok, secondUsage, secondCost)
	}
}

func writeRecord(t *testing.T, sink *render.LogSink, record agentkit.LogRecord) {
	t.Helper()
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	line = append(line, '\n')
	if _, err := sink.Write(line); err != nil {
		t.Fatal(err)
	}
}
