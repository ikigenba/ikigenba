package drafts_test

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
)

// R-L7ZX-HUWJ
func TestBusyProbeFailure(t *testing.T) {
	for _, which := range []string{"send", "discard"} {
		t.Run(which, func(t *testing.T) {
			f := fixture(t)
			d := f.add(t)
			f.setHandler(func(w http.ResponseWriter, _ *http.Request) {
				f.db.SetFailing(true)
				writeJSON(w, 200, map[string]any{"id": d.GmailDraft, "message": map[string]any{"id": d.Message, "threadId": d.Thread}})
			})
			got, err := operation(f, which, d.ID)
			var r *transfers.Refusal
			if !errors.Is(err, store.ErrUnreachable) || errors.As(err, &r) || !reflect.DeepEqual(got, store.Draft{}) {
				t.Fatalf("got %#v, error %v", got, err)
			}
			requests := f.snapshot()
			if len(requests) != 1 || requests[0].Method != http.MethodGet {
				t.Fatal(requests)
			}
			f.noEvents(t)
		})
	}
}

// R-L7ZX-HUWJ
func TestUnrelatedPushDoesNotRefuseFinish(t *testing.T) {
	for _, which := range []string{"send", "discard"} {
		for _, otherUser := range []bool{false, true} {
			f := fixture(t)
			d := f.add(t)
			user, draftID := f.caller.UserID, "different-draft"
			if otherUser {
				user, draftID = "other", d.ID
			}
			if _, err := f.st.AddTransfer(f.ctx, store.Transfer{Status: store.StatusRunning, Direction: store.DirectionPush, Draft: draftID, UserID: user, Message: d.Message, Path: "part"}); err != nil {
				t.Fatal(err)
			}
			got, err := operation(f, which, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			status := store.DraftSent
			if which == "discard" {
				status = store.DraftDiscarded
			}
			if got.Status != status {
				t.Fatal(got)
			}
		}
	}
}
