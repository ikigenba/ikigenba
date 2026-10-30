package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-8F60-MERY
func TestCampaignOpenedMigrationBytes(t *testing.T) {
	raw, err := fs.ReadFile(candidates.Migrations(), "0029_campaign_opened.sql")
	contractOK(t, err)
	contractEqual(t, string(raw), "ALTER TABLE campaign_uses ADD COLUMN opened TEXT NOT NULL DEFAULT '';\n")
}

// writeCampaignBeforeOpened executes exactly the previous store's column set and
// conflict update, so rollback cannot overwrite an opened value it does not know.
func writeCampaignBeforeOpened(ctx context.Context, tx *sql.Tx, u store.CampaignUse) error {
	encode := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO campaign_uses(candidate,campaign,prospect_list,added,rejected,reason,delivered,cohort) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT DO UPDATE SET candidate=excluded.candidate,campaign=excluded.campaign,prospect_list=excluded.prospect_list,added=excluded.added,rejected=excluded.rejected,reason=excluded.reason,delivered=excluded.delivered,cohort=excluded.cohort`, u.Candidate, u.Campaign, u.ProspectList, encode(u.Added), encode(u.Rejected), u.Reason, encode(u.Delivered), "")
	return err
}

// R-8GDX-06IN
func TestCampaignOpenedMigrationPreservesPreviousUses(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0028_owner_records.sql")})
	contractOK(t, err)
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	uses := []store.CampaignUse{{Candidate: "cand_1111111111111111", Campaign: "full", ProspectList: "pl_0123456789abcdef", Added: at, Rejected: at.Add(time.Hour), Reason: "declined", Delivered: at.Add(2 * time.Hour)}, {Candidate: "cand_2222222222222222", Campaign: "empty"}, {Candidate: "cand_1111111111111111", Campaign: "reject", Rejected: at, Reason: "first"}}
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range []string{"cand_1111111111111111", "cand_2222222222222222"} {
			if err := writePriorCandidate(ctx, tx, id); err != nil {
				return err
			}
		}
		for _, u := range uses {
			if err := writeCampaignBeforeOpened(ctx, tx, u); err != nil {
				return err
			}
		}
		return nil
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{})
	got, err := s.CampaignUses(ctx, uses[0].Candidate)
	contractOK(t, err)
	contractEqual(t, got, []store.CampaignUse{uses[0], uses[2]})
	got, err = s.CampaignUses(ctx, uses[1].Candidate)
	contractOK(t, err)
	contractEqual(t, got, []store.CampaignUse{uses[1]})
}

// R-8HLT-DY9C
func TestPreviousCampaignWritesPreserveFirstVisitAfterRollback(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	c := contractSeed(t, f)
	d := contractAttach(t, f, "run", contractObservation("rollback-other", "other", f.now))[0]
	opened := time.Date(2026, 10, 15, 12, 0, 0, 987654321, time.FixedZone("input", -5*3600))
	contractOK(t, f.s.MarkCampaignUseOpened(ctx, c, "campaign", opened))
	u := campaignUses(t, f, c)[0]
	u.Delivered = f.now.UTC().Truncate(time.Second)
	v := store.CampaignUse{Candidate: d, Campaign: "campaign", ProspectList: "previous list", Added: f.now.UTC().Truncate(time.Second), Reason: "old reason"}
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { return writeCampaignBeforeOpened(ctx, tx, v) }))
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { return writeCampaignBeforeOpened(ctx, tx, u) }))
	contractEqual(t, campaignUses(t, f, c), []store.CampaignUse{u})
	contractEqual(t, campaignUses(t, f, d), []store.CampaignUse{v})
	f.reopen()
	contractEqual(t, campaignUses(t, f, c), []store.CampaignUse{u})
	contractEqual(t, campaignUses(t, f, d), []store.CampaignUse{v})
}
