package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const loadToolsName = "load_tools"

var loadToolsSchema = json.RawMessage(`{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"}}},"required":["names"]}`)

type orchestrator struct {
	inventory   []Tool
	base        []Tool
	byName      map[string]Tool
	deferred    map[string]Tool
	groups      map[string][]Tool
	loaded      map[string]struct{}
	loadedOrder *[]string
}

type deferredLoader struct {
	description string
}

func (l deferredLoader) Name() string          { return loadToolsName }
func (l deferredLoader) Description() string   { return l.description }
func (deferredLoader) Schema() json.RawMessage { return loadToolsSchema }
func (deferredLoader) isTool()                 {}
func (deferredLoader) Call(context.Context, json.RawMessage) (string, error) {
	return "", fmt.Errorf("agentkit: %s is orchestrator-managed", loadToolsName)
}

func newOrchestrator(eager []Tool, groups []DeferredGroup, loadedOrder *[]string) *orchestrator {
	o := &orchestrator{
		base:        cloneTools(eager),
		byName:      make(map[string]Tool, len(eager)),
		deferred:    make(map[string]Tool),
		groups:      make(map[string][]Tool, len(groups)),
		loaded:      make(map[string]struct{}),
		loadedOrder: loadedOrder,
	}
	o.indexEagerTools(eager)
	o.inventory = append(o.inventory, eager...)
	o.registerDeferredGroups(groups)
	if len(groups) > 0 {
		o.registerLoader(groups)
	}
	o.sortBase()
	o.applyLoadedOrder(loadedOrder)
	return o
}

func (o *orchestrator) indexEagerTools(eager []Tool) {
	for _, tool := range eager {
		if tool != nil {
			o.byName[tool.Name()] = tool
		}
	}
}

func (o *orchestrator) registerDeferredGroups(groups []DeferredGroup) {
	for _, group := range groups {
		o.groups[group.Name] = cloneTools(group.Tools)
		for _, tool := range group.Tools {
			o.inventory = append(o.inventory, tool)
			if tool != nil {
				o.deferred[tool.Name()] = tool
			}
		}
	}
}

func (o *orchestrator) registerLoader(groups []DeferredGroup) {
	loader := deferredLoader{description: deferredCatalog(groups)}
	o.inventory = append(o.inventory, loader)
	o.base = append(o.base, loader)
	o.byName[loader.Name()] = loader
}

func (o *orchestrator) sortBase() {
	sort.SliceStable(o.base, func(i, j int) bool {
		if o.base[i] == nil {
			return o.base[j] != nil
		}
		if o.base[j] == nil {
			return false
		}
		return o.base[i].Name() < o.base[j].Name()
	})
}

func (o *orchestrator) applyLoadedOrder(loadedOrder *[]string) {
	if loadedOrder != nil {
		for _, name := range *loadedOrder {
			if tool, exists := o.deferred[name]; exists {
				o.loaded[name] = struct{}{}
				o.byName[name] = tool
			}
		}
	}
}

func deferredCatalog(groups []DeferredGroup) string {
	var catalog strings.Builder
	catalog.WriteString("Load deferred tools by group or tool name. Catalog:\n")
	for _, group := range groups {
		fmt.Fprintf(&catalog, "%q: %q [", group.Name, group.Blurb)
		for index, tool := range group.Tools {
			if index > 0 {
				catalog.WriteString(", ")
			}
			if tool == nil {
				catalog.WriteString("<invalid nil tool>")
			} else {
				fmt.Fprintf(&catalog, "%q", tool.Name())
			}
		}
		catalog.WriteString("]\n")
	}
	return strings.TrimSuffix(catalog.String(), "\n")
}

// validateToolSet is the Send-time gate over the complete live inventory.
func validateToolSet(tools []Tool) error {
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if tool == nil {
			return fmt.Errorf("tool inventory contains nil tool")
		}
		name := tool.Name()
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("duplicate tool name %q", name)
		}
		seen[name] = struct{}{}
		if err := ValidateToolSchema(tool.Schema()); err != nil {
			return fmt.Errorf("tool %q has invalid schema: %w", name, err)
		}
	}
	return nil
}

