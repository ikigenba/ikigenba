package sites

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"eventplane/consumer"

	sitefiles "sites/internal/files"
)

type pushPayload struct {
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

// NewPushHandler returns the repos:push consumer that keeps served files in
// sync with a site's repository main branch.
func NewPushHandler(store *Store, layout Layout, version VersionClient) consumer.Handler {
	return func(ctx context.Context, ev consumer.Event) error {
		var payload pushPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return fmt.Errorf("decode repos push: %w", err)
		}
		if payload.Branch != "main" {
			return nil
		}

		slug := pushSubjectSlug(ev.Subject)
		site, err := store.Get(ctx, slug)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("unknown pushed site %q: %w", slug, consumer.ErrSkip)
		}
		if err != nil {
			return fmt.Errorf("load pushed site %q: %w", slug, err)
		}
		if payload.SHA == site.RepoSha {
			return nil
		}

		entries, commit, err := version.Export(ctx, slug)
		if err != nil {
			return fmt.Errorf("export pushed site %q: %w", slug, err)
		}
		entries = Subtree(entries, site.Path)
		workingDir := layout.SiteDir(site.Visibility, slug)
		desired := make(map[string][]byte, len(entries))
		for _, entry := range entries {
			if hasGitSegment(entry.Path) {
				return fmt.Errorf("export pushed site %q: forbidden .git path %q", slug, entry.Path)
			}
			if _, err := sitefiles.ConfinePath(workingDir, entry.Path); err != nil {
				return fmt.Errorf("export pushed site %q: invalid path %q: %w", slug, entry.Path, err)
			}
			desired[entry.Path] = entry.Data
		}

		existing, err := regularFilesUnder(workingDir)
		if err != nil {
			return fmt.Errorf("walk pushed site %q: %w", slug, err)
		}
		if _, _, err := Reconcile(workingDir, desired, existing); err != nil {
			return fmt.Errorf("reconcile pushed site %q: %w", slug, err)
		}
		if err := store.SetRepoSha(ctx, slug, commit.Sha); err != nil {
			return fmt.Errorf("record pushed site %q sha: %w", slug, err)
		}
		return nil
	}
}

func pushSubjectSlug(subject string) string {
	parts := strings.Split(strings.TrimRight(subject, "/"), "/")
	return parts[len(parts)-1]
}

func hasGitSegment(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == ".git" {
			return true
		}
	}
	return false
}

func regularFilesUnder(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return paths, err
}
