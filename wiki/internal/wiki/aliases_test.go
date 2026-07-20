package wiki

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"wiki/internal/extract"
	"wiki/internal/page"
)

func TestAliasesMigrationCreatesConstrainedLookupTable(t *testing.T) {
	// R-BGPF-NVTU
	// R-BHXC-1NKJ
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	var tableSQL string
	if err := conn.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'aliases'`).
		Scan(&tableSQL); err != nil {
		t.Fatalf("lookup aliases table: %v", err)
	}
	compactTableSQL := strings.Join(strings.Fields(tableSQL), " ")
	for _, want := range []string{
		"norm_name TEXT NOT NULL UNIQUE",
		"subject_id TEXT NOT NULL REFERENCES subjects(id) ON DELETE RESTRICT",
		"name TEXT NOT NULL",
		"owner_id TEXT NOT NULL",
		"owner_email TEXT NOT NULL",
		"created_at TEXT NOT NULL",
	} {
		if !strings.Contains(compactTableSQL, want) {
			t.Fatalf("aliases SQL = %q, want %q", tableSQL, want)
		}
	}
	var indexName string
	if err := conn.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'aliases_subject'`).
		Scan(&indexName); err != nil {
		t.Fatalf("lookup aliases_subject index: %v", err)
	}

	subjects := NewSubjectStore(conn)
	if err := subjects.Save(ctx, Subject{ID: "subject-survivor", Name: "Café Noir", Type: "entity"}); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	aliases := NewAliasStore(conn)
	al := Alias{
		NormName:  "Cafe Old",
		SubjectID: "subject-survivor",
		Name:      "Café Old",
		OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
		CreatedAt: time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	}
	if err := aliases.Insert(ctx, al); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}
	if err := aliases.Insert(ctx, al); err == nil {
		t.Fatal("duplicate alias insert succeeded, want unique norm_name failure")
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM subjects WHERE id = ?`, "subject-survivor"); err == nil {
		t.Fatal("deleted aliased subject, want foreign-key restrict failure")
	}
}

func TestAliasStorePersistsLookupAndRepointsSubjects(t *testing.T) {
	// R-BJ58-FFB8
	// R-BKD4-T71X
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	for _, subject := range []Subject{
		{ID: "subject-old", Name: "Old Name", Type: "entity"},
		{ID: "subject-new", Name: "New Name", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save %s: %v", subject.ID, err)
		}
	}
	aliases := NewAliasStore(conn)
	if err := aliases.Insert(ctx, Alias{
		Name:      "  Café   Former  ",
		SubjectID: "subject-old",
		OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
		CreatedAt: "2026-06-23T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := aliases.GetByNormName(ctx, "cafe former")
	if err != nil {
		t.Fatalf("GetByNormName: %v", err)
	}
	if got.NormName != "cafe-former" || got.SubjectID != "subject-old" || got.Name != "  Café   Former  " {
		t.Fatalf("alias = %+v, want normalized lookup pointing at subject-old", got)
	}

	if err := aliases.RepointSubject(ctx, "subject-old", "subject-new"); err != nil {
		t.Fatalf("RepointSubject: %v", err)
	}
	got, err = aliases.GetByNormName(ctx, "CAFÉ FORMER")
	if err != nil {
		t.Fatalf("GetByNormName after repoint: %v", err)
	}
	if got.SubjectID != "subject-new" {
		t.Fatalf("alias subject = %q, want subject-new", got.SubjectID)
	}
}

func TestAliasStoreListAllReturnsEveryAliasForProjection(t *testing.T) {
	// R-1XX5-QDCY
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	for _, subject := range []Subject{
		{ID: "subject-one", Name: "Current One", Type: "entity"},
		{ID: "subject-two", Name: "Current Two", Type: "event"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save %s: %v", subject.ID, err)
		}
	}
	aliases := NewAliasStore(conn)
	for _, al := range []Alias{
		{Name: "Former Two", SubjectID: "subject-two", OwnerID: "owner-id", OwnerEmail: "owner@example.com", CreatedAt: "2026-06-24T12:01:00Z"},
		{Name: "Former One", SubjectID: "subject-one", OwnerID: "owner-id", OwnerEmail: "owner@example.com", CreatedAt: "2026-06-24T12:00:00Z"},
	} {
		if err := aliases.Insert(ctx, al); err != nil {
			t.Fatalf("Insert %s: %v", al.Name, err)
		}
	}

	got, err := aliases.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListAll returned %+v, want two aliases", got)
	}
	if got[0].NormName != "former-one" || got[0].SubjectID != "subject-one" {
		t.Fatalf("first alias = %+v, want former-one for subject-one", got[0])
	}
	if got[1].NormName != "former-two" || got[1].SubjectID != "subject-two" {
		t.Fatalf("second alias = %+v, want former-two for subject-two", got[1])
	}
}

func TestAliasStoreListMergesReturnsNewestAuditPage(t *testing.T) {
	// R-E4WX-G9H2
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	for _, subject := range []Subject{
		{ID: "subject-one", Name: "Current One", Type: "entity"},
		{ID: "subject-two", Name: "Current Two", Type: "entity"},
		{ID: "subject-three", Name: "Current Three", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save %s: %v", subject.ID, err)
		}
	}
	aliases := NewAliasStore(conn)
	for _, al := range []Alias{
		{Name: "Old One", SubjectID: "subject-one", OwnerID: "owner-id", OwnerEmail: "owner-a@example.com", CreatedAt: "2026-06-24T12:00:00Z"},
		{Name: "Old Two", SubjectID: "subject-two", OwnerID: "owner-id", OwnerEmail: "owner-b@example.com", CreatedAt: "2026-06-24T12:02:00Z"},
		{Name: "Old Three", SubjectID: "subject-three", OwnerID: "owner-id", OwnerEmail: "owner-c@example.com", CreatedAt: "2026-06-24T12:01:00Z"},
	} {
		if err := aliases.Insert(ctx, al); err != nil {
			t.Fatalf("Insert %s: %v", al.Name, err)
		}
	}

	first, next, err := aliases.ListMerges(ctx, page.Params{Limit: 2})
	if err != nil {
		t.Fatalf("ListMerges first page: %v", err)
	}
	if !sameStrings(aliasNames(first), []string{"Old Two", "Old Three"}) || next == "" {
		t.Fatalf("first page aliases = %v, next %q; want newest two plus cursor", aliasNames(first), next)
	}
	if first[0].SubjectID != "subject-two" || first[0].OwnerEmail != "owner-b@example.com" {
		t.Fatalf("first merge row = %+v, want audit metadata for newest alias", first[0])
	}

	second, next, err := aliases.ListMerges(ctx, page.Params{Limit: 2, Cursor: next})
	if err != nil {
		t.Fatalf("ListMerges second page: %v", err)
	}
	if next != "" || !sameStrings(aliasNames(second), []string{"Old One"}) {
		t.Fatalf("second page aliases = %v, next %q; want remaining alias only", aliasNames(second), next)
	}
}

func TestResolverPrefersSubjectsThenAliasesAndReportsNotFound(t *testing.T) {
	// R-BLL1-6YSM
	// R-BMSX-KQJB
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	for _, subject := range []Subject{
		{ID: "subject-canonical", Name: "Current Name", Type: "entity"},
		{ID: "subject-direct", Name: "Legacy Name", Type: "event"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save %s: %v", subject.ID, err)
		}
	}
	if err := NewAliasStore(conn).Insert(ctx, Alias{
		Name:      "Legacy Name",
		SubjectID: "subject-canonical",
		OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
		CreatedAt: "2026-06-23T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}
	resolver := NewResolver(conn)

	got, err := resolver.ResolveByName(ctx, "legacy name")
	if err != nil {
		t.Fatalf("ResolveByName direct: %v", err)
	}
	if got.ID != "subject-direct" {
		t.Fatalf("direct resolution = %+v, want subject-direct before alias", got)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM subjects WHERE id = ?`, "subject-direct"); err != nil {
		t.Fatalf("delete direct subject: %v", err)
	}
	got, err = resolver.ResolveByName(ctx, "LEGACY NAME")
	if err != nil {
		t.Fatalf("ResolveByName alias: %v", err)
	}
	if got.ID != "subject-canonical" {
		t.Fatalf("alias resolution = %+v, want subject-canonical", got)
	}

	if got, err := resolver.ResolveByName(ctx, "missing name"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("missing resolution = %+v, %v; want ErrSubjectNotFound", got, err)
	}
}

