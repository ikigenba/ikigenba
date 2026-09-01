package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
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

type phase13Location struct {
	City string `json:"city" jsonschema:"required,description=destination city,minLength=2,maxLength=40,pattern=^[A-Za-z ]+$"`
}

type phase13TypedInput struct {
	Query    string          `json:"query" jsonschema:"required,enum=red|green|blue,description=color query,minLength=3,maxLength=12,pattern=^[a-z]+$,format=hostname"`
	Count    int             `json:"count" jsonschema:"minimum=1,maximum=20,multipleOf=1"`
	Ratio    float64         `json:"ratio" jsonschema:"exclusiveMinimum=0,exclusiveMaximum=1"`
	Enabled  bool            `json:"enabled" jsonschema:"enum=true|false"`
	Location phase13Location `json:"location" jsonschema:"required,description=search location"`
	Labels   []string        `json:"labels" jsonschema:"required,minItems=1,maxItems=4,uniqueItems=true"`
}

type phase13BadLengthTag struct {
	Value string `json:"value" jsonschema:"minLength=many"`
}

type phase13UnknownTag struct {
	Value string `json:"value" jsonschema:"madeUp=yes"`
}

type phase26UnsupportedInput struct {
	Updates chan string `json:"updates"`
}

type phase13MapInput struct {
	Values map[string]string `json:"values"`
}

