package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
)

func refusal(word, text string) error { return errors.New(word + ": " + text) }
func invalid() error                  { return refusal(RefusalInvalid, RefuseInvalid) }
func artifactFailure() error          { return refusal(RefusalUnavailable, RefuseArtifacts) }
func storeError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return refusal(RefusalNotFound, RefuseNotFound)
	case errors.Is(err, store.ErrInvalid):
		return invalid()
	case errors.Is(err, store.ErrUnbalanced):
		return refusal(RefusalInvalid, RefuseUnbalanced)
	case errors.Is(err, store.ErrLocked):
		return refusal(RefusalInvalid, RefuseLocked)
	default:
		return refusal(RefusalUnavailable, store.Unreachable)
	}
}
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
func metadata(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, invalid()
	}
	m := map[string]string{}
	for d.More() {
		k, e := d.Token()
		if e != nil {
			return nil, invalid()
		}
		key, ok := k.(string)
		if !ok {
			return nil, invalid()
		}
		if _, exists := m[key]; exists {
			return nil, invalid()
		}
		var v any
		if e = d.Decode(&v); e != nil {
			return nil, invalid()
		}
		s, ok := v.(string)
		if !ok {
			return nil, invalid()
		}
		m[key] = s
	}
	if _, e = d.Token(); e != nil {
		return nil, invalid()
	}
	return m, nil
}
func metadataPointer(raw json.RawMessage) (*map[string]string, error) {
	m, e := metadata(raw)
	if e != nil || len(raw) == 0 {
		return nil, e
	}
	return &m, nil
}
func metadataAnswer(m map[string]string) json.RawMessage {
	if m == nil {
		return json.RawMessage(`{}`)
	}
	b, _ := json.Marshal(m)
	return b
}
func bookAnswer(s store.Status) Book {
	b := s.Book
	return Book{b.Currency, b.Precision, b.LockDate, b.FiscalYearStart, stamp(b.Updated), b.UpdatedUserID, s.OpenAccounts, s.Transactions, s.FailingAssertions}
}
func accountAnswer(a store.Account) Account {
	return Account{a.Name, a.Open, a.Close, stamp(a.Opened), a.OpenedUserID, stamp(a.Closed), a.ClosedUserID}
}
func postingAnswer(p store.Posting) Posting {
	return Posting{p.Account, p.Amount.String(), p.Amount.Commodity}
}
func transactionAnswer(t store.Transaction) Transaction {
	a := t.Annotation
	ps := make([]Posting, 0, len(t.Postings))
	for _, p := range t.Postings {
		ps = append(ps, postingAnswer(p))
	}
	return Transaction{t.ID, t.Seq, t.Date, t.Key, a.Flag, a.Payee, a.Narration, a.Tags, a.Links, metadataAnswer(a.Metadata), a.Version, t.Reverses, t.ReversedBy, stamp(t.Posted), t.PostedUserID, stamp(a.Written), a.WrittenUserID, ps}
}
func versionAnswer(a store.Annotation) Version {
	return Version{a.Version, a.Flag, a.Payee, a.Narration, a.Tags, a.Links, metadataAnswer(a.Metadata), stamp(a.Written), a.WrittenUserID}
}
func assertionAnswer(r store.AssertionRow) Assertion {
	a := r.Assertion
	return Assertion{a.ID, a.Account, a.Date, a.Expected.String(), a.Expected.Commodity, r.Status, r.Actual.String(), r.Difference.String(), stamp(a.Added), a.AddedUserID, stamp(a.Retracted), a.RetractedUserID}
}
func noteAnswer(n store.Note) Note {
	return Note{n.ID, n.Account, n.Date, n.Text, stamp(n.Added), n.AddedUserID, stamp(n.Retracted), n.RetractedUserID}
}
func documentAnswer(d store.Document) Document {
	return Document{d.ID, d.Account, d.Transaction, d.Date, d.Path, d.Artifact, stamp(d.Added), d.AddedUserID, stamp(d.Retracted), d.RetractedUserID}
}
func linesAnswer(ls []store.Line) []Line {
	out := make([]Line, 0, len(ls))
	for _, l := range ls {
		out = append(out, Line{l.Account, l.Amount.String(), l.Amount.Commodity})
	}
	return out
}
func rowAnswer(r store.RegisterRow) RegisterRow {
	return RegisterRow{r.Transaction, r.Date, r.Position, r.Account, r.Amount.String(), r.Amount.Commodity, r.Balance.String(), r.Payee, r.Narration}
}
func trailPostings(t store.Transaction) []trail.Posting {
	out := make([]trail.Posting, 0, len(t.Postings))
	for _, p := range t.Postings {
		out = append(out, trail.Posting{Account: p.Account, Amount: p.Amount.String(), Commodity: p.Amount.Commodity})
	}
	return out
}
func window(tool string, page *string, limit *int, into ...any) (int, error) {
	z, e := paging.Window(limit)
	if e != nil {
		return 0, e
	}
	if page != nil && *page != "" {
		if e = (paging.Token{Tool: tool}).Decode(*page, into...); e != nil {
			return 0, errors.New(paging.BadPage)
		}
	}
	return z, nil
}
func next(tool string, keys ...any) *string {
	s := (paging.Token{Tool: tool}).Encode(keys...)
	return &s
}
