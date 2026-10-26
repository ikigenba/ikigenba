package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

func assertionRow(ctx context.Context, tx *sql.Tx, id string) (AssertionRow, error) {
	var r AssertionRow
	var added, retracted string
	var units int64
	err := tx.QueryRowContext(ctx, `SELECT id,account,date,amount,added,added_user_id,retracted,retracted_user_id FROM assertions WHERE id=?`, id).Scan(&r.Assertion.ID, &r.Assertion.Account, &r.Assertion.Date, &units, &added, &r.Assertion.AddedUserID, &retracted, &r.Assertion.RetractedUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return AssertionRow{}, ErrNotFound
	}
	if err != nil {
		return AssertionRow{}, err
	}
	r.Assertion.Added = parseStamp(added)
	r.Assertion.Retracted = parseStamp(retracted)
	r.Assertion.Expected, err = amount(ctx, tx, units)
	if err != nil {
		return AssertionRow{}, err
	}
	actual, err := balance(ctx, tx, r.Assertion.Account, r.Assertion.Date)
	if err != nil {
		return AssertionRow{}, err
	}
	r.Actual, err = amount(ctx, tx, actual)
	if err != nil {
		return AssertionRow{}, err
	}
	r.Difference, err = amount(ctx, tx, actual-units)
	if err != nil {
		return AssertionRow{}, err
	}
	r.Status = StatusHolds
	if actual != units {
		r.Status = StatusFails
	}
	return r, nil
}

