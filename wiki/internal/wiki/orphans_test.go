package wiki

import (
	"context"
	"testing"
)

func TestOrphansReturnsSubjectsWithZeroInboundMentions(t *testing.T) {
	// R-QSR2-AFAD
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	svc := NewService(conn, nil, nil, nil)
	subjects := NewSubjectStore(conn)
	pages := NewPageStore(conn)

	saveSubject(t, ctx, subjects, Subject{ID: "subject-a", Name: "Alpha Lab", Type: "entity"})
	saveSubject(t, ctx, subjects, Subject{ID: "subject-b", Name: "Beta Launch", Type: "event"})
	saveSubject(t, ctx, subjects, Subject{ID: "subject-c", Name: "Gamma Memo", Type: "concept"})
	upsertPage(t, ctx, pages, Page{
		ID:        "page-a",
		SubjectID: "subject-a",
		Title:     "Alpha Lab",
		Body:      "Alpha Lab prepared the Beta Launch.",
	})

	got, err := svc.Orphans(ctx, "default")
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	if gotIDs := orphanSubjectIDs(got); !sameStrings(gotIDs, []string{"subject-c", "subject-a"}) {
		t.Fatalf("Orphans ids = %+v, want Gamma Memo and Alpha Lab but not referenced Beta Launch", gotIDs)
	}
}

func TestOrphansSelfMentionDoesNotRescueSubject(t *testing.T) {
	// R-QTYY-O712
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	svc := NewService(conn, nil, nil, nil)
	subjects := NewSubjectStore(conn)
	pages := NewPageStore(conn)

	saveSubject(t, ctx, subjects, Subject{ID: "subject-s", Name: "Solo Subject", Type: "entity"})
	upsertPage(t, ctx, pages, Page{
		ID:        "page-s",
		SubjectID: "subject-s",
		Title:     "Solo Subject",
		Body:      "Solo Subject only names Solo Subject.",
	})

	got, err := svc.Orphans(ctx, "default")
	if err != nil {
		t.Fatalf("Orphans before inbound page: %v", err)
	}
	if gotIDs := orphanSubjectIDs(got); !sameStrings(gotIDs, []string{"subject-s"}) {
		t.Fatalf("Orphans ids = %+v, want self-mentioned subject still orphan", gotIDs)
	}

	saveSubject(t, ctx, subjects, Subject{ID: "subject-r", Name: "Referrer", Type: "entity"})
	upsertPage(t, ctx, pages, Page{
		ID:        "page-r",
		SubjectID: "subject-r",
		Title:     "Referrer",
		Body:      "Referrer names Solo Subject from another page.",
	})

	got, err = svc.Orphans(ctx, "default")
	if err != nil {
		t.Fatalf("Orphans after inbound page: %v", err)
	}
	if gotIDs := orphanSubjectIDs(got); !sameStrings(gotIDs, []string{"subject-r"}) {
		t.Fatalf("Orphans ids = %+v, want Solo Subject removed after true inbound mention", gotIDs)
	}
}

