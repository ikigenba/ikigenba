package store_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

func TestAdoptWholeSecondUTCCatalog(t *testing.T) {
	// R-RIWA-WS33
	created := time.Date(2026, 10, 1, 12, 34, 56, 0, time.UTC)
	records := []store.Site{
		{ID: "sit_0000000000000001", Name: "docs", Slug: "docs", Owner: "alice", Repo: "repository-a", Ref: "main", Visibility: store.Public, Listed: true, Commit: strings.Repeat("a", 40), Created: created, Published: created.Add(time.Hour)},
		{ID: "sit_0000000000000002", Name: "draft", Slug: "draft-01234567", Owner: "alice", Repo: "repository-b", Ref: "refs/heads/next", Visibility: store.Private, Listed: false, Created: created.Add(-time.Hour)},
		{ID: "sit_0000000000000003", Name: "reference", Slug: "reference", Owner: "bob", Repo: "repository-c", Ref: "release", Visibility: store.Public, Listed: true, Commit: strings.Repeat("0123456789abcdef", 2) + "01234567", Created: time.Time{}, Published: created.Add(2 * time.Hour)},
	}
	for _, tc := range []struct {
		name    string
		records []store.Site
		apexID  string
	}{
		{name: "empty"},
		{name: "no-apex", records: records},
		{name: "published-apex", records: records, apexID: records[2].ID},
		{name: "unpublished-apex", records: []store.Site{{ID: "sit_0000000000000004", Name: "new", Slug: "new", Owner: "bob", Repo: "repository-d", Ref: "main", Visibility: store.Public, Created: created}}, apexID: "sit_0000000000000004"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state", "sites.db")
			clock := func() time.Time { return created.Add(24 * time.Hour) }
			legacy, err := db.Open(ctx, db.Config{Path: path, Migrations: fstest.MapFS{}, Now: clock})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = legacy.Close() })
			err = legacy.Write(ctx, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, "CREATE TABLE sites (id TEXT PRIMARY KEY, name TEXT NOT NULL, slug TEXT NOT NULL, owner TEXT NOT NULL, payload TEXT NOT NULL); CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL); DROP TABLE schema_migrations"); err != nil {
					return err
				}
				for _, x := range tc.records {
					payload, err := json.Marshal(map[string]any{
						"ID": x.ID, "Name": x.Name, "Slug": x.Slug, "Owner": x.Owner,
						"Repo": x.Repo, "Ref": x.Ref, "Visibility": x.Visibility,
						"Listed": x.Listed, "Commit": x.Commit, "Created": x.Created, "Published": x.Published,
					})
					if err != nil {
						return err
					}
					if _, err := tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload) VALUES(?,?,?,?,?)", x.ID, x.Name, x.Slug, x.Owner, string(payload)); err != nil {
						return err
					}
				}
				if tc.apexID != "" {
					_, err := tx.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('apex',?)", tc.apexID)
					return err
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := legacy.Close(); err != nil {
				t.Fatal(err)
			}
			d, err := db.Open(ctx, db.Config{Path: path, Migrations: sites.Migrations(), Now: clock})
			if err != nil || d == nil {
				t.Fatalf("adopt catalog: handle=%v, error=%v", d, err)
			}
			t.Cleanup(func() { _ = d.Close() })
			s := store.New(d, store.Config{Now: clock, Rand: &counter{}})
			for _, owner := range []string{"alice", "bob", "nobody"} {
				want := make(map[string]store.Site)
				for _, x := range tc.records {
					if x.Owner == owner {
						want[x.ID] = x
					}
				}
				got, err := s.List(ctx, owner)
				if err != nil {
					t.Fatal(err)
				}
				equal(t, len(got), len(want))
				for _, x := range got {
					w, ok := want[x.ID]
					if !ok {
						t.Fatalf("unexpected or duplicate site for %s: %s", owner, x.ID)
					}
					assertLegacySite(t, x, w)
					delete(want, x.ID)
				}
			}
			apex, set, err := s.Apex(ctx)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, set, tc.apexID != "")
			if set {
				for _, x := range tc.records {
					if x.ID == tc.apexID {
						assertLegacySite(t, apex, x)
					}
				}
			}
		})
	}
}

func assertLegacySite(t *testing.T, got, want store.Site) {
	t.Helper()
	if !got.Created.Equal(want.Created) || !got.Published.Equal(want.Published) {
		t.Fatalf("adopted times for %s: got %v, %v; want %v, %v", want.ID, got.Created, got.Published, want.Created, want.Published)
	}
	got.Created, got.Published = want.Created, want.Published
	equal(t, got, want)
}