// AssertBalance records an expected balance and returns its current evaluation.
func (s *Store) AssertBalance(ctx context.Context, in AssertionInput) (AssertionRow, error) {
	var out AssertionRow
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if in.UserID == "" {
			return ErrUnreachable
		}
		units, ok := ParseAmount(in.Amount, 2)
		if !accountPrefix(in.Account) || !ValidDate(in.Date) || !ok {
			return ErrInvalid
		}
		yes, err := locked(ctx, tx, in.Date)
		if err != nil {
			return err
		}
		if yes {
			return ErrLocked
		}
		id, err := s.mint(ctx, tx, "assertions", AssertionIDPrefix)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO assertions(id,account,date,amount,commodity,added,added_user_id) VALUES(?,?,?,?,?,?,?)`, id, in.Account, in.Date, units, "USD", stampText(stamp), in.UserID)
		if err != nil {
			return err
		}
		out, err = assertionRow(ctx, tx, id)
		return err
	})
	if err != nil {
		return AssertionRow{}, err
	}
	return out, nil
}

func noteRecord(ctx context.Context, tx *sql.Tx, id string) (Note, error) {
	var n Note
	var a, r string
	err := tx.QueryRowContext(ctx, `SELECT id,account,date,text,added,added_user_id,retracted,retracted_user_id FROM notes WHERE id=?`, id).Scan(&n.ID, &n.Account, &n.Date, &n.Text, &a, &n.AddedUserID, &r, &n.RetractedUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, err
	}
	n.Added = parseStamp(a)
	n.Retracted = parseStamp(r)
	return n, nil
}
func documentRecord(ctx context.Context, tx *sql.Tx, id string) (Document, error) {
	var d Document
	var a, r string
	err := tx.QueryRowContext(ctx, `SELECT id,account,txn,date,path,artifact,added,added_user_id,retracted,retracted_user_id FROM documents WHERE id=?`, id).Scan(&d.ID, &d.Account, &d.Transaction, &d.Date, &d.Path, &d.Artifact, &a, &d.AddedUserID, &r, &d.RetractedUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	d.Added = parseStamp(a)
	d.Retracted = parseStamp(r)
	return d, nil
}
func accountDate(ctx context.Context, tx *sql.Tx, name, date string) error {
	a, err := getAccount(ctx, tx, name)
	if err != nil {
		return err
	}
	if date < a.Open || (a.Close != "" && date > a.Close) {
		return ErrInvalid
	}
	return nil
}

// AddNote records text on a declared account.
func (s *Store) AddNote(ctx context.Context, in NoteInput) (Note, error) {
	var out Note
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if in.UserID == "" {
			return ErrUnreachable
		}
		text := strings.TrimSpace(in.Text)
		if !ValidAccountName(in.Account) || !ValidDate(in.Date) || text == "" || utf8.RuneCountInString(text) > MaxNote {
			return ErrInvalid
		}
		if err := accountDate(ctx, tx, in.Account, in.Date); err != nil {
			return err
		}
		yes, err := locked(ctx, tx, in.Date)
		if err != nil {
			return err
		}
		if yes {
			return ErrLocked
		}
		id, err := s.mint(ctx, tx, "notes", NoteIDPrefix)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO notes(id,account,date,text,added,added_user_id) VALUES(?,?,?,?,?,?)`, id, in.Account, in.Date, text, stampText(stamp), in.UserID)
		if err != nil {
			return err
		}
		out, err = noteRecord(ctx, tx, id)
		return err
	})
	if err != nil {
		return Note{}, err
	}
	return out, nil
}
func checkDocument(ctx context.Context, tx *sql.Tx, in DocumentInput) error {
	if in.UserID == "" {
		return ErrUnreachable
	}
	if !ValidAccountName(in.Account) || !ValidDate(in.Date) || in.Path == "" {
		return ErrInvalid
	}
	a, err := getAccount(ctx, tx, in.Account)
	if err != nil {
		return err
	}
	if in.Transaction != "" {
		if !ValidTransactionID(in.Transaction) {
			return ErrNotFound
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM transactions WHERE id=?)`, in.Transaction).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	if in.Date < a.Open || (a.Close != "" && in.Date > a.Close) {
		return ErrInvalid
	}
	yes, err := locked(ctx, tx, in.Date)
	if err != nil {
		return err
	}
	if yes {
		return ErrLocked
	}
	return nil
}

// CheckDocument checks document input without recording a reference.
func (s *Store) CheckDocument(ctx context.Context, in DocumentInput) error {
	return s.do(ctx, false, func(tx *sql.Tx, _ time.Time) error { return checkDocument(ctx, tx, in) })
}

// AddDocument records a reference to a verified artifact.
func (s *Store) AddDocument(ctx context.Context, in DocumentInput) (Document, error) {
	var out Document
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if in.Artifact == "" {
			return ErrUnreachable
		}
		if err := checkDocument(ctx, tx, in); err != nil {
			return err
		}
		id, err := s.mint(ctx, tx, "documents", DocumentIDPrefix)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO documents(id,account,txn,date,path,artifact,added,added_user_id) VALUES(?,?,?,?,?,?,?,?)`, id, in.Account, in.Transaction, in.Date, in.Path, in.Artifact, stampText(stamp), in.UserID)
		if err != nil {
			return err
		}
		out, err = documentRecord(ctx, tx, id)
		return err
	})
	if err != nil {
		return Document{}, err
	}
	return out, nil
}
func retract(ctx context.Context, tx *sql.Tx, table, id, user string, stamp time.Time) (bool, error) {
	var query, update string
	switch table {
	case "assertions":
		query = `SELECT retracted FROM assertions WHERE id=?`
		update = `UPDATE assertions SET retracted=?,retracted_user_id=? WHERE id=?`
	case "notes":
		query = `SELECT retracted FROM notes WHERE id=?`
		update = `UPDATE notes SET retracted=?,retracted_user_id=? WHERE id=?`
	case "documents":
		query = `SELECT retracted FROM documents WHERE id=?`
		update = `UPDATE documents SET retracted=?,retracted_user_id=? WHERE id=?`
	default:
		return false, ErrUnreachable
	}
	var old string
	err := tx.QueryRowContext(ctx, query, id).Scan(&old)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if old != "" {
		return false, nil
	}
	_, err = tx.ExecContext(ctx, update, stampText(stamp), user, id)
	return err == nil, err
}

