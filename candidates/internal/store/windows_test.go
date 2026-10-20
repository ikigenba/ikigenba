package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-YDI7-O1ZN R-YOGN-BHV4
func TestFindDeliveredUsesNewestMatchingPass(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 0}))
	ingest := contractNewPass(t, f, store.TriggerIngest)
	_, err := f.s.FinishPass(ctx, ingest.ID, store.Counts{})
	contractOK(t, err)
	old := contractNewPass(t, f, "older")
	old, err = f.s.FinishPass(ctx, old.ID, store.Counts{})
	contractOK(t, err)
	f.now = f.now.Add(time.Hour)
	newest := contractNewPass(t, f, "newer")
	newest, err = f.s.FailPass(ctx, newest.ID, store.ReasonScrapes)
	contractOK(t, err)
	tied := contractNewPass(t, f, "same-start")
	contractEqual(t, tied.Started, newest.Started)
	if tied.ID <= newest.ID {
		t.Fatal("fixture must exercise descending id tie order")
	}
	before := contractState(t, f)
	for _, pair := range [][2]string{{old.Delivery, old.Trigger}, {old.Delivery, "unrelated"}, {"another", old.Trigger}} {
		got, e := f.s.FindDelivered(ctx, pair[0], pair[1])
		contractOK(t, e)
		contractEqual(t, got, old)
	}
	got, err := f.s.FindDelivered(ctx, old.Delivery, newest.Trigger)
	contractOK(t, err)
	contractEqual(t, got, newest)
	got, err = f.s.FindDelivered(ctx, newest.Delivery, "unrelated")
	contractOK(t, err)
	contractEqual(t, got, newest)
	got, err = f.s.FindDelivered(ctx, newest.Delivery, tied.Trigger)
	contractOK(t, err)
	contractEqual(t, got, tied)
	got, err = f.s.FindDelivered(ctx, tied.Delivery, newest.Trigger)
	contractOK(t, err)
	contractEqual(t, got, tied)
	for _, pair := range [][2]string{{"", store.TriggerIngest}, {"", ""}, {"missing", "missing"}} {
		got, e := f.s.FindDelivered(ctx, pair[0], pair[1])
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, got, store.Pass{})
	}
	contractEqual(t, contractState(t, f), before)
	f.d.SetFailing(true)
	_, err = f.s.FindDelivered(ctx, old.Delivery, old.Trigger)
	contractError(t, err, store.ErrUnreachable)
}

// R-4HU1-P3L0
func TestNameFilterUnicodeLiteralAndNoCrossNameMatch(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	for _, name := range []string{"PETITE ÉCOLE LTD", "100% A_B", "1000 AXB", "ACME TRUCK"} {
		o := contractObservation(name, name, f.now)
		o.Reading.Names = []string{name, "ACME"}
		contractAttach(t, f, "run", o)
	}
	for _, name := range []string{"ÉCOLE", "école", "100%", "A_B"} {
		cs, e := f.s.ListCandidates(ctx, store.CandidateFilter{Name: name}, "", "", 2)
		contractOK(t, e)
		contractEqual(t, len(cs), 1)
	}
	cs, e := f.s.ListCandidates(ctx, store.CandidateFilter{Name: "TRUCK\nACME"}, "", "", 2)
	contractOK(t, e)
	if cs == nil || len(cs) != 0 {
		t.Fatal(cs)
	}
}