func TestProcessNextAppliesAliasedNameToSurvivorSubject(t *testing.T) {
	// R-BO0T-YIA0
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	if err := NewSubjectStore(conn).Save(ctx, Subject{ID: "subject-survivor", Name: "Current Name", Type: "entity"}); err != nil {
		t.Fatalf("Save survivor: %v", err)
	}
	if err := NewAliasStore(conn).Insert(ctx, Alias{
		Name:      "Former Name",
		SubjectID: "subject-survivor",
		OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
		CreatedAt: "2026-06-23T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}
	extractor := &recordingExtractor{batches: [][]extract.ExtractedSubject{{
		{Type: "entity", Name: "Former Name", Claims: []string{"Former Name shipped the release."}},
	}}}
	compiler := &recordingCompiler{}
	svc := NewService(conn, extractor, compiler, sequenceTimes(
		time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 23, 12, 0, 1, 0, time.UTC),
		time.Date(2026, 6, 23, 12, 0, 2, 0, time.UTC),
	))
	svc.newID = sequenceIDs("job-1", "claim-1")

	if _, err := svc.Ingest(ctx, "owner-id", "owner@example.com", "source", "Title", nil); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("ProcessNext = %v/%v, want true/nil", processed, err)
	}

	var subjectCount int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM subjects`).Scan(&subjectCount); err != nil {
		t.Fatalf("count subjects: %v", err)
	}
	if subjectCount != 1 {
		t.Fatalf("subject count = %d, want no duplicate minted for alias", subjectCount)
	}
	claims, _, err := NewClaimStore(conn).ListBySubject(ctx, "subject-survivor", pageParamsAll())
	if err != nil {
		t.Fatalf("ListBySubject survivor: %v", err)
	}
	if len(claims) != 1 || claims[0].Body != "Former Name shipped the release." {
		t.Fatalf("survivor claims = %+v, want alias claim on survivor", claims)
	}
	if len(compiler.subjects) != 1 || compiler.subjects[0].ID != "subject-survivor" {
		t.Fatalf("compiled subjects = %+v, want survivor", compiler.subjects)
	}
	if _, err := NewPageStore(conn).GetBySubject(ctx, "subject-survivor"); err != nil {
		t.Fatalf("GetBySubject survivor: %v", err)
	}
	if _, err := NewSubjectStore(conn).GetByNormName(ctx, "Former Name"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("alias name subject lookup err = %v, want sql.ErrNoRows", err)
	}
}

func pageParamsAll() page.Params {
	return page.Params{Limit: page.MaxLimit}
}

func aliasNames(aliases []Alias) []string {
	names := make([]string, 0, len(aliases))
	for _, al := range aliases {
		names = append(names, al.Name)
	}
	return names
}
