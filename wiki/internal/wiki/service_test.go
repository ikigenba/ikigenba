package wiki

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"appkit/telemetry"
	"eventplane/correlation"

	"wiki/internal/extract"
	"wiki/internal/llm"
	"wiki/internal/page"
)

func TestIngestStoresCarriedCorrelationIDAndLeavesBareContextEmpty(t *testing.T) {
	// R-XGME-DMUD
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	svc := NewService(conn, nil, nil, time.Now)
	svc.newID = sequenceIDs("job-carried", "job-bare")
	carried := "01KZ6V08B73Q7W1G5GR3C2E5MK"
	if _, err := svc.Ingest(correlation.WithContext(ctx, carried), "default", "owner", "owner@example.com", "carried", "", nil); err != nil {
		t.Fatalf("Ingest carried context: %v", err)
	}
	if _, err := svc.Ingest(ctx, "default", "owner", "owner@example.com", "bare", "", nil); err != nil {
		t.Fatalf("Ingest bare context: %v", err)
	}

	for jobID, want := range map[string]string{"job-carried": carried, "job-bare": ""} {
		var got string
		if err := conn.QueryRowContext(ctx, `SELECT correlation_id FROM jobs WHERE id = ?`, jobID).Scan(&got); err != nil {
			t.Fatalf("read correlation_id for %s: %v", jobID, err)
		}
		if got != want {
			t.Fatalf("job %s correlation_id = %q, want %q", jobID, got, want)
		}
	}
}

func TestJobContextAndAttributionUseStoredChainOrDurableJobRoot(t *testing.T) {
	// R-XJ27-56BR
	svc := &Service{recorder: &telemetry.Recorder{}}
	ctx := context.Background()
	storedID := "01KZ6V08B73Q7W1G5GR3C2E5MK"
	storedCtx, storedJob := svc.jobContext(ctx, Job{ID: "job-stored", CorrelationID: storedID})
	if got := jobAttribution(storedJob).GroupID; got != storedID {
		t.Fatalf("stored job attribution group = %q, want stored chain %q (not job id %q)", got, storedID, storedJob.ID)
	}
	if got := correlation.FromContext(storedCtx); got != storedID {
		t.Fatalf("stored job context correlation = %q, want %q", got, storedID)
	}

	firstCtx, firstJob := svc.jobContext(ctx, Job{ID: "01KZ6V08B73Q7W1G5GR3C2E5MM"})
	secondCtx, secondJob := svc.jobContext(ctx, Job{ID: "01KZ6V08B73Q7W1G5GR3C2E5MN"})
	if firstJob.CorrelationID != firstJob.ID || secondJob.CorrelationID != secondJob.ID {
		t.Fatalf("empty jobs resolved correlations = %q, %q; want durable roots %q, %q", firstJob.CorrelationID, secondJob.CorrelationID, firstJob.ID, secondJob.ID)
	}
	if got := jobAttribution(firstJob).GroupID; got != firstJob.ID {
		t.Fatalf("first job attribution group = %q, want durable job root %q", got, firstJob.ID)
	}
	if got := jobAttribution(secondJob).GroupID; got != secondJob.ID {
		t.Fatalf("second job attribution group = %q, want durable job root %q", got, secondJob.ID)
	}
	if correlation.FromContext(firstCtx) != firstJob.ID || correlation.FromContext(secondCtx) != secondJob.ID {
		t.Fatalf("derived contexts = %q, %q; want durable job roots %q, %q", correlation.FromContext(firstCtx), correlation.FromContext(secondCtx), firstJob.ID, secondJob.ID)
	}
}

func TestIngestReturnsJobIDFromPendingInsertWithoutExtraction(t *testing.T) {
	// R-M8RN-87WV
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	fixed := time.Date(2026, 6, 20, 20, 30, 0, 0, time.UTC)
	extractor := &recordingExtractor{}
	svc := NewService(conn, extractor, &recordingCompiler{}, func() time.Time { return fixed })
	svc.newID = sequenceIDs("job-1")

	jobID, err := svc.Ingest(ctx, "default", "owner-id", " owner@example.com ", "Acme Robotics opened a lab.", " Lab notes ", []string{"robotics"})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if jobID != "job-1" {
		t.Fatalf("jobID = %q, want job-1", jobID)
	}
	if extractor.calls != 0 {
		t.Fatalf("extractor calls = %d, want 0 on request path", extractor.calls)
	}

	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobPending {
		t.Fatalf("status = %q, want pending", status.Status)
	}
	if !status.ReceivedAt.Equal(fixed) {
		t.Fatalf("received_at = %v, want %v", status.ReceivedAt, fixed)
	}
	if status.StartedAt != nil || status.FinishedAt != nil || len(status.Subjects) != 0 {
		t.Fatalf("status = %+v, want pending job without worker fields or subjects", status)
	}
}

