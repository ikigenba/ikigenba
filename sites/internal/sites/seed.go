package sites

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Seed brings every unseeded site into the version plane. Sites are processed
// in the deterministic order supplied by Store.ListUnseeded, and successfully
// seeded rows are not revisited by later calls.
func Seed(ctx context.Context, store *Store, layout Layout, vc VersionClient) (int, error) {
	sites, err := store.ListUnseeded(ctx)
	if err != nil {
		return 0, err
	}

	seeded := 0
	for _, site := range sites {
		owner := Owner{ID: site.OwnerID, Email: site.OwnerEmail}
		if err := vc.Create(ctx, site.Slug, owner); err != nil {
			return seeded, fmt.Errorf("create seed repository %q: %w", site.Slug, err)
		}

		changes, err := seedChanges(layout.SiteDir(site.Visibility, site.Slug))
		if err != nil {
			return seeded, fmt.Errorf("read seed tree %q: %w", site.Slug, err)
		}
		if len(changes) > 0 {
			commit, err := vc.Commit(ctx, site.Slug, "seed "+site.Slug, changes)
			if err != nil {
				return seeded, fmt.Errorf("commit seed tree %q: %w", site.Slug, err)
			}
			if err := store.SetRepoSha(ctx, site.Slug, commit.Sha); err != nil {
				return seeded, err
			}
		}
		if err := store.MarkSeeded(ctx, site.Slug); err != nil {
			return seeded, err
		}
		seeded++
	}
	return seeded, nil
}

func seedChanges(root string) ([]FileChange, error) {
	changes := []FileChange{}
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		path, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		changes = append(changes, FileChange{Path: filepath.ToSlash(path), Data: data})
		return nil
	})
	return changes, err
}
