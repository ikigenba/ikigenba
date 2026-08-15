package wiki

import (
	"context"
	"errors"
	"testing"
	"wiki/internal/page"
)

func TestEffectiveExcludesSuppressedClaimsAndCorrections(t *testing.T) {
	// R-76SJ-0JPH
	rows := []Claim{
		{ID: "A", Kind: ClaimKind},
		{ID: "B", Kind: ClaimKind},
		{ID: "X", Kind: CorrectionKind},
	}
	claims, suppressedBy := Effective(rows, []Suppression{{Correction: "X", Suppressed: "A"}})
	if len(claims) != 1 || claims[0].ID != "B" {
		t.Fatalf("effective claims = %+v, want exactly B", claims)
	}
	if got := suppressedBy["A"]; len(got) != 1 || got[0] != "X" {
		t.Fatalf("suppressedBy[A] = %v, want [X]", got)
	}
	if _, ok := suppressedBy["B"]; ok {
		t.Fatalf("suppressedBy contains effective claim B: %#v", suppressedBy)
	}
}

func TestEffectiveRevivesTargetsWhenTheirSuppressorIsSuppressed(t *testing.T) {
	// R-780F-EBG6
	rows := []Claim{
		{ID: "A", Kind: ClaimKind},
		{ID: "X", Kind: CorrectionKind},
		{ID: "Y", Kind: CorrectionKind},
		{ID: "Z", Kind: CorrectionKind},
	}
	twoEdges := []Suppression{
		{Correction: "X", Suppressed: "A"},
		{Correction: "Y", Suppressed: "X"},
	}
	claims, suppressedBy := Effective(rows, twoEdges)
	if len(claims) != 1 || claims[0].ID != "A" {
		t.Fatalf("two-level effective claims = %+v, want revived A", claims)
	}
	if got := suppressedBy["X"]; len(got) != 1 || got[0] != "Y" {
		t.Fatalf("two-level suppressedBy[X] = %v, want [Y]", got)
	}

	threeEdges := append(twoEdges, Suppression{Correction: "Z", Suppressed: "Y"})
	claims, suppressedBy = Effective(rows, threeEdges)
	if len(claims) != 0 {
		t.Fatalf("three-level effective claims = %+v, want A suppressed again", claims)
	}
	if got := suppressedBy["A"]; len(got) != 1 || got[0] != "X" {
		t.Fatalf("three-level suppressedBy[A] = %v, want [X]", got)
	}
}

func TestSuppressionStoreInsertEnforcesEdgeInvariants(t *testing.T) {
	// R-798B-S36V
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	claims := NewClaimStore(db)
	for _, claim := range []Claim{
		{ID: "100-old-correction", SubjectID: "subject", JobID: "job-old", Kind: CorrectionKind},
		{ID: "200-claim-source", SubjectID: "subject", JobID: "job-claim", Kind: ClaimKind},
		{ID: "300-same-job-target", SubjectID: "subject", JobID: "job-same", Kind: CorrectionKind},
		{ID: "400-same-job-source", SubjectID: "subject", JobID: "job-same", Kind: CorrectionKind},
		{ID: "500-newer-correction", SubjectID: "subject", JobID: "job-new", Kind: CorrectionKind},
		{ID: "900-newer-claim", SubjectID: "subject", JobID: "job-newer-claim", Kind: ClaimKind},
	} {
		if err := claims.Save(ctx, claim); err != nil {
			t.Fatalf("Save %s: %v", claim.ID, err)
		}
	}
	store := NewSuppressionStore(db)
	invalid := []Suppression{
		{Correction: "200-claim-source", Suppressed: "900-newer-claim", CreatedAt: 1},
		{Correction: "100-old-correction", Suppressed: "500-newer-correction", CreatedAt: 2},
		{Correction: "400-same-job-source", Suppressed: "300-same-job-target", CreatedAt: 3},
	}
	for _, edge := range invalid {
		if err := store.Insert(ctx, edge); !errors.Is(err, ErrInvalidSuppression) {
			t.Errorf("Insert(%+v) error = %v, want ErrInvalidSuppression", edge, err)
		}
	}
	if err := store.Insert(ctx, Suppression{Correction: "100-old-correction", Suppressed: "900-newer-claim", CreatedAt: 4}); err != nil {
		t.Fatalf("older correction suppressing newer claim: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppressions`).Scan(&count); err != nil {
		t.Fatalf("count suppressions: %v", err)
	}
	if count != 1 {
		t.Fatalf("suppression count = %d, want only accepted edge", count)
	}
}

func TestClaimKindRoundTripsAndSuppressionDeletionCascadesBothEndpoints(t *testing.T) {
	// R-7AG8-5UXK
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	claims := NewClaimStore(db)
	for _, claim := range []Claim{
		{ID: "100-target", SubjectID: "subject", JobID: "job-target", Kind: ClaimKind},
		{ID: "200-deleted-claim", SubjectID: "subject", JobID: "job-delete", Kind: ClaimKind},
		{ID: "300-surviving-correction", SubjectID: "subject", JobID: "job-survive", Kind: CorrectionKind},
		{ID: "400-deleted-correction", SubjectID: "subject", JobID: "job-delete", Kind: CorrectionKind},
	} {
		if err := claims.Save(ctx, claim); err != nil {
			t.Fatalf("Save %s: %v", claim.ID, err)
		}
	}
	got, _, err := claims.ListBySubject(ctx, "subject", page.Params{})
	if err != nil {
		t.Fatalf("ListBySubject: %v", err)
	}
	kinds := map[string]string{}
	for _, claim := range got {
		kinds[claim.ID] = claim.Kind
	}
	if kinds["100-target"] != ClaimKind || kinds["400-deleted-correction"] != CorrectionKind {
		t.Fatalf("round-tripped kinds = %#v, want claim and correction", kinds)
	}

	suppressions := NewSuppressionStore(db)
	for _, edge := range []Suppression{
		{Correction: "400-deleted-correction", Suppressed: "100-target", CreatedAt: 1},
		{Correction: "300-surviving-correction", Suppressed: "200-deleted-claim", CreatedAt: 2},
	} {
		if err := suppressions.Insert(ctx, edge); err != nil {
			t.Fatalf("Insert %+v: %v", edge, err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := NewSuppressionStore(tx).DeleteForClaims(ctx, []string{"200-deleted-claim", "400-deleted-correction"}); err != nil {
		t.Fatalf("DeleteForClaims: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM claims WHERE id IN ('200-deleted-claim', '400-deleted-correction')`); err != nil {
		t.Fatalf("delete claims: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	var edgeCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppressions`).Scan(&edgeCount); err != nil {
		t.Fatalf("count suppressions: %v", err)
	}
	if edgeCount != 0 {
		t.Fatalf("suppression count after cascade = %d, want 0", edgeCount)
	}
	remaining, _, err := claims.ListBySubject(ctx, "subject", page.Params{})
	if err != nil {
		t.Fatalf("list remaining claims: %v", err)
	}
	edges, err := suppressions.ListBySubject(ctx, "subject")
	if err != nil {
		t.Fatalf("list remaining suppressions: %v", err)
	}
	effective, _ := Effective(remaining, edges)
	if len(effective) != 1 || effective[0].ID != "100-target" {
		t.Fatalf("effective claims after deleting correction = %+v, want revived target", effective)
	}
}
