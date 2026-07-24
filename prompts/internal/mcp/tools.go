package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"time"

	appkitmcp "appkit/mcp"
	"appkit/server"

	"prompts/internal/calls"
	"prompts/internal/prompt"
)

// toolPrefix brands every MCP tool name. Prompts currently exposes bare names.
const toolPrefix = ""

func tool(verb string) string { return toolPrefix + verb }

// Tools returns prompts' domain tool table. Chassis-owned tools such as health
// and reflection are supplied by appkit/mcp and must not be declared here.
func Tools(svc *prompt.Service, contentBase string) []appkitmcp.Tool {
	tools := []appkitmcp.Tool{
		desc(tool("describe"), "Return a detailed overview of prompts: what a prompt vs a run is, the create→run→poll→read lifecycle, full concurrency, the per-run sandbox, and LogRecord JSONL run output. Config requires model; provider is optional and defaults from the model catalog, or may select an alternate anthropic, openai, google, zai, or openrouter route. Optional keys tune sampling (temperature, top_p), output size (max_tokens), reasoning (effort, thinking_budget, thinking_level, thinking), retry/backoff behavior (max_attempts, base_delay, max_delay, max_elapsed, ignore_retry_after), tool loops (tool_loop_limit), provider endpoint override (base_url), and credential mode (auth). Call this first if you're unfamiliar with prompts. Takes no inputs.", obj(map[string]any{}),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				return toolDescribe()
			}),

		desc(tool("create"), "Create a new prompt for the caller. A prompt is a reusable definition (user_prompt, config, optional name/system_prompt) that you run on demand or wire to event triggers. Returns the new prompt_id. Optionally attach event triggers inline. Event-triggered runs receive the triggering event in the prompt (see describe / set_trigger).", obj(map[string]any{
			"user_prompt":   typ("string"),
			"config":        configSchema(),
			"name":          typ("string"),
			"system_prompt": typ("string"),
			"triggers":      triggersSchema(),
		}, "user_prompt", "config"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					UserPrompt   string         `json:"user_prompt"`
					Config       configInput    `json:"config"`
					Name         string         `json:"name"`
					SystemPrompt string         `json:"system_prompt"`
					Triggers     []triggerInput `json:"triggers"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				triggers := make([]prompt.TriggerSpec, 0, len(in.Triggers))
				for _, t := range in.Triggers {
					triggers = append(triggers, prompt.TriggerSpec{Filter: string(t)})
				}
				p, err := svc.Create(ctx, id.OwnerID, id.OwnerEmail, prompt.CreateInput{
					Name:         in.Name,
					UserPrompt:   in.UserPrompt,
					SystemPrompt: in.SystemPrompt,
					Config:       in.Config.toConfig(),
					Triggers:     triggers,
				})
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(map[string]any{"prompt_id": p.ID})
			}),

		desc(tool("import"), "Import a Dropbox-mirrored file as a prompt. 'source_path' is the file's path in the dropbox mirror. Fetches the current mirror bytes over loopback (valid UTF-8 under 1 MiB) and maps the file body to the prompt's user_prompt; 'name' defaults to the basename. Re-importing the same source_path updates the same prompt (upsert); system_prompt and config keep their defaults. Returns {prompt_id, name}.", obj(map[string]any{
			"source_path": typ("string"),
			"name":        typ("string"),
		}, "source_path"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					SourcePath string `json:"source_path"`
					Name       string `json:"name"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				p, err := svc.Import(ctx, id.OwnerID, id.OwnerEmail, in.SourcePath, in.Name)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(map[string]any{"prompt_id": p.ID, "name": p.Name})
			}),

		desc(tool("list"), "List the caller's prompts, each with its running run count and latest run (last_run).", obj(map[string]any{}),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				prompts, err := svc.List(ctx, id.OwnerID)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(map[string]any{"prompts": prompts})
			}),

		desc(tool("get"), "Get one of the caller's prompts, including its running run count and latest run (last_run).", obj(map[string]any{
			"prompt_id": typ("string"),
		}, "prompt_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					PromptID string `json:"prompt_id"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				detail, err := svc.Get(ctx, id.OwnerID, in.PromptID)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(detail)
			}),

		desc(tool("update"), "Update a prompt's name, user_prompt, system_prompt, and config. Always allowed (in-flight runs read their pinned inputs from disk, so they are unaffected).", obj(map[string]any{
			"prompt_id":     typ("string"),
			"user_prompt":   typ("string"),
			"system_prompt": typ("string"),
			"config":        configSchema(),
			"name":          typ("string"),
		}, "prompt_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					PromptID     string      `json:"prompt_id"`
					UserPrompt   string      `json:"user_prompt"`
					SystemPrompt string      `json:"system_prompt"`
					Config       configInput `json:"config"`
					Name         string      `json:"name"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				p, err := svc.Update(ctx, id.OwnerID, in.PromptID, prompt.UpdateInput{
					Name:         in.Name,
					UserPrompt:   in.UserPrompt,
					SystemPrompt: in.SystemPrompt,
					Config:       in.Config.toConfig(),
				})
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(p)
			}),

		desc(tool("delete"), "Delete one of the caller's prompts (a tombstone: the prompt row and its triggers are removed; its runs and their on-disk artifacts survive and stay readable by run_id). Always allowed.", obj(map[string]any{
			"prompt_id": typ("string"),
		}, "prompt_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					PromptID string `json:"prompt_id"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				if err := svc.Delete(ctx, id.OwnerID, in.PromptID); err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(map[string]any{"deleted": in.PromptID})
			}),

		desc(tool("set_trigger"), "Attach a canonical routing-key glob filter such as dropbox:create/bills/**. The literal source is before ':'; ** crosses subject path segments.", obj(map[string]any{
			"prompt_id": typ("string"), "filter": typ("string"),
		}, "prompt_id", "filter"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					PromptID string `json:"prompt_id"`
					Filter   string `json:"filter"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				trig, err := svc.SetTrigger(ctx, id.OwnerID, in.PromptID, in.Filter)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(trig)
			}),

		desc(tool("clear_trigger"), "Remove one canonical routing-key filter from one of the caller's prompts.", obj(map[string]any{
			"prompt_id": typ("string"), "filter": typ("string"),
		}, "prompt_id", "filter"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					PromptID string `json:"prompt_id"`
					Filter   string `json:"filter"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				if err := svc.ClearTrigger(ctx, id.OwnerID, in.PromptID, in.Filter); err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(map[string]any{"cleared": in.PromptID})
			}),

		desc(tool("run"), "Start a run for one of the caller's prompts. Always allowed — runs are fully concurrent, each in its own per-run sandbox. Returns the new run_id, status (\"running\"), and start time.", obj(map[string]any{
			"prompt_id": typ("string"),
		}, "prompt_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					PromptID string `json:"prompt_id"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				run, err := svc.Run(ctx, id.OwnerID, in.PromptID)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(map[string]any{"run_id": run.ID, "status": run.Status, "started_at": run.StartedAt})
			}),

		desc(tool("run_list"), "List the runs of one of the caller's prompts, newest first.", obj(map[string]any{
			"prompt_id": typ("string"),
		}, "prompt_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					PromptID string `json:"prompt_id"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				runs, err := svc.RunList(ctx, id.OwnerID, in.PromptID)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(map[string]any{"runs": runs})
			}),

		desc(tool("run_get"), "Get one run by run_id (the run stays readable after its prompt is deleted).", obj(map[string]any{
			"run_id": typ("string"),
		}, "run_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					RunID string `json:"run_id"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				run, err := svc.RunGet(ctx, id.OwnerID, in.RunID)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(run)
			}),

		desc(tool("run_output"), "Read a run's output log by run_id (append-only stream-json, one event per line). offset is 1-based; limit caps the number of lines (<=0 means from start / no limit).", obj(map[string]any{
			"run_id": typ("string"),
			"offset": typ("integer"),
			"limit":  typ("integer"),
		}, "run_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					RunID  string `json:"run_id"`
					Offset int    `json:"offset"`
					Limit  int    `json:"limit"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				out, err := svc.RunOutput(ctx, id.OwnerID, in.RunID, in.Offset, in.Limit)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.TextResult(out), nil
			}),

		desc(tool("run_cancel"), "Cancel an in-flight run by run_id. Idempotent.", obj(map[string]any{
			"run_id": typ("string"),
		}, "run_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					RunID string `json:"run_id"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				if err := svc.RunCancel(ctx, id.OwnerID, in.RunID); err != nil {
					return fail(err), nil
				}
				return appkitmcp.StructuredResult(map[string]any{"cancelled": in.RunID})
			}),

		desc(tool("run_fs_list"), "List entries under path within a run's sandbox folder by run_id (path defaults to the sandbox root). Non-directory entries include a loopback content_url for byte fetch by services (a run's Fetch tool or dropbox put(source_url)), not by the agent.", obj(map[string]any{
			"run_id": typ("string"),
			"path":   typ("string"),
		}, "run_id"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					RunID string `json:"run_id"`
					Path  string `json:"path"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				entries, err := svc.RunFsList(ctx, id.OwnerID, in.RunID, in.Path)
				if err != nil {
					return fail(err), nil
				}
				rendered := make([]map[string]any, 0, len(entries))
				for _, entry := range entries {
					out := map[string]any{
						"name":   entry.Name,
						"is_dir": entry.IsDir,
						"size":   entry.Size,
					}
					if !entry.IsDir {
						query := url.Values{"run_id": {in.RunID}, "path": {path.Join(in.Path, entry.Name)}}
						out["content_url"] = contentBase + "/run-content?" + query.Encode()
					}
					rendered = append(rendered, out)
				}
				return appkitmcp.StructuredResult(map[string]any{"entries": rendered})
			}),

		desc(tool("run_fs_read"), "Read a file within a run's sandbox folder by run_id. offset is 1-based; limit caps the number of lines (<=0 means from start / no limit).", obj(map[string]any{
			"run_id": typ("string"),
			"path":   typ("string"),
			"offset": typ("integer"),
			"limit":  typ("integer"),
		}, "run_id", "path"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					RunID  string `json:"run_id"`
					Path   string `json:"path"`
					Offset int    `json:"offset"`
					Limit  int    `json:"limit"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				out, err := svc.RunFsRead(ctx, id.OwnerID, in.RunID, in.Path, in.Offset, in.Limit)
				if err != nil {
					return fail(err), nil
				}
				return appkitmcp.TextResult(out), nil
			}),

		desc(tool("calls"), "Inspect inference calls. Without call_id, returns filtered and paginated metric rows without request/response bodies. With call_id, returns one detail row including retained bodies; pruned details carry bodies_pruned=true.", callsInputSchema(),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in callsInput
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				store := svc.CallStore()
				if store == nil {
					return fail(errors.New("calls store is not configured")), nil
				}
				if in.CallID != "" {
					row, err := store.Get(ctx, in.CallID)
					if errors.Is(err, sql.ErrNoRows) {
						return fail(fmt.Errorf("%w: call %q", prompt.ErrNotFound, in.CallID)), nil
					}
					if err != nil {
						return fail(err), nil
					}
					return appkitmcp.StructuredResult(callDetail(row))
				}
				filter, err := in.filter()
				if err != nil {
					return fail(err), nil
				}
				rows, err := store.List(ctx, filter)
				if err != nil {
					return fail(err), nil
				}
				out := make([]map[string]any, 0, len(rows))
				for _, row := range rows {
					out = append(out, callMetrics(row))
				}
				return appkitmcp.StructuredResult(map[string]any{"calls": out})
			}),

		desc(tool("usage"), "Aggregate inference usage by name, origin, model, or UTC day, optionally filtered by class and time window.", obj(map[string]any{
			"group_by": map[string]any{"type": "string", "enum": []string{"name", "origin", "model", "day"}},
			"class":    classSchema(), "since": typ("string"), "until": typ("string"),
		}, "group_by"),
			func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
				var in struct {
					GroupBy string `json:"group_by"`
					Class   string `json:"class"`
					Since   string `json:"since"`
					Until   string `json:"until"`
				}
				if err := parseArgs(args, &in); err != nil {
					return nil, err
				}
				group := calls.GroupBy(in.GroupBy)
				if group != calls.GroupByName && group != calls.GroupByOrigin && group != calls.GroupByModel && group != calls.GroupByDay {
					return fail(validationError("group_by must be one of name, origin, model, day")), nil
				}
				filter, err := (callsInput{Class: in.Class, Since: in.Since, Until: in.Until}).filter()
				if err != nil {
					return fail(err), nil
				}
				store := svc.CallStore()
				if store == nil {
					return fail(errors.New("calls store is not configured")), nil
				}
				buckets, err := store.Aggregate(ctx, group, filter)
				if err != nil {
					return fail(err), nil
				}
				out := make([]map[string]any, 0, len(buckets))
				for _, bucket := range buckets {
					out = append(out, map[string]any{"key": bucket.Key, "calls": bucket.Calls, "input_tokens": bucket.InputTokens, "output_tokens": bucket.OutputTokens, "total_tokens": bucket.TotalTokens, "cost_usd": bucket.CostUSD, "errors": bucket.Errors})
				}
				return appkitmcp.StructuredResult(map[string]any{"buckets": out})
			}),
	}
	schemas := outputSchemas()
	for i := range tools {
		tools[i].OutputSchema = schemas[tools[i].Name]
	}
	return tools
}

