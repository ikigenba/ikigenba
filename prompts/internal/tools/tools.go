// Package tools exposes the caller's prompt catalog and runs through MCP.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

// Public refusal formats keep tool copy separate from validation.
const (
	MissingPrompt string = "prompt '%s' not found"
	MissingRun    string = "run '%s' not found"
	InvalidName   string = "invalid name '%s'"
	InvalidEvent  string = "invalid event '%s'"
	NameTaken     string = "name '%s' is taken"
	NotSubscribed string = "'%s' is not subscribed to '%s'"
	Ended         string = "run '%s' has already ended"
	UnknownModel  string = "unknown model '%s'"
	InvalidTools  string = "invalid tools %s"
	InvalidSchema string = "invalid schema: %s"
	EmptyPrompt   string = "prompt must not be empty"
)

// Config supplies the shared catalog, run core and event writer.
type Config struct {
	Store     *store.Store
	Runs      *runs.Core
	Telemetry *telemetry.Writer
}

type handlers struct{ Config }

type textError string

func (e textError) Error() string { return string(e) }

func (h handlers) prompt(ctx context.Context, c identity.Caller, name string) (store.Prompt, error) {
	if !store.ValidName(name) {
		return store.Prompt{}, fmt.Errorf(MissingPrompt, name)
	}
	p, err := h.Store.Find(ctx, c.UserID, name)
	if errors.Is(err, store.ErrNotFound) {
		return p, fmt.Errorf(MissingPrompt, name)
	}
	return p, catalogError(err)
}
func (h handlers) runRecord(ctx context.Context, c identity.Caller, id string) (store.Run, error) {
	if !store.ValidRunID(id) {
		return store.Run{}, fmt.Errorf(MissingRun, id)
	}
	r, err := h.Store.FindRun(ctx, c.UserID, id)
	if errors.Is(err, store.ErrNotFound) {
		return r, fmt.Errorf(MissingRun, id)
	}
	return r, catalogError(err)
}
func catalogError(err error) error {
	if err != nil {
		return textError(store.Unreachable)
	}
	return nil
}
func validate(model, prompt *string, groups *[]string, schema json.RawMessage) error {
	if model != nil {
		if _, err := agent.Offering(*model); err != nil {
			return fmt.Errorf(UnknownModel, *model)
		}
	}
	if prompt != nil && *prompt == "" {
		return errors.New(EmptyPrompt)
	}
	if groups != nil && !agent.ValidGroups(*groups) {
		b, _ := json.Marshal(*groups)
		return fmt.Errorf(InvalidTools, string(b))
	}
	if len(schema) > 0 {
		if err := agentkit.ValidateOutputSchema(schema); err != nil {
			return fmt.Errorf(InvalidSchema, err.Error())
		}
	}
	return nil
}
func (h handlers) list(ctx context.Context, c identity.Caller, _ ListArgs) (PromptList, error) {
	ps, err := h.Store.List(ctx, c.UserID)
	if err != nil {
		return PromptList{}, catalogError(err)
	}
	out := PromptList{Prompts: make([]ListedPrompt, 0, len(ps))}
	for _, p := range ps {
		v := promptObject(p)
		out.Prompts = append(out.Prompts, ListedPrompt{ID: p.ID, Name: p.Name, Model: p.Model, Tools: v.Tools, Subscriptions: len(p.Subscriptions), LastRun: v.LastRun})
	}
	return out, nil
}
func (h handlers) show(ctx context.Context, c identity.Caller, a ShowArgs) (Prompt, error) {
	p, err := h.prompt(ctx, c, a.Name)
	return promptObject(p), err
}
func (h handlers) create(ctx context.Context, c identity.Caller, a CreateArgs) (Prompt, error) {
	if !store.ValidName(a.Name) {
		return Prompt{}, fmt.Errorf(InvalidName, a.Name)
	}
	taken, err := h.Store.Taken(ctx, a.Name)
	if err != nil {
		return Prompt{}, catalogError(err)
	}
	if taken {
		return Prompt{}, fmt.Errorf(NameTaken, a.Name)
	}
	if err = validate(&a.Model, &a.Prompt, a.Tools, a.Schema); err != nil {
		return Prompt{}, err
	}
	d := store.Draft{Owner: c.UserID, OwnerEmail: c.Email, Name: a.Name, Model: a.Model, Prompt: a.Prompt, Schema: a.Schema}
	if a.System != nil {
		d.System = *a.System
	}
	if a.Tools != nil {
		d.Tools = *a.Tools
	}
	p, err := h.Store.Create(ctx, d)
	if errors.Is(err, store.ErrNameTaken) {
		return Prompt{}, fmt.Errorf(NameTaken, a.Name)
	}
	if err != nil {
		return Prompt{}, catalogError(err)
	}
	h.Telemetry.Emit(ctx, "prompt.created", telemetry.Attrs{"prompt": p.ID})
	return promptObject(p), nil
}
func (h handlers) update(ctx context.Context, c identity.Caller, a UpdateArgs) (Prompt, error) {
	p, err := h.prompt(ctx, c, a.Name)
	if err != nil {
		return Prompt{}, err
	}
	if err = validate(a.Model, a.Prompt, a.Tools, a.Schema); err != nil {
		return Prompt{}, err
	}
	change := store.Change{Model: a.Model, Prompt: a.Prompt, System: a.System, Tools: a.Tools}
	if len(a.Schema) > 0 {
		change.Schema = &a.Schema
	}
	p, changed, err := h.Store.Update(ctx, p.ID, change)
	if err != nil {
		return Prompt{}, catalogError(err)
	}
	if changed {
		h.Telemetry.Emit(ctx, "prompt.updated", telemetry.Attrs{"prompt": p.ID})
	}
	return promptObject(p), nil
}
func (h handlers) delete(ctx context.Context, c identity.Caller, a DeleteArgs) (Deleted, error) {
	p, err := h.prompt(ctx, c, a.Name)
	if err != nil {
		return Deleted{}, err
	}
	if err = h.Runs.Delete(ctx, p.ID); err != nil {
		return Deleted{}, catalogError(err)
	}
	h.Telemetry.Emit(ctx, "prompt.deleted", telemetry.Attrs{"prompt": p.ID})
	return Deleted{Deleted: true, ID: p.ID}, nil
}
func (h handlers) subscription(ctx context.Context, c identity.Caller, name, event string, remove bool) (Prompt, error) {
	p, err := h.prompt(ctx, c, name)
	if err != nil {
		return Prompt{}, err
	}
	if !store.ValidEvent(event) {
		return Prompt{}, fmt.Errorf(InvalidEvent, event)
	}
	if remove {
		p, err = h.Store.Unsubscribe(ctx, p.ID, event)
	} else {
		p, err = h.Store.Subscribe(ctx, p.ID, event)
	}
	if errors.Is(err, store.ErrNotSubscribed) {
		return Prompt{}, fmt.Errorf(NotSubscribed, name, event)
	}
	return promptObject(p), catalogError(err)
}
func (h handlers) subscribe(ctx context.Context, c identity.Caller, a SubscribeArgs) (Prompt, error) {
	return h.subscription(ctx, c, a.Name, a.Event, false)
}
func (h handlers) unsubscribe(ctx context.Context, c identity.Caller, a UnsubscribeArgs) (Prompt, error) {
	return h.subscription(ctx, c, a.Name, a.Event, true)
}
func (h handlers) run(ctx context.Context, c identity.Caller, a RunArgs) (Started, error) {
	p, err := h.prompt(ctx, c, a.Name)
	if err != nil {
		return Started{}, err
	}
	r, err := h.Runs.Run(ctx, p, runs.Request{Input: a.Input, Caller: c})
	switch {
	case errors.Is(err, store.ErrNotFound):
		return Started{}, fmt.Errorf(MissingPrompt, a.Name)
	case errors.Is(err, runs.ErrDraining):
		return Started{}, textError(runs.Stopping)
	case errors.Is(err, runs.ErrCutOff), err != nil && context.Cause(ctx) != nil && errors.Is(err, context.Cause(ctx)):
		runtime.Goexit()
	case errors.Is(err, runs.ErrNoCgroup), errors.Is(err, runs.ErrQueueFull):
		return Started{}, err
	case err != nil:
		return Started{}, catalogError(err)
	}
	e := runEntry(r)
	return Started{ID: r.ID, Status: r.Status, Reason: e.Reason}, nil
}
func (h handlers) runs(ctx context.Context, c identity.Caller, a RunsArgs) (RunList, error) {
	p, err := h.prompt(ctx, c, a.Name)
	if err != nil {
		return RunList{}, err
	}
	rs, err := h.Store.Runs(ctx, p.ID)
	if err != nil {
		return RunList{}, catalogError(err)
	}
	out := RunList{Runs: make([]RunEntry, 0, len(rs))}
	for _, r := range rs {
		out.Runs = append(out.Runs, runEntry(r))
	}
	return out, nil
}
func (h handlers) result(ctx context.Context, c identity.Caller, a ResultArgs) (RunResult, error) {
	r, err := h.runRecord(ctx, c, a.Run)
	if err != nil {
		return RunResult{}, err
	}
	return h.runResult(r), nil
}
func (h handlers) cancel(ctx context.Context, c identity.Caller, a CancelArgs) (RunEntry, error) {
	r, err := h.runRecord(ctx, c, a.Run)
	if err != nil {
		return RunEntry{}, err
	}
	r, err = h.Runs.Cancel(ctx, r.ID)
	if errors.Is(err, store.ErrEnded) {
		return RunEntry{}, fmt.Errorf(Ended, a.Run)
	}
	if err != nil {
		return RunEntry{}, catalogError(err)
	}
	return runEntry(r), nil
}

