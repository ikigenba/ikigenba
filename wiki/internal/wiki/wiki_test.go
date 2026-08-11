package wiki

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"wiki/internal/extract"
	"wiki/internal/llm"
	"wiki/internal/retrieve"
)

func TestNormalizeAppliesPathSafePipeline(t *testing.T) {
	// R-RU0J-77HX
	if got, want := Normalize("  Ｓalaì!!!Apollo  11?? "), "salai-apollo-11"; got != want {
		t.Fatalf("Normalize(...) = %q, want %q", got, want)
	}
}

func TestNormalizeLongTitleUsesHyphenSeparatedLowercaseWords(t *testing.T) {
	// R-RV8F-KZ8M
	if got, want := Normalize("Lives of the Most Excellent Painters, Sculptors, and Architects"), "lives-of-the-most-excellent-painters-sculptors-and-architects"; got != want {
		t.Fatalf("Normalize(...) = %q, want %q", got, want)
	}
}

func TestNormalizeStripsDiacriticsFromSalai(t *testing.T) {
	// R-RXO8-CIQ0
	if got, want := Normalize("Salaì"), "salai"; got != want {
		t.Fatalf("Normalize(...) = %q, want %q", got, want)
	}
}

func TestNormalizeMapsApostropheToSeparator(t *testing.T) {
	// R-RYW4-QAGP
	if got, want := Normalize("Lorenzo de' Medici"), "lorenzo-de-medici"; got != want {
		t.Fatalf("Normalize(...) = %q, want %q", got, want)
	}
}

func TestNormalizeTrimsAndCollapsesPunctuation(t *testing.T) {
	// R-S041-427E
	if got, want := Normalize("!!!Hello, World!!!"), "hello-world"; got != want {
		t.Fatalf("Normalize(...) = %q, want %q", got, want)
	}
}

func TestNormalizeKeepsDigitsInWords(t *testing.T) {
	// R-S1BX-HTY3
	if got, want := Normalize("Apollo 11"), "apollo-11"; got != want {
		t.Fatalf("Normalize(...) = %q, want %q", got, want)
	}
}

func TestNormalizeIsIdempotentAndReturnsEmptyForSeparatorOnlyInputs(t *testing.T) {
	// R-S2JT-VLOS
	inputs := []string{
		"Lives of the Most Excellent Painters, Sculptors, and Architects",
		"Salaì",
		"Lorenzo de' Medici",
		"!!!Hello, World!!!",
		"Apollo 11",
	}
	for _, input := range inputs {
		once := Normalize(input)
		if got := Normalize(once); got != once {
			t.Fatalf("Normalize(Normalize(%q)) = %q, want %q", input, got, once)
		}
	}

	for _, input := range []string{"???", "", "   "} {
		if got := Normalize(input); got != "" {
			t.Fatalf("Normalize(%q) = %q, want empty string", input, got)
		}
	}
}