type callsInput struct {
	Class      string `json:"class"`
	Origin     string `json:"origin"`
	Name       string `json:"name"`
	GroupID    string `json:"group_id"`
	ErrorsOnly bool   `json:"errors_only"`
	Since      string `json:"since"`
	Until      string `json:"until"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	CallID     string `json:"call_id"`
}

func callsInputSchema() map[string]any {
	return obj(map[string]any{
		"class": classSchema(), "origin": typ("string"), "name": typ("string"),
		"group_id": typ("string"), "errors_only": typ("boolean"), "since": typ("string"),
		"until": typ("string"), "limit": typ("integer"), "offset": typ("integer"), "call_id": typ("string"),
	})
}

func classSchema() map[string]any {
	return map[string]any{"type": "string", "enum": []string{"session", "completion", "embedding"}}
}

func (in callsInput) filter() (calls.Filter, error) {
	f := calls.Filter{Class: calls.Class(in.Class), Origin: in.Origin, Name: in.Name, GroupID: in.GroupID, ErrorsOnly: in.ErrorsOnly, Limit: in.Limit, Offset: in.Offset}
	if f.Class != "" && f.Class != calls.ClassSession && f.Class != calls.ClassCompletion && f.Class != calls.ClassEmbedding {
		return calls.Filter{}, validationError("class must be one of session, completion, embedding")
	}
	var err error
	if f.Since, err = parseOptionalTime("since", in.Since); err != nil {
		return calls.Filter{}, err
	}
	if f.Until, err = parseOptionalTime("until", in.Until); err != nil {
		return calls.Filter{}, err
	}
	return f, nil
}

func parseOptionalTime(name, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, validationError(name + " must be an RFC3339 timestamp")
	}
	return parsed, nil
}

func validationError(message string) error {
	return fmt.Errorf("%w: %s", prompt.ErrValidation, message)
}

func callMetrics(row calls.Row) map[string]any {
	return map[string]any{
		"id": row.ID, "class": row.Class, "origin": row.Origin, "name": row.Name,
		"group_id": row.GroupID, "attempt": row.Attempt, "owner_email": row.OwnerEmail,
		"provider": row.Provider, "model": row.Model, "input_tokens": row.InputTokens,
		"output_tokens": row.OutputTokens, "total_tokens": row.TotalTokens,
		"usage_json": row.UsageJSON, "cost_usd": row.CostUSD, "error": row.Error,
		"started_at": row.StartedAt, "ended_at": row.EndedAt,
	}
}

func callDetail(row calls.Row) map[string]any {
	out := callMetrics(row)
	if row.RequestBody != nil {
		out["request_body"] = *row.RequestBody
	}
	if row.ResponseBody != nil {
		out["response_body"] = *row.ResponseBody
	}
	if row.RequestBody == nil && row.ResponseBody == nil {
		out["bodies_pruned"] = true
	}
	return out
}

func desc(name, description string, schema map[string]any, handler func(context.Context, json.RawMessage, server.Identity) (map[string]any, error)) appkitmcp.Tool {
	return appkitmcp.Tool{Name: name, Description: description, InputSchema: schema, Handler: handler}
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func typ(t string) map[string]any { return map[string]any{"type": t} }

// fail wraps a domain error as a coded MCP tool error.
func fail(err error) map[string]any { return appkitmcp.ErrorResult(codeFor(err), err.Error()) }

func codeFor(err error) appkitmcp.ErrorCode {
	switch {
	case errors.Is(err, prompt.ErrNotFound):
		return appkitmcp.ErrNotFound
	case errors.Is(err, prompt.ErrValidation):
		return appkitmcp.ErrValidation
	case errors.Is(err, prompt.ErrTooLarge):
		return appkitmcp.ErrTooLarge
	case errors.Is(err, prompt.ErrSourceUnavailable):
		return appkitmcp.ErrSourceUnavailable
	default:
		return appkitmcp.ErrInternal
	}
}

func outputSchemas() map[string]map[string]any {
	promptProps := map[string]any{
		"id": typ("string"), "owner_id": typ("string"), "owner_email": typ("string"), "name": typ("string"),
		"user_prompt": typ("string"), "system_prompt": typ("string"),
		"config":     map[string]any{"type": "object", "additionalProperties": true},
		"created_at": typ("string"), "updated_at": typ("string"), "source_path": typ("string"),
	}
	runProps := map[string]any{
		"id": typ("string"), "prompt_id": typ("string"), "owner_id": typ("string"), "owner_email": typ("string"),
		"prompt_name": typ("string"), "status": typ("string"), "started_at": typ("string"),
		"ended_at": typ("string"), "usage_json": typ("string"), "error": typ("string"),
		"log_path": typ("string"), "trigger_source": typ("string"), "trigger_kind": typ("string"),
		"trigger_subject": typ("string"), "trigger_event_id": typ("string"),
	}
	promptSchema := obj(promptProps, "id", "owner_id", "owner_email", "user_prompt", "config", "created_at", "updated_at")
	detailProps := make(map[string]any, len(promptProps)+2)
	for key, value := range promptProps {
		detailProps[key] = value
	}
	detailProps["running_count"] = typ("integer")
	detailProps["last_run"] = map[string]any{"type": []string{"object", "null"}, "additionalProperties": true}
	detailSchema := obj(detailProps, "id", "owner_id", "owner_email", "user_prompt", "config", "created_at", "updated_at", "running_count", "last_run")
	runSchema := obj(runProps, "id", "prompt_id", "owner_id", "owner_email", "status", "started_at", "log_path")
	triggerSchema := obj(map[string]any{
		"prompt_id": typ("string"), "source": typ("string"), "filter": typ("string"), "created_at": typ("string"),
	}, "prompt_id", "source", "filter", "created_at")
	return map[string]map[string]any{
		tool("calls"): obj(map[string]any{
			"calls": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
			"id":    typ("string"),
		}),
		tool("usage"): obj(map[string]any{
			"buckets": map[string]any{"type": "array", "items": obj(map[string]any{
				"key": typ("string"), "calls": typ("integer"), "input_tokens": typ("integer"), "output_tokens": typ("integer"), "total_tokens": typ("integer"), "cost_usd": typ("number"), "errors": typ("integer"),
			}, "key", "calls", "input_tokens", "output_tokens", "total_tokens", "cost_usd", "errors")},
		}, "buckets"),
		tool("create"):        obj(map[string]any{"prompt_id": typ("string")}, "prompt_id"),
		tool("import"):        obj(map[string]any{"prompt_id": typ("string"), "name": typ("string")}, "prompt_id", "name"),
		tool("list"):          obj(map[string]any{"prompts": map[string]any{"type": "array", "items": detailSchema}}, "prompts"),
		tool("get"):           detailSchema,
		tool("update"):        promptSchema,
		tool("delete"):        obj(map[string]any{"deleted": typ("string")}, "deleted"),
		tool("set_trigger"):   triggerSchema,
		tool("clear_trigger"): obj(map[string]any{"cleared": typ("string")}, "cleared"),
		tool("run"): obj(map[string]any{
			"run_id": typ("string"), "status": typ("string"), "started_at": typ("string"),
		}, "run_id", "status", "started_at"),
		tool("run_list"):   obj(map[string]any{"runs": map[string]any{"type": "array", "items": runSchema}}, "runs"),
		tool("run_get"):    runSchema,
		tool("run_cancel"): obj(map[string]any{"cancelled": typ("string")}, "cancelled"),
		tool("run_fs_list"): obj(map[string]any{
			"entries": map[string]any{"type": "array", "items": obj(map[string]any{
				"name": typ("string"), "is_dir": typ("boolean"), "size": typ("integer"), "content_url": typ("string"),
			}, "name", "is_dir", "size")},
		}, "entries"),
	}
}

// configSchema is the shared prompt.Config input schema.
func configSchema() map[string]any {
	return obj(map[string]any{
		"provider":           typ("string"),
		"model":              typ("string"),
		"temperature":        typ("number"),
		"top_p":              typ("number"),
		"max_tokens":         typ("integer"),
		"effort":             typ("string"),
		"thinking_budget":    typ("integer"),
		"thinking_level":     typ("string"),
		"thinking":           typ("boolean"),
		"max_attempts":       typ("integer"),
		"base_delay":         typ("string"),
		"max_delay":          typ("string"),
		"max_elapsed":        typ("string"),
		"ignore_retry_after": typ("boolean"),
		"tool_loop_limit":    typ("integer"),
		"base_url":           typ("string"),
		"auth":               typ("string"),
	}, "model")
}

// triggersSchema is create's optional inline canonical-key filter array.
func triggersSchema() map[string]any {
	return map[string]any{
		"type":  "array",
		"items": typ("string"),
	}
}

// paramError marks genuinely unparseable tool arguments — mapped to JSON-RPC
// -32602 by appkit/mcp rather than an MCP isError tool result.
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

// configFromInput maps the wire config object to prompt.Config.
type configInput struct {
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	Temperature      *float64 `json:"temperature"`
	TopP             *float64 `json:"top_p"`
	MaxTokens        int      `json:"max_tokens"`
	Effort           string   `json:"effort"`
	ThinkingBudget   *int     `json:"thinking_budget"`
	ThinkingLevel    string   `json:"thinking_level"`
	Thinking         *bool    `json:"thinking"`
	MaxAttempts      int      `json:"max_attempts"`
	BaseDelay        string   `json:"base_delay"`
	MaxDelay         string   `json:"max_delay"`
	MaxElapsed       string   `json:"max_elapsed"`
	IgnoreRetryAfter bool     `json:"ignore_retry_after"`
	ToolLoopLimit    int      `json:"tool_loop_limit"`
	BaseURL          string   `json:"base_url"`
	Auth             string   `json:"auth"`
}

func (c configInput) toConfig() prompt.Config {
	return prompt.Config{
		Provider:         c.Provider,
		Model:            c.Model,
		Temperature:      c.Temperature,
		TopP:             c.TopP,
		MaxTokens:        c.MaxTokens,
		Effort:           c.Effort,
		ThinkingBudget:   c.ThinkingBudget,
		ThinkingLevel:    c.ThinkingLevel,
		Thinking:         c.Thinking,
		MaxAttempts:      c.MaxAttempts,
		BaseDelay:        c.BaseDelay,
		MaxDelay:         c.MaxDelay,
		MaxElapsed:       c.MaxElapsed,
		IgnoreRetryAfter: c.IgnoreRetryAfter,
		ToolLoopLimit:    c.ToolLoopLimit,
		BaseURL:          c.BaseURL,
		Auth:             c.Auth,
	}
}

// triggerInput maps one wire trigger object to prompt.TriggerSpec.
type triggerInput string
