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

const runColumns = "id,prompt,model,user_id,request_id,trigger_kind,event,status,exit_code,started,finished,stdout_bytes,stderr_bytes,stdout_truncated,stderr_truncated,reason,calls,tool_calls,input_tokens,cached_tokens,output_tokens,reasoning_tokens,cost_nanos"

type scanner interface{ Scan(...any) error }

func scanPrompt(row scanner) (p Prompt, last string, err error) {
	var tools, created string
	var schema sql.NullString
	err = row.Scan(&p.ID, &p.Name, &p.Owner, &p.OwnerEmail, &p.Model, &p.Prompt, &p.System, &tools, &schema, &created, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return Prompt{}, "", ErrNotFound
	}
	if err != nil {
		return
	}
	err = json.Unmarshal([]byte(tools), &p.Tools)
	if err != nil {
		return
	}
	p.Created, err = time.Parse(time.RFC3339, created)
	if schema.Valid {
		p.Schema = json.RawMessage(schema.String)
	}
	p.Subscriptions = []Subscription{}
	return
}

const promptSelect = "SELECT p.id,p.name,p.owner_id,p.owner_email,p.model,p.prompt,p.system,p.tools,p.schema,p.created,COALESCE((SELECT id FROM runs WHERE prompt=p.id ORDER BY started DESC,id DESC LIMIT 1),'') FROM prompts p "

func completePrompt(tx *sql.Tx, p *Prompt, last string) error {
	rows, err := tx.Query("SELECT event,created FROM subscriptions WHERE prompt=? ORDER BY event", p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sub Subscription
		var created string
		if err = rows.Scan(&sub.Event, &created); err != nil {
			break
		}
		sub.Created, err = time.Parse(time.RFC3339, created)
		if err != nil {
			break
		}
		p.Subscriptions = append(p.Subscriptions, sub)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return err
	}
	if last != "" {
		r, e := queryRun(tx, "SELECT "+runColumns+" FROM runs WHERE id=?", last)
		if e != nil {
			return e
		}
		p.Last = &r
	}
	return nil
}
func queryPrompt(tx *sql.Tx, clause string, args ...any) (Prompt, error) {
	p, last, err := scanPrompt(tx.QueryRow(promptSelect+clause, args...))
	if err != nil {
		return Prompt{}, err
	}
	if err = completePrompt(tx, &p, last); err != nil {
		return Prompt{}, err
	}
	return p, nil
}
func queryPrompts(tx *sql.Tx, clause string, args ...any) ([]Prompt, error) {
	rows, err := tx.Query(promptSelect+clause, args...)
	if err != nil {
		return nil, err
	}
	ps := []Prompt{}
	lasts := []string{}
	for rows.Next() {
		p, last, e := scanPrompt(rows)
		if e != nil {
			err = e
			break
		}
		ps = append(ps, p)
		lasts = append(lasts, last)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	for i := range ps {
		if err = completePrompt(tx, &ps[i], lasts[i]); err != nil {
			return nil, err
		}
	}
	return ps, nil
}
func exists(tx *sql.Tx, query string, args ...any) (bool, error) {
	var yes bool
	err := tx.QueryRow("SELECT EXISTS("+query+")", args...).Scan(&yes)
	return yes, err
}
func requirePrompt(tx *sql.Tx, id string) error {
	yes, err := exists(tx, "SELECT 1 FROM prompts WHERE id=?", id)
	if err != nil {
		return err
	}
	if !yes {
		return ErrNotFound
	}
	return nil
}
func (s *Store) read(ctx context.Context, f func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.d.Read(ctx, f)
}
func (s *Store) write(ctx context.Context, f func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.d.Write(ctx, f)
}

// Find finds an owner's prompt by name.
func (s *Store) Find(ctx context.Context, owner, name string) (p Prompt, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		p, err = queryPrompt(tx, "WHERE p.owner_id=? AND p.name=?", owner, name)
		return err
	})
	return
}

// List lists a bounded window of an owner's prompts in name order.
func (s *Store) List(ctx context.Context, owner, after string, n int) (ps []Prompt, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			ps = []Prompt{}
			return nil
		}
		ps, err = queryPrompts(tx, "WHERE p.owner_id=? AND p.name>? ORDER BY p.name LIMIT ?", owner, after, n)
		return err
	})
	if err != nil {
		ps = nil
	}
	return
}