// Register installs the tools in their public discovery order.
func Register(srv *mcp.Server, cfg Config) {
	h := handlers{cfg}
	mcp.AddTool(srv, mcp.Tool[ListArgs, PromptList]{Name: "list", Description: "List your prompts.", Effect: mcp.Read, Handler: h.list})
	mcp.AddTool(srv, mcp.Tool[ShowArgs, Prompt]{Name: "show", Description: "Show a prompt.", Effect: mcp.Read, Handler: h.show})
	mcp.AddTool(srv, mcp.Tool[CreateArgs, Prompt]{Name: "create", Description: "Create a prompt.", Effect: mcp.Additive, Handler: h.create})
	mcp.AddTool(srv, mcp.Tool[UpdateArgs, Prompt]{Name: "update", Description: "Update a prompt.", Effect: mcp.Additive, Handler: h.update})
	mcp.AddTool(srv, mcp.Tool[DeleteArgs, Deleted]{Name: "delete", Description: "Delete a prompt and its runs.", Effect: mcp.Destructive, Handler: h.delete})
	mcp.AddTool(srv, mcp.Tool[SubscribeArgs, Prompt]{Name: "subscribe", Description: "Subscribe a prompt to an event pattern.", Effect: mcp.Additive, Handler: h.subscribe})
	mcp.AddTool(srv, mcp.Tool[UnsubscribeArgs, Prompt]{Name: "unsubscribe", Description: "Unsubscribe a prompt from an event pattern.", Effect: mcp.Destructive, Handler: h.unsubscribe})
	mcp.AddTool(srv, mcp.Tool[RunArgs, Started]{Name: "run", Description: "Start a prompt run.", Effect: mcp.Additive, Handler: h.run})
	mcp.AddTool(srv, mcp.Tool[RunsArgs, RunList]{Name: "runs", Description: "List a prompt's runs.", Effect: mcp.Read, Handler: h.runs})
	mcp.AddTool(srv, mcp.Tool[ResultArgs, RunResult]{Name: "result", Description: "Read a run's result and files.", Effect: mcp.Read, Handler: h.result})
	mcp.AddTool(srv, mcp.Tool[CancelArgs, RunEntry]{Name: "cancel", Description: "Cancel a queued or running run.", Effect: mcp.Destructive, Handler: h.cancel})
}
