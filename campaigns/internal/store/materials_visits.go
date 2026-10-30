package store

import (
	"context"
	"database/sql"
	"time"
)

// Materials reads material references in their original addition order.
func (s *Store) Materials(ctx context.Context, c string) ([]Material, error) {
	return result(ctx, s, false, func(tx *sql.Tx) ([]Material, error) {
		if _, e := campaign(ctx, tx, c); e != nil {
			return nil, e
		}
		return query[Material](ctx, tx, "SELECT * FROM materials WHERE campaign=? ORDER BY seq", c)
	})
}

// AddMaterial adds a material reference or updates its label and artifact in place.
func (s *Store) AddMaterial(ctx context.Context, m Material) (Material, bool, error) {
	return change(ctx, s, func(tx *sql.Tx) (Material, bool, error) {
		if m.AddedUserID == "" || m.Artifact == "" {
			return Material{}, false, ErrUnreachable
		}
		if m.Path == "" || !ValidText(m.Label) {
			return Material{}, false, ErrInvalid
		}
		if _, e := campaign(ctx, tx, m.Campaign); e != nil {
			return Material{}, false, e
		}
		rs, e := query[Material](ctx, tx, "SELECT * FROM materials WHERE campaign=? AND path=?", m.Campaign, m.Path)
		if e != nil {
			return Material{}, false, e
		}
		if len(rs) > 0 {
			old := rs[0]
			if old.Label == m.Label && old.Artifact == m.Artifact {
				return old, false, nil
			}
			old.Label = m.Label
			old.Artifact = m.Artifact
			e = exec(ctx, tx, "UPDATE materials SET label=?,artifact=? WHERE campaign=? AND path=?", m.Label, m.Artifact, m.Campaign, m.Path)
			return old, true, e
		}
		m.Added = s.stamp()
		e = exec(ctx, tx, "INSERT INTO materials(campaign,path,label,artifact,added,added_user_id,added_user_email)VALUES(?,?,?,?,?,?,?)", m.Campaign, m.Path, m.Label, m.Artifact, text(m.Added), m.AddedUserID, m.AddedUserEmail)
		return m, true, e
	})
}

// RemoveMaterial removes a material reference if present.
func (s *Store) RemoveMaterial(ctx context.Context, c, path string) (Material, bool, error) {
	return change(ctx, s, func(tx *sql.Tx) (Material, bool, error) {
		if _, e := campaign(ctx, tx, c); e != nil {
			return Material{}, false, e
		}
		rs, e := query[Material](ctx, tx, "SELECT * FROM materials WHERE campaign=? AND path=?", c, path)
		if e != nil {
			return Material{}, false, e
		}
		if len(rs) == 0 {
			return Material{}, false, nil
		}
		e = exec(ctx, tx, "DELETE FROM materials WHERE campaign=? AND path=?", c, path)
		return rs[0], true, e
	})
}

// Visit records a visit and reads its current destination.
func (s *Store) Visit(ctx context.Context, token string, in VisitInput) (Visited, error) {
	return result(ctx, s, true, func(tx *sql.Tx) (Visited, error) {
		p, e := one[Piece](ctx, tx, "SELECT p.* FROM pieces p JOIN campaigns c ON c.id=p.campaign WHERE p.token=? AND c.deleted=0", token)
		if e != nil {
			return Visited{}, e
		}
		c, e := campaign(ctx, tx, p.Campaign)
		if e != nil {
			return Visited{}, e
		}
		at := s.stamp()
		if e = exec(ctx, tx, "INSERT INTO visits(piece,at,user_agent,referrer,address)VALUES(?,?,?,?,?)", p.ID, at.Format(time.RFC3339), in.UserAgent, in.Referrer, in.Address); e != nil {
			return Visited{}, e
		}
		r, e := tx.ExecContext(ctx, "UPDATE members SET opened_emitted=? WHERE campaign=? AND candidate=? AND opened_emitted=''", at.Format(time.RFC3339), p.Campaign, p.Candidate)
		if e != nil {
			return Visited{}, e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return Visited{}, e
		}
		first, _, e := firstVisit(ctx, tx, MemberKey{p.Campaign, p.Candidate})
		return Visited{p.Campaign, p.Candidate, p.ID, c.Destination, n > 0, first}, e
	})
}

