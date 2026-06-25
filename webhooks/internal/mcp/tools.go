package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"appkit"

	"eventplane/outbox"

	"webhooks/internal/db"
)

// toolPrefix brands every MCP tool name. The webhooks surface uses bare verbs
// (empty prefix) — branding is applied by the suite at wiring, not here.
const toolPrefix = ""

// tool returns the (un)branded MCP tool name. Used by BOTH toolDescriptors and
// dispatchTool so the two sites cannot drift.
func tool(verb string) string { return toolPrefix + verb }

// toolDescriptors returns the fixed owner-facing tool surface: the four owner
// verbs (create / list / delete / rotate) plus the standard health and reflection
// verbs. Tool count is a function of verbs, not entities.
func toolDescriptors() []map[string]any {
	return []map[string]any{
		desc(tool("create"),
			"Provision a new inbound webhook owned by you. Omit 'name' for a freshly-generated opaque name, or pass a name matching ^[A-Za-z0-9_-]{1,64}$. Returns the webhook's trigger_url and a show-once signing secret (prefix ms_wh_) — the secret is shown ONLY here, never again, so capture it now.",
			obj(map[string]any{
				"name": descTyp("string", "optional; ^[A-Za-z0-9_-]{1,64}$. Omit for a generated name."),
			})),
		desc(tool("list"),
			"List your webhooks (owner-scoped — only your own). Each entry has name, trigger_url, created_at, and last_triggered_at (null until first fired). Secrets are never returned by list.",
			obj(map[string]any{})),
		desc(tool("delete"),
			"Delete one of your webhooks by name. Owner-scoped: a name you do not own returns not_found and changes nothing. Returns {deleted:true} on success.",
			obj(map[string]any{"name": typ("string")}, "name")),
		desc(tool("rotate"),
			"Issue a fresh show-once signing secret for one of your webhooks. The name and trigger_url are unchanged; the previous secret stops verifying immediately. Owner-scoped: a name you do not own returns not_found.",
			obj(map[string]any{"name": typ("string")}, "name")),
		desc(tool("health"),
			"Health + diagnostics for the webhooks service. Returns the fixed envelope (status, version, service, details) plus the authenticated caller's identity (owner_email, client_id). Takes no inputs.",
			obj(map[string]any{})),
		desc(tool("reflection"),
			"Self-describe webhooks's edges in the event graph. With no arguments, returns the index {publishes:[{type,description}], subscribes:[...]} — webhooks is a producer, so subscribes is empty. Pass 'event_type' (a published type) for its detail {type, description, schema, example}.",
			obj(map[string]any{
				"event_type": descTyp("string", "optional; a published event type to fetch the schema+example detail for"),
			})),
	}
}

func desc(name, description string, schema map[string]any) map[string]any {
	return map[string]any{"name": name, "description": description, "inputSchema": schema}
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func typ(t string) map[string]any { return map[string]any{"type": t} }

func descTyp(t, description string) map[string]any {
	return map[string]any{"type": t, "description": description}
}

// ── dispatch ──────────────────────────────────────────────────────────────

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (h *Handler) handleToolCall(ctx context.Context, w http.ResponseWriter, req jsonRPCRequest, id Identity) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeJSONRPCError(w, req.ID, -32602, "invalid params")
		return
	}
	res, err := h.dispatchTool(ctx, p.Name, p.Arguments, id)
	if err != nil {
		writeJSONRPCResult(w, req.ID, toolResultErr(err.Error()))
		return
	}
	writeJSONRPCResult(w, req.ID, res)
}

func (h *Handler) dispatchTool(ctx context.Context, name string, argsRaw json.RawMessage, id Identity) (map[string]any, error) {
	switch name {
	case tool("create"):
		return h.toolCreate(ctx, argsRaw, id)
	case tool("list"):
		return h.toolList(ctx, id)
	case tool("delete"):
		return h.toolDelete(ctx, argsRaw, id)
	case tool("rotate"):
		return h.toolRotate(ctx, argsRaw, id)
	case tool("health"):
		return h.toolHealth(ctx, id)
	case tool("reflection"):
		return h.toolReflection(argsRaw)
	default:
		return nil, errors.New("unknown tool: " + name)
	}
}

// ── tool implementations ─────────────────────────────────────────────────

// triggerURL renders a webhook's public POST endpoint: baseURL (trailing slash)
// + "in/" + name. Both create/rotate and list go through this one site so the
// rendered URL cannot drift between mint and read.
func (h *Handler) triggerURL(name string) string { return h.baseURL + "in/" + name }

