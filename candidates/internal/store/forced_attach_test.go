package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-WO5G-TEFB R-WPDD-7660
func TestForcedAttachComparesStoredReadingBytes(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	o := contractObservation("forced", "Original", f.now)
	id := contractAttach(t, f, "old", o)[0]
	links, e := f.s.Links(ctx, id)
	contractOK(t, e)
	old := links[0]
	f.now = f.now.Add(time.Hour)
	o.Digest = "other"
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, `UPDATE links SET gone=1`); return e }))
	count, ids, e := f.s.Attach(ctx, "new", true, []store.Observation{o})
	contractOK(t, e)
	contractEqual(t, count, store.Counts{Rows: 1, Unchanged: 1})
	contractEqual(t, ids, []string{id})
	want := old
	want.Run = "new"
	want.LastSeen = f.now.UTC().Truncate(time.Second)
	links, e = f.s.Links(ctx, id)
	contractOK(t, e)
	contractEqual(t, links, []store.Link{want})
	// An old release's omitted field decodes to the same reading, but its bytes differ.
	raw, e := json.Marshal(o.Reading)
	contractOK(t, e)
	var object map[string]json.RawMessage
	contractOK(t, json.Unmarshal(raw, &object))
	delete(object, "NameKey")
	raw, e = json.Marshal(object)
	contractOK(t, e)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE links SET reading=? WHERE candidate=?`, string(raw), id)
		return e
	}))
	links, e = f.s.Links(ctx, id)
	contractOK(t, e)
	o.Reading = links[0].Reading
	count, ids, e = f.s.Attach(ctx, "rewrite", true, []store.Observation{o})
	contractOK(t, e)
	contractEqual(t, count, store.Counts{Rows: 1, Changed: 1, Linked: 1})
	contractEqual(t, ids, []string{id})
	want = links[0]
	want.Digest = o.Digest
	want.Reading = o.Reading
	want.Run = "rewrite"
	want.LastSeen = f.now.UTC().Truncate(time.Second)
	links, e = f.s.Links(ctx, id)
	contractOK(t, e)
	contractEqual(t, links, []store.Link{want})
}

// R-WQL9-KXWP
func TestForcedAttachReplacesHeldMatchKeys(t *testing.T) {
	f := regNew(t)
	o := keyedObservation("one", "old-ein", "A", "B")
	_, ids := regAttach(t, f, "old", o)
	changed := keyedObservation("one", "new-ein", "A")
	changed.Digest = o.Digest
	count, touched, e := f.st.Attach(regCtx, "new", true, []store.Observation{changed})
	regNoErr(t, e)
	regEqual(t, count, store.Counts{Rows: 1, Changed: 1, Linked: 1})
	regEqual(t, touched, ids)
	for _, incoming := range []store.Observation{keyedObservation("old-ein", "old-ein", "OTHER"), keyedObservation("old-alias", "", "B")} {
		_, got := regAttach(t, f, "later", incoming)
		if got[0] == ids[0] {
			t.Fatal("retired key matched", incoming.SourceID)
		}
	}
	assertKeyAttach(t, f, keyedObservation("new-ein", "new-ein", "OTHER"), ids[0], store.LinkEIN, .9)
	// New keys remain usable after reopening.
	f.reopen(t)
	assertKeyAttach(t, f, keyedObservation("new-alias", "", "A"), ids[0], store.LinkMatchKey, .8)
}