func TestProcessNextMarksFailedJobStatusOnExtractError(t *testing.T) {
	// R-MG31-IUD1
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	times := sequenceTimes(
		time.Date(2026, 6, 20, 20, 31, 0, 0, time.UTC),
		time.Date(2026, 6, 20, 20, 31, 1, 0, time.UTC),
		time.Date(2026, 6, 20, 20, 31, 2, 0, time.UTC),
	)
	svc := NewService(conn, &recordingExtractor{err: errors.New("extract exploded")}, &recordingCompiler{}, times)
	svc.newID = sequenceIDs("job-1")

	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "bad source", "Bad source", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	processed, err := svc.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext returned error: %v", err)
	}
	if !processed {
		t.Fatal("ProcessNext processed = false, want true for pending job")
	}

	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobFailed {
		t.Fatalf("status = %q, want failed", status.Status)
	}
	if status.StartedAt == nil || status.FinishedAt == nil {
		t.Fatalf("status = %+v, want started and finished timestamps", status)
	}
	if !strings.Contains(status.Error, "extract exploded") {
		t.Fatalf("error = %q, want extract failure", status.Error)
	}
	if len(status.Subjects) != 0 {
		t.Fatalf("subjects = %#v, want none on failed extract", status.Subjects)
	}
}

func TestProcessNextReusesSubjectAndRecompilesFromCompleteClaims(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{
		{{
			Type:   "entity",
			Kind:   "company",
			Name:   "Acme Robotics",
			Claims: []string{"Acme Robotics opened a Tulsa lab."},
		}},
		{{
			Type:   "entity",
			Kind:   "company",
			Name:   " ACME   ROBOTICS ",
			Claims: []string{"Acme Robotics hired Mira Patel."},
		}},
	}}
	compiler := &recordingCompiler{}
	svc := NewService(conn, extractor, compiler, sequenceTimes(
		time.Date(2026, 6, 20, 20, 32, 0, 0, time.UTC),
		time.Date(2026, 6, 20, 20, 32, 1, 0, time.UTC),
		time.Date(2026, 6, 20, 20, 32, 2, 0, time.UTC),
		time.Date(2026, 6, 20, 20, 32, 3, 0, time.UTC),
		time.Date(2026, 6, 20, 20, 32, 4, 0, time.UTC),
		time.Date(2026, 6, 20, 20, 32, 5, 0, time.UTC),
	))
	svc.newID = sequenceIDs("job-1", "subject-1", "claim-1", "job-2", "claim-2")

	if _, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "source one", "One", nil); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("first ProcessNext = %v/%v, want true/nil", processed, err)
	}
	if _, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "source two", "Two", nil); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("second ProcessNext = %v/%v, want true/nil", processed, err)
	}

	if len(compiler.claimSets) != 2 {
		t.Fatalf("compile calls = %d, want 2", len(compiler.claimSets))
	}
	secondClaims := compiler.claimSets[1]
	if len(secondClaims) != 2 {
		t.Fatalf("second compile claims = %#v, want complete two-claim set", secondClaims)
	}
	if secondClaims[0].Body != "Acme Robotics opened a Tulsa lab." ||
		secondClaims[1].Body != "Acme Robotics hired Mira Patel." {
		t.Fatalf("second compile claims = %#v, want original plus new claim", secondClaims)
	}

	page, err := NewPageStore(conn).Get(ctx, "subject-1")
	if err != nil {
		t.Fatalf("Get page: %v", err)
	}
	if !strings.Contains(page.Body, "Acme Robotics hired Mira Patel.") {
		t.Fatalf("page body = %q, want recompiled body with latest claim", page.Body)
	}
}

