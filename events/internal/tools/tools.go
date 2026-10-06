// Package tools exposes the broker's retained log and subscriber controls over MCP.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

// Config provides the shared log and the tools' event writer.
type Config struct {
	Store     *store.Store
	Telemetry *telemetry.Writer
}

type catalogInput struct {
	Service string `json:"service"`
	Event   string `json:"event"`
}
type searchInput struct {
	Since     *string         `json:"since"`
	Until     *string         `json:"until"`
	Services  []string        `json:"services"`
	Events    []string        `json:"events"`
	User      *string         `json:"user"`
	RequestID *string         `json:"request_id"`
	Cause     *string         `json:"cause"`
	Attrs     json.RawMessage `json:"attrs"`
	Limit     *int64          `json:"limit"`
	Cursor    *string         `json:"cursor"`
}
type serviceInput struct {
	Service string `json:"service" mcp:"required"`
}
type producer struct {
	Service string   `json:"service"`
	Attrs   []string `json:"attrs"`
}
type catalogEntry struct {
	Event    string     `json:"event"`
	Emits    []producer `json:"emits"`
	Accepts  []string   `json:"accepts"`
	Count    int64      `json:"count"`
	LastSeen *string    `json:"last_seen"`
}
type catalogOutput struct {
	Events []catalogEntry `json:"events"`
}
type record struct {
	ID        string          `json:"id"`
	Time      string          `json:"time"`
	Service   string          `json:"service"`
	Event     string          `json:"event"`
	RequestID string          `json:"request_id"`
	User      string          `json:"user"`
	Attrs     json.RawMessage `json:"attrs"`
	Cause     string          `json:"cause"`
	Depth     int             `json:"depth"`
	Seq       int64           `json:"seq"`
	Received  string          `json:"received"`
}
type searchOutput struct {
	Records []record `json:"records"`
	Cursor  *string  `json:"cursor"`
}
type reason struct {
	Event string `json:"event"`
	Name  string `json:"name"`
	Seq   int64  `json:"seq"`
	Error string `json:"error"`
}
type subscriberEntry struct {
	Service string  `json:"service"`
	Status  string  `json:"status"`
	Cursor  int64   `json:"cursor"`
	Lag     int64   `json:"lag"`
	Since   string  `json:"since"`
	Reason  *reason `json:"reason"`
}
type subscribersOutput struct {
	Subscribers []subscriberEntry `json:"subscribers"`
}

func wireTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000Z") }
func entry(s store.Subscriber) subscriberEntry {
	e := subscriberEntry{Service: s.Service, Status: string(s.Status), Cursor: s.Cursor, Lag: s.Lag, Since: wireTime(s.Since)}
	if s.Reason != nil {
		e.Reason = &reason{Event: s.Reason.Event, Name: s.Reason.Name, Seq: s.Reason.Seq, Error: s.Reason.Error}
	}
	return e
}

var errLog = errors.New("cannot reach the log; try again later")

