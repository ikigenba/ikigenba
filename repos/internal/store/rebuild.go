package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const createdLayout = "2006-01-02T15:04:05Z"

func (s *Store) identified(ctx context.Context, entry os.DirEntry) (Repo, bool) {
	id := strings.TrimSuffix(entry.Name(), ".git")
	if !strings.HasSuffix(entry.Name(), ".git") || !ValidID(id) {
		return Repo{}, false
	}
	dir := filepath.Join(s.cfg.Root, entry.Name())
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return Repo{}, false
	}
	config, err := filepath.Abs(filepath.Join(dir, "config"))
	if err != nil {
		return Repo{}, false
	}
	values := make([]string, 4)
	for i, key := range []string{"id", "name", "owner", "created"} {
		output, err := s.cfg.Git.Output(ctx, "/", "config", "--file", config, "--get", "ikigenba."+key)
		if err != nil || !strings.HasSuffix(string(output), "\n") {
			return Repo{}, false
		}
		values[i] = strings.TrimSuffix(string(output), "\n")
	}
	created, err := time.Parse(createdLayout, values[3])
	if values[0] != id || !ValidName(values[1]) || values[2] == "" || err != nil || created.Format(createdLayout) != values[3] {
		return Repo{}, false
	}
	return Repo{ID: id, Name: values[1], Owner: values[2], Created: created.UTC(), Available: true}, true
}

func (s *Store) rebuild(ctx context.Context, tx *sql.Tx) error {
	entries, err := os.ReadDir(s.cfg.Root)
	if err != nil {
		return err
	}
	var repos []Repo
	for _, entry := range entries {
		if r, ok := s.identified(ctx, entry); ok {
			repos = append(repos, r)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
	sort.Slice(repos, func(i, j int) bool {
		if !repos[i].Created.Equal(repos[j].Created) {
			return repos[i].Created.Before(repos[j].Created)
		}
		return repos[i].ID < repos[j].ID
	})
	seen := make(map[[2]string]bool)
	for _, r := range repos {
		key := [2]string{r.Owner, r.Name}
		if seen[key] {
			s.excluded = append(s.excluded, r.ID)
			continue
		}
		seen[key] = true
		if _, err = tx.ExecContext(ctx, "INSERT INTO repos ("+columns+") VALUES(?,?,?,?,?)", r.ID, r.Name, r.Owner, r.Created.Format(createdLayout), r.Available); err != nil {
			return err
		}
	}
	return nil
}
