package crm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

	"appkit/logging"
)

const (
	tokenAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	tokenLength   = 6
	mintAttempts  = 16
)

// Mint creates a new globally unique live token for a live contact. It owns the
// transaction and deliberately does not append an outbox event.
func (s *Service) Mint(ctx context.Context, in MintInput) (MintResult, error) {
	if in.ContactID == "" {
		return MintResult{}, invalid("contact_id", "contact_id is required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return MintResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	live, err := liveExists(tx, "contacts", in.ContactID)
	if err != nil {
		return MintResult{}, err
	}
	if !live {
		return MintResult{}, fmt.Errorf("contact %s: %w", in.ContactID, ErrNotFound)
	}

	for range mintAttempts {
		token, err := randomToken()
		if err != nil {
			return MintResult{}, fmt.Errorf("generate contact token: %w", err)
		}
		_, err = tx.Exec(`INSERT INTO contact_tokens
			(id, contact_id, token, campaign, created_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, NULL)`,
			logging.NewULID(), in.ContactID, token, nullableCampaign(in.Campaign), fmtTime(s.Now().UTC()))
		if err == nil {
			if err := tx.Commit(); err != nil {
				return MintResult{}, fmt.Errorf("commit mint: %w", err)
			}
			return MintResult{Token: token, ContactID: in.ContactID, Campaign: in.Campaign}, nil
		}
		if !errors.Is(mapUniqueErr(err, "contact token"), ErrConflict) {
			return MintResult{}, fmt.Errorf("insert contact token: %w", err)
		}
	}
	return MintResult{}, errors.New("crm: token collision retry budget exhausted")
}

func randomToken() (string, error) {
	token := make([]byte, tokenLength)
	bound := big.NewInt(int64(len(tokenAlphabet)))
	for i := range token {
		n, err := rand.Int(rand.Reader, bound)
		if err != nil {
			return "", err
		}
		token[i] = tokenAlphabet[n.Int64()]
	}
	return string(token), nil
}

func nullableCampaign(campaign *string) any {
	if campaign == nil {
		return nil
	}
	return *campaign
}
