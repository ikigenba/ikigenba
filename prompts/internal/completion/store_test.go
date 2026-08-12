package completion

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testQueueItem(t *testing.T, store *Store, consumer, key string) Item {
	t.Helper()
	item, created, err := store.Ensure(t.Context(), Item{Consumer: consumer, Key: key, Origin: consumer, Name: "wiki.extract", Request: `{}`})
	if err != nil || !created {
		t.Fatalf("Ensure = %#v, %v, %v", item, created, err)
	}
	return item
}

// R-ZJKL-8UQY
func TestStoreClaimReclaimsExpiredLeaseUnderNewOwner(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	first := NewStore(database, "owner-a", func() time.Time { return now })
	item := testQueueItem(t, first, "service:wiki", "expired")
	if _, err := first.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL + time.Second)
	second := NewStore(database, "owner-b", func() time.Time { return now })
	reclaimed, err := second.Claim(t.Context())
	if err != nil || reclaimed.ID != item.ID || reclaimed.Owner != "owner-b" || reclaimed.Reclaims != 1 || reclaimed.Status != StatusRunning {
		t.Fatalf("reclaimed = %#v, %v", reclaimed, err)
	}
}

// R-ZM0E-0E8C
func TestStoreRenewedLeaseIsNotClaimable(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	owner := NewStore(database, "owner-a", func() time.Time { return now })
	item := testQueueItem(t, owner, "service:wiki", "renewed")
	if _, err := owner.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL - time.Minute)
	if renewed, err := owner.Renew(t.Context(), item.ID); err != nil || !renewed {
		t.Fatalf("Renew = %v, %v", renewed, err)
	}
	now = now.Add(2 * time.Minute)
	other := NewStore(database, "owner-b", func() time.Time { return now })
	if _, err := other.Claim(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Claim = %v, want no work", err)
	}
	got, err := owner.Get(t.Context(), item.ID, item.Consumer)
	if err != nil || got.Owner != "owner-a" || got.Reclaims != 0 || !got.LeaseExpiresAt.After(now) {
		t.Fatalf("renewed row = %#v, %v", got, err)
	}
}

// R-ZKSH-MMHN
func TestStoreSecondExpiredLeaseFailsAsAbandonedAndEnqueuesResult(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	a := NewStore(database, "owner-a", func() time.Time { return now })
	item := testQueueItem(t, a, "service:wiki", "poison")
	if _, err := a.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL + time.Second)
	b := NewStore(database, "owner-b", func() time.Time { return now })
	if _, err := b.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL + time.Second)
	c := NewStore(database, "owner-c", func() time.Time { return now })
	if _, err := c.Claim(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Claim at reclaim bound = %v", err)
	}
	got, err := c.Get(t.Context(), item.ID, item.Consumer)
	if err != nil || got.Status != StatusFailed || got.Reclaims != 2 || !strings.Contains(got.Error, "abandon") || got.ErrorCode != "abandoned" || got.FinishedAt.IsZero() {
		t.Fatalf("abandoned item = %#v, %v", got, err)
	}
	inbox, err := c.Inbox(t.Context(), item.Consumer)
	if err != nil || len(inbox) != 1 || inbox[0].ID != item.ID {
		t.Fatalf("Inbox = %#v, %v", inbox, err)
	}
}

// R-ZN8A-E5Z1
func TestStoreReleaseIsFreeAcrossRepeatedClaimCycles(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := NewStore(database, "owner-a", func() time.Time { return now })
	item := testQueueItem(t, store, "service:wiki", "deploys")
	for range 3 {
		if _, err := store.Claim(t.Context()); err != nil {
			t.Fatal(err)
		}
		if released, err := store.Release(t.Context(), item.ID); err != nil || !released {
			t.Fatalf("Release = %v, %v", released, err)
		}
	}
	got, _ := store.Get(t.Context(), item.ID, item.Consumer)
	if got.Status != StatusQueued || got.Owner != "" || !got.LeaseExpiresAt.IsZero() || got.Reclaims != 0 {
		t.Fatalf("released item = %#v", got)
	}
}

// R-06QO-IHU5
func TestStoreReleaseRequiresCurrentOwnerAndRunningStatus(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	a := NewStore(database, "owner-a", func() time.Time { return now })
	item := testQueueItem(t, a, "service:wiki", "guards")
	if _, err := a.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL + time.Second)
	b := NewStore(database, "owner-b", func() time.Time { return now })
	reclaimed, err := b.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if released, err := a.Release(t.Context(), item.ID); err != nil || released {
		t.Fatalf("stale Release = %v, %v", released, err)
	}
	stillRunning, err := b.Get(t.Context(), item.ID, item.Consumer)
	if err != nil {
		t.Fatal(err)
	}
	if stillRunning.Status != StatusRunning || stillRunning.Owner != reclaimed.Owner || !stillRunning.LeaseExpiresAt.Equal(reclaimed.LeaseExpiresAt) {
		t.Fatalf("stale release changed row: %#v", stillRunning)
	}
	if err := b.Complete(t.Context(), item.ID, `true`, `{}`, 1); err != nil {
		t.Fatal(err)
	}
	done, err := b.Get(t.Context(), item.ID, item.Consumer)
	if err != nil {
		t.Fatal(err)
	}
	if released, err := b.Release(t.Context(), item.ID); err != nil || released {
		t.Fatalf("done Release = %v, %v", released, err)
	}
	after, err := b.Get(t.Context(), item.ID, item.Consumer)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusDone || after.Owner != done.Owner || !after.LeaseExpiresAt.Equal(done.LeaseExpiresAt) {
		t.Fatalf("release changed done row: before=%#v after=%#v", done, after)
	}
}

