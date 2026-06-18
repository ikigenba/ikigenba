// Package mcp implements a minimal MCP transport for the /mcp endpoint and the
// notify tool surface.
//
// notify exposes two MCP tools — health (the end-to-end auth proof) and
// reflection (its event-plane edges). notify is an event-plane consumer; the
// real work happens in the background consumer loop, not over MCP.
//
// The transport speaks JSON-RPC 2.0 over plain HTTP POST (no SSE/streaming),
// responding with Content-Type: application/json. It carries NO token logic:
// nginx introspects every request via auth_request against the dashboard's
// authorization server and injects X-Owner-Email / X-Client-Id authoritatively
// before forwarding here. The handler is mounted behind the server's
// requireIdentityHeaders gate, so by the time a request arrives the caller
// identity is already established. There is intentionally no bearer parsing, no
// token store, no rate limiter, and no WWW-Authenticate / 401 / 429 emission in
// this package — that all lives in the dashboard.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	"eventplane/consumer"
	"eventplane/outbox"

	"notify/internal/push"
)

// Identity is the authenticated caller, as told to us authoritatively by nginx
// (via the server's requireIdentityHeaders gate) through request headers.
type Identity struct {
	OwnerEmail string
	ClientID   string
}

// Handler is the http.Handler for POST /mcp. It is constructed at wiring time
// with the health-envelope inputs (version, service, optional reporter) threaded
// from appkit's Router accessors plus the push client backing the send verb, and
// dispatches JSON-RPC methods. notify holds no MCP-side domain *store*; its
// reactive domain runs in the background consumer loop, while the proactive send
// verb publishes through the same push client (plan-notify-mcp-send.md §7).
type Handler struct {
	version       string
	service       string
	health        func(context.Context) (map[string]any, error)
	events        outbox.Registry
	subscriptions func() []consumer.Subscription
	push          *push.Client
}

// NewHandler builds a Handler. version/service/health populate the
// health envelope; health is the optional per-service reporter
// (nil → details is {}). events is the published-event registry (empty for
// notify, a consumer-only service) and subscriptions the live subscription
// provider, both rendered by reflection. push is the ntfy client backing the
// send verb; following crm's non-nil-service discipline a nil push client is a
// wiring error and panics here rather than deferring a nil dereference to the
// first send.
func NewHandler(version, service string,
	health func(context.Context) (map[string]any, error),
	events outbox.Registry, subscriptions func() []consumer.Subscription,
	pushClient *push.Client) *Handler {
	if pushClient == nil {
		panic("mcp: push client is required")
	}
	return &Handler{
		version:       version,
		service:       service,
		health:        health,
		events:        events,
		subscriptions: subscriptions,
		push:          pushClient,
	}
}

// ServeHTTP dispatches a single JSON-RPC 2.0 request. Identity is read from the
// nginx-injected headers (always present behind requireIdentityHeaders).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := Identity{
		OwnerEmail: r.Header.Get("X-Owner-Email"),
		ClientID:   r.Header.Get("X-Client-Id"),
	}
	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, -32700, "parse error")
		return
	}
	switch req.Method {
	case "initialize":
		writeJSONRPCResult(w, req.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "Notify", "version": "1"},
			"instructions": "Push notifications to the owner's device. Call send to push a " +
				"notification proactively. notify is also an event-plane consumer that pushes " +
				"automatically for each subscribed event — check health for status and reflection " +
				"for what it subscribes to.",
		})
	case "notifications/initialized":
		// fire-and-forget notification — no response per JSON-RPC.
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeJSONRPCResult(w, req.ID, map[string]any{"tools": toolDescriptors()})
	case "tools/call":
		h.handleToolCall(r.Context(), w, req, id)
	default:
		writeJSONRPCError(w, req.ID, -32601, "method not found")
	}
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func writeJSONRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      idOrNull(id),
		"result":  result,
	})
}

func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      idOrNull(id),
		"error":   map[string]any{"code": code, "message": msg},
	})
}

func idOrNull(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	return json.RawMessage(id)
}

// Result-shape helpers for tool calls. MCP `tools/call` returns
// {content: [{type: "text", text: "..."}], isError?: bool}.
func toolResultText(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func toolResultErr(msg string) map[string]any {
	return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": msg}}}
}
