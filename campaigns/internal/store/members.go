package store

import (
	"context"
	"database/sql"
)

// GetMember reads a member with its pieces and engagement.
func (s *Store) GetMember(ctx context.Context, c, id string) (MemberRow, error) {
	return result(ctx, s, false, func(tx *sql.Tx) (MemberRow, error) {
		m, e := member(ctx, tx, c, id)
		if e != nil {
			return MemberRow{}, e
		}
		return memberRow(ctx, tx, m)
	})
}

// GetPiece reads a piece and its engagement.
func (s *Store) GetPiece(ctx context.Context, id string) (PieceRow, error) {
	return result(ctx, s, false, func(tx *sql.Tx) (PieceRow, error) {
		p, e := piece(ctx, tx, id)
		if e != nil {
			return PieceRow{}, e
		}
		g, e := engagement(ctx, tx, id)
		return PieceRow{p, g}, e
	})
}

// AddMember adds a member and its pieces together, or returns the existing member.
func (s *Store) AddMember(ctx context.Context, in MemberInput) (MemberRow, bool, error) {
	return change(ctx, s, func(tx *sql.Tx) (MemberRow, bool, error) {
		if in.UserID == "" {
			return MemberRow{}, false, ErrUnreachable
		}
		if !ValidCandidateID(in.Candidate) || in.Cohort != "" && !ValidCohortID(in.Cohort) || !channelSet(in.Channels) {
			return MemberRow{}, false, ErrInvalid
		}
		c, e := campaign(ctx, tx, in.Campaign)
		if e != nil {
			return MemberRow{}, false, e
		}
		ms, e := query[Member](ctx, tx, "SELECT * FROM members WHERE campaign=? AND candidate=?", in.Campaign, in.Candidate)
		if e != nil {
			return MemberRow{}, false, e
		}
		if len(ms) > 0 {
			r, e := memberRow(ctx, tx, ms[0])
			return r, false, e
		}
		for _, v := range in.Channels {
			if !has(c.Channels, v) {
				return MemberRow{}, false, ErrInvalid
			}
		}
		m := Member{Campaign: in.Campaign, Candidate: in.Candidate, Cohort: in.Cohort, Added: s.stamp(), AddedUserID: in.UserID, AddedUserEmail: in.UserEmail}
		if e = exec(ctx, tx, "INSERT INTO members(campaign,candidate,cohort,added,added_user_id,added_user_email)VALUES(?,?,?,?,?,?)", m.Campaign, m.Candidate, m.Cohort, text(m.Added), m.AddedUserID, m.AddedUserEmail); e != nil {
			return MemberRow{}, false, e
		}
		for _, ch := range channels(in.Channels) {
			id, e := s.mint(ctx, tx, "pieces", "id", PieceIDPrefix, 8)
			if e != nil {
				return MemberRow{}, false, e
			}
			token, e := s.mint(ctx, tx, "pieces", "token", "", 16)
			if e != nil {
				return MemberRow{}, false, e
			}
			if e = exec(ctx, tx, "INSERT INTO pieces(id,campaign,candidate,channel,state,token,created)VALUES(?,?,?,?,?,?,?)", id, m.Campaign, m.Candidate, ch, StatePlanned, token, text(m.Added)); e != nil {
				return MemberRow{}, false, e
			}
		}
		r, e := memberRow(ctx, tx, m)
		return r, true, e
	})
}

