package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-EEWA-0TAP
func TestLinkKeysMigrationBytes(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0021_link_keys.sql")
	contractOK(t, err)
	contractEqual(t, string(b), "ALTER TABLE links ADD COLUMN ein_key TEXT NOT NULL DEFAULT '';\nCREATE INDEX links_ein ON links(ein_key);\nCREATE TABLE link_name_keys (scraper TEXT NOT NULL, source_id TEXT NOT NULL, position INTEGER NOT NULL, name_key TEXT NOT NULL, PRIMARY KEY(scraper,source_id,position));\nCREATE INDEX link_name_keys_key ON link_name_keys(name_key);\n")
}

func priorLink(t *testing.T, id, source string, gone bool) (store.Link, string) {
	t.Helper()
	raw := `{"Name":"Prior business","NameKey":"PRIOR","State":"MN","Zip":"55072","Names":["Earlier alias"],"Street":"Previous road","PhoneKey":"1234567890"}`
	var reading sources.Reading
	contractOK(t, json.Unmarshal([]byte(raw), &reading))
	at, err := time.Parse(time.RFC3339, "2025-04-03T06:07:08+02:00")
	contractOK(t, err)
	return store.Link{Candidate: id, Scraper: "ppp", SourceID: source, Run: "old-run", Digest: "old-digest", Reading: reading, Reason: store.LinkSourceKey, Confidence: .71, FirstSeen: at.UTC(), LastSeen: at.Add(time.Hour).UTC(), Gone: gone}, raw
}

func writePriorLink(ctx context.Context, tx *sql.Tx, l store.Link, raw string, update bool) error {
	args := []any{l.Candidate, l.Run, l.Digest, l.Reading.NameKey, l.Reading.State, l.Reading.Zip, l.Reading.PhoneKey, l.Reading.DomainKey, raw, l.Reason, l.Confidence, l.FirstSeen.Format(time.RFC3339), l.LastSeen.Format(time.RFC3339), l.Gone, l.Scraper, l.SourceID}
	query := `INSERT INTO links(candidate,run,digest,name_key,state,zip,phone_key,domain_key,reading,reason,confidence,first_seen,last_seen,gone,scraper,source_id) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if update {
		query = `UPDATE links SET candidate=?,run=?,digest=?,name_key=?,state=?,zip=?,phone_key=?,domain_key=?,reading=?,reason=?,confidence=?,first_seen=?,last_seen=?,gone=? WHERE scraper=? AND source_id=?`
	}
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

// R-EG46-EL1E
func TestLinkKeysUpgradePreservesPreviousLinks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0020_headcounts.sql")})
	contractOK(t, err)
	first, raw := priorLink(t, "prior-candidate", "one", false)
	second, _ := priorLink(t, "prior-candidate", "two", true)
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		if e := writePriorCandidate(ctx, tx, first.Candidate); e != nil {
			return e
		}
		if e := writePriorLink(ctx, tx, first, raw, false); e != nil {
			return e
		}
		return writePriorLink(ctx, tx, second, raw, false)
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{})
	got, err := s.Links(ctx, first.Candidate)
	contractOK(t, err)
	contractEqual(t, got, []store.Link{first, second})
}

// R-EHC2-SCS3 R-IHSZ-TWWH
func TestLinkKeysPreviousWriterAndStaleIndexes(t *testing.T) {
	f := contractDB(t)
	o := keyedObservation("current", "111111111", "CURRENT", "EXTRA")
	ids := contractAttach(t, f, "initial", o)
	untouched := keyedObservation("untouched", "222222222", "UNTOUCHED")
	other := contractAttach(t, f, "initial", untouched)
	before, err := f.s.Links(context.Background(), other[0])
	contractOK(t, err)
	replaced, raw := priorLink(t, ids[0], "current", true)
	replaced.Scraper = o.Scraper
	inserted, _ := priorLink(t, ids[0], "rollback-new", false)
	contractOK(t, f.d.Write(context.Background(), func(tx *sql.Tx) error {
		if e := writePriorLink(context.Background(), tx, replaced, raw, true); e != nil {
			return e
		}
		return writePriorLink(context.Background(), tx, inserted, raw, false)
	}))
	check := func() {
		links, e := f.s.Links(context.Background(), ids[0])
		contractOK(t, e)
		contractEqual(t, len(links), 2)
		for _, l := range links {
			if l.SourceID == replaced.SourceID {
				contractEqual(t, l, replaced)
			} else {
				contractEqual(t, l, inserted)
			}
		}
		links, e = f.s.Links(context.Background(), other[0])
		contractOK(t, e)
		contractEqual(t, links, before)
	}
	check()
	f.reopen()
	check()
	// The previous writer leaves physical EIN and alias indexes in place;
	// matching must consult the current reading rather than those stale values.
	for _, obs := range []store.Observation{keyedObservation("stale-ein", "111111111", "ABSENT"), keyedObservation("stale-alias", "", "EXTRA")} {
		counts, newIDs, e := f.s.Attach(context.Background(), "after", false, []store.Observation{obs})
		contractOK(t, e)
		contractEqual(t, counts, store.Counts{Rows: 1, New: 1, Created: 1})
		if newIDs[0] == ids[0] {
			t.Fatal("matched stale index key")
		}
	}
	obs := keyedObservation("rollback-name", "", "", "PRIOR")
	counts, newIDs, e := f.s.Attach(context.Background(), "after", false, []store.Observation{obs})
	contractOK(t, e)
	contractEqual(t, counts, store.Counts{Rows: 1, New: 1, Linked: 1})
	contractEqual(t, newIDs, ids)
}
