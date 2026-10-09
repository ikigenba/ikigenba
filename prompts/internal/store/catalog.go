package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

const promptColumns = "id,name,owner_id,owner_email,model,prompt,system,tools,schema,created"
const runColumns = "id,prompt,model,user_id,request_id,trigger_kind,event,status,exit_code,started,finished,stdout_bytes,stderr_bytes,stdout_truncated,stderr_truncated,reason,calls,tool_calls,input_tokens,cached_tokens,output_tokens,reasoning_tokens,cost_nanos"

type catalog struct {
	prompts   map[string]Prompt
	runs      map[string]Run
	delivered map[[2]string]bool
}

func load(tx *sql.Tx) (catalog, error) {
	c := catalog{map[string]Prompt{}, map[string]Run{}, map[[2]string]bool{}}
	rows, err := tx.Query("SELECT " + promptColumns + " FROM prompts")
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var p Prompt
		var tools, created string
		var schema sql.NullString
		err = rows.Scan(&p.ID, &p.Name, &p.Owner, &p.OwnerEmail, &p.Model, &p.Prompt, &p.System, &tools, &schema, &created)
		if err != nil {
			break
		}
		if err = json.Unmarshal([]byte(tools), &p.Tools); err != nil {
			break
		}
		p.Created, err = time.Parse(time.RFC3339, created)
		if err != nil {
			break
		}
		if schema.Valid {
			p.Schema = json.RawMessage(schema.String)
		}
		p.Subscriptions = []Subscription{}
		c.prompts[p.ID] = p
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return c, err
	}
	rows, err = tx.Query("SELECT prompt,event,created FROM subscriptions ORDER BY event")
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var id, created string
		var sub Subscription
		err = rows.Scan(&id, &sub.Event, &created)
		if err != nil {
			break
		}
		sub.Created, err = time.Parse(time.RFC3339, created)
		if err != nil {
			break
		}
		p := c.prompts[id]
		p.Subscriptions = append(p.Subscriptions, sub)
		c.prompts[id] = p
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return c, err
	}
	rows, err = tx.Query("SELECT " + runColumns + " FROM runs")
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var r Run
		var started string
		var finished sql.NullString
		err = rows.Scan(&r.ID, &r.Prompt, &r.Model, &r.User, &r.RequestID, &r.Trigger, &r.Event, &r.Status, &r.ExitCode, &started, &finished, &r.StdoutBytes, &r.StderrBytes, &r.StdoutTruncated, &r.StderrTruncated, &r.Reason, &r.Usage.Calls, &r.Usage.ToolCalls, &r.Usage.InputTokens, &r.Usage.CachedTokens, &r.Usage.OutputTokens, &r.Usage.ReasoningTokens, &r.Usage.CostNanos)
		if err != nil {
			break
		}
		r.Started, err = time.Parse(time.RFC3339, started)
		if err != nil {
			break
		}
		if finished.Valid {
			r.Finished, err = time.Parse(time.RFC3339, finished.String)
			if err != nil {
				break
			}
		}
		c.runs[r.ID] = r
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return c, err
	}
	rows, err = tx.Query("SELECT prompt,event FROM event_runs")
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var pair [2]string
		err = rows.Scan(&pair[0], &pair[1])
		if err != nil {
			break
		}
		c.delivered[pair] = true
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return c, err
	}
	for id, p := range c.prompts {
		rs := c.promptRuns(id)
		if len(rs) > 0 {
			r := rs[0]
			p.Last = &r
			c.prompts[id] = p
		}
	}
	return c, nil
}
func (c catalog) promptRuns(id string) []Run {
	rs := []Run{}
	for _, r := range c.runs {
		if r.Prompt == id {
			rs = append(rs, r)
		}
	}
	slices.SortFunc(rs, func(a, b Run) int {
		if a.Started.After(b.Started) {
			return -1
		}
		if a.Started.Before(b.Started) {
			return 1
		}
		return strings.Compare(b.ID, a.ID)
	})
	return rs
}
func (s *Store) read(ctx context.Context, f func(catalog) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.d.Read(ctx, func(tx *sql.Tx) error {
		c, err := load(tx)
		if err != nil {
			return err
		}
		return f(c)
	})
}
func (s *Store) write(ctx context.Context, f func(*sql.Tx, catalog) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.d.Write(ctx, func(tx *sql.Tx) error {
		c, err := load(tx)
		if err != nil {
			return err
		}
		return f(tx, c)
	})
}

