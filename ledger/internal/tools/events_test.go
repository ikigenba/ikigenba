package tools_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
)

func mutations(h *harness) []telemetry.Event {
	out := []telemetry.Event{}
	for _, e := range h.trail.Events() {
		switch e.Name {
		case trail.EventAccountOpened, trail.EventAccountClosed, trail.EventTransactionPosted, trail.EventTransactionAnnotated, trail.EventTransactionReversed, trail.EventAssertionAdded, trail.EventAssertionRetracted, trail.EventNoteAdded, trail.EventNoteRetracted, trail.EventDocumentAdded, trail.EventDocumentRetracted, trail.EventBookChanged:
			out = append(out, e)
		}
	}
	return out
}
func postings(tx tools.Transaction) []trail.Posting {
	out := []trail.Posting{}
	for _, p := range tx.Postings {
		out = append(out, trail.Posting{Account: p.Account, Amount: p.Amount, Commodity: p.Commodity})
	}
	return out
}
func (h *harness) expectEvents(action func(), want func(context.Context, *telemetry.Writer, *events.Emitter)) {
	h.t.Helper()
	h.flush()
	beforeTrail, beforeBus := len(mutations(h)), len(h.bus.Events())
	action()
	h.flush()
	wc, ec := &telemetry.Capture{}, &events.Capture{}
	now := func() time.Time { return h.now }
	pause := func(context.Context, time.Duration) {}
	w := telemetry.New(telemetry.Config{Service: "ledger", Sink: wc, Now: now, Sleep: pause, Rand: randomness(100), Stderr: io.Discard})
	em := events.New(events.Config{Service: "ledger", Sink: ec, Telemetry: w, Emits: trail.Emits(), Now: now, Sleep: pause, Rand: randomness(101), Stderr: io.Discard})
	defer w.Shutdown(context.Background(), "test")
	defer em.Shutdown(context.Background())
	ctx := events.NewContext(identity.NewContext(h.t.Context(), h.caller), events.Cause{ID: "evt_0123456789abcdef", Depth: 3})
	if want != nil {
		want(ctx, w, em)
	}
	if err := em.Flush(h.t.Context()); err != nil {
		h.t.Fatal(err)
	}
	if err := w.Flush(h.t.Context()); err != nil {
		h.t.Fatal(err)
	}
	actualTrail, actualBus := mutations(h)[beforeTrail:], h.bus.Events()[beforeBus:]
	expectedTrail, expectedBus := wc.Events(), ec.Events()
	if len(actualTrail) != len(expectedTrail) || len(actualBus) != len(expectedBus) {
		h.t.Fatalf("events trail %+v bus %+v; want %+v %+v", actualTrail, actualBus, expectedTrail, expectedBus)
	}
	for i, e := range actualTrail {
		equal(h.t, e, expectedTrail[i])
	}
	for i, e := range actualBus {
		e.ID = expectedBus[i].ID
		equal(h.t, e, expectedBus[i])
	}
}

