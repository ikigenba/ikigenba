package store

import (
	"context"
	"database/sql"
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
		e = exec(ctx, tx, "INSERT INTO visits(piece,at,user_agent,referrer,address)VALUES(?,?,?,?,?)", p.ID, text(s.stamp()), in.UserAgent, in.Referrer, in.Address)
		return Visited{p.Campaign, p.ID, c.Destination}, e
	})
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