func (o *orchestrator) advertisedSnapshot() []Tool {
	snapshot := cloneTools(o.base)
	if o.loadedOrder == nil {
		return snapshot
	}
	for _, name := range *o.loadedOrder {
		tool, exists := o.deferred[name]
		if !exists {
			continue
		}
		snapshot = append(snapshot, tool)
		o.byName[name] = tool
	}
	return snapshot
}

func (o *orchestrator) load(tool Tool) {
	if tool == nil {
		return
	}
	name := tool.Name()
	if _, exists := o.loaded[name]; exists {
		return
	}
	o.loaded[name] = struct{}{}
	if o.loadedOrder != nil {
		*o.loadedOrder = append(*o.loadedOrder, name)
	}
}

func (o *orchestrator) dispatchLoader(toolUseID string, names []string) ToolResult {
	result := ToolResult{ToolUseID: toolUseID}
	var unknown []string
	for _, name := range names {
		if tools, exists := o.groups[name]; exists {
			for _, tool := range tools {
				o.load(tool)
			}
			continue
		}
		if tool, exists := o.deferred[name]; exists {
			o.load(tool)
			continue
		}
		unknown = append(unknown, name)
	}
	result.Content = "Deferred tools loaded."
	if len(unknown) > 0 {
		result.Content += fmt.Sprintf(" Unknown names: %q.", unknown)
	}
	return result
}

func (o *orchestrator) dispatchLoaderSuspended(call ToolUse) ToolResult {
	return ToolResult{
		ToolUseID: call.ID,
		Content:   "agentkit: a savepoint is live; deferred-tool loading is suspended and no tools were loaded",
		IsError:   true,
	}
}

func validateToolArguments(schema, arguments json.RawMessage) (any, error) {
	var schemaNode map[string]any
	schemaDecoder := json.NewDecoder(bytes.NewReader(schema))
	schemaDecoder.UseNumber()
	if err := schemaDecoder.Decode(&schemaNode); err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	var value any
	argumentDecoder := json.NewDecoder(bytes.NewReader(arguments))
	argumentDecoder.UseNumber()
	if err := argumentDecoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureToolSchemaEOF(argumentDecoder); err != nil {
		return nil, err
	}
	if err := validateToolArgumentNode(schemaNode, value, "$"); err != nil {
		return nil, err
	}
	return value, nil
}

func loaderNames(arguments any) []string {
	items := arguments.(map[string]any)["names"].([]any)
	names := make([]string, len(items))
	for index, item := range items {
		names[index] = item.(string)
	}
	return names
}

// dispatch resolves and runs one model tool call. Every failure is returned
// in-band so the model can correct the call on its next round-trip.
func (o *orchestrator) dispatch(ctx context.Context, call ToolUse, savepointLive bool) ToolResult {
	result := ToolResult{ToolUseID: call.ID}
	tool, exists := o.byName[call.Name]
	if !exists {
		result.Content = fmt.Sprintf("agentkit: unknown tool %q", call.Name)
		result.IsError = true
		if deferred, known := o.deferred[call.Name]; known {
			if savepointLive {
				result.Content = fmt.Sprintf("agentkit: unknown tool %q because the deferred tool is not loaded; a live savepoint is suspending the load, so call %q after releasing it", call.Name, loadToolsName)
			} else {
				o.load(deferred)
				result.Content = fmt.Sprintf("agentkit: unknown tool %q because the deferred tool is not loaded; call %q first, then retry with its advertised schema", call.Name, loadToolsName)
			}
		}
		return result
	}
	arguments, err := validateToolArguments(tool.Schema(), call.Input)
	if err != nil {
		result.Content = fmt.Sprintf("agentkit: invalid arguments for tool %q: %v", call.Name, err)
		result.IsError = true
		return result
	}
	if _, managed := tool.(deferredLoader); managed {
		if savepointLive {
			return o.dispatchLoaderSuspended(call)
		}
		return o.dispatchLoader(call.ID, loaderNames(arguments))
	}
	return dispatchTool(ctx, tool, call)
}

// dispatchTool runs one regular tool call, reporting a call error in-band as
// an error result rather than failing the turn.
func dispatchTool(ctx context.Context, tool Tool, call ToolUse) ToolResult {
	result := ToolResult{ToolUseID: call.ID}
	content, err := tool.Call(ctx, append(json.RawMessage(nil), call.Input...))
	result.Content = content
	if err != nil {
		result.Content = err.Error()
		result.IsError = true
	}
	return result
}
