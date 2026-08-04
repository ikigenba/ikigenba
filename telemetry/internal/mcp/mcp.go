package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"appkit"
	appkitmcp "appkit/mcp"
	"appkit/server"
	"telemetry/internal/db"
	"telemetry/internal/record"
	"telemetry/internal/telemetry"
)

const Instructions = "Forensic record store for this box: the audit trail of who did what, when, and in what order across every service — MCP calls, HTTP requests, outbound calls, event publishes and consumes, and service starts and stops. Use `search` to find records (by time, service, kind, actor, operation, outcome, or content digest), `chain` to follow one correlation id through every service it touched, and `get` to read one record in full; call `guide` for the record model and worked examples."

// NewHandler constructs the telemetry MCP endpoint using the router's chassis hooks.
func NewHandler(store *db.Store, clock telemetry.Clock, rt *appkit.Router) (http.Handler, error) {
	return appkitmcp.New(appkitmcp.Options{
		Service:       rt.Service(),
		Version:       rt.Version(),
		Instructions:  Instructions,
		Tools:         Tools(store, clock),
		Health:        rt.Health(),
		Events:        rt.Events(),
		Publishes:     rt.Publishes(),
		Subscriptions: rt.Subscriptions(),
		Recorder:      rt.Recorder(),
	})
}

// Tools returns telemetry's complete, read-only domain tool table.
func Tools(store *db.Store, clock telemetry.Clock) []appkitmcp.Tool {
	_ = clock // retained as the domain time seam for the MCP surface.
	return []appkitmcp.Tool{
		{
			Name: "search", Description: "Find bounded forensic records using AND-composed filters; returns newest first with an opaque continuation cursor.",
			InputSchema: searchInputSchema(), OutputSchema: searchOutputSchema(),
			Handler: searchHandler(store),
		},
		{
			Name: "chain", Description: "Follow one correlation id across every service in chronological order.",
			InputSchema: objectSchema(map[string]any{"correlation_id": stringSchema()}, "correlation_id"), OutputSchema: chainOutputSchema(),
			Handler: chainHandler(store),
		},
		{
			Name: "get", Description: "Read one forensic record in full by id.",
			InputSchema: objectSchema(map[string]any{"id": stringSchema()}, "id"), OutputSchema: recordSchema(),
			Handler: getHandler(store),
		},
		{
			Name: "guide", Description: "Read the guide to the record model, field meanings, and worked examples.",
			InputSchema: objectSchema(map[string]any{}), Handler: func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
				return appkitmcp.TextResult(guideText), nil
			},
		},
	}
}

