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

	got, err := svc.Orphans(ctx)
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

	got, err := svc.Orphans(ctx)
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

	got, err = svc.Orphans(ctx)
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

	got, err := svc.Orphans(ctx)
	if err != nil {
		t.Fatalf("Orphans before alias: %v", err)
	}
	if gotIDs := orphanSubjectIDs(got); !sameStrings(gotIDs, []string{"subject-w", "subject-f"}) {
		t.Fatalf("Orphans ids = %+v, want Workshop Notes orphan before alias resolves Vasari", gotIDs)
	}

	if err := aliases.Insert(ctx, Alias{
		Name:      "Vasari",
		SubjectID: "subject-w",
		CreatedBy: "owner@example.com",
		CreatedAt: "2026-06-25T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}

	got, err = svc.Orphans(ctx)
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

	first, err := svc.Orphans(ctx)
	if err != nil {
		t.Fatalf("first Orphans: %v", err)
	}
	second, err := svc.Orphans(ctx)
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
