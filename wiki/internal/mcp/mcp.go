// Package mcp implements wiki's initial JSON-RPC MCP smoke surface.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	"appkit"
)

// Handler serves a small MCP-compatible JSON-RPC endpoint for Phase 01.
type Handler struct {
	version string
	service string
	health  func(context.Context) (map[string]any, error)
}

// NewHandler builds the MCP handler from appkit's route-time service metadata.
func NewHandler(version, service string, health func(context.Context) (map[string]any, error)) *Handler {
	return &Handler{version: version, service: service, health: health}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nil, -32700, "parse error")
		return
	}

	switch req.Method {
	case "initialize":
		writeResult(w, req.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "Wiki", "version": h.version},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeResult(w, req.ID, map[string]any{"tools": []map[string]any{healthTool()}})
	case "tools/call":
		h.handleToolCall(r.Context(), w, req)
	default:
		writeError(w, req.ID, -32601, "method not found")
	}
}

func (h *Handler) handleToolCall(ctx context.Context, w http.ResponseWriter, req request) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeError(w, req.ID, -32602, "invalid params")
		return
	}
	if params.Name != "ikigenba_wiki_health" {
		writeResult(w, req.ID, toolError("unknown tool"))
		return
	}
	details := map[string]any{}
	if h.health != nil {
		d, err := h.health(ctx)
		if err != nil {
			details = map[string]any{"error": err.Error()}
		} else if d != nil {
			details = d
		}
	}
	env := appkit.Envelope(h.version, h.service, details)
	b, err := json.Marshal(env)
	if err != nil {
		writeResult(w, req.ID, toolError(err.Error()))
		return
	}
	writeResult(w, req.ID, toolText(string(b)))
}

func healthTool() map[string]any {
	return map[string]any{
		"name":        "ikigenba_wiki_health",
		"description": "Report wiki service health.",
		"inputSchema": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		},
	}
}

type request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      idOrNull(id),
		"result":  result,
	})
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
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
	return id
}

func toolText(text string) map[string]any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
}

func toolError(text string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]string{{"type": "text", "text": text}},
	}
}
