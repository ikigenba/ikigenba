package mcp

import (
	"appkit/server"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"scripts/internal/script"
	"strings"

	appkitmcp "appkit/mcp"
)

// toolPrefix brands every MCP tool name (DECISIONS §1). It is the suite name
// `ikigenba` plus the service name, defined once and used in BOTH the descriptor
// list and the dispatch switch so the two sites cannot drift.
const toolPrefix = ""

// tool returns the branded MCP tool name for a verb.
func tool(verb string) string { return toolPrefix + verb }

type toolHandlers struct {
	svc         *script.Service
	contentBase string
}

// Tools returns scripts' service-owned MCP tool declarations. The shared appkit
// MCP transport appends the chassis health and reflection tools.
func Tools(svc *script.Service, contentBase string) []appkitmcp.Tool {
	h := newToolHandlers(svc, contentBase)
	return []appkitmcp.Tool{
		desc(tool("describe"), "Return a detailed overview of scripts: what a script is, the create→run→poll→read lifecycle, triggers, and the runtime contract. Call this first if you're unfamiliar with scripts. Takes no inputs.", obj(map[string]any{}), h.toolHandler("describe")),

		desc(tool("create"), "Create a new script for the caller. body is the Python source. Returns the new script_id.", obj(map[string]any{
			"name":   typ("string"),
			"body":   typ("string"),
			"config": configSchema(),
		}, "name", "body"), h.toolHandler("create")),

		desc(tool("import"), "Import a Dropbox-mirrored file as a script. 'source_path' is the file's path in the dropbox mirror (e.g. \"/scripts/nightly.py\"). Fetches the current mirror bytes over loopback, requires valid UTF-8 text under 1 MiB, and upserts on source_path: re-importing the same path updates the same script instead of creating a duplicate. 'name' defaults to the file's basename. Returns {script_id, name}.", obj(map[string]any{
			"source_path": typ("string"),
			"name":        typ("string"),
		}, "source_path"), h.toolHandler("import")),

		desc(tool("list"), "List the caller's scripts, each with its derived running_count and last_run.", obj(map[string]any{}), h.toolHandler("list")),

		desc(tool("get"), "Get one of the caller's scripts, including running_count and last_run.", obj(map[string]any{
			"script_id": typ("string"),
		}, "script_id"), h.toolHandler("get")),

		desc(tool("update"), "Update a script's name, body, and/or config. Any field may be omitted to leave it unchanged.", obj(map[string]any{
			"script_id": typ("string"),
			"name":      typ("string"),
			"body":      typ("string"),
			"config":    configSchema(),
		}, "script_id"), h.toolHandler("update")),

		desc(tool("delete"), "Delete one of the caller's scripts (tombstone): the script row and its triggers are removed, but its run history and on-disk artifacts survive.", obj(map[string]any{
			"script_id": typ("string"),
		}, "script_id"), h.toolHandler("delete")),

		desc(tool("set_trigger"), "Bind a script to an upstream canonical routing-key filter (for example \"dropbox:create/bills/**/*.pdf\"). The source segment is literal and ** matches across subject paths. When a matching event fires, scripts starts a run.", obj(map[string]any{
			"script_id": typ("string"),
			"filter":    typ("string"),
		}, "script_id", "filter"), h.toolHandler("set_trigger")),

		desc(tool("clear_trigger"), "Remove an event trigger from a script.", obj(map[string]any{
			"script_id": typ("string"),
			"filter":    typ("string"),
		}, "script_id", "filter"), h.toolHandler("clear_trigger")),

		desc(tool("run"), "Start a manual run of one of the caller's scripts. Always allowed — runs are fully concurrent. Returns the run_id and start time.", obj(map[string]any{
			"script_id": typ("string"),
		}, "script_id"), h.toolHandler("run")),

		desc(tool("run_list"), "List runs, optionally filtered by script_id, status (running|succeeded|failed|cancelled), and/or correlation_id. Each carries elapsed_secs.", obj(map[string]any{
			"script_id":      typ("string"),
			"status":         typ("string"),
			"correlation_id": typ("string"),
		}), h.toolHandler("run_list")),

		desc(tool("run_get"), "Get one run by run_id, including status, exit_code, and elapsed_secs.", obj(map[string]any{
			"run_id": typ("string"),
		}, "run_id"), h.toolHandler("run_get")),

		desc(tool("run_output"), "Read a run's captured output. stream is stdout|stderr|both (default both). offset is 1-based; limit caps lines (<=0 means from start / no limit).", obj(map[string]any{
			"run_id": typ("string"),
			"stream": typ("string"),
			"offset": typ("integer"),
			"limit":  typ("integer"),
		}, "run_id"), h.toolHandler("run_output")),

		desc(tool("run_cancel"), "Cancel an in-flight run by run_id (kills the process group). Idempotent.", obj(map[string]any{
			"run_id": typ("string"),
		}, "run_id"), h.toolHandler("run_cancel")),

		desc(tool("run_fs_list"), "List entries under path within a run's persisted dir tree (path defaults to the run root). Non-directory entries carry a loopback content_url for byte fetch by services, such as a run's suite.fetch or share put(source_url), not by the agent.", obj(map[string]any{
			"run_id": typ("string"),
			"path":   typ("string"),
		}, "run_id"), h.toolHandler("run_fs_list")),

		desc(tool("run_fs_read"), "Read a file within a run's persisted dir. offset is 1-based; limit caps lines (<=0 means from start / no limit).", obj(map[string]any{
			"run_id": typ("string"),
			"path":   typ("string"),
			"offset": typ("integer"),
			"limit":  typ("integer"),
		}, "run_id", "path"), h.toolHandler("run_fs_read")),
	}
}

