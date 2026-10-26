package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Store describes a ledger value.
type Store struct {
	d   *db.DB
	cfg Config
	mu  sync.Mutex
}

// New builds a store over the supplied database handle.
func New(d *db.DB, cfg Config) *Store { return &Store{d: d, cfg: cfg} }
func (s *Store) do(ctx context.Context, write bool, fn func(*sql.Tx, time.Time) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrUnreachable, err)
	}
	run := s.d.Read
	if write {
		run = s.d.Write
	}
	err := run(ctx, func(tx *sql.Tx) error { return fn(tx, s.cfg.Now().UTC().Truncate(time.Second)) })
	if err == nil {
		return nil
	}
	for _, e := range []error{ErrNotFound, ErrInvalid, ErrLocked, ErrUnbalanced, ErrUnreachable} {
		if errors.Is(err, e) {
			return err
		}
	}
	return errors.Join(ErrUnreachable, err)
}
func stampText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
func parseStamp(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
func book(ctx context.Context, tx *sql.Tx) (Book, error) {
	var b Book
	var updated string
	err := tx.QueryRowContext(ctx, `SELECT b.currency,c.precision,b.lock_date,b.fiscal_year_start,b.updated,b.updated_user_id FROM book b JOIN commodities c ON c.code=b.currency WHERE b.id=1`).Scan(&b.Currency, &b.Precision, &b.LockDate, &b.FiscalYearStart, &updated, &b.UpdatedUserID)
	b.Updated = parseStamp(updated)
	return b, err
}
func amount(ctx context.Context, tx *sql.Tx, units int64) (Amount, error) {
	b, e := book(ctx, tx)
	return Amount{Units: units, Commodity: b.Currency, Precision: b.Precision}, e
}
func balance(ctx context.Context, tx *sql.Tx, prefix, date string) (int64, error) {
	var n int64
	e := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM postings WHERE date<=? AND (account=? OR (account>=? AND account<?))`, date, prefix, prefix+":", prefix+";").Scan(&n)
	return n, e
}
func locked(ctx context.Context, tx *sql.Tx, date string) (bool, error) {
	b, e := book(ctx, tx)
	return b.LockDate != "" && date <= b.LockDate, e
}
func (s *Store) mint(ctx context.Context, tx *sql.Tx, table, prefix string) (string, error) {
	switch table {
	case "transactions", "assertions", "notes", "documents":
	default:
		return "", ErrUnreachable
	}
	for range 8 {
		var bytes [8]byte
		if _, e := io.ReadFull(s.cfg.Rand, bytes[:]); e != nil {
			return "", errors.Join(ErrUnreachable, e)
		}
		id := prefix + hex.EncodeToString(bytes[:])
		var n int
		e := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=?", id).Scan(&n)
		if e != nil {
			return "", e
		}
		if n == 0 {
			return id, nil
		}
	}
	return "", ErrUnreachable
}
func status(ctx context.Context, tx *sql.Tx, today string) (Status, error) {
	var r Status
	var e error
	r.Book, e = book(ctx, tx)
	if e != nil {
		return Status{}, e
	}
	e = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM accounts WHERE close_date=''),(SELECT count(*) FROM transactions),(SELECT COALESCE(MAX(date),'') FROM transactions)`).Scan(&r.OpenAccounts, &r.Transactions, &r.LatestDate)
	if e != nil {
		return Status{}, e
	}
	r.FailingAssertions, e = statusFailing(ctx, tx, today)
	return r, e
}

// Status reads or changes the corresponding ledger records.
func (s *Store) Status(ctx context.Context) (Status, error) {
	var r Status
	e := s.do(ctx, false, func(tx *sql.Tx, t time.Time) error {
		var e error
		r, e = status(ctx, tx, t.Format(DateLayout))
		return e
	})
	if e != nil {
		return Status{}, e
	}
	return r, nil
}

// SetBook reads or changes the corresponding ledger records.
func (s *Store) SetBook(ctx context.Context, in BookEdit) (Status, bool, error) {
	var r Status
	changed := false
	e := s.do(ctx, true, func(tx *sql.Tx, t time.Time) error {
		if in.UserID == "" {
			return ErrUnreachable
		}
		if in.LockDate != nil && *in.LockDate != "" && !ValidDate(*in.LockDate) || in.FiscalYearStart != nil && !ValidMonthDay(*in.FiscalYearStart) {
			return ErrInvalid
		}
		b, e := book(ctx, tx)
		if e != nil {
			return e
		}
		lock, fiscal := b.LockDate, b.FiscalYearStart
		if in.LockDate != nil {
			lock = *in.LockDate
		}
		if in.FiscalYearStart != nil {
			fiscal = *in.FiscalYearStart
		}
		changed = lock != b.LockDate || fiscal != b.FiscalYearStart
		if changed {
			if _, e = tx.ExecContext(ctx, `UPDATE book SET lock_date=?,fiscal_year_start=?,updated=?,updated_user_id=? WHERE id=1`, lock, fiscal, stampText(t), in.UserID); e != nil {
				return e
			}
		}
		r, e = status(ctx, tx, t.Format(DateLayout))
		return e
	})
	if e != nil {
		return Status{}, false, e
	}
	return r, changed, nil
}
