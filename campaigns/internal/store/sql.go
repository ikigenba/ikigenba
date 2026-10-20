package store

import (
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"io"
	"net/url"
	"reflect"
	"strings"
	"time"
)

func validID(s, p string) bool {
	if !strings.HasPrefix(s, p) || len(s) != len(p)+16 {
		return false
	}
	for _, c := range s[len(p):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ValidCampaignID accepts campaign ids.
func ValidCampaignID(s string) bool { return validID(s, CampaignIDPrefix) }

// ValidPieceID accepts piece ids.
func ValidPieceID(s string) bool { return validID(s, PieceIDPrefix) }

// ValidCandidateID accepts candidate ids.
func ValidCandidateID(s string) bool { return validID(s, "cand_") }

// ValidCohortID accepts cohort ids.
func ValidCohortID(s string) bool { return validID(s, "coh_") }

// ValidToken accepts a lowercase base32 token of length 26.
func ValidToken(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

// ValidText accepts text that is not entirely whitespace.
func ValidText(s string) bool { return strings.TrimSpace(s) != "" }

// ValidChannel accepts mail or email.
func ValidChannel(s string) bool { return s == ChannelMail || s == ChannelEmail }
func channelSet(cs []string) bool {
	seen := map[string]bool{}
	for _, c := range cs {
		if !ValidChannel(c) || seen[c] {
			return false
		}
		seen[c] = true
	}
	return true
}

// ValidChannels accepts a nonempty set of supported channels.
func ValidChannels(cs []string) bool { return len(cs) > 0 && channelSet(cs) }

// ValidDestination accepts absolute HTTP and HTTPS URLs with a host.
func ValidDestination(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
func channels(cs []string) []string {
	r := []string{}
	for _, c := range []string{ChannelMail, ChannelEmail} {
		for _, v := range cs {
			if c == v {
				r = append(r, c)
			}
		}
	}
	return r
}
func has(cs []string, c string) bool {
	for _, v := range cs {
		if v == c {
			return true
		}
	}
	return false
}
func text(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
func query[T any](ctx context.Context, tx *sql.Tx, q string, args ...any) ([]T, error) {
	rows, e := tx.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	out := []T{}
	for rows.Next() {
		var v T
		rv := reflect.ValueOf(&v).Elem()
		vals := make([]any, len(cols))
		for i := range vals {
			vals[i] = new(any)
		}
		if e = rows.Scan(vals...); e != nil {
			return nil, e
		}
		for i, c := range cols {
			key := strings.ReplaceAll(c, "_", "")
			for j := 0; j < rv.NumField(); j++ {
				if strings.ToLower(rv.Type().Field(j).Name) != key {
					continue
				}
				f := rv.Field(j)
				x := *(vals[i].(*any))
				switch f.Interface().(type) {
				case time.Time:
					st, _ := x.(string)
					t, _ := time.Parse(time.RFC3339, st)
					f.Set(reflect.ValueOf(t))
				case []string:
					st, _ := x.(string)
					cs := []string{}
					if st != "" {
						cs = strings.Split(st, ",")
					}
					f.Set(reflect.ValueOf(cs))
				default:
					switch f.Kind() {
					case reflect.String:
						if x != nil {
							f.SetString(x.(string))
						}
					case reflect.Bool:
						f.SetBool(x.(int64) != 0)
					case reflect.Int64:
						f.SetInt(x.(int64))
					}
				}
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func one[T any](ctx context.Context, tx *sql.Tx, q string, args ...any) (T, error) {
	rs, e := query[T](ctx, tx, q, args...)
	if e != nil {
		var z T
		return z, e
	}
	if len(rs) == 0 {
		var z T
		return z, ErrNotFound
	}
	return rs[0], nil
}
func exec(ctx context.Context, tx *sql.Tx, q string, args ...any) error {
	_, e := tx.ExecContext(ctx, q, args...)
	return e
}
func campaign(ctx context.Context, tx *sql.Tx, id string) (Campaign, error) {
	return one[Campaign](ctx, tx, "SELECT * FROM campaigns WHERE id=? AND deleted=0", id)
}
func piece(ctx context.Context, tx *sql.Tx, id string) (Piece, error) {
	return one[Piece](ctx, tx, "SELECT p.* FROM pieces p JOIN campaigns c ON c.id=p.campaign WHERE p.id=? AND c.deleted=0", id)
}
func member(ctx context.Context, tx *sql.Tx, c, id string) (Member, error) {
	if _, e := campaign(ctx, tx, c); e != nil {
		return Member{}, e
	}
	return one[Member](ctx, tx, "SELECT * FROM members WHERE campaign=? AND candidate=?", c, id)
}
func (s *Store) mint(ctx context.Context, tx *sql.Tx, table, col, prefix string, size int) (string, error) {
	for range 8 {
		b := make([]byte, size)
		if _, e := io.ReadFull(s.cfg.Rand, b); e != nil {
			return "", e
		}
		v := prefix + hex.EncodeToString(b)
		if size == 16 {
			v = strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
		}
		var n int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE "+col+"=?", v).Scan(&n); e != nil {
			return "", e
		}
		if n == 0 {
			return v, nil
		}
	}
	return "", ErrUnreachable
}
func engagement(ctx context.Context, tx *sql.Tx, id string) (PieceEngagement, error) {
	var v PieceEngagement
	var a, b sql.NullString
	e := tx.QueryRowContext(ctx, "SELECT count(*), min(at), max(at) FROM visits WHERE piece=?", id).Scan(&v.Opens, &a, &b)
	v.First, _ = time.Parse(time.RFC3339, a.String)
	v.Last, _ = time.Parse(time.RFC3339, b.String)
	return v, e
}
func memberRow(ctx context.Context, tx *sql.Tx, m Member) (MemberRow, error) {
	r := MemberRow{Member: m, Pieces: []PieceRow{}}
	ps, e := query[Piece](ctx, tx, "SELECT * FROM pieces WHERE campaign=? AND candidate=? ORDER BY CASE channel WHEN 'mail' THEN 0 ELSE 1 END", m.Campaign, m.Candidate)
	if e != nil {
		return MemberRow{}, e
	}
	for _, p := range ps {
		g, e := engagement(ctx, tx, p.ID)
		if e != nil {
			return MemberRow{}, e
		}
		r.Pieces = append(r.Pieces, PieceRow{p, g})
		if g.Opens > 0 {
			r.Engagement.Opens += g.Opens
			if r.Engagement.First.IsZero() || g.First.Before(r.Engagement.First) {
				r.Engagement.First = g.First
			}
			if g.Last.After(r.Engagement.Last) {
				r.Engagement.Last = g.Last
			}
		}
	}
	r.Engagement.Opened = r.Engagement.Opens > 0
	return r, nil
}
func counts(ctx context.Context, tx *sql.Tx, id string) (Counts, error) {
	var c Counts
	e := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM members WHERE campaign=?),(SELECT count(*) FROM pieces WHERE campaign=? AND state='delivered'),(SELECT count(*) FROM members m WHERE campaign=? AND EXISTS(SELECT 1 FROM pieces p JOIN visits v ON v.piece=p.id WHERE p.campaign=m.campaign AND p.candidate=m.candidate))`, id, id, id).Scan(&c.Members, &c.Delivered, &c.Opened)
	return c, e
}