// RejectMember records the first rejection without changing pieces.
func (s *Store) RejectMember(ctx context.Context, in Rejection) (MemberRow, bool, error) {
	return change(ctx, s, func(tx *sql.Tx) (MemberRow, bool, error) {
		if in.UserID == "" {
			return MemberRow{}, false, ErrUnreachable
		}
		if !ValidText(in.Why) {
			return MemberRow{}, false, ErrInvalid
		}
		m, e := member(ctx, tx, in.Campaign, in.Candidate)
		if e != nil {
			return MemberRow{}, false, e
		}
		b := !m.Rejected
		if b {
			m.Rejected = true
			m.RejectedAt = s.stamp()
			m.RejectedUserID = in.UserID
			m.RejectedUserEmail = in.UserEmail
			m.Why = in.Why
			if e = exec(ctx, tx, "UPDATE members SET rejected=1,rejected_at=?,rejected_user_id=?,rejected_user_email=?,why=? WHERE campaign=? AND candidate=?", text(m.RejectedAt), in.UserID, in.UserEmail, in.Why, in.Campaign, in.Candidate); e != nil {
				return MemberRow{}, false, e
			}
		}
		r, e := memberRow(ctx, tx, m)
		return r, b, e
	})
}
func (s *Store) action(ctx context.Context, in PieceAction, deliver bool) (Piece, bool, error) {
	return change(ctx, s, func(tx *sql.Tx) (Piece, bool, error) {
		if in.UserID == "" {
			return Piece{}, false, ErrUnreachable
		}
		p, e := piece(ctx, tx, in.Piece)
		if e != nil {
			return Piece{}, false, e
		}
		target := StateDropped
		if deliver {
			target = StateDelivered
		}
		if p.State == target {
			return p, false, nil
		}
		if p.State != StatePlanned {
			return Piece{}, false, ErrInvalid
		}
		if deliver {
			var m Member
			m, e = member(ctx, tx, p.Campaign, p.Candidate)
			if e != nil {
				return Piece{}, false, e
			}
			if m.Rejected {
				return Piece{}, false, ErrInvalid
			}
			p.Delivered = s.stamp()
			p.DeliveredUserID = in.UserID
			p.DeliveredUserEmail = in.UserEmail
			p.How = HowHand
			e = exec(ctx, tx, "UPDATE pieces SET state=?,delivered=?,delivered_user_id=?,delivered_user_email=?,how=? WHERE id=?", target, text(p.Delivered), in.UserID, in.UserEmail, HowHand, p.ID)
		} else {
			p.Dropped = s.stamp()
			p.DroppedUserID = in.UserID
			p.DroppedUserEmail = in.UserEmail
			e = exec(ctx, tx, "UPDATE pieces SET state=?,dropped=?,dropped_user_id=?,dropped_user_email=? WHERE id=?", target, text(p.Dropped), in.UserID, in.UserEmail, p.ID)
		}
		p.State = target
		return p, true, e
	})
}

// DropPiece drops a planned piece.
func (s *Store) DropPiece(ctx context.Context, in PieceAction) (Piece, bool, error) {
	return s.action(ctx, in, false)
}

// DeliverPiece marks a planned piece delivered by hand.
func (s *Store) DeliverPiece(ctx context.Context, in PieceAction) (Piece, bool, error) {
	return s.action(ctx, in, true)
}

// DeliverChannel delivers the eligible planned pieces in member order.
func (s *Store) DeliverChannel(ctx context.Context, in ChannelDelivery) ([]Piece, error) {
	return result(ctx, s, true, func(tx *sql.Tx) ([]Piece, error) {
		if in.UserID == "" {
			return nil, ErrUnreachable
		}
		if !ValidChannel(in.Channel) {
			return nil, ErrInvalid
		}
		c, e := campaign(ctx, tx, in.Campaign)
		if e != nil {
			return nil, e
		}
		if !has(c.Channels, in.Channel) {
			return nil, ErrInvalid
		}
		ps, e := query[Piece](ctx, tx, "SELECT p.* FROM pieces p JOIN members m ON m.campaign=p.campaign AND m.candidate=p.candidate WHERE p.campaign=? AND p.channel=? AND p.state='planned' AND m.rejected=0 ORDER BY p.created,p.candidate", in.Campaign, in.Channel)
		if e != nil {
			return nil, e
		}
		stamp := s.stamp()
		for i := range ps {
			p := &ps[i]
			p.State = StateDelivered
			p.Delivered = stamp
			p.DeliveredUserID = in.UserID
			p.DeliveredUserEmail = in.UserEmail
			p.How = HowHand
			if e = exec(ctx, tx, "UPDATE pieces SET state=?,delivered=?,delivered_user_id=?,delivered_user_email=?,how=? WHERE id=?", p.State, text(stamp), in.UserID, in.UserEmail, HowHand, p.ID); e != nil {
				return nil, e
			}
		}
		return ps, nil
	})
}

// MembersFirst is the indexed SQL statement for this window.
const MembersFirst = "SELECT candidate FROM members WHERE campaign=? ORDER BY added,candidate LIMIT ?"

