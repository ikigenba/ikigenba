package crm

import (
	"errors"
	"testing"
)

func TestOrganization_RoundTrip(t *testing.T) {
	s := newTestStore(t)

	var orgID string
	// Create.
	withTx(t, s, func(tx *txAlias) {
		sum, err := s.orgs.Save(tx, "", OrganizationInput{Name: sp("Acme Inc"), Domain: sp("acme.com")}, s.Now())
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if sum.ID == "" || sum.Type != "organization" || sum.Label != "Acme Inc" {
			t.Fatalf("bad summary: %+v", sum)
		}
		orgID = sum.ID
	})

	// Get card.
	withTx(t, s, func(tx *txAlias) {
		card, err := s.orgs.Get(tx, orgID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if card["name"] != "Acme Inc" || card["domain"] != "acme.com" {
			t.Fatalf("bad card: %+v", card)
		}
		for _, k := range []string{"contacts", "open_deals", "recent_interactions"} {
			if _, ok := card[k]; !ok {
				t.Errorf("card missing relation %q", k)
			}
		}
	})

	// Update: change name, clear domain.
	withTx(t, s, func(tx *txAlias) {
		if _, err := s.orgs.Save(tx, orgID, OrganizationInput{Name: sp("Acme LLC"), Domain: sp("")}, s.Now()); err != nil {
			t.Fatalf("update: %v", err)
		}
	})
	withTx(t, s, func(tx *txAlias) {
		card, err := s.orgs.Get(tx, orgID)
		if err != nil {
			t.Fatalf("get after update: %v", err)
		}
		if card["name"] != "Acme LLC" {
			t.Errorf("name not updated: %v", card["name"])
		}
		if _, ok := card["domain"]; ok {
			t.Errorf("domain not cleared: %v", card["domain"])
		}
	})

	// Search.
	withTx(t, s, func(tx *txAlias) {
		got, err := s.orgs.Search(tx, SearchParams{Query: "acme"})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(got) != 1 || got[0].ID != orgID {
			t.Fatalf("search want 1 hit %s, got %+v", orgID, got)
		}
	})

	// Delete → Get is not_found; Search is empty.
	withTx(t, s, func(tx *txAlias) {
		if err := s.orgs.Delete(tx, orgID, s.Now()); err != nil {
			t.Fatalf("delete: %v", err)
		}
	})
	withTx(t, s, func(tx *txAlias) {
		if _, err := s.orgs.Get(tx, orgID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("get after delete: want ErrNotFound, got %v", err)
		}
		got, err := s.orgs.Search(tx, SearchParams{Query: "acme"})
		if err != nil {
			t.Fatalf("search after delete: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("search after delete: want 0, got %d", len(got))
		}
	})
}

func TestOrganization_CreateRequiresName(t *testing.T) {
	s := newTestStore(t)
	withTx(t, s, func(tx *txAlias) {
		if _, err := s.orgs.Save(tx, "", OrganizationInput{}, s.Now()); !errors.Is(err, ErrValidation) {
			t.Fatalf("want ErrValidation, got %v", err)
		}
	})
}

func TestOrganization_UpdateMissingIsNotFound(t *testing.T) {
	s := newTestStore(t)
	withTx(t, s, func(tx *txAlias) {
		if _, err := s.orgs.Save(tx, "NOPE", OrganizationInput{Name: sp("x")}, s.Now()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