func TestProcessNextCompilesFromSubjectIdentityAndClaimsOnly(t *testing.T) {
	// R-MB7F-ZRE9
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	const stalePageMarker = "STALE PAGE BODY MUST NOT REENTER COMPILE"
	if err := NewSubjectStore(conn).Save(ctx, Subject{
		ID:       "subject-1",
		Name:     "Acme Robotics",
		NormName: Normalize("Acme Robotics"),
		Type:     "entity",
	}); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	if err := NewClaimStore(conn).Save(ctx, Claim{
		ID:        "claim-old",
		SubjectID: "subject-1",
		JobID:     "job-old",
		Body:      "older retained claim",
	}); err != nil {
		t.Fatalf("Save old claim: %v", err)
	}
	if err := NewPageStore(conn).Upsert(ctx, Page{
		ID:        "subject-1",
		SubjectID: "subject-1",
		Title:     "Old page",
		Body:      stalePageMarker,
	}); err != nil {
		t.Fatalf("Save stale page: %v", err)
	}

	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{{
		{
			Type:   "entity",
			Kind:   "company",
			Name:   "Acme Robotics",
			Claims: []string{"new extracted claim"},
		},
	}}}
	compiler := &recordingCompiler{}
	svc := NewService(conn, extractor, compiler, sequenceTimes(
		time.Date(2026, 6, 22, 8, 9, 0, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 9, 1, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 9, 2, 0, time.UTC),
	))
	svc.newID = sequenceIDs("job-1", "claim-new")

	if _, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "Acme source", "Acme", nil); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("ProcessNext = %v/%v, want true/nil", processed, err)
	}

	if len(compiler.subjects) != 1 || compiler.subjects[0].ID != "subject-1" || compiler.subjects[0].Name != "Acme Robotics" {
		t.Fatalf("compiled subjects = %+v, want existing subject identity", compiler.subjects)
	}
	if len(compiler.claimSets) != 1 || len(compiler.claimSets[0]) != 2 {
		t.Fatalf("compiled claims = %+v, want old and new claims only", compiler.claimSets)
	}
	for _, claim := range compiler.claimSets[0] {
		if strings.Contains(claim.Body, stalePageMarker) {
			t.Fatalf("compiled claim body = %q, contains stale page body", claim.Body)
		}
	}
	page, err := NewPageStore(conn).GetBySubject(ctx, "subject-1")
	if err != nil {
		t.Fatalf("GetBySubject: %v", err)
	}
	if strings.Contains(page.Body, stalePageMarker) {
		t.Fatalf("page body = %q, want recompiled body without stale page input", page.Body)
	}
}

func TestRerunRefreshesPagesFTSForRewrittenPage(t *testing.T) {
	// R-22JI-6KW7
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{
		{{
			Type:   "entity",
			Kind:   "company",
			Name:   "Acme Robotics",
			Claims: []string{"Acme Robotics opened a Tulsa lab."},
		}},
		{{
			Type:   "entity",
			Kind:   "company",
			Name:   "Acme Robotics",
			Claims: []string{"Acme Robotics opened an Austin lab."},
		}},
	}}
	compiler := &recordingCompiler{}
	svc := NewService(conn, extractor, compiler, sequenceTimes(
		time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 21, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 6, 21, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 6, 21, 10, 0, 3, 0, time.UTC),
		time.Date(2026, 6, 21, 10, 0, 4, 0, time.UTC),
	))
	svc.newID = sequenceIDs("job-1", "subject-1", "claim-1", "claim-2")

	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "Acme Robotics opened a Tulsa lab.", "Tulsa lab", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	processed, err := svc.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatal("ProcessNext processed = false, want true")
	}
	assertPagesFTSMatchCount(t, ctx, conn, `"Tulsa"`, 1)

	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobDone || len(status.Subjects) != 1 || status.Subjects[0] != "subject-1" {
		t.Fatalf("status = %+v, want done with subject-1", status)
	}
	if _, err := svc.Rerun(ctx, jobID); err != nil {
		t.Fatalf("Rerun: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("rerun ProcessNext = %v/%v, want true/nil", processed, err)
	}
	assertPagesFTSMatchCount(t, ctx, conn, `"Tulsa"`, 0)
	assertPagesFTSMatchCount(t, ctx, conn, `"Austin"`, 1)
}

func TestAbortPendingJobMarksAbortedAndPreventsProcessing(t *testing.T) {
	// R-0SCX-95OZ
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	extractor := &recordingExtractor{}
	svc := NewService(conn, extractor, &recordingCompiler{}, clockAt(now))
	svc.newID = sequenceIDs("job-1")

	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "Acme Robotics opened a lab.", "Lab", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	result, err := svc.Abort(ctx, jobID)
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if !result.Aborted || result.Status != JobAborted {
		t.Fatalf("Abort result = %+v, want aborted status", result)
	}

	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobAborted || status.FinishedAt == nil {
		t.Fatalf("status = %+v, want aborted with finished_at", status)
	}
	processed, err := svc.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if processed {
		t.Fatal("ProcessNext processed = true, want aborted pending job skipped")
	}
	if extractor.calls != 0 {
		t.Fatalf("extractor calls = %d, want 0 after abort", extractor.calls)
	}
}