// RetractAssertion marks an assertion retracted once.
func (s *Store) RetractAssertion(ctx context.Context, r Retraction) (AssertionRow, bool, error) {
	var out AssertionRow
	var changed bool
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if r.UserID == "" {
			return ErrUnreachable
		}
		if !ValidAssertionID(r.ID) {
			return ErrNotFound
		}
		var err error
		changed, err = retract(ctx, tx, "assertions", r.ID, r.UserID, stamp)
		if err != nil {
			return err
		}
		out, err = assertionRow(ctx, tx, r.ID)
		return err
	})
	if err != nil {
		return AssertionRow{}, false, err
	}
	return out, changed, nil
}

// RetractNote marks a note retracted once.
func (s *Store) RetractNote(ctx context.Context, r Retraction) (Note, bool, error) {
	var out Note
	var changed bool
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if r.UserID == "" {
			return ErrUnreachable
		}
		if !ValidNoteID(r.ID) {
			return ErrNotFound
		}
		var err error
		changed, err = retract(ctx, tx, "notes", r.ID, r.UserID, stamp)
		if err != nil {
			return err
		}
		out, err = noteRecord(ctx, tx, r.ID)
		return err
	})
	if err != nil {
		return Note{}, false, err
	}
	return out, changed, nil
}

// RetractDocument marks a document retracted once.
func (s *Store) RetractDocument(ctx context.Context, r Retraction) (Document, bool, error) {
	var out Document
	var changed bool
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if r.UserID == "" {
			return ErrUnreachable
		}
		if !ValidDocumentID(r.ID) {
			return ErrNotFound
		}
		var err error
		changed, err = retract(ctx, tx, "documents", r.ID, r.UserID, stamp)
		if err != nil {
			return err
		}
		out, err = documentRecord(ctx, tx, r.ID)
		return err
	})
	if err != nil {
		return Document{}, false, err
	}
	return out, changed, nil
}
func recordIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) (out []string, resultErr error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func accountRecords(ctx context.Context, tx *sql.Tx, name string) ([]AssertionRow, []Note, []Document, error) {
	assertions := []AssertionRow{}
	notes := []Note{}
	docs := []Document{}
	ids, err := recordIDs(ctx, tx, `SELECT id FROM assertions WHERE account=? ORDER BY date,id`, name)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, id := range ids {
		r, e := assertionRow(ctx, tx, id)
		if e != nil {
			return nil, nil, nil, e
		}
		assertions = append(assertions, r)
	}
	ids, err = recordIDs(ctx, tx, `SELECT id FROM notes WHERE account=? ORDER BY date,id`, name)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, id := range ids {
		r, e := noteRecord(ctx, tx, id)
		if e != nil {
			return nil, nil, nil, e
		}
		notes = append(notes, r)
	}
	ids, err = recordIDs(ctx, tx, `SELECT id FROM documents WHERE account=? ORDER BY date,id`, name)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, id := range ids {
		r, e := documentRecord(ctx, tx, id)
		if e != nil {
			return nil, nil, nil, e
		}
		docs = append(docs, r)
	}
	return assertions, notes, docs, nil
}

// ListFailing returns a window of current unretracted assertion failures.
func (s *Store) ListFailing(ctx context.Context, date, account, id string, n int) ([]AssertionRow, error) {
	out := []AssertionRow{}
	err := s.do(ctx, false, func(tx *sql.Tx, stamp time.Time) error {
		if n < 1 {
			return ErrUnreachable
		}
		query := FailingFirst
		args := []any{stamp.Format(DateLayout), n}
		if id != "" {
			query = FailingAfter
			args = []any{stamp.Format(DateLayout), date, account, id, n}
		}
		ids, err := recordIDs(ctx, tx, query, args...)
		if err != nil {
			return err
		}
		for _, id := range ids {
			r, e := assertionRow(ctx, tx, id)
			if e != nil {
				return e
			}
			out = append(out, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func statusFailing(ctx context.Context, tx *sql.Tx, today string) (int, error) {
	ids, err := recordIDs(ctx, tx, FailingFirst, today, int(^uint(0)>>1))
	return len(ids), err
}