// ListBefore lists a bounded window of an owner's prompts in reverse name order.
func (s *Store) ListBefore(ctx context.Context, owner, before string, n int) (ps []Prompt, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			ps = []Prompt{}
			return nil
		}
		if before == "" {
			ps, err = queryPrompts(tx, "WHERE p.owner_id=? ORDER BY p.name DESC LIMIT ?", owner, n)
		} else {
			ps, err = queryPrompts(tx, "WHERE p.owner_id=? AND p.name<? ORDER BY p.name DESC LIMIT ?", owner, before, n)
		}
		return err
	})
	if err != nil {
		ps = nil
	}
	return
}

// Taken reports whether a prompt name is in use.
func (s *Store) Taken(ctx context.Context, name string) (yes bool, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error { yes, err = exists(tx, "SELECT 1 FROM prompts WHERE name=?", name); return err })
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
	err = s.write(ctx, func(tx *sql.Tx) error {
		if !ValidName(d.Name) || d.Owner == "" || d.Model == "" || d.Prompt == "" {
			return errInvalid
		}
		taken, e := exists(tx, "SELECT 1 FROM prompts WHERE name=?", d.Name)
		if e != nil {
			return e
		}
		if taken {
			return ErrNameTaken
		}
		var id string
		for range 8 {
			v, e := NewPromptID(s.cfg.Rand)
			if e != nil {
				return e
			}
			occupied, e := exists(tx, "SELECT 1 FROM prompts WHERE id=?", v)
			if e != nil {
				return e
			}
			if !occupied {
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
		_, e = tx.Exec("INSERT INTO prompts VALUES (?,?,?,?,?,?,?,?,?,?)", promptArgs(p)...)
		return e
	})
	if err != nil {
		p = Prompt{}
	}
	return
}
func validChange(c Change) bool {
	return (c.Model == nil || *c.Model != "") && (c.Prompt == nil || *c.Prompt != "")
}

// Update replaces selected fields and reports whether their values changed.
func (s *Store) Update(ctx context.Context, id string, ch Change) (p Prompt, changed bool, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		if !validChange(ch) {
			return errInvalid
		}
		old, e := queryPrompt(tx, "WHERE p.id=?", id)
		if e != nil {
			return e
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
			p.Schema = nil
			if len(*ch.Schema) > 0 {
				p.Schema = bytes.Clone(*ch.Schema)
			}
		}
		changed = p.Model != old.Model || p.Prompt != old.Prompt || p.System != old.System || !slices.Equal(p.Tools, old.Tools) || !bytes.Equal(p.Schema, old.Schema)
		args := promptArgs(p)
		_, e = tx.Exec("UPDATE prompts SET model=?,prompt=?,system=?,tools=?,schema=? WHERE id=?", args[4], args[5], args[6], args[7], args[8], id)
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
	return s.write(ctx, func(tx *sql.Tx) error {
		if err := requirePrompt(tx, id); err != nil {
			return err
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
	err = s.write(ctx, func(tx *sql.Tx) error {
		if !ValidEvent(event) {
			return errInvalid
		}
		v, e := queryPrompt(tx, "WHERE p.id=?", id)
		if e != nil {
			return e
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

// Subscribers lists each prompt matching an event name once.
func (s *Store) Subscribers(ctx context.Context, event string) (ps []Prompt, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		if !ValidEvent(event) || strings.Contains(event, "*") {
			ps = []Prompt{}
			return nil
		}
		ps, err = queryPrompts(tx, "WHERE EXISTS (SELECT 1 FROM subscriptions s WHERE s.prompt=p.id AND ? GLOB s.event AND length(s.event)-length(replace(s.event,'.',''))=?) ORDER BY p.id", event, strings.Count(event, "."))
		return err
	})
	if err != nil {
		ps = nil
	}
	return
}

// Delivered reports whether a prompt already had a run for an event ID.
func (s *Store) Delivered(ctx context.Context, prompt, event string) (yes bool, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		yes, err = exists(tx, "SELECT 1 FROM event_runs WHERE prompt=? AND event=?", prompt, event)
		return err
	})
	return
}