func TestAbortTerminalJobLeavesStatusUnchanged(t *testing.T) {
	// R-0TKT-MXFO
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	svc := NewService(conn, &recordingExtractor{}, &recordingCompiler{}, clockAt(time.Date(2026, 6, 22, 8, 1, 0, 0, time.UTC)))
	svc.newID = sequenceIDs("job-1")
	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "empty source", "Empty", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("ProcessNext = %v/%v, want true/nil", processed, err)
	}

	result, err := svc.Abort(ctx, jobID)
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if result.Aborted || result.Status != JobDone {
		t.Fatalf("Abort result = %+v, want unchanged done status", result)
	}
	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobDone {
		t.Fatalf("status = %q, want done", status.Status)
	}
}

func TestAbortWorkingJobIsNotOverwrittenByWorkerFinish(t *testing.T) {
	// R-0USQ-0P6D
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	extractor := &blockingExtractor{
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		canceled: make(chan struct{}),
	}
	svc := NewService(conn, extractor, &recordingCompiler{}, clockAt(time.Date(2026, 6, 22, 8, 2, 0, 0, time.UTC)))
	svc.newID = sequenceIDs("job-1")
	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "Acme Robotics opened a lab.", "Lab", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	type processResult struct {
		processed bool
		err       error
	}
	done := make(chan processResult, 1)
	go func() {
		processed, err := svc.ProcessNext(ctx)
		done <- processResult{processed: processed, err: err}
	}()

	select {
	case <-extractor.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor was not entered")
	}
	result, err := svc.Abort(ctx, jobID)
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if !result.Aborted || result.Status != JobAborted {
		t.Fatalf("Abort result = %+v, want working job aborted", result)
	}
	select {
	case <-extractor.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor context was not canceled by abort")
	}

	select {
	case got := <-done:
		if got.err != nil || !got.processed {
			t.Fatalf("ProcessNext = %v/%v, want true/nil", got.processed, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ProcessNext did not return")
	}

	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobAborted || status.StartedAt == nil || status.FinishedAt == nil {
		t.Fatalf("status = %+v, want aborted working job with lifecycle timestamps", status)
	}
	assertTableCount(t, ctx, conn, "subjects", 0)
	assertTableCount(t, ctx, conn, "claims", 0)
	assertTableCount(t, ctx, conn, "pages", 0)
}

func TestProcessNextRollsBackIntegratedRowsWhenCompileFails(t *testing.T) {
	// R-0W0M-EGX2
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{{
		{
			Type:   "entity",
			Kind:   "company",
			Name:   "Acme Robotics",
			Claims: []string{"Acme Robotics opened a Tulsa lab."},
		},
	}}}
	compiler := &recordingCompiler{err: errors.New("compile exploded")}
	svc := NewService(conn, extractor, compiler, sequenceTimes(
		time.Date(2026, 6, 22, 8, 3, 0, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 3, 1, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 3, 2, 0, time.UTC),
	))
	svc.newID = sequenceIDs("job-1", "subject-1", "claim-1")

	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "Acme Robotics opened a lab.", "Lab", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	processed, err := svc.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatal("ProcessNext processed = false, want true")
	}

	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobFailed || !strings.Contains(status.Error, "compile exploded") {
		t.Fatalf("status = %+v, want failed with compile error", status)
	}
	assertTableCount(t, ctx, conn, "subjects", 0)
	assertTableCount(t, ctx, conn, "claims", 0)
	assertTableCount(t, ctx, conn, "pages", 0)
}

func TestRerunTerminalJobRequeuesAndUsesOriginalSourceText(t *testing.T) {
	// R-0X8I-S8NR
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	extractor := &recordingExtractor{}
	svc := NewService(conn, extractor, &recordingCompiler{}, sequenceTimes(
		time.Date(2026, 6, 22, 8, 4, 0, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 4, 1, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 4, 2, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 4, 3, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 4, 4, 0, time.UTC),
	))
	svc.newID = sequenceIDs("job-1")

	source := "Acme Robotics opened a lab from the original source."
	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", source, "Original title", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("first ProcessNext = %v/%v, want true/nil", processed, err)
	}

	result, err := svc.Rerun(ctx, jobID)
	if err != nil {
		t.Fatalf("Rerun: %v", err)
	}
	if !result.Requeued || result.Status != JobPending {
		t.Fatalf("Rerun result = %+v, want requeued pending", result)
	}
	pending, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus after Rerun: %v", err)
	}
	if pending.Status != JobPending || pending.StartedAt != nil || pending.FinishedAt != nil || pending.Error != "" {
		t.Fatalf("status after Rerun = %+v, want clean pending job", pending)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("second ProcessNext = %v/%v, want true/nil", processed, err)
	}
	if len(extractor.texts) != 2 || extractor.texts[0] != source || extractor.texts[1] != source {
		t.Fatalf("extractor texts = %#v, want original source used for both runs", extractor.texts)
	}
}

