package pat

import (
	"context"
	"testing"
)

// R-6SZ5-T6CC
func TestEnforcedOwnerIDSchemaSupportsCreateListAndValidate(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	const (
		ownerEmail = "owner@example.com"
		ownerID    = "identity-handle"
	)

	plaintext, created, err := store.Create(ctx, ownerEmail, ownerID, "build agent")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	listed, err := store.ListByOwner(ctx, ownerEmail)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListByOwner returned %d tokens, want 1", len(listed))
	}
	if listed[0].ID != created.ID || listed[0].OwnerID != ownerID {
		t.Errorf("listed token = %+v, want ID %q and owner_id %q", listed[0], created.ID, ownerID)
	}

	validated, err := store.ValidatePAT(ctx, plaintext)
	if err != nil {
		t.Fatalf("ValidatePAT: %v", err)
	}
	if validated.ID != created.ID || validated.OwnerID != ownerID {
		t.Errorf("validated token = %+v, want ID %q and owner_id %q", validated, created.ID, ownerID)
	}
}
