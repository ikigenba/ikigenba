package store_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/store"
)

func TestAgentLogRecordIsStoredAsTranscript(t *testing.T) {
	// R-IVH9-GSSC
	createdAt := time.Date(2026, 9, 7, 1, 2, 3, 4, time.UTC)
	s := createStore(t, func() time.Time { return createdAt })
	w := store.NewLogWriter(s, "pass.agent", func(record agentkit.LogRecord) string {
		return fmt.Sprintf("%s %s #%d", record.Type, record.ID, record.Seq)
	})
	log := agentkit.NewLog(w, func() time.Time { return createdAt }, "agent-id")
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	zeroUsage := agentkit.Usage{}
	zeroCost := agentkit.Cost(0)
	wantRaw, err := json.Marshal(agentkit.LogRecord{
		Type: agentkit.RecordSummary, ID: "agent-id", Time: createdAt, Seq: 0,
		Usage: &zeroUsage, Cost: &zeroCost,
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.Fetch(1)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Address != "pass.agent" || entry.Kind != store.Kind("transcript") || entry.Text != "summary agent-id #0" {
		t.Fatalf("entry = %#v", entry)
	}
	if !slices.Equal(entry.Raw, wantRaw) {
		t.Fatalf("raw = %s, want %s", entry.Raw, wantRaw)
	}
	if !entry.Created.Equal(createdAt) {
		t.Fatalf("created = %v, want %v", entry.Created, createdAt)
	}
	if _, err := s.Fetch(2); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second entry error = %v, want ErrNotFound", err)
	}

	line := append(slices.Clone(wantRaw), '\n')
	n, err := w.Write(line)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(line) {
		t.Fatalf("Write count = %d, want %d", n, len(line))
	}
	second, err := s.Fetch(2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(second.Raw, wantRaw) {
		t.Fatalf("raw = %s, want newline-stripped %s", second.Raw, wantRaw)
	}
}

func TestSummaryIsRetainedAfterItIsPersisted(t *testing.T) {
	// R-IWP5-UKJ1
	s := createStore(t, func() time.Time {
		return time.Date(2026, 9, 7, 2, 3, 4, 5, time.UTC)
	})
	w := store.NewLogWriter(s, "root.worker", func(record agentkit.LogRecord) string {
		return "rendered " + string(record.Type)
	})
	if usage, cost, ok := w.Summary(); usage != (agentkit.Usage{}) || cost != 0 || ok {
		t.Fatalf("initial Summary = (%+v, %d, %t)", usage, cost, ok)
	}

	wantUsage := agentkit.Usage{
		InputTokens: 11, CachedTokens: 12, CacheWrite5mTokens: 13,
		CacheWrite1hTokens: 14, OutputTokens: 15, ReasoningTokens: 16,
	}
	wantCost := agentkit.Cost(987)
	record := agentkit.LogRecord{
		Type: agentkit.RecordSummary, ID: "worker", Seq: 7,
		Time:  time.Date(2026, 9, 7, 3, 4, 5, 6, time.UTC),
		Usage: &wantUsage, Cost: &wantCost,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	line := append(slices.Clone(raw), '\n')
	if n, err := w.Write(line); err != nil || n != len(line) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(line))
	}
	usage, cost, ok := w.Summary()
	if usage != wantUsage || cost != wantCost || !ok {
		t.Fatalf("Summary = (%+v, %d, %t), want (%+v, %d, true)", usage, cost, ok, wantUsage, wantCost)
	}
	entry, err := s.Fetch(1)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != store.Kind("transcript") || entry.Text != "rendered summary" || !slices.Equal(entry.Raw, raw) {
		t.Fatalf("persisted summary = %#v", entry)
	}
}

func TestLogWriterReturnsStoreFailure(t *testing.T) {
	// R-IVH9-GSSC
	s := createStore(t, time.Now)
	w := store.NewLogWriter(s, "agent", func(agentkit.LogRecord) string { return "summary" })
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"type":"summary","time":"2026-09-07T00:00:00Z","seq":1,"usage":{},"cost":0}` + "\n")
	if n, err := w.Write(raw); n != 0 || err == nil {
		t.Fatalf("Write after Close = (%d, %v), want (0, error)", n, err)
	}
	if usage, cost, ok := w.Summary(); usage != (agentkit.Usage{}) || cost != 0 || ok {
		t.Fatalf("Summary after failed write = (%+v, %d, %t)", usage, cost, ok)
	}
}
