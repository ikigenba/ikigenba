package page

import (
	"context"
	"strings"
	"testing"
)

func TestReadExcerpt(t *testing.T) {
	conn := newTestDB(t)
	s := NewStore(conn)
	ctx := context.Background()

	insertSubject(t, conn, "subj-acme", TypeEntity, "Acme Corp")
	insertAlias(t, conn, TypeEntity, "Acme Corp", "subj-acme")
	insertAlias(t, conn, TypeEntity, "ACME", "subj-acme")
	body := strings.Repeat("x", 1000)
	insertPage(t, conn, "subj-acme", "Acme Corp", body)

	ex, err := s.ReadExcerpt(ctx, "subj-acme", 600)
	if err != nil {
		t.Fatalf("ReadExcerpt: %v", err)
	}
	if ex.CanonicalName != "Acme Corp" {
		t.Errorf("CanonicalName = %q, want Acme Corp", ex.CanonicalName)
	}
	// Full alias list (normalized keys), sorted.
	wantAliases := []string{Normalize("ACME"), Normalize("Acme Corp")}
	if len(ex.Aliases) != 2 || ex.Aliases[0] != wantAliases[0] || ex.Aliases[1] != wantAliases[1] {
		t.Errorf("Aliases = %v, want %v", ex.Aliases, wantAliases)
	}
	// Body truncated to the configured length.
	if len(ex.Body) != 600 {
		t.Errorf("Body length = %d, want 600", len(ex.Body))
	}
}

func TestReadExcerptNoPage(t *testing.T) {
	conn := newTestDB(t)
	s := NewStore(conn)
	ctx := context.Background()

	// A subject with no page row (a just-minted collision id) is not an error.
	insertSubject(t, conn, "subj-bare", TypeEntity, "Bare")
	ex, err := s.ReadExcerpt(ctx, "subj-bare", 600)
	if err != nil {
		t.Fatalf("ReadExcerpt with no page: %v", err)
	}
	if ex.Body != "" {
		t.Errorf("Body = %q, want empty for a page-less subject", ex.Body)
	}
	if ex.CanonicalName != "Bare" {
		t.Errorf("CanonicalName = %q, want Bare", ex.CanonicalName)
	}
}

func TestReadExcerptUnknownSubject(t *testing.T) {
	conn := newTestDB(t)
	s := NewStore(conn)
	_, err := s.ReadExcerpt(context.Background(), "does-not-exist", 600)
	if err == nil {
		t.Fatal("expected error for an unknown subject id")
	}
}

func TestReadExcerptTruncatesOnRuneBoundary(t *testing.T) {
	conn := newTestDB(t)
	s := NewStore(conn)
	ctx := context.Background()

	insertSubject(t, conn, "subj-uni", TypeEntity, "Uni")
	// Multibyte runes: "é" is 2 bytes. A truncation at an odd byte must back up.
	insertPage(t, conn, "subj-uni", "Uni", strings.Repeat("é", 100))

	ex, err := s.ReadExcerpt(ctx, "subj-uni", 5)
	if err != nil {
		t.Fatalf("ReadExcerpt: %v", err)
	}
	// 5 bytes lands mid-rune (2 bytes each); must back up to 4 bytes (2 runes).
	if len(ex.Body) != 4 {
		t.Errorf("Body length = %d, want 4 (rune-boundary truncation)", len(ex.Body))
	}
	if !strings.HasPrefix(strings.Repeat("é", 100), ex.Body) {
		t.Errorf("Body %q is not a clean prefix", ex.Body)
	}
}
