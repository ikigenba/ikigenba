package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/ikigenba/ikigenba/sessions/internal/figures"
)

func summarizeIn(ctx context.Context, tx *sql.Tx, key figures.SessionKey, rules []figures.Rule, prices []figures.Price, away time.Duration) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.tool,s.id,s.parent_tool,s.parent_id,s.host,s.directory,s.command,s.headless,t.moment,t.whose,t.model,t.input,t.cached,t.output,t.reasoning FROM sessions s JOIN turns t ON t.tool=s.tool AND t.id=s.id WHERE s.root_tool=? AND s.root_id=? ORDER BY s.tool,s.id,t.moment,CASE t.whose WHEN 'person' THEN 0 WHEN 'agent' THEN 1 ELSE 2 END`, key.Tool, key.ID)
	if err != nil {
		return err
	}
	var root figures.Session
	subs := make([]figures.Session, 0)
	var current figures.Session
	have := false
	flush := func() {
		if !have {
			return
		}
		if current.Key == key {
			root = current
		} else {
			subs = append(subs, current)
		}
	}
	for rows.Next() {
		var k, p figures.SessionKey
		var host, dir, cmd, model string
		var headless bool
		var ms int64
		var who figures.Whose
		var tokens figures.Tokens
		if err = rows.Scan(&k.Tool, &k.ID, &p.Tool, &p.ID, &host, &dir, &cmd, &headless, &ms, &who, &model, &tokens.Input, &tokens.Cached, &tokens.Output, &tokens.Reasoning); err != nil {
			_ = rows.Close()
			return err
		}
		if !have || k != current.Key {
			flush()
			current = figures.Session{Key: k, Parent: p, Host: host, Directory: dir, Command: cmd, Headless: headless}
			have = true
		}
		current.Turns = append(current.Turns, figures.Turn{Moment: instant(ms), Whose: who, Model: model, Tokens: tokens})
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	flush()
	if len(root.Turns) == 0 {
		return nil
	}
	sum := figures.Summarize(root, subs, rules, prices, away)
	first, last := root.Turns[0].Moment, root.Turns[len(root.Turns)-1].Moment
	var person string
	if err = tx.QueryRowContext(ctx, "SELECT person FROM sessions WHERE tool=? AND id=?", key.Tool, key.ID).Scan(&person); err != nil {
		return err
	}
	f := sum.Figures
	_, err = tx.ExecContext(ctx, `INSERT INTO summaries(tool,id,first,last,person,host,command,kind,attention,wall,agent,input,cached,output,reasoning,dollars,unpriced,subagents) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tool,id) DO UPDATE SET first=excluded.first,last=excluded.last,person=excluded.person,host=excluded.host,command=excluded.command,kind=excluded.kind,attention=excluded.attention,wall=excluded.wall,agent=excluded.agent,input=excluded.input,cached=excluded.cached,output=excluded.output,reasoning=excluded.reasoning,dollars=excluded.dollars,unpriced=excluded.unpriced,subagents=excluded.subagents`, key.Tool, key.ID, first.UnixMilli(), last.UnixMilli(), person, root.Host, root.Command, sum.Kind, int64(f.Attention), int64(f.Wall), int64(f.Agent), f.Tokens.Input, f.Tokens.Cached, f.Tokens.Output, f.Tokens.Reasoning, f.Dollars.String(), f.Unpriced, sum.Subagents)
	return err
}
func settingsIn(ctx context.Context, tx *sql.Tx) ([]figures.Rule, []figures.Price, time.Duration, error) {
	r, e := rulesIn(ctx, tx)
	if e != nil {
		return nil, nil, 0, e
	}
	p, e := pricesIn(ctx, tx)
	if e != nil {
		return nil, nil, 0, e
	}
	a, e := awayIn(ctx, tx)
	return r, p, a, e
}
func (s *Store) refresh(ctx context.Context) (int, error) {
	var pending bool
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		var a, b sql.NullString
		if e := tx.QueryRowContext(ctx, "SELECT refresh_after_tool,refresh_after_id FROM settings WHERE id=1").Scan(&a, &b); e != nil {
			return e
		}
		pending = a.Valid && b.Valid
		return nil
	})
	if err != nil {
		return 0, inaccessible(err)
	}
	if !pending {
		return 0, nil
	}
	// Always restart at the first root: a persisted mark may describe an earlier configuration.
	var after figures.SessionKey
	total := 0
	for {
		count := 0
		err = s.d.Write(ctx, func(tx *sql.Tx) error {
			rows, e := tx.QueryContext(ctx, "SELECT tool,id FROM sessions WHERE tool=root_tool AND id=root_id AND (tool,id)>(?,?) ORDER BY tool,id LIMIT ?", after.Tool, after.ID, BatchSize)
			if e != nil {
				return e
			}
			keys := make([]figures.SessionKey, 0, BatchSize)
			for rows.Next() {
				var k figures.SessionKey
				if e = rows.Scan(&k.Tool, &k.ID); e != nil {
					_ = rows.Close()
					return e
				}
				keys = append(keys, k)
			}
			e = rows.Err()
			ce := rows.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
			r, p, a, e := settingsIn(ctx, tx)
			if e != nil {
				return e
			}
			for _, k := range keys {
				if e = summarizeIn(ctx, tx, k, r, p, a); e != nil {
					return e
				}
				after = k
			}
			count = len(keys)
			if count < BatchSize {
				_, e = tx.ExecContext(ctx, "UPDATE settings SET refresh_after_tool=NULL,refresh_after_id=NULL WHERE id=1")
			} else {
				_, e = tx.ExecContext(ctx, "UPDATE settings SET refresh_after_tool=?,refresh_after_id=? WHERE id=1", after.Tool, after.ID)
			}
			return e
		})
		if err != nil {
			return 0, inaccessible(err)
		}
		total += count
		if count < BatchSize {
			return total, nil
		}
	}
}
