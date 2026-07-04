// Package mcp implements github's JSON-RPC 2.0 MCP tool surface.
package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	gh "github/internal/gh"
)

// Identity is the authenticated caller injected by nginx and appkit's identity gate.
type Identity struct {
	OwnerEmail string
	ClientID   string
}

// GitHubClient is the slice of the GitHub client driven by the MCP tools.
type GitHubClient interface {
	ReposList(ctx context.Context) ([]gh.Repo, error)
	RepoGet(ctx context.Context, repo string) (gh.Repo, error)
	PRList(ctx context.Context, repo, state string) ([]gh.PR, error)
	PRGet(ctx context.Context, repo string, number int) (gh.PRDetail, error)
	PRComment(ctx context.Context, repo string, number int, body string) (gh.Comment, error)
	PRReview(ctx context.Context, repo string, number int, event, body string) (gh.Review, error)
	PRMerge(ctx context.Context, repo string, number int, method string) (gh.MergeResult, error)
	IssueList(ctx context.Context, repo, state string) ([]gh.Issue, error)
	IssueGet(ctx context.Context, repo string, number int) (gh.Issue, error)
	IssueCreate(ctx context.Context, repo, title, body string) (gh.Issue, error)
	IssueComment(ctx context.Context, repo string, number int, body string) (gh.Comment, error)
	IssueUpdate(ctx context.Context, repo string, number int, patch gh.IssuePatch) (gh.Issue, error)
	FileGet(ctx context.Context, repo, path, ref string) (gh.FileContent, error)
	FilePut(ctx context.Context, repo, path string, in gh.FilePut) (gh.FileCommit, error)
}

// Handler is the POST /mcp JSON-RPC handler.
type Handler struct {
	client  GitHubClient
	version string
	service string
	health  func(context.Context) (map[string]any, error)
	logger  *slog.Logger
}

// NewHandler builds a Handler.
func NewHandler(client GitHubClient, version, service string, health func(context.Context) (map[string]any, error), logger *slog.Logger) *Handler {
	if client == nil {
		panic("mcp: github client is required")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{client: client, version: version, service: service, health: health, logger: logger}
}

// ServeHTTP dispatches one JSON-RPC 2.0 request.
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
			"serverInfo":      map[string]any{"name": "github", "version": h.version},
		})
	case "notifications/initialized":
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

func toolResultText(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func toolResultErr(msg string) map[string]any {
	return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": msg}}}
}