func TestNewToolDerivesDocumentedTagVocabularyAndReturnsSchemaFaults(t *testing.T) {
	// R-3ZDR-CW24
	tool, err := NewTool("typed", "typed schema", func(context.Context, phase13TypedInput) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Type       string `json:"type"`
		Required   []string
		Properties map[string]struct {
			Type             string            `json:"type"`
			Description      string            `json:"description"`
			Enum             []any             `json:"enum"`
			Minimum          *float64          `json:"minimum"`
			Maximum          *float64          `json:"maximum"`
			ExclusiveMinimum *float64          `json:"exclusiveMinimum"`
			ExclusiveMaximum *float64          `json:"exclusiveMaximum"`
			MultipleOf       *float64          `json:"multipleOf"`
			MinLength        *int              `json:"minLength"`
			MaxLength        *int              `json:"maxLength"`
			Pattern          string            `json:"pattern"`
			Format           string            `json:"format"`
			MinItems         *int              `json:"minItems"`
			MaxItems         *int              `json:"maxItems"`
			UniqueItems      *bool             `json:"uniqueItems"`
			Items            map[string]string `json:"items"`
			Properties       map[string]any    `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	query := schema.Properties["query"]
	if schema.Type != "object" || !reflect.DeepEqual(schema.Required, []string{"query", "location", "labels"}) {
		t.Fatalf("derived root = type %q required %v", schema.Type, schema.Required)
	}
	if query.Type != "string" || query.Description != "color query" || !reflect.DeepEqual(query.Enum, []any{"red", "green", "blue"}) ||
		query.MinLength == nil || *query.MinLength != 3 || query.MaxLength == nil || *query.MaxLength != 12 || query.Pattern != "^[a-z]+$" || query.Format != "hostname" {
		t.Fatalf("derived constrained string = %#v", query)
	}
	count := schema.Properties["count"]
	if count.Minimum == nil || *count.Minimum != 1 || count.Maximum == nil || *count.Maximum != 20 || count.MultipleOf == nil || *count.MultipleOf != 1 {
		t.Fatalf("derived numeric constraints = %#v", count)
	}
	ratio := schema.Properties["ratio"]
	if ratio.ExclusiveMinimum == nil || *ratio.ExclusiveMinimum != 0 || ratio.ExclusiveMaximum == nil || *ratio.ExclusiveMaximum != 1 {
		t.Fatalf("derived exclusive constraints = %#v", ratio)
	}
	labels := schema.Properties["labels"]
	if labels.Type != "array" || labels.MinItems == nil || *labels.MinItems != 1 || labels.MaxItems == nil || *labels.MaxItems != 4 || labels.UniqueItems == nil || !*labels.UniqueItems || labels.Items["type"] != "string" {
		t.Fatalf("derived constrained array = %#v", labels)
	}
	location := schema.Properties["location"]
	if location.Type != "object" || location.Description != "search location" || location.Properties["city"] == nil {
		t.Fatalf("derived nested object = %#v", location)
	}

	invalid := []struct {
		name string
		call func() (Tool, error)
		want string
	}{
		{"malformed value", func() (Tool, error) {
			return NewTool("bad-length", "", func(context.Context, phase13BadLengthTag) (string, error) { return "", nil })
		}, "minLength"},
		{"unknown key", func() (Tool, error) {
			return NewTool("bad-key", "", func(context.Context, phase13UnknownTag) (string, error) { return "", nil })
		}, "madeUp"},
		{"derived noncanonical map", func() (Tool, error) {
			return NewTool("map", "", func(context.Context, phase13MapInput) (string, error) { return "", nil })
		}, "additionalProperties"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			panicked := false
			var gotErr error
			func() {
				defer func() { panicked = recover() != nil }()
				_, gotErr = test.call()
			}()
			if panicked || gotErr == nil || !strings.Contains(gotErr.Error(), test.want) {
				t.Fatalf("NewTool panicked=%t error=%v, want nonpanic diagnostic containing %q", panicked, gotErr, test.want)
			}
		})
	}
}

func TestMustToolMatchesNewToolAndPanicsOnTheSameInvalidSchema(t *testing.T) {
	// R-40LN-QNST
	callback := func(_ context.Context, input typedToolInput) (string, error) { return "seen:" + input.Query, nil }
	regular, err := NewTool("lookup", "description", callback)
	if err != nil {
		t.Fatal(err)
	}
	must := MustTool("lookup", "description", callback)
	if regular.Name() != must.Name() || regular.Description() != must.Description() || !bytes.Equal(regular.Schema(), must.Schema()) {
		t.Fatalf("NewTool and MustTool differ: %q/%q/%s versus %q/%q/%s", regular.Name(), regular.Description(), regular.Schema(), must.Name(), must.Description(), must.Schema())
	}
	for _, tool := range []Tool{regular, must} {
		got, callErr := tool.Call(context.Background(), json.RawMessage(`{"query":"value"}`))
		if callErr != nil || got != "seen:value" {
			t.Fatalf("identical callback adaptation = %q, %v", got, callErr)
		}
	}
	defer func() {
		panicValue := recover()
		if panicValue == nil || !strings.Contains(fmt.Sprint(panicValue), "additionalProperties") {
			t.Fatalf("MustTool panic = %v, want canonical-subset diagnostic", panicValue)
		}
	}()
	_ = MustTool("invalid", "", func(context.Context, phase13MapInput) (string, error) { return "", nil })
}

func TestNewToolFromSchemaValidatesAndPreservesRawBoundary(t *testing.T) {
	// R-41TK-4FJI
	schema := json.RawMessage(`{"type":"object","properties":{"names":{"type":"array","items":{"type":"string","minLength":1},"minItems":1}},"required":["names"]}`)
	args := json.RawMessage(`{"names":["Ada"]}`)
	var received json.RawMessage
	tool, err := NewToolFromSchema("raw", "dynamic", schema, func(_ context.Context, got json.RawMessage) (string, error) {
		received = got
		return "raw-result", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Call(context.Background(), args)
	if err != nil || result != "raw-result" || !bytes.Equal(received, args) || !bytes.Equal(tool.Schema(), schema) {
		t.Fatalf("raw tool result=%q err=%v received=%s schema=%s", result, err, received, tool.Schema())
	}
	if invalid, invalidErr := NewToolFromSchema("bad", "", json.RawMessage(`{"type":"object","additionalProperties":true}`), func(context.Context, json.RawMessage) (string, error) {
		return "", nil
	}); invalidErr == nil || invalid != nil || !strings.Contains(invalidErr.Error(), "additionalProperties") {
		t.Fatalf("invalid raw construction = %#v, %v", invalid, invalidErr)
	}
}

func TestOnlyMustToolPanicsForInvalidSchemas(t *testing.T) {
	// R-60JQ-B4JS
	tests := []struct {
		name string
		call func() (Tool, error)
	}{
		{name: "malformed typed tag", call: func() (Tool, error) {
			return NewTool("bad-tag", "", func(context.Context, phase13BadLengthTag) (string, error) { return "", nil })
		}},
		{name: "unsupported typed input", call: func() (Tool, error) {
			return NewTool("bad-type", "", func(context.Context, phase26UnsupportedInput) (string, error) { return "", nil })
		}},
		{name: "malformed raw JSON", call: func() (Tool, error) {
			return NewToolFromSchema("bad-json", "", json.RawMessage(`{"type":"object"`), func(context.Context, json.RawMessage) (string, error) { return "", nil })
		}},
		{name: "noncanonical raw schema", call: func() (Tool, error) {
			return NewToolFromSchema("bad-subset", "", json.RawMessage(`{"type":"object","additionalProperties":true}`), func(context.Context, json.RawMessage) (string, error) { return "", nil })
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panicked := false
			var tool Tool
			var err error
			func() {
				defer func() { panicked = recover() != nil }()
				tool, err = test.call()
			}()
			if panicked || err == nil || tool != nil {
				t.Fatalf("constructor result = tool %#v, error %v, panicked %t; want nil/useful error/no panic", tool, err, panicked)
			}
		})
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("MustTool accepted invalid schema without panicking")
		}
	}()
	_ = MustTool("must-fail", "", func(context.Context, phase26UnsupportedInput) (string, error) { return "", nil })
}

func TestValidateToolSchemaRecursivelyDefinesCanonicalSubset(t *testing.T) {
	// R-449C-VZ0W
	accepted := []json.RawMessage{
		json.RawMessage(`{"type":"object"}`),
		json.RawMessage(`{"type":"object","description":"input","properties":{"nested":{"type":"object","properties":{"name":{"type":"string","description":"name","enum":["a","b"],"minLength":1,"maxLength":4,"pattern":"^[ab]+$","format":"token"}},"required":["name"]},"values":{"type":"array","items":{"type":"number","minimum":0,"maximum":10,"exclusiveMinimum":-1,"exclusiveMaximum":11,"multipleOf":0.5},"minItems":1,"maxItems":3,"uniqueItems":true},"optional":{"anyOf":[{"type":"integer"},{"type":"null"}]}},"required":["nested","values"]}`),
	}
	for _, schema := range accepted {
		if err := ValidateToolSchema(schema); err != nil {
			t.Errorf("canonical schema rejected: %s: %v", schema, err)
		}
	}
	rejected := []struct {
		schema json.RawMessage
		want   string
	}{
		{json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"#/$defs/X"}}}`), "$ref"},
		{json.RawMessage(`{"type":"object","$defs":{"X":{"type":"string"}}}`), "$defs"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"type":"object","additionalProperties":false}}}`), "additionalProperties"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"type":"array"}}}`), "items"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"type":"array","items":[{"type":"string"}]}}}`), "tuple"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"type":"funky"}}}`), "funky"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"allOf":[{"type":"string"}]}}}`), "allOf"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[{"type":"string"},{"type":"number"}]}}}`), "anyOf"},
		{json.RawMessage(`{"type":"object","required":["missing"]}`), "missing"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","minLength":"one"}}}`), "minLength"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","pattern":"["}}}`), "pattern"},
	}
	for _, test := range rejected {
		err := ValidateToolSchema(test.schema)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("ValidateToolSchema(%s) = %v, want diagnostic containing %q", test.schema, err, test.want)
		}
	}
}