// R-032Z-D6M2
func TestStoreCompleteAcceptsStaleSuccessButNeverOverwritesDone(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	a := NewStore(database, "owner-a", func() time.Time { return now })
	item := testQueueItem(t, a, "service:wiki", "success-wins")
	if _, err := a.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL + time.Second)
	b := NewStore(database, "owner-b", func() time.Time { return now })
	if _, err := b.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := a.Complete(t.Context(), item.ID, `{"winner":"a"}`, `{"total":7}`, 2); err != nil {
		t.Fatal(err)
	}
	if err := b.Complete(t.Context(), item.ID, `{"winner":"b"}`, `{"total":9}`, 3); err != nil {
		t.Fatal(err)
	}
	got, err := a.Get(t.Context(), item.ID, item.Consumer)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDone || got.Result != `{"winner":"a"}` || got.UsageJSON != `{"total":7}` || got.CostUSD != 2 {
		t.Fatalf("done item = %#v", got)
	}
}

// R-04AV-QYCR
func TestStoreFailRequiresCurrentOwnerAndNeverOverwritesDone(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	a := NewStore(database, "owner-a", func() time.Time { return now })
	item := testQueueItem(t, a, "service:wiki", "failure-guards")
	if _, err := a.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL + time.Second)
	b := NewStore(database, "owner-b", func() time.Time { return now })
	if _, err := b.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := a.Fail(t.Context(), item.ID, "stale", `{}`, 1); err != nil {
		t.Fatal(err)
	}
	running, err := b.Get(t.Context(), item.ID, item.Consumer)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != StatusRunning || running.Error != "" {
		t.Fatalf("stale failure landed: %#v", running)
	}
	if err := b.Complete(t.Context(), item.ID, `true`, `{}`, 2); err != nil {
		t.Fatal(err)
	}
	if err := b.Fail(t.Context(), item.ID, "late", `{}`, 3); err != nil {
		t.Fatal(err)
	}
	done, err := b.Get(t.Context(), item.ID, item.Consumer)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusDone || done.Error != "" || done.Result != `true` || done.CostUSD != 2 {
		t.Fatalf("failure overwrote success: %#v", done)
	}
}

// R-ZPO3-5PGF
func TestStoreTerminalWriteAfterAckIsLoggedNoOp(t *testing.T) {
	database := openCompletionTestDB(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	store := NewStore(database, "owner-a", time.Now, logger)
	item := testQueueItem(t, store, "service:wiki", "acked-running")
	if _, err := store.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack(t.Context(), item.ID, item.Consumer); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(t.Context(), item.ID, `true`, `{}`, 1); err != nil {
		t.Fatalf("Complete deleted row = %v", err)
	}
	if _, err := store.Get(t.Context(), item.ID, item.Consumer); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted row resurrected: %v", err)
	}
	if !strings.Contains(logs.String(), item.ID) || !strings.Contains(logs.String(), "terminal write lost") {
		t.Fatalf("log = %q, want lost write and id", logs.String())
	}
}