func TestRerunReplacesJobClaimsAndRecompilesPage(t *testing.T) {
	// R-0YGF-60EG
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{
		{{
			Type:   "entity",
			Kind:   "company",
			Name:   "Acme Robotics",
			Claims: []string{"old claim from first run"},
		}},
		{{
			Type:   "entity",
			Kind:   "company",
			Name:   "Acme Robotics",
			Claims: []string{"new claim from rerun"},
		}},
	}}
	compiler := &recordingCompiler{}
	svc := NewService(conn, extractor, compiler, sequenceTimes(
		time.Date(2026, 6, 22, 8, 5, 0, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 5, 1, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 5, 2, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 5, 3, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 5, 4, 0, time.UTC),
	))
	svc.newID = sequenceIDs("job-1", "subject-1", "claim-1", "claim-2")

	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "Acme source", "Acme", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("first ProcessNext = %v/%v, want true/nil", processed, err)
	}
	if _, err := svc.Rerun(ctx, jobID); err != nil {
		t.Fatalf("Rerun: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("second ProcessNext = %v/%v, want true/nil", processed, err)
	}

	claims, _, err := NewClaimStore(conn).ListBySubject(ctx, "subject-1", page.Params{})
	if err != nil {
		t.Fatalf("ListBySubject: %v", err)
	}
	if len(claims) != 1 || claims[0].JobID != jobID || claims[0].Body != "new claim from rerun" {
		t.Fatalf("claims after rerun = %+v, want exactly the new job claim", claims)
	}
	page, err := NewPageStore(conn).GetBySubject(ctx, "subject-1")
	if err != nil {
		t.Fatalf("GetBySubject: %v", err)
	}
	if strings.Contains(page.Body, "old claim") || !strings.Contains(page.Body, "new claim from rerun") {
		t.Fatalf("page body = %q, want recompiled page with new claim only", page.Body)
	}
}

func TestRerunRefreshesSubjectsDroppedByNewExtraction(t *testing.T) {
	// R-0ZOB-JS55
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{
		{
			{Type: "entity", Kind: "company", Name: "Alpha Co", Claims: []string{"Alpha old claim"}},
			{Type: "entity", Kind: "company", Name: "Beta Co", Claims: []string{"Beta claim from first job"}},
			{Type: "concept", Kind: "concept", Name: "Dropped Concept", Claims: []string{"Dropped claim from first job"}},
		},
		{
			{Type: "entity", Kind: "company", Name: "Beta Co", Claims: []string{"Beta kept by another job"}},
		},
		{
			{Type: "entity", Kind: "company", Name: "Alpha Co", Claims: []string{"Alpha rerun claim"}},
		},
	}}
	svc := NewService(conn, extractor, &recordingCompiler{}, sequenceTimes(
		time.Date(2026, 6, 22, 8, 6, 0, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 6, 1, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 6, 2, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 6, 3, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 6, 4, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 6, 5, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 6, 6, 0, time.UTC),
		time.Date(2026, 6, 22, 8, 6, 7, 0, time.UTC),
	))
	svc.newID = sequenceIDs(
		"job-1", "subject-alpha", "claim-alpha-1", "subject-beta", "claim-beta-1", "subject-dropped", "claim-dropped-1",
		"job-2", "claim-beta-2",
		"claim-alpha-2",
	)

	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "first source", "First", nil)
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("first ProcessNext = %v/%v, want true/nil", processed, err)
	}
	if _, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "second source", "Second", nil); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("second ProcessNext = %v/%v, want true/nil", processed, err)
	}
	if _, err := svc.Rerun(ctx, jobID); err != nil {
		t.Fatalf("Rerun: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("rerun ProcessNext = %v/%v, want true/nil", processed, err)
	}

	betaClaims, _, err := NewClaimStore(conn).ListBySubject(ctx, "subject-beta", page.Params{})
	if err != nil {
		t.Fatalf("ListBySubject beta: %v", err)
	}
	if len(betaClaims) != 1 || betaClaims[0].JobID != "job-2" || betaClaims[0].Body != "Beta kept by another job" {
		t.Fatalf("beta claims = %+v, want only the other job's retained claim", betaClaims)
	}
	betaPage, err := NewPageStore(conn).GetBySubject(ctx, "subject-beta")
	if err != nil {
		t.Fatalf("GetBySubject beta: %v", err)
	}
	if strings.Contains(betaPage.Body, "first job") || !strings.Contains(betaPage.Body, "Beta kept by another job") {
		t.Fatalf("beta page body = %q, want reduced claim set", betaPage.Body)
	}
	if _, err := NewPageStore(conn).GetBySubject(ctx, "subject-dropped"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("dropped page lookup err = %v, want sql.ErrNoRows", err)
	}
	if _, err := NewSubjectStore(conn).Get(ctx, "subject-dropped"); err != nil {
		t.Fatalf("dropped subject row was not retained: %v", err)
	}
}

