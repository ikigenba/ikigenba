package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func writeOldWebsite(ctx context.Context, tx *sql.Tx, id string, found []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,found,source,at,cost) VALUES (?,?,?,?,?,?)`, id, sources.WebsiteCheck, string(found), "business.test", "2026-01-02T03:04:05Z", .25)
	return err
}

// R-HBTZ-T61P R-HD1W-6XSE R-H22S-R045
func TestFactsMigrationAndRollbackResults(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "facts.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0011_enrichments.sql")})
	contractOK(t, err)
	id, pid := "cand_1111111111111111", "pass_1111111111111111"
	e := websiteResult(id, sources.WebsiteHTTPS)
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		if err := writePriorCandidate(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE candidates SET email='office@business.test' WHERE id=?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO assessments(candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,excluded,rules,scored) VALUES (?,0.6,600000,'[]',100,200,0.7,'2020-01-01T00:00:00Z','pattern','2026-01-01T00:00:00Z','[]','pass',3,2,'http','http://old.test',42,1,?,'2026-10-01T00:00:00Z')`, id, sources.Rules); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none") VALUES (?,'rebuild','done','','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z',1,2,3,4,5,6,7,8,9,'owner','mail','request','','rebuild','rebuild',10,11,12,13,14)`, pid); err != nil {
			return err
		}
		return writeOldWebsite(ctx, tx, id, e.Found)
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }})
	es, err := s.Enrichments(ctx, id)
	contractOK(t, err)
	contractEqual(t, len(es), 1)
	e.Cost = .25
	e.At = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	contractEqual(t, es[0], e)
	_, err = s.GetFacts(ctx, id)
	contractError(t, err, store.ErrNotFound)
	assertPriorJudgment(t, s, id)
	p, err := s.GetPass(ctx, pid)
	contractOK(t, err)
	contractEqual(t, p, store.Pass{ID: pid, Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, Status: store.StatusDone, Started: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Finished: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, UserID: "owner", UserEmail: "mail", RequestID: "request"})
	contractOK(t, s.FillFacts(ctx))
	facts, err := s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, facts.Website, sources.WebsiteHTTPS)
	contractEqual(t, facts.Written, []store.Written{{Kind: sources.WebsiteCheck, At: e.At}})
	// The previous writer continues to work after a rollback, leaving empty inputs.
	e.Found = websiteResult(id, sources.WebsiteNone).Found
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error { return writeOldWebsite(ctx, tx, id, e.Found) }))
	es, err = s.Enrichments(ctx, id)
	contractOK(t, err)
	contractEqual(t, es[0].Inputs, "")
	contractEqual(t, es[0].Found, e.Found)
	due, err := s.DueEnrichments(ctx, sources.WebsiteCheck, "", 10)
	contractOK(t, err)
	contractEqual(t, due, []store.Due{{Candidate: id, Inputs: "business.test"}})
	contractOK(t, s.FillFacts(ctx))
	kept, err := s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, kept, facts)
	_, err = s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteNone)})
	contractOK(t, err)
	due, err = s.DueEnrichments(ctx, sources.WebsiteCheck, "", 10)
	contractOK(t, err)
	contractEqual(t, due, []store.Due{})
}
