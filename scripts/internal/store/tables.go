package store

import (
	"context"
	"time"
)

// SyncTables rebuilds the table mirror from the authoritative stored catalog.
func (s *Store) SyncTables(ctx context.Context) error {
	return s.transaction(ctx, true, func(c *catalog) error { return c.mirror(ctx, c.data) })
}

func (s *catalog) mirror(ctx context.Context, c content) error {
	for _, query := range []string{"DELETE FROM runs", "DELETE FROM scripts"} {
		if _, err := s.tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	for _, sc := range c.Scripts {
		if _, err := s.tx.ExecContext(ctx, "INSERT INTO scripts(id,name,owner,repo,ref,created) VALUES(?,?,?,?,?,?)", sc.ID, sc.Name, sc.Owner, sc.Repo, sc.Ref, sc.Created.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	for _, r := range c.Runs {
		var finished any
		if !r.Finished.IsZero() {
			finished = r.Finished.UTC().Format(time.RFC3339)
		}
		truncated := 0
		if r.Truncated {
			truncated = 1
		}
		if _, err := s.tx.ExecContext(ctx, "INSERT INTO runs(id,script,sha,ref,user,request_id,trigger,event,status,exit_code,started,finished,stdout_bytes,stderr_bytes,truncated,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", r.ID, r.Script, r.SHA, r.Ref, r.User, r.RequestID, r.Trigger, r.Event, r.Status, r.ExitCode, r.Started.UTC().Format(time.RFC3339), finished, r.StdoutBytes, r.StderrBytes, truncated, r.Reason); err != nil {
			return err
		}
	}
	return nil
}
