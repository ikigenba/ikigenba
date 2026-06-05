package job

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubJob is a deterministic Job for tests. It coordinates with the test via
// channels rather than sleeps: started is closed when Run begins, release is
// closed by the test to let Run return (in the success path), and Run honors
// ctx so a Cancel/TTL unblocks it. fail forces an error return; usage is the
// usage blob returned.
type stubJob struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	usage    string
	fail     error
	// blockOnCtx, when true, makes Run return only when ctx is done (used for
	// the cancel test); otherwise Run waits on release.
	blockOnCtx bool
}

func newStubJob() *stubJob {
	return &stubJob{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (j *stubJob) Run(ctx context.Context) (string, error) {
	close(j.started)
	defer close(j.finished)

	if j.blockOnCtx {
		<-ctx.Done()
		return j.usage, ctx.Err()
	}

	select {
	case <-j.release:
		return j.usage, j.fail
	case <-ctx.Done():
		return j.usage, ctx.Err()
	}
}

func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// Test 1: spawn → succeed. A job that completes normally ends StatusSucceeded
// with the usage blob recorded.
func TestSpawnSucceed(t *testing.T) {
	store := NewMemStore()
	r := New(store, 0) // no TTL

	jb := newStubJob()
	jb.usage = `{"usage":{"input_tokens":10}}`

	rec, err := r.Spawn(Record{ID: "run-1", FlightKey: "key-1"}, jb)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if rec.Status != StatusRunning {
		t.Fatalf("returned record status = %q, want running", rec.Status)
	}

	awaitClosed(t, jb.started, "job start")

	// Persisted as running while in flight.
	got, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Load while running: %v", err)
	}
	if got.Status != StatusRunning {
		t.Fatalf("in-flight status = %q, want running", got.Status)
	}

	close(jb.release)
	awaitClosed(t, jb.finished, "job finish")
	r.wait()

	got, err = store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Load after finish: %v", err)
	}
	if got.Status != StatusSucceeded {
		t.Fatalf("terminal status = %q, want succeeded", got.Status)
	}
	if got.UsageJSON != jb.usage {
		t.Fatalf("usage = %q, want %q", got.UsageJSON, jb.usage)
	}
	if got.Error != "" {
		t.Fatalf("error = %q, want empty on success", got.Error)
	}
	if got.EndedAt.IsZero() {
		t.Fatalf("EndedAt is zero, want a terminal timestamp")
	}
	if !got.Status.Terminal() {
		t.Fatalf("Terminal() = false for status %q", got.Status)
	}
}

// Test 1b: a job that returns an error ends StatusFailed with the message.
func TestSpawnFail(t *testing.T) {
	store := NewMemStore()
	r := New(store, 0)

	jb := newStubJob()
	jb.fail = errors.New("boom")

	if _, err := r.Spawn(Record{ID: "run-f", FlightKey: "k"}, jb); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	awaitClosed(t, jb.started, "job start")
	close(jb.release)
	awaitClosed(t, jb.finished, "job finish")
	r.wait()

	got, _ := store.Load(context.Background(), "run-f")
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.Error != "boom" {
		t.Fatalf("error = %q, want %q", got.Error, "boom")
	}
}

