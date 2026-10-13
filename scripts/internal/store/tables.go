package store

import (
	"context"
	"time"
)

func (s *catalog) persist(ctx context.Context, c content) error {
	for id := range s.data.Runs {
		if _, ok := c.Runs[id]; !ok {
			if _, err := s.tx.ExecContext(ctx, "DELETE FROM runs WHERE id=?", id); err != nil {
				return err
			}
		}
	}
	for id := range s.data.Scripts {
		if _, ok := c.Scripts[id]; !ok {
			if _, err := s.tx.ExecContext(ctx, "DELETE FROM scripts WHERE id=?", id); err != nil {
				return err
			}
		}
	}
	for id, sc := range c.Scripts {
		old, ok := s.data.Scripts[id]
		if !ok {
			if _, err := s.tx.ExecContext(ctx, "INSERT INTO scripts(id,name,owner,repo,ref,created) VALUES(?,?,?,?,?,?)", sc.ID, sc.Name, sc.Owner, sc.Repo, sc.Ref, sc.Created.UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		} else if old.Ref != sc.Ref {
			if _, err := s.tx.ExecContext(ctx, "UPDATE scripts SET ref=? WHERE id=?", sc.Ref, sc.ID); err != nil {
				return err
			}
		}
	}
	for id, r := range c.Runs {
		old, ok := s.data.Runs[id]
		if ok && old == r {
			continue
		}
		var finished any
		if !r.Finished.IsZero() {
			finished = r.Finished.UTC().Format(time.RFC3339)
		}
		truncated := 0
		if r.Truncated {
			truncated = 1
		}
		if !ok {
			if _, err := s.tx.ExecContext(ctx, "INSERT INTO runs(id,script,sha,ref,user,request_id,trigger,event,status,exit_code,started,finished,stdout_bytes,stderr_bytes,truncated,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", r.ID, r.Script, r.SHA, r.Ref, r.User, r.RequestID, r.Trigger, r.Event, r.Status, r.ExitCode, r.Started.UTC().Format(time.RFC3339), finished, r.StdoutBytes, r.StderrBytes, truncated, r.Reason); err != nil {
				return err
			}
		} else {
			if _, err := s.tx.ExecContext(ctx, "UPDATE runs SET status=?,exit_code=?,finished=?,stdout_bytes=?,stderr_bytes=?,truncated=?,reason=? WHERE id=?", r.Status, r.ExitCode, finished, r.StdoutBytes, r.StderrBytes, truncated, r.Reason, r.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
