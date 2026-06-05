package job

import (
	"context"
	"sync"
	"time"
)

// MemStore is an in-memory, concurrency-safe Store implementation. It is the
// reference implementation of the single-flight + crash-recovery contract and
// is used by the package's own tests; a consumer's real Store (SQLite-backed in
// the suite) must reproduce the same semantics, in particular that Insert
// rejects a second running record with the same FlightKey (ErrFlightInUse) under
// one serialized writer.
//
// MemStore is process-local and not persisted, so its "crash recovery" is only
// meaningful within a test that seeds a running record and then sweeps it.
type MemStore struct {
	mu   sync.Mutex
	recs map[string]Record
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{recs: make(map[string]Record)}
}

// Insert persists rec in StatusRunning, rejecting a second running record that
// shares rec.FlightKey with ErrFlightInUse — the single-flight gate. A
// duplicate ID is also rejected (ErrFlightInUse), since re-inserting a live run
// is the same conflict.
func (m *MemStore) Insert(_ context.Context, rec Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.recs[rec.ID]; exists {
		return ErrFlightInUse
	}
	if rec.FlightKey != "" {
		for _, existing := range m.recs {
			if existing.FlightKey == rec.FlightKey && existing.Status == StatusRunning {
				return ErrFlightInUse
			}
		}
	}
	m.recs[rec.ID] = rec
	return nil
}

// Load returns the record by id, or ErrNotFound if absent.
func (m *MemStore) Load(_ context.Context, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.recs[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return rec, nil
}

// UpdateTerminal writes the run's end state. A missing id returns ErrNotFound.
func (m *MemStore) UpdateTerminal(_ context.Context, id string, status Status, endedAt time.Time, usageJSON, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.recs[id]
	if !ok {
		return ErrNotFound
	}
	rec.Status = status
	rec.EndedAt = endedAt
	rec.UsageJSON = usageJSON
	rec.Error = errMsg
	m.recs[id] = rec
	return nil
}

// SweepRunning flips every record still StatusRunning to StatusFailed with an
// "interrupted by restart" error and an endedAt stamp, returning the count
// swept. A no-op (returns 0) when nothing is running.
func (m *MemStore) SweepRunning(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	n := 0
	for id, rec := range m.recs {
		if rec.Status == StatusRunning {
			rec.Status = StatusFailed
			rec.EndedAt = now
			rec.Error = "interrupted by restart"
			m.recs[id] = rec
			n++
		}
	}
	return n, nil
}

// seedRunning is a test helper that directly inserts a record bypassing the
// single-flight gate, used to simulate a crash-orphaned running record.
func (m *MemStore) seedRunning(rec Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec.Status = StatusRunning
	m.recs[rec.ID] = rec
}
