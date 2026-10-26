package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"
)

func readAnnotation(ctx context.Context, tx *sql.Tx, seq, version int64) (Annotation, error) {
	var a Annotation
	var tags, links, metadata, written string
	err := tx.QueryRowContext(ctx, `SELECT version,flag,payee,narration,tags,links,metadata,written,written_user_id FROM annotations WHERE txn=? AND version=?`, seq, version).Scan(&a.Version, &a.Flag, &a.Payee, &a.Narration, &tags, &links, &metadata, &written, &a.WrittenUserID)
	if err != nil {
		return Annotation{}, err
	}
	if err = json.Unmarshal([]byte(tags), &a.Tags); err != nil {
		return Annotation{}, err
	}
	if err = json.Unmarshal([]byte(links), &a.Links); err != nil {
		return Annotation{}, err
	}
	if err = json.Unmarshal([]byte(metadata), &a.Metadata); err != nil {
		return Annotation{}, err
	}
	a.Written = parseStamp(written)
	return a, nil
}

func getTransaction(ctx context.Context, tx *sql.Tx, id string) (Transaction, error) {
	var t Transaction
	var posted string
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT id,seq,date,key,reverses,reversed_by,version,posted,posted_user_id FROM transactions WHERE id=?`, id).Scan(&t.ID, &t.Seq, &t.Date, &t.Key, &t.Reverses, &t.ReversedBy, &version, &posted, &t.PostedUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return Transaction{}, ErrNotFound
	}
	if err != nil {
		return Transaction{}, err
	}
	t.Posted = parseStamp(posted)
	b, err := book(ctx, tx)
	if err != nil {
		return Transaction{}, err
	}
	t.Postings = make([]Posting, 0)
	rows, err := tx.QueryContext(ctx, `SELECT position,account,amount,commodity FROM postings WHERE txn=? ORDER BY position`, t.Seq)
	if err != nil {
		return Transaction{}, err
	}
	for rows.Next() {
		var p Posting
		p.Amount.Precision = b.Precision
		if err = rows.Scan(&p.Position, &p.Account, &p.Amount.Units, &p.Amount.Commodity); err != nil {
			break
		}
		t.Postings = append(t.Postings, p)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return Transaction{}, err
	}
	if closeErr != nil {
		return Transaction{}, closeErr
	}
	t.Annotation, err = readAnnotation(ctx, tx, t.Seq, version)
	return t, err
}

// GetTransaction reads a transaction with its current annotation.
func (s *Store) GetTransaction(ctx context.Context, id string) (Transaction, error) {
	var out Transaction
	err := s.do(ctx, false, func(tx *sql.Tx, _ time.Time) error { var err error; out, err = getTransaction(ctx, tx, id); return err })
	if err != nil {
		return Transaction{}, err
	}
	return out, nil
}

func writeAnnotation(ctx context.Context, tx *sql.Tx, seq int64, a Annotation) error {
	tags, err := json.Marshal(a.Tags)
	if err != nil {
		return err
	}
	links, err := json.Marshal(a.Links)
	if err != nil {
		return err
	}
	md, err := json.Marshal(a.Metadata)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO annotations (txn,version,flag,payee,narration,tags,links,metadata,written,written_user_id) VALUES (?,?,?,?,?,?,?,?,?,?)`, seq, a.Version, a.Flag, a.Payee, a.Narration, string(tags), string(links), string(md), stampText(a.Written), a.WrittenUserID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tags WHERE txn=?`, seq); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM links WHERE txn=?`, seq); err != nil {
		return err
	}
	for _, tag := range a.Tags {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tags (tag,txn) VALUES (?,?)`, tag, seq); err != nil {
			return err
		}
	}
	for _, link := range a.Links {
		if _, err = tx.ExecContext(ctx, `INSERT INTO links (link,txn) VALUES (?,?)`, link, seq); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) insertTransaction(ctx context.Context, tx *sql.Tx, t *Transaction) error {
	var err error
	t.ID, err = s.mint(ctx, tx, "transactions", TransactionIDPrefix)
	if err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM transactions`).Scan(&t.Seq); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO transactions (seq,id,date,key,reverses,reversed_by,version,posted,posted_user_id) VALUES (?,?,?,?,?,?,?,?,?)`, t.Seq, t.ID, t.Date, t.Key, t.Reverses, t.ReversedBy, t.Annotation.Version, stampText(t.Posted), t.PostedUserID); err != nil {
		return err
	}
	for _, p := range t.Postings {
		if _, err = tx.ExecContext(ctx, `INSERT INTO postings (txn,position,account,amount,commodity,date) VALUES (?,?,?,?,?,?)`, t.Seq, p.Position, p.Account, p.Amount.Units, p.Amount.Commodity, t.Date); err != nil {
			return err
		}
	}
	return writeAnnotation(ctx, tx, t.Seq, t.Annotation)
}

func checkPostingDates(ctx context.Context, tx *sql.Tx, ps []Posting, date string) error {
	accounts := make([]Account, 0, len(ps))
	for _, p := range ps {
		a, err := getAccount(ctx, tx, p.Account)
		if err != nil {
			return err
		}
		accounts = append(accounts, a)
	}
	for _, a := range accounts {
		if date < a.Open || a.Close != "" && date > a.Close {
			return ErrInvalid
		}
	}
	yes, err := locked(ctx, tx, date)
	if err != nil {
		return err
	}
	if yes {
		return ErrLocked
	}
	return nil
}

// Post records a balanced transaction or returns its keyed predecessor.
func (s *Store) Post(ctx context.Context, in PostInput) (Transaction, bool, error) {
	var out Transaction
	changed := false
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if in.UserID == "" {
			return ErrUnreachable
		}
		if in.Key != "" {
			var id string
			err := tx.QueryRowContext(ctx, `SELECT id FROM transactions WHERE key=?`, in.Key).Scan(&id)
			if err == nil {
				out, err = getTransaction(ctx, tx, id)
				return err
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		a, valid := normalizeWords(in.Payee, in.Narration, in.Tags, in.Links, in.Metadata)
		if in.Key != "" && !ValidKey(in.Key) || !ValidDate(in.Date) || in.Flag != "" && !ValidFlag(in.Flag) || !valid || len(in.Postings) > 100 {
			return ErrInvalid
		}
		ps := make([]Posting, 0, len(in.Postings))
		var sum int64
		missing := -1
		count := 0
		for i, p := range in.Postings {
			if !ValidAccountName(p.Account) {
				return ErrInvalid
			}
			var units int64
			if p.Amount == nil {
				missing = i
				count++
			} else {
				var ok bool
				units, ok = ParseAmount(*p.Amount, 2)
				if !ok {
					return ErrInvalid
				}
				sum += units
			}
			ps = append(ps, Posting{Position: int64(i + 1), Account: p.Account, Amount: Amount{Units: units}})
		}
		if len(ps) < 2 || count > 1 || count == 0 && sum != 0 {
			return ErrUnbalanced
		}
		if count == 1 {
			if sum > 999999999999999 || sum < -999999999999999 {
				return ErrInvalid
			}
			ps[missing].Amount.Units = -sum
		}
		if err := checkPostingDates(ctx, tx, ps, in.Date); err != nil {
			return err
		}
		for i := range ps {
			v, err := amount(ctx, tx, ps[i].Amount.Units)
			if err != nil {
				return err
			}
			ps[i].Amount = v
		}
		a.Version = 1
		a.Flag = in.Flag
		if a.Flag == "" {
			a.Flag = FlagPending
		}
		a.Written = stamp
		a.WrittenUserID = in.UserID
		out = Transaction{Date: in.Date, Key: in.Key, Posted: stamp, PostedUserID: in.UserID, Postings: ps, Annotation: a}
		if err := s.insertTransaction(ctx, tx, &out); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return Transaction{}, false, err
	}
	return out, changed, nil
}

func editedAnnotation(base Annotation, e AnnotationEdit) (Annotation, bool) {
	if e.Payee != nil {
		base.Payee = *e.Payee
	}
	if e.Narration != nil {
		base.Narration = *e.Narration
	}
	if e.Tags != nil {
		base.Tags = *e.Tags
	}
	if e.Links != nil {
		base.Links = *e.Links
	}
	if e.Metadata != nil {
		base.Metadata = *e.Metadata
	}
	a, ok := normalizeWords(base.Payee, base.Narration, base.Tags, base.Links, base.Metadata)
	a.Flag = base.Flag
	if e.Flag != nil {
		a.Flag = *e.Flag
	}
	return a, ok && (e.Flag == nil || ValidFlag(*e.Flag))
}
func sameWords(a, b Annotation) bool {
	return a.Flag == b.Flag && a.Payee == b.Payee && a.Narration == b.Narration && slices.Equal(a.Tags, b.Tags) && slices.Equal(a.Links, b.Links) && maps.Equal(a.Metadata, b.Metadata)
}

// Annotate replaces supplied words and retains prior versions.
func (s *Store) Annotate(ctx context.Context, e AnnotationEdit) (Transaction, bool, error) {
	var out Transaction
	changed := false
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if e.UserID == "" {
			return ErrUnreachable
		}
		if _, ok := editedAnnotation(Annotation{}, e); !ok {
			return ErrInvalid
		}
		t, err := getTransaction(ctx, tx, e.Transaction)
		if err != nil {
			return err
		}
		a, ok := editedAnnotation(t.Annotation, e)
		if !ok {
			return ErrInvalid
		}
		out = t
		if sameWords(a, t.Annotation) {
			return nil
		}
		a.Version = t.Annotation.Version + 1
		a.Written = stamp
		a.WrittenUserID = e.UserID
		if err = writeAnnotation(ctx, tx, t.Seq, a); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE transactions SET version=? WHERE seq=?`, a.Version, t.Seq); err != nil {
			return err
		}
		out.Annotation = a
		changed = true
		return nil
	})
	if err != nil {
		return Transaction{}, false, err
	}
	return out, changed, nil
}

