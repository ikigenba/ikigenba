package oauth

import (
	"context"
	"testing"
	"time"
)

// R-6U72-6Y31
func TestEnforcedOwnerIDSchemaSupportsIssueAndAccessValidation(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	store := NewTokenStore(database, 15*time.Minute, 24*time.Hour)
	clk := newClock()
	store.Now = clk.now

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	pair, err := store.IssueChainAndTokens(
		ctx, tx, "client-1", "owner@example.com", "identity-handle", "https://example.com/mcp",
	)
	if err != nil {
		t.Fatalf("IssueChainAndTokens: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	validated, err := store.ValidateAccess(ctx, pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccess: %v", err)
	}
	if validated.Token.Kind != KindAccess || validated.Token.ChainID != pair.ChainID {
		t.Errorf("validated token = %+v, want access token for chain %q", validated.Token, pair.ChainID)
	}
	if validated.Chain.ID != pair.ChainID || validated.Chain.OwnerID != "identity-handle" {
		t.Errorf("validated chain = %+v, want ID %q and owner_id identity-handle", validated.Chain, pair.ChainID)
	}
}