func TestRerunRefusesInProgressJobsWithoutChangingStatus(t *testing.T) {
	// R-10W7-XJVU
	t.Run("pending", func(t *testing.T) {
		ctx := context.Background()
		conn := migratedDB(t, ctx)
		defer conn.Close()

		svc := NewService(conn, &recordingExtractor{}, &recordingCompiler{}, clockAt(time.Date(2026, 6, 22, 8, 7, 0, 0, time.UTC)))
		svc.newID = sequenceIDs("job-1")
		jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "source", "Title", nil)
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		result, err := svc.Rerun(ctx, jobID)
		if !errors.Is(err, ErrJobNotTerminal) {
			t.Fatalf("Rerun err = %v, want ErrJobNotTerminal", err)
		}
		if result.Requeued || result.Status != JobPending {
			t.Fatalf("Rerun result = %+v, want unchanged pending", result)
		}
		status, err := svc.JobStatus(ctx, jobID)
		if err != nil {
			t.Fatalf("JobStatus: %v", err)
		}
		if status.Status != JobPending {
			t.Fatalf("status = %q, want pending", status.Status)
		}
	})

	t.Run("working", func(t *testing.T) {
		ctx := context.Background()
		conn := migratedDB(t, ctx)
		defer conn.Close()

		extractor := &blockingExtractor{
			entered: make(chan struct{}),
			release: make(chan struct{}),
		}
		svc := NewService(conn, extractor, &recordingCompiler{}, clockAt(time.Date(2026, 6, 22, 8, 8, 0, 0, time.UTC)))
		svc.newID = sequenceIDs("job-1")
		jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "source", "Title", nil)
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}

		done := make(chan error, 1)
		go func() {
			_, err := svc.ProcessNext(ctx)
			done <- err
		}()
		select {
		case <-extractor.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("extractor was not entered")
		}

		result, err := svc.Rerun(ctx, jobID)
		if !errors.Is(err, ErrJobNotTerminal) {
			t.Fatalf("Rerun err = %v, want ErrJobNotTerminal", err)
		}
		if result.Requeued || result.Status != JobWorking {
			t.Fatalf("Rerun result = %+v, want unchanged working", result)
		}
		status, err := svc.JobStatus(ctx, jobID)
		if err != nil {
			t.Fatalf("JobStatus: %v", err)
		}
		if status.Status != JobWorking {
			t.Fatalf("status = %q, want working", status.Status)
		}

		close(extractor.release)
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("ProcessNext returned error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("ProcessNext did not return")
		}
	})
}

