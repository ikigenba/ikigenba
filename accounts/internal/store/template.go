package store

import (
	"context"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// TemplateSettings identifies the git repository and ref to seed new folders.
type TemplateSettings struct{ URL, Ref string }

// DefaultTemplateSettings returns the team template setting for a new record.
func DefaultTemplateSettings() TemplateSettings {
	return TemplateSettings{protocol.DefaultTemplateURL, protocol.DefaultTemplateRef}
}

func readTemplateSettings(q query) (TemplateSettings, error) {
	t := DefaultTemplateSettings()
	err := q.row("SELECT url, ref FROM template_settings WHERE scope = ''").Scan(&t.URL, &t.Ref)
	return t, ignoreNoRows(err)
}

// TemplateSettings reads the team template settings.
func (s *recordReader) TemplateSettings(ctx context.Context) (TemplateSettings, error) {
	return read(ctx, s, true, readTemplateSettings)
}

type templateChange struct {
	settings TemplateSettings
	changed  bool
}

// SetTemplateSettings replaces the provided settings without changing desired-state versions.
func (s *Store) SetTemplateSettings(ctx context.Context, url, ref *string) (TemplateSettings, bool, error) {
	out, err := write(ctx, s, true, func(w *writing) (templateChange, error) {
		previous, err := readTemplateSettings(w.query)
		if err != nil {
			return templateChange{}, err
		}
		next := previous
		if url != nil {
			next.URL = *url
		}
		if ref != nil {
			next.Ref = *ref
		}
		if valid.TemplateURL(next.URL) != nil || valid.TemplateRef(next.Ref) != nil {
			return templateChange{}, ErrUnreachable
		}
		out := templateChange{settings: next, changed: next != previous}
		if !out.changed {
			return out, nil
		}
		if err := w.exec("DELETE FROM template_settings WHERE scope <> ''"); err != nil {
			return templateChange{}, err
		}
		return out, w.exec(`INSERT INTO template_settings (scope, url, ref) VALUES ('', ?, ?)
			ON CONFLICT (scope) DO UPDATE SET url = excluded.url, ref = excluded.ref`, next.URL, next.Ref)
	})
	return out.settings, out.changed, err
}

func recordTemplateCommits(q query, host string, r protocol.Report) error {
	for _, seed := range r.Seeds {
		if protocol.ValidTemplateCommit(seed.Commit) {
			if err := q.exec("UPDATE accounts SET template = ? WHERE host = ? AND gid = ? AND template = ''",
				seed.Commit, host, seed.GID); err != nil {
				return err
			}
		}
	}
	for _, home := range r.Homes {
		if protocol.ValidTemplateCommit(home.Commit) {
			if err := q.exec(`UPDATE ssh_users SET template = ? WHERE uid = ? AND template = ''
				AND account IN (SELECT slug FROM accounts WHERE host = ?)`, home.Commit, home.UID, host); err != nil {
				return err
			}
		}
	}
	return nil
}
