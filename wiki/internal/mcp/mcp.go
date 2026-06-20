// Package mcp implements wiki's initial JSON-RPC MCP smoke surface.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"appkit"
)

// Handler serves a small MCP-compatible JSON-RPC endpoint for Phase 01.
type Handler struct {
	version string
	service string
	health  func(context.Context) (map[string]any, error)
	ingest  func(context.Context, string, string, string, []string) (string, error)
	status  func(context.Context, string) (any, error)
	ask     func(context.Context, string, string) (any, error)
}

type ingestService interface {
	Ingest(ctx context.Context, owner, text, title string, tags []string) (string, error)
}

type jobStatusFunc[T any] interface {
	JobStatus(ctx context.Context, jobID string) (T, error)
}

// Option configures optional MCP tools backed by wiki domain services.
type Option func(*Handler)

// WithIngestService enables the ingest tool.
func WithIngestService(s ingestService) Option {
	return func(h *Handler) {
		if s != nil {
			h.ingest = s.Ingest
		}
	}
}

// WithJobStatusService enables the job-status tool.
func WithJobStatusService[T any](s jobStatusFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.status = func(ctx context.Context, jobID string) (any, error) {
				return s.JobStatus(ctx, jobID)
			}
		}
	}
}

// WithAskFunc enables the grounded ask tool.
func WithAskFunc[T any](ask func(context.Context, string, string) (T, error)) Option {
	return func(h *Handler) {
		if ask != nil {
			h.ask = func(ctx context.Context, owner, question string) (any, error) {
				return ask(ctx, owner, question)
			}
		}
	}
}

// NewHandler builds the MCP handler from appkit's route-time service metadata.
func NewHandler(version, service string, health func(context.Context) (map[string]any, error), opts ...Option) *Handler {
	h := &Handler{version: version, service: service, health: health}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
		writeResult(w, req.ID, map[string]any{"tools": h.tools()})
	case "tools/call":
		h.handleToolCall(r.Context(), w, req)
	default:
		writeError(w, req.ID, -32601, "method not found")
	}
}

func (h *Handler) handleToolCall(ctx context.Context, w http.ResponseWriter, req request) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeError(w, req.ID, -32602, "invalid params")
		return
	}
	switch params.Name {
	case "ikigenba_wiki_health":
		h.handleHealthCall(ctx, w, req)
	case "ikigenba_wiki_ingest_text":
		h.handleIngestCall(ctx, w, req, params.Arguments)
	case "ikigenba_wiki_job_status":
		h.handleJobStatusCall(ctx, w, req, params.Arguments)
	case "ikigenba_wiki_ask":
		h.handleAskCall(ctx, w, req, params.Arguments)
	default:
		writeResult(w, req.ID, toolError("unknown tool"))
		return
	}
}

func (h *Handler) handleHealthCall(ctx context.Context, w http.ResponseWriter, req request) {
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

func (h *Handler) handleIngestCall(ctx context.Context, w http.ResponseWriter, req request, raw json.RawMessage) {
	if h.ingest == nil {
		writeResult(w, req.ID, toolError("ingest tool is not configured"))
		return
	}
	id, ok := appkit.IdentityFrom(ctx)
	if !ok {
		writeResult(w, req.ID, toolError("missing authenticated identity"))
		return
	}
	var args struct {
		Text  string   `json:"text"`
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		writeResult(w, req.ID, toolError(err.Error()))
		return
	}
	if strings.TrimSpace(args.Text) == "" {
		writeResult(w, req.ID, toolError("text is required"))
		return
	}
	jobID, err := h.ingest(ctx, id.OwnerEmail, args.Text, args.Title, args.Tags)
	if err != nil {
		writeResult(w, req.ID, toolError(err.Error()))
		return
	}
	writeJSONTextResult(w, req.ID, map[string]string{"job_id": jobID})
}

func (h *Handler) handleJobStatusCall(ctx context.Context, w http.ResponseWriter, req request, raw json.RawMessage) {
	if h.status == nil {
		writeResult(w, req.ID, toolError("job_status tool is not configured"))
		return
	}
	var args struct {
		JobID string `json:"job_id"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		writeResult(w, req.ID, toolError(err.Error()))
		return
	}
	if strings.TrimSpace(args.JobID) == "" {
		writeResult(w, req.ID, toolError("job_id is required"))
		return
	}
	status, err := h.status(ctx, args.JobID)
	if err != nil {
		writeResult(w, req.ID, toolError(err.Error()))
		return
	}
	writeJSONTextResult(w, req.ID, status)
}

func (h *Handler) handleAskCall(ctx context.Context, w http.ResponseWriter, req request, raw json.RawMessage) {
	if h.ask == nil {
		writeResult(w, req.ID, toolError("ask tool is not configured"))
		return
	}
	id, ok := appkit.IdentityFrom(ctx)
	if !ok {
		writeResult(w, req.ID, toolError("missing authenticated identity"))
		return
	}
	var args struct {
		Question string `json:"question"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		writeResult(w, req.ID, toolError(err.Error()))
		return
	}
	if strings.TrimSpace(args.Question) == "" {
		writeResult(w, req.ID, toolError("question is required"))
		return
	}
	answer, err := h.ask(ctx, id.OwnerEmail, args.Question)
	if err != nil {
		writeResult(w, req.ID, toolError(err.Error()))
		return
	}
	writeJSONTextResult(w, req.ID, answer)
}

func (h *Handler) tools() []map[string]any {
	tools := []map[string]any{healthTool()}
	if h.ingest != nil {
		tools = append(tools, ingestTool())
	}
	if h.status != nil {
		tools = append(tools, jobStatusTool())
	}
	if h.ask != nil {
		tools = append(tools, askTool())
	}
	return tools
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

func ingestTool() map[string]any {
	return map[string]any{
		"name":        "ikigenba_wiki_ingest_text",
		"description": "Queue source text for wiki ingestion.",
		"inputSchema": objectSchema(map[string]any{
			"text":  map[string]any{"type": "string"},
			"title": map[string]any{"type": "string"},
			"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, []string{"text"}),
	}
}

func jobStatusTool() map[string]any {
	return map[string]any{
		"name":        "ikigenba_wiki_job_status",
		"description": "Return the status of a wiki ingest job.",
		"inputSchema": objectSchema(map[string]any{
			"job_id": map[string]any{"type": "string"},
		}, []string{"job_id"}),
	}
}

func askTool() map[string]any {
	return map[string]any{
		"name":        "ikigenba_wiki_ask",
		"description": "Answer a question using the authenticated owner's wiki.",
		"inputSchema": objectSchema(map[string]any{
			"question": map[string]any{"type": "string"},
		}, []string{"question"}),
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
}

type request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
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

func writeJSONTextResult(w http.ResponseWriter, id json.RawMessage, value any) {
	b, err := json.Marshal(value)
	if err != nil {
		writeResult(w, id, toolError(err.Error()))
		return
	}
	writeResult(w, id, toolText(string(b)))
}

func toolError(text string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]string{{"type": "text", "text": text}},
	}
}