func TestServiceListsSubjectsAndReadsClaimsAndPagesBySubject(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	svc := NewService(conn, nil, nil, nil)
	subjects := NewSubjectStore(conn)
	claims := NewClaimStore(conn)
	pages := NewPageStore(conn)
	if err := subjects.Save(ctx, Subject{ID: "subject-1", Name: "Acme Robotics", Type: "entity"}); err != nil {
		t.Fatalf("Save subject-1: %v", err)
	}
	if err := subjects.Save(ctx, Subject{ID: "subject-2", Name: "Acme Launch", Type: "event"}); err != nil {
		t.Fatalf("Save subject-2: %v", err)
	}
	if err := claims.Save(ctx, Claim{ID: "claim-1", SubjectID: "subject-1", JobID: "job-1", Body: "Acme Robotics opened a lab."}); err != nil {
		t.Fatalf("Save claim: %v", err)
	}
	if err := pages.Upsert(ctx, Page{ID: "page-1", SubjectID: "subject-1", Title: "Acme Robotics", Body: "Acme Robotics opened a lab."}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}

	gotSubjects, err := svc.Subjects(ctx, "entity", "robot")
	if err != nil {
		t.Fatalf("Subjects: %v", err)
	}
	if len(gotSubjects) != 1 || gotSubjects[0].ID != "subject-1" {
		t.Fatalf("Subjects = %+v, want subject-1 only", gotSubjects)
	}
	gotClaims, err := svc.ClaimsBySubject(ctx, "subject-1")
	if err != nil {
		t.Fatalf("ClaimsBySubject: %v", err)
	}
	if len(gotClaims) != 1 || gotClaims[0].ID != "claim-1" {
		t.Fatalf("ClaimsBySubject = %+v, want claim-1", gotClaims)
	}
	gotPage, err := svc.PageBySubject(ctx, "subject-1")
	if err != nil {
		t.Fatalf("PageBySubject: %v", err)
	}
	if gotPage.ID != "page-1" || gotPage.Title != "Acme Robotics" {
		t.Fatalf("PageBySubject = %+v, want page-1", gotPage)
	}
}

type recordingExtractor struct {
	calls   int
	err     error
	headers []extract.DocumentHeader
	texts   []string
	batches [][]extract.ExtractedSubject
}

func (e *recordingExtractor) Extract(_ context.Context, _ llm.Attribution, h extract.DocumentHeader, text string) ([]extract.ExtractedSubject, error) {
	e.calls++
	e.headers = append(e.headers, h)
	e.texts = append(e.texts, text)
	if e.err != nil {
		return nil, e.err
	}
	if len(e.batches) == 0 {
		return nil, nil
	}
	out := e.batches[0]
	e.batches = e.batches[1:]
	return out, nil
}

type blockingExtractor struct {
	entered  chan struct{}
	release  chan struct{}
	canceled chan struct{}
}

func (e *blockingExtractor) Extract(ctx context.Context, _ llm.Attribution, _ extract.DocumentHeader, _ string) ([]extract.ExtractedSubject, error) {
	close(e.entered)
	select {
	case <-e.release:
		return nil, nil
	case <-ctx.Done():
		if e.canceled != nil {
			close(e.canceled)
		}
		return nil, ctx.Err()
	}
}

type recordingCompiler struct {
	subjects  []Subject
	claimSets [][]Claim
	err       error
}

func (c *recordingCompiler) Compile(_ context.Context, _ llm.Attribution, subject Subject, claims []Claim) (string, string, error) {
	c.subjects = append(c.subjects, subject)
	copied := append([]Claim(nil), claims...)
	c.claimSets = append(c.claimSets, copied)
	if c.err != nil {
		return "", "", c.err
	}
	var bodies []string
	for _, claim := range claims {
		bodies = append(bodies, claim.Body)
	}
	return subject.Name, strings.Join(bodies, "\n"), nil
}

func sequenceIDs(ids ...string) func() string {
	i := 0
	return func() string {
		if i >= len(ids) {
			return "extra-id"
		}
		id := ids[i]
		i++
		return id
	}
}

func sequenceTimes(times ...time.Time) func() time.Time {
	i := 0
	return func() time.Time {
		if i >= len(times) {
			return times[len(times)-1]
		}
		t := times[i]
		i++
		return t
	}
}