func newToolHandlers(svc *script.Service, contentBase string) *toolHandlers {
	if svc == nil {
		panic("mcp: script service is required")
	}
	return &toolHandlers{svc: svc, contentBase: strings.TrimRight(contentBase, "/")}
}

func (h *toolHandlers) toolHandler(verb string) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
		return h.dispatchTool(ctx, tool(verb), id, args)
	}
}

func desc(name, description string, schema map[string]any, handler func(context.Context, json.RawMessage, server.Identity) (map[string]any, error)) appkitmcp.Tool {
	return appkitmcp.Tool{Name: name, Description: description, InputSchema: schema, OutputSchema: outputSchema(name), Handler: handler}
}

func outputSchema(name string) map[string]any {
	switch name {
	case tool("create"):
		return objSchema(map[string]any{"script_id": typ("string")}, "script_id")
	case tool("import"):
		return objSchema(map[string]any{"script_id": typ("string"), "name": typ("string")}, "script_id", "name")
	case tool("list"):
		return objSchema(map[string]any{"scripts": arraySchema(openObjectSchema())}, "scripts")
	case tool("get"):
		return scriptDetailSchema()
	case tool("update"):
		return scriptSchema()
	case tool("delete"):
		return objSchema(map[string]any{"deleted": typ("string")}, "deleted")
	case tool("set_trigger"):
		return triggerSchema()
	case tool("clear_trigger"):
		return objSchema(map[string]any{"cleared": typ("string")}, "cleared")
	case tool("run"):
		return objSchema(map[string]any{"run_id": typ("string"), "status": typ("string"), "started_at": typ("string"), "correlation_id": typ("string")}, "run_id", "status", "started_at", "correlation_id")
	case tool("run_list"):
		return objSchema(map[string]any{"runs": arraySchema(runSchema())}, "runs")
	case tool("run_get"):
		return runSchema()
	case tool("run_cancel"):
		return objSchema(map[string]any{"cancelled": typ("string")}, "cancelled")
	case tool("run_fs_list"):
		return objSchema(map[string]any{"entries": arraySchema(fileEntrySchema())}, "entries")
	default:
		return nil
	}
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func typ(t string) map[string]any { return map[string]any{"type": t} }

func objSchema(props map[string]any, required ...string) map[string]any {
	o := obj(props, required...)
	o["additionalProperties"] = true
	return o
}

func openObjectSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": true}
}

func nullable(schema map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}