// MembersAfter is the indexed SQL statement for this window.
const MembersAfter = "SELECT candidate FROM members WHERE campaign=? AND (added,candidate)>(?,?) ORDER BY added,candidate LIMIT ?"

// MembersLast is the indexed SQL statement for this window.
const MembersLast = "SELECT candidate FROM members WHERE campaign=? ORDER BY added DESC,candidate DESC LIMIT ?"

// MembersPreceding is the indexed SQL statement for this window.
const MembersPreceding = "SELECT candidate FROM members WHERE campaign=? AND (added,candidate)<(?,?) ORDER BY added DESC,candidate DESC LIMIT ?"

func (s *Store) memberWindow(ctx context.Context, c, added, id string, n int, rev bool) ([]MemberRow, error) {
	return result(ctx, s, false, func(tx *sql.Tx) ([]MemberRow, error) {
		if n < 1 {
			return nil, ErrUnreachable
		}
		if _, e := campaign(ctx, tx, c); e != nil {
			return nil, e
		}
		q := MembersFirst
		if rev {
			q = MembersLast
		}
		args := []any{c, n}
		if id != "" {
			q = MembersAfter
			if rev {
				q = MembersPreceding
			}
			args = []any{c, added, id, n}
		}
		ids, e := keys(ctx, tx, q, args...)
		if e != nil {
			return nil, e
		}
		out := []MemberRow{}
		for _, id := range ids {
			m, e := member(ctx, tx, c, id)
			if e != nil {
				return nil, e
			}
			r, e := memberRow(ctx, tx, m)
			if e != nil {
				return nil, e
			}
			out = append(out, r)
		}
		return out, nil
	})
}

// ListMembers reads a window in member order.
func (s *Store) ListMembers(ctx context.Context, c, added, id string, n int) ([]MemberRow, error) {
	return s.memberWindow(ctx, c, added, id, n, false)
}

// ListMembersReversed reads a window in reverse member order.
func (s *Store) ListMembersReversed(ctx context.Context, c, added, id string, n int) ([]MemberRow, error) {
	return s.memberWindow(ctx, c, added, id, n, true)
}

// MergeFirst is the indexed SQL statement for this window.
const MergeFirst = "SELECT p.id FROM pieces p CROSS JOIN members m ON m.campaign=p.campaign AND m.candidate=p.candidate WHERE p.campaign=? AND p.channel=? AND (?=1 OR (p.state='planned' AND m.rejected=0)) ORDER BY p.created,p.candidate LIMIT ?"

// MergeAfter is the indexed SQL statement for this window.
const MergeAfter = "SELECT p.id FROM pieces p CROSS JOIN members m ON m.campaign=p.campaign AND m.candidate=p.candidate WHERE p.campaign=? AND p.channel=? AND (?=1 OR (p.state='planned' AND m.rejected=0)) AND (p.created,p.candidate)>(?,?) ORDER BY p.created,p.candidate LIMIT ?"

// MergeList reads a channel’s pieces in member order.
func (s *Store) MergeList(ctx context.Context, c, ch string, all bool, created, id string, n int) ([]MergeRow, error) {
	return result(ctx, s, false, func(tx *sql.Tx) ([]MergeRow, error) {
		if n < 1 {
			return nil, ErrUnreachable
		}
		if !ValidChannel(ch) {
			return nil, ErrInvalid
		}
		ca, e := campaign(ctx, tx, c)
		if e != nil {
			return nil, e
		}
		if !has(ca.Channels, ch) {
			return nil, ErrInvalid
		}
		scope := 0
		if all {
			scope = 1
		}
		q := MergeFirst
		args := []any{c, ch, scope, n}
		if id != "" {
			q = MergeAfter
			args = []any{c, ch, scope, created, id, n}
		}
		ids, e := keys(ctx, tx, q, args...)
		if e != nil {
			return nil, e
		}
		out := []MergeRow{}
		for _, id := range ids {
			p, e := piece(ctx, tx, id)
			if e != nil {
				return nil, e
			}
			m, e := member(ctx, tx, c, p.Candidate)
			if e != nil {
				return nil, e
			}
			out = append(out, MergeRow{p, m.Rejected})
		}
		return out, nil
	})
}