func clockAt(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func assertPagesFTSMatchCount(t *testing.T, ctx context.Context, conn interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, match string, want int) {
	t.Helper()

	var got int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pages_fts WHERE pages_fts MATCH ?`, match).
		Scan(&got); err != nil {
		t.Fatalf("count pages_fts matches for %q: %v", match, err)
	}
	if got != want {
		t.Fatalf("pages_fts matches for %q = %d, want %d", match, got, want)
	}
}

func TestIngestIntegratesOnlyIntoNamedScope(t *testing.T) {
	// R-GXIJ-YZV5
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	for _, scope := range []string{"s1", "s2"} {
		if _, err := NewScopeStore(conn).Create(ctx, scope); err != nil {
			t.Fatalf("Create scope %s: %v", scope, err)
		}
	}
	subjects := NewSubjectStore(conn)
	pages := NewPageStore(conn)
	if err := subjects.Save(ctx, "s2", Subject{ID: "s2-acme", Name: "Acme Robotics", Type: "entity"}); err != nil {
		t.Fatalf("Save s2 subject: %v", err)
	}
	if err := pages.Upsert(ctx, Page{ID: "s2-acme", SubjectID: "s2-acme", Title: "S2 original", Body: "s2 page sentinel"}); err != nil {
		t.Fatalf("Upsert s2 page: %v", err)
	}
	if err := NewClaimStore(conn).Save(ctx, Claim{ID: "s2-old-claim", SubjectID: "s2-acme", JobID: "seed", Body: "s2 old claim"}); err != nil {
		t.Fatalf("Save s2 claim: %v", err)
	}

	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{
		{{Name: "Acme Robotics", Type: "entity", Claims: []string{"s1 claim"}}},
		{{Name: "Acme Robotics", Type: "entity", Claims: []string{"s2 new claim"}}},
	}}
	svc := NewService(conn, extractor, &recordingCompiler{}, time.Now)
	if _, err := svc.Ingest(ctx, "s1", "owner", "owner@example.com", "s1 source", "s1", nil); err != nil {
		t.Fatalf("Ingest s1: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("ProcessNext s1 = %v, %v", processed, err)
	}
	unchanged, err := pages.GetBySubject(ctx, "s2-acme")
	if err != nil || unchanged.Body != "s2 page sentinel" {
		t.Fatalf("s2 page after s1 ingest = %+v, %v; want byte-identical sentinel", unchanged, err)
	}
	s1Subject, err := subjects.GetByNormName(ctx, "s1", "Acme Robotics")
	if err != nil {
		t.Fatalf("Get s1 subject: %v", err)
	}
	if s1Subject.ID == "s2-acme" {
		t.Fatal("same-named subjects share an id across scopes")
	}
	if got, err := pages.GetBySubject(ctx, s1Subject.ID); err != nil || got.Body != "s1 claim" {
		t.Fatalf("s1 page = %+v, %v; want s1 claim", got, err)
	}

	if _, err := svc.Ingest(ctx, "s2", "owner", "owner@example.com", "s2 source", "s2", nil); err != nil {
		t.Fatalf("Ingest s2: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("ProcessNext s2 = %v, %v", processed, err)
	}
	updated, err := pages.GetBySubject(ctx, "s2-acme")
	if err != nil || !strings.Contains(updated.Body, "s2 old claim") || !strings.Contains(updated.Body, "s2 new claim") || strings.Contains(updated.Body, "s1 claim") {
		t.Fatalf("s2 page after s2 ingest = %+v, %v; want only s2 claims", updated, err)
	}
	if got, err := pages.GetBySubject(ctx, s1Subject.ID); err != nil || got.Body != "s1 claim" {
		t.Fatalf("s1 page after s2 ingest = %+v, %v; want unchanged s1 page", got, err)
	}
}

func TestSuccessfulIntegrateInvalidatesOnlyTheJobsScope(t *testing.T) {
	// R-0DQI-XKD5
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	for _, scope := range []string{"s1", "s2"} {
		if _, err := NewScopeStore(conn).Create(ctx, scope); err != nil {
			t.Fatalf("Create(%s): %v", scope, err)
		}
	}

	cache := map[string]string{"s1": "old s1 answer", "s2": "old s2 answer"}
	computes := map[string]int{}
	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{{{
		Name: "Acme Robotics", Type: "entity", Claims: []string{"new s1 content"},
	}}}}
	svc := NewService(conn, extractor, &recordingCompiler{}, time.Now)
	svc.AskInvalidate = func(scope string) { delete(cache, scope) }
	if _, err := svc.Ingest(ctx, "s1", "owner", "owner@example.com", "source", "title", nil); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("ProcessNext = %v, %v; want true, nil", processed, err)
	}
	ask := func(scope string) string {
		if answer, ok := cache[scope]; ok {
			return answer
		}
		computes[scope]++
		subject, err := NewSubjectStore(conn).GetByNormName(ctx, scope, "Acme Robotics")
		if err != nil {
			t.Fatalf("GetByNormName(%s): %v", scope, err)
		}
		page, err := NewPageStore(conn).GetBySubject(ctx, subject.ID)
		if err != nil {
			t.Fatalf("GetBySubject(%s): %v", scope, err)
		}
		cache[scope] = page.Body
		return page.Body
	}
	if got := ask("s1"); got != "new s1 content" || computes["s1"] != 1 {
		t.Fatalf("next s1 ask = %q with %d computes, want new content from one recomputation", got, computes["s1"])
	}
	if got := ask("s2"); got != "old s2 answer" || computes["s2"] != 0 {
		t.Fatalf("s2 cached answer = %q, want untouched hit", got)
	}
}

func assertTableCount(t *testing.T, ctx context.Context, conn interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table string, want int) {
	t.Helper()

	var got int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