func arraySchema(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func scriptSchema() map[string]any {
	props := map[string]any{}
	for _, key := range []string{"ID", "OwnerID", "OwnerEmail", "Name", "Body", "NameKey", "RepoSeededAt", "SourcePath", "CreatedAt", "UpdatedAt"} {
		props[key] = typ("string")
	}
	props["Config"] = openObjectSchema()
	return objSchema(props, "ID", "OwnerID", "OwnerEmail", "Name", "Body", "Config", "NameKey", "RepoSeededAt", "SourcePath", "CreatedAt", "UpdatedAt")
}

func scriptDetailSchema() map[string]any {
	props := map[string]any{}
	for key, schema := range scriptSchema()["properties"].(map[string]any) {
		props[key] = schema
	}
	props["RunningCount"] = typ("integer")
	props["LastRun"] = nullable(openObjectSchema())
	return objSchema(props, "ID", "OwnerID", "OwnerEmail", "Name", "Body", "Config", "NameKey", "RepoSeededAt", "SourcePath", "CreatedAt", "UpdatedAt", "RunningCount", "LastRun")
}

func triggerSchema() map[string]any {
	return objSchema(map[string]any{"ScriptID": typ("string"), "Source": typ("string"), "Filter": typ("string"), "CreatedAt": typ("string")}, "ScriptID", "Source", "Filter", "CreatedAt")
}

func runSchema() map[string]any {
	props := map[string]any{}
	for _, key := range []string{"id", "script_id", "status", "started_at", "ended_at", "error", "trigger_source", "trigger_kind", "trigger_subject", "trigger_event_id", "correlation_id", "repo_sha", "stdout_path", "stderr_path"} {
		props[key] = typ("string")
	}
	props["exit_code"] = nullable(typ("integer"))
	props["elapsed_secs"] = typ("integer")
	return objSchema(props, "id", "script_id", "status", "exit_code", "started_at", "ended_at", "error", "trigger_source", "trigger_kind", "trigger_subject", "trigger_event_id", "correlation_id", "repo_sha", "stdout_path", "stderr_path", "elapsed_secs")
}

func fileEntrySchema() map[string]any {
	return objSchema(map[string]any{"path": typ("string"), "is_dir": typ("boolean"), "size": typ("integer"), "content_url": typ("string")}, "path", "is_dir", "size")
}

// configSchema is the shared script.Config input schema (minimal day-one).
func configSchema() map[string]any {
	return obj(map[string]any{
		"interpreter":  typ("string"),
		"timeout_secs": typ("integer"),
	})
}

// paramError marks genuinely unparseable tool arguments — mapped to JSON-RPC
// -32602 rather than an MCP isError tool result.
type paramError struct{ err error }

func (e *paramError) Error() string { return "invalid params: " + e.err.Error() }

func parseArgs(args json.RawMessage, v any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, v); err != nil {
		return &paramError{err}
	}
	return nil
}

// configInput maps the wire config object to script.Config.
type configInput struct {
	Interpreter string `json:"interpreter"`
	TimeoutSecs int    `json:"timeout_secs"`
}

func (c configInput) toConfig() script.Config {
	return script.Config{
		Interpreter: c.Interpreter,
		TimeoutSecs: c.TimeoutSecs,
	}
}

func (h *toolHandlers) dispatchTool(ctx context.Context, name string, id server.Identity, args json.RawMessage) (map[string]any, error) {
	handlers := map[string]func(context.Context, server.Identity, json.RawMessage) (map[string]any, error){
		tool("describe"):      h.describe,
		tool("create"):        h.create,
		tool("import"):        h.importScript,
		tool("list"):          h.list,
		tool("get"):           h.get,
		tool("update"):        h.update,
		tool("delete"):        h.delete,
		tool("set_trigger"):   h.setTrigger,
		tool("clear_trigger"): h.clearTrigger,
		tool("run"):           h.run,
		tool("run_list"):      h.runList,
		tool("run_get"):       h.runGet,
		tool("run_output"):    h.runOutput,
		tool("run_cancel"):    h.runCancel,
		tool("run_fs_list"):   h.runFSList,
		tool("run_fs_read"):   h.runFSRead,
	}
	handler, ok := handlers[name]
	if !ok {
		return nil, errors.New("unknown tool: " + name)
	}
	return handler(ctx, id, args)
}

func (h *toolHandlers) describe(_ context.Context, _ server.Identity, _ json.RawMessage) (map[string]any, error) {
	return toolDescribe()
}

func (h *toolHandlers) create(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		Name   string      `json:"name"`
		Body   string      `json:"body"`
		Config configInput `json:"config"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	sc, err := h.svc.CreateForOwner(ctx, id.OwnerID, id.OwnerEmail, script.CreateInput{Name: in.Name, Body: in.Body, Config: in.Config.toConfig()})
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(map[string]any{"script_id": sc.ID})
}

func (h *toolHandlers) importScript(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		SourcePath string `json:"source_path"`
		Name       string `json:"name"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	sc, err := h.svc.ImportForOwner(ctx, id.OwnerID, id.OwnerEmail, in.SourcePath, in.Name)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(map[string]any{"script_id": sc.ID, "name": sc.Name})
}

func (h *toolHandlers) list(ctx context.Context, id server.Identity, _ json.RawMessage) (map[string]any, error) {
	scripts, err := h.svc.List(ctx, id.OwnerID)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(map[string]any{"scripts": scripts})
}

