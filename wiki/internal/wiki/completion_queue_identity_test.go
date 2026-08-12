package wiki

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"wiki/internal/extract"
)

func TestStagedPlansMintSharedSubjectIdentityOnlyDuringIntegration(t *testing.T) {
	// R-ORRV-XUTW
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	jobs := NewJobStore(conn)
	for _, id := range []string{"job-a", "job-b"} {
		if err := jobs.Save(ctx, Job{ID: id, Scope: "default", Status: JobWorking, Phase: "extract"}); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}
	svc := NewService(conn, nil, nil, time.Now)
	svc.newID = sequenceIDs("claim-a", "claim-b", "subject-shared")
	extracted := []extract.ExtractedSubject{{Type: "entity", Name: "Shared Subject", Claims: []string{"shared fact"}}}
	planA, err := svc.stageExtractPlan(ctx, Job{ID: "job-a", Scope: "default"}, extracted)
	if err != nil {
		t.Fatalf("stage job-a: %v", err)
	}
	planB, err := svc.stageExtractPlan(ctx, Job{ID: "job-b", Scope: "default"}, extracted)
	if err != nil {
		t.Fatalf("stage job-b: %v", err)
	}
	for _, jobID := range []string{"job-a", "job-b"} {
		var raw string
		if err := conn.QueryRowContext(ctx, `SELECT payload FROM job_staging WHERE job_id = ? AND stage = 'extract'`, jobID).Scan(&raw); err != nil {
			t.Fatalf("read staged %s: %v", jobID, err)
		}
		var payload struct {
			NewSubjects []map[string]any `json:"new_subjects"`
			Claims      []map[string]any `json:"claims"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("decode staged %s: %v", jobID, err)
		}
		if len(payload.NewSubjects) != 1 || payload.NewSubjects[0]["norm_name"] != "shared-subject" {
			t.Fatalf("staged new subjects for %s = %#v", jobID, payload.NewSubjects)
		}
		if _, hasID := payload.NewSubjects[0]["id"]; hasID {
			t.Fatalf("staged new subject for %s minted an id: %#v", jobID, payload.NewSubjects[0])
		}
		if len(payload.Claims) != 1 || payload.Claims[0]["norm_name"] != "shared-subject" {
			t.Fatalf("staged claims for %s = %#v, want norm_name identity", jobID, payload.Claims)
		}
		if _, hasSubjectID := payload.Claims[0]["subject_id"]; hasSubjectID {
			t.Fatalf("staged claim for %s carries subject_id: %#v", jobID, payload.Claims[0])
		}
		stageCompiledPage(t, ctx, conn, jobID, "shared-subject")
	}
	if err := svc.integrateStaged(ctx, Job{ID: "job-a", Scope: "default"}, planA); err != nil {
		t.Fatalf("integrate job-a: %v", err)
	}
	if err := svc.integrateStaged(ctx, Job{ID: "job-b", Scope: "default"}, planB); err != nil {
		t.Fatalf("integrate job-b: %v", err)
	}
	var subjects, claims int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM subjects WHERE norm_name = 'shared-subject'`).Scan(&subjects); err != nil {
		t.Fatalf("count subjects: %v", err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM claims c JOIN subjects s ON s.id = c.subject_id WHERE s.norm_name = 'shared-subject'`).Scan(&claims); err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if subjects != 1 || claims != 2 {
		t.Fatalf("integrated rows = %d subjects/%d claims, want 1/2", subjects, claims)
	}
}

func TestStagedIntegrationResolvesMergedAliasAndFailsStaleNameAtomically(t *testing.T) {
	// R-OSZS-BMKL
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	if err := subjects.Save(ctx, Subject{ID: "subject-former", Name: "Former Name", Type: "entity"}); err != nil {
		t.Fatalf("Save former: %v", err)
	}
	if err := subjects.Save(ctx, Subject{ID: "subject-survivor", Name: "Survivor", Type: "entity"}); err != nil {
		t.Fatalf("Save survivor: %v", err)
	}
	jobs := NewJobStore(conn)
	for id, phase := range map[string]string{"job-alias": "extract", "job-stale": "compile"} {
		if err := jobs.Save(ctx, Job{ID: id, Scope: "default", Status: JobWorking, Phase: phase}); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}
	svc := NewService(conn, nil, nil, time.Now)
	svc.newID = sequenceIDs("claim-alias")
	aliasPlan, err := svc.stageExtractPlan(ctx, Job{ID: "job-alias", Scope: "default"}, []extract.ExtractedSubject{{
		Type: "entity", Name: "Former Name", Claims: []string{"fact after merge"},
	}})
	if err != nil {
		t.Fatalf("stage alias plan: %v", err)
	}
	stageCompiledPage(t, ctx, conn, "job-alias", "former-name")
	if err := NewAliasStore(conn).Insert(ctx, Alias{NormName: "former-name", Name: "Former Name", SubjectID: "subject-survivor", CreatedAt: "2026-08-12T00:00:00Z"}); err != nil {
		t.Fatalf("Insert merge alias: %v", err)
	}
	if err := subjects.Delete(ctx, "subject-former"); err != nil {
		t.Fatalf("Delete merged subject: %v", err)
	}
	if err := svc.integrateStaged(ctx, Job{ID: "job-alias", Scope: "default"}, aliasPlan); err != nil {
		t.Fatalf("integrate alias plan: %v", err)
	}
	var attached string
	if err := conn.QueryRowContext(ctx, `SELECT subject_id FROM claims WHERE job_id = 'job-alias'`).Scan(&attached); err != nil || attached != "subject-survivor" {
		t.Fatalf("alias claim subject = %q, %v; want subject-survivor", attached, err)
	}

	stalePlan := stagedIntegration{
		Claims: []stagedClaim{{ID: "claim-stale", NormName: "vanished-name", JobID: "job-stale", Body: "must not persist"}},
		Pages:  []stagedPage{{Subject: stagedSubject{NormName: "vanished-name", Name: "Vanished Name", Type: "entity"}, Claims: []stagedClaim{{ID: "claim-stale", NormName: "vanished-name", JobID: "job-stale", Body: "must not persist"}}}},
	}
	if err := svc.integrateStaged(ctx, Job{ID: "job-stale", Scope: "default"}, stalePlan); err != nil {
		t.Fatalf("integrate stale plan: %v", err)
	}
	status, err := jobs.Get(ctx, "job-stale")
	if err != nil {
		t.Fatalf("Get stale job: %v", err)
	}
	if status.Status != JobFailed || !strings.Contains(status.Error, "vanished-name") || !strings.Contains(status.Error, "stale staged plan") {
		t.Fatalf("stale job = %+v, want failed with named stale-plan reason", status)
	}
	var partial int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM claims WHERE job_id = 'job-stale'`).Scan(&partial); err != nil || partial != 0 {
		t.Fatalf("stale partial claims = %d, %v; want 0", partial, err)
	}
}

func stageCompiledPage(t *testing.T, ctx context.Context, conn *sql.DB, jobID, normName string) {
	t.Helper()
	payload, err := json.Marshal(stagedCompiledPage{Title: "Compiled", Body: "compiled body"})
	if err != nil {
		t.Fatalf("marshal compiled page: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO job_staging (job_id, stage, unit, payload, units)
		VALUES (?, 'compile', ?, ?, 1)`, jobID, normName, string(payload)); err != nil {
		t.Fatalf("stage compiled page for %s: %v", jobID, err)
	}
}
