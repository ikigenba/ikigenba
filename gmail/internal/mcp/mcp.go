// Package mcp implements a minimal MCP transport for the /mcp endpoint and the
// ikigenba_gmail_* tool surface.
//
// gmail is a connector + event-plane producer (gmail-connector-decisions §1).
// This is the P1 STUB surface: it exposes only the two chassis tools —
// ikigenba_gmail_health (the end-to-end auth proof + health envelope) and
// ikigenba_gmail_reflection (self-describes the three mail.* events the producer
// will emit). The full normal-mailbox tool set (list/read/thread/send/draft/
// labels/label/unlabel/trash/delete) lands in P4; the producer that actually
// emits the mail.* events lands in P3. The stub lets the binary serve and the
// dashboard inventory list it (MCP=true) before either of those phases.
//
// The transport speaks JSON-RPC 2.0 over plain HTTP POST (no SSE/streaming),
// responding with Content-Type: application/json. It carries NO token logic:
// nginx introspects every request via auth_request against the dashboard's
// authorization server and injects X-Owner-Email / X-Client-Id authoritatively
// before forwarding here. The handler is mounted behind the server's
// RequireIdentity gate, so by the time a request arrives the caller identity is
// already established. There is intentionally no bearer parsing, no token store,
// no rate limiter, and no WWW-Authenticate / 401 / 429 emission in this package —
// that all lives in the dashboard.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	"eventplane/consumer"
	"eventplane/outbox"
)

// Identity is the authenticated caller, as told to us authoritatively by nginx
// (via the server's RequireIdentity gate) through request headers.
type Identity struct {
	OwnerEmail string
	ClientID   string
}

// Handler is the http.Handler for POST /mcp. It is constructed once at wiring
// time with the health-envelope inputs (version, service, optional reporter) and
// the event-graph inputs (events registry, subscriptions provider) threaded from
// appkit's Router accessors, and dispatches JSON-RPC methods. The P1 stub holds
// no domain service (gmail has none yet — that arrives in P2/P3).
type Handler struct {
	version       string
	service       string
	health        func(context.Context) (map[string]any, error)
	events        outbox.Registry
	subscriptions func() []consumer.Subscription
}

// NewHandler builds a Handler. version/service/health populate the
// ikigenba_gmail_health envelope; health is gmail's optional per-service
// reporter (nil in P1 — gmail has no telemetry yet). events is the
// published-event registry and subscriptions the live subscription provider,
// both rendered by ikigenba_gmail_reflection. A nil events registry falls back
// to gmail's static mail.* registry so reflection always describes the producer
// even before appkit threads Spec.Events.
func NewHandler(version, service string,
	health func(context.Context) (map[string]any, error),
	events outbox.Registry, subscriptions func() []consumer.Subscription) *Handler {
	if events == nil {
		events = Events
	}
	return &Handler{
		version:       version,
		service:       service,
		health:        health,
		events:        events,
		subscriptions: subscriptions,
	}
}

// ServeHTTP dispatches a single JSON-RPC 2.0 request. Identity is read from the
// nginx-injected headers (always present behind RequireIdentity).
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
			"serverInfo":      map[string]any{"name": "Gmail", "version": "1"},
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
