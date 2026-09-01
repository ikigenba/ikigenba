package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type typedToolInput struct {
	Query string `json:"query" jsonschema:"required"`
}

func TestTypedToolExposesContractAndAdaptsRawArguments(t *testing.T) {
	wantErr := errors.New("callback failure")
	var received typedToolInput
	tool, err := NewTool("lookup", "look up a query", func(_ context.Context, input typedToolInput) (string, error) {
		received = input
		return "typed result", wantErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.Name() != "lookup" || tool.Description() != "look up a query" {
		t.Fatalf("tool identity = %q/%q", tool.Name(), tool.Description())
	}
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Type != "object" || schema.Properties["query"] == nil {
		t.Fatalf("derived schema = %s, want object with query property", tool.Schema())
	}
	result, callErr := tool.Call(context.Background(), json.RawMessage(`{"query":"weather"}`))
	if result != "typed result" || !errors.Is(callErr, wantErr) || received.Query != "weather" {
		t.Fatalf("Call = %q, %v with input %#v", result, callErr, received)
	}

	must := MustTool("must", "must constructor", func(context.Context, typedToolInput) (string, error) {
		return "must result", nil
	})
	if got, err := must.Call(context.Background(), json.RawMessage(`{"query":"value"}`)); err != nil || got != "must result" {
		t.Fatalf("MustTool Call = %q, %v", got, err)
	}
}

func TestRawSchemaToolPreservesSchemaAndCallbackBoundary(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)
	args := json.RawMessage(`{"query":"weather"}`)
	var received json.RawMessage
	tool, err := NewToolFromSchema("raw", "raw schema", schema, func(_ context.Context, input json.RawMessage) (string, error) {
		received = input
		return "raw result", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.Name() != "raw" || tool.Description() != "raw schema" || !reflect.DeepEqual(tool.Schema(), schema) {
		t.Fatalf("raw tool contract = %q/%q/%s", tool.Name(), tool.Description(), tool.Schema())
	}
	result, err := tool.Call(context.Background(), args)
	if err != nil || result != "raw result" || !reflect.DeepEqual(received, args) {
		t.Fatalf("Call = %q, %v with args %s", result, err, received)
	}
}

func TestValidateToolSchemaIsCallablePublicChecker(t *testing.T) {
	if err := ValidateToolSchema(json.RawMessage(`{"type":"object"}`)); err != nil {
		t.Fatalf("valid object schema rejected: %v", err)
	}
	if err := ValidateToolSchema(json.RawMessage(`{"type":"string"}`)); err == nil {
		t.Fatal("non-object schema accepted")
	}
}
