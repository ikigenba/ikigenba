package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
)

// toolDescriptors returns the fixed two-tool dropbox surface (PLAN.md §3).
// dropbox is a daemon: the service side is read-only, so there are no write
// verbs. dropbox_whoami is the kept identity probe; dropbox_health is the
// forward-looking status tool that returns the same identity content in v1 and
// supersedes whoami later (the migration is additive). Both take no inputs.
func toolDescriptors() []map[string]any {
	return []map[string]any{
		desc("dropbox_whoami",
			"Return the authenticated caller's identity (owner email and client id) as established by the platform's auth gate. Takes no inputs; the end-to-end auth proof. Slated to be superseded by dropbox_health.",
			obj(map[string]any{})),

		desc("dropbox_health",
			"Health/status probe for the dropbox mirror daemon. Returns the caller's identity (owner email and client id, identical to dropbox_whoami) plus telemetry: mirror_bytes (indexed logical size), disk_free_bytes / disk_total_bytes (mirror filesystem), and failed_files (count of files the sync engine could not download). Takes no inputs.",
			obj(map[string]any{})),
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

// ── dispatch ──────────────────────────────────────────────────────────────

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (h *Handler) handleToolCall(w http.ResponseWriter, req jsonRPCRequest, id Identity) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeJSONRPCError(w, req.ID, -32602, "invalid params")
		return
	}
	res, err := h.dispatchTool(p.Name, id)
	if err != nil {
		writeJSONRPCResult(w, req.ID, toolResultErr(err.Error()))
		return
	}
	writeJSONRPCResult(w, req.ID, res)
}

func (h *Handler) dispatchTool(name string, id Identity) (map[string]any, error) {
	switch name {
	case "dropbox_whoami":
		return h.toolWhoami(id)
	case "dropbox_health":
		return h.toolHealth(id)
	default:
		return nil, errors.New("unknown tool: " + name)
	}
}

// ── tool implementations ─────────────────────────────────────────────────

func (h *Handler) toolWhoami(id Identity) (map[string]any, error) {
	info, err := h.svc.Whoami(id.OwnerEmail, id.ClientID)
	if err != nil {
		return toolResultErr(err.Error()), nil
	}
	return toolResultJSON(map[string]any{
		"owner_email": info.OwnerEmail,
		"client_id":   info.ClientID,
	})
}

func (h *Handler) toolHealth(id Identity) (map[string]any, error) {
	info, err := h.svc.Health(id.OwnerEmail, id.ClientID)
	if err != nil {
		return toolResultErr(err.Error()), nil
	}
	// Identity (the auth proof, identical to dropbox_whoami) plus mirror/disk
	// telemetry (PLAN.md §3): mirror_bytes = SUM(size) over the index,
	// disk_free/total_bytes from a statfs on the mirror, failed_files = count of
	// poison rows the engine advanced past.
	return toolResultJSON(map[string]any{
		"owner_email":      info.OwnerEmail,
		"client_id":        info.ClientID,
		"mirror_bytes":     info.MirrorBytes,
		"disk_free_bytes":  info.DiskFreeBytes,
		"disk_total_bytes": info.DiskTotalBytes,
		"failed_files":     info.FailedFiles,
	})
}

// ── shared helpers ──────────────────────────────────────────────────────

func toolResultJSON(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return toolResultText(string(b)), nil
}
