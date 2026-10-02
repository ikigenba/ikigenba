// Package tools exposes read-only MCP tools over telemetry's trail.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
)

const timestamp = "2006-01-02T15:04:05.000000Z"

const catalogDescription = "The services, the events each records, and the attribute keys each carries.\n\nWithout arguments, every service in the trail in name order, each with its events in name order, and for each event how many records it has, when the latest was recorded, and the attribute keys its records carry. Pass service or event, or both, to narrow it. Call it first to learn what search and count can filter on."
const searchDescription = "The records that match a filter, newest first.\n\nEvery argument is optional and they combine: since and until bound the time (RFC 3339, since inclusive, until exclusive), services and events take any of the names given, user and request_id match exactly, and attrs is an object whose every pair a record must carry. limit is 1 to 500, 50 when left out. When more records match, the result carries a cursor; pass it back with the same filters for the next page."
const countDescription = "How many records match a filter, grouped by a field or a time bucket.\n\nTakes the filters of search. Without by, the total. With by, one of service, event, user, request_id, minute, hour, day, or attrs.<key>, the total and one group per value with its count."
const traceDescription = "Every record of one request, from every service it touched, oldest first.\n\nPass the request id; the result is the records of that id from every service, in the order they happened. An id the trail does not hold gives no records, not an error."

type catalogInput struct {
	Service string `json:"service" description:"Keep only this service's entry."`
	Event   string `json:"event" description:"Keep only this event, in every service's entry."`
}
type traceInput struct {
	RequestID string `json:"request_id" mcp:"required" description:"The request id, exactly as a service recorded it."`
}
type record struct {
	Time      string          `json:"time" mcp:"required"`
	Service   string          `json:"service" mcp:"required"`
	Event     string          `json:"event" mcp:"required"`
	RequestID string          `json:"request_id" mcp:"required"`
	User      string          `json:"user" mcp:"required"`
	Attrs     json.RawMessage `json:"attrs" mcp:"required"`
}
type traceOutput struct {
	Records []record `json:"records" mcp:"required" description:"Every record of the request, oldest first."`
}
type searchOutput struct {
	Records []record `json:"records" mcp:"required" description:"The matching records, newest first."`
	Cursor  *string  `json:"cursor" description:"Present when more records match: pass it back with the same filters for the next page."`
}
type eventEntry struct {
	Event    string   `json:"event" mcp:"required"`
	Count    int64    `json:"count" mcp:"required"`
	LastSeen string   `json:"last_seen" mcp:"required"`
	Attrs    []string `json:"attrs" mcp:"required"`
}
type catalogEntry struct {
	Service string       `json:"service" mcp:"required"`
	Events  []eventEntry `json:"events" mcp:"required"`
}
type catalogOutput struct {
	Services []catalogEntry `json:"services" mcp:"required" description:"Every service with records, in name order, each with its events in name order."`
}
type group struct {
	Key   string `json:"key" mcp:"required" description:"The group's value as text: the field's value, the bucket's start, or the attribute's value."`
	Count int64  `json:"count" mcp:"required"`
}
type countOutput struct {
	Total  int64    `json:"total" mcp:"required" description:"How many records match the filter."`
	Groups *[]group `json:"groups" description:"Present when by was given: one group per value, with its count."`
}
type searchInput struct {
	Since     *string         `json:"since" description:"Keep records at or after this RFC 3339 time."`
	Until     *string         `json:"until" description:"Keep records before this RFC 3339 time."`
	Services  []string        `json:"services" description:"Keep records of any of these services."`
	Events    []string        `json:"events" description:"Keep records of any of these events."`
	User      *string         `json:"user" description:"Keep records whose user is exactly this."`
	RequestID *string         `json:"request_id" description:"Keep records whose request id is exactly this."`
	Attrs     json.RawMessage `json:"attrs" description:"Keep records that carry every one of these keys with exactly this value; a number matches a number and a string a string."`
	Limit     *int            `json:"limit" description:"The most records one page holds, 1 to 500; 50 when left out."`
	Cursor    *string         `json:"cursor" description:"The cursor a previous page gave, to continue it with the same filters."`
}
type countInput struct {
	Since     *string         `json:"since" description:"Keep records at or after this RFC 3339 time."`
	Until     *string         `json:"until" description:"Keep records before this RFC 3339 time."`
	Services  []string        `json:"services" description:"Keep records of any of these services."`
	Events    []string        `json:"events" description:"Keep records of any of these events."`
	User      *string         `json:"user" description:"Keep records whose user is exactly this."`
	RequestID *string         `json:"request_id" description:"Keep records whose request id is exactly this."`
	Attrs     json.RawMessage `json:"attrs" description:"Keep records that carry every one of these keys with exactly this value; a number matches a number and a string a string."`
	By        *string         `json:"by" description:"Group the count by service, event, user, request_id, minute, hour, day, or attrs.<key>."`
}