func firstVisit(ctx context.Context, tx *sql.Tx, key MemberKey) (time.Time, bool, error) {
	var first sql.NullString
	e := tx.QueryRowContext(ctx, "SELECT min(v.at) FROM pieces p JOIN visits v ON v.piece=p.id WHERE p.campaign=? AND p.candidate=?", key.Campaign, key.Candidate).Scan(&first)
	if e != nil || !first.Valid {
		return time.Time{}, false, e
	}
	// Earlier releases encoded a visit at the zero clock as empty text.
	if first.String == "" {
		return time.Time{}, true, nil
	}
	at, e := time.Parse(time.RFC3339, first.String)
	return at, true, e
}

// MarkOpened examines a page of members and marks previously untold opens.
func (s *Store) MarkOpened(ctx context.Context, after MemberKey, n int) (MemberKey, []Opened, error) {
	type page struct {
		last   MemberKey
		opened []Opened
	}
	v, e := result(ctx, s, true, func(tx *sql.Tx) (page, error) {
		if n < 1 {
			return page{}, ErrUnreachable
		}
		members, e := query[MemberKey](ctx, tx, "SELECT campaign,candidate FROM members WHERE (campaign,candidate)>(?,?) ORDER BY campaign,candidate LIMIT ?", after.Campaign, after.Candidate, n)
		if e != nil {
			return page{}, e
		}
		out := page{opened: []Opened{}}
		if len(members) == n {
			out.last = members[len(members)-1]
		}
		at := s.stamp()
		for _, key := range members {
			first, found, e := firstVisit(ctx, tx, key)
			if e != nil {
				return page{}, e
			}
			if !found {
				continue
			}
			r, e := tx.ExecContext(ctx, "UPDATE members SET opened_emitted=? WHERE campaign=? AND candidate=? AND opened_emitted='' AND EXISTS(SELECT 1 FROM campaigns WHERE id=? AND deleted=0)", at.Format(time.RFC3339), key.Campaign, key.Candidate, key.Campaign)
			if e != nil {
				return page{}, e
			}
			changed, e := r.RowsAffected()
			if e != nil {
				return page{}, e
			}
			if changed > 0 {
				out.opened = append(out.opened, Opened{key.Campaign, key.Candidate, first})
			}
		}
		return out, nil
	})
	return v.last, v.opened, e
}

// VisitsFirst is the indexed SQL statement for this window.
const VisitsFirst = "SELECT seq FROM visits WHERE piece=? ORDER BY seq DESC LIMIT ?"

// VisitsAfter is the indexed SQL statement for this window.
const VisitsAfter = "SELECT seq FROM visits WHERE piece=? AND seq<? ORDER BY seq DESC LIMIT ?"

// ListVisits reads visits newest recorded first.
func (s *Store) ListVisits(ctx context.Context, id string, seq int64, n int) ([]Visit, error) {
	return result(ctx, s, false, func(tx *sql.Tx) ([]Visit, error) {
		if n < 1 {
			return nil, ErrUnreachable
		}
		if _, e := piece(ctx, tx, id); e != nil {
			return nil, e
		}
		q := VisitsFirst
		args := []any{id, n}
		if seq > 0 {
			q = VisitsAfter
			args = []any{id, seq, n}
		}
		rs, e := tx.QueryContext(ctx, q, args...)
		if e != nil {
			return nil, e
		}
		ids := []int64{}
		for rs.Next() {
			var v int64
			if e = rs.Scan(&v); e != nil {
				_ = rs.Close()
				return nil, e
			}
			ids = append(ids, v)
		}
		e = rs.Err()
		_ = rs.Close()
		if e != nil {
			return nil, e
		}
		out := []Visit{}
		for _, id := range ids {
			v, e := one[Visit](ctx, tx, "SELECT * FROM visits WHERE seq=?", id)
			if e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		return out, nil
	})
}
