// Package trail queues the ledger's change records and bus notices.
package trail

import (
	"context"
	"encoding/json"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// EventTransactionPosted names the transaction.posted record.
const EventTransactionPosted = "transaction.posted"

// EventAccountOpened names the account.opened record.
const EventAccountOpened = "account.opened"

// EventAccountClosed names the account.closed record.
const EventAccountClosed = "account.closed"

// EventTransactionAnnotated names the transaction.annotated record.
const EventTransactionAnnotated = "transaction.annotated"

// EventTransactionReversed names the transaction.reversed record.
const EventTransactionReversed = "transaction.reversed"

// EventAssertionAdded names the assertion.added record.
const EventAssertionAdded = "assertion.added"

// EventAssertionRetracted names the assertion.retracted record.
const EventAssertionRetracted = "assertion.retracted"

// EventNoteAdded names the note.added record.
const EventNoteAdded = "note.added"

// EventNoteRetracted names the note.retracted record.
const EventNoteRetracted = "note.retracted"

// EventDocumentAdded names the document.added record.
const EventDocumentAdded = "document.added"

// EventDocumentRetracted names the document.retracted record.
const EventDocumentRetracted = "document.retracted"

// EventBookChanged names the book.changed record.
const EventBookChanged = "book.changed"

// Posting is the public content of one posting in a bus notice.
type Posting struct{ Account, Amount, Commodity string }

// Emits returns independent bus declarations.
func Emits() []events.Emission {
	return []events.Emission{{Event: EventTransactionPosted, Attrs: []string{"transaction", "date", "flag", "key", "reverses", "postings"}}}
}
func postedAttrs(transaction, date, flag, key, reverses string, postings []Posting) telemetry.Attrs {
	type wirePosting struct {
		Account   string `json:"account"`
		Amount    string `json:"amount"`
		Commodity string `json:"commodity"`
	}
	rows := make([]wirePosting, 0, len(postings))
	for _, p := range postings {
		rows = append(rows, wirePosting(p))
	}
	data, _ := json.Marshal(rows)
	return telemetry.Attrs{"transaction": transaction, "date": date, "flag": flag, "key": key, "reverses": reverses, "postings": string(data)}
}

// TransactionPosted queues one trail record and one bus notice.
func TransactionPosted(ctx context.Context, w *telemetry.Writer, em *events.Emitter, transaction, date, flag, key, reverses string, postings []Posting) {
	attrs := postedAttrs(transaction, date, flag, key, reverses, postings)
	if w != nil {
		w.Emit(ctx, EventTransactionPosted, attrs)
	}
	if em != nil {
		em.Emit(ctx, EventTransactionPosted, events.Attrs(attrs))
	}
}

// ReversalPosted queues the bus notice for a reversal.
func ReversalPosted(ctx context.Context, em *events.Emitter, transaction, date, flag, key, reverses string, postings []Posting) {
	if em != nil {
		em.Emit(ctx, EventTransactionPosted, events.Attrs(postedAttrs(transaction, date, flag, key, reverses, postings)))
	}
}

// AccountOpened queues the change record.
func AccountOpened(ctx context.Context, w *telemetry.Writer, account, date string) {
	if w != nil {
		w.Emit(ctx, EventAccountOpened, telemetry.Attrs{"account": account, "date": date})
	}
}

// AccountClosed queues the change record.
func AccountClosed(ctx context.Context, w *telemetry.Writer, account, date string) {
	if w != nil {
		w.Emit(ctx, EventAccountClosed, telemetry.Attrs{"account": account, "date": date})
	}
}

// TransactionAnnotated queues the change record.
func TransactionAnnotated(ctx context.Context, w *telemetry.Writer, transaction string, version int64) {
	if w != nil {
		w.Emit(ctx, EventTransactionAnnotated, telemetry.Attrs{"transaction": transaction, "version": version})
	}
}

// TransactionReversed queues the change record.
func TransactionReversed(ctx context.Context, w *telemetry.Writer, transaction, reverses, date string) {
	if w != nil {
		w.Emit(ctx, EventTransactionReversed, telemetry.Attrs{"transaction": transaction, "reverses": reverses, "date": date})
	}
}

// AssertionAdded queues the change record.
func AssertionAdded(ctx context.Context, w *telemetry.Writer, assertion, account, date, amount, commodity string) {
	if w != nil {
		w.Emit(ctx, EventAssertionAdded, telemetry.Attrs{"assertion": assertion, "account": account, "date": date, "amount": amount, "commodity": commodity})
	}
}

// AssertionRetracted queues the change record.
func AssertionRetracted(ctx context.Context, w *telemetry.Writer, assertion string) {
	if w != nil {
		w.Emit(ctx, EventAssertionRetracted, telemetry.Attrs{"assertion": assertion})
	}
}

// NoteAdded queues the change record.
func NoteAdded(ctx context.Context, w *telemetry.Writer, note, account, date string) {
	if w != nil {
		w.Emit(ctx, EventNoteAdded, telemetry.Attrs{"note": note, "account": account, "date": date})
	}
}

// NoteRetracted queues the change record.
func NoteRetracted(ctx context.Context, w *telemetry.Writer, note string) {
	if w != nil {
		w.Emit(ctx, EventNoteRetracted, telemetry.Attrs{"note": note})
	}
}

// DocumentAdded queues the change record.
func DocumentAdded(ctx context.Context, w *telemetry.Writer, document, account, date, transaction, artifact string) {
	if w != nil {
		w.Emit(ctx, EventDocumentAdded, telemetry.Attrs{"document": document, "account": account, "date": date, "transaction": transaction, "artifact": artifact})
	}
}

// DocumentRetracted queues the change record.
func DocumentRetracted(ctx context.Context, w *telemetry.Writer, document string) {
	if w != nil {
		w.Emit(ctx, EventDocumentRetracted, telemetry.Attrs{"document": document})
	}
}

// BookChanged queues the change record.
func BookChanged(ctx context.Context, w *telemetry.Writer, lockDate, fiscalYearStart string) {
	if w != nil {
		w.Emit(ctx, EventBookChanged, telemetry.Attrs{"lock_date": lockDate, "fiscal_year_start": fiscalYearStart})
	}
}
