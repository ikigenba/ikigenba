package store_test

import (
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/gmail/internal/store"
)

// R-GQ5X-XRFJ R-GSLQ-PAWX R-GTTN-32NM
func TestListBeforeWindows(t *testing.T) {
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
	reverseDrafts := []store.Draft{allDrafts[2], allDrafts[1], allDrafts[0]}
	reverseTransfers := []store.Transfer{allTransfers[2], allTransfers[1], allTransfers[0]}
	base := cDraft.Created.UTC().Format(time.RFC3339)
	for _, tc := range []struct {
		name, user, position, draftID, transferID string
		n                                         int
		from, to                                  int
	}{
		{"last", "owner", "", "", "", 2, 0, 2},
		{"tie", "owner", base, cDraft.ID, cTransfer.ID, 2, 1, 3},
		{"above-tie", "owner", base, bDraft.ID, bTransfer.ID, 5, 2, 3},
		{"newest", "owner", aDraft.Created.UTC().Format(time.RFC3339), aDraft.ID, aTransfer.ID, 5, 3, 3},
		{"single", "owner", "", "", "", 1, 0, 1},
		{"exact", "owner", "", "", "", 3, 0, 3},
		{"oversize", "owner", "", "", "", 5, 0, 3},
		{"zero", "owner", "", "", "", 0, 0, 0},
		{"negative", "owner", "", "", "", -1, 0, 0},
		{"zero-position", "owner", base, cDraft.ID, cTransfer.ID, 0, 0, 0},
		{"negative-position", "owner", base, cDraft.ID, cTransfer.ID, -1, 0, 0},
		{"absent-time", "owner", cDraft.Created.Add(time.Second).Format(time.RFC3339), "absent", "absent", 2, 2, 3},
		{"absent-id", "owner", base, cDraft.ID + "x", cTransfer.ID + "x", 2, 1, 3},
		{"empty-time", "owner", "", bDraft.ID, bTransfer.ID, 5, 0, 3},
		{"empty-id", "owner", base, "", "", 5, 0, 3},
		{"before-oldest", "owner", cDraft.Created.Add(-time.Second).Format(time.RFC3339), "absent", "absent", 5, 0, 3},
		{"after-newest", "owner", aDraft.Created.Add(time.Second).Format(time.RFC3339), "absent", "absent", 5, 3, 3},
		{"absent-user", "absent", "", "", "", 5, 0, 0},
		{"quoted-user", "owner' OR 1=1 --", "", "", "", 5, 0, 0},
		{"quoted-time", "owner", "zz' OR 1=1 --", "", "", 5, 0, 0},
		{"quoted-id", "owner", base, "zz' OR 1=1 --", "zz' OR 1=1 --", 5, 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds, err := f.s.ListDraftsBefore(ctx, tc.user, tc.position, tc.draftID, tc.n)
			if err != nil {
				t.Fatal(err)
			}
			eq(t, ds, append([]store.Draft{}, reverseDrafts[tc.from:tc.to]...))
			ts, err := f.s.ListTransfersBefore(ctx, tc.user, tc.position, tc.transferID, tc.n)
			if err != nil {
				t.Fatal(err)
			}
			eq(t, ts, append([]store.Transfer{}, reverseTransfers[tc.from:tc.to]...))
		})
	}
}
