package wiki

import (
	"context"
	"strings"
	"testing"
	"time"
	"wiki/internal/page"
)

func TestReapExpiredFailsAndCleansWaitingJobThenUnblocksScope(t *testing.T) {
	// R-PA2D-OEYB
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store := NewJobStore(db)
	saveLeaseTestJobs(t, ctx, store, Job{ID: "lost", Scope: "alpha"}, Job{ID: "next", Scope: "alpha"})
	if _, ok, err := store.ClaimPending(ctx, now.Add(-2*time.Hour), 30*time.Minute); err != nil || !ok {
		t.Fatalf("claim lost = %v/%v", ok, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET phase = 'extract', deadline_at = ? WHERE id = 'lost'`, formatTime(now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_staging(job_id, stage, unit, payload, units) VALUES ('lost','extract','','{}',1)`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nil, nil, func() time.Time { return now })
	if n, err := svc.ReapExpired(ctx, now); err != nil || n != 1 {
		t.Fatalf("ReapExpired = %d/%v", n, err)
	}
	got, err := store.Get(ctx, "lost")
	if err != nil || got.Status != JobFailed || !strings.Contains(got.Error, "completion never arrived") {
		t.Fatalf("reaped job = %+v/%v", got, err)
	}
	assertTableCount(t, ctx, db, "job_staging", 0)
	if next, ok, err := store.ClaimPending(ctx, now, time.Minute); err != nil || !ok || next.ID != "next" {
		t.Fatalf("next claim = %+v/%v/%v", next, ok, err)
	}
}

func TestProgressExtendsDeadlineButAbsoluteLifetimeStillReaps(t *testing.T) {
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store := NewJobStore(db)
	if err := store.InsertIngest(ctx, Job{ID: "progress", Scope: "default", Status: JobWorking, Phase: "compile", ReceivedAt: now.Add(-jobAbsoluteLifetime + time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := store.ExtendDeadline(ctx, "progress", now.Add(jobProgressDeadline)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nil, nil, func() time.Time { return now })
	if n, err := svc.ReapExpired(ctx, now); err != nil || n != 0 {
		t.Fatalf("progressing reap = %d/%v", n, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET received_at = ? WHERE id = 'progress'`, formatTime(now.Add(-jobAbsoluteLifetime-time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET phase = 'compile' WHERE id = 'progress'`); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ReapExpired(ctx, now); err != nil || n != 1 {
		t.Fatalf("absolute reap = %d/%v", n, err)
	}
}

func TestSurfaceReportsOnlyFiveStatusesAcrossInternalPhases(t *testing.T) {
	// R-PCI6-FYFP
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewJobStore(db)
	valid := map[string]bool{JobPending: true, JobWorking: true, JobDone: true, JobFailed: true, JobAborted: true}
	rows := []Job{{ID: "pending", Status: JobPending}, {ID: "handoff", Status: JobWorking, Phase: "extract"}, {ID: "compile", Status: JobWorking, Phase: "compile"}, {ID: "done", Status: JobDone}, {ID: "failed", Status: JobFailed}, {ID: "aborted", Status: JobAborted}}
	for _, row := range rows {
		if err := store.Save(ctx, row); err != nil {
			t.Fatal(err)
		}
		status, err := store.Status(ctx, row.ID)
		if err != nil || !valid[status.Status] || status.Status == row.Phase {
			t.Fatalf("status %s = %+v/%v", row.ID, status, err)
		}
	}
}

func TestAllFiveStatusFilterIncludesHandoffAndCountMatches(t *testing.T) {
	// R-PEXZ-7HX3
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewJobStore(db)
	for i, status := range []string{JobPending, JobWorking, JobDone, JobFailed, JobAborted} {
		if err := store.Save(ctx, Job{ID: status, Status: status, Phase: map[bool]string{true: "extract"}[i == 1]}); err != nil {
			t.Fatal(err)
		}
	}
	filter := JobFilter{Statuses: []string{JobPending, JobWorking, JobDone, JobFailed, JobAborted}}
	jobs, _, err := store.ListJobs(ctx, filter, page.Params{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	count, err := store.CountJobs(ctx, filter)
	if err != nil || len(jobs) != 5 || count != len(jobs) {
		t.Fatalf("list/count = %d/%d/%v", len(jobs), count, err)
	}
}

func TestJobStatusReportsRunningProgressAndDoneSubjects(t *testing.T) {
	// R-PDQ2-TQ6E
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewJobStore(db)
	if err := store.Save(ctx, Job{ID: "job", Status: JobWorking, Phase: "compile"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_staging(job_id,stage,unit,payload,units) VALUES ('job','extract','','{}',3),('job','compile','one','{}',3)`); err != nil {
		t.Fatal(err)
	}
	running, err := store.Status(ctx, "job")
	if err != nil || running.UnitsComplete != 1 || running.UnitsTotal != 3 {
		t.Fatalf("running = %+v/%v", running, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subjects(id,scope,type,name,norm_name) VALUES ('subject','default','entity','Subject','subject')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO claims(id,subject_id,job_id,body) VALUES ('claim','subject','job','body')`); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, "job", JobDone, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	done, err := store.Status(ctx, "job")
	if err != nil || len(done.Subjects) != 1 || done.Subjects[0] != "subject" {
		t.Fatalf("done = %+v/%v", done, err)
	}
}

func TestHealthReportsRunningAgeAndDegradesOnNoProgress(t *testing.T) {
	// R-PHDR-Z1EH
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store := NewJobStore(db)
	if err := store.Save(ctx, Job{ID: "running", Status: JobWorking}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET started_at = ? WHERE id = 'running'`, formatTime(now.Add(-90*time.Second))); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nil, nil, func() time.Time { return now })
	svc.noProgressStreak = noProgressHealthThreshold
	details, err := svc.Health(ctx)
	if err != nil || details["running_jobs"] != 1 || details["oldest_running_age_seconds"] != int64(90) || details["no_progress_streak"] != noProgressHealthThreshold || details["degraded"] != true {
		t.Fatalf("health = %#v/%v", details, err)
	}
}
