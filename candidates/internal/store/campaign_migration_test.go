package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-T493-LKSK
func TestCampaignMigrationBytes(t *testing.T) {
	got, err := fs.ReadFile(candidates.Migrations(), "0016_campaign_uses.sql")
	contractOK(t, err)
	contractEqual(t, string(got), "CREATE TABLE campaign_uses (candidate TEXT NOT NULL, campaign TEXT NOT NULL, cohort TEXT NOT NULL DEFAULT '', added TEXT NOT NULL DEFAULT '', rejected TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '', delivered TEXT NOT NULL DEFAULT '', PRIMARY KEY (candidate, campaign)) WITHOUT ROWID;\n")
}

// R-T5GZ-ZCJ9
func TestCampaignUpgradePreservesCandidatesWithEmptyHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0015_website_search.sql")})
	contractOK(t, err)
	ids := []string{"cand_1111111111111111", "cand_2222222222222222"}
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			if err := writePriorCandidate(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	st := store.New(d, store.Config{})
	for _, id := range ids {
		us, err := st.CampaignUses(ctx, id)
		contractOK(t, err)
		contractEqual(t, us, []store.CampaignUse{})
		c, err := st.GetCandidate(ctx, id)
		contractOK(t, err)
		contractEqual(t, c.ID, id)
	}
	// Previous-release writes still operate after the expand.
	id := "cand_3333333333333333"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error { return writePriorCandidate(ctx, tx, id) }))
	us, err := st.CampaignUses(ctx, id)
	contractOK(t, err)
	contractEqual(t, us, []store.CampaignUse{})
}