// R-S1WI-F3JS R-S34E-SVAH R-S4CB-6N16 R-KGIJ-B287 R-KHQF-OTYW R-KIYC-2LPL R-KK68-GDGA R-KMM1-7WXO R-S5K7-KERV R-KP1T-ZGF2
func TestExactMutationEventsAndQuietCalls(t *testing.T) {
	f := &artifactFake{head: 200, id: "artifact-one"}
	h := newHarness(t, f.factory(t))
	var ac tools.AccountChange
	for _, name := range []string{"Assets:Bank", "Income:Sales", "Expenses:Unused"} {
		h.expectEvents(func() { ac = h.open(name) }, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
			trail.AccountOpened(ctx, w, ac.Account.Name, ac.Account.OpenDate)
		})
		h.expectEvents(func() { h.open(name) }, nil)
	}
	var tx tools.TransactionChange
	h.expectEvents(func() { tx = h.post("event-entry") }, func(ctx context.Context, w *telemetry.Writer, em *events.Emitter) {
		v := tx.Transaction
		trail.TransactionPosted(ctx, w, em, v.ID, v.Date, v.Flag, v.Key, "", postings(v))
	})
	h.expectEvents(func() { h.post("event-entry") }, nil)
	h.expectEvents(func() {
		tx = success[tools.TransactionChange](h, "annotate", tools.AnnotateArgs{Transaction: tx.Transaction.ID, Payee: ptr("private-person"), Metadata: []byte(`{"private":"secret"}`)})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		trail.TransactionAnnotated(ctx, w, tx.Transaction.ID, tx.Transaction.Version)
	})
	h.expectEvents(func() {
		success[tools.TransactionChange](h, "annotate", tools.AnnotateArgs{Transaction: tx.Transaction.ID})
	}, nil)
	original := tx.Transaction.ID
	var rev tools.TransactionChange
	h.expectEvents(func() { rev = success[tools.TransactionChange](h, "reverse", tools.ReverseArgs{Transaction: original}) }, func(ctx context.Context, w *telemetry.Writer, em *events.Emitter) {
		v := rev.Transaction
		trail.ReversalPosted(ctx, em, v.ID, v.Date, v.Flag, v.Key, v.Reverses, postings(v))
		trail.TransactionReversed(ctx, w, v.ID, v.Reverses, v.Date)
	})
	h.expectEvents(func() { success[tools.TransactionChange](h, "reverse", tools.ReverseArgs{Transaction: original}) }, nil)
	var assertion tools.Assertion
	h.expectEvents(func() {
		assertion = success[tools.Assertion](h, "assert_balance", tools.AssertBalanceArgs{Account: "Assets:Bank", Date: "2026-04-02", Amount: "42.00"})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		a := assertion
		trail.AssertionAdded(ctx, w, a.ID, a.Account, a.Date, a.Amount, a.Commodity)
	})
	h.expectEvents(func() {
		success[tools.AssertionChange](h, "retract_assertion", tools.AssertionArgs{Assertion: assertion.ID})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		trail.AssertionRetracted(ctx, w, assertion.ID)
	})
	h.expectEvents(func() {
		success[tools.AssertionChange](h, "retract_assertion", tools.AssertionArgs{Assertion: assertion.ID})
	}, nil)
	var note tools.Note
	h.expectEvents(func() {
		note = success[tools.Note](h, "add_note", tools.AddNoteArgs{Account: "Assets:Bank", Date: "2026-04-02", Text: "private note"})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		trail.NoteAdded(ctx, w, note.ID, note.Account, note.Date)
	})
	h.expectEvents(func() { success[tools.NoteChange](h, "retract_note", tools.NoteArgs{Note: note.ID}) }, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		trail.NoteRetracted(ctx, w, note.ID)
	})
	h.expectEvents(func() { success[tools.NoteChange](h, "retract_note", tools.NoteArgs{Note: note.ID}) }, nil)
	var document tools.Document
	h.expectEvents(func() {
		document = success[tools.Document](h, "add_document", tools.AddDocumentArgs{Account: "Assets:Bank", Transaction: &original, Date: "2026-04-02", Path: "private/path.pdf"})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		d := document
		trail.DocumentAdded(ctx, w, d.ID, d.Account, d.Date, d.Transaction, d.Artifact)
	})
	h.expectEvents(func() {
		success[tools.DocumentChange](h, "retract_document", tools.DocumentArgs{Document: document.ID})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		trail.DocumentRetracted(ctx, w, document.ID)
	})
	h.expectEvents(func() {
		success[tools.DocumentChange](h, "retract_document", tools.DocumentArgs{Document: document.ID})
	}, nil)
	var book tools.BookChange
	h.expectEvents(func() {
		book = success[tools.BookChange](h, "set_book", tools.SetBookArgs{LockDate: ptr("2026-01-15")})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		trail.BookChanged(ctx, w, book.Book.LockDate, book.Book.FiscalYearStart)
	})
	h.expectEvents(func() {
		book = success[tools.BookChange](h, "set_book", tools.SetBookArgs{FiscalYearStart: ptr("03-01")})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		trail.BookChanged(ctx, w, book.Book.LockDate, book.Book.FiscalYearStart)
	})
	h.expectEvents(func() { success[tools.BookChange](h, "set_book", tools.SetBookArgs{}) }, nil)
	h.expectEvents(func() {
		ac = success[tools.AccountChange](h, "close_account", tools.AccountDateArgs{Name: "Expenses:Unused", Date: "2026-04-02"})
	}, func(ctx context.Context, w *telemetry.Writer, _ *events.Emitter) {
		trail.AccountClosed(ctx, w, ac.Account.Name, ac.Account.CloseDate)
	})
	h.expectEvents(func() {
		success[tools.AccountChange](h, "close_account", tools.AccountDateArgs{Name: "Expenses:Unused", Date: "2026-04-02"})
	}, nil)
	for _, tc := range []struct {
		name string
		args any
	}{{"book", tools.BookArgs{}}, {"accounts", tools.AccountsArgs{}}, {"account", tools.AccountArgs{Name: "Assets:Bank"}}, {"transaction", tools.TransactionArgs{Transaction: original}}, {"history", tools.HistoryArgs{Transaction: original}}, {"check", tools.CheckArgs{}}, {"journal", tools.JournalArgs{}}, {"register", tools.RegisterArgs{Account: "Assets:Bank"}}, {"balances", tools.BalancesArgs{}}, {"balance_sheet", tools.BalanceSheetArgs{}}, {"income_statement", tools.IncomeStatementArgs{From: "2026-01-01", To: "2026-04-08"}}} {
		h.expectEvents(func() {
			r := h.call(tc.name, tc.args)
			if r.IsError() {
				t.Fatal(tc.name, unpack(t, r))
			}
		}, nil)
	}
	h.expectEvents(func() {
		h.refuse("transaction", tools.TransactionArgs{Transaction: "bad-private-id"}, tools.RefusalNotFound+": "+tools.RefuseNotFound)
	}, nil)
	if len(f.received()) != 2 {
		t.Fatal(f.received())
	}
}