func (h *toolHandlers) get(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		ScriptID string `json:"script_id"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	detail, err := h.svc.Get(ctx, id.OwnerID, in.ScriptID)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(detail)
}

func (h *toolHandlers) update(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		ScriptID string       `json:"script_id"`
		Name     *string      `json:"name"`
		Body     *string      `json:"body"`
		Config   *configInput `json:"config"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	update := script.UpdateInput{Name: in.Name, Body: in.Body}
	if in.Config != nil {
		config := in.Config.toConfig()
		update.Config = &config
	}
	sc, err := h.svc.Update(ctx, id.OwnerID, in.ScriptID, update)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(sc)
}

func (h *toolHandlers) delete(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		ScriptID string `json:"script_id"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	if err := h.svc.Delete(ctx, id.OwnerID, in.ScriptID); err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(map[string]any{"deleted": in.ScriptID})
}

func (h *toolHandlers) setTrigger(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		ScriptID string `json:"script_id"`
		Filter   string `json:"filter"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	trigger, err := h.svc.SetTrigger(ctx, id.OwnerID, in.ScriptID, in.Filter)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(trigger)
}

func (h *toolHandlers) clearTrigger(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		ScriptID string `json:"script_id"`
		Filter   string `json:"filter"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	if err := h.svc.ClearTrigger(ctx, id.OwnerID, in.ScriptID, in.Filter); err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(map[string]any{"cleared": in.ScriptID})
}

func (h *toolHandlers) run(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		ScriptID string `json:"script_id"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	run, err := h.svc.Run(ctx, id.OwnerID, in.ScriptID)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(map[string]any{"run_id": run.ID, "status": run.Status, "started_at": run.StartedAt, "correlation_id": run.CorrelationID})
}

func (h *toolHandlers) runList(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		ScriptID      string `json:"script_id"`
		Status        string `json:"status"`
		CorrelationID string `json:"correlation_id"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	runs, err := h.svc.RunList(ctx, id.OwnerID, in.ScriptID, in.Status, in.CorrelationID)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(map[string]any{"runs": runs})
}

func (h *toolHandlers) runGet(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		RunID string `json:"run_id"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	run, err := h.svc.RunGet(ctx, id.OwnerID, in.RunID)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(run)
}

func (h *toolHandlers) runOutput(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		RunID  string `json:"run_id"`
		Stream string `json:"stream"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	out, err := h.svc.RunOutput(ctx, id.OwnerID, in.RunID, in.Stream, in.Offset, in.Limit)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultText(out), nil
}

func (h *toolHandlers) runCancel(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		RunID string `json:"run_id"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	if err := h.svc.RunCancel(ctx, id.OwnerID, in.RunID); err != nil {
		return structuredError(err), nil
	}
	return toolResultJSON(map[string]any{"cancelled": in.RunID})
}

func (h *toolHandlers) runFSList(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		RunID string `json:"run_id"`
		Path  string `json:"path"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	entries, err := h.svc.RunFsList(ctx, id.OwnerID, in.RunID, in.Path)
	if err != nil {
		return structuredError(err), nil
	}
	for i := range entries {
		if entries[i].IsDir {
			continue
		}
		query := url.Values{"run_id": {in.RunID}, "path": {entries[i].Path}}
		entries[i].ContentURL = h.contentBase + "/run-content?" + query.Encode()
	}
	return toolResultJSON(map[string]any{"entries": entries})
}

func (h *toolHandlers) runFSRead(ctx context.Context, id server.Identity, args json.RawMessage) (map[string]any, error) {
	var in struct {
		RunID  string `json:"run_id"`
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := parseArgs(args, &in); err != nil {
		return nil, err
	}
	out, err := h.svc.RunFsRead(ctx, id.OwnerID, in.RunID, in.Path, in.Offset, in.Limit)
	if err != nil {
		return structuredError(err), nil
	}
	return toolResultText(out), nil
}

// ── shared helpers ──────────────────────────────────────────────────────

func toolResultText(text string) map[string]any {
	return appkitmcp.TextResult(text)
}

func structuredError(err error) map[string]any {
	code := appkitmcp.ErrInternal
	switch {
	case errors.Is(err, script.ErrNotFound):
		code = appkitmcp.ErrNotFound
	case errors.Is(err, script.ErrConflict):
		code = appkitmcp.ErrConflict
	case errors.Is(err, script.ErrValidation):
		code = appkitmcp.ErrValidation
	case errors.Is(err, script.ErrTooLarge):
		code = appkitmcp.ErrTooLarge
	case errors.Is(err, script.ErrSourceUnavailable):
		code = appkitmcp.ErrSourceUnavailable
	}
	return appkitmcp.ErrorResult(code, err.Error())
}

func toolResultJSON(v any) (map[string]any, error) {
	return appkitmcp.StructuredResult(v)
}
