package wiki

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestClaimPendingAcquiresOneDurableLeasePerScopeAtomically(t *testing.T) {
	// R-OXVD-UPJD
	ctx := context.Background()
	now := time.Date(2026, 8, 12, 4, 0, 0, 0, time.UTC)

	t.Run("lease excludes a second job", func(t *testing.T) {
		conn := migratedDB(t, ctx)
		defer conn.Close()
		store := NewJobStore(conn)
		saveLeaseTestJobs(t, ctx, store, Job{ID: "job-1", Scope: "alpha"}, Job{ID: "job-2", Scope: "alpha"})

		job, ok, err := store.ClaimPending(ctx, now, 15*time.Minute)
		if err != nil || !ok || job.ID != "job-1" {
			t.Fatalf("first ClaimPending = %+v/%v/%v, want job-1", job, ok, err)
		}
		var leaseJob, acquiredAt, expiresAt string
		if err := conn.QueryRowContext(ctx, `SELECT job_id, acquired_at, expires_at FROM ingest_lease WHERE scope = 'alpha'`).Scan(&leaseJob, &acquiredAt, &expiresAt); err != nil {
			t.Fatalf("read lease: %v", err)
		}
		if leaseJob != "job-1" || acquiredAt != formatTime(now) || expiresAt != formatTime(now.Add(15*time.Minute)) {
			t.Fatalf("lease = %q/%q/%q, want admitted job and requested interval", leaseJob, acquiredAt, expiresAt)
		}
		if _, ok, err := store.ClaimPending(ctx, now.Add(time.Second), 15*time.Minute); err != nil || ok {
			t.Fatalf("second ClaimPending = %v/%v, want held scope refused", ok, err)
		}
	})

	t.Run("lease insert failure rolls back status", func(t *testing.T) {
		conn := migratedDB(t, ctx)
		defer conn.Close()
		store := NewJobStore(conn)
		saveLeaseTestJobs(t, ctx, store, Job{ID: "job-atomic", Scope: "alpha"})
		if _, err := conn.ExecContext(ctx, `CREATE TRIGGER reject_lease BEFORE INSERT ON ingest_lease BEGIN SELECT RAISE(ABORT, 'injected lease failure'); END`); err != nil {
			t.Fatalf("create failure trigger: %v", err)
		}
		if _, _, err := store.ClaimPending(ctx, now, time.Minute); err == nil {
			t.Fatal("ClaimPending succeeded, want injected lease failure")
		}
		assertLeaseTestStatus(t, ctx, conn, "job-atomic", JobPending)
		assertLeaseTestCount(t, ctx, conn, 0)
	})
}

func TestClaimPendingAllowsAnotherScopeWhileLeaseIsHeld(t *testing.T) {
	// R-P2QZ-DSI5
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	store := NewJobStore(conn)
	saveLeaseTestJobs(t, ctx, store, Job{ID: "job-alpha", Scope: "alpha"}, Job{ID: "job-beta", Scope: "beta"})

	first, ok, err := store.ClaimPending(ctx, time.Now(), time.Hour)
	if err != nil || !ok || first.ID != "job-alpha" {
		t.Fatalf("first ClaimPending = %+v/%v/%v, want alpha", first, ok, err)
	}
	second, ok, err := store.ClaimPending(ctx, time.Now(), time.Hour)
	if err != nil || !ok || second.ID != "job-beta" {
		t.Fatalf("second ClaimPending = %+v/%v/%v, want beta despite alpha lease", second, ok, err)
	}
	assertLeaseTestCount(t, ctx, conn, 2)
}

