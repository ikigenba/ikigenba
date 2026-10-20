package store

import (
	"context"
	"database/sql"
	"strings"
)

type changed[T any] struct {
	Value   T
	Changed bool
}

func result[T any](ctx context.Context, s *Store, w bool, f func(*sql.Tx) (T, error)) (T, error) {
	var v T
	e := s.run(ctx, w, func(tx *sql.Tx) error { var e error; v, e = f(tx); return e })
	if e != nil {
		var z T
		return z, e
	}
	return v, nil
}
func change[T any](ctx context.Context, s *Store, f func(*sql.Tx) (T, bool, error)) (T, bool, error) {
	v, e := result(ctx, s, true, func(tx *sql.Tx) (changed[T], error) { v, b, e := f(tx); return changed[T]{v, b}, e })
	return v.Value, v.Changed, e
}

// CreateCampaign creates a new campaign with an id drawn from Rand.
func (s *Store) CreateCampaign(ctx context.Context, c Campaign) (Campaign, error) {
	return result(ctx, s, true, func(tx *sql.Tx) (Campaign, error) {
		if c.CreatedUserID == "" {
			return Campaign{}, ErrUnreachable
		}
		if !ValidText(c.Name) || !ValidChannels(c.Channels) || !ValidDestination(c.Destination) {
			return Campaign{}, ErrInvalid
		}
		id, e := s.mint(ctx, tx, "campaigns", "id", CampaignIDPrefix, 8)
		if e != nil {
			return Campaign{}, e
		}
		c.ID = id
		c.Created = s.stamp()
		c.Deleted = false
		c.Channels = channels(c.Channels)
		e = exec(ctx, tx, "INSERT INTO campaigns(id,name,description,channels,destination,created,created_user_id,created_user_email) VALUES(?,?,?,?,?,?,?,?)", c.ID, c.Name, c.Description, strings.Join(c.Channels, ","), c.Destination, text(c.Created), c.CreatedUserID, c.CreatedUserEmail)
		return c, e
	})
}

// GetCampaign reads a live campaign.
func (s *Store) GetCampaign(ctx context.Context, id string) (Campaign, error) {
	return result(ctx, s, false, func(tx *sql.Tx) (Campaign, error) { return campaign(ctx, tx, id) })
}

// EditCampaign updates the provided editable parts of a live campaign.
func (s *Store) EditCampaign(ctx context.Context, in CampaignEdit) (Campaign, bool, error) {
	return change(ctx, s, func(tx *sql.Tx) (Campaign, bool, error) {
		if in.Name != nil && !ValidText(*in.Name) || in.Destination != nil && !ValidDestination(*in.Destination) {
			return Campaign{}, false, ErrInvalid
		}
		c, e := campaign(ctx, tx, in.ID)
		if e != nil {
			return Campaign{}, false, e
		}
		old := c
		for _, p := range []struct {
			dst *string
			src *string
		}{{&c.Name, in.Name}, {&c.Description, in.Description}, {&c.Destination, in.Destination}} {
			if p.src != nil {
				*p.dst = *p.src
			}
		}
		b := c.Name != old.Name || c.Description != old.Description || c.Destination != old.Destination
		if b {
			e = exec(ctx, tx, "UPDATE campaigns SET name=?,description=?,destination=? WHERE id=?", c.Name, c.Description, c.Destination, c.ID)
		}
		return c, b, e
	})
}

// DeleteCampaign marks a campaign deleted unless a piece was delivered.
func (s *Store) DeleteCampaign(ctx context.Context, id string) (Campaign, bool, error) {
	return change(ctx, s, func(tx *sql.Tx) (Campaign, bool, error) {
		c, e := one[Campaign](ctx, tx, "SELECT * FROM campaigns WHERE id=?", id)
		if e != nil {
			return Campaign{}, false, e
		}
		if c.Deleted {
			return c, false, nil
		}
		n, e := counts(ctx, tx, id)
		if e != nil {
			return Campaign{}, false, e
		}
		if n.Delivered > 0 {
			return Campaign{}, false, ErrInvalid
		}
		c.Deleted = true
		e = exec(ctx, tx, "UPDATE campaigns SET deleted=1 WHERE id=?", id)
		return c, true, e
	})
}

// Counts summarizes campaign membership, delivery and engagement.
func (s *Store) Counts(ctx context.Context, id string) (Counts, error) {
	return result(ctx, s, false, func(tx *sql.Tx) (Counts, error) {
		if _, e := campaign(ctx, tx, id); e != nil {
			return Counts{}, e
		}
		return counts(ctx, tx, id)
	})
}

// CampaignsFirst is the indexed SQL statement for this window.
const CampaignsFirst = "SELECT id FROM campaigns WHERE deleted=0 ORDER BY created DESC,id DESC LIMIT ?"

// CampaignsAfter is the indexed SQL statement for this window.
const CampaignsAfter = "SELECT id FROM campaigns WHERE deleted=0 AND (created,id)<(?,?) ORDER BY created DESC,id DESC LIMIT ?"

// CampaignsLast is the indexed SQL statement for this window.
const CampaignsLast = "SELECT id FROM campaigns WHERE deleted=0 ORDER BY created,id LIMIT ?"

// CampaignsPreceding is the indexed SQL statement for this window.
const CampaignsPreceding = "SELECT id FROM campaigns WHERE deleted=0 AND (created,id)>(?,?) ORDER BY created,id LIMIT ?"

func keys(ctx context.Context, tx *sql.Tx, q string, args ...any) ([]string, error) {
	rs, e := tx.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rs.Close() }()
	out := []string{}
	for rs.Next() {
		var v string
		if e = rs.Scan(&v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rs.Err()
}
func (s *Store) campaignWindow(ctx context.Context, created, id string, n int, reverse bool) ([]CampaignRow, error) {
	return result(ctx, s, false, func(tx *sql.Tx) ([]CampaignRow, error) {
		if n < 1 {
			return nil, ErrUnreachable
		}
		q := CampaignsFirst
		args := []any{n}
		if reverse {
			q = CampaignsLast
		}
		if id != "" {
			q = CampaignsAfter
			if reverse {
				q = CampaignsPreceding
			}
			args = []any{created, id, n}
		}
		ids, e := keys(ctx, tx, q, args...)
		if e != nil {
			return nil, e
		}
		out := []CampaignRow{}
		for _, id := range ids {
			c, e := campaign(ctx, tx, id)
			if e != nil {
				return nil, e
			}
			g, e := counts(ctx, tx, id)
			if e != nil {
				return nil, e
			}
			out = append(out, CampaignRow{c, g})
		}
		return out, nil
	})
}

// ListCampaigns reads a window in newest-first campaign order.
func (s *Store) ListCampaigns(ctx context.Context, created, id string, n int) ([]CampaignRow, error) {
	return s.campaignWindow(ctx, created, id, n, false)
}

// ListCampaignsReversed reads a window in oldest-first campaign order.
func (s *Store) ListCampaignsReversed(ctx context.Context, created, id string, n int) ([]CampaignRow, error) {
	return s.campaignWindow(ctx, created, id, n, true)
}
