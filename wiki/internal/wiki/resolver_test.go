package wiki

import (
	"context"
	"errors"
	"testing"
)

func TestResolverResolveByPathFallsBackToAliasToken(t *testing.T) {
	// R-AF1X-PG7K
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	if err := NewSubjectStore(conn).Save(ctx, Subject{
		ID:   "subject-survivor",
		Name: "Winner Widget",
		Type: "entity",
	}); err != nil {
		t.Fatalf("Save survivor: %v", err)
	}
	if err := NewAliasStore(conn).Insert(ctx, Alias{
		Name:      "Folded Widget",
		SubjectID: "subject-survivor",
		CreatedBy: "owner@example.com",
		CreatedAt: "2026-06-24T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}

	got, err := NewResolver(conn).ResolveByPath(ctx, "entity/folded-widget")
	if err != nil {
		t.Fatalf("ResolveByPath alias: %v", err)
	}
	if got.ID != "subject-survivor" || got.Name != "Winner Widget" {
		t.Fatalf("ResolveByPath alias = %+v, want survivor subject", got)
	}
}

func TestResolverResolveByPathPrefersExactLiveSubjectAndPreservesTypeDiscipline(t *testing.T) {
	// R-AG2Y-PH8L
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := NewSubjectStore(conn)
	for _, subject := range []Subject{
		{ID: "subject-survivor", Name: "Canonical Widget", Type: "entity"},
		{ID: "subject-direct", Name: "Legacy Widget", Type: "entity"},
		{ID: "subject-typed", Name: "Typed Widget", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save %s: %v", subject.ID, err)
		}
	}
	if err := NewAliasStore(conn).Insert(ctx, Alias{
		Name:      "Legacy Widget",
		SubjectID: "subject-survivor",
		CreatedBy: "owner@example.com",
		CreatedAt: "2026-06-24T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}
	resolver := NewResolver(conn)

	got, err := resolver.ResolveByPath(ctx, "entity/legacy-widget")
	if err != nil {
		t.Fatalf("ResolveByPath direct: %v", err)
	}
	if got.ID != "subject-direct" {
		t.Fatalf("direct resolution = %+v, want live subject before alias", got)
	}

	if got, err := resolver.ResolveByPath(ctx, "event/typed-widget"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("wrong-type resolution = %+v, %v; want ErrSubjectNotFound", got, err)
	}
}

func TestResolverResolveByPathReportsUnknownAndMalformedPathsNotFound(t *testing.T) {
	// R-AH3Z-PJ9M
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	if err := NewSubjectStore(conn).Save(ctx, Subject{
		ID:   "subject-known",
		Name: "Known Widget",
		Type: "entity",
	}); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	resolver := NewResolver(conn)

	for _, path := range []string{"entity/missing-widget", "entity/"} {
		if got, err := resolver.ResolveByPath(ctx, path); !errors.Is(err, ErrSubjectNotFound) {
			t.Fatalf("ResolveByPath(%q) = %+v, %v; want ErrSubjectNotFound", path, got, err)
		}
	}
}
