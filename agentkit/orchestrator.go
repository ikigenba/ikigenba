package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

const loadToolsName = "load_tools"

var loadToolsSchema = json.RawMessage(`{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"}}},"required":["names"]}`)

type orchestrator struct {
	inventory  []Tool
	advertised []Tool
	byName     map[string]Tool
	deferred   map[string]Tool
	pending    []Tool
}

func newOrchestrator(eager []Tool, groups []DeferredGroup) *orchestrator {
	o := &orchestrator{
		advertised: cloneTools(eager),
		byName:     make(map[string]Tool, len(eager)),
		deferred:   make(map[string]Tool),
	}
	for _, tool := range eager {
		if tool != nil {
			o.byName[tool.Name()] = tool
		}
	}
	o.inventory = append(o.inventory, eager...)
	for _, group := range groups {
		for _, tool := range group.Tools {
			o.inventory = append(o.inventory, tool)
			if tool != nil {
				o.deferred[tool.Name()] = tool
			}
		}
	}
	loader := concreteTool{
		name:        loadToolsName,
		description: "Load deferred tools by name",
		schema:      append(json.RawMessage(nil), loadToolsSchema...),
		call: func(context.Context, json.RawMessage) (string, error) {
			return "", fmt.Errorf("agentkit: %s is unavailable", loadToolsName)
		},
	}
	o.inventory = append(o.inventory, loader)
	if len(groups) > 0 {
		o.advertised = append(o.advertised, loader)
		o.byName[loader.Name()] = loader
	}
	return o
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
	snapshot := cloneTools(o.advertised)
	for _, tool := range o.pending {
		o.byName[tool.Name()] = tool
	}
	o.pending = nil
	return snapshot
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
			o.advertised = append(o.advertised, deferred)
			o.pending = append(o.pending, deferred)
			delete(o.deferred, call.Name)
		}
		return result
	}
	if err := validateToolArguments(tool.Schema(), call.Input); err != nil {
		result.Content = fmt.Sprintf("agentkit: invalid arguments for tool %q: %v", call.Name, err)
		result.IsError = true
		return result
	}
	content, err := tool.Call(ctx, append(json.RawMessage(nil), call.Input...))
	result.Content = content
	if err != nil {
		result.Content = err.Error()
		result.IsError = true
	}
	return result
}
