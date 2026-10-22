package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-NUZV-NOR9
func TestOpensMigrationBytes(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0019_opens.sql")
	contractOK(t, err)
	contractEqual(t, string(b), "CREATE TABLE opens (seq INTEGER PRIMARY KEY, prospect_list TEXT NOT NULL, at TEXT NOT NULL, user_id TEXT NOT NULL, user_email TEXT NOT NULL, filters TEXT NOT NULL, ordering TEXT NOT NULL, size INTEGER NOT NULL, members TEXT NOT NULL, passing TEXT NOT NULL);\nCREATE INDEX opens_prospect_list ON opens(prospect_list,seq);\n")
}
func priorProspectList(ctx context.Context, tx *sql.Tx, id, filters string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO prospect_lists(id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed) VALUES (?,'prior list',?,'[]',30,'prior','prior@test','2026-01-01T00:00:00Z','prior','prior@test','2026-01-01T00:00:00Z')`, id, filters)
	return err
}

// R-3JJQ-52J4 R-3KRM-IU9T
func TestProspectListOpensAcrossUpgradeAndRollback(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0018_drive_route.sql")})
	contractOK(t, err)
	id := "pl_0011223344556677"
	candidate := "cand_0011223344556677"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		if err := writePriorCandidate(ctx, tx, candidate); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO judgments(candidate,operating,operating_key,evidence,excluded,rules,calculated) VALUES (?,0.7,700000,'[]',0,1,'2026-01-01T00:00:00Z')`, candidate); err != nil {
			return err
		}
		return priorProspectList(ctx, tx, id, `{"categories":[],"revenue_min":5,"revenue_unknown":true}`)
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }})
	k, err := s.GetProspectList(ctx, id)
	contractOK(t, err)
	want := store.ProspectList{ID: id, Name: "prior list", Size: 30, Filters: store.ProspectListFilters{RevenueMin: ptrInt(5), RevenueUnknown: true}, Order: []store.ProspectListOrder{{Field: store.OrderOperatingConfidence, Direction: store.DirectionDesc}}, CreatedUserID: "prior", CreatedUserEmail: "prior@test", ChangedUserID: "prior", ChangedUserEmail: "prior@test", Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Changed: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	contractEqual(t, k, want)
	read, err := s.ReadProspectList(ctx, id)
	contractOK(t, err)
	contractEqual(t, read.Changes, emptyChanges())
	contractEqual(t, len(read.Members), 1)
	os, err := s.Opens(ctx, id)
	contractOK(t, err)
	contractEqual(t, os, []store.Open{})
	_, err = s.OpenProspectList(ctx, id, "reader", "reader@test")
	contractOK(t, err)
	before, err := s.Opens(ctx, id)
	contractOK(t, err)
	added := "pl_8899aabbccddeeff"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error { return priorProspectList(ctx, tx, added, `{"class":"private"}`) }))
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE prospect_lists SET filters='{}' WHERE id=?`, id)
		return err
	}))
	updated, err := s.GetProspectList(ctx, id)
	contractOK(t, err)
	want.Filters = store.ProspectListFilters{}
	contractEqual(t, updated, want)
	addedList, err := s.GetProspectList(ctx, added)
	contractOK(t, err)
	want.ID = added
	want.Filters = store.ProspectListFilters{Class: "private"}
	contractEqual(t, addedList, want)
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM prospect_lists WHERE id=?`, id)
		return err
	}))
	after, err := s.Opens(ctx, id)
	contractOK(t, err)
	contractEqual(t, after, before)
	never, err := s.Opens(ctx, added)
	contractOK(t, err)
	contractEqual(t, never, []store.Open{})
	_, err = s.GetProspectList(ctx, id)
	contractError(t, err, store.ErrNotFound)
	_, err = s.ReadProspectList(ctx, id)
	contractError(t, err, store.ErrNotFound)
	_, err = s.OpenProspectList(ctx, id, "reader", "")
	contractError(t, err, store.ErrNotFound)
}