// R-00N6-LN4O
func TestStoreAckHTTPIsConsumerPartitioned(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	item := testQueueItem(t, store, "service:wiki", "partition-ack")
	if _, err := store.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(t.Context(), item.ID, `true`, `{}`, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack(t.Context(), item.ID, "service:other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("store Ack as wrong consumer = %v, want ErrNotFound", err)
	}
	if _, err := store.Get(t.Context(), item.ID, item.Consumer); err != nil {
		t.Fatalf("store Ack as wrong consumer deleted row: %v", err)
	}
	handler := completionMux(NewHTTP(store, func(string) string { return "test-key" }, func() bool { return false }))
	wrong := httptest.NewRecorder()
	handler.ServeHTTP(wrong, httptest.NewRequest(http.MethodDelete, "/completions/"+item.ID+"?consumer=service:other", nil))
	if wrong.Code != http.StatusNotFound {
		t.Fatalf("wrong consumer Ack = %d", wrong.Code)
	}
	if inbox, _ := store.Inbox(t.Context(), item.Consumer); len(inbox) != 1 || inbox[0].ID != item.ID {
		t.Fatalf("owning inbox lost row: %#v", inbox)
	}
	if _, err := store.Get(t.Context(), item.ID, item.Consumer); err != nil {
		t.Fatalf("wrong Ack deleted row: %v", err)
	}
	right := httptest.NewRecorder()
	handler.ServeHTTP(right, httptest.NewRequest(http.MethodDelete, "/completions/"+item.ID+"?consumer="+item.Consumer, nil))
	if right.Code != http.StatusNoContent {
		t.Fatalf("own Ack = %d: %s", right.Code, right.Body.String())
	}
}

// R-01V2-ZEVD
func TestStoreGetHTTPIsConsumerPartitioned(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	item := testQueueItem(t, store, "service:wiki", "partition-get")
	if _, err := store.Get(t.Context(), item.ID, "service:other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("store Get as wrong consumer = %v, want ErrNotFound", err)
	}
	if got, err := store.Get(t.Context(), item.ID, item.Consumer); err != nil || got.ID != item.ID {
		t.Fatalf("store Get as owning consumer = %#v, %v", got, err)
	}
	handler := completionMux(NewHTTP(store, func(string) string { return "test-key" }, func() bool { return false }))
	for consumer, want := range map[string]int{"service:other": http.StatusNotFound, item.Consumer: http.StatusOK} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/completions/"+item.ID+"?consumer="+consumer, nil))
		if recorder.Code != want {
			t.Errorf("Get as %s = %d, want %d: %s", consumer, recorder.Code, want, recorder.Body.String())
		}
	}
}

// R-096H-A1BJ
func TestStoreClaimPrefersConsumerWithoutRunningWork(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := NewStore(database, "owner-a", func() time.Time { return now })
	testQueueItem(t, store, "service:first", "first-running")
	backlog := testQueueItem(t, store, "service:first", "first-backlog")
	claimed, err := store.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Consumer != "service:first" {
		t.Fatalf("first Claim = %#v", claimed)
	}
	now = now.Add(time.Second)
	preferred := testQueueItem(t, store, "service:second", "second-new")
	next, err := store.Claim(t.Context())
	if err != nil || next.ID != preferred.ID {
		t.Fatalf("preferred Claim = %#v, %v; backlog=%q", next, err, backlog.ID)
	}
	third := testQueueItem(t, store, "service:third", "third-new")
	after, err := store.Claim(t.Context())
	if err != nil || after.ID != third.ID {
		t.Fatalf("consumer with running/no queued blocked Claim = %#v, %v", after, err)
	}
}

// R-JOT1-SJRO
func TestStoreSweepDeletesExpiredTerminalAndReclaimBoundItemsOnly(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store := NewStore(database, "owner-a", func() time.Time { return now })
	abandoned := testQueueItem(t, store, "service:wiki", "reclaim-bound")
	if _, err := store.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL + time.Second)
	if _, err := NewStore(database, "owner-b", func() time.Time { return now }).Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(LeaseTTL + time.Second)
	if _, err := NewStore(database, "owner-c", func() time.Time { return now }).Claim(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reclaim-bound Claim = %v", err)
	}
	reaped, err := store.Get(t.Context(), abandoned.ID, abandoned.Consumer)
	if err != nil || reaped.Status != StatusFailed || reaped.FinishedAt.IsZero() {
		t.Fatalf("reclaim-bound item = %#v, %v", reaped, err)
	}
	now = now.Add(RetentionTTL + time.Second)
	seed := func(id, status string, created, finished time.Time, reclaims int) {
		t.Helper()
		finishedAt := ""
		if !finished.IsZero() {
			finishedAt = formatTime(finished)
		}
		_, err := database.ExecContext(t.Context(), `INSERT INTO completions
			(id,consumer,origin,key,name,request,status,reclaims,created_at,finished_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, id, "service:wiki", "service:wiki", id, "wiki.extract", `{}`, status, reclaims, formatTime(created), finishedAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed("expired-done", StatusDone, now.Add(-10*24*time.Hour), now.Add(-RetentionTTL-time.Second), 0)
	seed("young-failed", StatusFailed, now.Add(-10*24*time.Hour), now.Add(-RetentionTTL+time.Second), 0)
	seed("old-queued", StatusQueued, now.Add(-20*24*time.Hour), time.Time{}, 0)
	deleted, err := store.Sweep(t.Context())
	if err != nil || deleted != 2 {
		t.Fatalf("Sweep = %d, %v", deleted, err)
	}
	for _, id := range []string{"young-failed", "old-queued"} {
		if _, err := store.Get(t.Context(), id, "service:wiki"); err != nil {
			t.Errorf("%s was swept: %v", id, err)
		}
	}
	for _, id := range []string{"expired-done", abandoned.ID} {
		if _, err := store.Get(t.Context(), id, "service:wiki"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s survived: %v", id, err)
		}
	}
}
