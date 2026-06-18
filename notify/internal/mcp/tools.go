package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"appkit"

	"eventplane/consumer"
	"eventplane/outbox"

	"notify/internal/push"
)

// toolPrefix brands every MCP tool name (DECISIONS §1). It is the suite name
// ikigenba + the service name; HTTP route paths are NOT branded.
const toolPrefix = ""

// tool returns the branded, fully-qualified MCP tool name. Used by BOTH
// toolDescriptors and dispatchTool so the two sites cannot drift.
func tool(verb string) string { return toolPrefix + verb }

// toolDescriptors returns the notify tool set: send, health, and reflection.
// send is notify's one write verb — the whole domain is "push a notification", so
// it is exactly one tool (plan-notify-mcp-send.md §1); deferred ntfy features
// become fields, never new tools. health/reflection are the read-only chassis
// tools. Schemas are hand-coded; a full JSON Schema isn't required by MCP clients
// but improves the LLM hinting.
func toolDescriptors() []map[string]any {
	return []map[string]any{
		desc(tool("send"),
			"Push a notification to the owner's device. 'message' (required) is the body. "+
				"Optional: 'title' (a short headline); 'priority' (one of min|low|default|high|urgent; "+
				"drives device alerting, default 'default'); 'tags' (an array of strings — known emoji "+
				"shortcodes like \"warning\" or \"white_check_mark\" render as leading emoji, others as "+
				"text labels); 'click' (an absolute URL opened when the owner taps the notification). "+
				"The topic is fixed server-side. Returns {ok:true} once ntfy accepts the push (delivery "+
				"to the device is not guaranteed and is not reported).",
			obj(map[string]any{
				"message":  descTyp("string", "the notification body; required and non-empty"),
				"title":    descTyp("string", "optional short headline"),
				"priority": enumTyp("string", "min", "low", "default", "high", "urgent"),
				"tags":     map[string]any{"type": "array", "items": typ("string"), "description": "optional ntfy tags; emoji shortcodes render as icons, others as labels"},
				"click":    descTyp("string", "optional absolute URL opened when the owner taps the notification"),
			}, "message")),
		desc(tool("health"), "Health + diagnostics for the notify service. Returns the fixed envelope (status, version, service, details) plus the authenticated caller's identity (owner_email, client_id). Takes no inputs.", obj(map[string]any{})),
		desc(tool("reflection"),
			"Self-describe notify's edges in the event graph. With no arguments, returns the index {publishes:[{type,description}], subscribes:[{source,filter,description}]} — notify is a consumer, so publishes is empty. Resolve a subscribed edge's payload shape by calling the source service's reflection tool.",
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

func enumTyp(t string, vals ...string) map[string]any {
	return map[string]any{"type": t, "enum": vals}
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
	case tool("send"):
		return h.toolSend(ctx, argsRaw)
	case tool("health"):
		return h.toolHealth(ctx, id)
	case tool("reflection"):
		return h.toolReflection(argsRaw)
	default:
		return nil, errors.New("unknown tool: " + name)
	}
}

// ── tool implementations ─────────────────────────────────────────────────

// toolHealth renders the shared health envelope (status/version/service/details)
// via appkit.Envelope and then adds the authenticated caller's identity — the
// end-to-end auth-chain proof (DECISIONS §6). notify supplies no reporter, so
// details renders as {}.
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
	env := appkit.Envelope(h.version, h.service, details) // status/version/service/details
	env["owner_email"] = id.OwnerEmail
	env["client_id"] = id.ClientID
	return toolResultJSON(env)
}

// toolReflection self-describes notify's edges in the event graph (the
// reflection tool). No event_type → the index {publishes,
// subscribes}; notify is a consumer, so publishes is empty and subscribes lists
// its one crm/contact.created in-edge. With event_type → that published type's
// detail; against notify's empty registry every type is unknown, so it returns
// the corrective error (the ledger bad_root pattern) with an empty valid list.
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
		"subscribes": renderSubscriptions(h.subscriptions),
	})
}

