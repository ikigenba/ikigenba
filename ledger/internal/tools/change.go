package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
)

func (h handlers) setBook(ctx context.Context, u identity.Caller, in SetBookArgs) (out BookChange, err error) {
	s, ch, e := h.Store.SetBook(ctx, store.BookEdit{LockDate: in.LockDate, FiscalYearStart: in.FiscalYearStart, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.BookChanged(ctx, h.Telemetry, s.Book.LockDate, s.Book.FiscalYearStart)
	}
	return BookChange{bookAnswer(s), ch}, nil
}
func (h handlers) openAccount(ctx context.Context, u identity.Caller, in AccountDateArgs) (out AccountChange, err error) {
	a, ch, e := h.Store.OpenAccount(ctx, store.AccountInput{Name: in.Name, Date: in.Date, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.AccountOpened(ctx, h.Telemetry, a.Name, a.Open)
	}
	return AccountChange{accountAnswer(a), ch}, nil
}
func (h handlers) closeAccount(ctx context.Context, u identity.Caller, in AccountDateArgs) (out AccountChange, err error) {
	a, ch, e := h.Store.CloseAccount(ctx, store.AccountInput{Name: in.Name, Date: in.Date, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.AccountClosed(ctx, h.Telemetry, a.Name, a.Close)
	}
	return AccountChange{accountAnswer(a), ch}, nil
}
func (h handlers) post(ctx context.Context, u identity.Caller, in PostArgs) (out TransactionChange, err error) {
	m, e := metadata(in.Metadata)
	if e != nil {
		return out, e
	}
	ps := make([]store.PostingInput, 0, len(in.Postings))
	for _, p := range in.Postings {
		ps = append(ps, store.PostingInput{Account: p.Account, Amount: p.Amount})
	}
	t, ch, e := h.Store.Post(ctx, store.PostInput{Date: in.Date, Postings: ps, Flag: value(in.Flag), Payee: value(in.Payee), Narration: value(in.Narration), Tags: in.Tags, Links: in.Links, Metadata: m, Key: value(in.Key), UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.TransactionPosted(ctx, h.Telemetry, h.Events, t.ID, t.Date, t.Annotation.Flag, t.Key, "", trailPostings(t))
	}
	return TransactionChange{transactionAnswer(t), ch}, nil
}
func (h handlers) annotate(ctx context.Context, u identity.Caller, in AnnotateArgs) (out TransactionChange, err error) {
	m, e := metadataPointer(in.Metadata)
	if e != nil {
		return out, e
	}
	t, ch, e := h.Store.Annotate(ctx, store.AnnotationEdit{Transaction: in.Transaction, Flag: in.Flag, Payee: in.Payee, Narration: in.Narration, Tags: in.Tags, Links: in.Links, Metadata: m, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.TransactionAnnotated(ctx, h.Telemetry, t.ID, t.Annotation.Version)
	}
	return TransactionChange{transactionAnswer(t), ch}, nil
}
func (h handlers) reverse(ctx context.Context, u identity.Caller, in ReverseArgs) (out TransactionChange, err error) {
	m, e := metadataPointer(in.Metadata)
	if e != nil {
		return out, e
	}
	t, ch, e := h.Store.Reverse(ctx, store.ReversalInput{Transaction: in.Transaction, Date: value(in.Date), Payee: in.Payee, Narration: in.Narration, Tags: in.Tags, Links: in.Links, Metadata: m, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.ReversalPosted(ctx, h.Events, t.ID, t.Date, t.Annotation.Flag, t.Key, t.Reverses, trailPostings(t))
		trail.TransactionReversed(ctx, h.Telemetry, t.ID, t.Reverses, t.Date)
	}
	return TransactionChange{transactionAnswer(t), ch}, nil
}
func (h handlers) assertBalance(ctx context.Context, u identity.Caller, in AssertBalanceArgs) (out Assertion, err error) {
	r, e := h.Store.AssertBalance(ctx, store.AssertionInput{Account: in.Account, Date: in.Date, Amount: in.Amount, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	a := r.Assertion
	trail.AssertionAdded(ctx, h.Telemetry, a.ID, a.Account, a.Date, a.Expected.String(), a.Expected.Commodity)
	return assertionAnswer(r), nil
}
func (h handlers) retractAssertion(ctx context.Context, u identity.Caller, in AssertionArgs) (out AssertionChange, err error) {
	r, ch, e := h.Store.RetractAssertion(ctx, store.Retraction{ID: in.Assertion, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.AssertionRetracted(ctx, h.Telemetry, r.Assertion.ID)
	}
	return AssertionChange{assertionAnswer(r), ch}, nil
}
func (h handlers) addNote(ctx context.Context, u identity.Caller, in AddNoteArgs) (out Note, err error) {
	n, e := h.Store.AddNote(ctx, store.NoteInput{Account: in.Account, Date: in.Date, Text: in.Text, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	trail.NoteAdded(ctx, h.Telemetry, n.ID, n.Account, n.Date)
	return noteAnswer(n), nil
}
func (h handlers) retractNote(ctx context.Context, u identity.Caller, in NoteArgs) (out NoteChange, err error) {
	n, ch, e := h.Store.RetractNote(ctx, store.Retraction{ID: in.Note, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.NoteRetracted(ctx, h.Telemetry, n.ID)
	}
	return NoteChange{noteAnswer(n), ch}, nil
}
func (h handlers) addDocument(ctx context.Context, u identity.Caller, in AddDocumentArgs) (out Document, err error) {
	if in.Path == "" {
		return out, invalid()
	}
	d := store.DocumentInput{Account: in.Account, Transaction: value(in.Transaction), Date: in.Date, Path: in.Path, UserID: u.UserID}
	if e := h.Store.CheckDocument(ctx, d); e != nil {
		return out, storeError(e)
	}
	if h.Artifacts == nil {
		return out, artifactFailure()
	}
	if _, e := h.Artifacts.Head(ctx, in.Path); e != nil {
		switch {
		case errors.Is(e, artifacts.ErrBadPath):
			return out, invalid()
		case errors.Is(e, artifacts.ErrNotFound):
			return out, refusal(RefusalNotFound, RefuseNoArtifact)
		default:
			return out, artifactFailure()
		}
	}
	md, e := h.Artifacts.Show(ctx, in.Path)
	if e != nil || md.ID == "" {
		return out, artifactFailure()
	}
	d.Artifact = md.ID
	r, e := h.Store.AddDocument(ctx, d)
	if e != nil {
		return out, storeError(e)
	}
	trail.DocumentAdded(ctx, h.Telemetry, r.ID, r.Account, r.Date, r.Transaction, r.Artifact)
	return documentAnswer(r), nil
}
func (h handlers) retractDocument(ctx context.Context, u identity.Caller, in DocumentArgs) (out DocumentChange, err error) {
	d, ch, e := h.Store.RetractDocument(ctx, store.Retraction{ID: in.Document, UserID: u.UserID})
	if e != nil {
		return out, storeError(e)
	}
	if ch {
		trail.DocumentRetracted(ctx, h.Telemetry, d.ID)
	}
	return DocumentChange{documentAnswer(d), ch}, nil
}
