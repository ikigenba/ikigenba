package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/sessions/internal/figures"
)

type turnKey struct {
	key    figures.SessionKey
	moment int64
	whose  figures.Whose
}
type turnValue struct {
	model  string
	tokens figures.Tokens
}

func keyOf(t, id string) figures.SessionKey { return figures.SessionKey{Tool: figures.Tool(t), ID: id} }
func parentOf(p *SessionRef) figures.SessionKey {
	if p == nil {
		return figures.SessionKey{}
	}
	return keyOf(p.Tool, p.ID)
}
func validatePush(entries []PushSession) error {
	if len(entries) > MaxPushSessions {
		return refused(TooManySessions)
	}
	n := 0
	for _, e := range entries {
		n += len(e.Turns)
	}
	if n > MaxPushTurns {
		return refused(TooManyTurns)
	}
	for _, e := range entries {
		if _, ok := figures.ParseTool(e.Tool); !ok {
			return refused(UnknownTool)
		}
		if e.ID == "" {
			return refused(MissingID)
		}
		if e.Parent != nil {
			_, ok := figures.ParseTool(e.Parent.Tool)
			if !ok || e.Parent.ID == "" {
				return refused(BadParent)
			}
			if *e.Parent == (SessionRef{Tool: e.Tool, ID: e.ID}) {
				return refused(OwnParent)
			}
		}
		if e.Host == "" {
			return refused(MissingHost)
		}
		if !validDirectory(e.Directory) {
			return refused(BadDirectory)
		}
		for _, t := range e.Turns {
			m, err := time.Parse(time.RFC3339Nano, t.Moment)
			if err != nil || m.Before(time.Unix(0, 0)) {
				return refused(BadMoment)
			}
			if _, ok := figures.ParseWhose(t.Whose); !ok {
				return refused(UnknownWhose)
			}
			for _, n := range []int64{t.Input, t.Cached, t.Output, t.Reasoning} {
				if n < 0 || n > MaxTurnCount {
					return refused(BadCount)
				}
			}
		}
	}
	return nil
}
func parentsIn(ctx context.Context, tx *sql.Tx) (map[figures.SessionKey]figures.SessionKey, error) {
	rows, err := tx.QueryContext(ctx, "SELECT tool,id,parent_tool,parent_id FROM sessions")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	p := make(map[figures.SessionKey]figures.SessionKey)
	for rows.Next() {
		var k, v figures.SessionKey
		if err = rows.Scan(&k.Tool, &k.ID, &v.Tool, &v.ID); err != nil {
			return nil, err
		}
		p[k] = v
	}
	return p, rows.Err()
}
func closesCycle(k, p figures.SessionKey, parents map[figures.SessionKey]figures.SessionKey) bool {
	seen := make(map[figures.SessionKey]bool)
	for p != (figures.SessionKey{}) {
		if p == k {
			return true
		}
		if seen[p] {
			return false
		}
		seen[p] = true
		next, ok := parents[p]
		if !ok {
			return false
		}
		p = next
	}
	return false
}
func latestIn(ctx context.Context, tx *sql.Tx, refs []SessionRef) ([]LatestTurn, error) {
	out := make([]LatestTurn, 0, len(refs))
	for _, r := range refs {
		var ms sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT max(moment) FROM turns WHERE tool=? AND id=?", r.Tool, r.ID).Scan(&ms); err != nil {
			return nil, err
		}
		v := LatestTurn{Tool: r.Tool, ID: r.ID}
		if ms.Valid {
			v.Latest = instant(ms.Int64)
		}
		out = append(out, v)
	}
	return out, nil
}