// renderSubscriptions flattens the live subscription provider to the reflection
// in-edges: one {source, filter, description} per Subscription. The Handler is
// dropped — only the declared graph edge is reported. A nil provider (or nil
// result) renders as an empty list.
func renderSubscriptions(provider func() []consumer.Subscription) []map[string]any {
	out := []map[string]any{}
	if provider == nil {
		return out
	}
	for _, s := range provider() {
		out = append(out, map[string]any{
			"source":      s.Source,
			"filter":      s.Filter,
			"description": s.Description,
		})
	}
	return out
}

// reflectionUnknownTypeError renders the corrective error envelope for an unknown
// event_type, listing the valid types so the agent can self-correct (mirrors
// ledger's bad_root corrective message).
func reflectionUnknownTypeError(e *outbox.UnknownEventTypeError) string {
	env := map[string]any{"error": map[string]any{
		"code":    "unknown_event_type",
		"message": "unknown event_type " + e.Type + "; valid types: " + strings.Join(e.Valid, ", "),
	}}
	b, _ := json.Marshal(env)
	return string(b)
}

// toolSend is notify's one write verb: it publishes a single notification to the
// owner's fixed ntfy topic and reports the real outcome synchronously
// (plan-notify-mcp-send.md §4). It is deliberately NOT best-effort like the
// consumer path — an explicit send tells the caller whether ntfy accepted the
// push. Caller errors render as a `validation` envelope (so the agent
// self-corrects); an ntfy rejection or unreachable server renders as a generic
// `upstream` envelope that never leaks the topic or token.
func (h *Handler) toolSend(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Message  string   `json:"message"`
		Title    string   `json:"title,omitempty"`
		Priority string   `json:"priority,omitempty"`
		Tags     []string `json:"tags,omitempty"`
		Click    string   `json:"click,omitempty"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(a.Message) == "" {
		return validationErr("message", "message is required and must be non-empty"), nil
	}
	prio, err := mapPriority(a.Priority)
	if err != nil {
		return validationErr("priority", err.Error()), nil
	}
	if a.Click != "" {
		if err := validateClick(a.Click); err != nil {
			return validationErr("click", err.Error()), nil
		}
	}
	if err := h.push.Publish(ctx, push.Notification{
		Message:  a.Message,
		Title:    a.Title,
		Priority: prio,
		Tags:     a.Tags,
		Click:    a.Click,
	}); err != nil {
		// Generic upstream failure — never surface the topic/token or the raw ntfy
		// error (the secrets hard rule); the agent only needs to know it did not land.
		return upstreamErr(), nil
	}
	return toolResultJSON(map[string]any{"ok": true})
}

// mapPriority translates the send verb's string enum to ntfy's numeric priority
// (plan-notify-mcp-send.md §2). An empty value is "unset" → 0, which Publish maps
// to an omitted header (ntfy's own default). Any other value is a caller error
// whose message lists the valid choices so the agent self-corrects.
func mapPriority(s string) (int, error) {
	switch s {
	case "":
		return 0, nil
	case "min":
		return 1, nil
	case "low":
		return 2, nil
	case "default":
		return 3, nil
	case "high":
		return 4, nil
	case "urgent":
		return 5, nil
	default:
		return 0, fmt.Errorf("priority must be one of min, low, default, high, urgent")
	}
}

// validateClick enforces the §5 rule: a well-formed ABSOLUTE URL (any scheme the
// device may understand — https, mailto, tel, app deep-links). It is intentionally
// light — we reject only what is clearly not a URL and otherwise pass it through.
func validateClick(s string) error {
	u, err := url.Parse(s)
	if err != nil || !u.IsAbs() {
		return fmt.Errorf("click must be a well-formed absolute URL")
	}
	return nil
}

// validationErr / upstreamErr render the two closed-vocabulary error envelopes
// (plan-notify-mcp-send.md §5) into a tool-call error result, mirroring crm's
// errorEnvelope/toolErr pattern. field is omitted when empty.
func validationErr(field, message string) map[string]any {
	e := map[string]any{"code": "validation", "message": message}
	if field != "" {
		e["field"] = field
	}
	b, _ := json.Marshal(map[string]any{"error": e})
	return toolResultErr(string(b))
}

func upstreamErr() map[string]any {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{
		"code":    "upstream",
		"message": "the notification service rejected the request or was unreachable",
	}})
	return toolResultErr(string(b))
}

// ── shared helpers ──────────────────────────────────────────────────────

func toolResultJSON(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return toolResultText(string(b)), nil
}
