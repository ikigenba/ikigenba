package store_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/store"
)

func TestConfiguredAndPublishedPaths(t *testing.T) {
	// R-TP9N-N3I1 R-TQHK-0V8Q R-TRPG-EMZF R-TSXC-SEQ4 R-TVD5-JY7I R-TXSY-BHOW R-U1GN-GSWZ R-TU59-66GT R-TWL1-XPY7
	for i, path := range []string{"", "docs", " odd/../path\x00\\é"} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			now := stamp
			filename := filepath.Join(t.TempDir(), "db")
			s, handle := openAt(t, filename, store.Config{Now: func() time.Time { return now }})
			d := draft("paths")
			d.Path = path
			x := create(t, s, d)
			equal(t, x.Path, path)
			equal(t, x.PublishedPath, "")
			equal(t, x.Commit, "")
			equal(t, x.Published, time.Time{})
			assertStoredRecord(t, handle, x)
			_, err := s.SetApex(ctx, x.ID)
			equal(t, err, nil)
			for _, published := range []string{"other", "", path, path} {
				now = now.Add(time.Second)
				previous := x
				x, err = s.Publish(ctx, x.ID, strings.Repeat("a", 40), published)
				equal(t, err, nil)
				previous.Commit = strings.Repeat("a", 40)
				previous.PublishedPath = published
				previous.Published = now.UTC().Truncate(time.Second)
				equal(t, x, previous)
				assertStoredRecord(t, handle, x)
				for _, next := range []string{path, "changed", "changed", ""} {
					previous = x
					var fields []string
					x, fields, err = s.Update(ctx, x.ID, store.Change{Path: &next})
					equal(t, err, nil)
					want := []string{}
					if previous.Path != next {
						want = append(want, "path")
					}
					equal(t, fields, want)
					previous.Path = next
					equal(t, x, previous)
					assertStoredRecord(t, handle, x)
				}
				got, err := s.Find(ctx, x.Owner, x.Name)
				equal(t, err, nil)
				equal(t, got, x)
				got, err = s.BySlug(ctx, x.Slug)
				equal(t, err, nil)
				equal(t, got, x)
				rows, err := s.List(ctx, x.Owner, "", 2)
				equal(t, err, nil)
				equal(t, rows, []store.Site{x})
				rows, err = s.Visible(ctx, x.Owner, "", 2)
				equal(t, err, nil)
				equal(t, rows, []store.Site{x})
				got, set, err := s.Apex(ctx)
				equal(t, err, nil)
				equal(t, set, true)
				equal(t, got, x)
			}
			visibility, listed, ref, path := store.Private, false, "next", "final"
			previous := x
			equal(t, s.ClearApex(ctx), nil)
			x, fields, err := s.Update(ctx, x.ID, store.Change{Visibility: &visibility, Listed: &listed, Ref: &ref, Path: &path})
			equal(t, err, nil)
			equal(t, fields, []string{"visibility", "listed", "ref", "path"})
			previous.Visibility, previous.Listed, previous.Ref, previous.Path = visibility, listed, ref, path
			equal(t, x, previous)
			before := snapshot(t, s)
			equal(t, handle.Close(), nil)
			s, _ = openAt(t, filename, store.Config{})
			equal(t, snapshot(t, s), before)
		})
	}
}

func TestPayloadCompatibility(t *testing.T) {
	// R-H4BE-JRMO R-H5JA-XJDD
	for _, missing := range []string{"Path", "PublishedPath", "both", "neither"} {
		for _, extra := range []bool{false, true} {
			t.Run(fmt.Sprint(missing, extra), func(t *testing.T) {
				s, handle := openAt(t, filepath.Join(t.TempDir(), "db"), store.Config{})
				x := create(t, s, draft("compat"))
				var err error
				x, err = s.Publish(ctx, x.ID, strings.Repeat("b", 40), "published")
				equal(t, err, nil)
				path := "configured"
				x, _, err = s.Update(ctx, x.ID, store.Change{Path: &path})
				equal(t, err, nil)
				_, err = s.SetApex(ctx, x.ID)
				equal(t, err, nil)
				payload, err := json.Marshal(x)
				equal(t, err, nil)
				var members map[string]any
				equal(t, json.Unmarshal(payload, &members), nil)
				if missing == "Path" || missing == "both" {
					delete(members, "Path")
					x.Path = ""
				}
				if missing == "PublishedPath" || missing == "both" {
					delete(members, "PublishedPath")
					x.PublishedPath = ""
				}
				if extra {
					members["FutureField"] = map[string]any{"nested": []any{true, 42, "value"}}
				}
				payload, err = json.Marshal(members)
				equal(t, err, nil)
				equal(t, handle.Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "UPDATE sites SET payload=? WHERE id=?", string(payload), x.ID)
					return err
				}), nil)
				got, err := s.Find(ctx, x.Owner, x.Name)
				equal(t, err, nil)
				equal(t, got, x)
				got, err = s.BySlug(ctx, x.Slug)
				equal(t, err, nil)
				equal(t, got, x)
				for _, read := range []func() ([]store.Site, error){func() ([]store.Site, error) { return s.List(ctx, x.Owner, "", 2) }, func() ([]store.Site, error) { return s.Visible(ctx, x.Owner, "", 2) }, func() ([]store.Site, error) { return s.VisibleBefore(ctx, x.Owner, "", 2) }} {
					rows, err := read()
					equal(t, err, nil)
					equal(t, rows, []store.Site{x})
				}
				got, set, err := s.Apex(ctx)
				equal(t, err, nil)
				equal(t, set, true)
				equal(t, got, x)
				got, err = s.SetApex(ctx, x.ID)
				equal(t, err, nil)
				equal(t, got, x)
				got, _, err = s.Update(ctx, x.ID, store.Change{})
				equal(t, err, nil)
				equal(t, got, x)
				got, err = s.Publish(ctx, x.ID, x.Commit, x.PublishedPath)
				equal(t, err, nil)
				equal(t, got, x)
				_, err = s.Delete(ctx, x.ID)
				equal(t, err, nil)
			})
		}
	}
}