// Push validates and keeps turns and their summaries in one transaction.
func (s *Store) Push(ctx context.Context, userID, email string, entries []PushSession) (Pushed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Pushed{}, err
	}
	if userID == "" {
		return Pushed{}, ErrUnreachable
	}
	if err := validatePush(entries); err != nil {
		return Pushed{}, err
	}
	out := Pushed{}
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		for _, e := range entries {
			var owner string
			err := tx.QueryRowContext(ctx, "SELECT user_id FROM sessions WHERE tool=? AND id=?", e.Tool, e.ID).Scan(&owner)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && owner != userID {
				return refused(OtherOwner)
			}
		}
		parents, err := parentsIn(ctx, tx)
		if err != nil {
			return err
		}
		oldRoots := make(map[figures.SessionKey]figures.SessionKey)
		for k := range parents {
			oldRoots[k] = figures.Root(k, parents)
		}
		before := make(map[turnKey]*turnValue)
		final := make(map[turnKey]turnValue)
		touched := make(map[figures.SessionKey]bool)
		for _, e := range entries {
			k := keyOf(e.Tool, e.ID)
			if len(e.Turns) == 0 {
				continue
			}
			touched[k] = true
			p := parentOf(e.Parent)
			if closesCycle(k, p, parents) {
				p = figures.SessionKey{}
			}
			if _, held := parents[k]; !held {
				cmd := e.Command
				if cmd.Name == "" {
					cmd = Command{}
				}
				_, err = tx.ExecContext(ctx, "INSERT INTO sessions(tool,id,parent_tool,parent_id,root_tool,root_id,user_id,person,host,directory,headless,command,argument) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", e.Tool, e.ID, p.Tool, p.ID, e.Tool, e.ID, userID, email, e.Host, e.Directory, e.Headless, cmd.Name, cmd.Argument)
				if err != nil {
					return err
				}
				parents[k] = p
			} else {
				if parents[k] == (figures.SessionKey{}) && e.Parent != nil {
					_, err = tx.ExecContext(ctx, "UPDATE sessions SET parent_tool=?,parent_id=? WHERE tool=? AND id=?", p.Tool, p.ID, k.Tool, k.ID)
					if err != nil {
						return err
					}
					parents[k] = p
				}
				if e.Command.Name != "" {
					_, err = tx.ExecContext(ctx, "UPDATE sessions SET command=?,argument=? WHERE tool=? AND id=? AND command=''", e.Command.Name, e.Command.Argument, k.Tool, k.ID)
					if err != nil {
						return err
					}
				}
			}
			for _, t := range e.Turns {
				m, _ := time.Parse(time.RFC3339Nano, t.Moment)
				tk := turnKey{k, m.UnixMilli(), figures.Whose(t.Whose)}
				v := turnValue{t.Model, figures.Tokens{Input: t.Input, Cached: t.Cached, Output: t.Output, Reasoning: t.Reasoning}}
				if _, ok := before[tk]; !ok {
					var old turnValue
					err = tx.QueryRowContext(ctx, "SELECT model,input,cached,output,reasoning FROM turns WHERE tool=? AND id=? AND moment=? AND whose=?", k.Tool, k.ID, tk.moment, tk.whose).Scan(&old.model, &old.tokens.Input, &old.tokens.Cached, &old.tokens.Output, &old.tokens.Reasoning)
					switch {
					case errors.Is(err, sql.ErrNoRows):
						before[tk] = nil
					case err != nil:
						return err
					default:
						before[tk] = &old
					}
				}
				final[tk] = v
			}
		}
		for k, v := range final {
			old := before[k]
			if old == nil {
				out.New++
			} else if *old != v {
				out.Changed++
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO turns(tool,id,moment,whose,model,input,cached,output,reasoning) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(tool,id,moment,whose) DO UPDATE SET model=excluded.model,input=excluded.input,cached=excluded.cached,output=excluded.output,reasoning=excluded.reasoning", k.key.Tool, k.key.ID, k.moment, k.whose, v.model, v.tokens.Input, v.tokens.Cached, v.tokens.Output, v.tokens.Reasoning)
			if err != nil {
				return err
			}
		}
		refresh := make(map[figures.SessionKey]bool)
		counted := make(map[figures.SessionKey]bool)
		for k := range parents {
			root := figures.Root(k, parents)
			if old, ok := oldRoots[k]; !ok || old != root {
				refresh[root] = true
			}
			_, err = tx.ExecContext(ctx, "UPDATE sessions SET root_tool=?,root_id=? WHERE tool=? AND id=?", root.Tool, root.ID, k.Tool, k.ID)
			if err != nil {
				return err
			}
			if touched[k] {
				refresh[root] = true
				counted[root] = true
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM summaries WHERE NOT EXISTS (SELECT 1 FROM sessions s WHERE s.tool=summaries.tool AND s.id=summaries.id AND s.tool=s.root_tool AND s.id=s.root_id)"); err != nil {
			return err
		}
		r, p, a, err := settingsIn(ctx, tx)
		if err != nil {
			return err
		}
		for k := range refresh {
			if err = summarizeIn(ctx, tx, k, r, p, a); err != nil {
				return err
			}
		}
		out.Refreshed = len(counted)
		refs := make([]SessionRef, 0, len(entries))
		for _, e := range entries {
			refs = append(refs, SessionRef{e.Tool, e.ID})
		}
		out.Sessions, err = latestIn(ctx, tx, refs)
		return err
	})
	if err != nil {
		return Pushed{}, inaccessible(err)
	}
	return out, nil
}

// Latest reports held moments in the order of the supplied references.
func (s *Store) Latest(ctx context.Context, refs []SessionRef) ([]LatestTurn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, refused(NoSessions)
	}
	if len(refs) > MaxLatestSessions {
		return nil, refused(TooManySessions)
	}
	for _, r := range refs {
		if _, ok := figures.ParseTool(r.Tool); !ok {
			return nil, refused(UnknownTool)
		}
		if r.ID == "" {
			return nil, refused(MissingID)
		}
	}
	var out []LatestTurn
	err := s.d.Read(ctx, func(tx *sql.Tx) error { var e error; out, e = latestIn(ctx, tx, refs); return e })
	if err != nil {
		return nil, inaccessible(err)
	}
	return out, nil
}
