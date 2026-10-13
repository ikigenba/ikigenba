package store_test

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

func TestListWindows(t *testing.T) {
	// R-4K8H-0X5P R-4GKR-VLXM
	s := open(t, store.Config{})
	var records []store.Site
	for _, name := range []string{"aa", "a1", "a-b", "z"} {
		records = append(records, create(t, s, draft(name)))
	}
	other := draft("a0")
	other.Owner = "bob"
	create(t, s, other)
	sorted := []store.Site{records[2], records[1], records[0], records[3]}
	for _, tc := range []struct {
		after string
		n     int
		want  []store.Site
	}{
		{"", 1, sorted[:1]}, {"", 4, sorted}, {"", 8, sorted}, {"-", 8, sorted}, {"a-b", 2, sorted[1:3]}, {"a0", 8, sorted[1:]}, {"aa", 8, sorted[3:]}, {"z", 8, []store.Site{}}, {string([]byte{255}), 8, []store.Site{}}, {"", 0, []store.Site{}}, {"", -1, []store.Site{}},
	} {
		got, err := s.List(ctx, "alice", tc.after, tc.n)
		equal(t, err, nil)
		equal(t, got, tc.want)
	}
	got, err := s.List(ctx, "bob", "", 8)
	equal(t, err, nil)
	equal(t, len(got), 1)
	equal(t, got[0].Name, other.Name)
	got, err = s.List(ctx, "", "", 8)
	equal(t, err, nil)
	equal(t, got, []store.Site{})
}

func TestVisibleWindows(t *testing.T) {
	// R-4LGD-EOWE R-4MO9-SGN3
	s := open(t, store.Config{})
	var rows []store.Site
	for i, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		d := draft(name)
		if i%2 != 0 {
			d.Owner = "bob"
		}
		d.Listed = i%4 < 2
		if i%3 == 0 {
			d.Visibility = store.Private
		}
		rows = append(rows, create(t, s, d))
	}
	for _, user := range []string{"alice", "bob", "", "nobody"} {
		want := []store.Site{}
		for _, x := range rows {
			if x.Listed || x.Owner == user {
				want = append(want, x)
			}
		}
		for _, n := range []int{-2, 0, 1, 2, len(want), len(want) + 1} {
			end := min(max(n, 0), len(want))
			got, err := s.Visible(ctx, user, n)
			equal(t, err, nil)
			equal(t, got, want[:end])
		}
	}
}

func TestListedBackfill(t *testing.T) {
	// R-4E4Z-42G8
	first, err := fs.ReadFile(sites.Migrations(), "0001_catalog.sql")
	equal(t, err, nil)
	for _, untracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "tracked", true: "untracked"}[untracked], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sites.db")
			clock := func() time.Time { return stamp }
			d, err := db.Open(ctx, db.Config{Path: path, Migrations: fstest.MapFS{"0001_catalog.sql": &fstest.MapFile{Data: first}}, Now: clock})
			equal(t, err, nil)
			t.Cleanup(func() { _ = d.Close() })
			payloads := []string{`{"Listed":true}`, `{"Listed":false}`, `{}`}
			err = d.Write(ctx, func(tx *sql.Tx) error {
				for i, payload := range payloads {
					if _, err := tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload) VALUES(?,?,?,?,?)", i, i, i, "owner", payload); err != nil {
						return err
					}
				}
				if untracked {
					_, err := tx.ExecContext(ctx, "DROP TABLE schema_migrations")
					return err
				}
				return nil
			})
			equal(t, err, nil)
			equal(t, d.Close(), nil)
			d, err = db.Open(ctx, db.Config{Path: path, Migrations: sites.Migrations(), Now: clock})
			equal(t, err, nil)
			t.Cleanup(func() { _ = d.Close() })
			err = d.Read(ctx, func(tx *sql.Tx) error {
				for i, want := range payloads {
					var id, name, slug, owner, payload string
					var listed int
					if err := tx.QueryRowContext(ctx, "SELECT id,name,slug,owner,payload,listed FROM sites WHERE id=?", i).Scan(&id, &name, &slug, &owner, &payload, &listed); err != nil {
						return err
					}
					key := string(rune('0' + i))
					equal(t, []string{id, name, slug, owner, payload}, []string{key, key, key, "owner", want})
					equal(t, listed, map[bool]int{false: 0, true: 1}[i == 0])
				}
				return nil
			})
			equal(t, err, nil)
		})
	}
}

