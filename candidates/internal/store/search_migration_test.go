package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-4J5Q-HYYN
func TestSearchIndexMigrationBytes(t *testing.T) {
	got, e := fs.ReadFile(candidates.Migrations(), "0030_search_index.sql")
	contractOK(t, e)
	want := "CREATE VIRTUAL TABLE search_index USING fts5(name, candidate UNINDEXED, display UNINDEXED, tokenize='trigram');\n" +
		"CREATE VIRTUAL TABLE search_index_vocab USING fts5vocab(search_index, 'row');\n" +
		"INSERT INTO search_index(name, candidate, display) SELECT name, id, 1 FROM candidates WHERE status = 'active' AND name <> '' UNION SELECT j.value, c.id, 0 FROM candidates AS c, json_each(c.names) AS j WHERE c.status = 'active' AND j.type = 'text' AND j.value <> '' AND j.value <> c.name;\n"
	contractEqual(t, string(got), want)
}

type searchPriorRow struct {
	id, name, status string
	names            []string
}

func searchUpgrade(t *testing.T, rows []searchPriorRow) *store.Store {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "search.db")
	clock := func() time.Time { return time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC) }
	d, e := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0029_campaign_opened.sql"), Now: clock})
	contractOK(t, e)
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO candidates(id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated) VALUES (?,?,'',?,?,?,'','','','','','','','[]','','[]',?,?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, row := range rows {
			b, err := json.Marshal(row.names)
			if err != nil {
				return err
			}
			if _, err = stmt.ExecContext(ctx, row.id, row.status, row.name, string(b), strings.ToLower(strings.Join(row.names, "\n")), clock().Format(time.RFC3339), clock().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		return nil
	}))
	contractOK(t, d.Close())
	d, e = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations(), Now: clock})
	contractOK(t, e)
	t.Cleanup(func() { contractOK(t, d.Close()) })
	return store.New(d, store.Config{Now: clock, Rand: &contractRandom{next: 1}})
}

func TestSearchIndexBackfillsPreviousRelease(t *testing.T) {
	rows := []searchPriorRow{
		{"cand_0000000000000001", "Acme Trucking", store.StatusActive, []string{"Acme Trucking", "ACME TRUCKING LLC", "", "Alias Freight", "Alias Freight"}},
		{"cand_0000000000000002", "Zed Holdings", store.StatusActive, []string{"Zed Holdings", "Acme Trucking"}},
		{"cand_0000000000000003", "Merged Corporation", store.StatusMerged, []string{"Merged Corporation", "Merged Alternate", ""}},
		{"cand_0000000000000004", "Ébène Co", store.StatusActive, []string{"Ébène Co", "ÉBÈNE ALT"}},
		{"cand_0000000000000005", "A \"Quoted\" Name", store.StatusActive, []string{"A \"Quoted\" Name", "Dash-and.Space"}},
	}
	s := searchUpgrade(t, rows)
	ids := []string{}
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	cs := searchCandidates(t, s, ids)
	for _, q := range []string{"acme", "Acme Trucking", "alias freight", "merged", "éb", "ébène", "ÉBÈNE ALT", "a \"quoted\" name", "\"quoted\"", "dash-and.space"} {
		assertSearchRanking(t, s, cs, q, 50)
	}
}

// R-7LQD-OD4I
func TestSearchIgnoresPreviousReleaseMergeStaleIndex(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := searchAttach(t, f, "stale", "Stale Search Company")
	b := searchAttach(t, f, "survivor", "Other Company")
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE candidates SET status='merged',merged_into=? WHERE id=?", b, a)
		return e
	}))
	for _, limit := range []int{1, 10, 50} {
		got, e := f.s.Search(ctx, "Stale Search Company", limit)
		contractOK(t, e)
		contractEqual(t, len(got), 0)
	}
}

func searchRowID(i int) string { return fmt.Sprintf("cand_%016x", i+1) }