// Register adds the four trail tools to srv in their advertised order.
func Register(srv *mcp.Server, s *store.Store) {
	mcp.AddTool(srv, mcp.Tool[catalogInput, catalogOutput]{Name: "catalog", Description: catalogDescription, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, in catalogInput) (catalogOutput, error) {
		entries, err := s.Catalog(ctx, in.Service, in.Event)
		if err != nil {
			return catalogOutput{}, errors.New("cannot read the trail")
		}
		out := catalogOutput{Services: make([]catalogEntry, 0, len(entries))}
		for _, e := range entries {
			ce := catalogEntry{Service: e.Service, Events: make([]eventEntry, 0, len(e.Events))}
			for _, v := range e.Events {
				attrs := append([]string{}, v.Attrs...)
				ce.Events = append(ce.Events, eventEntry{v.Event, v.Count, v.LastSeen.UTC().Format(timestamp), attrs})
			}
			out.Services = append(out.Services, ce)
		}
		return out, nil
	}})
	mcp.AddTool(srv, mcp.Tool[searchInput, searchOutput]{Name: "search", Description: searchDescription, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, in searchInput) (searchOutput, error) {
		f, err := filter(in.Since, in.Until, in.Services, in.Events, in.User, in.RequestID, in.Attrs)
		if err != nil {
			return searchOutput{}, err
		}
		limit := 50
		if in.Limit != nil {
			limit = *in.Limit
		}
		if limit < 1 || limit > 500 {
			return searchOutput{}, fmt.Errorf("limit must be between 1 and 500, got %d", limit)
		}
		var cursor store.Cursor
		if in.Cursor != nil {
			cursor = store.Cursor(*in.Cursor)
		}
		page, err := s.Search(ctx, f, limit, cursor)
		if errors.Is(err, store.ErrCursor) {
			return searchOutput{}, errors.New("cursor is not one search issued")
		}
		if err != nil {
			return searchOutput{}, errors.New("cannot read the trail")
		}
		out := searchOutput{Records: records(page.Records)}
		if page.Next != "" {
			next := string(page.Next)
			out.Cursor = &next
		}
		return out, nil
	}})
	mcp.AddTool(srv, mcp.Tool[countInput, countOutput]{Name: "count", Description: countDescription, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, in countInput) (countOutput, error) {
		f, err := filter(in.Since, in.Until, in.Services, in.Events, in.User, in.RequestID, in.Attrs)
		if err != nil {
			return countOutput{}, err
		}
		if in.By == nil {
			total, e := s.Count(ctx, f)
			if e != nil {
				return countOutput{}, errors.New("cannot read the trail")
			}
			return countOutput{Total: total}, nil
		}
		total, groups, err := s.CountBy(ctx, f, store.GroupBy(*in.By))
		if errors.Is(err, store.ErrGroupBy) {
			return countOutput{}, fmt.Errorf("by must be service, event, user, request_id, minute, hour, day, or attrs.<key>, got '%s'", *in.By)
		}
		if err != nil {
			return countOutput{}, errors.New("cannot read the trail")
		}
		out := make([]group, 0, len(groups))
		for _, g := range groups {
			out = append(out, group{g.Key, g.Count})
		}
		return countOutput{Total: total, Groups: &out}, nil
	}})
	mcp.AddTool(srv, mcp.Tool[traceInput, traceOutput]{Name: "trace", Description: traceDescription, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, in traceInput) (traceOutput, error) {
		rs, err := s.Trace(ctx, in.RequestID)
		if err != nil {
			return traceOutput{}, errors.New("cannot read the trail")
		}
		return traceOutput{Records: records(rs)}, nil
	}})
}
func records(rs []store.Record) []record {
	out := make([]record, 0, len(rs))
	for _, r := range rs {
		out = append(out, record{r.Time.UTC().Format(timestamp), r.Service, r.Event, r.RequestID, r.User, r.Attrs})
	}
	return out
}
func filter(since, until *string, services, events []string, user, requestID *string, attrs json.RawMessage) (store.Filter, error) {
	f := store.Filter{Services: services, Events: events, User: user, RequestID: requestID}
	for _, bound := range []struct {
		name   string
		value  *string
		target **time.Time
	}{{"since", since, &f.Since}, {"until", until, &f.Until}} {
		if bound.value != nil {
			v, err := time.Parse(time.RFC3339, *bound.value)
			if err != nil {
				return f, fmt.Errorf("%s is not an RFC 3339 time: '%s'", bound.name, *bound.value)
			}
			*bound.target = &v
		}
	}
	if len(attrs) > 0 && string(attrs) != "null" {
		var members map[string]json.RawMessage
		if err := json.Unmarshal(attrs, &members); err != nil {
			return f, err
		}
		f.Attrs = make(telemetry.Attrs, len(members))
		for k, v := range members {
			f.Attrs[k] = attribute(v)
		}
	}
	return f, nil
}
func attribute(raw json.RawMessage) any {
	var value any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil
	}
	if n, ok := value.(json.Number); ok {
		text := string(n)
		if text != "-0" {
			if v, err := strconv.ParseInt(text, 10, 64); err == nil {
				return v
			}
			if v, err := strconv.ParseUint(text, 10, 64); err == nil {
				return v
			}
		}
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil
		}
		return v
	}
	return value
}