type searchArgs struct {
	Since      string `json:"since"`
	Until      string `json:"until"`
	Service    string `json:"service"`
	Kind       string `json:"kind"`
	OwnerEmail string `json:"owner_email"`
	ClientID   string `json:"client_id"`
	OpContains string `json:"op_contains"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	SHA256     string `json:"sha256"`
	Limit      *int   `json:"limit"`
	Cursor     string `json:"cursor"`
}

type searchResult struct {
	Records          []record.Record `json:"records"`
	NextCursor       string          `json:"next_cursor"`
	RetentionHorizon string          `json:"retention_horizon"`
}

type chainResult struct {
	Records           []record.Record `json:"records"`
	Count             int             `json:"count"`
	RetentionHorizon  string          `json:"retention_horizon"`
	PossiblyTruncated bool            `json:"possibly_truncated"`
}

func searchHandler(store *db.Store) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
		var args searchArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return appkitmcp.ErrorResult(appkitmcp.ErrValidation, "arguments: invalid JSON"), nil
		}
		limit := 50
		if args.Limit != nil {
			limit = *args.Limit
		}
		if limit < 1 || limit > 500 {
			return appkitmcp.ErrorResult(appkitmcp.ErrValidation, "limit must be between 1 and 500"), nil
		}
		if args.Kind != "" && !validKind(args.Kind) {
			return appkitmcp.ErrorResult(appkitmcp.ErrValidation, "kind is unknown"), nil
		}
		since, failure := normalizedArgument("since", args.Since)
		if failure != nil {
			return failure, nil
		}
		until, failure := normalizedArgument("until", args.Until)
		if failure != nil {
			return failure, nil
		}
		cursor, err := db.ParseCursor(args.Cursor)
		if err != nil {
			return appkitmcp.ErrorResult(appkitmcp.ErrValidation, "cursor is invalid"), nil
		}
		items, _, err := store.Search(ctx, db.Query{
			Since: since, Until: until, Service: args.Service, Kind: args.Kind,
			OwnerEmail: args.OwnerEmail, ClientID: args.ClientID, OpContains: args.OpContains,
			Status: args.Status, Error: args.Error, SHA256: args.SHA256, Limit: limit + 1, Cursor: cursor,
		})
		if err != nil {
			return appkitmcp.ErrorResult(appkitmcp.ErrInternal, "search failed"), nil
		}
		next := ""
		if len(items) > limit {
			items = items[:limit]
			last := items[len(items)-1]
			next = (db.Cursor{Time: last.Time, ID: last.ID}).String()
		}
		horizon, errResult := retentionHorizon(ctx, store)
		if errResult != nil {
			return errResult, nil
		}
		if items == nil {
			items = []record.Record{}
		}
		return appkitmcp.StructuredResult(searchResult{Records: items, NextCursor: next, RetentionHorizon: horizon})
	}
}

func chainHandler(store *db.Store) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	type argsType struct {
		CorrelationID string `json:"correlation_id"`
	}
	return func(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
		var args argsType
		if err := json.Unmarshal(raw, &args); err != nil || args.CorrelationID == "" {
			return appkitmcp.ErrorResult(appkitmcp.ErrValidation, "correlation_id is required"), nil
		}
		items, err := store.Chain(ctx, args.CorrelationID)
		if err != nil {
			return appkitmcp.ErrorResult(appkitmcp.ErrInternal, "chain failed"), nil
		}
		horizon, errResult := retentionHorizon(ctx, store)
		if errResult != nil {
			return errResult, nil
		}
		if items == nil {
			items = []record.Record{}
		}
		truncated := len(items) > 0 && horizon != "" && items[0].Time <= horizon
		return appkitmcp.StructuredResult(chainResult{Records: items, Count: len(items), RetentionHorizon: horizon, PossiblyTruncated: truncated})
	}
}

func getHandler(store *db.Store) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	type argsType struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
		var args argsType
		if err := json.Unmarshal(raw, &args); err != nil || args.ID == "" {
			return appkitmcp.ErrorResult(appkitmcp.ErrValidation, "id is required"), nil
		}
		item, found, err := store.Get(ctx, args.ID)
		if err != nil {
			return appkitmcp.ErrorResult(appkitmcp.ErrInternal, "get failed"), nil
		}
		if !found {
			return appkitmcp.ErrorResult(appkitmcp.ErrNotFound, fmt.Sprintf("record %q was not found", args.ID)), nil
		}
		return appkitmcp.StructuredResult(item)
	}
}

func normalizedArgument(name, value string) (string, map[string]any) {
	if value == "" {
		return "", nil
	}
	normalized, err := record.NormalizeTime(value)
	if err != nil {
		return "", appkitmcp.ErrorResult(appkitmcp.ErrValidation, name+" is not an RFC3339 timestamp")
	}
	return normalized, nil
}

func retentionHorizon(ctx context.Context, store *db.Store) (string, map[string]any) {
	horizon, _, err := store.Oldest(ctx)
	if err != nil {
		return "", appkitmcp.ErrorResult(appkitmcp.ErrInternal, "retention horizon failed")
	}
	return horizon, nil
}

func searchInputSchema() map[string]any {
	properties := map[string]any{}
	for _, name := range []string{"since", "until", "service", "owner_email", "client_id", "op_contains", "status", "error", "sha256", "cursor"} {
		properties[name] = stringSchema()
	}
	properties["kind"] = map[string]any{"type": "string", "enum": kindStrings()}
	properties["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 500, "default": 50}
	return objectSchema(properties)
}

func searchOutputSchema() map[string]any {
	return objectSchema(map[string]any{"records": arraySchema(recordSchema()), "next_cursor": stringSchema(), "retention_horizon": stringSchema()}, "records", "next_cursor", "retention_horizon")
}
func chainOutputSchema() map[string]any {
	return objectSchema(map[string]any{"records": arraySchema(recordSchema()), "count": map[string]any{"type": "integer"}, "retention_horizon": stringSchema(), "possibly_truncated": map[string]any{"type": "boolean"}}, "records", "count", "retention_horizon", "possibly_truncated")
}
func recordSchema() map[string]any {
	actor := objectSchema(map[string]any{"owner_email": stringSchema(), "client_id": stringSchema()})
	outcome := objectSchema(map[string]any{
		"status": stringSchema(), "error": stringSchema(), "duration_ms": map[string]any{"type": "integer"},
		"bytes": map[string]any{"type": "integer"}, "sha256": stringSchema(),
	})
	jsonObject := map[string]any{"type": "object", "additionalProperties": true}
	return objectSchema(map[string]any{
		"id": stringSchema(), "time": stringSchema(), "correlation_id": stringSchema(), "service": stringSchema(),
		"kind": map[string]any{"type": "string", "enum": kindStrings()}, "actor": actor, "op": stringSchema(),
		"params": jsonObject, "outcome": outcome, "detail": jsonObject,
	}, "id", "time", "correlation_id", "service", "kind", "actor", "op", "outcome")
}
func stringSchema() map[string]any { return map[string]any{"type": "string"} }
func arraySchema(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}
func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
func kindStrings() []string {
	return []string{string(record.KindEdge), string(record.KindRequest), string(record.KindOutbound), string(record.KindPublish), string(record.KindConsume), string(record.KindRoot), string(record.KindLifecycle)}
}
func validKind(value string) bool {
	for _, kind := range kindStrings() {
		if value == kind {
			return true
		}
	}
	return false
}