// Find finds an owner's prompt by name.
func (s *Store) Find(ctx context.Context, owner, name string) (p Prompt, err error) {
	err = s.read(ctx, func(c catalog) error {
		for _, v := range c.prompts {
			if v.Owner == owner && v.Name == name {
				p = v
				return nil
			}
		}
		return ErrNotFound
	})
	return
}

// List lists an owner's prompts in name order.
func (s *Store) List(ctx context.Context, owner string) (ps []Prompt, err error) {
	ps = []Prompt{}
	err = s.read(ctx, func(c catalog) error {
		for _, p := range c.prompts {
			if p.Owner == owner {
				ps = append(ps, p)
			}
		}
		slices.SortFunc(ps, func(a, b Prompt) int { return strings.Compare(a.Name, b.Name) })
		return nil
	})
	if err != nil {
		ps = nil
	}
	return
}

// Taken reports whether a prompt name is in use.
func (s *Store) Taken(ctx context.Context, name string) (yes bool, err error) {
	err = s.read(ctx, func(c catalog) error {
		for _, p := range c.prompts {
			if p.Name == name {
				yes = true
			}
		}
		return nil
	})
	return
}
func schemaValue(v json.RawMessage) any {
	if len(v) == 0 {
		return nil
	}
	return string(v)
}
func promptArgs(p Prompt) []any {
	tools, _ := json.Marshal(p.Tools)
	return []any{p.ID, p.Name, p.Owner, p.OwnerEmail, p.Model, p.Prompt, p.System, string(tools), schemaValue(p.Schema), p.Created.Format(time.RFC3339)}
}

// Create inserts a new prompt with a unique ID.
func (s *Store) Create(ctx context.Context, d Draft) (p Prompt, err error) {
	err = s.write(ctx, func(tx *sql.Tx, c catalog) error {
		if !ValidName(d.Name) || d.Owner == "" || d.Model == "" || d.Prompt == "" {
			return errInvalid
		}
		for _, v := range c.prompts {
			if v.Name == d.Name {
				return ErrNameTaken
			}
		}
		var id string
		for range 8 {
			v, e := NewPromptID(s.cfg.Rand)
			if e != nil {
				return e
			}
			if _, ok := c.prompts[v]; !ok {
				id = v
				break
			}
		}
		if id == "" {
			return errInvalid
		}
		p = Prompt{ID: id, Name: d.Name, Owner: d.Owner, OwnerEmail: d.OwnerEmail, Model: d.Model, Prompt: d.Prompt, System: d.System, Tools: append([]string{}, d.Tools...), Created: stamp(s.cfg.Now()), Subscriptions: []Subscription{}}
		if len(d.Schema) > 0 {
			p.Schema = bytes.Clone(d.Schema)
		}
		_, e := tx.Exec("INSERT INTO prompts VALUES (?,?,?,?,?,?,?,?,?,?)", promptArgs(p)...)
		return e
	})
	if err != nil {
		p = Prompt{}
	}
	return
}
func validChange(c Change) bool {
	return (c.Model == nil || *c.Model != "") && (c.Prompt == nil || *c.Prompt != "") && (c.Schema == nil || len(*c.Schema) != 0)
}

// Update replaces selected fields and reports whether their values changed.
func (s *Store) Update(ctx context.Context, id string, ch Change) (p Prompt, changed bool, err error) {
	err = s.write(ctx, func(tx *sql.Tx, c catalog) error {
		if !validChange(ch) {
			return errInvalid
		}
		old, ok := c.prompts[id]
		if !ok {
			return ErrNotFound
		}
		p = old
		if ch.Model != nil {
			p.Model = *ch.Model
		}
		if ch.Prompt != nil {
			p.Prompt = *ch.Prompt
		}
		if ch.System != nil {
			p.System = *ch.System
		}
		if ch.Tools != nil {
			p.Tools = append([]string{}, (*ch.Tools)...)
		}
		if ch.Schema != nil {
			p.Schema = bytes.Clone(*ch.Schema)
		}
		changed = p.Model != old.Model || p.Prompt != old.Prompt || p.System != old.System || !slices.Equal(p.Tools, old.Tools) || !bytes.Equal(p.Schema, old.Schema)
		args := promptArgs(p)
		_, e := tx.Exec("UPDATE prompts SET model=?,prompt=?,system=?,tools=?,schema=? WHERE id=?", args[4], args[5], args[6], args[7], args[8], id)
		return e
	})
	if err != nil {
		p = Prompt{}
		changed = false
	}
	return
}

