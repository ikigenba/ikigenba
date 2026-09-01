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
	hasLoader   bool
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
	for _, tool := range eager {
		if tool != nil {
			o.byName[tool.Name()] = tool
		}
	}
	o.inventory = append(o.inventory, eager...)
	for _, group := range groups {
		o.groups[group.Name] = cloneTools(group.Tools)
		for _, tool := range group.Tools {
			o.inventory = append(o.inventory, tool)
			if tool != nil {
				o.deferred[tool.Name()] = tool
			}
		}
	}
	if len(groups) > 0 {
		loader := concreteTool{
			name:        loadToolsName,
			description: deferredCatalog(groups),
			schema:      append(json.RawMessage(nil), loadToolsSchema...),
			call: func(context.Context, json.RawMessage) (string, error) {
				return "", fmt.Errorf("agentkit: %s is orchestrator-managed", loadToolsName)
			},
		}
		o.inventory = append(o.inventory, loader)
		o.base = append(o.base, loader)
		o.byName[loader.Name()] = loader
		o.hasLoader = true
	}
	sort.SliceStable(o.base, func(i, j int) bool {
		if o.base[i] == nil {
			return o.base[j] != nil
		}
		if o.base[j] == nil {
			return false
		}
		return o.base[i].Name() < o.base[j].Name()
	})
	if loadedOrder != nil {
		for _, name := range *loadedOrder {
			if tool, exists := o.deferred[name]; exists {
				o.loaded[name] = struct{}{}
				o.byName[name] = tool
			}
		}
	}
	return o
}

func deferredCatalog(groups []DeferredGroup) string {
	var catalog strings.Builder
	catalog.WriteString("Load deferred tools by group or tool name. Catalog:\n")
	for _, group := range groups {
		fmt.Fprintf(&catalog, "%s: %s [", group.Name, group.Blurb)
		for index, tool := range group.Tools {
			if index > 0 {
				catalog.WriteString(", ")
			}
			if tool == nil {
				catalog.WriteString("<invalid nil tool>")
			} else {
				catalog.WriteString(tool.Name())
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

func (o *orchestrator) dispatchLoader(call ToolUse) ToolResult {
	result := ToolResult{ToolUseID: call.ID}
	var input struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal(call.Input, &input); err != nil {
		result.Content = fmt.Sprintf("agentkit: invalid arguments for tool %q: %v", loadToolsName, err)
		result.IsError = true
		return result
	}
	var unknown []string
	for _, name := range input.Names {
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
		result.Content += fmt.Sprintf(" Unknown names: %s.", strings.Join(unknown, ", "))
	}
	return result
}

func validateToolArguments(schema, arguments json.RawMessage) error {
	var schemaNode map[string]any
	schemaDecoder := json.NewDecoder(bytes.NewReader(schema))
	schemaDecoder.UseNumber()
	if err := schemaDecoder.Decode(&schemaNode); err != nil {
		return fmt.Errorf("decode schema: %w", err)
	}
	var value any
	argumentDecoder := json.NewDecoder(bytes.NewReader(arguments))
	argumentDecoder.UseNumber()
	if err := argumentDecoder.Decode(&value); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureToolSchemaEOF(argumentDecoder); err != nil {
		return err
	}
	return validateToolArgumentNode(schemaNode, value, "$")
}

// dispatch resolves and runs one model tool call. Every failure is returned
// in-band so the model can correct the call on its next round-trip.
func (o *orchestrator) dispatch(ctx context.Context, call ToolUse) ToolResult {
	result := ToolResult{ToolUseID: call.ID}
	tool, exists := o.byName[call.Name]
	if !exists {
		result.Content = fmt.Sprintf("agentkit: unknown tool %q", call.Name)
		result.IsError = true
		if deferred, known := o.deferred[call.Name]; known {
			o.load(deferred)
			result.Content = fmt.Sprintf("agentkit: unknown tool %q because the deferred tool is not loaded; call %q first, then retry with its advertised schema", call.Name, loadToolsName)
		}
		return result
	}
	if err := validateToolArguments(tool.Schema(), call.Input); err != nil {
		result.Content = fmt.Sprintf("agentkit: invalid arguments for tool %q: %v", call.Name, err)
		result.IsError = true
		return result
	}
	if call.Name == loadToolsName && o.hasLoader {
		return o.dispatchLoader(call)
	}
	content, err := tool.Call(ctx, append(json.RawMessage(nil), call.Input...))
	result.Content = content
	if err != nil {
		result.Content = err.Error()
		result.IsError = true
	}
	return result
}
