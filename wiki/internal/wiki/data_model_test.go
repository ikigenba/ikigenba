package wiki

import (
	"context"
	"database/sql"
	"testing"

	"wiki/internal/db"
)

func TestNormalizePipeline(t *testing.T) {
	// R-7TVC-E7ZZ
	tests := map[string]string{
		"  Café\u0301\tNOIR  ": "cafe noir",
		"\u212bngström":        "angstrom",
		"ＡＬＰＨＡ   Beta":         "alpha beta",
	}
	for in, want := range tests {
		if got := normalize(in); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDomainStoresPersistPhaseOneModel(t *testing.T) {
	// R-7V38-RZQO
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	jobs := NewJobStore(conn)
	subjects := NewSubjectStore(conn)
	claims := NewClaimStore(conn)
	pages := NewPageStore(conn)

	if err := jobs.Save(ctx, Job{ID: "job-1", Status: "running"}); err != nil {
		t.Fatalf("Save job: %v", err)
	}
	if err := subjects.Save(ctx, Subject{ID: "subject-1", Name: " Café Noir ", Type: "entity"}); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	if err := claims.Save(ctx, Claim{ID: "claim-1", SubjectID: "subject-1", JobID: "job-1", Body: "Café Noir is a test subject."}); err != nil {
		t.Fatalf("Save claim: %v", err)
	}
	if err := pages.Upsert(ctx, Page{ID: "page-1", SubjectID: "subject-1", Title: "Café Noir", Body: "A generated page."}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}

	job, err := jobs.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("Get job: %v", err)
	}
	if job.Status != "running" {
		t.Fatalf("job.Status = %q, want running", job.Status)
	}

	subject, err := subjects.GetByNormName(ctx, "cafe noir")
	if err != nil {
		t.Fatalf("GetByNormName: %v", err)
	}
	if subject.ID != "subject-1" || subject.NormName != "cafe noir" {
		t.Fatalf("subject = %+v, want subject-1 with normalized name cafe noir", subject)
	}

	gotClaims, err := claims.ListBySubject(ctx, "subject-1")
	if err != nil {
		t.Fatalf("ListBySubject: %v", err)
	}
	if len(gotClaims) != 1 || gotClaims[0].JobID != "job-1" {
		t.Fatalf("claims = %+v, want one claim for job-1", gotClaims)
	}

	page, err := pages.Get(ctx, "page-1")
	if err != nil {
		t.Fatalf("Get page: %v", err)
	}
	if page.Title != "Café Noir" || page.Body != "A generated page." {
		t.Fatalf("page = %+v, want saved title and body", page)
	}
}

func migratedDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	conn, err := db.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(ctx, conn); err != nil {
		conn.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return conn
}