// Delete removes a prompt and all its catalog records.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.write(ctx, func(tx *sql.Tx, c catalog) error {
		if _, ok := c.prompts[id]; !ok {
			return ErrNotFound
		}
		for _, query := range []string{"DELETE FROM subscriptions WHERE prompt=?", "DELETE FROM runs WHERE prompt=?", "DELETE FROM event_runs WHERE prompt=?"} {
			if _, err := tx.Exec(query, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec("DELETE FROM prompts WHERE id=?", id)
		return err
	})
}
func (s *Store) subscription(ctx context.Context, id, event string, remove bool) (p Prompt, err error) {
	err = s.write(ctx, func(tx *sql.Tx, c catalog) error {
		if !ValidEvent(event) {
			return errInvalid
		}
		v, ok := c.prompts[id]
		if !ok {
			return ErrNotFound
		}
		index := slices.IndexFunc(v.Subscriptions, func(sub Subscription) bool { return sub.Event == event })
		if remove {
			if index < 0 {
				return ErrNotSubscribed
			}
			if _, e := tx.Exec("DELETE FROM subscriptions WHERE prompt=? AND event=?", id, event); e != nil {
				return e
			}
			v.Subscriptions = append(v.Subscriptions[:index:index], v.Subscriptions[index+1:]...)
		} else if index < 0 {
			created := stamp(s.cfg.Now())
			if _, e := tx.Exec("INSERT INTO subscriptions VALUES (?,?,?)", id, event, created.Format(time.RFC3339)); e != nil {
				return e
			}
			v.Subscriptions = append(v.Subscriptions, Subscription{event, created})
			slices.SortFunc(v.Subscriptions, func(a, b Subscription) int { return strings.Compare(a.Event, b.Event) })
		}
		p = v
		return nil
	})
	if err != nil {
		p = Prompt{}
	}
	return
}

// Subscribe adds an event pattern idempotently.
func (s *Store) Subscribe(ctx context.Context, id, event string) (Prompt, error) {
	return s.subscription(ctx, id, event, false)
}

// Unsubscribe removes an exact event pattern.
func (s *Store) Unsubscribe(ctx context.Context, id, event string) (Prompt, error) {
	return s.subscription(ctx, id, event, true)
}
func matches(pattern, event string) bool {
	if !ValidEvent(event) || strings.Contains(event, "*") {
		return false
	}
	p, e := strings.Split(pattern, "."), strings.Split(event, ".")
	if len(p) != len(e) {
		return false
	}
	for i := range p {
		if p[i] != "*" && p[i] != e[i] {
			return false
		}
	}
	return true
}

// Subscribers lists each prompt matching an event name once.
func (s *Store) Subscribers(ctx context.Context, event string) (ps []Prompt, err error) {
	ps = []Prompt{}
	err = s.read(ctx, func(c catalog) error {
		for _, p := range c.prompts {
			for _, sub := range p.Subscriptions {
				if matches(sub.Event, event) {
					ps = append(ps, p)
					break
				}
			}
		}
		slices.SortFunc(ps, func(a, b Prompt) int { return strings.Compare(a.ID, b.ID) })
		return nil
	})
	if err != nil {
		ps = nil
	}
	return
}

// Delivered reports whether a prompt already had a run for an event ID.
func (s *Store) Delivered(ctx context.Context, prompt, event string) (yes bool, err error) {
	err = s.read(ctx, func(c catalog) error { yes = c.delivered[[2]string{prompt, event}]; return nil })
	return
}
