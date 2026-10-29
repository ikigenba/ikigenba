package store

import (
	"context"
	"database/sql"

	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

var pageColumns = columns("candidate:Candidate seq:Number url:URL title:Title text:Text reason:Reason size:Size fetched:Fetched user_id:UserID user_email:UserEmail")

// AddPage records a new copy without replacing the candidate's older copies.
func (s *Store) AddPage(ctx context.Context, p Page) (Page, error) {
	var out Page
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if p.URL == "" || p.UserID == "" || p.Size < 0 || !validPageReason(p.Reason) || (p.Reason != "" && p.Text != "") {
			return ErrUnreachable
		}
		c, err := candidateKey(ctx, tx, p.Candidate)
		if err != nil {
			return err
		}
		if c.Status != StatusActive {
			return ErrInvalid
		}
		var urls, exists int64
		if err = scanSQL(ctx, tx, "SELECT COUNT(DISTINCT url),COALESCE(MAX(url=?),0) FROM pages WHERE candidate=?", []any{p.URL, p.Candidate}, &urls, &exists); err != nil {
			return err
		}
		if exists == 0 && urls >= PageCap {
			return ErrPageCap
		}
		p.Fetched = s.now()
		if err = execSQL(ctx, tx, "INSERT INTO pages(candidate,url,title,text,reason,size,fetched,user_id,user_email) VALUES (?,?,?,?,?,?,?,?,?)", p.Candidate, p.URL, p.Title, p.Text, p.Reason, p.Size, p.Fetched.Format("2006-01-02T15:04:05Z07:00"), p.UserID, p.UserEmail); err != nil {
			return err
		}
		if err = scanSQL(ctx, tx, "SELECT last_insert_rowid()", nil, &p.Number); err != nil {
			return err
		}
		out = p
		return nil
	}))
	if err != nil {
		return Page{}, err
	}
	return out, nil
}

// Pages lists the newest copy of each URL without its text.
func (s *Store) Pages(ctx context.Context, candidate string) ([]Page, error) {
	var out []Page
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if _, err := candidateKey(ctx, tx, candidate); err != nil {
			return err
		}
		var err error
		out, err = readQueryRecords[Page](ctx, tx, pageColumns, "SELECT candidate,seq,url,title,'' AS text,COALESCE(reason,'') AS reason,size,fetched,user_id,user_email FROM pages WHERE candidate=? AND seq IN (SELECT MAX(seq) FROM pages WHERE candidate=? GROUP BY url) ORDER BY seq DESC", candidate, candidate)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetPage reads one stored copy on the candidate that currently holds it.
func (s *Store) GetPage(ctx context.Context, candidate string, number int64) (Page, error) {
	var out Page
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		ps, err := readQueryRecords[Page](ctx, tx, pageColumns, "SELECT candidate,seq,url,title,text,COALESCE(reason,'') AS reason,size,fetched,user_id,user_email FROM pages WHERE candidate=? AND seq=?", candidate, number)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			return ErrNotFound
		}
		out = ps[0]
		return nil
	}))
	if err != nil {
		return Page{}, err
	}
	return out, nil
}

// CountPages counts distinct URLs and all copies kept on a candidate.
func (s *Store) CountPages(ctx context.Context, candidate string) (PageCount, error) {
	var out PageCount
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if _, err := candidateKey(ctx, tx, candidate); err != nil {
			return err
		}
		return scanSQL(ctx, tx, "SELECT COUNT(DISTINCT url),COUNT(*) FROM pages WHERE candidate=?", []any{candidate}, &out.URLs, &out.Copies)
	}))
	if err != nil {
		return PageCount{}, err
	}
	return out, nil
}

// Refetch outcomes distinguish readable, unreadable and failed fetches.
const (
	RefetchRead       = "read"
	RefetchUnreadable = "unreadable"
	RefetchFailed     = "failed"
)

func validPageReason(reason string) bool {
	return reason == "" || reason == website.ReasonScripted || reason == website.ReasonRedirect || reason == website.ReasonEmpty
}

// PendingRefetches reads a bounded page of the migration's unsettled work list.
func (s *Store) PendingRefetches(ctx context.Context, after int64, n int) ([]Page, error) {
	var out []Page
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		var err error
		out, err = readQueryRecords[Page](ctx, tx, pageColumns, "SELECT candidate,pages.seq,url,title,text,COALESCE(reason,'') AS reason,size,fetched,user_id,user_email FROM pages JOIN page_refetches ON page_refetches.seq=pages.seq WHERE page_refetches.outcome IS NULL AND pages.seq>? ORDER BY pages.seq LIMIT ?", after, n)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SettleRefetch keeps the first outcome for an existing migration work entry.
func (s *Store) SettleRefetch(ctx context.Context, number int64, outcome string) error {
	return unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if outcome != RefetchRead && outcome != RefetchUnreadable && outcome != RefetchFailed {
			return ErrUnreachable
		}
		var count int64
		if err := scanSQL(ctx, tx, "SELECT COUNT(*) FROM page_refetches WHERE seq=?", []any{number}, &count); err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		return execSQL(ctx, tx, "UPDATE page_refetches SET outcome=? WHERE seq=? AND outcome IS NULL", outcome, number)
	}))
}