// Reverse records the negation of a transaction once.
func (s *Store) Reverse(ctx context.Context, in ReversalInput) (Transaction, bool, error) {
	var out Transaction
	changed := false
	err := s.do(ctx, true, func(tx *sql.Tx, stamp time.Time) error {
		if in.UserID == "" {
			return ErrUnreachable
		}
		o, lookupErr := getTransaction(ctx, tx, in.Transaction)
		if lookupErr == nil && o.ReversedBy != "" {
			var err error
			out, err = getTransaction(ctx, tx, o.ReversedBy)
			return err
		}
		if lookupErr != nil && !errors.Is(lookupErr, ErrNotFound) {
			return lookupErr
		}
		e := AnnotationEdit{Payee: in.Payee, Narration: in.Narration, Tags: in.Tags, Links: in.Links, Metadata: in.Metadata}
		if _, ok := editedAnnotation(Annotation{}, e); !ok || in.Date != "" && !ValidDate(in.Date) {
			return ErrInvalid
		}
		if lookupErr != nil {
			return lookupErr
		}
		date := in.Date
		if date == "" {
			date = stamp.Format(DateLayout)
		}
		if date < o.Date {
			return ErrInvalid
		}
		if err := checkPostingDates(ctx, tx, o.Postings, date); err != nil {
			return err
		}
		a, _ := editedAnnotation(o.Annotation, e)
		a.Version = 1
		a.Flag = FlagCleared
		a.Written = stamp
		a.WrittenUserID = in.UserID
		ps := slices.Clone(o.Postings)
		for i := range ps {
			ps[i].Amount.Units = -ps[i].Amount.Units
		}
		out = Transaction{Date: date, Reverses: o.ID, Posted: stamp, PostedUserID: in.UserID, Postings: ps, Annotation: a}
		if err := s.insertTransaction(ctx, tx, &out); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE transactions SET reversed_by=? WHERE seq=?`, out.ID, o.Seq); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return Transaction{}, false, err
	}
	return out, changed, nil
}

// ListVersions returns annotation history in descending version order.
func (s *Store) ListVersions(ctx context.Context, id string, version int64, n int) ([]Annotation, error) {
	var out []Annotation
	err := s.do(ctx, false, func(tx *sql.Tx, _ time.Time) error {
		if n < 1 {
			return ErrUnreachable
		}
		t, err := getTransaction(ctx, tx, id)
		if err != nil {
			return err
		}
		query := VersionsFirst
		args := []any{t.Seq, n}
		if version > 0 {
			query = VersionsAfter
			args = []any{t.Seq, version, n}
		}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		var versions []int64
		for rows.Next() {
			var v int64
			if err = rows.Scan(&v); err != nil {
				break
			}
			versions = append(versions, v)
		}
		if err == nil {
			err = rows.Err()
		}
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		out = make([]Annotation, 0, len(versions))
		for _, v := range versions {
			a, err := readAnnotation(ctx, tx, t.Seq, v)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func validRange(from, to string) bool {
	return (from == "" || ValidDate(from)) && (to == "" || ValidDate(to)) && (from == "" || to == "" || from <= to)
}

// ListTransactions returns the next journal window.
func (s *Store) ListTransactions(ctx context.Context, f JournalFilter, date string, seq int64, n int) ([]Transaction, error) {
	var out []Transaction
	err := s.do(ctx, false, func(tx *sql.Tx, _ time.Time) error {
		if n < 1 {
			return ErrUnreachable
		}
		if !validRange(f.From, f.To) || f.Account != "" && !accountPrefix(f.Account) || f.Tag != "" && !ValidToken(f.Tag) || f.Link != "" && !ValidToken(f.Link) || f.Flag != "" && !ValidFlag(f.Flag) {
			return ErrInvalid
		}
		query := TransactionsFirst
		args := []any{f.From, f.To, f.Account, f.Tag, f.Link, f.Payee, f.Flag}
		if date != "" {
			query = TransactionsAfter
			args = append(args, date, seq)
		}
		args = append(args, n)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		var seqs []int64
		for rows.Next() {
			var v int64
			if err = rows.Scan(&v); err != nil {
				break
			}
			seqs = append(seqs, v)
		}
		if err == nil {
			err = rows.Err()
		}
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		out = make([]Transaction, 0, len(seqs))
		for _, v := range seqs {
			var id string
			if err = tx.QueryRowContext(ctx, `SELECT id FROM transactions WHERE seq=?`, v).Scan(&id); err != nil {
				return err
			}
			t, err := getTransaction(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListRegister returns a register window with historical running balances.
func (s *Store) ListRegister(ctx context.Context, account, from, to, date string, seq, position int64, n int) ([]RegisterRow, error) {
	var out []RegisterRow
	err := s.do(ctx, false, func(tx *sql.Tx, _ time.Time) error {
		if n < 1 {
			return ErrUnreachable
		}
		if !accountPrefix(account) || !validRange(from, to) {
			return ErrInvalid
		}
		query := RegisterFirst
		args := []any{account, from, to}
		if date != "" {
			query = RegisterAfter
			args = append(args, date, seq, position)
		}
		args = append(args, n)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		type key struct{ seq, pos int64 }
		var keys []key
		for rows.Next() {
			var k key
			if err = rows.Scan(&k.seq, &k.pos); err != nil {
				break
			}
			keys = append(keys, k)
		}
		if err == nil {
			err = rows.Err()
		}
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		out = make([]RegisterRow, 0, len(keys))
		for _, k := range keys {
			var id string
			if err = tx.QueryRowContext(ctx, `SELECT id FROM transactions WHERE seq=?`, k.seq).Scan(&id); err != nil {
				return err
			}
			t, err := getTransaction(ctx, tx, id)
			if err != nil {
				return err
			}
			p := t.Postings[k.pos-1]
			var units int64
			if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM postings WHERE (account=?1 OR (account>?1||':' AND account<?1||';')) AND (date,txn,position)<=(?2,?3,?4)`, account, t.Date, t.Seq, k.pos).Scan(&units); err != nil {
				return err
			}
			bal, err := amount(ctx, tx, units)
			if err != nil {
				return err
			}
			out = append(out, RegisterRow{Transaction: t.ID, Date: t.Date, Seq: t.Seq, Position: p.Position, Account: p.Account, Amount: p.Amount, Balance: bal, Payee: t.Annotation.Payee, Narration: t.Annotation.Narration})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
