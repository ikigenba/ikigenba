package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-VIOF-L0A2
func TestCandidateParkSkipsEmptyParkAndPreservesRecords(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	ks := seedParks(t, f)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 42}))
	unwalked := contractAttach(t, f, "records", contractObservation("unwalked", "Unwalked", f.now))[0]
	p := walkPass(t, f)
	walkSweep(t, f, p, ks[0], walkFound("a-key", "Alpha", "3205550160", ""))
	walkSweep(t, f, p, ks[1], walkFound("b-key", "Beta", "3205550160", ""))
	ps, err := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, err)
	id := ps[0].Candidate
	_, err = f.s.FinishWalk(ctx, p.ID, store.Counts{}, false)
	contractOK(t, err)
	f.now = f.now.Add(time.Hour)
	p = walkPass(t, f)
	walkSweep(t, f, p, ks[0], walkFound("z-key", "Zulu", "3205550160", ""))
	_, _, err = f.s.JudgeAll(ctx, "", 100, f.now)
	contractOK(t, err)
	assertPark := func(candidate, want string, missing bool) {
		t.Helper()
		before := contractState(t, f)
		walks, err := f.s.PassWalks(ctx, p.ID)
		contractOK(t, err)
		park, err := f.s.CandidatePark(ctx, candidate)
		if missing {
			contractError(t, err, store.ErrNotFound)
		} else {
			contractOK(t, err)
			contractEqual(t, park, want)
		}
		contractEqual(t, contractState(t, f), before)
		after, err := f.s.PassWalks(ctx, p.ID)
		contractOK(t, err)
		contractEqual(t, after, walks)
	}
	assertPark(id, ks[0].ID, false)
	assertPark(unwalked, "", false)
	assertPark("missing", "", true)
	// An empty park is ineligible even when its Place ID precedes every other link.
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE links SET park='' WHERE scraper=? AND source_id=?", places.Name, "a-key")
		return err
	}))
	assertPark(id, ks[1].ID, false)
	f.reopen()
	assertPark(id, ks[1].ID, false)
}