func TestOrphansCountsAliasMentionsAsCanonicalInbound(t *testing.T) {
	// R-QV6V-1YRR
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	svc := NewService(conn, nil, nil, nil)
	subjects := NewSubjectStore(conn)
	pages := NewPageStore(conn)
	aliases := NewAliasStore(conn)

	saveSubject(t, ctx, subjects, Subject{ID: "subject-w", Name: "Workshop Notes", Type: "concept"})
	saveSubject(t, ctx, subjects, Subject{ID: "subject-f", Name: "Field Report", Type: "entity"})
	upsertPage(t, ctx, pages, Page{
		ID:        "page-f",
		SubjectID: "subject-f",
		Title:     "Field Report",
		Body:      "The field report mentions Vasari, without naming the canonical title.",
	})

	got, err := svc.Orphans(ctx, "default")
	if err != nil {
		t.Fatalf("Orphans before alias: %v", err)
	}
	if gotIDs := orphanSubjectIDs(got); !sameStrings(gotIDs, []string{"subject-w", "subject-f"}) {
		t.Fatalf("Orphans ids = %+v, want Workshop Notes orphan before alias resolves Vasari", gotIDs)
	}

	if err := aliases.Insert(ctx, Alias{
		Name:      "Vasari",
		SubjectID: "subject-w",
		OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
		CreatedAt: "2026-06-25T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}

	got, err = svc.Orphans(ctx, "default")
	if err != nil {
		t.Fatalf("Orphans after alias: %v", err)
	}
	if gotIDs := orphanSubjectIDs(got); !sameStrings(gotIDs, []string{"subject-f"}) {
		t.Fatalf("Orphans ids = %+v, want canonical Workshop Notes removed by Vasari alias mention", gotIDs)
	}
}

func TestOrphansReturnsDeterministicPathOrder(t *testing.T) {
	// R-QWER-FQIG
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	svc := NewService(conn, nil, nil, nil)
	subjects := NewSubjectStore(conn)

	saveSubject(t, ctx, subjects, Subject{ID: "subject-z", Name: "Zeta Entity", Type: "entity"})
	saveSubject(t, ctx, subjects, Subject{ID: "subject-a", Name: "Alpha Concept", Type: "concept"})
	saveSubject(t, ctx, subjects, Subject{ID: "subject-b", Name: "Beta Event", Type: "event"})

	first, err := svc.Orphans(ctx, "default")
	if err != nil {
		t.Fatalf("first Orphans: %v", err)
	}
	second, err := svc.Orphans(ctx, "default")
	if err != nil {
		t.Fatalf("second Orphans: %v", err)
	}
	want := []string{"concept/alpha-concept", "entity/zeta-entity", "event/beta-event"}
	if got := orphanSubjectPaths(first); !sameStrings(got, want) {
		t.Fatalf("first Orphans paths = %+v, want %+v", got, want)
	}
	if got := orphanSubjectPaths(second); !sameStrings(got, want) {
		t.Fatalf("second Orphans paths = %+v, want stable %+v", got, want)
	}
}

func TestOrphansAreComputedWithinOneScope(t *testing.T) {
	// R-H169-4B38
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
	if err := subjects.Save(ctx, "s1", Subject{ID: "s1-source", Name: "Source Note", Type: "concept"}); err != nil {
		t.Fatalf("Save s1 source: %v", err)
	}
	if err := subjects.Save(ctx, "s2", Subject{ID: "s2-target", Name: "Remote Target", Type: "entity"}); err != nil {
		t.Fatalf("Save s2 target: %v", err)
	}
	if err := pages.Upsert(ctx, Page{ID: "s1-source", SubjectID: "s1-source", Title: "Source", Body: "Source Note mentions Remote Target."}); err != nil {
		t.Fatalf("Upsert s1 page: %v", err)
	}
	svc := NewService(conn, nil, nil, nil)
	s1, err := svc.Orphans(ctx, "s1")
	if err != nil || !sameStrings(orphanSubjectIDs(s1), []string{"s1-source"}) {
		t.Fatalf("s1 Orphans = %+v, %v; want only s1 source", orphanSubjectIDs(s1), err)
	}
	s2, err := svc.Orphans(ctx, "s2")
	if err != nil || !sameStrings(orphanSubjectIDs(s2), []string{"s2-target"}) {
		t.Fatalf("s2 Orphans = %+v, %v; want cross-scope mention to have no effect", orphanSubjectIDs(s2), err)
	}
}

func orphanSubjectIDs(subjects []Subject) []string {
	ids := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		ids = append(ids, subject.ID)
	}
	return ids
}

func orphanSubjectPaths(subjects []Subject) []string {
	paths := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		paths = append(paths, Path(subject))
	}
	return paths
}
