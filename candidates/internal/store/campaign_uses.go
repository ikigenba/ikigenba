package store

import (
	"context"
	"database/sql"
	"time"
)

// CampaignUse records a campaign's use of one business.
type CampaignUse struct {
	Candidate, Campaign, Cohort string
	Added, Rejected             time.Time
	Reason                      string
	Delivered                   time.Time
}

var campaignUseColumns = columns("candidate:Candidate campaign:Campaign cohort:Cohort added:Added rejected:Rejected reason:Reason delivered:Delivered")

// AddCampaignUse keeps the earliest add and its originating cohort.
func (s *Store) AddCampaignUse(ctx context.Context, candidate, campaign, cohort string, at time.Time) error {
	return s.writeCampaignUse(ctx, candidate, campaign, at, func(u *CampaignUse, a time.Time, _ bool) {
		if u.Added.IsZero() || a.Before(u.Added) {
			u.Added, u.Cohort = a, cohort
		}
	})
}

// RejectCampaignUse keeps the first rejection and its reason.
func (s *Store) RejectCampaignUse(ctx context.Context, candidate, campaign, reason string, at time.Time) error {
	return s.writeCampaignUse(ctx, candidate, campaign, at, func(u *CampaignUse, a time.Time, _ bool) {
		if u.Rejected.IsZero() || a.Before(u.Rejected) {
			u.Rejected, u.Reason = a, reason
		}
	})
}

// DeliverCampaignUse keeps the latest delivery.
func (s *Store) DeliverCampaignUse(ctx context.Context, candidate, campaign string, at time.Time) error {
	return s.writeCampaignUse(ctx, candidate, campaign, at, func(u *CampaignUse, a time.Time, exists bool) {
		if !exists || a.After(u.Delivered) {
			u.Delivered = a
		}
	})
}

func (s *Store) writeCampaignUse(ctx context.Context, candidate, campaign string, at time.Time, apply func(*CampaignUse, time.Time, bool)) error {
	return unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if at.IsZero() {
			return ErrUnreachable
		}
		if candidate == "" || campaign == "" {
			return ErrInvalid
		}
		c, err := candidateKey(ctx, tx, candidate)
		if err != nil {
			return err
		}
		for c.Status == StatusMerged {
			c, err = candidateKey(ctx, tx, c.MergedInto)
			if err != nil {
				return err
			}
		}
		us, err := readRecords[CampaignUse](ctx, tx, "campaign_uses", campaignUseColumns, " WHERE candidate=? AND campaign=?", c.ID, campaign)
		if err != nil {
			return err
		}
		u := CampaignUse{Candidate: c.ID, Campaign: campaign}
		if len(us) > 0 {
			u = us[0]
		}
		apply(&u, at.UTC().Truncate(time.Second), len(us) > 0)
		return insertRecord(ctx, tx, "campaign_uses", campaignUseColumns, u, nil)
	}))
}

// CampaignUses reads a candidate's own history, without following merges.
func (s *Store) CampaignUses(ctx context.Context, candidate string) ([]CampaignUse, error) {
	var out []CampaignUse
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if _, err := candidateKey(ctx, tx, candidate); err != nil {
			return err
		}
		var err error
		out, err = readRecords[CampaignUse](ctx, tx, "campaign_uses", campaignUseColumns, " WHERE candidate=? ORDER BY added DESC,campaign", candidate)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func mergeCampaignUses(ctx context.Context, tx *sql.Tx, survivor, other string) error {
	uses, err := readRecords[CampaignUse](ctx, tx, "campaign_uses", campaignUseColumns, " WHERE candidate IN (?,?) ORDER BY candidate,campaign", survivor, other)
	if err != nil {
		return err
	}
	kept := make(map[string]CampaignUse)
	for _, u := range uses {
		if u.Candidate == survivor {
			kept[u.Campaign] = u
		}
	}
	for _, v := range uses {
		if v.Candidate != other {
			continue
		}
		u, ok := kept[v.Campaign]
		if !ok {
			u = v
			u.Candidate = survivor
		} else {
			if !v.Added.IsZero() && (u.Added.IsZero() || v.Added.Before(u.Added)) {
				u.Added, u.Cohort = v.Added, v.Cohort
			}
			if !v.Rejected.IsZero() && (u.Rejected.IsZero() || v.Rejected.Before(u.Rejected)) {
				u.Rejected, u.Reason = v.Rejected, v.Reason
			}
			if v.Delivered.After(u.Delivered) {
				u.Delivered = v.Delivered
			}
		}
		if err = insertRecord(ctx, tx, "campaign_uses", campaignUseColumns, u, nil); err != nil {
			return err
		}
	}
	return deleteRecord(ctx, tx, "campaign_uses", "candidate=?", other)
}
