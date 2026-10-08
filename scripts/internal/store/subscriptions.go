package store

import (
	"context"
	"errors"
	"sort"

	"github.com/ikigenba/ikigenba/appkit/events"
)

// Subscribe adds an event subscription, preserving an existing creation time.
func (s *Store) Subscribe(ctx context.Context, id, event string) (Script, error) {
	return s.subscription(ctx, id, event, true)
}

// Unsubscribe removes an existing subscription.
func (s *Store) Unsubscribe(ctx context.Context, id, event string) (Script, error) {
	return s.subscription(ctx, id, event, false)
}

func (s *Store) subscription(ctx context.Context, id, event string, add bool) (Script, error) {
	var out Script
	err := s.transaction(ctx, true, func(c *catalog) error {
		if !ValidEvent(event) {
			return errors.New("invalid event")
		}
		sc, ok := c.data.Scripts[id]
		if !ok {
			return ErrNotFound
		}
		found := -1
		for i, sub := range c.subscriptions[id] {
			if sub.Event == event {
				found = i
				break
			}
		}
		if add && found == -1 {
			created := normalize(c.cfg.Now())
			if _, err := c.tx.ExecContext(ctx, "INSERT INTO subscriptions(script,event,created) VALUES(?,?,?)", id, event, created.Unix()); err != nil {
				return err
			}
			c.subscriptions[id] = append(c.subscriptions[id], Subscription{Event: event, Created: created})
			sort.Slice(c.subscriptions[id], func(i, j int) bool { return c.subscriptions[id][i].Event < c.subscriptions[id][j].Event })
		} else if !add {
			if found == -1 {
				return ErrNotSubscribed
			}
			if _, err := c.tx.ExecContext(ctx, "DELETE FROM subscriptions WHERE script=? AND event=?", id, event); err != nil {
				return err
			}
			c.subscriptions[id] = append(c.subscriptions[id][:found], c.subscriptions[id][found+1:]...)
		}
		out = c.script(sc)
		return nil
	})
	if err != nil {
		return Script{}, err
	}
	return out, nil
}

// Subscribers returns each script with a pattern matching the event name.
func (s *Store) Subscribers(ctx context.Context, event string) ([]Script, error) {
	out := []Script{}
	err := s.transaction(ctx, false, func(c *catalog) error {
		for id, subs := range c.subscriptions {
			for _, sub := range subs {
				if events.Match(sub.Event, event) {
					if sc, ok := c.data.Scripts[id]; ok {
						out = append(out, c.script(sc))
					}
					break
				}
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *catalog) delivered(ctx context.Context, script, eventID string) (bool, error) {
	var count int
	err := s.tx.QueryRowContext(ctx, "SELECT count(*) FROM event_runs WHERE script=? AND event=?", script, eventID).Scan(&count)
	return count > 0, err
}

// Delivered reports durable event-run memory, including pruned runs.
func (s *Store) Delivered(ctx context.Context, script, eventID string) (bool, error) {
	var out bool
	err := s.transaction(ctx, false, func(c *catalog) error { var err error; out, err = c.delivered(ctx, script, eventID); return err })
	if err != nil {
		return false, err
	}
	return out, nil
}