func assertStoredRecord(t *testing.T, d *db.DB, x store.Site) {
	t.Helper()
	err := d.Read(ctx, func(tx *sql.Tx) error {
		var listed int
		var payload string
		if err := tx.QueryRowContext(ctx, "SELECT listed,payload FROM sites WHERE id=?", x.ID).Scan(&listed, &payload); err != nil {
			return err
		}
		equal(t, listed, map[bool]int{false: 0, true: 1}[x.Listed])
		var got map[string]any
		if err := json.Unmarshal([]byte(payload), &got); err != nil {
			return err
		}
		created, err := x.Created.MarshalJSON()
		if err != nil {
			return err
		}
		published, err := x.Published.MarshalJSON()
		if err != nil {
			return err
		}
		var c, p string
		if err := json.Unmarshal(created, &c); err != nil {
			return err
		}
		if err := json.Unmarshal(published, &p); err != nil {
			return err
		}
		equal(t, got, map[string]any{"ID": x.ID, "Name": x.Name, "Slug": x.Slug, "Owner": x.Owner, "Repo": x.Repo, "Ref": x.Ref, "Visibility": x.Visibility, "Commit": x.Commit, "Listed": x.Listed, "Created": c, "Published": p})
		return nil
	})
	equal(t, err, nil)
}

func TestStoredListedAndPayload(t *testing.T) {
	// R-4NW6-68DS R-V51S-AZ1A
	s, d := openAt(t, filepath.Join(t.TempDir(), "sites.db"), store.Config{})
	for _, listed := range []bool{true, false} {
		draft := draft(map[bool]string{true: "listed", false: "unlisted"}[listed])
		draft.Listed = listed
		x := create(t, s, draft)
		assertStoredRecord(t, d, x)
		var err error
		x, err = s.Publish(ctx, x.ID, strings.Repeat("b", 40))
		equal(t, err, nil)
		assertStoredRecord(t, d, x)
		for _, next := range []bool{!listed, listed} {
			x, _, err = s.Update(ctx, x.ID, store.Change{Listed: &next})
			equal(t, err, nil)
			assertStoredRecord(t, d, x)
		}
	}
}

func TestPayloadListingWinsDuringMixedRelease(t *testing.T) {
	// R-4P42-K04H
	for _, listed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unlisted", true: "listed"}[listed], func(t *testing.T) {
			s, d := openAt(t, filepath.Join(t.TempDir(), "sites.db"), store.Config{})
			x := create(t, s, draft("mixed"))
			x.Listed = listed
			payload, err := json.Marshal(x)
			equal(t, err, nil)
			err = d.Write(ctx, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, "DELETE FROM sites WHERE id=?", x.ID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload) VALUES(?,?,?,?,?)", x.ID, x.Name, x.Slug, x.Owner, string(payload)); err != nil {
					return err
				}
				_, err := tx.ExecContext(ctx, "UPDATE sites SET listed=?,payload=? WHERE id=?", !listed, string(payload), x.ID)
				return err
			})
			equal(t, err, nil)
			got, err := s.Find(ctx, x.Owner, x.Name)
			equal(t, err, nil)
			equal(t, got, x)
			got, err = s.BySlug(ctx, x.Slug)
			equal(t, err, nil)
			equal(t, got, x)
			rows, err := s.List(ctx, x.Owner, "", 8)
			equal(t, err, nil)
			equal(t, rows, []store.Site{x})
			for _, user := range []string{x.Owner, "other"} {
				rows, err = s.Visible(ctx, user, 8)
				equal(t, err, nil)
				want := []store.Site{}
				if listed || user == x.Owner {
					want = append(want, x)
				}
				equal(t, rows, want)
			}
			got, err = s.SetApex(ctx, x.ID)
			equal(t, err, nil)
			equal(t, got, x)
			got, set, err := s.Apex(ctx)
			equal(t, err, nil)
			equal(t, set, true)
			equal(t, got, x)
			got, _, err = s.Update(ctx, x.ID, store.Change{})
			equal(t, err, nil)
			equal(t, got, x)
			assertStoredRecord(t, d, got)
			err = d.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "UPDATE sites SET listed=? WHERE id=?", !listed, x.ID)
				return err
			})
			equal(t, err, nil)
			got, err = s.Publish(ctx, x.ID, strings.Repeat("a", 40))
			equal(t, err, nil)
			equal(t, got.Listed, x.Listed)
			assertStoredRecord(t, d, got)
		})
	}
}

func TestReadsDecodeOnlyTheirWindow(t *testing.T) {
	s, d := openAt(t, filepath.Join(t.TempDir(), "sites.db"), store.Config{})
	x := create(t, s, draft("a"))
	err := d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload) VALUES(?,?,?,?,?)", "bad", "z", "z", "bob", "invalid-json")
		return err
	})
	equal(t, err, nil)
	got, err := s.Find(ctx, x.Owner, x.Name)
	equal(t, err, nil)
	equal(t, got, x)
	got, err = s.BySlug(ctx, x.Slug)
	equal(t, err, nil)
	equal(t, got, x)
	rows, err := s.List(ctx, x.Owner, "", 1)
	equal(t, err, nil)
	equal(t, rows, []store.Site{x})
	rows, err = s.Visible(ctx, "other", 1)
	equal(t, err, nil)
	equal(t, rows, []store.Site{x})
}