func TestProcessNextSkipsContentFreeNameSubject(t *testing.T) {
	// R-Z5JL-2IBS
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	svc := serviceWithExtractedSubjects(conn, []extract.ExtractedSubject{{
		Type:   "entity",
		Kind:   "company",
		Name:   " !!! ",
		Claims: []string{"this claim has no valid subject"},
	}})
	svc.newID = sequenceIDs("job-1")

	jobID := ingestAndProcess(t, ctx, svc)
	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobDone || len(status.Subjects) != 0 {
		t.Fatalf("status = %+v, want done with no subjects", status)
	}

	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM subjects WHERE norm_name = ''`).Scan(&count); err != nil {
		t.Fatalf("count empty norm_name subjects: %v", err)
	}
	if count != 0 {
		t.Fatalf("empty norm_name subject count = %d, want 0", count)
	}
}

func TestProcessNextSkipsClaimsForContentFreeNameSubject(t *testing.T) {
	// R-Z6RH-GA2H
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	svc := serviceWithExtractedSubjects(conn, []extract.ExtractedSubject{{
		Type:   "entity",
		Kind:   "company",
		Name:   "???",
		Claims: []string{"orphan claim must not be stored"},
	}})
	svc.newID = sequenceIDs("job-1")

	ingestAndProcess(t, ctx, svc)
	assertTableCount(t, ctx, conn, "claims", 0)
}

func TestProcessNextCreatesSiblingWhenContentFreeNameSkipped(t *testing.T) {
	// R-Z7ZD-U1T6
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	svc := serviceWithExtractedSubjects(conn, []extract.ExtractedSubject{
		{
			Type:   "entity",
			Kind:   "company",
			Name:   " / / / ",
			Claims: []string{"skipped sibling claim"},
		},
		{
			Type:   "entity",
			Kind:   "company",
			Name:   "Acme Robotics",
			Claims: []string{"Acme Robotics opened a Tulsa lab."},
		},
	})
	svc.newID = sequenceIDs("job-1", "subject-1", "claim-1")

	jobID := ingestAndProcess(t, ctx, svc)
	status, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobDone || len(status.Subjects) != 1 || status.Subjects[0] != "subject-1" {
		t.Fatalf("status = %+v, want done with subject-1 only", status)
	}

	subject, err := NewSubjectStore(conn).GetByNormName(ctx, "Acme Robotics")
	if err != nil {
		t.Fatalf("GetByNormName Acme Robotics: %v", err)
	}
	if subject.ID != "subject-1" || subject.NormName != "acme-robotics" {
		t.Fatalf("subject = %+v, want subject-1 with normalized Acme name", subject)
	}
	page, err := NewPageStore(conn).GetBySubject(ctx, "subject-1")
	if err != nil {
		t.Fatalf("GetBySubject subject-1: %v", err)
	}
	if page.Title != "Acme Robotics" || !strings.Contains(page.Body, "Tulsa lab") {
		t.Fatalf("page = %+v, want compiled Acme Robotics page", page)
	}
}

func TestMergeMintsForwardRoutingAliasForLoserName(t *testing.T) {
	// R-HUDR-AWS9
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	if err := subjects.Save(ctx, Subject{ID: "subject-winner", Name: "Winner Subject", Type: "entity"}); err != nil {
		t.Fatalf("Save winner: %v", err)
	}
	if err := subjects.Save(ctx, Subject{ID: "subject-loser", Name: "Loser Subject", Type: "entity"}); err != nil {
		t.Fatalf("Save loser: %v", err)
	}

	svc := NewService(conn, nil, &recordingCompiler{}, sequenceTimes(
		time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 24, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 6, 24, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 6, 24, 10, 0, 3, 0, time.UTC),
	))
	svc.newID = sequenceIDs("job-merge")
	jobID, err := svc.MergeSubjects(ctx, "default", "subject-loser", "subject-winner")
	if err != nil {
		t.Fatalf("MergeSubjects: %v", err)
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
	if status.Status != JobDone {
		t.Fatalf("status = %q, want done", status.Status)
	}

	got, err := NewResolver(conn).ResolveByName(ctx, "Loser Subject")
	if err != nil {
		t.Fatalf("ResolveByName loser name: %v", err)
	}
	if got.ID != "subject-winner" || got.Name != "Winner Subject" {
		t.Fatalf("resolved subject = %+v, want winner", got)
	}

	var aliasSubject string
	if err := conn.QueryRowContext(ctx,
		`SELECT subject_id FROM aliases WHERE norm_name = ?`, Normalize("Loser Subject")).
		Scan(&aliasSubject); err != nil {
		t.Fatalf("lookup normalized loser alias: %v", err)
	}
	if aliasSubject != "subject-winner" {
		t.Fatalf("alias subject = %q, want subject-winner", aliasSubject)
	}
}

func TestIngestClaimedBeforeScopeDeletionDiscardsDeadGeneration(t *testing.T) {
	// R-RJPF-C7S5
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	scopes := NewScopeStore(conn)
	if _, err := scopes.Create(ctx, "zombie-scope"); err != nil {
		t.Fatalf("Create zombie scope: %v", err)
	}
	compiler := newZombieBlockingCompiler()
	svc := NewService(
		conn,
		&recordingExtractor{batches: [][]extract.ExtractedSubject{{{
			Type:   "entity",
			Kind:   "company",
			Name:   "Dead Generation Labs",
			Claims: []string{"This claim belongs only to the deleted generation."},
		}}}},
		compiler,
		time.Now,
	)
	svc.newID = sequenceIDs("job-zombie", "subject-zombie", "claim-zombie")
	jobID, err := svc.Ingest(ctx, "zombie-scope", "owner-id", "owner@example.com", "dead source", "Dead source", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	job, ok, err := svc.jobs.ClaimPending(ctx, time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimPending = %+v, %v, %v; want claimed job", job, ok, err)
	}

	result := make(chan error, 1)
	go func() { result <- svc.processClaimed(ctx, job) }()
	select {
	case <-compiler.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker path did not reach compile")
	}
	if err := scopes.Delete(ctx, "zombie-scope"); err != nil {
		t.Fatalf("Delete zombie scope: %v", err)
	}
	if _, err := scopes.Create(ctx, "zombie-scope"); err != nil {
		t.Fatalf("Recreate zombie scope: %v", err)
	}
	close(compiler.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("processClaimed after job deletion = %v, want clean nil discard", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker path did not return after compile release")
	}

	assertQueryCount(t, ctx, conn, `SELECT COUNT(*) FROM subjects WHERE scope = ?`, 0, "zombie-scope")
	assertQueryCount(t, ctx, conn, `SELECT COUNT(*) FROM claims WHERE job_id = ?`, 0, jobID)
	assertQueryCount(t, ctx, conn, `SELECT COUNT(*) FROM pages WHERE subject_id = ?`, 0, "subject-zombie")
	assertQueryCount(t, ctx, conn, `SELECT COUNT(*) FROM jobs WHERE id = ?`, 0, jobID)
}

func TestMergeWithDeletedJobRowDiscardsWithoutRewritingSubjects(t *testing.T) {
	// R-RKXB-PZIU
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	for _, subject := range []Subject{
		{ID: "merge-winner", Name: "Merge Winner", Type: "entity"},
		{ID: "merge-loser", Name: "Merge Loser", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save subject %s: %v", subject.ID, err)
		}
	}
	claims := NewClaimStore(conn)
	for _, claim := range []Claim{
		{ID: "winner-claim", SubjectID: "merge-winner", JobID: "job-existing", Body: "Winner fact."},
		{ID: "loser-claim", SubjectID: "merge-loser", JobID: "job-existing", Body: "Loser fact."},
	} {
		if err := claims.Save(ctx, claim); err != nil {
			t.Fatalf("Save claim %s: %v", claim.ID, err)
		}
	}
	pages := NewPageStore(conn)
	for _, page := range []Page{
		{ID: "merge-winner", SubjectID: "merge-winner", Title: "Winner Before", Body: "winner before"},
		{ID: "merge-loser", SubjectID: "merge-loser", Title: "Loser Before", Body: "loser before"},
	} {
		if err := pages.Upsert(ctx, page); err != nil {
			t.Fatalf("Upsert page %s: %v", page.ID, err)
		}
	}

	compiler := newZombieBlockingCompiler()
	svc := NewService(conn, nil, compiler, time.Now)
	svc.newID = sequenceIDs("job-merge-zombie")
	jobID, err := svc.MergeSubjects(ctx, "default", "merge-loser", "merge-winner")
	if err != nil {
		t.Fatalf("MergeSubjects: %v", err)
	}
	job, ok, err := svc.jobs.ClaimPending(ctx, time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimPending = %+v, %v, %v; want claimed merge job", job, ok, err)
	}

	result := make(chan error, 1)
	go func() { result <- svc.processClaimed(ctx, job) }()
	select {
	case <-compiler.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("merge worker path did not reach compile")
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, jobID); err != nil {
		t.Fatalf("Delete merge job: %v", err)
	}
	close(compiler.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("processClaimed merge after job deletion = %v, want clean nil discard", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("merge worker path did not return after compile release")
	}

	assertQueryCount(t, ctx, conn, `SELECT COUNT(*) FROM jobs WHERE id = ?`, 0, jobID)
	assertQueryCount(t, ctx, conn, `SELECT COUNT(*) FROM aliases`, 0)
	if _, err := subjects.Get(ctx, "merge-loser"); err != nil {
		t.Fatalf("loser subject after discard: %v", err)
	}
	assertQueryCount(t, ctx, conn, `SELECT COUNT(*) FROM claims WHERE subject_id = ?`, 1, "merge-loser")
	for subjectID, wantBody := range map[string]string{
		"merge-winner": "winner before",
		"merge-loser":  "loser before",
	} {
		page, err := pages.GetBySubject(ctx, subjectID)
		if err != nil {
			t.Fatalf("GetBySubject %s: %v", subjectID, err)
		}
		if page.Body != wantBody {
			t.Fatalf("page %s body = %q, want unchanged %q", subjectID, page.Body, wantBody)
		}
	}
}

type zombieBlockingCompiler struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newZombieBlockingCompiler() *zombieBlockingCompiler {
	return &zombieBlockingCompiler{entered: make(chan struct{}), release: make(chan struct{})}
}

func (c *zombieBlockingCompiler) Compile(ctx context.Context, _ llm.Attribution, subject Subject, claims []Claim) (string, string, error) {
	c.once.Do(func() { close(c.entered) })
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	case <-c.release:
		var bodies []string
		for _, claim := range claims {
			bodies = append(bodies, claim.Body)
		}
		return subject.Name, strings.Join(bodies, "\n"), nil
	}
}

func assertQueryCount(t *testing.T, ctx context.Context, db *sql.DB, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if got != want {
		t.Fatalf("query count = %d, want %d for %s", got, want, query)
	}
}

func TestLoadVectorCacheEntriesLoadsStoredPageEmbeddings(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	for _, subject := range []Subject{
		{ID: "subject-a", Name: "Alpha Lab", NormName: "alpha-lab", Type: "entity"},
		{ID: "subject-b", Name: "Beta Lab", NormName: "beta-lab", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save subject %s: %v", subject.ID, err)
		}
	}
	pages := NewPageStore(conn)
	if err := pages.Upsert(ctx, Page{ID: "subject-a", SubjectID: "subject-a", Title: "Alpha Lab", Body: "Alpha body"}); err != nil {
		t.Fatalf("Upsert Alpha page: %v", err)
	}
	embeddings := NewEmbeddingStore(conn)
	if err := embeddings.Upsert(ctx, Embedding{SubjectID: "subject-a", Model: "model", Dims: 2, Vec: []float32{1, 0}, ContentHash: "hash-a", UpdatedAt: 1}); err != nil {
		t.Fatalf("Upsert Alpha embedding: %v", err)
	}
	if err := embeddings.Upsert(ctx, Embedding{SubjectID: "subject-b", Model: "model", Dims: 2, Vec: []float32{0, 1}, ContentHash: "hash-b", UpdatedAt: 2}); err != nil {
		t.Fatalf("Upsert Beta embedding: %v", err)
	}

	entries, err := LoadVectorCacheEntries(ctx, conn)
	if err != nil {
		t.Fatalf("LoadVectorCacheEntries: %v", err)
	}
	cacheEntries := make([]retrieve.VectorEntry, 0, len(entries))
	for _, entry := range entries {
		cacheEntries = append(cacheEntries, retrieve.VectorEntry{
			Scope:     entry.Scope,
			SubjectID: entry.SubjectID,
			Title:     entry.Title,
			Vec:       entry.Vec,
		})
	}
	cache := retrieve.NewVectorCache()
	cache.Replace(cacheEntries)
	retriever := retrieve.NewVectorRetriever(func(context.Context, llm.Attribution, string) ([]float32, error) {
		return []float32{1, 0}, nil
	}, cache)
	got, err := retriever.Search(ctx, "default", "alpha", retrieve.SearchLimits{Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Hits) != 1 {
		t.Fatalf("hits = %+v, want only embeddings with pages", got.Hits)
	}
	if got.Hits[0].PageID != "subject-a" || got.Hits[0].Title != "Alpha Lab" || got.Hits[0].Score != 1 {
		t.Fatalf("hit = %+v, want hydrated Alpha page vector", got.Hits[0])
	}
}

func TestConfigLeavesPromptsClientToCompositionRoot(t *testing.T) {
	cfg, err := NewConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.LLM != nil {
		t.Fatal("NewConfig should leave the prompts client to the composition root")
	}
}

func serviceWithExtractedSubjects(conn any, subjects []extract.ExtractedSubject) *Service {
	return NewService(conn, &recordingExtractor{batches: [][]extract.ExtractedSubject{subjects}}, &recordingCompiler{}, sequenceTimes(
		time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 23, 9, 0, 1, 0, time.UTC),
		time.Date(2026, 6, 23, 9, 0, 2, 0, time.UTC),
	))
}

func ingestAndProcess(t *testing.T, ctx context.Context, svc *Service) string {
	t.Helper()

	jobID, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "source text", "Source title", nil)
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
	return jobID
}
