package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"appkit"

	"eventplane/consumer"
	"eventplane/outbox"
)

// toolPrefix brands every MCP tool name (DECISIONS §1). It is the suite name
// ikigenba + the service name; HTTP route paths are NOT branded.
const toolPrefix = "ikigenba_gmail_"

// tool returns the branded, fully-qualified MCP tool name. Used by BOTH
// toolDescriptors and dispatchTool so the two sites cannot drift.
func tool(verb string) string { return toolPrefix + verb }

// ── published events (producer half) ─────────────────────────────────────────

// Mail event type names (decisions §1). Emission lands in P3; the registry is
// declared here in P1 so the reflection tool already self-describes the producer.
const (
	EventMailReceived = "mail.received"
	EventMailSent     = "mail.sent"
	EventMailDeleted  = "mail.deleted"
)

// mailReceivedPayload is the wire shape of a mail.received event (decisions §1
// table): an inbound message that landed in INBOX.
type mailReceivedPayload struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id"`
	From       string `json:"from"`
	Subject    string `json:"subject"`
	Snippet    string `json:"snippet"`
	ReceivedAt string `json:"received_at"`
}

// mailSentPayload is the wire shape of a mail.sent event: a message that carries
// SENT (and not INBOX) — our own sends, whether via MCP or the Gmail UI.
type mailSentPayload struct {
	ID       string `json:"id"`
	ThreadID string `json:"thread_id"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	Snippet  string `json:"snippet"`
	SentAt   string `json:"sent_at"`
}

// mailDeletedPayload is the wire shape of a mail.deleted event: a message moved
// to Trash (labelsAdded: TRASH), not a permanent expunge.
type mailDeletedPayload struct {
	ID        string `json:"id"`
	ThreadID  string `json:"thread_id"`
	Subject   string `json:"subject"`
	DeletedAt string `json:"deleted_at"`
}

// Events is the published-event Registry for the reflection tool and (in P3)
// Append-time validation, wired via Spec.Events. The three mail.* types are the
// producer's complete published set (decisions §1). Each entry carries a
// filled-in Sample of its real payload struct — the single source for both the
// reflected JSON Schema and the worked example, so schema/example/wire shape
// can't diverge.
var Events = outbox.Registry{
	{
		Type:        EventMailReceived,
		Description: "An inbound message arrived in the mailbox (Gmail History messagesAdded carrying the INBOX label). Carries message identity + envelope headers; fetch the full message via the read tool.",
		Sample: mailReceivedPayload{
			ID:         "18f2a1b3c4d5e6f7",
			ThreadID:   "18f2a1b3c4d5e6f0",
			From:       "alice@example.com",
			Subject:    "Lunch tomorrow?",
			Snippet:    "Are you free around noon to grab lunch...",
			ReceivedAt: "2026-06-03T12:00:00.000000000Z",
		},
	},
	{
		Type:        EventMailSent,
		Description: "A message was sent from the mailbox (Gmail History messagesAdded carrying the SENT label and not INBOX) — covers our own sends uniformly, whether via the send MCP tool or the Gmail UI.",
		Sample: mailSentPayload{
			ID:       "18f2a1b3c4d5e6f8",
			ThreadID: "18f2a1b3c4d5e6f0",
			To:       "bob@example.com",
			Subject:  "Re: Lunch tomorrow?",
			Snippet:  "Sounds good, noon works for me...",
			SentAt:   "2026-06-03T12:05:00.000000000Z",
		},
	},
	{
		Type:        EventMailDeleted,
		Description: "A message was moved to Trash (Gmail History labelsAdded: TRASH). This is the discard signal, not a permanent expunge — the message still exists in Trash, so its payload is still fetchable.",
		Sample: mailDeletedPayload{
			ID:        "18f2a1b3c4d5e6f9",
			ThreadID:  "18f2a1b3c4d5e6f0",
			Subject:   "Old newsletter",
			DeletedAt: "2026-06-03T12:10:00.000000000Z",
		},
	},
}

// ── tool descriptors ──────────────────────────────────────────────────────────

// toolDescriptors returns the P1 STUB gmail surface: only the two chassis tools.
// The full normal-mailbox tool set (list/read/thread/send/draft/labels/label/
// unlabel/trash/delete) is added in P4. health is the end-to-end auth proof;
// reflection self-describes the three mail.* events the producer will emit
// (emission itself lands in P3).
func toolDescriptors() []map[string]any {
	return []map[string]any{
		desc(tool("health"),
			"Health + diagnostics for the gmail service. Returns the fixed envelope (status, version, service, details) plus the authenticated caller's identity (owner_email, client_id) as established by the platform's auth gate — the end-to-end auth-chain proof. Takes no inputs.",
			obj(map[string]any{})),
		desc(tool("reflection"),
			"Self-describe gmail's place in the event graph: 'publishes' (the event types this service emits — mail.received, mail.sent, mail.deleted) and 'subscribes' (the event types it listens to — empty for gmail, a producer). With no arguments, returns the index: {publishes:[{type,description}], subscribes:[{source,filter,description}]}. Pass 'event_type' (one of the published types) to get its publish detail — {type, description, schema (JSON Schema of the payload), example (a worked instance)}. Resolve a subscribed edge's shape by calling the source service's reflection tool.",
			obj(map[string]any{
				"event_type": descTyp("string", "optional; a published event type to fetch the schema+example detail for"),
			})),
	}
}

// ── schema helpers ──────────────────────────────────────────────────────────

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
// end-to-end auth-chain proof. gmail has no per-service reporter in P1, so
// details renders as an empty object unless a reporter is later wired.
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

// toolReflection self-describes gmail's edges in the event graph (the
// ikigenba_<svc>_reflection tool). No event_type → the index {publishes,
// subscribes}; with event_type → that published type's {type, description,
// schema, example}. An unknown event_type returns a corrective error listing the
// valid types, not an empty result.
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
// in-edges: one {source, filter, description} per Subscription. A nil provider
// (or nil result) renders as an empty list — gmail is a producer, so this is
// always empty.
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
// event_type, listing the valid types so the agent can self-correct.
func reflectionUnknownTypeError(e *outbox.UnknownEventTypeError) string {
	env := map[string]any{"error": map[string]any{
		"code":    "unknown_event_type",
		"message": "unknown event_type " + e.Type + "; valid types: " + strings.Join(e.Valid, ", "),
	}}
	b, _ := json.Marshal(env)
	return string(b)
}

// ── shared helpers ──────────────────────────────────────────────────────

func toolResultJSON(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return toolResultText(string(b)), nil
}
