package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const accountColumns = `name,open_date,close_date,opened,opened_user_id,closed,closed_user_id`

func scanAccount(row interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var opened, closed string
	e := row.Scan(&a.Name, &a.Open, &a.Close, &opened, &a.OpenedUserID, &closed, &a.ClosedUserID)
	a.Opened = parseStamp(opened)
	a.Closed = parseStamp(closed)
	return a, e
}
func getAccount(ctx context.Context, tx *sql.Tx, name string) (Account, error) {
	a, e := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE name=?`, name))
	if errors.Is(e, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, e
}

// OpenAccount reads or changes the corresponding ledger records.
func (s *Store) OpenAccount(ctx context.Context, in AccountInput) (Account, bool, error) {
	var a Account
	changed := false
	e := s.do(ctx, true, func(tx *sql.Tx, t time.Time) error {
		if in.UserID == "" {
			return ErrUnreachable
		}
		if !ValidAccountName(in.Name) {
			return ErrInvalid
		}
		var e error
		a, e = getAccount(ctx, tx, in.Name)
		if e == nil {
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if !ValidDate(in.Date) {
			return ErrInvalid
		}
		a = Account{Name: in.Name, Open: in.Date, Opened: t, OpenedUserID: in.UserID}
		_, e = tx.ExecContext(ctx, `INSERT INTO accounts (name,open_date,opened,opened_user_id) VALUES (?,?,?,?)`, a.Name, a.Open, stampText(a.Opened), a.OpenedUserID)
		changed = e == nil
		return e
	})
	if e != nil {
		return Account{}, false, e
	}
	return a, changed, nil
}

// CloseAccount reads or changes the corresponding ledger records.
func (s *Store) CloseAccount(ctx context.Context, in AccountInput) (Account, bool, error) {
	var a Account
	changed := false
	e := s.do(ctx, true, func(tx *sql.Tx, t time.Time) error {
		if in.UserID == "" {
			return ErrUnreachable
		}
		if !ValidAccountName(in.Name) {
			return ErrInvalid
		}
		var e error
		a, e = getAccount(ctx, tx, in.Name)
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		if e == nil && a.Close != "" {
			return nil
		}
		if !ValidDate(in.Date) {
			return ErrInvalid
		}
		if errors.Is(e, ErrNotFound) {
			return e
		}
		var latest string
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(date),'') FROM postings WHERE account=?`, in.Name).Scan(&latest); e != nil {
			return e
		}
		if in.Date < a.Open || in.Date < latest {
			return ErrInvalid
		}
		a.Close = in.Date
		a.Closed = t
		a.ClosedUserID = in.UserID
		_, e = tx.ExecContext(ctx, `UPDATE accounts SET close_date=?,closed=?,closed_user_id=? WHERE name=?`, a.Close, stampText(a.Closed), a.ClosedUserID, a.Name)
		changed = e == nil
		return e
	})
	if e != nil {
		return Account{}, false, e
	}
	return a, changed, nil
}

// Accounts reads or changes the corresponding ledger records.
func (s *Store) Accounts(ctx context.Context) ([]Account, error) {
	r := []Account{}
	e := s.do(ctx, false, func(tx *sql.Tx, _ time.Time) error {
		rows, e := tx.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY name`)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			a, e := scanAccount(rows)
			if e != nil {
				return e
			}
			r = append(r, a)
		}
		return rows.Err()
	})
	if e != nil {
		return nil, e
	}
	return r, nil
}

// GetAccount reads or changes the corresponding ledger records.
func (s *Store) GetAccount(ctx context.Context, name, date string) (AccountRead, error) {
	var r AccountRead
	e := s.do(ctx, false, func(tx *sql.Tx, t time.Time) error {
		if !ValidAccountName(name) || date != "" && !ValidDate(date) {
			return ErrInvalid
		}
		var e error
		r.Account, e = getAccount(ctx, tx, name)
		if e != nil {
			return e
		}
		r.Date = date
		if r.Date == "" {
			r.Date = t.Format(DateLayout)
		}
		n, e := balance(ctx, tx, name, r.Date)
		if e != nil {
			return e
		}
		r.Balance, e = amount(ctx, tx, n)
		if e != nil {
			return e
		}
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM postings WHERE account=?`, name).Scan(&r.Postings); e != nil {
			return e
		}
		r.Assertions, r.Notes, r.Documents, e = accountRecords(ctx, tx, name)
		return e
	})
	if e != nil {
		return AccountRead{}, e
	}
	return r, nil
}
