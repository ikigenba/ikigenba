package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/gmail/internal/store"
)

// R-31AY-J8LV R-32IU-X0CK R-33QR-AS39 R-L6S1-435U
func TestListWindows(t *testing.T) {
	f := setup(t)
	f.now = time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	cDraft, cTransfer := addD(t, f, draft()), addT(t, f, transfer())
	bDraft, bTransfer := addD(t, f, draft()), addT(t, f, transfer())
	if bDraft.ID <= cDraft.ID || bTransfer.ID <= cTransfer.ID {
		t.Fatal("fixture ids do not establish tie order")
	}
	f.now = f.now.Add(5 * time.Second)
	aDraft, aTransfer := addD(t, f, draft()), addT(t, f, transfer())
	var err error
	bDraft, err = f.s.AttachToDraft(ctx, bDraft.ID, store.Attachment{Path: "attached", Name: "file", ContentType: "application/example", Bytes: 9}, "attached-message", "attached-thread")
	if err != nil {
		t.Fatal(err)
	}
	cDraft, err = f.s.MarkDraftSent(ctx, cDraft.ID, "sent-message", "sent-thread", f.now)
	if err != nil {
		t.Fatal(err)
	}
	bTransfer, err = f.s.StartTransfer(ctx, bTransfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	bTransfer, err = f.s.FinishTransfer(ctx, bTransfer.ID, store.Ending{Status: store.StatusDone, Bytes: 23, URL: "artifact-url", Link: "mail-link", ArtifactID: "artifact-id"})
	if err != nil {
		t.Fatal(err)
	}
	otherDraft, otherTransfer := draft(), transfer()
	otherDraft.UserID, otherTransfer.UserID = "other", "other"
	addD(t, f, otherDraft)
	addT(t, f, otherTransfer)
	allDrafts, err := f.s.ListDrafts(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	allTransfers, err := f.s.ListTransfers(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, allDrafts, []store.Draft{aDraft, bDraft, cDraft})
	eq(t, allTransfers, []store.Transfer{aTransfer, bTransfer, cTransfer})
	for _, tc := range []struct {
		position, draftID, transferID string
		n                             int
		from, to                      int
	}{
		{"", "", "", 2, 0, 2},
		{bDraft.Created.UTC().Format(time.RFC3339), bDraft.ID, bTransfer.ID, 2, 2, 3},
		{aDraft.Created.UTC().Format(time.RFC3339), aDraft.ID, aTransfer.ID, 5, 1, 3},
		{"", "", "", 3, 0, 3},
		{"", "", "", 5, 0, 3},
		{"", "", "", 0, 0, 0},
		{"", "", "", -1, 0, 0},
		{cDraft.Created.UTC().Format(time.RFC3339), cDraft.ID, cTransfer.ID, 2, 3, 3},
		{"", bDraft.ID, bTransfer.ID, 2, 0, 0},
		{bDraft.Created.UTC().Format(time.RFC3339), "", "", 2, 0, 0},
	} {
		ds, e := f.s.ListDraftsAfter(ctx, "owner", tc.position, tc.draftID, tc.n)
		if e != nil {
			t.Fatal(e)
		}
		eq(t, ds, append([]store.Draft{}, allDrafts[tc.from:tc.to]...))
		ts, e := f.s.ListTransfersAfter(ctx, "owner", tc.position, tc.transferID, tc.n)
		if e != nil {
			t.Fatal(e)
		}
		eq(t, ts, append([]store.Transfer{}, allTransfers[tc.from:tc.to]...))
	}
	ds, err := f.s.ListDraftsAfter(ctx, "absent", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ds, []store.Draft{})
	ts, err := f.s.ListTransfersAfter(ctx, "absent", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ts, []store.Transfer{})
}

// R-32IU-X0CK R-366K-2BKN
func TestHasActivePush(t *testing.T) {
	for _, status := range []string{store.StatusQueued, store.StatusRunning} {
		for _, ending := range []string{store.StatusDone, store.StatusFailed} {
			f := setup(t)
			check := func(user, draftID string, want bool) {
				t.Helper()
				got, err := f.s.HasActivePush(ctx, user, draftID)
				if err != nil {
					t.Fatal(err)
				}
				eq(t, got, want)
			}
			check("owner", "draft", false)
			v := transfer()
			v.Direction, v.Status, v.Draft = store.DirectionPush, status, "draft"
			push := addT(t, f, v)
			check("owner", "draft", true)
			check("other", "draft", false)
			check("owner", "different", false)
			if status == store.StatusQueued && ending == store.StatusDone {
				if _, err := f.s.StartTransfer(ctx, push.ID); err != nil {
					t.Fatal(err)
				}
			}
			e := store.Ending{Status: ending}
			if ending == store.StatusFailed {
				e.Reason = store.ReasonProvider
			}
			if _, err := f.s.FinishTransfer(ctx, push.ID, e); err != nil {
				t.Fatal(err)
			}
			check("owner", "draft", false)
			v.Direction = store.DirectionPull
			addT(t, f, v)
			check("owner", "draft", false)
		}
	}
}

// R-ZID7-WQ4N R-ZJL4-AHVC
func TestWindowAndPushUnavailable(t *testing.T) {
	f := setup(t)
	draft := addD(t, f, draft())
	transfer := addT(t, f, transfer())
	check := func(c context.Context) {
		t.Helper()
		checkError := func(err error) {
			t.Helper()
			requireErr(t, err, store.ErrUnreachable)
			if c.Err() != nil {
				requireErr(t, err, c.Err())
			}
			for _, forbidden := range []error{store.ErrNotFound, store.ErrEnded, store.ErrNotOpen} {
				if errors.Is(err, forbidden) {
					t.Fatal(err)
				}
			}
		}
		for _, n := range []int{0, 2} {
			_, err := f.s.ListDraftsAfter(c, "owner", "", "", n)
			checkError(err)
			_, err = f.s.ListTransfersAfter(c, "owner", "", "", n)
			checkError(err)
		}
		active, err := f.s.HasActivePush(c, "owner", "draft")
		checkError(err)
		eq(t, active, false)
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	check(c)
	f.d.SetFailing(true)
	check(ctx)
	check(c)
	f.d.SetFailing(false)
	checkD(t, f, draft)
	checkT(t, f, transfer)
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	check(ctx)
	check(c)
	later := open(t, f)
	defer func() { _ = later.Close() }()
	f.s = store.New(later, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	checkD(t, f, draft)
	checkT(t, f, transfer)
}
