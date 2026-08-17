package crm

import (
	"context"
	"errors"
	"regexp"
	"testing"
)

func TestMintCreatesCrockfordTokenWithCampaign(t *testing.T) {
	s := newTestStore(t)
	contact := mustSave(t, s, "contact", "", map[string]any{"display_name": "Ada"}, false)
	campaign := "summer-2026"

	got, err := s.Mint(context.Background(), MintInput{ContactID: contact.ID, Campaign: &campaign})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	// R-9QOE-LQTI
	if !regexp.MustCompile(`^[0123456789abcdefghjkmnpqrstvwxyz]{6}$`).MatchString(got.Token) {
		t.Fatalf("token %q is not six lowercase Crockford-base32 characters", got.Token)
	}
	var rowContact, rowCampaign string
	if err := s.DB.QueryRow(`SELECT contact_id, campaign FROM contact_tokens WHERE token = ? AND deleted_at IS NULL`, got.Token).Scan(&rowContact, &rowCampaign); err != nil {
		t.Fatalf("load minted row: %v", err)
	}
	// R-9QOE-LQTI
	if rowContact != contact.ID || rowCampaign != campaign || got.ContactID != contact.ID || got.Campaign == nil || *got.Campaign != campaign {
		t.Fatalf("mint result/row mismatch: result=%+v row_contact=%q row_campaign=%q", got, rowContact, rowCampaign)
	}
}

func TestMintTokensAreGloballyUnique(t *testing.T) {
	s := newTestStore(t)
	contact := mustSave(t, s, "contact", "", map[string]any{"display_name": "Ada"}, false)
	seen := map[string]bool{}
	for range 256 {
		got, err := s.Mint(context.Background(), MintInput{ContactID: contact.ID})
		if err != nil {
			t.Fatalf("mint batch: %v", err)
		}
		// R-9RWA-ZIK7
		if seen[got.Token] {
			t.Fatalf("mint returned duplicate token %q", got.Token)
		}
		seen[got.Token] = true
	}
	var token string
	if err := s.DB.QueryRow(`SELECT token FROM contact_tokens LIMIT 1`).Scan(&token); err != nil {
		t.Fatalf("select token: %v", err)
	}
	_, err := s.DB.Exec(`INSERT INTO contact_tokens (id, contact_id, token, created_at) VALUES ('duplicate-id', ?, ?, '2026-08-17T00:00:00Z')`, contact.ID, token)
	// R-9RWA-ZIK7
	if err == nil {
		t.Fatal("direct insert of a duplicate live token unexpectedly succeeded")
	}
}

func TestSearchByTokenOnlyResolvesLiveToken(t *testing.T) {
	s := newTestStore(t)
	contact := mustSave(t, s, "contact", "", map[string]any{"display_name": "Ada"}, false)
	minted, err := s.Mint(context.Background(), MintInput{ContactID: contact.ID})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	search := func(token string) []Summary {
		t.Helper()
		got, err := s.Search(context.Background(), SearchParams{Type: "contact", Filters: map[string]any{"token": token}})
		if err != nil {
			t.Fatalf("search token: %v", err)
		}
		return got
	}

	got := search(minted.Token)
	// R-9T47-DAAW
	if len(got) != 1 || got[0].ID != contact.ID {
		t.Fatalf("live token search = %+v, want contact %s", got, contact.ID)
	}
	if _, err := s.DB.Exec(`UPDATE contact_tokens SET deleted_at = '2026-08-17T00:00:00Z' WHERE token = ?`, minted.Token); err != nil {
		t.Fatalf("soft-delete token: %v", err)
	}
	// R-9T47-DAAW
	if got := search(minted.Token); len(got) != 0 {
		t.Fatalf("soft-deleted token resolved contacts: %+v", got)
	}
	// R-9T47-DAAW
	if got := search("zzzzzz"); len(got) != 0 {
		t.Fatalf("unknown token resolved contacts: %+v", got)
	}
}

func TestMintTwiceKeepsCampaignsAndBothResolve(t *testing.T) {
	s := newTestStore(t)
	contact := mustSave(t, s, "contact", "", map[string]any{"display_name": "Ada"}, false)
	firstCampaign, secondCampaign := "email", "postcard"
	first, err := s.Mint(context.Background(), MintInput{ContactID: contact.ID, Campaign: &firstCampaign})
	if err != nil {
		t.Fatalf("first mint: %v", err)
	}
	second, err := s.Mint(context.Background(), MintInput{ContactID: contact.ID, Campaign: &secondCampaign})
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	for token, campaign := range map[string]string{first.Token: firstCampaign, second.Token: secondCampaign} {
		var stored string
		if err := s.DB.QueryRow(`SELECT campaign FROM contact_tokens WHERE token = ? AND contact_id = ? AND deleted_at IS NULL`, token, contact.ID).Scan(&stored); err != nil {
			t.Fatalf("load campaign for %s: %v", token, err)
		}
		matches, err := s.Search(context.Background(), SearchParams{Type: "contact", Filters: map[string]any{"token": token}})
		if err != nil {
			t.Fatalf("resolve %s: %v", token, err)
		}
		// R-9UC3-R21L
		if first.Token == second.Token || stored != campaign || len(matches) != 1 || matches[0].ID != contact.ID {
			t.Fatalf("token/campaign resolution mismatch: first=%+v second=%+v stored=%q matches=%+v", first, second, stored, matches)
		}
	}
}

func TestMintMissingContactReturnsNotFoundWithoutRow(t *testing.T) {
	s := newTestStore(t)
	var before, after int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM contact_tokens`).Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}
	_, err := s.Mint(context.Background(), MintInput{ContactID: "01DOESNOTEXIST"})
	if countErr := s.DB.QueryRow(`SELECT COUNT(*) FROM contact_tokens`).Scan(&after); countErr != nil {
		t.Fatalf("count after: %v", countErr)
	}
	// R-9VK0-4TSA
	if !errors.Is(err, ErrNotFound) || after != before {
		t.Fatalf("mint missing contact error=%v row count %d -> %d", err, before, after)
	}
}

func TestMintRequiresContactID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Mint(context.Background(), MintInput{})
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != "contact_id" {
		t.Fatalf("missing contact_id error = %#v, want field validation", err)
	}
}