// Register adds the five tools in their published order.
func Register(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, mcp.Tool[catalogInput, catalogOutput]{Name: "catalog", Description: "Every event the suite emits, who emits and accepts it, counts and last seen.", Effect: mcp.Read,
		Handler: func(ctx context.Context, _ identity.Caller, in catalogInput) (catalogOutput, error) {
			items, err := cfg.Store.Catalog(ctx, in.Service, in.Event)
			if err != nil {
				return catalogOutput{}, errLog
			}
			out := catalogOutput{Events: make([]catalogEntry, 0, len(items))}
			for _, item := range items {
				e := catalogEntry{Event: item.Event, Emits: make([]producer, 0, len(item.Emits)), Accepts: append([]string{}, item.Accepts...), Count: item.Count}
				for _, p := range item.Emits {
					e.Emits = append(e.Emits, producer{Service: p.Service, Attrs: append([]string{}, p.Attrs...)})
				}
				if item.Count > 0 {
					v := wireTime(item.LastSeen)
					e.LastSeen = &v
				}
				out.Events = append(out.Events, e)
			}
			return out, nil
		}})
	mcp.AddTool(srv, mcp.Tool[searchInput, searchOutput]{Name: "search", Description: "The retained log, newest first, filtered by service, event, user, request id, cause or attributes.", Effect: mcp.Read,
		Handler: func(ctx context.Context, _ identity.Caller, in searchInput) (searchOutput, error) {
			return search(ctx, cfg.Store, in)
		}})
	mcp.AddTool(srv, mcp.Tool[struct{}, subscribersOutput]{Name: "subscribers", Description: "Each subscriber's status, reason, cursor and lag.", Effect: mcp.Read,
		Handler: func(ctx context.Context, _ identity.Caller, _ struct{}) (subscribersOutput, error) {
			items, err := cfg.Store.Subscribers(ctx)
			if err != nil {
				return subscribersOutput{}, errLog
			}
			out := subscribersOutput{Subscribers: make([]subscriberEntry, 0, len(items))}
			for _, item := range items {
				out.Subscribers = append(out.Subscribers, entry(item))
			}
			return out, nil
		}})
	mcp.AddTool(srv, mcp.Tool[serviceInput, subscriberEntry]{Name: "skip", Description: "Skip the event a paused subscriber is stuck on and resume it.", Effect: mcp.Destructive,
		Handler: func(ctx context.Context, _ identity.Caller, in serviceInput) (subscriberEntry, error) {
			s, r, err := cfg.Store.Skip(ctx, in.Service)
			if err != nil {
				return subscriberEntry{}, controlError(err, in.Service)
			}
			cfg.Telemetry.Emit(ctx, "event.skipped", telemetry.Attrs{"event": r.Event, "service": in.Service})
			return entry(s), nil
		}})
	mcp.AddTool(srv, mcp.Tool[serviceInput, subscriberEntry]{Name: "resume", Description: "Retry the event a paused subscriber is stuck on.", Effect: mcp.Additive,
		Handler: func(ctx context.Context, _ identity.Caller, in serviceInput) (subscriberEntry, error) {
			s, err := cfg.Store.Resume(ctx, in.Service)
			if err != nil {
				return subscriberEntry{}, controlError(err, in.Service)
			}
			return entry(s), nil
		}})
}

func controlError(err error, service string) error {
	switch {
	case errors.Is(err, store.ErrNoSubscriber):
		return fmt.Errorf("no subscriber '%s'", service)
	case errors.Is(err, store.ErrNotPaused):
		return fmt.Errorf("'%s' is not paused", service)
	default:
		return errLog
	}
}

func search(ctx context.Context, st *store.Store, in searchInput) (searchOutput, error) {
	f := store.Filter{Services: in.Services, Events: in.Events, User: in.User, RequestID: in.RequestID, Cause: in.Cause}
	for _, field := range []struct {
		name   string
		value  *string
		target **time.Time
	}{{"since", in.Since, &f.Since}, {"until", in.Until, &f.Until}} {
		if field.value != nil {
			t, err := time.Parse(time.RFC3339, *field.value)
			if err != nil {
				return searchOutput{}, fmt.Errorf("%s is not an RFC 3339 time: '%s'", field.name, *field.value)
			}
			*field.target = &t
		}
	}
	limit := int64(50)
	if in.Limit != nil {
		limit = *in.Limit
	}
	if limit < 1 || limit > 500 {
		return searchOutput{}, fmt.Errorf("limit must be between 1 and 500, got %d", limit)
	}
	after := store.Cursor("")
	if in.Cursor != nil {
		after = store.Cursor(*in.Cursor)
	}
	f.Attrs = decodeAttrs(in.Attrs)
	p, err := st.Search(ctx, f, int(limit), after)
	if errors.Is(err, store.ErrCursor) {
		return searchOutput{}, errors.New("cursor is not one search issued")
	}
	if err != nil {
		return searchOutput{}, errLog
	}
	out := searchOutput{Records: make([]record, 0, len(p.Records))}
	for _, e := range p.Records {
		attrs, err := json.Marshal(e.Attrs)
		if err != nil {
			return searchOutput{}, errLog
		}
		out.Records = append(out.Records, record{ID: e.ID, Time: wireTime(e.Time), Service: e.Service, Event: e.Name, RequestID: e.RequestID, User: e.User, Attrs: attrs, Cause: e.Cause, Depth: e.Depth, Seq: e.Seq, Received: wireTime(e.Received)})
	}
	if p.Next != "" {
		v := string(p.Next)
		out.Cursor = &v
	}
	return out, nil
}

func decodeAttrs(raw json.RawMessage) events.Attrs {
	var values map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&values); err != nil {
		return nil
	}
	for k, v := range values {
		if n, ok := v.(json.Number); ok {
			if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
				values[k] = i
				continue
			}
			if u, err := strconv.ParseUint(string(n), 10, 64); err == nil {
				values[k] = u
				continue
			}
			if f, err := strconv.ParseFloat(string(n), 64); err == nil {
				values[k] = f
			} else {
				values[k] = nil
			}
		}
	}
	return values
}