func TestTerminalTransitionsReleaseLeaseAtomicallyAndUnblockScope(t *testing.T) {
	// R-OZ3A-8HA2
	for _, test := range []struct {
		name       string
		wantStatus string
		finish     func(context.Context, *sql.DB, *JobStore, time.Time) error
	}{
		{name: "done", wantStatus: JobDone, finish: func(ctx context.Context, conn *sql.DB, _ *JobStore, now time.Time) error {
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			_, err = finishDoneInTx(ctx, tx, "job-1", now)
			return err
		}},
		{name: "failed", wantStatus: JobFailed, finish: func(ctx context.Context, _ *sql.DB, store *JobStore, now time.Time) error {
			_, err := store.FinishWorking(ctx, "job-1", JobFailed, now, "failed")
			return err
		}},
		{name: "aborted", wantStatus: JobAborted, finish: func(ctx context.Context, _ *sql.DB, store *JobStore, now time.Time) error {
			_, err := store.Abort(ctx, "job-1", now)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			conn := migratedDB(t, ctx)
			defer conn.Close()
			store := NewJobStore(conn)
			saveLeaseTestJobs(t, ctx, store, Job{ID: "job-1", Scope: "alpha"}, Job{ID: "job-2", Scope: "alpha"})
			if _, ok, err := store.ClaimPending(ctx, time.Now(), time.Hour); err != nil || !ok {
				t.Fatalf("ClaimPending = %v/%v", ok, err)
			}
			if _, err := conn.ExecContext(ctx, `CREATE TRIGGER reject_release BEFORE DELETE ON ingest_lease BEGIN SELECT RAISE(ABORT, 'injected release failure'); END`); err != nil {
				t.Fatalf("create failure trigger: %v", err)
			}
			if err := test.finish(ctx, conn, store, time.Now()); err == nil {
				t.Fatal("terminal transition succeeded, want injected release failure")
			}
			assertLeaseTestStatus(t, ctx, conn, "job-1", JobWorking)
			assertLeaseTestCount(t, ctx, conn, 1)
			if _, err := conn.ExecContext(ctx, `DROP TRIGGER reject_release`); err != nil {
				t.Fatalf("drop failure trigger: %v", err)
			}
			if err := test.finish(ctx, conn, store, time.Now()); err != nil {
				t.Fatalf("terminal transition: %v", err)
			}
			assertLeaseTestStatus(t, ctx, conn, "job-1", test.wantStatus)
			assertLeaseTestCount(t, ctx, conn, 0)
			next, ok, err := store.ClaimPending(ctx, time.Now(), time.Hour)
			if err != nil || !ok || next.ID != "job-2" {
				t.Fatalf("next ClaimPending = %+v/%v/%v, want job-2", next, ok, err)
			}
		})
	}
}

func TestRequeueWorkingReleasesInheritedLease(t *testing.T) {
	// R-P1J3-00RG
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	store := NewJobStore(conn)
	saveLeaseTestJobs(t, ctx, store, Job{ID: "job-1", Scope: "alpha"})
	if _, ok, err := store.ClaimPending(ctx, time.Now(), time.Hour); err != nil || !ok {
		t.Fatalf("ClaimPending = %v/%v", ok, err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TRIGGER reject_requeue_release BEFORE DELETE ON ingest_lease BEGIN SELECT RAISE(ABORT, 'injected release failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	if _, err := store.RequeueWorking(ctx); err == nil {
		t.Fatal("RequeueWorking succeeded, want injected release failure")
	}
	assertLeaseTestStatus(t, ctx, conn, "job-1", JobWorking)
	assertLeaseTestCount(t, ctx, conn, 1)
	if _, err := conn.ExecContext(ctx, `DROP TRIGGER reject_requeue_release`); err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	if n, err := store.RequeueWorking(ctx); err != nil || n != 1 {
		t.Fatalf("RequeueWorking = %d/%v, want one", n, err)
	}
	assertLeaseTestStatus(t, ctx, conn, "job-1", JobPending)
	assertLeaseTestCount(t, ctx, conn, 0)
	if job, ok, err := store.ClaimPending(ctx, time.Now(), time.Hour); err != nil || !ok || job.ID != "job-1" {
		t.Fatalf("ClaimPending after sweep = %+v/%v/%v, want requeued job", job, ok, err)
	}
}

func saveLeaseTestJobs(t *testing.T, ctx context.Context, store *JobStore, jobs ...Job) {
	t.Helper()
	for _, job := range jobs {
		job.Status = JobPending
		if err := store.Save(ctx, job); err != nil {
			t.Fatalf("Save %s: %v", job.ID, err)
		}
	}
}

func assertLeaseTestStatus(t *testing.T, ctx context.Context, conn *sql.DB, jobID, want string) {
	t.Helper()
	var got string
	if err := conn.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&got); err != nil {
		t.Fatalf("read job status: %v", err)
	}
	if got != want {
		t.Fatalf("job %s status = %q, want %q", jobID, got, want)
	}
}

func assertLeaseTestCount(t *testing.T, ctx context.Context, conn *sql.DB, want int) {
	t.Helper()
	var got int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM ingest_lease`).Scan(&got); err != nil {
		t.Fatalf("count leases: %v", err)
	}
	if got != want {
		t.Fatalf("lease count = %d, want %d", got, want)
	}
}