// Test 2: spawn → cancel. A long-running job, Cancel(id) cancels its context
// and the record ends StatusCancelled.
func TestSpawnCancel(t *testing.T) {
	store := NewMemStore()
	r := New(store, 0)

	jb := newStubJob()
	jb.blockOnCtx = true // returns only when ctx is cancelled

	if _, err := r.Spawn(Record{ID: "run-2", FlightKey: "key-2"}, jb); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	awaitClosed(t, jb.started, "job start")

	if !r.Cancel("run-2") {
		t.Fatalf("Cancel returned false, want true (run in flight)")
	}

	awaitClosed(t, jb.finished, "job finish after cancel")
	r.wait()

	got, _ := store.Load(context.Background(), "run-2")
	if got.Status != StatusCancelled {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	if got.Error != "cancelled" {
		t.Fatalf("error = %q, want %q", got.Error, "cancelled")
	}

	// Cancel is idempotent: a finished/unknown id returns false.
	if r.Cancel("run-2") {
		t.Fatalf("second Cancel returned true, want false (no longer in flight)")
	}
	if r.Cancel("nope") {
		t.Fatalf("Cancel of unknown id returned true, want false")
	}
}

// Test 3: single-flight rejection. Spawning a second job with the same flight
// key while the first is running is rejected with ErrFlightInUse; the first
// run's state is unchanged.
func TestSingleFlightRejection(t *testing.T) {
	store := NewMemStore()
	r := New(store, 0)

	first := newStubJob()
	if _, err := r.Spawn(Record{ID: "run-a", FlightKey: "shared"}, first); err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	awaitClosed(t, first.started, "first job start")

	// Second spawn with the same flight key while the first is running.
	second := newStubJob()
	_, err := r.Spawn(Record{ID: "run-b", FlightKey: "shared"}, second)
	if !errors.Is(err, ErrFlightInUse) {
		t.Fatalf("second Spawn err = %v, want ErrFlightInUse", err)
	}

	// The rejected run was never persisted.
	if _, err := store.Load(context.Background(), "run-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load run-b = %v, want ErrNotFound (never inserted)", err)
	}
	// The second job's goroutine was never launched.
	select {
	case <-second.started:
		t.Fatalf("rejected job's Run was launched, want not launched")
	default:
	}
	// The first run is unchanged (still running).
	got, _ := store.Load(context.Background(), "run-a")
	if got.Status != StatusRunning {
		t.Fatalf("first run status = %q, want still running", got.Status)
	}

	// Let the first finish, then the key frees up and a new run may take it.
	close(first.release)
	awaitClosed(t, first.finished, "first job finish")
	r.wait()

	third := newStubJob()
	if _, err := r.Spawn(Record{ID: "run-c", FlightKey: "shared"}, third); err != nil {
		t.Fatalf("third Spawn after first finished: %v", err)
	}
	awaitClosed(t, third.started, "third job start")
	close(third.release)
	awaitClosed(t, third.finished, "third job finish")
	r.wait()
}

// Test 4: crash-recovery sweep. A "running" record with no live goroutine
// (simulating a crash) is marked terminal (failed) by Recover; a fresh sweep
// with no orphans is a no-op.
func TestCrashRecoverySweep(t *testing.T) {
	store := NewMemStore()
	r := New(store, 0)

	// Simulate a crash: a running record whose goroutine died with the process.
	store.seedRunning(Record{ID: "orphan-1", FlightKey: "k1", StartedAt: time.Now().UTC()})
	store.seedRunning(Record{ID: "orphan-2", FlightKey: "k2", StartedAt: time.Now().UTC()})

	n, err := r.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if n != 2 {
		t.Fatalf("swept %d, want 2", n)
	}

	for _, id := range []string{"orphan-1", "orphan-2"} {
		got, err := store.Load(context.Background(), id)
		if err != nil {
			t.Fatalf("Load %s: %v", id, err)
		}
		if got.Status != StatusFailed {
			t.Fatalf("%s status = %q, want failed", id, got.Status)
		}
		if !got.Status.Terminal() {
			t.Fatalf("%s Terminal() = false", id)
		}
		if got.EndedAt.IsZero() {
			t.Fatalf("%s EndedAt is zero, want a sweep timestamp", id)
		}
		if got.Error == "" {
			t.Fatalf("%s error is empty, want an interrupted message", id)
		}
	}

	// A fresh sweep with no orphans is a no-op.
	n, err = r.Recover(context.Background())
	if err != nil {
		t.Fatalf("second Recover: %v", err)
	}
	if n != 0 {
		t.Fatalf("second sweep swept %d, want 0", n)
	}
}

// TestStatusTerminal pins the Status.Terminal() classification.
func TestStatusTerminal(t *testing.T) {
	cases := map[Status]bool{
		StatusRunning:   false,
		StatusSucceeded: true,
		StatusFailed:    true,
		StatusCancelled: true,
	}
	for s, want := range cases {
		if got := s.Terminal(); got != want {
			t.Fatalf("Status(%q).Terminal() = %v, want %v", s, got, want)
		}
	}
}

// TestTTLExpiry confirms a TTL-bounded run that outlives its deadline ends
// failed with the TTL message (distinct from user-cancel).
func TestTTLExpiry(t *testing.T) {
	store := NewMemStore()
	r := New(store, 20*time.Millisecond)

	jb := newStubJob()
	jb.blockOnCtx = true // only returns when ctx (here the TTL) fires

	if _, err := r.Spawn(Record{ID: "run-ttl", FlightKey: "k"}, jb); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	awaitClosed(t, jb.started, "job start")
	awaitClosed(t, jb.finished, "job finish via TTL")
	r.wait()

	got, _ := store.Load(context.Background(), "run-ttl")
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want failed (TTL)", got.Status)
	}
	if got.Error != "run TTL exceeded" {
		t.Fatalf("error = %q, want %q", got.Error, "run TTL exceeded")
	}
}