// webhookView is the secret-free projection of a stored webhook returned by list:
// name, trigger_url, created_at, last_triggered_at (null until first fired). It
// deliberately omits any secret material.
func (h *Handler) webhookView(wh db.Webhook) map[string]any {
	v := map[string]any{
		"name":              wh.Name,
		"trigger_url":       h.triggerURL(wh.Name),
		"created_at":        wh.CreatedAt.UTC().Format(time.RFC3339Nano),
		"last_triggered_at": nil,
	}
	if wh.LastTriggeredAt != nil {
		v["last_triggered_at"] = wh.LastTriggeredAt.UTC().Format(time.RFC3339Nano)
	}
	return v
}

// toolCreate provisions a webhook owned by the authenticated caller. The
// plaintext secret is surfaced exactly once here (prefix ms_wh_); only its hash
// is persisted. An empty name is server-generated.
func (h *Handler) toolCreate(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Name string `json:"name,omitempty"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
	}
	wh, secret, err := h.svc.Create(ctx, id.OwnerEmail, a.Name)
	if err != nil {
		return toolErr(err), nil
	}
	out := h.webhookView(wh)
	out["secret"] = secret // show-once
	return toolResultJSON(out)
}

// toolList returns the caller's own webhooks, owner-scoped, secret-free.
func (h *Handler) toolList(ctx context.Context, id Identity) (map[string]any, error) {
	whs, err := h.svc.List(ctx, id.OwnerEmail)
	if err != nil {
		return toolErr(err), nil
	}
	items := make([]map[string]any, 0, len(whs))
	for _, wh := range whs {
		items = append(items, h.webhookView(wh))
	}
	return toolResultJSON(map[string]any{"items": items})
}

// toolDelete removes the caller's webhook by name. A name the caller does not own
// (deleted==false) is reported as not_found, mutating nothing.
func (h *Handler) toolDelete(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	deleted, err := h.svc.Delete(ctx, id.OwnerEmail, a.Name)
	if err != nil {
		return toolErr(err), nil
	}
	if !deleted {
		return toolErr(webhooksNotFound()), nil
	}
	return toolResultJSON(map[string]any{"deleted": true})
}

// toolRotate issues a fresh show-once secret for the caller's webhook. The name
// and trigger_url are unchanged. A missing or not-owned name maps to not_found.
func (h *Handler) toolRotate(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	secret, err := h.svc.Rotate(ctx, id.OwnerEmail, a.Name)
	if err != nil {
		return toolErr(err), nil
	}
	return toolResultJSON(map[string]any{
		"name":        a.Name,
		"trigger_url": h.triggerURL(a.Name),
		"secret":      secret, // show-once
	})
}

// toolHealth renders the shared health envelope (status/version/service/details)
// via appkit.Envelope and then adds the authenticated caller's identity — the
// gated, MCP-side variant of the health surface. webhooks supplies no reporter
// unless wired, so details renders as {} by default.
func (h *Handler) toolHealth(ctx context.Context, id Identity) (map[string]any, error) {
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
	env["owner_email"] = id.OwnerEmail
	env["client_id"] = id.ClientID
	return toolResultJSON(env)
}

// toolReflection self-describes webhooks's edges in the event graph. No
// event_type → the index {publishes, subscribes}; with event_type → that
// published type's {type, description, schema, example}. An unknown event_type
// returns a corrective error listing the valid types, not an empty result.
// webhooks is a pure producer, so subscribes is always empty.
func (h *Handler) toolReflection(raw json.RawMessage) (map[string]any, error) {
	var a struct {
		EventType string `json:"event_type,omitempty"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
	}
	if a.EventType != "" {
		detail, err := h.events.Detail(a.EventType)
		if err != nil {
			var unknown *outbox.UnknownEventTypeError
			if errors.As(err, &unknown) {
				return toolResultErr(reflectionUnknownTypeError(unknown)), nil
			}
			return nil, err
		}
		return toolResultJSON(detail)
	}
	return toolResultJSON(map[string]any{
		"publishes":  h.events.Index(),
		"subscribes": []map[string]any{},
	})
}

// reflectionUnknownTypeError renders the corrective error envelope for an unknown
// event_type, listing the valid types so the agent can self-correct.
func reflectionUnknownTypeError(e *outbox.UnknownEventTypeError) string {
	env := map[string]any{"error": map[string]any{
		"code":    "unknown_event_type",
		"message": "unknown event_type " + e.Type + "; valid types: " + strings.Join(e.Valid, ", "),
	}}
	b, _ := json.Marshal(env)
	return string(b)
}

// webhooksNotFound returns the domain not-found sentinel so delete's "no row
// matched" path renders the same not_found envelope as the Service's own
// ErrNotFound (used by rotate). One sentinel, one envelope code.
func webhooksNotFound() error { return errNotFound }

// ── shared helpers ──────────────────────────────────────────────────────

func toolResultJSON(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return toolResultText(string(b)), nil
}
